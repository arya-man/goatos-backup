#!/usr/bin/env python3
"""Generates a Mermaid flowchart + a self-contained HTML page for the Goat OS Android app's
navigation graph (Compose Navigation), for CI publishing (GitHub Pages / job summary).

Source of truth: apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/ui/AppNavHost.kt.
The ROUTES / EDGES data below is a hand-verified extraction of every `composable(Routes.X)`
destination and every `navController.navigate(...)` / `popBackStack()` call inside it, plus the
local (non-route) bottom-sheet overlays each screen can open. AppNavHost.kt threads a few routes
through small helper functions (Routes.recordRoute(shedId), Routes.scanRoute(shedId),
calendarTargetRoute(target)) whose target depends on runtime state (e.g. "is this shed done?");
those are captured here as the two/three concrete destinations they can resolve to, with the
condition on the edge label. If AppNavHost.kt's routing changes, update ROUTES/EDGES/SHEETS below
to match, then re-run this script — it does not parse the Kotlin source (that changes far less
often than it would take to keep a robust parser correct for these dynamic-route helpers).

Usage:
    python3 tools/android/generate-nav-graph.py [output_dir]

Default output_dir: apps/goatos-android/docs/nav-graph
Writes: <output_dir>/index.html (self-contained page, Mermaid rendered via CDN script)
        <output_dir>/nav-graph.mmd (the raw Mermaid source, for reuse elsewhere)
"""
from __future__ import annotations

import sys
from pathlib import Path
from datetime import datetime, timezone

# id -> (label, route path)
ROUTES: dict[str, tuple[str, str]] = {
    "CALENDAR": ("Calendar", "/calendar"),
    "VACCINATION": ("Sheds (vaccination execution)", "/vaccination"),
    "SCAN": ("Scan", "/scan?shedId={shedId}"),
    "SUBMIT": ("Submit", "/submit"),
    "LEADERSHIP": ("Overview (leadership)", "/leadership"),
    "RECORD": ("Record (read-only)", "/record?shedId={shedId}"),
    "OVERDUE": ("Overdue", "/overdue"),
    "RESCHEDULE": ("Reschedule", "/reschedule"),
    "YOU": ("You / Settings", "you"),
    "RFID": ("RFID reader", "/rfid"),
    "ALERTS": ("Alerts", "/alerts"),
}

START = "CALENDAR"

# Local (non-route) bottom-sheet overlays a screen can open, keyed by the route it opens from.
SHEETS: dict[str, list[str]] = {
    "LEADERSHIP": ["ScopePickerSheet", "DataGapsSheet", "DosesGivenSheet"],
    "YOU": ["LanguageSheet"],
}

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

# Bottom-nav / tab destinations reachable per role (mock nav bars), for context only — these are
# not navigate() calls, they're the persistent bottom bar. Kept separate from EDGES so the
# diagram doesn't conflate "user tapped a card" with "user tapped a tab".
TABS: dict[str, list[str]] = {
    "operator": ["CALENDAR", "VACCINATION", "ALERTS", "YOU"],
    "director_ceo": ["CALENDAR", "LEADERSHIP", "ALERTS", "YOU"],
}


def mermaid_id(route_id: str) -> str:
    return f"r_{route_id.lower()}"


def build_mermaid() -> str:
    lines = ["flowchart TD"]
    for rid, (label, path) in ROUTES.items():
        node = mermaid_id(rid)
        text = f"{label}<br/><code>{path}</code>"
        if rid == START:
            lines.append(f'    {node}(["{text}"]):::start')
        else:
            lines.append(f'    {node}["{text}"]')
    lines.append("    back((Back / pop)):::back")
    for src, dst, label in EDGES:
        s = mermaid_id(src)
        d = "back" if dst == "back" else mermaid_id(dst)
        safe_label = label.replace('"', "'")
        lines.append(f'    {s} -->|"{safe_label}"| {d}')
    for rid, sheets in SHEETS.items():
        s = mermaid_id(rid)
        for sheet in sheets:
            sheet_node = f"sheet_{rid.lower()}_{sheet.lower()}"
            lines.append(f'    {sheet_node}(["{sheet} (overlay)"]):::sheet')
            lines.append(f"    {s} -.-> {sheet_node}")
    lines.append("    classDef start fill:#8ad457,stroke:#5f9c37,color:#0a0f0c,font-weight:bold;")
    lines.append("    classDef back fill:#232b26,stroke:#3a453e,color:#c9d6cf,stroke-dasharray: 3 3;")
    lines.append("    classDef sheet fill:#182019,stroke:#3a453e,color:#9db3a7,stroke-dasharray: 2 2;")
    return "\n".join(lines)


HTML_TEMPLATE = """<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Goat OS Android — navigation graph</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<script src="https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js"></script>
<style>
  :root {{ color-scheme: dark light; }}
  body {{
    margin: 0; padding: 24px; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    background: #0a0f0c; color: #e7ede9;
  }}
  h1 {{ font-size: 20px; margin: 0 0 4px; }}
  p.meta {{ color: #8aa196; font-size: 13px; margin: 0 0 20px; }}
  .legend {{ display: flex; gap: 18px; flex-wrap: wrap; margin-bottom: 18px; font-size: 12.5px; color: #c9d6cf; }}
  .legend span.dot {{ display: inline-block; width: 10px; height: 10px; border-radius: 3px; margin-right: 6px; vertical-align: middle; }}
  .graph-wrap {{ background: #0d1310; border: 1px solid #232b26; border-radius: 14px; padding: 20px; overflow: auto; }}
  a {{ color: #8ad457; }}
  code {{ font-size: 11px; }}
</style>
</head>
<body>
  <h1>Goat OS Android — navigation graph</h1>
  <p class="meta">Generated {generated_at} from AppNavHost.kt (routes + navigate() edges). Dashed
  nodes are local bottom-sheet overlays, not navigation-graph destinations. Regenerate with
  <code>python3 tools/android/generate-nav-graph.py</code> after routing changes.</p>
  <div class="legend">
    <span><span class="dot" style="background:#8ad457"></span>Start destination (Calendar)</span>
    <span><span class="dot" style="background:#232b26;border:1px dashed #3a453e"></span>Back / pop</span>
    <span><span class="dot" style="background:#182019;border:1px dashed #3a453e"></span>Local overlay (sheet, not a route)</span>
  </div>
  <div class="graph-wrap">
    <pre class="mermaid">
{mermaid}
    </pre>
  </div>
  <script>
    mermaid.initialize({{ startOnLoad: true, theme: 'dark' }});
  </script>
</body>
</html>
"""


def main() -> None:
    out_dir = Path(sys.argv[1]) if len(sys.argv) > 1 else Path("apps/goatos-android/docs/nav-graph")
    out_dir.mkdir(parents=True, exist_ok=True)

    mermaid_src = build_mermaid()
    (out_dir / "nav-graph.mmd").write_text(mermaid_src + "\n", encoding="utf-8")

    html = HTML_TEMPLATE.format(
        generated_at=datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M UTC"),
        mermaid=mermaid_src,
    )
    (out_dir / "index.html").write_text(html, encoding="utf-8")
    print(f"Wrote {out_dir / 'index.html'}")
    print(f"Wrote {out_dir / 'nav-graph.mmd'}")


if __name__ == "__main__":
    main()
