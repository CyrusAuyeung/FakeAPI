"""Build portable release archives and checksums locally; does not publish."""
from pathlib import Path
import hashlib
import os
import shutil
import subprocess
import tarfile
import zipfile
import re

root = Path(__file__).resolve().parents[1]
version = os.environ.get('GITHUB_REF_NAME', '0.5.0-alpha.2').removeprefix('v')
if not re.fullmatch(r'\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?', version):
    if os.environ.get('GITHUB_REF', '').startswith('refs/tags/'):
        raise SystemExit('Release tag must use a semantic version.')
    version = '0.5.0-alpha.2'
commit = os.environ.get('GITHUB_SHA', 'unknown')
output = root / 'bin/releases'
output.mkdir(parents=True, exist_ok=True)
targets = [('windows', 'amd64'), ('linux', 'amd64'), ('linux', 'arm64'), ('darwin', 'amd64'), ('darwin', 'arm64')]
checksums = []
for system, architecture in targets:
    stage = output / f'fakeapi_{version}_{system}_{architecture}'
    stage.mkdir(exist_ok=True)
    assert stage.resolve().is_relative_to(output.resolve())
    for old_helper in [stage / 'assets/embed.go', stage / 'examples/embed.go']:
        if old_helper.is_file():
            old_helper.unlink()
    name = 'fakeapi.exe' if system == 'windows' else 'fakeapi'
    environment = dict(os.environ, GOOS=system, GOARCH=architecture, CGO_ENABLED='0')
    subprocess.run(['go', 'build', '-trimpath', '-ldflags', f'-s -w -X fakeapi/internal/cli.Version={version} -X fakeapi/internal/cli.Commit={commit}', '-o', str(stage / name), './cmd/fakeapi'], cwd=root, env=environment, check=True)
    for filename in ['LICENSE', 'README.md', 'README.en.md', 'CHANGELOG.md', 'compose.yaml']:
        shutil.copy2(root / filename, stage / filename)
    for dirname in ['assets', 'examples', 'docs', 'schemas']:
        shutil.copytree(root / dirname, stage / dirname, dirs_exist_ok=True, ignore=shutil.ignore_patterns('*.go'))
    archive = Path(str(stage) + ('.zip' if system == 'windows' else '.tar.gz'))
    if system == 'windows':
        with zipfile.ZipFile(archive, 'w', zipfile.ZIP_DEFLATED) as file:
            for item in stage.rglob('*'):
                if item.is_file():
                    file.write(item, item.relative_to(output))
    else:
        with tarfile.open(archive, 'w:gz') as file:
            file.add(stage, arcname=stage.name)
    checksums.append(f'{hashlib.sha256(archive.read_bytes()).hexdigest()}  {archive.name}')
(output / 'SHA256SUMS').write_text('\n'.join(checksums) + '\n', encoding='ascii')
print('Built release archives:', len(targets))
