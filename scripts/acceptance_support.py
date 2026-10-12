"""Shared, loopback-only helpers for release acceptance and measurements."""
import importlib.util
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import threading

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('runtime_smoke', ROOT / 'scripts/runtime-smoke.py')
runtime = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runtime)


class MockUpstream:
    def __init__(self, text='A' * 128):
        self.text = text
        self.calls = 0
        self.lock = threading.Lock()
        owner = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_POST(self):
                body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
                if self.path != '/v1/chat/completions' or body.get('model') != 'acceptance-mock':
                    self.send_error(400)
                    return
                with owner.lock:
                    owner.calls += 1
                usage = {'prompt_tokens': 1, 'completion_tokens': 32}
                if body.get('stream'):
                    chunks = [
                        {'choices': [{'index': 0, 'delta': {'content': owner.text}, 'finish_reason': 'stop'}]},
                        {'choices': [], 'usage': usage},
                    ]
                    data = ''.join('data: ' + json.dumps(v) + '\n\n' for v in chunks) + 'data: [DONE]\n\n'
                    content_type = 'text/event-stream'
                else:
                    data = json.dumps({'choices': [{'message': {'content': owner.text}, 'finish_reason': 'stop'}], 'usage': usage})
                    content_type = 'application/json'
                encoded = data.encode('utf-8')
                self.send_response(200)
                self.send_header('Content-Type', content_type)
                self.send_header('Content-Length', str(len(encoded)))
                self.end_headers()
                self.wfile.write(encoded)

        self.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.url = f'http://127.0.0.1:{self.server.server_port}'

    def __enter__(self):
        self.thread.start()
        return self

    def __exit__(self, *args):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=5)


def replace_once(source, old, new):
    assert source.count(old) == 1, 'acceptance template changed; update fixture explicitly'
    return source.replace(old, new, 1)


def enable_mock(backend, url):
    path = Path(backend.config)
    source = path.read_text(encoding='utf-8')
    source = replace_once(source, 'upstreams: {}', 'upstreams:\n  acceptance:\n'
                          '    kind: openai_chat\n    base_url: ' + json.dumps(url + '/v1') + '\n'
                          '    api_key_env: FAKEAPI_ACCEPTANCE_UPSTREAM_KEY\n'
                          '    max_concurrent: 64\n    reasoning_translation: omit\n'
                          '    timeouts: {connect: 2s, first_event: 5s, idle: 5s, total: 10s}')
    source = replace_once(source, 'targets: {}', 'targets:\n  acceptance:\n'
                          '    upstream: acceptance\n    model: acceptance-mock\n'
                          '    max_tokens_parameter: max_tokens\n    capabilities: {developer_role: true}')
    source = replace_once(source, 'default_target: null', 'default_target: acceptance')
    source = replace_once(source, '- id: ai-greeting\n  enabled: false', '- id: ai-greeting\n  enabled: true')
    path.write_text(source, encoding='utf-8', newline='\n')
    backend.env['FAKEAPI_ACCEPTANCE_UPSTREAM_KEY'] = 'loopback-test-only'


def chat(text='只回答 OK', stream=False):
    return {'model': 'fakeapi-test', 'messages': [{'role': 'user', 'content': text}], 'stream': stream}


def create_key(backend, *options):
    result = json.loads(backend.cli('keys', 'create', '--policy', 'test', '--format', 'json', *options))
    runtime.PRIVATE_VALUES.append(result['key'])
    return result['key'], result['metadata']['id']


def assert_private_logs(backend, keys):
    logs = backend.logs()
    assert all(key not in logs for key in keys), 'raw Key in logs'
    entries = [json.loads(line) for line in logs.splitlines() if line.startswith('{')]
    assert entries and all(set(v) == {'time', 'model', 'duration_ms', 'rule'} for v in entries)


def write_report(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, indent=2, ensure_ascii=False) + '\n', encoding='utf-8')
