"""Exercise a deployed CLI and HTTP server; never calls an external AI service.

Use --binary for native/release builds, or --docker --image for Compose.
All deployments, Keys, containers, volumes, and reports are isolated per run.
"""
import argparse
import json
import os
from pathlib import Path
import platform
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[1]
FLAGS = subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0
HTTP = urllib.request.build_opener(urllib.request.ProxyHandler({}))
PRIVATE_VALUES = []


def execute(command, env=None, timeout=60):
    result = subprocess.run(command, cwd=ROOT, env=env, capture_output=True,
                            creationflags=FLAGS, timeout=timeout)
    if result.returncode:
        # A keys-create command's output may contain a credential. Keep it private.
        detail = result.stderr.decode('utf-8', errors='replace').strip()
        for value in PRIVATE_VALUES:
            detail = detail.replace(value, '<redacted>')
        raise RuntimeError(f'command failed ({result.returncode}): {Path(command[0]).name}: {detail[:2000]}')
    return result.stdout.decode('utf-8')


def free_port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


class Native:
    def __init__(self, directory, binary):
        self.directory = directory
        self.binary = binary
        self.state = None
        self.config = str(directory / 'deployment/fakeapi.yaml')
        self.log = directory / 'server.log'
        self.process = None
        self.output = None
        self.url = f'http://127.0.0.1:{free_port()}'
        self.env = {k: v for k, v in os.environ.items()
                    if not k.startswith('FAKEAPI_')}
        self.backup = str(directory / 'backup.sqlite')
        execute([str(binary), 'init', '--template', 'testing', '--dir',
                 str(Path(self.config).parent)], env=self.env)

    def cli(self, *args):
        state = ['--state', str(self.state)] if self.state else []
        return execute([str(self.binary), *args, '--config', self.config, *state], env=self.env)

    def start(self):
        self.output = self.log.open('ab')
        self.process = subprocess.Popen(
            [str(self.binary), 'serve', '--config', self.config,
             '--listen', self.url.removeprefix('http://'),
             *(['--state', str(self.state)] if self.state else [])],
            env=self.env, stdout=self.output, stderr=self.output, creationflags=FLAGS)

    def stop(self):
        if self.process is not None:
            self.process.terminate()
            try:
                self.process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=5)
            self.process = None
        if self.output is not None:
            self.output.close()
            self.output = None

    def logs(self):
        return self.log.read_text(encoding='utf-8')

    def cleanup(self):
        self.stop()


class Container:
    def __init__(self, directory, image):
        if os.name == 'nt':
            raise RuntimeError('container acceptance runs on a Linux host')
        self.directory = directory
        self.config = '/config/fakeapi.yaml'
        self.backup = '/data/acceptance-backup.sqlite'
        self.url = None
        self.project = 'fakeapi-acceptance-' + uuid.uuid4().hex[:12]
        self.env = {k: v for k, v in os.environ.items()
                    if not k.startswith('FAKEAPI_')}
        self.env['FAKEAPI_IMAGE'] = image
        runtime = directory / 'deployment'
        runtime.mkdir()
        execute(['docker', 'run', '--rm', '--user', f'{os.getuid()}:{os.getgid()}',
                 '--mount', f'type=bind,source={runtime},target=/config', image,
                 'init', '--template', 'testing', '--shared-config', '--dir', '/config'], env=self.env)
        override = directory / 'compose.override.yaml'
        # !override replaces the production host port instead of adding another.
        override.write_text('services:\n  fakeapi:\n    ports: !override\n'
                            '      - "127.0.0.1::8787"\n    volumes:\n'
                            '      - type: bind\n'
                            f'        source: {json.dumps(str(runtime))}\n'
                            '        target: /config\n        read_only: true\n',
                            encoding='utf-8')
        self.command = ['docker', 'compose', '--project-name', self.project,
                        '--file', str(ROOT / 'compose.yaml'), '--file', str(override)]

    def compose(self, *args):
        return execute([*self.command, *args], env=self.env, timeout=90)

    def cli(self, *args):
        return self.compose('run', '--rm', '--no-deps', '-T', 'fakeapi',
                            *args, '--config', self.config)

    def start(self):
        self.compose('up', '--detach', '--no-build', '--wait', '--wait-timeout', '45')
        self.url = 'http://' + self.compose('port', 'fakeapi', '8787').strip()
        details = json.loads(execute(['docker', 'inspect',
                                     self.compose('ps', '--quiet', 'fakeapi').strip()],
                                    env=self.env))[0]
        assert details['Config']['User'] == '65532:65532', 'container user'
        assert details['HostConfig']['ReadonlyRootfs'], 'read-only root filesystem'
        assert 'ALL' in details['HostConfig']['CapDrop'], 'dropped capabilities'
        assert any('no-new-privileges' in v for v in details['HostConfig']['SecurityOpt'])
        config_mount = next(v for v in details['Mounts'] if v['Destination'] == '/config')
        data_mount = next(v for v in details['Mounts'] if v['Destination'] == '/data')
        assert not config_mount['RW'] and data_mount['RW'], 'config and data mounts'

    def stop(self):
        self.compose('stop', '--timeout', '15')

    def logs(self):
        return self.compose('logs', '--no-color', '--no-log-prefix', 'fakeapi')

    def cleanup(self):
        assert self.project.startswith('fakeapi-acceptance-')
        self.compose('down', '--volumes', '--remove-orphans')


def request(backend, path, key=None, body=None, headers=None, status=200):
    values = dict(headers or {})
    if key:
        values['Authorization'] = 'Bearer ' + key
    data = None
    if body is not None:
        data = json.dumps(body, ensure_ascii=False).encode('utf-8')
        values['Content-Type'] = 'application/json'
    req = urllib.request.Request(backend.url + path, data=data, headers=values)
    try:
        response = HTTP.open(req, timeout=15)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        raw = response.read().decode('utf-8')
        code = None
        if response.code != status:
            try:
                code = json.loads(raw).get('error', {}).get('code')
            except (ValueError, AttributeError):
                pass
        assert response.code == status, f'{path}: expected {status}, got {response.code}, code={code}'
        return raw, response.headers


def ready(backend):
    for _ in range(100):
        try:
            request(backend, '/healthz')
            return
        except (OSError, AssertionError):
            time.sleep(.1)
    raise RuntimeError('service readiness deadline exceeded')


def check(backend, passed):
    keys = []

    def key(*options):
        value = json.loads(backend.cli('keys', 'create', '--policy', 'test',
                                      '--format', 'json', *options))
        keys.append(value['key'])
        PRIVATE_VALUES.append(value['key'])
        return value['key'], value['metadata']['id']

    def metadata(key_id, *options):
        return json.loads(backend.cli('keys', 'show', '--id', key_id, *options))

    def ok(label):
        passed.append(label)
        print('PASS', label, flush=True)

    assert backend.cli('config', 'check').strip() == 'configuration valid'
    assets = [line.split('\t') for line in backend.cli('assets', 'lint').splitlines()]
    assert {v[0] for v in assets if v[1] == 'ascii'} == {
        'nailong-wave', 'nailong-stand', 'nailong-laugh', 'nailong-think'}
    primary, primary_id = key('--total-limit', '50')
    backend.start()
    ready(backend)
    ok('CLI initialization, configuration, four ASCII assets, and service startup')

    request(backend, '/v1/models', status=401)
    catalog, _ = request(backend, '/v1/models', primary)
    assert {v['id'] for v in json.loads(catalog)['data']} == {'fakeapi-frog', 'fakeapi-test'}
    request(backend, '/v1/models/fakeapi-test', primary)
    request(backend, '/anthropic/v1/models', primary,
            headers={'anthropic-version': '2023-06-01'})
    request(backend, '/v1beta/models', headers={'x-goog-api-key': primary})
    assert metadata(primary_id)['total_used'] == 0
    ok('authentication and four-protocol model discovery without quota charges')

    if os.name == 'nt':
        powershell, powershell_id = key('--total-limit', '4')
        environment = dict(backend.env, FAKEAPI_POWERSHELL_TEST_KEY=powershell,
                           FAKEAPI_POWERSHELL_TEST_URL=backend.url)
        execute(['powershell.exe', '-NoProfile', '-NonInteractive', '-File',
                 str(ROOT / 'scripts/test-powershell.ps1')], env=environment)
        assert metadata(powershell_id)['total_used'] == 4
        ok('Windows PowerShell direct API calls and Chinese UTF-8 without a desktop client')

    chat = {'model': 'fakeapi-test', 'messages': [{'role': 'user', 'content': '只回答 OK'}]}
    responses = {'model': 'fakeapi-test', 'input': '只回答 OK'}
    anthropic = {**chat, 'max_tokens': 128}
    gemini = {'contents': [{'role': 'user', 'parts': [{'text': '只回答 OK'}]}]}
    calls = [
        ('/v1/chat/completions', chat, {}, lambda v: v['choices'][0]['message']['content'], '[DONE]'),
        ('/v1/responses', responses, {}, lambda v: v['output'][0]['content'][0]['text'], 'response.completed'),
        ('/anthropic/v1/messages', anthropic, {'anthropic-version': '2023-06-01'}, lambda v: v['content'][0]['text'], 'message_stop'),
        ('/v1beta/models/fakeapi-test:generateContent', gemini, {'x-goog-api-key': primary}, lambda v: v['candidates'][0]['content']['parts'][0]['text'], 'STOP'),
    ]
    for path, body, headers, text, end in calls:
        raw, _ = request(backend, path, primary, body, headers)
        assert text(json.loads(raw)) == 'OK', f'{path}: ordinary reply'
        if ':generateContent' in path:
            stream_path = path.replace(':generateContent', ':streamGenerateContent?alt=sse')
            stream_body = body
        else:
            stream_path, stream_body = path, {**body, 'stream': True}
        raw, stream_headers = request(backend, stream_path, primary, stream_body, headers)
        assert 'text/event-stream' in stream_headers.get('Content-Type', '')
        assert end in raw, f'{path}: successful stream terminator'
        for line in raw.splitlines():
            if line.startswith('data:'):
                payload = line[5:].strip()
                if payload != '[DONE]':
                    json.loads(payload)
        if ':generateContent' in path:
            assert '[DONE]' not in raw, 'Gemini must use its native stream ending'
    assert metadata(primary_id)['total_used'] == 8
    ok('four protocols: ordinary JSON, parseable SSE, native termination, and quota accounting')

    canary = 'acceptance-private-message-' + uuid.uuid4().hex
    latest = {**chat, 'model': 'fakeapi-frog', 'messages': [
        {'role': 'user', 'content': '1+1'}, {'role': 'assistant', 'content': '2'},
        {'role': 'user', 'content': canary}]}
    raw, _ = request(backend, '/v1/chat/completions', primary, latest)
    assert '```' in json.loads(raw)['choices'][0]['message']['content']
    raw, _ = request(backend, '/v1/chat/completions', primary,
                     {**chat, 'model': 'fakeapi-frog', 'messages': [{'role': 'user', 'content': '/laugh'}]})
    assert '```' in json.loads(raw)['choices'][0]['message']['content']
    assert metadata(primary_id)['total_used'] == 10
    ok('latest-user matching and laughing character reply')

    limited, limited_id = key('--total-limit', '2')
    for _ in range(2):
        request(backend, '/v1/chat/completions', limited, chat)
    request(backend, '/v1/chat/completions', limited, chat, status=429)
    assert metadata(limited_id)['total_used'] == 2
    expired, _ = key('--expires-at', '2000-01-01T00:00:00Z')
    request(backend, '/v1/chat/completions', expired, chat, status=401)
    revoked, revoked_id = key()
    backend.cli('keys', 'revoke', '--id', revoked_id)
    request(backend, '/v1/chat/completions', revoked, chat, status=401)
    backend.cli('keys', 'update', '--id', primary_id, '--total-limit', '10')
    request(backend, '/v1/chat/completions', primary, chat, status=429)
    ok('total quota, expired Keys, live revocation, and live policy updates')

    # Keep both RPM requests in the same fixed UTC minute.
    rate, rate_id = key('--rpm', '1')
    remaining = 60 - time.time() % 60
    if remaining < 2:
        time.sleep(remaining + .1)
    request(backend, '/v1/chat/completions', rate, chat)
    request(backend, '/v1/chat/completions', rate, chat, status=429)
    assert metadata(rate_id)['total_used'] == 1
    ok('per-Key RPM limit without charging rejected requests')

    backend.cli('db', 'backup', '--out', backend.backup)
    assert metadata(limited_id, '--state', backend.backup)['total_used'] == 2
    backend.stop()
    backend.start()
    ready(backend)
    assert metadata(limited_id)['total_used'] == 2
    request(backend, '/v1/chat/completions', limited, chat, status=429)
    request(backend, '/v1/chat/completions', revoked, chat, status=401)
    ok('consistent backup and persistent quota/revocation across service restart')

    backend.stop()
    logs = backend.logs()
    assert canary not in logs and all(value not in logs for value in keys), 'private log content'
    entries = [json.loads(line) for line in logs.splitlines() if line.startswith('{')]
    assert entries and all(set(row) == {'time', 'model', 'duration_ms', 'rule'} for row in entries)
    ok('request logs contain exactly four fields and no messages or raw Keys')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, default=ROOT / 'bin' / ('fakeapi.exe' if os.name == 'nt' else 'fakeapi'))
    parser.add_argument('--docker', action='store_true')
    parser.add_argument('--image', default='fakeapi:ci')
    parser.add_argument('--report', type=Path)
    args = parser.parse_args()
    cache = ROOT / '.cache'
    cache.mkdir(exist_ok=True)
    mode = 'docker' if args.docker else 'native'
    report = args.report or cache / 'acceptance' / f'{mode}-{platform.system().lower()}-{platform.machine().lower()}.json'
    passed = []
    started = time.monotonic()
    failure = None
    with tempfile.TemporaryDirectory(prefix='runtime-smoke-', dir=cache) as directory:
        assert Path(directory).resolve().is_relative_to(cache.resolve())
        backend = None
        try:
            backend = Container(Path(directory), args.image) if args.docker else Native(Path(directory), args.binary.resolve())
            check(backend, passed)
        except Exception as error:
            failure = f'{type(error).__name__}: {error}'
        finally:
            if backend is not None:
                try:
                    backend.cleanup()
                except Exception as error:
                    failure = failure or f'cleanup failed: {type(error).__name__}'
    report.parent.mkdir(parents=True, exist_ok=True)
    report.write_text(json.dumps({'mode': mode, 'system': platform.system(),
                                 'architecture': platform.machine(), 'passed': passed,
                                 'success': failure is None, 'failure': failure,
                                 'duration_seconds': round(time.monotonic() - started, 3)},
                                indent=2, ensure_ascii=False) + '\n', encoding='utf-8')
    if failure:
        print('FAIL', failure, file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
