"""Start an isolated FakeAPI deployment for the pinned Cherry Studio UI test."""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('runtime_smoke', ROOT / 'scripts/runtime-smoke.py')
runtime = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runtime)


def main():
    cache = ROOT / '.cache'
    cache.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='cherry-server-', dir=cache) as directory:
        assert Path(directory).resolve().is_relative_to(cache.resolve())
        backend = runtime.Native(Path(directory), ROOT / 'bin/fakeapi')
        config = Path(backend.config)
        source = config.read_text(encoding='utf-8').replace('write_timeout: 10s', 'write_timeout: 60s')
        if os.environ.get('FAKEAPI_RECORD_STREAM') == '1':
            # Exercise the public configurable pacing with no upstream AI.
            source = source.replace('thinking: {enabled: true, channel: portable_text, text_asset: thinking-natural}',
                                    'thinking: {enabled: true, channel: native, text_asset: thinking-natural}', 1)
            source = source.replace('- id: fakeapi-frog\n  display_name: 奶蛙 · 自然\n  profile: instant',
                                    '- id: fakeapi-frog\n  display_name: 奶蛙 · 自然\n  profile: natural', 1)
            source = source.replace('first_output_delay: 300ms, thinking_to_body_delay: 300ms, graphemes_per_second: 180, chunk_size: 8,',
                                    'first_output_delay: 1200ms, thinking_to_body_delay: 1200ms, graphemes_per_second: 450, chunk_size: 16,', 1)
            source = source.replace('jitter: 20ms, pause_probability: 0.02, pause_duration: 100ms, max_total: 30s',
                                    'jitter: 0ms, pause_probability: 0, pause_duration: 0ms, max_total: 30s', 1)
        wave = (config.parent / 'assets/nailong/wave.txt').read_text(encoding='utf-8')
        # A temporary slow route makes the real client's Stop button testable.
        source = source.replace('matchers:\n', 'matchers:\n  client-cancel:\n    any:\n    - {exact: /client/cancel}\n', 1)
        source = source.replace('rules:\n', 'rules:\n- id: client-cancel\n  enabled: true\n  priority: 2000\n'
                                '  when: {matchers_any: [client-cancel]}\n'
                                '  action: {type: preset, text: ' + json.dumps(wave * 4) + '}\n'
                                '  behavior: {profile: natural, thinking: {enabled: false}}\n', 1)
        config.write_text(source, encoding='utf-8', newline='\n')
        backend.cli('config', 'check')
        created = json.loads(backend.cli('keys', 'create', '--policy', 'test', '--format', 'json', '--max-concurrent', '1'))
        key = created['key']
        environment = os.environ.copy()
        environment.update(FAKEAPI_TEST_URL=backend.url, FAKEAPI_CLIENT_KEY=key,
                           FAKEAPI_TEST_ASSETS=str(config.parent / 'assets'), PYTHONIOENCODING='utf-8')
        backend.start()
        try:
            runtime.ready(backend)
            result = subprocess.run(['node', str(ROOT / 'tests/clients/cherry.mjs')],
                                    cwd=ROOT, env=environment, capture_output=True, timeout=600)
            print(result.stdout.decode('utf-8').replace(key, '<redacted>'), end='', flush=True)
            if result.returncode:
                print(result.stderr.decode('utf-8').replace(key, '<redacted>'), file=sys.stderr)
                return result.returncode
        finally:
            backend.cleanup()
            # Keep only the service's four allowed fields for failure diagnosis.
            rows = [json.loads(line) for line in backend.logs().splitlines() if line.startswith('{')]
            assert all(set(row) == {'time', 'model', 'duration_ms', 'rule'} for row in rows)
            evidence = ROOT / '.cache/cherry-evidence'
            evidence.mkdir(exist_ok=True)
            (evidence / 'server-requests.json').write_text(json.dumps(rows, indent=2) + '\n', encoding='utf-8')
        logs = backend.logs()
        assert key not in logs, 'raw Key in server log'
        rows = [json.loads(line) for line in logs.splitlines() if line.startswith('{')]
        assert rows and all(set(row) == {'time', 'model', 'duration_ms', 'rule'} for row in rows)
        print('PASS Cherry Studio server log privacy')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
