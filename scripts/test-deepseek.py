"""Opt-in, bounded real DeepSeek hybrid acceptance (never run by normal CI).

Uses only Python's standard library and an already built FakeAPI binary.
Credentials stay in memory/child environment; reports contain no message bodies.
"""
import argparse
from datetime import datetime, timezone
import getpass
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sys
import tempfile
import time
import traceback
import urllib.error
import urllib.request
import uuid
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('runtime_smoke', ROOT / 'scripts/runtime-smoke.py')
SMOKE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SMOKE)
BASE_URL = 'https://api.deepseek.com'
AI_CALL_LIMIT = 12


def configure(backend, credential, model):
    # Work only on the freshly initialized, temporary hybrid deployment.
    source = Path(backend.config).read_text(encoding='utf-8')
    source = source.replace('developer_role_fallback: reject', 'developer_role_fallback: system')
    for field in ('json_schema', 'developer_role', 'parallel_tool_calls_parameter'):
        source = source.replace(field + ': true', field + ': false')
    source = source.replace('write_timeout: 10s', 'write_timeout: 30s')
    Path(backend.config).write_text(source, encoding='utf-8', newline='\n')
    backend.env.update(FAKEAPI_UPSTREAM_BASE_URL=BASE_URL,
                       FAKEAPI_UPSTREAM_API_KEY=credential,
                       FAKEAPI_UPSTREAM_MODEL=model,
                       FAKEAPI_UPSTREAM_SVG_MODEL=model)


def key(backend, policy, ai_limit):
    created = json.loads(backend.cli('keys', 'create', '--policy', policy,
                                    '--ai-limit', str(ai_limit), '--total-limit', '32',
                                    '--rpm', 'unlimited', '--format', 'json'))
    SMOKE.PRIVATE_VALUES.append(created['key'])
    return created['key'], created['metadata']['id']


def metadata(backend, key_id):
    value = json.loads(backend.cli('keys', 'show', '--id', key_id))
    return value['total_used'], value['ai_used']


def request(backend, path, credential, body, headers=None, status=200):
    values = {'Authorization': 'Bearer ' + credential, 'Content-Type': 'application/json', **(headers or {})}
    req = urllib.request.Request(backend.url + path, headers=values,
                                 data=json.dumps(body, ensure_ascii=False).encode('utf-8'))
    start = time.monotonic()
    try:
        response = SMOKE.HTTP.open(req, timeout=135)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        raw = response.read().decode('utf-8')
        code = None
        if response.status != status:
            try:
                code = json.loads(raw).get('error', {}).get('code')
            except (ValueError, AttributeError):
                pass
        assert response.status == status, f'HTTP {response.status}, expected {status}, code={code}'
        return raw, response.headers, round((time.monotonic() - start) * 1000)


def events(raw):
    result = []
    for line in raw.splitlines():
        if line.startswith('data:'):
            value = line[5:].strip()
            if value and value != '[DONE]':
                result.append(json.loads(value))
    assert result and not any('error' in value or value.get('type') == 'error' for value in result), 'stream error'
    return result


def chat(text, **options):
    return {'model': 'fakeapi-test', 'messages': [{'role': 'user', 'content': text}],
            'max_tokens': 768, **options}


def chat_text(raw):
    return json.loads(raw)['choices'][0]['message']['content']


def extract_svg(text):
    # Real chat models can put a valid SVG inside Markdown and introductory text.
    # Validate the artifact without altering the API response itself.
    begin = text.find('<svg')
    end = text.find('</svg>', begin)
    assert begin >= 0 and end >= 0, 'complete SVG missing from reply'
    svg = text[begin:end + len('</svg>')]
    assert ET.fromstring(svg).tag.rsplit('}', 1)[-1] == 'svg', 'invalid pelican SVG'
    return svg


def verify_logs(backend):
    raw = backend.logs()
    assert all(secret not in raw for secret in SMOKE.PRIVATE_VALUES), 'credential found in log'
    entries = []
    for line in raw.splitlines():
        if line.startswith('{'):
            entry = json.loads(line)
            assert set(entry) == {'time', 'model', 'duration_ms', 'rule'}, 'unexpected log field'
            entries.append(entry)
    assert entries, 'request logs missing'
    return entries


def run(backend, passed, report):
    primary, primary_id = key(backend, 'hybrid', AI_CALL_LIMIT)
    demo, demo_id = key(backend, 'demo', 0)
    blocked, blocked_id = key(backend, 'hybrid', 0)
    backend.start()
    SMOKE.ready(backend)
    assert backend.cli('config', 'check').strip() == 'configuration valid'

    def ok(label, duration=None, **details):
        passed.append({'check': label, **({'duration_ms': duration} if duration is not None else {}), **details})
        print('PASS', label, flush=True)

    SMOKE.request(backend, '/v1/models', primary)
    assert metadata(backend, primary_id) == (0, 0)
    ok('hybrid initialization and model discovery without quota charges')
    raw, _, _ = request(backend, '/v1/chat/completions', demo, chat('你好', reasoning_effort='none'))
    assert chat_text(raw) and metadata(backend, demo_id) == (1, 0)
    ok('demo Key stays on the local greeting rule')

    raw, _, _ = request(backend, '/v1/chat/completions', blocked, chat('你好'), status=429)
    assert json.loads(raw)['error']['code'] == 'ai_quota_exceeded'
    assert metadata(backend, blocked_id) == (0, 0)
    ok('zero AI quota rejects before charging total usage')

    ordinary = chat('请帮我设计一个图书管理系统', max_tokens=4096)
    ordinary['messages'] = [{'role': 'user', 'content': '你好'}, {'role': 'assistant', 'content': '你好'}, *ordinary['messages']]
    raw, _, _ = request(backend, '/v1/chat/completions', primary, ordinary)
    portraits = [path.read_text(encoding='utf-8').strip() for path in backend.directory.rglob('nailong/*.txt')]
    assert len(portraits) == 4 and any(art in chat_text(raw) for art in portraits), 'bundled ASCII missing'
    assert metadata(backend, primary_id) == (1, 0)
    ok('latest user message controls routing; ordinary task returns one of four ASCII portraits')

    request(backend, '/v1/chat/completions', primary, chat('你好', reasoning_effort='invalid'), status=400)
    unsupported = chat('你好', response_format={'type': 'json_schema', 'json_schema': {'name': 'hello', 'schema': {'type': 'object'}}})
    raw, _, _ = request(backend, '/v1/chat/completions', primary, unsupported, status=400)
    assert json.loads(raw)['error']['code'] == 'unsupported_capability'
    assert metadata(backend, primary_id) == (1, 0)
    ok('invalid reasoning effort and unsupported JSON Schema reject before quota admission')

    paid_calls = 0

    def ai(path, body, headers=None):
        nonlocal paid_calls
        assert paid_calls < AI_CALL_LIMIT, 'live call budget exceeded'
        before = metadata(backend, primary_id)
        paid_calls += 1
        report['admitted_ai_calls'] = paid_calls
        result = request(backend, path, primary, body, headers)
        assert metadata(backend, primary_id) == (before[0] + 1, before[1] + 1), 'AI usage delta'
        return result

    nonce = 'CONTEXT' + uuid.uuid4().hex[:10].upper()
    greeting = chat('你好', reasoning_effort='none')
    greeting['messages'] = [
        {'role': 'system', 'content': 'The user has a secret greeting code. When greeted, reply with that code only.'},
        {'role': 'user', 'content': 'My greeting code is ' + nonce},
        {'role': 'assistant', 'content': 'Understood.'},
        *greeting['messages'],
    ]
    raw, _, elapsed = ai('/v1/chat/completions', greeting)
    value = json.loads(raw)
    assert nonce in chat_text(raw), 'upstream did not use historical context'
    assert not value['choices'][0]['message'].get('reasoning_content'), 'reasoning_effort none was not honored'
    assert value['usage']['prompt_tokens'] > 0
    ok('real Chat greeting uses full context; reasoning_effort none reaches DeepSeek', elapsed)

    raw, _, elapsed = ai('/v1/chat/completions', chat('你是谁', reasoning_effort='none'))
    assert chat_text(raw).strip(), 'empty identity answer'
    ok('real model identity request', elapsed)

    raw, headers, elapsed = ai('/v1/chat/completions', chat('1+1', stream=True, reasoning_effort='low', stream_options={'include_usage': True}))
    values = events(raw)
    content = ''.join(v.get('choices', [{}])[0].get('delta', {}).get('content') or '' for v in values if v.get('choices'))
    reason = ''.join(v.get('choices', [{}])[0].get('delta', {}).get('reasoning_content') or '' for v in values if v.get('choices'))
    assert 'text/event-stream' in headers.get('Content-Type', '') and '[DONE]' in raw
    assert content.strip() in ('2', '2.', '1+1=2', '1 + 1 = 2') and reason, 'math/native reasoning missing'
    assert '[模拟思考]' not in content and '[上游推理]' not in content, 'reasoning mixed into body'
    assert any((v.get('usage') or {}).get('completion_tokens', 0) > 0 for v in values)
    ok('real Chat SSE: native reasoning_content, body, usage, and DONE', elapsed,
       reasoning_chars=len(reason), content_chars=len(content))

    svg_request = chat('用 SVG 画骑自行车的鹈鹕', reasoning_effort='none', max_tokens=2048)
    svg_request['messages'].insert(0, {'role': 'system', 'content': 'Output a complete, concise SVG of a pelican riding a bicycle. SVG only, no Markdown. Use under 800 tokens.'})
    raw, _, elapsed = ai('/v1/chat/completions', svg_request)
    svg = extract_svg(chat_text(raw))
    fixture = next(backend.directory.rglob('pelican-bicycle.svg.txt')).read_text(encoding='utf-8').strip()
    assert svg != fixture, 'static fixture returned instead of real AI'
    ok('real pelican bicycle reply contains valid SVG distinct from the bundled fixture', elapsed, svg_chars=len(svg))

    instruction = 'Respond with exactly OK. No punctuation, explanation, or Markdown.'
    protocols = [
        ('Responses', '/v1/responses', {'model': 'fakeapi-test', 'input': '只回答 OK', 'instructions': instruction, 'max_output_tokens': 768}, {},
         lambda v: ''.join(c.get('text', '') for o in v['output'] for c in o.get('content', [])), 'response.completed'),
        ('Anthropic', '/anthropic/v1/messages', {**chat('只回答 OK'), 'system': instruction}, {'anthropic-version': '2023-06-01'},
         lambda v: ''.join(c.get('text', '') for c in v['content']), 'message_stop'),
        ('Gemini', '/v1beta/models/fakeapi-test:generateContent', {'contents': [{'parts': [{'text': '只回答 OK'}]}], 'systemInstruction': {'parts': [{'text': instruction}]}, 'generationConfig': {'maxOutputTokens': 768}}, {},
         lambda v: ''.join(p.get('text', '') for p in v['candidates'][0]['content']['parts']), 'STOP'),
    ]
    for name, path, body, headers, text, terminator in protocols:
        raw, _, elapsed = ai(path, body, headers)
        assert text(json.loads(raw)).strip() == 'OK', name + ' body'
        ok('real ' + name + ' JSON through the Chat upstream adapter', elapsed)
        stream_body = {**body, 'stream': True}
        stream_path = path
        if name == 'Gemini':
            stream_path = path.replace(':generateContent', ':streamGenerateContent?alt=sse')
            stream_body = body
        raw, stream_headers, elapsed = ai(stream_path, stream_body, headers)
        values = events(raw)
        assert 'text/event-stream' in stream_headers.get('Content-Type', '') and terminator in raw
        if name == 'Responses':
            body_text = ''.join(v.get('delta', '') for v in values if v.get('type') == 'response.output_text.delta')
        elif name == 'Anthropic':
            body_text = ''.join(v.get('delta', {}).get('text', '') for v in values if v.get('type') == 'content_block_delta')
        else:
            body_text = ''.join(p.get('text', '') for v in values for c in v.get('candidates', []) for p in c.get('content', {}).get('parts', []))
        assert body_text.strip() == 'OK', name + ' stream body'
        ok('real ' + name + ' SSE with native completion', elapsed)

    json_request = chat('你好', reasoning_effort='none', response_format={'type': 'json_object'})
    json_request['messages'].insert(0, {'role': 'system', 'content': 'Reply to the greeting with JSON only: an object with a greeting string. JSON is required.'})
    raw, _, elapsed = ai('/v1/chat/completions', json_request)
    assert isinstance(json.loads(chat_text(raw)), dict)
    ok('real JSON object mode without mock thinking text', elapsed)

    raw, _, elapsed = ai('/v1/chat/completions', chat('1+1', reasoning_effort='none'))
    assert chat_text(raw).strip() in ('2', '2.', '1+1=2', '1 + 1 = 2')
    ok('real simple math with thinking disabled', elapsed)
    assert paid_calls == AI_CALL_LIMIT
    before = metadata(backend, primary_id)
    raw, _, _ = request(backend, '/v1/chat/completions', primary, chat('你好'), status=429)
    assert json.loads(raw)['error']['code'] == 'ai_quota_exceeded' and metadata(backend, primary_id) == before
    raw, _, _ = request(backend, '/v1/chat/completions', primary, ordinary)
    assert any(art in chat_text(raw) for art in portraits)
    assert metadata(backend, primary_id) == (before[0] + 1, before[1])
    ok('exhausted AI quota keeps local ASCII available and rejected AI requests uncharged')
    backend.stop()
    entries = verify_logs(backend)
    assert {'ai-greeting', 'ai-identity', 'ai-tests', 'ai-pelican', 'default-frog', 'preset-greeting'} <= {e['rule'] for e in entries}
    ok('four-field request logs contain matched rules and no credentials')
    return paid_calls


def authentication_failure(directory, binary, model, passed):
    backend = SMOKE.Native(directory, binary, template='hybrid')
    configure(backend, 'sk-fakeapi-invalid-live-test', model)
    local_key, key_id = key(backend, 'hybrid', 1)
    try:
        backend.start()
        SMOKE.ready(backend)
        raw, _, elapsed = request(backend, '/v1/chat/completions', local_key, chat('你好'), status=502)
        assert json.loads(raw)['error']['code'] == 'upstream_authentication_error'
        assert metadata(backend, key_id) == (1, 1), 'admitted failed AI call accounting'
        backend.stop()
        verify_logs(backend)
        passed.append({'check': 'real provider authentication error is sanitized and charged once', 'duration_ms': elapsed})
        print('PASS real provider authentication error is sanitized and charged once', flush=True)
    finally:
        backend.cleanup()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, default=ROOT / 'bin' / ('fakeapi.exe' if os.name == 'nt' else 'fakeapi'))
    parser.add_argument('--model', default='deepseek-flash')
    parser.add_argument('--key-stdin', action='store_true', help='read one credential line from stdin instead of the environment/hidden prompt')
    parser.add_argument('--report', type=Path, default=ROOT / '.cache/deepseek-live/report.json')
    args = parser.parse_args()
    if args.key_stdin and sys.stdin.isatty():
        parser.error('--key-stdin requires piped input; omit it for the hidden prompt')
    credential = sys.stdin.readline().strip() if args.key_stdin else os.environ.get('FAKEAPI_UPSTREAM_API_KEY', '').strip()
    if not credential and not args.key_stdin and sys.stdin.isatty():
        credential = getpass.getpass('DeepSeek test key (hidden): ')
    if not credential:
        parser.error('set FAKEAPI_UPSTREAM_API_KEY or use an interactive hidden prompt/--key-stdin')
    SMOKE.PRIVATE_VALUES.append(credential)
    passed = []
    report = {'provider': 'DeepSeek', 'base_url': BASE_URL, 'model': args.model,
              'started_at': datetime.now(timezone.utc).isoformat(), 'ai_call_limit': AI_CALL_LIMIT,
              'max_output_tokens_per_call': 2048, 'checks': passed, 'status': 'failed'}
    try:
        report['binary_sha256'] = hashlib.sha256(args.binary.read_bytes()).hexdigest()
        report['binary_version'] = SMOKE.execute([str(args.binary.resolve()), 'version']).strip()
        req = urllib.request.Request(BASE_URL + '/models', headers={'Authorization': 'Bearer ' + credential})
        with urllib.request.urlopen(req, timeout=25) as response:
            catalog = json.load(response)
        assert args.model in {v['id'] for v in catalog['data']}, 'requested model not present in provider catalog'
        passed.append({'check': 'real provider authentication and model catalog'})
        print('PASS real provider authentication and model catalog', flush=True)
        args.report.parent.mkdir(parents=True, exist_ok=True)
        with tempfile.TemporaryDirectory(prefix='run-', dir=args.report.parent) as temporary:
            backend = SMOKE.Native(Path(temporary) / 'success', args.binary.resolve(), template='hybrid')
            configure(backend, credential, args.model)
            try:
                report['successful_ai_calls'] = run(backend, passed, report)
            finally:
                backend.cleanup()
            authentication_failure(Path(temporary) / 'authentication-failure', args.binary.resolve(), args.model, passed)
        report['status'] = 'passed'
    except Exception as error:
        # Never serialize provider messages, credentials, raw bodies, or tracebacks.
        report['error_type'] = type(error).__name__
        frame = traceback.extract_tb(error.__traceback__)[-1]
        report['error_location'] = {'file': Path(frame.filename).name, 'function': frame.name, 'line': frame.lineno}
        if isinstance(error, AssertionError):
            report['error'] = str(error)
        elif isinstance(error, urllib.error.HTTPError):
            report['provider_http_status'] = error.code
        print('FAIL', report.get('error', report['error_type']), flush=True)
    finally:
        report['finished_at'] = datetime.now(timezone.utc).isoformat()
        args.report.parent.mkdir(parents=True, exist_ok=True)
        encoded = json.dumps(report, ensure_ascii=False, indent=2) + '\n'
        for secret in SMOKE.PRIVATE_VALUES:
            encoded = encoded.replace(secret, '<redacted>')
        args.report.write_text(encoded, encoding='utf-8', newline='\n')
        print('Report:', args.report, flush=True)
    return 0 if report['status'] == 'passed' else 1


if __name__ == '__main__':
    sys.exit(main())
