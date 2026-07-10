#!/usr/bin/env python3
"""Assembles every Paparazzi golden PNG from apps/goatos-android/app/src/test/snapshots/images
into one self-contained static HTML gallery page (item 7), for CI publishing (GitHub Pages /
job summary). Images are embedded as base64 data URIs so the page has zero external references
and can be opened standalone or hosted as-is.

Two sections are rendered:
  - "Screens"              — one image per ScreenshotTest method (every screen, once).
  - "Role chrome coverage" — one image per RoleChromeScreenshotTest method (every role tier's
    nav chrome + landing screen, once — CEO/Director/Park Head/Park Manager/Operator).

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

# RoleChromeScreenshotTest method name -> (display title, one-line description).
ROLE_META: dict[str, tuple[str, str]] = {
    "role_ceo": ("CEO / superuser", "EXPANDED chrome, all 4 modules, landing Overview"),
    "role_ceo_drawer": ("CEO / superuser · drawer open", "Module switcher (EXPANDED-only chrome)"),
    "role_director": ("Director", "EXPANDED chrome, >=2 modules, landing Overview"),
    "role_park_head": ("Park Head", "EXPANDED chrome, >=2 modules for the park, landing Overview"),
    "role_park_manager": ("Park Manager", "MINIMAL bottom-bar, single vaccination module, landing Calendar"),
    "role_operator": ("Operator", "MINIMAL bottom-bar, single vaccination module, landing Calendar"),
}

# Matches Paparazzi's default snapshot filename:
#   <package>_<ClassName>_<testMethod>_<snapshotName>.png
# Our shot(name) helper always calls the test method and the snapshot the same string, so the
# suffix after "_<ClassName>_" is "<key>_<key>" — captured whole here (group 2 may itself
# contain underscores, e.g. "role_ceo_drawer") and split via _split_doubled_key below rather
# than parsed by a second regex group, which would be ambiguous for underscore-containing keys.
FILENAME_RE = re.compile(r"^.+_(ScreenshotTest|RoleChromeScreenshotTest)_(.+)\.png$")


def _split_doubled_key(mid: str) -> str:
    """mid is "<key>_<key>" (method name + snapshot name, always identical by convention).
    Recovers <key> by position rather than by a second regex group, so keys containing
    underscores (e.g. "role_ceo_drawer") split correctly."""
    n = len(mid)
    half = (n - 1) // 2
    if n % 2 == 1 and mid[half] == "_" and mid[:half] == mid[half + 1 :]:
        return mid[:half]
    return mid  # fallback: unrecognized shape, show the raw string rather than guessing


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
  h2 {{ font-size: 16px; margin: 28px 0 10px; color: #cddbd3; }}
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
  (app/src/test/snapshots/images). Regenerate with
  <code>./gradlew :app:recordPaparazziDevDebug</code> then
  <code>python3 tools/android/build-screenshot-gallery.py</code>.
  {screen_count} screens, {role_count} role-chrome variants.</p>
  <h2>Screens</h2>
  <div class="grid">
{screen_cards}
  </div>
  <h2>Role chrome coverage</h2>
  <p class="meta">Nav chrome (drawer/module-switcher vs bottom-bar-only) + landing screen, one
  per role tier — see RoleChromeScreenshotTest.kt.</p>
  <div class="grid">
{role_cards}
  </div>
</body>
</html>
"""

CARD_TEMPLATE = """    <div class="card">
      <img src="data:image/png;base64,{data}" alt="{title}">
      <div class="body"><h3>{title}</h3><p>{description}</p></div>
    </div>"""


def _collect(pngs: list[Path], class_name: str, meta: dict[str, tuple[str, str]]) -> list[str]:
    order = list(meta.keys())

    def sort_key(png: Path) -> tuple[int, str]:
        m = FILENAME_RE.match(png.name)
        key = _split_doubled_key(m.group(2)) if m else png.stem
        return (order.index(key) if key in order else len(order), png.name)

    cards = []
    seen = set()
    matching = [
        png for png in pngs if (m := FILENAME_RE.match(png.name)) is not None and m.group(1) == class_name
    ]
    for png in sorted(matching, key=sort_key):
        m = FILENAME_RE.match(png.name)
        key = _split_doubled_key(m.group(2)) if m else png.stem
        if key in seen:
            continue
        seen.add(key)
        title, description = meta.get(key, (key, ""))
        data = base64.b64encode(png.read_bytes()).decode("ascii")
        cards.append(CARD_TEMPLATE.format(data=data, title=title, description=description))
    return cards


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

    screen_cards = _collect(pngs, "ScreenshotTest", SCREEN_META)
    role_cards = _collect(pngs, "RoleChromeScreenshotTest", ROLE_META)

    html = HTML_TEMPLATE.format(
        generated_at=datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M UTC"),
        screen_count=len(screen_cards),
        role_count=len(role_cards),
        screen_cards="\n".join(screen_cards),
        role_cards="\n".join(role_cards),
    )
    (out_dir / "index.html").write_text(html, encoding="utf-8")
    print(f"Wrote {out_dir / 'index.html'} ({len(screen_cards)} screens, {len(role_cards)} role variants)")


if __name__ == "__main__":
    main()
