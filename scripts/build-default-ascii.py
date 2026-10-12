"""Build text assets from the four user-selected poses (Pillow, dev-only).

Images are read locally and are not bundled with the project. Output is plain
ASCII text, not edited raster images. Usage: python ... image1 image2 image3
"""
from pathlib import Path
import colorsys
import sys
from PIL import Image, ImageDraw

root = Path(__file__).resolve().parents[1]
if len(sys.argv) != 4:
    raise SystemExit('Usage: build-default-ascii.py WAVE_AND_STAND_IMAGE LAUGH_IMAGE THINK_IMAGE')
references = [Image.open(p).convert("RGB") for p in sys.argv[1:]]
poses = [("wave", 0, (.025, .26, .625, .94)), ("stand", 0, (.615, .215, .975, .94)),
         ("laugh", 1, (0, 0, 1, 1)), ("think", 2, (0, 0, 1, 1))]
for name, index, crop in poses:
    image = references[index]
    w, h = image.size
    image = image.crop(tuple(int(v * (w if i % 2 == 0 else h)) for i, v in enumerate(crop)))
    # Yellow/brown silhouette excludes the pink poster, white borders and text.
    mask = Image.new("L", image.size)
    pixels = []
    for r, g, b in image.getdata():
        hue, sat, val = colorsys.rgb_to_hsv(r / 255, g / 255, b / 255)
        pixels.append(255 if .06 <= hue <= .20 and sat > .18 and val > .10 else 0)
    mask.putdata(pixels)
    # Keep the dominant connected silhouette; isolated poster lettering must
    # not become part of a character. The densest row crosses the body.
    row = max(range(mask.height), key=lambda y: sum(pixels[y * mask.width:(y + 1) * mask.width]))
    points = [x for x in range(mask.width) if pixels[row * mask.width + x]]
    ImageDraw.floodfill(mask, (points[len(points) // 2], row), 64, thresh=0)
    pixels = [255 if value == 64 else 0 for value in mask.getdata()]
    mask.putdata(pixels)
    exterior = mask.copy()
    for x in range(exterior.width):
        for y in (0, exterior.height - 1):
            if exterior.getpixel((x, y)) == 0:
                ImageDraw.floodfill(exterior, (x, y), 128, thresh=0)
    for y in range(exterior.height):
        for x in (0, exterior.width - 1):
            if exterior.getpixel((x, y)) == 0:
                ImageDraw.floodfill(exterior, (x, y), 128, thresh=0)
    mask.putdata([255 if a or b == 0 else 0 for a, b in zip(pixels, exterior.getdata())])
    bounds = mask.getbbox()
    if bounds is None:
        raise ValueError("No character found")
    image, mask = image.crop(bounds), mask.crop(bounds)
    width = 64
    height = max(20, round(image.height / image.width * width * .48))
    small = image.resize((width, height), Image.Resampling.LANCZOS)
    shape = mask.resize((width, height), Image.Resampling.BOX)
    ramp = ".,:;irsXA253hMHGS#9B&@"
    rows = []
    for y in range(height):
        row = []
        for x in range(width):
            r, g, b = small.getpixel((x, y))
            coverage = shape.getpixel((x, y))
            # Dark eyes and mouth enclosed by the silhouette remain readable.
            interior = x > 0 and x < width - 1 and shape.getpixel((x - 1, y)) > 90 and shape.getpixel((x + 1, y)) > 90
            if coverage < 65 and not interior:
                row.append(" ")
            else:
                gray = .2126 * r + .7152 * g + .0722 * b
                row.append(ramp[min(len(ramp) - 1, int((1 - gray / 255) * len(ramp)))])
        rows.append("".join(row).rstrip())
    output = root / "assets/nailong" / f"{name}.txt"
    output.parent.mkdir(parents=True, exist_ok=True)
    while rows and not rows[0]:
        rows.pop(0)
    while rows and not rows[-1]:
        rows.pop()
    output.write_text("\n".join(rows) + "\n", encoding="ascii", newline="\n")
