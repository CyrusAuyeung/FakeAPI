"""Generate an offline, standalone preview of exactly the four bundled poses."""
from pathlib import Path
from html import escape

root = Path(__file__).resolve().parents[1]
titles = {'wave': '挥手', 'stand': '站立', 'laugh': '捧腹大笑', 'think': '思考'}
cards = []
for name, title in titles.items():
    text = (root / f'assets/nailong/{name}.txt').read_text(encoding='ascii')
    cards.append(f'<article><h2>{title}</h2><pre>{escape(text)}</pre></article>')
html = '''<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>FakeAPI · 内置 ASCII</title>
<style>body{margin:32px;background:#111827;color:#e2e8f0;font:16px system-ui}h1{font-size:24px}main{display:grid;grid-template-columns:repeat(2,max-content);gap:24px}article{background:#1e293b;padding:24px;border-radius:12px}h2{margin:0 0 20px;font-size:18px;color:#fbbf24}pre{font:12px/1.15 "Consolas","Courier New",monospace;white-space:pre;margin:0}@media(max-width:1050px){main{grid-template-columns:max-content}}</style>
<h1>FakeAPI · 四份内置 ASCII</h1><p>依据用户指定的四个姿态生成；原始图片不进入发行包。文本宽度 64 列。</p><main>''' + ''.join(cards) + '</main></html>'
(root / 'docs').mkdir(exist_ok=True)
(root / 'docs/ascii-preview.html').write_text(html, encoding='utf-8')
if '--png' in __import__('sys').argv:
    # Static text rasterization for local visual QA; no HTML execution.
    from PIL import Image, ImageDraw, ImageFont
    font = ImageFont.truetype('C:/Windows/Fonts/consola.ttf', 14)
    label = ImageFont.truetype('C:/Windows/Fonts/msyh.ttc', 22)
    canvas = Image.new('RGB', (1240, 1930), '#111827')
    draw = ImageDraw.Draw(canvas)
    for index, (name, title) in enumerate(titles.items()):
        x, y = 24 + (index % 2) * 620, 24 + (index // 2) * 950
        draw.rounded_rectangle((x, y, x + 590, y + 930), radius=14, fill='#1e293b')
        draw.text((x + 20, y + 16), title, font=label, fill='#fbbf24')
        text = (root / f'assets/nailong/{name}.txt').read_text(encoding='ascii')
        for row, line in enumerate(text.splitlines()):
            draw.text((x + 20, y + 58 + row * 14), line, font=font, fill='#e2e8f0')
    canvas.save(root / '.cache/ascii-preview.png')
