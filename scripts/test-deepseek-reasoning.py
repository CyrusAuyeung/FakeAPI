"""Opt-in real multi-turn/controls acceptance: five successful AI calls maximum."""
from datetime import datetime, timezone
import getpass
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('live', ROOT / 'scripts/test-deepseek.py')
LIVE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(LIVE)


def main():
    credential = os.environ.get('FAKEAPI_UPSTREAM_API_KEY', '')
    if not credential and sys.stdin.isatty():
        credential = getpass.getpass('DeepSeek test key (hidden): ')
    if not credential:
        print('Set FAKEAPI_UPSTREAM_API_KEY or use an interactive terminal.', file=sys.stderr)
        return 2
    LIVE.SMOKE.PRIVATE_VALUES.append(credential)
    binary = ROOT / 'bin' / ('fakeapi.exe' if os.name == 'nt' else 'fakeapi')
    output = ROOT / '.cache/deepseek-live/reasoning-report.json'
    output.parent.mkdir(parents=True, exist_ok=True)
    passed = []
    report = {'provider': 'DeepSeek', 'model': 'deepseek-flash', 'status': 'failed',
              'started_at': datetime.now(timezone.utc).isoformat(), 'ai_call_limit': 5,
              'max_output_tokens_per_call': 768, 'checks': passed,
              'binary_sha256': hashlib.sha256(binary.read_bytes()).hexdigest()}
    try:
        report['binary_version'] = LIVE.SMOKE.execute([str(binary), 'version']).strip()
        with tempfile.TemporaryDirectory(prefix='reasoning-', dir=output.parent) as temporary:
            backend = LIVE.SMOKE.Native(Path(temporary), binary, template='deepseek')
            LIVE.configure(backend, credential, 'deepseek-flash')
            local_key, key_id = LIVE.key(backend, 'hybrid', 5)
            calls = 0

            def call(label, path, body, headers=None):
                nonlocal calls
                assert calls < 5, 'call budget exceeded'
                calls += 1
                report['admitted_ai_calls'] = calls
                raw, response_headers, elapsed = LIVE.request(backend, path, local_key, body, headers)
                assert LIVE.metadata(backend, key_id) == (calls, calls), 'quota delta'
                return raw, response_headers, elapsed

            def ok(label, elapsed=None):
                passed.append({'check': label, **({'duration_ms': elapsed} if elapsed is not None else {})})
                print('PASS', label, flush=True)

            try:
                backend.start()
                LIVE.SMOKE.ready(backend)
                raw, _, elapsed = call('native', '/v1/chat/completions', LIVE.chat('1+1', stream=True, reasoning_effort='low'))
                values = LIVE.events(raw)
                content = ''.join(v['choices'][0].get('delta', {}).get('content') or '' for v in values if v.get('choices'))
                reasoning = ''.join(v['choices'][0].get('delta', {}).get('reasoning_content') or '' for v in values if v.get('choices'))
                assert reasoning and re.search(r'(?<!\d)2(?!\d)', content) and '[DONE]' in raw, 'native math stream incomplete'
                ok('real native Chat reasoning stream', elapsed)

                replay = LIVE.chat('你好', reasoning_effort='none')
                replay['messages'] = [{'role': 'user', 'content': '1+1'},
                                      {'role': 'assistant', 'content': content, 'reasoning_content': reasoning},
                                      {'role': 'user', 'content': '你好'}]
                raw, _, elapsed = call('replay', '/v1/chat/completions', replay)
                reply = json.loads(raw)
                assert LIVE.chat_text(raw).strip() and not reply['choices'][0]['message'].get('reasoning_content'), 'replay/disable failed'
                assert reply['usage']['completion_tokens_details']['reasoning_tokens'] == 0
                ok('nonempty reasoning history replay and disabled next Chat turn', elapsed)

                raw, _, elapsed = call('responses', '/v1/responses', {'model': 'fakeapi-test', 'input': '只回答 OK', 'max_output_tokens': 768, 'reasoning': {'effort': 'none'}})
                reply = json.loads(raw)
                assert reply['usage']['output_tokens_details']['reasoning_tokens'] == 0, 'Responses did not disable upstream thinking'
                assert any(p.get('text', '').strip() == 'OK' for o in reply['output'] for p in o.get('content', [])), 'Responses text'
                ok('Responses effort none maps to real upstream disable', elapsed)

                raw, _, elapsed = call('anthropic', '/anthropic/v1/messages', {**LIVE.chat('只回答 OK'), 'thinking': {'type': 'disabled'}}, {'anthropic-version': '2023-06-01'})
                reply = json.loads(raw)
                assert ''.join(p.get('text', '') for p in reply['content']).strip() == 'OK' and reply['usage']['output_tokens'] <= 16, 'Anthropic disable/text'
                ok('Anthropic disabled thinking produces a short real answer', elapsed)

                raw, _, elapsed = call('gemini', '/v1beta/models/fakeapi-test:generateContent', {'contents': [{'parts': [{'text': '只回答 OK'}]}], 'generationConfig': {'maxOutputTokens': 768, 'thinkingConfig': {'thinkingBudget': 0}}})
                reply = json.loads(raw)
                assert reply['usageMetadata']['thoughtsTokenCount'] == 0, 'Gemini zero budget did not disable thinking'
                assert ''.join(p.get('text', '') for p in reply['candidates'][0]['content']['parts']).strip() == 'OK', 'Gemini text'
                ok('Gemini zero budget maps to real upstream disable', elapsed)

                raw, _, _ = LIVE.request(backend, '/v1/chat/completions', local_key, LIVE.chat('你好'), status=429)
                assert json.loads(raw)['error']['code'] == 'ai_quota_exceeded' and LIVE.metadata(backend, key_id) == (5, 5)
                replay['messages'][-1]['content'] = '请帮我设计一个图书管理系统'
                replay['max_tokens'] = 4096
                raw, _, _ = LIVE.request(backend, '/v1/chat/completions', local_key, replay)
                portraits = [p.read_text(encoding='utf-8').strip() for p in backend.directory.rglob('nailong/*.txt')]
                assert any(p in LIVE.chat_text(raw) for p in portraits) and LIVE.metadata(backend, key_id) == (6, 5)
                ok('reasoning replay still permits local ASCII after AI quota exhaustion')
                backend.stop()
                LIVE.verify_logs(backend)
                ok('request logs contain exactly four fields and no credentials')
                report['successful_ai_calls'] = calls
            finally:
                backend.cleanup()
        report['status'] = 'passed'
    except Exception as error:
        report['error_type'] = type(error).__name__
        if isinstance(error, AssertionError):
            report['error'] = str(error)
        print('FAIL', report.get('error') or type(error).__name__, flush=True)
    finally:
        report['finished_at'] = datetime.now(timezone.utc).isoformat()
        encoded = json.dumps(report, ensure_ascii=False, indent=2) + '\n'
        for value in LIVE.SMOKE.PRIVATE_VALUES:
            encoded = encoded.replace(value, '<redacted>')
        output.write_text(encoded, encoding='utf-8')
        print('Report:', output, flush=True)
    return 0 if report['status'] == 'passed' else 1


if __name__ == '__main__':
    sys.exit(main())
