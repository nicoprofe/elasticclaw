#!/usr/bin/env python3
"""Regenerate build/macos/ElasticClaw.icns from the shared icon source.

macOS reads an app's Dock, Finder and Launchpad icon from a .icns file inside the
bundle. The usual way to make one is `iconutil`, which only exists on macOS — and
depending on it would mean the icon could only ever be built on the release runner,
where a failure surfaces as a shipped app with a blank icon. The .icns container is
simple enough (a header plus typed, length-prefixed PNG entries) that generating it
here keeps the artifact checked in, reviewable and reproducible from any machine.

Run this only when build/windows/icon-source.png changes:

    python3 scripts/make-macos-icns.py

Requires Pillow. The output is deterministic: the same source produces the same
bytes, so an accidental run shows up as no diff.
"""

import struct
import sys
from pathlib import Path

try:
    from PIL import Image
except ImportError:  # pragma: no cover - developer-facing message
    sys.exit("Pillow is required: pip install pillow")

ROOT = Path(__file__).resolve().parent.parent
SOURCE = ROOT / "build" / "windows" / "icon-source.png"
TARGET = ROOT / "build" / "macos" / "ElasticClaw.icns"

# (OSType, pixel size). These are the PNG-based types every macOS version that this
# app supports understands. The retina variants matter: without ic11/ic13 the Dock
# scales a smaller image up and the icon looks soft on every modern display.
ENTRIES = [
    (b"icp4", 16),
    (b"icp5", 32),
    (b"ic11", 32),  # 16x16@2x
    (b"icp6", 64),
    (b"ic12", 64),  # 32x32@2x
    (b"ic07", 128),
    (b"ic08", 256),
    (b"ic13", 256),  # 128x128@2x
    (b"ic09", 512),
    (b"ic14", 512),  # 256x256@2x
    (b"ic10", 1024),  # 512x512@2x
]


def png_bytes(image: Image.Image, size: int) -> bytes:
    """Return `image` resized to size x size as deterministic PNG bytes."""
    import io

    resized = image.resize((size, size), Image.LANCZOS)
    buf = io.BytesIO()
    # optimize=True is deterministic; the default PNG writer stamps no timestamp.
    resized.save(buf, format="PNG", optimize=True)
    return buf.getvalue()


def main() -> None:
    if not SOURCE.exists():
        sys.exit(f"icon source not found: {SOURCE}")

    source = Image.open(SOURCE).convert("RGBA")
    if source.width != source.height:
        sys.exit(f"icon source must be square, got {source.width}x{source.height}")

    chunks = []
    for ostype, size in ENTRIES:
        data = png_bytes(source, size)
        # Each entry: 4-byte OSType, 4-byte big-endian length covering the header
        # itself, then the payload.
        chunks.append(ostype + struct.pack(">I", len(data) + 8) + data)

    body = b"".join(chunks)
    icns = b"icns" + struct.pack(">I", len(body) + 8) + body

    TARGET.parent.mkdir(parents=True, exist_ok=True)
    TARGET.write_bytes(icns)
    print(f"wrote {TARGET.relative_to(ROOT)} ({len(icns)} bytes, {len(ENTRIES)} sizes)")


if __name__ == "__main__":
    main()
