"""Test real previous/candidate binaries, same-schema downgrade, and backup recovery.

Only temporary deployments are used. No real AI, existing data, or Keys are touched.
"""
import argparse
from contextlib import closing
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import sqlite3
import subprocess
import tempfile
import time

from acceptance_support import ROOT, MockUpstream, assert_private_logs, chat, create_key, enable_mock, runtime, write_report


def fingerprint(path):
    # Logical content excludes journal headers; opening SQLite can change those.
    with closing(sqlite3.connect(path)) as db:
        value = list(db.iterdump())
        assert db.execute('PRAGMA integrity_check').fetchone() == ('ok',)
    return hashlib.sha256('\n'.join(value).encode('utf-8')).hexdigest()


def check(backend, candidate, passed):
    def ok(label):
        passed.append(label)
        print('PASS', label, flush=True)

    def metadata(key_id):
        return json.loads(backend.cli('keys', 'show', '--id', key_id))

    previous = backend.binary
    with MockUpstream() as upstream:
        enable_mock(backend, upstream.url)
        active, active_id = create_key(backend, '--tags', 'test,ai', '--ai-limit', '4',
                                      '--total-limit', '9', '--expires-in', '30d', '--label', 'upgrade')
        revoked, revoked_id = create_key(backend)
        rate, rate_id = create_key(backend, '--rpm', '1')
        backend.start()
        runtime.ready(backend)
        raw, _ = runtime.request(backend, '/v1/chat/completions', active, chat('hello'))
        assert json.loads(raw)['choices'][0]['message']['content'] == upstream.text
        runtime.request(backend, '/v1/chat/completions', active, chat())
        backend.cli('keys', 'revoke', '--id', revoked_id)
        before = metadata(active_id)
        assert before['total_used'] == 2 and before['ai_used'] == 1
        ok('previous binary creates Keys and commits preset/AI usage and revocation')

        backend.cli('db', 'backup', '--out', backend.backup)
        backend.stop()
        baseline = fingerprint(Path(backend.config).parent / 'data/state.sqlite')
        backend.binary = candidate
        assert backend.cli('config', 'check').strip() == 'configuration valid'
        assert 'no pending migrations' in backend.cli('db', 'migrate')
        assert metadata(active_id) == before
        assert metadata(revoked_id)['revoked_at'] is not None
        assert fingerprint(Path(backend.config).parent / 'data/state.sqlite') == baseline
        backend.start()
        runtime.ready(backend)
        runtime.request(backend, '/v1/models', active)
        runtime.request(backend, '/v1/chat/completions', revoked, chat(), status=401)
        runtime.request(backend, '/v1/chat/completions', active, chat('hello'))
        runtime.request(backend, '/v1/chat/completions', active, chat(stream=True))
        assert metadata(active_id)['total_used'] == 4 and metadata(active_id)['ai_used'] == 2
        ok('candidate preserves policies, expiry, Key hash, usage and revocation; serves JSON/SSE')

        # Verify the stored window independently of slow-runner UTC minute changes.
        runtime.request(backend, '/v1/chat/completions', rate, chat())
        state = Path(backend.config).parent / 'data/state.sqlite'
        with closing(sqlite3.connect(state)) as db:
            window = db.execute('SELECT minute_start_ms,accepted_count FROM rate_windows WHERE key_id=?', (rate_id,)).fetchone()
        assert window is not None and window[1] == 1
        after = metadata(active_id)
        backend.stop()
        backend.binary = previous
        backend.start()
        runtime.ready(backend)
        assert metadata(active_id) == after
        with closing(sqlite3.connect(state)) as db:
            assert db.execute('SELECT minute_start_ms,accepted_count FROM rate_windows WHERE key_id=?', (rate_id,)).fetchone() == window
        remaining = 60 - time.time() % 60
        if remaining < 1:
            time.sleep(remaining + .1)
        rate_status = 429 if window[0] == int(time.time() // 60) * 60000 else 200
        runtime.request(backend, '/v1/chat/completions', rate, chat(), status=rate_status)
        runtime.request(backend, '/v1/chat/completions', revoked, chat(), status=401)
        runtime.request(backend, '/v1/chat/completions', active, chat())
        assert metadata(rate_id)['total_used'] == (1 if rate_status == 429 else 2)
        assert metadata(active_id)['total_used'] == 5 and metadata(active_id)['ai_used'] == 2
        backend.stop()
        ok('same-schema downgrade preserves new usage, revocation and RPM window')

        # Recover into another path; never overwrite a live database or its WAL.
        recovered = backend.directory / 'recovered.sqlite'
        shutil.copy2(backend.backup, recovered)
        backend.state = recovered
        backend.binary = previous
        assert metadata(active_id) == before
        assert metadata(revoked_id)['revoked_at'] is not None
        backend.start()
        runtime.ready(backend)
        runtime.request(backend, '/v1/chat/completions', revoked, chat(), status=401)
        runtime.request(backend, '/v1/chat/completions', active, chat())
        assert metadata(active_id)['total_used'] == 3 and metadata(active_id)['ai_used'] == 1
        backend.stop()
        ok('previous binary starts from a consistent pre-upgrade backup at its snapshot counters')

        future = backend.directory / 'future-schema.sqlite'
        shutil.copy2(backend.backup, future)
        with closing(sqlite3.connect(future)) as db, db:
            db.execute('INSERT INTO schema_migrations VALUES(2,1)')
        unchanged = fingerprint(future)
        for binary in (previous, candidate):
            result = subprocess.run([str(binary), 'db', 'migrate', '--config', backend.config,
                                     '--state', str(future)], env=backend.env, capture_output=True,
                                    creationflags=runtime.FLAGS, timeout=30)
            assert result.returncode != 0 and b'E_DB_SCHEMA' in result.stderr
            assert fingerprint(future) == unchanged, 'future schema changed during rejection'
        ok('both binaries reject a future schema without changing its logical contents')
        assert upstream.calls == 2, 'unexpected upstream call during recovery'
        assert_private_logs(backend, [active, revoked, rate])
        ok('upgrade/recovery logs retain exactly four public fields')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--previous-binary', type=Path, default=ROOT / '.cache/previous' / ('fakeapi.exe' if os.name == 'nt' else 'fakeapi'))
    parser.add_argument('--binary', type=Path, default=ROOT / 'bin' / ('fakeapi.exe' if os.name == 'nt' else 'fakeapi'))
    parser.add_argument('--report', type=Path)
    args = parser.parse_args()
    previous, candidate = args.previous_binary.resolve(), args.binary.resolve()
    assert previous != candidate and previous.is_file() and candidate.is_file()
    versions = {label: runtime.execute([str(binary), 'version']).strip()
                for label, binary in [('previous', previous), ('candidate', candidate)]}
    cache = ROOT / '.cache'
    cache.mkdir(exist_ok=True)
    passed, failure = [], None
    started = time.monotonic()
    with tempfile.TemporaryDirectory(prefix='upgrade-smoke-', dir=cache) as directory:
        assert Path(directory).resolve().is_relative_to(cache.resolve())
        backend = runtime.Native(Path(directory), previous)
        try:
            check(backend, candidate, passed)
        except Exception as error:
            failure = f'{type(error).__name__}: {error}'
        finally:
            backend.cleanup()
    report = args.report or cache / 'acceptance' / f'upgrade-{platform.system().lower()}-{platform.machine().lower()}.json'
    write_report(report, {'system': platform.system(), 'architecture': platform.machine(),
                         'versions': versions, 'schema': 1, 'passed': passed, 'success': failure is None,
                         'failure': failure, 'duration_seconds': round(time.monotonic() - started, 3)})
    if failure:
        print('FAIL', failure)
        return 1
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
