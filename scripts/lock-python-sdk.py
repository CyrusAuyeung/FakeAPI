"""Lock installed development-only SDK dependencies, without touching global Python."""
from pathlib import Path
from importlib.metadata import distributions

root = Path(__file__).resolve().parents[1]
lines = sorted(f"{dist.metadata['Name']}=={dist.version}" for dist in distributions(path=[str(root / '.cache/sdk-python')]))
(root / 'tests/sdk/python/requirements.txt').write_text('\n'.join(lines) + '\n', encoding='utf-8')
