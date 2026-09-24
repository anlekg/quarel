#!/usr/bin/env bash
# Builds the .ico files (Windows program and tray icons) from the SVG sources.
# Needs rsvg-convert (librsvg2-bin) and Python 3.
set -euo pipefail
cd "$(dirname "$0")"
for name in server identity; do
  tmp=$(mktemp -d)
  for s in 16 20 24 32 40 48 64 256; do rsvg-convert -w $s -h $s quarel-$name.svg -o $tmp/$s.png; done
  python3 - "$tmp" quarel-$name.ico <<'PY'
import struct, sys, os
tmp, out = sys.argv[1], sys.argv[2]
sizes = [16, 20, 24, 32, 40, 48, 64, 256]
pngs = [open(os.path.join(tmp, f"{s}.png"), "rb").read() for s in sizes]
head = struct.pack("<HHH", 0, 1, len(sizes))
offset = 6 + 16 * len(sizes)
entries, data = b"", b""
for s, png in zip(sizes, pngs):
    entries += struct.pack("<BBBBHHII", s % 256, s % 256, 0, 0, 1, 32, len(png), offset + len(data))
    data += png
open(out, "wb").write(head + entries + data)
PY
  rm -rf "$tmp"
done
cp quarel-server.ico ../../cmd/quarel-server/quarel.ico
cp quarel-identity.ico ../../cmd/quarel-identity/quarel.ico
