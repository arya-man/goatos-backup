#!/usr/bin/env python3
"""Generates a thumbnail-based navigation graph for the Goat OS Android app.

Each node is a Paparazzi screenshot thumbnail with route label and navigate() edges
between them. The graph is rendered as self-contained SVG + HTML (no external deps).

Source of truth: apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/ui/AppNavHost.kt.
The ROUTES / EDGES data below is a hand-verified extraction of every `composable(Routes.X)`
destination and every `navController.navigate(...)` / `popBackStack()` call, plus local
bottom-sheet overlays. If AppNavHost.kt's routing changes, update ROUTES/EDGES/SHEETS below,
then re-run this script.

Usage:
    python3 tools/android/generate-nav-graph.py [output_dir]

Default output_dir: apps/goatos-android/docs/nav-graph
Writes: <output_dir>/index.html (self-contained page with embedded Paparazzi PNGs as base64)
"""
from __future__ import annotations

import base64
import json
import sys
from pathlib import Path
from datetime import datetime, timezone

# id -> (label, route path, screenshot_name)
ROUTES: dict[str, tuple[str, str, str]] = {
    "CALENDAR": ("Calendar", "/calendar", "calendar"),
    "VACCINATION": ("Sheds", "/vaccination", "sheds"),
    "SCAN": ("Scan", "/scan?shedId={shedId}", "scan"),
    "SUBMIT": ("Submit", "/submit", "submit"),
    "LEADERSHIP": ("Overview", "/leadership", "overview"),
    "RECORD": ("Record", "/record?shedId={shedId}", "record"),
    "OVERDUE": ("Overdue", "/overdue", "overdue"),
    "RESCHEDULE": ("Reschedule", "/reschedule", "reschedule"),
    "YOU": ("Settings", "you", "you"),
    "RFID": ("RFID", "/rfid", "rfid"),
    "ALERTS": ("Alerts", "/alerts", "alerts"),
}

START = "CALENDAR"

# (from, to, label) — every navController.navigate(...) edge. "back" is popBackStack().
EDGES: list[tuple[str, str, str]] = [
    ("CALENDAR", "RECORD", "tap history/week item → history target is a past record"),
    ("CALENDAR", "VACCINATION", "tap week item (no record target) or tap a day"),
    ("VACCINATION", "RECORD", "tap a DONE shed → read-only record"),
    ("VACCINATION", "SCAN", "tap an open shed → execute (Scan → Submit)"),
    ("SCAN", "SUBMIT", "Submit"),
    ("SCAN", "back", "Back"),
    ("LEADERSHIP", "RESCHEDULE", "Decision tapped"),
    ("LEADERSHIP", "RECORD", "Shed tapped (leadership = read-only follow-up)"),
    ("LEADERSHIP", "OVERDUE", "KPI tapped (not \"given\")"),
    ("OVERDUE", "RESCHEDULE", "Row tapped"),
    ("OVERDUE", "back", "Back"),
    ("RESCHEDULE", "back", "Confirm or Back"),
    ("RECORD", "back", "Close"),
    ("YOU", "RFID", "Pair RFID"),
    ("YOU", "ALERTS", "Toggle notifications"),
]

SNAPSHOTS_DIR = Path("apps/goatos-android/app/src/test/snapshots/images")


def find_screenshot_file(screen_name: str) -> Path | None:
    """Find the Paparazzi screenshot PNG for a screen name."""
    if not SNAPSHOTS_DIR.is_dir():
        return None
    pattern = f"*_ScreenshotTest_{screen_name}_*.png"
    matches = list(SNAPSHOTS_DIR.glob(pattern))
    return matches[0] if matches else None


def load_screenshot_base64(screen_name: str) -> str | None:
    """Load a screenshot PNG and return it as base64 data URI."""
    path = find_screenshot_file(screen_name)
    if not path:
        return None
    data = base64.b64encode(path.read_bytes()).decode("ascii")
    return f"data:image/png;base64,{data}"


HTML_TEMPLATE = """<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Goat OS Android — navigation graph</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
  :root {{ color-scheme: dark light; }}
  body {{
    margin: 0; padding: 24px; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    background: #0a0f0c; color: #e7ede9;
  }}
  h1 {{ font-size: 20px; margin: 0 0 4px; }}
  p.meta {{ color: #8aa196; font-size: 13px; margin: 0 0 24px; line-height: 1.5; }}
  .legend {{ display: flex; gap: 20px; flex-wrap: wrap; margin-bottom: 20px; font-size: 12px; color: #c9d6cf; }}
  .legend span {{ display: flex; align-items: center; gap: 6px; }}
  .legend .dot {{ display: inline-block; width: 12px; height: 12px; border-radius: 3px; }}
  .graph-wrap {{ position: relative; background: #0d1310; border: 1px solid #232b26; border-radius: 14px; padding: 20px; min-height: 600px; overflow: auto; }}
  svg {{ position: absolute; top: 0; left: 0; width: 100%; height: 100%; pointer-events: none; }}
  .nodes {{ position: relative; display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 80px; padding: 40px 20px; }}
  .node {{
    position: relative; cursor: pointer; transition: all 0.2s;
  }}
  .node-img {{
    display: block; width: 100%; height: auto; border-radius: 8px; border: 2px solid #232b26;
    background: #000; transition: border-color 0.2s; box-shadow: 0 2px 8px rgba(0,0,0,0.3);
  }}
  .node:hover .node-img {{
    border-color: #8ad457; box-shadow: 0 4px 12px rgba(138, 212, 87, 0.2);
  }}
  .node.start .node-img {{
    border-color: #8ad457; box-shadow: 0 0 12px rgba(138, 212, 87, 0.4);
  }}
  .node-label {{
    position: absolute; bottom: -30px; left: 0; right: 0; text-align: center; font-size: 12px;
    font-weight: 500; color: #c9d6cf; white-space: nowrap; text-overflow: ellipsis; overflow: hidden;
  }}
  .node-route {{
    position: absolute; bottom: -48px; left: 0; right: 0; text-align: center; font-size: 9px;
    color: #667b77; font-family: 'Courier New', monospace;
  }}
  .edge-label {{
    font-size: 10px; fill: #8aa196; background: rgba(13, 19, 16, 0.9); padding: 2px 4px;
    pointer-events: none; text-anchor: middle;
  }}
  .back-node {{
    position: absolute; bottom: 40px; right: 40px; width: 50px; height: 50px;
    border: 2px dashed #3a453e; border-radius: 50%; display: flex; align-items: center; justify-content: center;
    font-size: 10px; color: #c9d6cf; text-align: center; line-height: 1.2; font-weight: 500;
  }}
  a {{ color: #8ad457; text-decoration: none; }}
  a:hover {{ text-decoration: underline; }}
</style>
</head>
<body>
  <h1>Goat OS Android — navigation graph</h1>
  <p class="meta">Generated {generated_at} from AppNavHost.kt (routes + navigate() edges, with
  Paparazzi screenshot thumbnails). Solid borders are navigable routes. Solid green border indicates start destination (Calendar).
  Each node shows the screen name and its route path. Regenerate with <code>python3 tools/android/generate-nav-graph.py</code> after routing changes.</p>
  <div class="legend">
    <span><span class="dot" style="background:#8ad457"></span>Start (Calendar)</span>
    <span><span class="dot" style="background:#232b26;border:1px dashed #3a453e"></span>Back / pop</span>
  </div>
  <div class="graph-wrap" id="graph">
    <svg id="edges" style="z-index: 1;"></svg>
    <div class="nodes" id="nodes" style="z-index: 2;"></div>
    <div class="back-node">Back</div>
  </div>
  <script>
    const routes = {routes_json};
    const edges = {edges_json};

    // Render nodes from screenshot images
    const nodesDiv = document.getElementById('nodes');
    Object.entries(routes).forEach(([id, {{label, route, img}}]) => {{
      const nodeEl = document.createElement('div');
      nodeEl.className = 'node' + (id === 'CALENDAR' ? ' start' : '');
      nodeEl.id = 'node-' + id;
      nodeEl.innerHTML = `
        <img src="${{img}}" alt="${{label}}" class="node-img" />
        <div class="node-label">${{label}}</div>
        <div class="node-route">${{route.substring(0, 20)}}</div>
      `;
      nodesDiv.appendChild(nodeEl);
    }});

    // Draw SVG edges after DOM layout
    setTimeout(() => {{
      const svg = document.getElementById('edges');
      const graphRect = document.getElementById('graph').getBoundingClientRect();
      svg.setAttribute('width', graphRect.width);
      svg.setAttribute('height', graphRect.height);

      edges.forEach(([from, to, label]) => {{
        const fromEl = document.getElementById('node-' + from);
        const toEl = to === 'back' ? document.querySelector('.back-node') : document.getElementById('node-' + to);
        if (!fromEl || !toEl) return;

        const fromRect = fromEl.getBoundingClientRect();
        const toRect = toEl.getBoundingClientRect();
        const graphOffsetX = document.getElementById('graph').getBoundingClientRect().left;
        const graphOffsetY = document.getElementById('graph').getBoundingClientRect().top;

        const x1 = fromRect.left - graphOffsetX + fromRect.width / 2;
        const y1 = fromRect.top - graphOffsetY + fromRect.height / 2;
        const x2 = toRect.left - graphOffsetX + toRect.width / 2;
        const y2 = toRect.top - graphOffsetY + toRect.height / 2;

        const line = document.createElementNS('http://www.w3.org/2000/svg', 'line');
        line.setAttribute('x1', x1);
        line.setAttribute('y1', y1);
        line.setAttribute('x2', x2);
        line.setAttribute('y2', y2);
        line.setAttribute('stroke', '#3a453e');
        line.setAttribute('stroke-width', '1.5');
        line.setAttribute('stroke-dasharray', to === 'back' ? '4 2' : '0');
        svg.appendChild(line);

        if (label && label.length < 40) {{
          const text = document.createElementNS('http://www.w3.org/2000/svg', 'text');
          text.setAttribute('x', (x1 + x2) / 2);
          text.setAttribute('y', (y1 + y2) / 2 - 8);
          text.setAttribute('class', 'edge-label');
          text.textContent = label.substring(0, 30);
          svg.appendChild(text);
        }}
      }});
    }}, 200);
  </script>
</body>
</html>
"""


def main() -> None:
    out_dir = Path(sys.argv[1]) if len(sys.argv) > 1 else Path("apps/goatos-android/docs/nav-graph")
    out_dir.mkdir(parents=True, exist_ok=True)

    # Load screenshots as base64
    routes_json = {}
    for rid, (label, path, screen_name) in ROUTES.items():
        img_data = load_screenshot_base64(screen_name)
        if not img_data:
            print(f"Warning: No screenshot found for {screen_name}, using placeholder")
            img_data = f"data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='200' height='400'%3E%3Crect fill='%23232b26' width='200' height='400'/%3E%3Ctext x='50%25' y='50%25' text-anchor='middle' fill='%238aa196' font-size='12'%3E{screen_name}%3C/text%3E%3C/svg%3E"
        routes_json[rid] = {"label": label, "route": path, "img": img_data}

    # Prepare edges JSON
    edges_data = []
    for src, dst, label in EDGES:
        edges_data.append([src, dst, label])

    html = HTML_TEMPLATE.format(
        generated_at=datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M UTC"),
        routes_json=json.dumps(routes_json),
        edges_json=json.dumps(edges_data),
    )
    (out_dir / "index.html").write_text(html, encoding="utf-8")
    print(f"Wrote {out_dir / 'index.html'} (with embedded Paparazzi thumbnails)")


if __name__ == "__main__":
    main()
