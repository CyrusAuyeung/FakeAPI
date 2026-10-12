"""Report statement coverage after merging repeated coverpkg instrumentation."""
from collections import defaultdict
from pathlib import Path
import json
import sys

profile = Path(sys.argv[1] if len(sys.argv) > 1 else 'coverage.out')
blocks = {}
for line in profile.read_text(encoding='utf-8').splitlines()[1:]:
    location, statements, count = line.split()
    previous = blocks.get(location, (int(statements), False))
    blocks[location] = (int(statements), previous[1] or int(count) > 0)
groups = defaultdict(lambda: [0, 0])
for location, (statements, covered) in blocks.items():
    filename = location.split(':')[0]
    module = str(Path(filename).parent).replace('\\', '/')
    for group in [module, 'total']:
        groups[group][0] += statements if covered else 0
        groups[group][1] += statements
    if filename.endswith('/protocol/encode.go'):
        groups['protocol/encode.go'][0] += statements if covered else 0
        groups['protocol/encode.go'][1] += statements
print(json.dumps({name: {'covered': values[0], 'statements': values[1], 'percent': round(100*values[0]/values[1], 1)} for name, values in sorted(groups.items())}, indent=2))
