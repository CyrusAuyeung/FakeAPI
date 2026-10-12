"""Verify five archives and execute the extracted archive for the current host."""
from pathlib import Path
from contextlib import contextmanager
import os
import platform
import struct
import subprocess
import tarfile
import tempfile
import sys
import time
import zipfile
import hashlib

root = Path(__file__).resolve().parents[1]
release = root / 'bin/releases'
checksums = (release / 'SHA256SUMS').read_text(encoding='ascii').splitlines()
assert len(checksums) == 5
for row in checksums:
    checksum, filename = row.split('  ', 1)
    assert Path(filename).name == filename
    archive = release / filename
    assert hashlib.sha256(archive.read_bytes()).hexdigest() == checksum
    if filename.endswith('.zip'):
        with zipfile.ZipFile(archive) as zipped:
            files = zipped.namelist()
            binary = zipped.read(next(item for item in files if item.endswith('/fakeapi.exe')))
        assert binary[:2] == b'MZ'
        header = struct.unpack_from('<I', binary, 0x3C)[0]
        assert struct.unpack_from('<H', binary, header + 4)[0] == 0x8664
    else:
        with tarfile.open(archive) as packed:
            files = packed.getnames()
            binary = packed.extractfile(next(item for item in files if item.endswith('/fakeapi'))).read()
        if '_linux_' in filename:
            assert binary[:4] == b'\x7fELF'
            assert struct.unpack_from('<H', binary, 18)[0] == (183 if 'arm64' in filename else 62)
        else:
            assert binary[:4] == b'\xcf\xfa\xed\xfe'
            assert struct.unpack_from('<I', binary, 4)[0] == (0x100000C if 'arm64' in filename else 0x1000007)
    for required in ['README.md', 'LICENSE', 'docs/compatibility.md', 'docs/THIRD_PARTY_NOTICES.md', 'schemas/config.v1.schema.json']:
        assert any(item.endswith('/' + required) for item in files), (filename, required)
    assert not any(item.endswith('.go') for item in files), 'runtime archive contains build-only Go sources'
print('PASS five archive checksums, architecture headers, and required documentation')

system = {'Windows': 'windows', 'Linux': 'linux', 'Darwin': 'darwin'}[platform.system()]
architecture = {'AMD64': 'amd64', 'x86_64': 'amd64', 'arm64': 'arm64', 'aarch64': 'arm64'}[platform.machine()]
cache = (root / '.cache').resolve()
cache.mkdir(exist_ok=True)
flags = subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0


@contextmanager
def extraction_directory():
    directory = tempfile.TemporaryDirectory(prefix='release-smoke-', dir=cache)
    assert Path(directory.name).resolve().is_relative_to(cache)
    try:
        yield directory.name
    finally:
        # Windows antivirus may briefly hold an executable after its process exits.
        for attempt in range(10):
            try:
                directory.cleanup()
                break
            except PermissionError:
                if os.name != 'nt' or attempt == 9:
                    raise
                time.sleep(.2)


with extraction_directory() as directory:
    temporary = Path(directory).resolve()
    assert temporary.is_relative_to(cache)
    archive = release / next(row.split('  ', 1)[1] for row in checksums
                             if f'_{system}_{architecture}.' in row)
    if archive.suffix == '.zip':
        with zipfile.ZipFile(archive) as zipped:
            for member in zipped.namelist():
                assert (temporary / member).resolve().is_relative_to(temporary)
            zipped.extractall(temporary)
    else:
        with tarfile.open(archive) as packed:
            for member in packed.getmembers():
                assert (temporary / member.name).resolve().is_relative_to(temporary)
                assert not member.issym() and not member.islnk()
            packed.extractall(temporary, filter='data')
    executable = next(temporary.glob('*/' + ('fakeapi.exe' if os.name == 'nt' else 'fakeapi')))
    version = subprocess.run([str(executable), 'version'], capture_output=True,
                             creationflags=flags, check=True).stdout
    assert archive.name.split('_')[1].encode('ascii') in version
    result = subprocess.run([sys.executable, str(root / 'scripts/runtime-smoke.py'),
                             '--binary', str(executable), '--report',
                             str(cache / 'acceptance' / f'release-{system}-{architecture}.json')],
                            capture_output=True, creationflags=flags, timeout=120)
    print(result.stdout.decode('utf-8').replace('\r\n', '\n'), end='', flush=True)
    if result.returncode:
        print(result.stderr.decode('utf-8'), file=sys.stderr)
        raise SystemExit(result.returncode)
print(f'PASS extracted {system}/{architecture} release runtime acceptance')
