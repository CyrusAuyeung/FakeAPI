"""Extract the two approved YAML examples; the plan remains unchanged."""
from pathlib import Path
import re

root = Path(__file__).resolve().parents[1]
source = (root / "FakeAPI_Development_Plan.md").read_text(encoding="utf-8")
for appendix, name in [("A", "preset"), ("B", "hybrid")]:
    section = source.split(f"## 附录 {appendix}：", 1)[1]
    content = re.search(r"```yaml\n(.*?)\n```", section, re.S).group(1) + "\n"
    for old, new in {"frog-idle": "nailong-stand", "frog-hello": "nailong-wave",
                     "frog-thinking": "nailong-think", "frog-milk": "nailong-laugh",
                     "frog-shrug": "nailong-think", "frog-party": "nailong-wave"}.items():
        content = content.replace(old, new)
    # The user-selected 64-column portraits need a pace that fits the budget.
    content = content.replace('graphemes_per_second: 60, chunk_size: 4', 'graphemes_per_second: 180, chunk_size: 8')
    content = content.replace('graphemes_per_second: 24, chunk_size: 4', 'graphemes_per_second: 120, chunk_size: 8')
    content = content.replace('pause_probability: 0.1, pause_duration: 800ms', 'pause_probability: 0.02, pause_duration: 300ms')
    # Follow character references instead of literal milk/frog associations.
    for old, new in {
        'milk-command': 'laugh-command', 'croak-easter': 'laugh-easter',
        'afterwork-easter': 'thinking-easter', 'party-command': 'wave-command',
        'easter-milk': 'easter-laugh-command', 'easter-croak': 'easter-laugh',
        'easter-afterwork': 'easter-thinking', 'easter-party': 'easter-wave',
        'egg-milk': 'line-laugh', 'egg-croak': 'line-belly-laugh',
        'egg-afterwork': 'line-thinking', 'egg-party': 'line-break-laugh',
        '/milk': '/laugh', '/party': '/wave', 'keyword: 呱呱': 'keyword: 哈哈哈',
        'keyword: 下班': 'keyword: 思考',
        '你好，我是奶蛙，呱。': '你好！有什么可以帮你？',
        '你好，奶蛙已就位。': '你好！今天想聊些什么？',
    }.items():
        content = content.replace(old, new)
    destination = root / "examples" / name / "fakeapi.yaml"
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text(content, encoding="utf-8", newline="\n")
content = (root / "examples/preset/fakeapi.yaml").read_text(encoding="utf-8")
content = content.replace("profile: natural", "profile: instant").replace("profile: playful", "profile: instant")
content = content.replace("mode: random, algorithm: pcg_v1, seed: null, allow_request_seed: false", "mode: deterministic, algorithm: pcg_v1, seed: 42, allow_request_seed: true")
destination = root / "examples/testing/fakeapi.yaml"
destination.parent.mkdir(parents=True, exist_ok=True)
destination.write_text(content, encoding="utf-8", newline="\n")
