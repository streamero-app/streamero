#!/usr/bin/env python3
"""Generate the Streamero Manage app icon: the brand icon (docs/brand/icon-1024.png) with a
small "controls" badge (a ringed disc with three sliders) in the free bottom-right corner,
inside the iOS corner mask. Icon only, no text.

Pure Python (zlib + struct), so it runs anywhere without image libraries:

    python3 scripts/gen-manage-icon.py docs/brand/icon-1024.png \
        ios/StreameroManage/Resources/ManageAssets.xcassets/ManageAppIcon.appiconset/AppIcon-1024.png
"""
import math
import struct
import sys
import zlib


def read_png(path):
    with open(path, "rb") as f:
        data = f.read()
    assert data[:8] == b"\x89PNG\r\n\x1a\n", "not a PNG"
    pos, idat = 8, b""
    width = height = None
    while pos < len(data):
        length, kind = struct.unpack(">I4s", data[pos:pos + 8])
        body = data[pos + 8:pos + 8 + length]
        if kind == b"IHDR":
            width, height, depth, color, _, _, interlace = struct.unpack(">IIBBBBB", body)
            assert depth == 8 and color in (2, 6) and interlace == 0, "need 8-bit RGB(A), non-interlaced"
            bpp = 3 if color == 2 else 4
        elif kind == b"IDAT":
            idat += body
        pos += 12 + length
    raw = zlib.decompress(idat)
    stride = width * bpp
    rows, prev, i = [], bytearray(stride), 0
    for _ in range(height):
        ftype = raw[i]
        line = bytearray(raw[i + 1:i + 1 + stride])
        i += 1 + stride
        for x in range(stride):
            a = line[x - bpp] if x >= bpp else 0
            b = prev[x]
            c = prev[x - bpp] if x >= bpp else 0
            if ftype == 1:
                line[x] = (line[x] + a) & 0xFF
            elif ftype == 2:
                line[x] = (line[x] + b) & 0xFF
            elif ftype == 3:
                line[x] = (line[x] + (a + b) // 2) & 0xFF
            elif ftype == 4:
                p = a + b - c
                pa, pb, pc = abs(p - a), abs(p - b), abs(p - c)
                pred = a if pa <= pb and pa <= pc else (b if pb <= pc else c)
                line[x] = (line[x] + pred) & 0xFF
        rows.append(line)
        prev = line
    # Normalise to RGB.
    if bpp == 4:
        rows = [bytearray(v for x in range(width) for v in r[x * 4:x * 4 + 3]) for r in rows]
    return width, height, rows


def write_png(path, width, height, rows):
    raw = b"".join(b"\x00" + bytes(r) for r in rows)

    def chunk(kind, body):
        return struct.pack(">I", len(body)) + kind + body + struct.pack(">I", zlib.crc32(kind + body) & 0xFFFFFFFF)

    out = b"\x89PNG\r\n\x1a\n"
    out += chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0))
    out += chunk(b"IDAT", zlib.compress(raw, 9))
    out += chunk(b"IEND", b"")
    with open(path, "wb") as f:
        f.write(out)


def clamp(v):
    return max(0.0, min(1.0, v))


def blend(rows, x, y, color, alpha):
    if alpha <= 0:
        return
    r = rows[y]
    for k in range(3):
        r[x * 3 + k] = int(round(r[x * 3 + k] * (1 - alpha) + color[k] * alpha))


def seg_dist(px, py, ax, ay, bx, by):
    dx, dy = bx - ax, by - ay
    t = clamp(((px - ax) * dx + (py - ay) * dy) / (dx * dx + dy * dy))
    return math.hypot(px - (ax + t * dx), py - (ay + t * dy))


def main():
    src, dst = sys.argv[1], sys.argv[2]
    width, height, rows = read_png(src)
    s = width / 1024.0
    cx, cy, radius = 852 * s, 852 * s, 104 * s
    ring = 13 * s
    gap = 12 * s  # dark halo between the badge and the mark behind it
    ink = (10, 10, 10)
    # The brand mark's gold, light at the top.
    ring_top, ring_bottom = (246, 222, 166), (196, 150, 72)
    paper = (244, 239, 228)
    sliders = [(-38, -0.45), (0, 0.35), (38, -0.1)]  # (y offset, knob position -1…1)
    track_half = 50 * s
    track_w = 6 * s
    knob_r = 12 * s

    x0, x1 = int(cx - radius - gap - 2), int(cx + radius + gap + 2)
    y0, y1 = int(cy - radius - gap - 2), int(cy + radius + gap + 2)
    for y in range(max(0, y0), min(height, y1)):
        for x in range(max(0, x0), min(width, x1)):
            px, py = x + 0.5, y + 0.5
            d = math.hypot(px - cx, py - cy)
            # Halo + disc.
            blend(rows, x, y, ink, clamp(radius + gap - d + 0.5))
            # Ring with a warm vertical gradient (same light as the mark).
            t = clamp((py - (cy - radius)) / (2 * radius))
            rc = tuple(ring_top[k] * (1 - t) + ring_bottom[k] * t for k in range(3))
            inside_outer = clamp(radius - d + 0.5)
            inside_inner = clamp(radius - ring - d + 0.5)
            blend(rows, x, y, rc, inside_outer - inside_inner)
            # Three sliders.
            for off, knob in sliders:
                ly = cy + off * s
                ax, bx = cx - track_half, cx + track_half
                dist = seg_dist(px, py, ax, ly, bx, ly)
                blend(rows, x, y, paper, 0.55 * clamp(track_w / 2 - dist + 0.5))
                kx = cx + knob * track_half
                kd = math.hypot(px - kx, py - ly)
                blend(rows, x, y, ink, clamp(knob_r + 5 * s - kd + 0.5))
                blend(rows, x, y, paper, clamp(knob_r - kd + 0.5))
    write_png(dst, width, height, rows)


if __name__ == "__main__":
    main()
