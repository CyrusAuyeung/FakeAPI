"""Create an isolated local deployment, then run six genuine SDK contracts."""
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request

root = Path(__file__).resolve().parents[1]
binary = root / 'bin' / ('fakeapi.exe' if os.name == 'nt' else 'fakeapi')
flags = subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0

def main():
    (root / '.cache').mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='sdk-', dir=root / '.cache') as directory:
        runtime = Path(directory)
        subprocess.run([str(binary), 'init', '--template', 'testing', '--dir', str(runtime)], check=True, capture_output=True, creationflags=flags)
        config = runtime / 'fakeapi.yaml'
        source = config.read_text(encoding='utf-8')
        start, end = source.index('  natural:\n'), source.index('  playful:\n')
        source = source[:start] + '''  natural:
    thinking: {enabled: false}
    timing: {first_output_delay: 0ms, thinking_to_body_delay: 0ms, graphemes_per_second: 10, chunk_size: 2,
      jitter: 0ms, pause_probability: 0, pause_duration: 0ms, max_total: 20s}
''' + source[end:]
        start, end = source.index('- id: fakeapi-frog\n'), source.index('- id: fakeapi-test\n')
        source = source[:start] + source[start:end].replace('profile: instant', 'profile: natural') + source[end:]
        config.write_text(source, encoding='utf-8', newline='\n')
        created = subprocess.run([str(binary), 'keys', 'create', '--config', str(config), '--policy', 'test', '--format', 'json'], check=True, capture_output=True, creationflags=flags)
        key = json.loads(created.stdout)['key']
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            port = sock.getsockname()[1]
        url = f'http://127.0.0.1:{port}'
        env = os.environ.copy()
        env['FAKEAPI_CLIENT_KEY'] = key
        env['FAKEAPI_TEST_URL'] = url
        env['PYTHONIOENCODING'] = 'utf-8'
        logs = runtime / 'server.log'
        with logs.open('wb') as output:
            process = subprocess.Popen([str(binary), 'serve', '--config', str(config), '--listen', f'127.0.0.1:{port}'], stdout=output, stderr=output, creationflags=flags)
            try:
                for _ in range(100):
                    try:
                        with urllib.request.urlopen(url + '/healthz', timeout=.5) as response:
                            if response.status == 200:
                                break
                    except OSError:
                        time.sleep(.05)
                else:
                    raise RuntimeError('local service did not start')
                for command in ([sys.executable, str(root / 'tests/sdk/python/smoke.py')], ['node', str(root / 'tests/sdk/node/smoke.mjs')]):
                    result = subprocess.run(command, env=env, creationflags=flags, capture_output=True, text=True, encoding='utf-8')
                    print(result.stdout.replace(key, '<redacted>'), end='')
                    if result.returncode:
                        print(result.stderr.replace(key, '<redacted>'), file=sys.stderr)
                        raise RuntimeError('SDK contract test failed')
            finally:
                process.terminate()
                process.wait(timeout=10)
        if key.encode() in logs.read_bytes():
            raise RuntimeError('SDK key leaked into server logs')
        print('PASS SDK server log privacy')

if __name__ == '__main__':
    main()
