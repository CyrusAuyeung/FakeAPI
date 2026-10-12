"""Collect the pinned runtime dependencies' distributed license texts."""
from pathlib import Path
import json
import re
import subprocess

root = Path(__file__).resolve().parents[1]
result = subprocess.run(['go', 'list', '-m', '-json', 'all'], cwd=root, check=True, capture_output=True, text=True, encoding='utf-8').stdout
decoder = json.JSONDecoder()
modules = []
while result.strip():
    result = result.lstrip()
    item, length = decoder.raw_decode(result)
    modules.append(item)
    result = result[length:]
destination = root / 'docs/licenses'
destination.mkdir(parents=True, exist_ok=True)
rows = []
for module in modules:
    if module.get('Main'):
        continue
    if not module.get('Dir'):
        downloaded = subprocess.run(['go', 'mod', 'download', '-json', module['Path'] + '@' + module['Version']], cwd=root, check=True, capture_output=True, text=True, encoding='utf-8')
        module['Dir'] = json.loads(downloaded.stdout)['Dir']
    folder = Path(module['Dir'])
    licenses = [file for file in folder.iterdir() if file.is_file() and re.match(r'^(LICENSE|COPYING|COPYRIGHT|NOTICE)(?:$|[. -])', file.name, re.I)]
    if not licenses:
        raise RuntimeError('Missing license text for ' + module['Path'])
    for file in licenses:
        name = re.sub(r'[^A-Za-z0-9._-]', '_', module['Path']) + '-' + module['Version'] + '-' + file.name
        (destination / name).write_bytes(file.read_bytes())
    rows.append(f"| `{module['Path']}` | `{module['Version']}` | [License files](licenses/) |")
goroot = Path(subprocess.run(['go', 'env', 'GOROOT'], check=True, capture_output=True, text=True).stdout.strip())
(destination / 'Go-LICENSE').write_bytes((goroot / 'LICENSE').read_bytes())
content = '# Third-party notices\n\nRuntime dependencies are locked by `go.mod/go.sum`. Full distributed license/notice texts are retained under `docs/licenses/`. The Go toolchain and standard library BSD license is included as `Go-LICENSE`.\n\n| Module | Version | Notices |\n|---|---|---|\n' + '\n'.join(rows)
content += '\n\nOfficial provider SDKs are development-only test dependencies, not bundled with the binary or container. Python versions are locked in `tests/sdk/python/requirements.txt`; Node versions and dependencies are locked in `tests/sdk/node/package-lock.json`. OpenAI/Google SDK projects use Apache-2.0; Anthropic SDK projects use MIT.\n\nCharacter-derived ASCII material has separate provenance; see [ASSET_NOTICES](ASSET_NOTICES.md).\n'
(root / 'docs/THIRD_PARTY_NOTICES.md').write_text(content, encoding='utf-8')
print('Collected runtime notices:', len(rows))
