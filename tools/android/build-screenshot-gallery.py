#!/usr/bin/env python3
"""Assembles every Paparazzi golden PNG from apps/goatos-android/app/src/test/snapshots/images
into one self-contained static HTML gallery page (item 7), for CI publishing (GitHub Pages /
job summary). Images are embedded as base64 data URIs so the page has zero external references
and can be opened standalone or hosted as-is.

Usage:
    python3 tools/android/build-screenshot-gallery.py [output_dir]

Default output_dir: apps/goatos-android/docs/screenshot-gallery
Writes: <output_dir>/index.html
"""
from __future__ import annotations

import base64
import re
import sys
from datetime import datetime, timezone
from pathlib import Path

SNAPSHOTS_DIR = Path("apps/goatos-android/app/src/test/snapshots/images")

# ScreenshotTest method name -> (display title, one-line description) for a readable gallery.
SCREEN_META: dict[str, tuple[str, str]] = {
    "login": ("Login", "Work email + password sign-in, language picker"),
    "calendar": ("Calendar", "Universal landing — week/month/history"),
    "sheds": ("Sheds", "Today's sheds / drive status"),
    "scan": ("Scan", "Tap-to-vaccinate execution loop"),
    "submit": ("Submit", "Shed record submission + sync banner"),
    "overview": ("Overview", "Leadership coverage, backlog, decisions"),
    "record": ("Record", "Read-only shed/drive record"),
    "rfid": ("RFID reader", "Reader pairing + test read"),
    "alerts": ("Alerts", "Notification feed with delivery-channel chips"),
    "you": ("You / Settings", "Language, RFID, notifications, sign out"),
    "overdue": ("Overdue", "Missed vs in-buffer rescheduling list"),
    "reschedule": ("Reschedule", "New-date + assign-to form"),
}

# Matches Paparazzi's default snapshot filename: <package>_<ClassName>_<testMethod>_<snapshotName>.png
FILENAME_RE = re.compile(r"^.+_ScreenshotTest_([a-zA-Z0-9]+)_[a-zA-Z0-9]+\.png$")

HTML_TEMPLATE = """<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Goat OS Android — screenshot gallery</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
  :root {{ color-scheme: dark light; }}
  body {{
    margin: 0; padding: 24px; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    background: #0a0f0c; color: #e7ede9;
  }}
  h1 {{ font-size: 20px; margin: 0 0 4px; }}
  p.meta {{ color: #8aa196; font-size: 13px; margin: 0 0 22px; }}
  .grid {{
    display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); gap: 18px;
  }}
  .card {{
    background: #0d1310; border: 1px solid #232b26; border-radius: 14px; overflow: hidden;
    display: flex; flex-direction: column;
  }}
  .card img {{ width: 100%; display: block; background: #000; }}
  .card .body {{ padding: 12px 14px 14px; }}
  .card h3 {{ margin: 0 0 3px; font-size: 14px; }}
  .card p {{ margin: 0; font-size: 12px; color: #8aa196; }}
</style>
</head>
<body>
  <h1>Goat OS Android — screenshot gallery</h1>
  <p class="meta">Generated {generated_at} from Paparazzi golden images
  (app/src/test/snapshots/images), one per ScreenshotTest method. Regenerate with
  <code>./gradlew :app:recordPaparazziDevDebug</code> then
  <code>python3 tools/android/build-screenshot-gallery.py</code>. {count} screens.</p>
  <div class="grid">
{cards}
  </div>
</body>
</html>
"""

CARD_TEMPLATE = """    <div class="card">
      <img src="data:image/png;base64,{data}" alt="{title}">
      <div class="body"><h3>{title}</h3><p>{description}</p></div>
    </div>"""


def main() -> None:
    out_dir = Path(sys.argv[1]) if len(sys.argv) > 1 else Path("apps/goatos-android/docs/screenshot-gallery")
    out_dir.mkdir(parents=True, exist_ok=True)

    if not SNAPSHOTS_DIR.is_dir():
        print(f"No snapshots found at {SNAPSHOTS_DIR} — run ./gradlew :app:recordPaparazziDevDebug first.")
        sys.exit(1)

    pngs = sorted(SNAPSHOTS_DIR.glob("*.png"))
    if not pngs:
        print(f"No PNGs in {SNAPSHOTS_DIR}.")
        sys.exit(1)

    cards = []
    seen = set()
    order = list(SCREEN_META.keys())

    def sort_key(png: Path) -> tuple[int, str]:
        m = FILENAME_RE.match(png.name)
        key = m.group(1) if m else png.stem
        return (order.index(key) if key in order else len(order), png.name)

    for png in sorted(pngs, key=sort_key):
        m = FILENAME_RE.match(png.name)
        key = m.group(1) if m else png.stem
        if key in seen:
            continue
        seen.add(key)
        title, description = SCREEN_META.get(key, (key, ""))
        data = base64.b64encode(png.read_bytes()).decode("ascii")
        cards.append(CARD_TEMPLATE.format(data=data, title=title, description=description))

    html = HTML_TEMPLATE.format(
        generated_at=datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M UTC"),
        count=len(cards),
        cards="\n".join(cards),
    )
    (out_dir / "index.html").write_text(html, encoding="utf-8")
    print(f"Wrote {out_dir / 'index.html'} ({len(cards)} screens)")


if __name__ == "__main__":
    main()
