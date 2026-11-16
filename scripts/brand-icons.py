"""Builds every app icon from docs/brand/icon-source.png (the owner's mockup).

Run from the repo root inside any Python image with Pillow, e.g.:
  docker run --rm -u 1000:1000 -e HOME=/tmp -v "$PWD":/w -w /w python:3.12-slim \
    sh -c 'pip install -q --user pillow && python scripts/brand-icons.py'
It writes the PNGs to docs/brand/out/; copy them to the app folders listed in docs/brand/README.md.
"""
import os
from PIL import Image, ImageDraw, ImageFilter
SRC = 'docs/brand/icon-source.png'
OUT = 'docs/brand/out'
os.makedirs(OUT, exist_ok=True)
src = Image.open(SRC).convert('RGB')
W, H = src.size
dark = lambda p: sum(p) < 150
mid_row = [src.getpixel((x, H // 2)) for x in range(W)]
mid_col = [src.getpixel((W // 2, y)) for y in range(H)]
xs = [x for x, p in enumerate(mid_row) if dark(p)]; ys = [y for y, p in enumerate(mid_col) if dark(p)]
x0, x1, y0, y1 = xs[0], xs[-1], ys[0], ys[-1]
print('tile', x0, x1, y0, y1, x1 - x0, y1 - y0)
side = min(x1 - x0, y1 - y0)
inset = 10  # skip the tile's soft rim
cx, cy = (x0 + x1) / 2, (y0 + y1) / 2
half = side / 2 - inset
tile = src.crop((round(cx - half), round(cy - half), round(cx + half), round(cy + half)))
n = tile.size[0]
# The mockup's tile has rounded corners showing the background: paint everything
# outside the rounded square with the tile's own black, so the icon is full-bleed.
radius = int(n * 0.235)
mask = Image.new('L', (n, n), 0)
ImageDraw.Draw(mask).rounded_rectangle((8, 8, n - 9, n - 9), radius=radius, fill=255)
mask = mask.filter(ImageFilter.GaussianBlur(2.5))
# The tile is a vertical gradient: fill each row with the tile's own colour at the centre
# of that row (the art never reaches the top and bottom bands, so sample the nearest band).
corner = tile.getpixel((n // 2, int(n * 0.06)))
bg = Image.new('RGB', (n, n))
for y in range(n):
    # Near the top and bottom the centre column is plain tile; elsewhere sample just inside
    # the straight side edge (the art never reaches it).
    if y < n * 0.12 or y > n * 0.88:
        c = tile.getpixel((n // 2, min(max(y, int(n * 0.04)), int(n * 0.96))))
    else:
        c = tile.getpixel((int(n * 0.06), y))
    ImageDraw.Draw(bg).line((0, y, n, y), fill=c)
full = Image.composite(tile, bg, mask)
big = full.resize((1024, 1024), Image.LANCZOS).filter(ImageFilter.UnsharpMask(radius=1.6, percent=70, threshold=2))
big.save(OUT + '/icon-1024.png')
def save(size, path, img=big):
    img.resize((size, size), Image.LANCZOS).save(path)
for d, s in {'mdpi': 48, 'hdpi': 72, 'xhdpi': 96, 'xxhdpi': 144, 'xxxhdpi': 192}.items():
    save(s, f'{OUT}/ic_launcher-{d}.png')
save(512, OUT + '/AppMark.png')
save(512, OUT + '/icon-512.png'); save(192, OUT + '/icon-192.png'); save(180, OUT + '/apple-touch-icon.png')
# Rounded version for in-app marks and the browser tab (transparent corners).
rmask = Image.new('L', (1024, 1024), 0)
ImageDraw.Draw(rmask).rounded_rectangle((0, 0, 1023, 1023), radius=230, fill=255)
rounded = big.convert('RGBA'); rounded.putalpha(rmask)
rounded.resize((256, 256), Image.LANCZOS).save(OUT + '/mark-256.png')
rounded.resize((64, 64), Image.LANCZOS).save(OUT + '/favicon-64.png')
# Android TV banner: icon only, centred on the tile black.
for name, (bw, bh) in {'banner-xhdpi': (320, 180), 'banner-xxhdpi': (480, 270)}.items():
    b = Image.new('RGB', (bw, bh), (14, 14, 14))
    s = int(bh * 0.7)
    m = rounded.resize((s, s), Image.LANCZOS)
    b.paste(m, ((bw - s) // 2, (bh - s) // 2), m)
    b.save(f'{OUT}/{name}.png')
print('corner colour', corner)
