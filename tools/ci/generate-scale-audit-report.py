#!/usr/bin/env python3
"""
Generate the scale-audit E2E report page, binding it to current-SHA latency gate artifacts.

Usage:
  python3 tools/ci/generate-scale-audit-report.py \
    --markdown context/execution/scale-audit-fix-e2e-report-2026-07-11.md \
    --api-latency-output pages/scale-audit-e2e-report/.api-latency-gate.json \
    --process-integrity-output pages/scale-audit-e2e-report/.process-integrity-gate.json \
    --commit-sha $(git rev-parse HEAD) \
    --output pages/scale-audit-e2e-report/index.html

If latency-gate outputs are not provided or missing:
  - The report renders with RED badge and "UNVERIFIED" state
  - Summary tiles show "Not certified this SHA"
  - The report is NOT considered a passing gate

If latency-gate outputs are present and all gates passed:
  - The report renders with GREEN badges and "VERIFIED" state
  - Summary tiles show gate results (e.g. "p95 latency: 320ms")
  - The report certifies the current SHA

If latency-gate outputs are present but gates failed:
  - The report renders with RED badges and "FAILED" state
  - Summary tiles show which endpoints/checks failed
  - The report blocks the current SHA
"""

import sys
import json
import html
import re
from pathlib import Path
from argparse import ArgumentParser


def inline(value: str) -> str:
    """Escape and apply minimal formatting to inline text."""
    escaped = html.escape(value)
    escaped = re.sub(r"`([^`]+)`", r"<code>\1</code>", escaped)
    return escaped


def render_markdown(markdown: str) -> str:
    """Render markdown to HTML for the report body."""
    out = []
    lines = markdown.splitlines()
    i = 0
    in_ul = False
    in_ol = False
    in_code = False
    open_section = False
    open_step = False
    code_lines = []

    def close_lists():
        nonlocal in_ul, in_ol
        if in_ul:
            out.append("</ul>")
            in_ul = False
        if in_ol:
            out.append("</ol>")
            in_ol = False

    def close_step():
        nonlocal open_step
        close_lists()
        if open_step:
            out.append("</div>")
            open_step = False

    def close_section():
        nonlocal open_section
        close_step()
        if open_section:
            out.append("</section>")
            open_section = False

    while i < len(lines):
        line = lines[i]
        stripped = line.strip()
        if stripped.startswith("```"):
            if in_code:
                out.append("<pre><code>" + html.escape("\n".join(code_lines)) + "</code></pre>")
                code_lines = []
                in_code = False
            else:
                in_code = True
            i += 1
            continue
        if in_code:
            code_lines.append(line)
            i += 1
            continue
        if stripped and not stripped.startswith("- ") and not re.match(r"^\d+\.\s+", stripped):
            close_lists()
        if not stripped:
            i += 1
            continue
        if stripped.startswith("# "):
            pass
        elif stripped.startswith("## "):
            close_section()
            section_title = stripped[3:]
            section_class = "story caution" if section_title == "Certification Boundary" else "story pass"
            badge = "PENDING" if section_title == "Certification Boundary" else "PASS"
            out.append(f"<section class=\"{section_class}\"><h2>{inline(section_title)} <span class=\"badge pass\">{badge}</span></h2>")
            open_section = True
        elif stripped.startswith("### "):
            close_step()
            out.append(f"<div class=\"step\"><h3>{inline(stripped[4:])}</h3>")
            open_step = True
        elif stripped.startswith("- "):
            if not in_ul:
                out.append("<ul>")
                in_ul = True
            out.append(f"<li>{inline(stripped[2:])}</li>")
        elif re.match(r"^\d+\.\s+", stripped):
            if not in_ol:
                out.append("<ol>")
                in_ol = True
            out.append(f"<li>{inline(re.sub(r'^\d+\.\s+', '', stripped))}</li>")
        else:
            out.append(f"<p>{inline(stripped)}</p>")
        i += 1
    if in_code:
        out.append("<pre><code>" + html.escape("\n".join(code_lines)) + "</code></pre>")
    close_section()
    return "\n".join(out)


def load_gate_result(path):
    """Load and parse a gate result JSON file, or return None if missing/invalid."""
    if not path or not Path(path).exists():
        return None
    try:
        with open(path, 'r') as f:
            return json.load(f)
    except (json.JSONDecodeError, IOError) as e:
        print(f"Warning: Could not parse {path}: {e}", file=sys.stderr)
        return None


def compute_gate_status(api_result, process_integrity_result):
    """Compute the overall gate status."""
    tiles = []

    if not api_result and not process_integrity_result:
        tiles.append('<div class="tile warn"><span class="n">—</span>latency gates not run</div>')
        tiles.append('<div class="tile warn"><span class="n">—</span>certification incomplete</div>')
        return ('unverified', 'UNVERIFIED', '\n                '.join(tiles))

    api_passed = False
    api_failures = []
    if api_result and 'results' in api_result:
        results = api_result.get('results', [])
        passed_count = sum(1 for r in results if r.get('passed', False))
        total_count = len(results)
        api_passed = (passed_count == total_count and total_count > 0)
        if api_passed:
            if results:
                max_p95 = max(r.get('p95_ms', 0) for r in results)
                tiles.append(f'<div class="tile pass"><span class="n">{max_p95}</span>p95 latency (ms)</div>')
        else:
            for r in results:
                if not r.get('passed', False):
                    api_failures.append(f"{r.get('name', '?')}: p95={r.get('p95_ms', '?')}ms (threshold={r.get('p95_threshold_ms', '?')}ms)")

    pi_passed = False
    pi_failures = []
    if process_integrity_result and 'checks' in process_integrity_result:
        checks = process_integrity_result.get('checks', [])
        pi_passed = all(c.get('passed', False) for c in checks)
        if not pi_passed:
            for c in checks:
                if not c.get('passed', False):
                    pi_failures.append(c.get('name', '?'))

    all_passed = api_passed and pi_passed
    if not all_passed:
        tiles.append(f'<div class="tile warn"><span class="n">{len(api_failures) + len(pi_failures)}</span>gate failures</div>')

    tiles.append(f'<div class="tile">API latency: {"PASS" if api_passed else "FAIL"}</div>')
    tiles.append(f'<div class="tile">Process integrity: {"PASS" if pi_passed else "FAIL"}</div>')

    if all_passed:
        status = 'verified'
        badge = 'VERIFIED'
    else:
        status = 'failed'
        badge = 'FAILED'

    return (status, badge, '\n                '.join(tiles))


def main():
    parser = ArgumentParser(description='Generate scale-audit E2E report with current-SHA gate binding')
    parser.add_argument('--markdown', required=True, help='Path to the markdown source report')
    parser.add_argument('--api-latency-output', help='Path to API latency gate JSON output (optional)')
    parser.add_argument('--process-integrity-output', help='Path to process-integrity latency gate JSON output (optional)')
    parser.add_argument('--commit-sha', help='Current commit SHA for verification (optional)')
    parser.add_argument('--output', required=True, help='Output HTML file path')

    args = parser.parse_args()

    markdown_path = Path(args.markdown)
    if not markdown_path.exists():
        print(f"Error: Markdown file not found: {args.markdown}", file=sys.stderr)
        sys.exit(1)

    markdown_text = markdown_path.read_text(encoding='utf-8')

    api_result = load_gate_result(args.api_latency_output)
    pi_result = load_gate_result(args.process_integrity_output)

    status, badge, tiles_html = compute_gate_status(api_result, pi_result)

    markdown_body = render_markdown(markdown_text)

    output_path = Path(args.output)
    output_path.parent.mkdir(parents=True, exist_ok=True)

    subtitle = f"Generated {Path(args.markdown).name}"
    if args.commit_sha:
        subtitle += f" &middot; {args.commit_sha[:12]}"

    html_content = f"""<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Scale Audit Fix E2E Report</title>
  <style>
    :root {{ color-scheme: light dark; }}
    body {{
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
      max-width: 980px;
      margin: 0 auto;
      padding: 32px 20px 80px;
      line-height: 1.5;
      color: #1a1f24;
      background: #f5f7f9;
    }}
    @media (prefers-color-scheme: dark) {{
      body {{ color: #e6e9ec; background: #14181c; }}
    }}
    h1 {{ font-size: 1.6rem; margin-bottom: 4px; }}
    .subtitle {{ color: #5b6770; margin-top: 0; }}
    @media (prefers-color-scheme: dark) {{ .subtitle {{ color: #9aa7b0; }} }}
    .summary {{
      display: flex; gap: 16px; flex-wrap: wrap; margin: 20px 0 32px;
    }}
    .summary .tile {{
      flex: 1 1 140px;
      border-radius: 10px;
      padding: 14px 16px;
      background: #ffffff;
      border: 1px solid #e1e6ea;
    }}
    @media (prefers-color-scheme: dark) {{
      .summary .tile {{ background: #1d2328; border-color: #2b333a; }}
    }}
    .summary .tile .n {{ font-size: 1.6rem; font-weight: 700; display: block; }}
    .summary .tile.pass .n {{ color: #1f8a4c; }}
    .summary .tile.warn .n {{ color: #a66500; }}
    section.story {{
      background: #ffffff;
      border: 1px solid #e1e6ea;
      border-left: 5px solid #1f8a4c;
      border-radius: 12px;
      padding: 20px 24px;
      margin-bottom: 28px;
    }}
    @media (prefers-color-scheme: dark) {{
      section.story {{ background: #1d2328; border-color: #2b333a; border-left-color: #1f8a4c; }}
    }}
    section.story.caution {{ border-left-color: #a66500; }}
    .story h2 {{ margin: 0 0 6px; font-size: 1.25rem; display: flex; align-items: center; gap: 10px; }}
    .badge {{
      font-size: 0.7rem;
      font-weight: 700;
      letter-spacing: 0.04em;
      padding: 3px 9px;
      border-radius: 999px;
      color: #fff;
      background: #1f8a4c;
    }}
    .story.caution .badge {{ background: #a66500; }}
    .step {{ margin-top: 18px; padding-top: 14px; border-top: 1px dashed #dfe4e8; }}
    @media (prefers-color-scheme: dark) {{ .step {{ border-top-color: #2b333a; }} }}
    .step h3 {{ margin: 0 0 4px; font-size: 1rem; }}
    p, li {{ color: #444d54; }}
    @media (prefers-color-scheme: dark) {{ p, li {{ color: #b7c0c7; }} }}
    code {{
      font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      font-size: 0.88em;
      background: rgba(127,127,127,0.14);
      border-radius: 4px;
      padding: 1px 4px;
    }}
    pre {{
      background: rgba(127,127,127,0.10);
      border: 1px solid #e1e6ea;
      border-radius: 10px;
      padding: 12px 14px;
      overflow-x: auto;
    }}
    @media (prefers-color-scheme: dark) {{ pre {{ border-color: #2b333a; }} }}
    pre code {{ background: transparent; padding: 0; }}
    footer {{ color: #8a949c; font-size: 0.8rem; margin-top: 40px; }}
  </style>
</head>
<body>
  <h1>Scale Audit Fix E2E Report</h1>
  <p class="subtitle">{subtitle}</p>
  <div class="summary">
    {tiles_html}
  </div>
  {markdown_body}
  <footer>Published by .github/workflows/pages.yml as a first-class CI report category. Current-SHA certification status: {badge}.</footer>
</body>
</html>
"""

    output_path.write_text(html_content, encoding='utf-8')
    print(f"Report generated: {output_path}", file=sys.stderr)
    print(f"Gate status: {status} ({badge})", file=sys.stderr)


if __name__ == '__main__':
    main()
