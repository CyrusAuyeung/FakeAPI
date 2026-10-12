"""Measure an isolated native deployment; results are observations, not CI speed gates.

Includes FULL-synchronous SQLite accounting and four-field logs. No external AI.
Pacing is instant except the explicit cancellation probe. Capacity is raised to 128.
"""
import argparse
from concurrent.futures import ThreadPoolExecutor
import ctypes
import json
import math
import os
from pathlib import Path
import platform
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.request

from acceptance_support import ROOT, MockUpstream, assert_private_logs, chat, create_key, enable_mock, replace_once, runtime, write_report


def rss_kib(pid):
    if os.name == 'nt':
        from ctypes import wintypes

        class Counters(ctypes.Structure):
            _fields_ = [('cb', wintypes.DWORD), ('faults', wintypes.DWORD)] + [
                (name, ctypes.c_size_t) for name in ('peak', 'working', 'peak_paged', 'paged',
                                                    'peak_nonpaged', 'nonpaged', 'pagefile', 'peak_pagefile')]

        kernel, psapi = ctypes.WinDLL('kernel32', use_last_error=True), ctypes.WinDLL('psapi', use_last_error=True)
        kernel.OpenProcess.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
        kernel.OpenProcess.restype = wintypes.HANDLE
        kernel.CloseHandle.argtypes = [wintypes.HANDLE]
        psapi.GetProcessMemoryInfo.argtypes = [wintypes.HANDLE, ctypes.POINTER(Counters), wintypes.DWORD]
        handle = kernel.OpenProcess(0x0400 | 0x0010, False, pid)
        if not handle:
            raise OSError('unable to read process RSS')
        try:
            value = Counters()
            value.cb = ctypes.sizeof(value)
            if not psapi.GetProcessMemoryInfo(handle, ctypes.byref(value), value.cb):
                raise OSError('unable to read working set')
            return value.working / 1024
        finally:
            kernel.CloseHandle(handle)
    if platform.system() == 'Linux':
        for line in Path(f'/proc/{pid}/status').read_text().splitlines():
            if line.startswith('VmRSS:'):
                return int(line.split()[1])
        raise OSError('process RSS unavailable')
    return int(subprocess.check_output(['ps', '-o', 'rss=', '-p', str(pid)], timeout=3).strip())


def percentile(values, fraction):
    return sorted(values)[max(0, math.ceil(len(values) * fraction) - 1)]


def prepare(backend, upstream_url):
    path = Path(backend.config)
    source = path.read_text(encoding='utf-8')
    source = replace_once(source, 'max_concurrent: 64, max_messages:', 'max_concurrent: 128, max_messages:')
    source = replace_once(source, 'matchers:\n', 'matchers:\n'
                          '  perf-text:\n    any:\n    - {exact: /perf/128}\n'
                          '  perf-cancel:\n    any:\n    - {exact: /perf/cancel}\n')
    source = replace_once(source, 'rules:\n', 'rules:\n'
                          '- id: perf-text\n  enabled: true\n  priority: 2000\n'
                          '  when: {matchers_any: [perf-text]}\n'
                          '  action: {type: preset, text: ' + json.dumps('A' * 128) + ', strict_text: true}\n'
                          '  behavior: {profile: instant, thinking: {enabled: false}}\n'
                          '- id: perf-cancel\n  enabled: true\n  priority: 2000\n'
                          '  when: {matchers_any: [perf-cancel]}\n'
                          '  action: {type: preset, text: ' + json.dumps('A' * 128) + '}\n'
                          '  behavior: {profile: natural, thinking: {enabled: false}}\n')
    path.write_text(source, encoding='utf-8', newline='\n')
    enable_mock(backend, upstream_url)
    source = path.read_text(encoding='utf-8')
    source = replace_once(source, '    max_concurrent: 64\n', '    max_concurrent: 128\n')
    path.write_text(source, encoding='utf-8', newline='\n')


def measure(backend, samples, report):
    with MockUpstream() as upstream:
        prepare(backend, upstream.url)
        key, key_id = create_key(backend, '--max-concurrent', '128', '--tags', 'test,ai', '--ai-limit', 'unlimited')
        startup = []
        for _ in range(3):
            begin = time.perf_counter()
            backend.start()
            for _ in range(500):
                try:
                    runtime.request(backend, '/readyz')
                    break
                except (OSError, AssertionError):
                    time.sleep(.01)
            else:
                raise RuntimeError('readiness deadline exceeded')
            startup.append(round((time.perf_counter() - begin) * 1000, 3))
            if len(startup) < 3:
                backend.stop()
        report['startup_to_ready_ms'] = startup
        report['rss_idle_kib'] = round(rss_kib(backend.process.pid), 1)
        memory, sampler_errors = [], []
        stop = threading.Event()

        def sample_memory():
            while not stop.is_set():
                try:
                    memory.append(rss_kib(backend.process.pid))
                except Exception as error:
                    sampler_errors.append(type(error).__name__)
                stop.wait(.25)

        thread = threading.Thread(target=sample_memory, daemon=True)
        thread.start()
        charged, mock_calls = 0, 0
        try:
            cases = [('preset-json-128', '/perf/128', False, c) for c in (1, 4, 16, 64)] + [
                ('preset-sse-128', '/perf/128', True, 4), ('preset-sse-128', '/perf/128', True, 64),
                ('mock-json-128', 'hello', False, 16), ('mock-json-128', 'hello', False, 64),
                ('mock-sse-128', 'hello', True, 16),
                ('ascii-json', '/frog', False, 4), ('ascii-sse', '/frog', True, 4),
            ]
            report['scenarios'] = []
            for name, prompt, stream, concurrency in cases:
                body = chat(prompt, stream)

                def invoke(_):
                    started = time.perf_counter()
                    raw, _ = runtime.request(backend, '/v1/chat/completions', key, body)
                    elapsed = (time.perf_counter() - started) * 1000
                    if stream:
                        assert raw.endswith('data: [DONE]\n\n')
                        pieces = []
                        for line in raw.splitlines():
                            if line.startswith('data: ') and line != 'data: [DONE]':
                                value = json.loads(line[6:])
                                for choice in value.get('choices', []):
                                    pieces.append(choice.get('delta', {}).get('content', ''))
                        text = ''.join(pieces)
                    else:
                        text = json.loads(raw)['choices'][0]['message']['content']
                    if prompt == '/frog':
                        assert '```' in text
                    else:
                        assert text == upstream.text
                    return elapsed, len(raw.encode('utf-8'))

                for i in range(5):
                    invoke(i)
                charged += 5
                started = time.perf_counter()
                with ThreadPoolExecutor(max_workers=concurrency) as pool:
                    values = list(pool.map(invoke, range(samples)))
                elapsed = time.perf_counter() - started
                charged += samples
                if name.startswith('mock-'):
                    mock_calls += samples + 5
                latency = [v[0] for v in values]
                report['scenarios'].append({'name': name, 'concurrency': concurrency, 'requests': samples,
                                            'p50_ms': round(percentile(latency, .50), 3),
                                            'p95_ms': round(percentile(latency, .95), 3),
                                            'max_ms': round(max(latency), 3),
                                            'requests_per_second': round(samples / elapsed, 2),
                                            'mean_wire_bytes': round(sum(v[1] for v in values) / samples)})
                print(f'PASS {name} concurrency={concurrency} requests={samples}', flush=True)

            large = chat('P' * 8388608)
            started = time.perf_counter()
            runtime.request(backend, '/v1/chat/completions', key, large, status=413)
            report['oversize_rejection'] = {'request_bytes': len(json.dumps(large).encode('utf-8')),
                                            'configured_limit_bytes': 8388608,
                                            'duration_ms': round((time.perf_counter() - started) * 1000, 3)}
            metadata = json.loads(backend.cli('keys', 'show', '--id', key_id))
            assert metadata['total_used'] == charged and metadata['ai_used'] == mock_calls
            assert upstream.calls == mock_calls

            isolated, isolated_id = create_key(backend, '--max-concurrent', '1')
            req = urllib.request.Request(backend.url + '/v1/chat/completions',
                                         data=json.dumps(chat('/perf/cancel', True)).encode('utf-8'),
                                         headers={'Authorization': 'Bearer ' + isolated, 'Content-Type': 'application/json'})
            response = runtime.HTTP.open(req, timeout=15)
            try:
                while True:
                    line = response.readline()
                    assert line, 'cancel probe stream ended before its first event'
                    if line.startswith(b'data: '):
                        break
            finally:
                response.close()
            started = time.perf_counter()
            deadline = time.monotonic() + 5
            refused = 0
            while True:
                probe = urllib.request.Request(backend.url + '/v1/chat/completions',
                                               data=json.dumps(chat()).encode('utf-8'),
                                               headers={'Authorization': 'Bearer ' + isolated, 'Content-Type': 'application/json'})
                try:
                    with runtime.HTTP.open(probe, timeout=5) as recovered:
                        assert json.load(recovered)['choices'][0]['message']['content'] == 'OK'
                    break
                except urllib.error.HTTPError as error:
                    with error:
                        assert error.code == 503, 'unexpected recovery probe error'
                        error.read()
                    refused += 1
                    assert time.monotonic() < deadline, 'cancellation did not release capacity'
                    time.sleep(.005)
            report['cancel_to_recovery_ms'] = round((time.perf_counter() - started) * 1000, 3)
            report['cancel_recovery_capacity_refusals'] = refused
            assert time.monotonic() < deadline
            assert json.loads(backend.cli('keys', 'show', '--id', isolated_id))['total_used'] == 2
            assert_private_logs(backend, [key, isolated])
            memory.append(rss_kib(backend.process.pid))
        finally:
            stop.set()
            thread.join(timeout=5)
        assert memory and not sampler_errors, 'RSS sampler failed'
        report['rss_max_observed_kib'] = round(max(memory), 1)
        report['rss_sample_interval_ms'] = 250
        report['charged_requests'] = charged
        report['mock_calls'] = mock_calls


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, default=ROOT / 'bin' / ('fakeapi.exe' if os.name == 'nt' else 'fakeapi'))
    parser.add_argument('--samples', type=int, default=128)
    parser.add_argument('--report', type=Path)
    args = parser.parse_args()
    assert 64 <= args.samples <= 4096, 'sample count must be between 64 and 4096'
    binary = args.binary.resolve()
    report = {'system': platform.system(), 'architecture': platform.machine(), 'logical_cpus': os.cpu_count(),
              'python': platform.python_version(), 'version': runtime.execute([str(binary), 'version']).strip(),
              'binary_bytes': binary.stat().st_size, 'scope': 'native loopback, instant, SQLite FULL, logs enabled, capacity 128; not a speed gate'}
    cache = ROOT / '.cache'
    cache.mkdir(exist_ok=True)
    started = time.monotonic()
    failure = None
    with tempfile.TemporaryDirectory(prefix='performance-', dir=cache) as directory:
        assert Path(directory).resolve().is_relative_to(cache.resolve())
        backend = runtime.Native(Path(directory), binary)
        try:
            measure(backend, args.samples, report)
        except Exception as error:
            failure = f'{type(error).__name__}: {error}'
        finally:
            backend.cleanup()
    report.update(success=failure is None, failure=failure, duration_seconds=round(time.monotonic() - started, 3))
    path = args.report or cache / 'acceptance' / f'performance-{platform.system().lower()}-{platform.machine().lower()}.json'
    write_report(path, report)
    if failure:
        print('FAIL', failure)
        return 1
    print('PASS performance observations, durable accounting, rejection and cancellation recovery')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
