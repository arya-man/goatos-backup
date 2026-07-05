#!/usr/bin/env python3
"""Summarize local Claude/Codex transcript token usage for AI routing checks."""

from __future__ import annotations

import argparse
import html
import json
import os
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable


@dataclass
class SessionStats:
    agent: str
    path: Path
    cwd: str = ""
    tokens: int = 0
    graph_events: int = 0
    rtk_events: int = 0
    tool_events: int = 0
    matched_project: bool = False
    day: str = ""
    # Replaceable native calls (candidates the graph/RTK would have absorbed).
    grep_events: int = 0
    read_events: int = 0
    gitdiff_events: int = 0
    # Codex multi-agent orchestration is visible in parent transcripts, but
    # child-agent token usage is not separately exposed in local JSONL.
    codex_agent_spawns: int = 0
    codex_agent_waits: int = 0
    codex_agent_sends: int = 0
    codex_agent_closes: int = 0
    codex_agent_resumes: int = 0


def load_jsonl(path: Path) -> Iterable[dict]:
    try:
        with path.open(encoding="utf-8", errors="ignore") as handle:
            for line in handle:
                line = line.strip()
                if not line:
                    continue
                try:
                    value = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if isinstance(value, dict):
                    yield value
    except OSError:
        return


def token_total(usage: object) -> int:
    if not isinstance(usage, dict):
        return 0
    if isinstance(usage.get("total_tokens"), int):
        return int(usage["total_tokens"])
    keys = (
        "input_tokens",
        "cached_input_tokens",
        "cache_creation_input_tokens",
        "cache_read_input_tokens",
        "output_tokens",
        "reasoning_output_tokens",
    )
    return sum(int(usage.get(k) or 0) for k in keys)


def text_blob(value: object) -> str:
    try:
        return json.dumps(value, sort_keys=True)
    except TypeError:
        return str(value)


def project_match(project: str, *values: str) -> bool:
    if not project:
        return True
    needle = project.lower()
    return any(needle in (value or "").lower() for value in values)


def is_graph_marker(value: str) -> bool:
    marker = value.lower()
    return (
        "code_review_graph" in marker
        or "code-review-graph" in marker
        or "graphify" in marker
    )


def is_rtk_marker(value: str) -> bool:
    marker = value.lower()
    return "rtk" in marker


def is_gitdiff_command(value: str) -> bool:
    """A shell command that dumps a large diff/log an RTK/distill pass would trim."""
    marker = value.lower()
    return any(p in marker for p in ("git diff", "git show", "git log", "git blame"))


def is_grep_command(value: str) -> bool:
    marker = value.lower()
    return any(p in marker for p in ("grep ", "rg ", "ripgrep", "ag ", "ack "))


def claude_tool_calls(message: dict) -> Iterable[tuple[str, object]]:
    content = message.get("content")
    if not isinstance(content, list):
        return
    for block in content:
        if isinstance(block, dict) and block.get("type") == "tool_use":
            yield str(block.get("name") or ""), block.get("input")


def codex_files(root: Path) -> list[Path]:
    paths: list[Path] = []
    paths.extend(root.glob("sessions/**/*.jsonl"))
    paths.extend(root.glob("archived_sessions/*.jsonl"))
    return sorted(set(paths))


def claude_files(root: Path) -> list[Path]:
    paths: list[Path] = []
    for base in (root / "projects", root):
        if base.exists():
            paths.extend(base.glob("**/*.jsonl"))
    return sorted(set(paths))


def iso_day(value: object) -> str:
    """YYYY-MM-DD from an ISO-8601 timestamp string, else empty."""
    if not isinstance(value, str) or len(value) < 10:
        return ""
    head = value[:10]
    if head[4] == "-" and head[7] == "-" and head[:4].isdigit() and head[5:7].isdigit() and head[8:10].isdigit():
        return head
    return ""


def codex_agent_call_kind(name: str) -> str:
    return {
        "spawn_agent": "spawns",
        "wait_agent": "waits",
        "send_input": "sends",
        "close_agent": "closes",
        "resume_agent": "resumes",
    }.get(name, "")


def analyze_codex(path: Path, project: str) -> SessionStats:
    stats = SessionStats(agent="codex", path=path)
    max_total = 0
    saw_incremental = False
    for event in load_jsonl(path):
        payload = event.get("payload") if isinstance(event.get("payload"), dict) else {}
        if not stats.day:
            stats.day = iso_day(event.get("timestamp"))

        if event.get("type") == "session_meta":
            stats.cwd = str(payload.get("cwd") or "")
        elif event.get("type") == "turn_context":
            stats.cwd = stats.cwd or str(payload.get("cwd") or "")

        if project_match(project, stats.cwd, str(path)):
            stats.matched_project = True

        if event.get("type") == "event_msg" and payload.get("type") == "token_count":
            info = payload.get("info") if isinstance(payload.get("info"), dict) else {}
            last = token_total(info.get("last_token_usage"))
            total = token_total(info.get("total_token_usage"))
            max_total = max(max_total, total)
            if last:
                stats.tokens += last
                saw_incremental = True

        if event.get("type") == "response_item":
            name = str(payload.get("name") or "")
            args = str(payload.get("arguments") or "")
            if payload.get("type") == "function_call":
                stats.tool_events += 1
                marker = f"{name} {args}"
                agent_kind = codex_agent_call_kind(name)
                if agent_kind == "spawns":
                    stats.codex_agent_spawns += 1
                elif agent_kind == "waits":
                    stats.codex_agent_waits += 1
                elif agent_kind == "sends":
                    stats.codex_agent_sends += 1
                elif agent_kind == "closes":
                    stats.codex_agent_closes += 1
                elif agent_kind == "resumes":
                    stats.codex_agent_resumes += 1
                if is_graph_marker(marker):
                    stats.graph_events += 1
                if is_rtk_marker(marker):
                    stats.rtk_events += 1
                elif is_gitdiff_command(marker):
                    stats.gitdiff_events += 1
                if is_grep_command(marker):
                    stats.grep_events += 1

    if not saw_incremental:
        stats.tokens = max_total
    return stats


def analyze_claude(path: Path, project: str) -> SessionStats:
    # Subagent + workflow transcripts are separate LLM runs with their own
    # context windows — real ADDITIONAL spend, not the parent's tokens. Keep
    # them in their own bucket so the subagent burn is visible, never folded
    # into the main "claude" line.
    parts = set(path.parts)
    agent = "claude-sub" if ("subagents" in parts or "workflows" in parts) else "claude"
    stats = SessionStats(agent=agent, path=path)
    seen_usage: set[str] = set()
    for event in load_jsonl(path):
        if not stats.day:
            stats.day = iso_day(event.get("timestamp"))
        cwd = event.get("cwd") or event.get("project") or event.get("project_path")
        if isinstance(cwd, str) and cwd:
            stats.cwd = stats.cwd or cwd
        if project_match(project, stats.cwd, str(path)):
            stats.matched_project = True

        message = event.get("message")
        usage = None
        if isinstance(message, dict):
            usage = message.get("usage")
            for name, tool_input in claude_tool_calls(message):
                stats.tool_events += 1
                marker = f"{name} {text_blob(tool_input)}"
                if is_graph_marker(marker):
                    stats.graph_events += 1
                if is_rtk_marker(marker):
                    stats.rtk_events += 1
                elif name == "Read":
                    stats.read_events += 1
                elif name in ("Grep", "Glob"):
                    stats.grep_events += 1
                elif name == "Bash" and is_gitdiff_command(marker):
                    stats.gitdiff_events += 1
        usage = usage or event.get("usage")
        if usage:
            usage_key = json.dumps(usage, sort_keys=True)
            request_key = str(event.get("requestId") or event.get("uuid") or "")
            key = f"{request_key}:{usage_key}"
            if key not in seen_usage:
                seen_usage.add(key)
                stats.tokens += token_total(usage)

    return stats


def session_day(path: Path) -> str:
    """Fallback YYYY-MM-DD when a transcript carried no event timestamp: from a
    Codex YYYY/MM/DD path segment, else the file's modification date (last resort
    only — mtime can be wrong if a transcript was copied/reindexed). Empty if
    neither is available."""
    parts = path.parts
    for i in range(len(parts) - 2):
        y, m, d = parts[i], parts[i + 1], parts[i + 2]
        if len(y) == 4 and y.isdigit() and len(m) == 2 and m.isdigit() and len(d) == 2 and d.isdigit():
            return f"{y}-{m}-{d}"
    try:
        import datetime

        return datetime.date.fromtimestamp(path.stat().st_mtime).isoformat()
    except (OSError, ValueError):
        return ""


def print_table(rows: list[SessionStats], limit: int) -> None:
    shown = rows[:limit]
    print("agent      tokens      graph  rtk  tools  spawns  transcript")
    print("---------  ----------  -----  ---  -----  ------  ----------")
    for row in shown:
        rel = str(row.path).replace(str(Path.home()), "~")
        print(
            f"{row.agent:<9}  {row.tokens:>10,}  {row.graph_events:>5}  "
            f"{row.rtk_events:>3}  {row.tool_events:>5}  "
            f"{row.codex_agent_spawns:>6}  {rel}"
        )


def agent_summary(rows: list[SessionStats]) -> dict[str, dict[str, int]]:
    """Per-agent totals across ALL sessions (not just the top-N table)."""
    summary: dict[str, dict[str, int]] = {}
    for row in rows:
        acc = summary.setdefault(
            row.agent,
            {
                "sessions": 0,
                "tokens": 0,
                "graph": 0,
                "rtk": 0,
                "tools": 0,
                "graph_sessions": 0,
                "rtk_sessions": 0,
                "codex_spawns": 0,
                "codex_waits": 0,
                "codex_sends": 0,
                "codex_closes": 0,
                "codex_resumes": 0,
            },
        )
        acc["sessions"] += 1
        acc["tokens"] += row.tokens
        acc["graph"] += row.graph_events
        acc["rtk"] += row.rtk_events
        acc["tools"] += row.tool_events
        acc["graph_sessions"] += 1 if row.graph_events else 0
        acc["rtk_sessions"] += 1 if row.rtk_events else 0
        acc["codex_spawns"] += row.codex_agent_spawns
        acc["codex_waits"] += row.codex_agent_waits
        acc["codex_sends"] += row.codex_agent_sends
        acc["codex_closes"] += row.codex_agent_closes
        acc["codex_resumes"] += row.codex_agent_resumes
    return summary


def by_day(rows: list[SessionStats]) -> list[dict[str, int | str]]:
    """Daily token totals + graph/RTK adoption, oldest first."""
    days: dict[str, dict[str, int]] = {}
    for row in rows:
        if not row.day:
            continue
        acc = days.setdefault(row.day, {"sessions": 0, "tokens": 0, "graph_sessions": 0, "rtk_sessions": 0})
        acc["sessions"] += 1
        acc["tokens"] += row.tokens
        acc["graph_sessions"] += 1 if row.graph_events else 0
        acc["rtk_sessions"] += 1 if row.rtk_events else 0
    return [{"day": day, **vals} for day, vals in sorted(days.items())]


def daily_savings(
    rows: list[SessionStats],
    graph_save_tok: int,
    rtk_save_tok: int,
    read_replace_frac: float,
    rate: float,
) -> list[dict[str, float | str | int]]:
    """Per-day saved vs missed ($ + tokens) — the series the trend chart buckets
    by day/week/month so the report shows movement, not an ever-growing pile."""
    days: dict[str, dict[str, float]] = {}
    for r in rows:
        if not r.day:
            continue
        a = days.setdefault(r.day, {"sessions": 0, "saved_tok": 0, "missed_tok": 0})
        a["sessions"] += 1
        a["saved_tok"] += r.graph_events * graph_save_tok + r.rtk_events * rtk_save_tok
        a["missed_tok"] += (
            r.grep_events * graph_save_tok
            + r.gitdiff_events * rtk_save_tok
            + int(r.read_events * read_replace_frac) * graph_save_tok
        )
    out: list[dict[str, float | str | int]] = []
    for day in sorted(days):
        a = days[day]
        out.append(
            {
                "d": day,
                "sess": int(a["sessions"]),
                "sav": round(a["saved_tok"] * rate, 2),
                "mis": round(a["missed_tok"] * rate, 2),
            }
        )
    return out


def savings_model(
    rows: list[SessionStats],
    graph_save_tok: int,
    rtk_save_tok: int,
    read_replace_frac: float,
    price_per_mtok: float,
) -> dict[str, float]:
    """Estimate tokens/$ already saved by the stack, plus missed savings from
    grep/read/git-diff calls that bypassed the graph/RTK.

    Per-call token savings are conservative constants from this repo's measured
    eval (CRG structural query ~-99%, RTK diff ~-98%, repowise health -91%:
    ~9k tok avoided per structural query, ~15k per diff dump). Tunable via flags.
    """
    graph_calls = sum(r.graph_events for r in rows)
    rtk_calls = sum(r.rtk_events for r in rows)
    grep_calls = sum(r.grep_events for r in rows)
    read_calls = sum(r.read_events for r in rows)
    gitdiff_calls = sum(r.gitdiff_events for r in rows)

    realized_tok = graph_calls * graph_save_tok + rtk_calls * rtk_save_tok
    # Unclaimed: greps that could be one graph query, diffs that could be one RTK
    # pass, and a conservative fraction of file reads a graph lookup would answer.
    unclaimed_tok = (
        grep_calls * graph_save_tok
        + gitdiff_calls * rtk_save_tok
        + int(read_calls * read_replace_frac) * graph_save_tok
    )
    rate = price_per_mtok / 1_000_000.0
    return {
        "graph_calls": graph_calls,
        "rtk_calls": rtk_calls,
        "grep_calls": grep_calls,
        "read_calls": read_calls,
        "gitdiff_calls": gitdiff_calls,
        "realized_tok": realized_tok,
        "unclaimed_tok": unclaimed_tok,
        "realized_usd": realized_tok * rate,
        "unclaimed_usd": unclaimed_tok * rate,
        "price_per_mtok": price_per_mtok,
    }


def print_savings(m: dict[str, float]) -> None:
    print()
    print("SAVINGS MODEL  (conservative; per-call constants from measured eval)")
    print(f"  price assumed: ${m['price_per_mtok']:.2f} / 1M tokens (input-heavy; Opus ~5x)")
    print(f"  SAVED BY TOOLS: {int(m['realized_tok']):,} tok  ~= ${m['realized_usd']:,.2f}")
    print(f"    from {int(m['graph_calls'])} graph calls + {int(m['rtk_calls'])} RTK calls")
    print(f"  MISSED SAVINGS (raw bypass): {int(m['unclaimed_tok']):,} tok  ~= ${m['unclaimed_usd']:,.2f}")
    print(f"    ({int(m['grep_calls'])} greps + {int(m['gitdiff_calls'])} git-diffs + a fraction of {int(m['read_calls'])} reads bypass the 4)")


def print_agent_summary(summary: dict[str, dict[str, int]]) -> None:
    print()
    print("PER-AGENT TOTALS (all sessions)")
    print("agent      sessions  tokens          graph_calls  rtk_calls  tools   spawned  graph_sess  rtk_sess")
    print("---------  --------  --------------  -----------  ---------  ------  -------  ----------  --------")
    for agent, acc in sorted(summary.items(), key=lambda kv: kv[1]["tokens"], reverse=True):
        print(
            f"{agent:<9}  {acc['sessions']:>8}  {acc['tokens']:>14,}  "
            f"{acc['graph']:>11}  {acc['rtk']:>9}  {acc['tools']:>6}  "
            f"{acc['codex_spawns']:>7}  {acc['graph_sessions']:>10}  {acc['rtk_sessions']:>8}"
        )


def print_codex_agent_summary(summary: dict[str, dict[str, int]]) -> None:
    codex = summary.get("codex", {})
    spawns = int(codex.get("codex_spawns", 0))
    waits = int(codex.get("codex_waits", 0))
    sends = int(codex.get("codex_sends", 0))
    closes = int(codex.get("codex_closes", 0))
    resumes = int(codex.get("codex_resumes", 0))
    if not any((spawns, waits, sends, closes, resumes)):
        return
    print()
    print("CODEX MULTI-AGENT VISIBILITY")
    print(f"  parent transcript calls: {spawns} spawn + {waits} wait + {sends} send + {closes} close + {resumes} resume")
    print("  child-agent token usage is not separately exposed in local Codex JSONL;")
    print("  Codex token totals remain parent-session totals, with orchestration called out here.")


def print_by_day(days: list[dict[str, int | str]], limit: int = 30) -> None:
    print()
    print(f"BY DAY (last {limit}) — tokens + adoption")
    print("day         sessions  tokens          graph_sess  rtk_sess")
    print("----------  --------  --------------  ----------  --------")
    for entry in days[-limit:]:
        print(
            f"{entry['day']:<10}  {entry['sessions']:>8}  {int(entry['tokens']):>14,}  "
            f"{entry['graph_sessions']:>10}  {entry['rtk_sessions']:>8}"
        )


TREND_JS = """<script>
const DAILY = __DAILY__;
let WIN = 30, GRAN = 'day';
function monday(ds){ const d = new Date(ds + 'T00:00:00'); const off = (d.getDay() + 6) % 7; d.setDate(d.getDate() - off); return d.toISOString().slice(0,10); }
function bucketKey(ds){ return GRAN === 'month' ? ds.slice(0,7) : GRAN === 'week' ? monday(ds) : ds; }
function render(){
  if (!DAILY.length) return;
  const maxD = DAILY[DAILY.length - 1].d;
  const cut = new Date(maxD + 'T00:00:00'); cut.setDate(cut.getDate() - (WIN - 1));
  const cutS = cut.toISOString().slice(0,10);
  const rows = WIN >= 9999 ? DAILY : DAILY.filter(x => x.d >= cutS);
  const b = {}; let tsav = 0, tmis = 0, tsess = 0;
  for (const x of rows){ const k = bucketKey(x.d); (b[k] = b[k] || {sav:0, mis:0, sess:0}); b[k].sav += x.sav; b[k].mis += x.mis; b[k].sess += x.sess; tsav += x.sav; tmis += x.mis; tsess += x.sess; }
  const keys = Object.keys(b).sort();
  const maxTot = Math.max(1, ...keys.map(k => b[k].sav + b[k].mis));
  document.getElementById('winsum').innerHTML =
    '<b style="color:#7ee787">$' + tsav.toFixed(0) + '</b> saved &nbsp;·&nbsp; <b style="color:#e3b341">$' + tmis.toFixed(0) + '</b> missed &nbsp;·&nbsp; ' + tsess + ' sessions in this window (' + keys.length + ' ' + GRAN + 's)';
  document.getElementById('trend').innerHTML = keys.map(k => {
    const t = b[k], tot = t.sav + t.mis, w = 100 * tot / maxTot, sp = tot ? 100 * t.sav / tot : 0;
    return '<div class="trow"><span class="tlabel">' + k + '</span>' +
      '<span class="tbar" style="width:' + Math.max(3, w) + '%"><span class="tsav" style="width:' + sp + '%"></span><span class="tmis" style="width:' + (100 - sp) + '%"></span></span>' +
      '<span class="tnum">$' + t.sav.toFixed(0) + ' saved / $' + t.mis.toFixed(0) + ' missed</span></div>';
  }).join('');
}
document.querySelectorAll('.controls button').forEach(btn => btn.onclick = () => {
  if (btn.dataset.win){ WIN = +btn.dataset.win; btn.parentNode.querySelectorAll('[data-win]').forEach(x => x.classList.toggle('on', x === btn)); }
  if (btn.dataset.gran){ GRAN = btn.dataset.gran; btn.parentNode.querySelectorAll('[data-gran]').forEach(x => x.classList.toggle('on', x === btn)); }
  render();
});
render();
</script>"""


def render_html(project: str, summary: dict[str, dict[str, int]], days: list[dict[str, int | str]], rows: list[SessionStats], limit: int, model: dict[str, float], daily: list[dict[str, float | str | int]]) -> str:
    total = sum(a["tokens"] for a in summary.values())
    sessions = sum(a["sessions"] for a in summary.values())
    graph_sess = sum(a["graph_sessions"] for a in summary.values())
    rtk_sess = sum(a["rtk_sessions"] for a in summary.values())
    codex = summary.get("codex", {})
    codex_spawns = int(codex.get("codex_spawns", 0))
    codex_waits = int(codex.get("codex_waits", 0))
    codex_sends = int(codex.get("codex_sends", 0))
    codex_closes = int(codex.get("codex_closes", 0))
    codex_resumes = int(codex.get("codex_resumes", 0))
    max_day = max((int(d["tokens"]) for d in days), default=1) or 1

    def bar(entry: dict[str, int | str]) -> str:
        pct = 100 * int(entry["tokens"]) / max_day
        g = int(entry["graph_sessions"])
        r = int(entry["rtk_sessions"])
        return (
            f'<div class="row"><span class="day">{html.escape(str(entry["day"]))}</span>'
            f'<span class="track"><span class="fill" style="width:{pct:.1f}%"></span></span>'
            f'<span class="num">{int(entry["tokens"]):,}</span>'
            f'<span class="adopt">{int(entry["sessions"])} sess · graph {g} · rtk {r}</span></div>'
        )

    agent_rows = "".join(
        f"<tr><td>{html.escape(a)}</td><td>{acc['sessions']:,}</td><td>{acc['tokens']:,}</td>"
        f"<td>{acc['graph_sessions']}</td><td>{acc['rtk_sessions']}</td>"
        f"<td>{acc['tools']:,}</td><td>{acc['codex_spawns']:,}</td></tr>"
        for a, acc in sorted(summary.items(), key=lambda kv: kv[1]["tokens"], reverse=True)
    )
    top_rows = "".join(
        f"<tr><td>{html.escape(r.agent)}</td><td>{r.tokens:,}</td><td>{r.graph_events}</td>"
        f"<td>{r.rtk_events}</td><td>{r.tool_events}</td><td>{r.codex_agent_spawns}</td>"
        f"<td class=\"path\">{html.escape(str(r.path).replace(str(Path.home()), '~'))}</td></tr>"
        for r in rows[:limit]
    )
    project_safe = html.escape(project)
    trend_js = TREND_JS.replace("__DAILY__", json.dumps(daily))
    return f"""<!doctype html><html><head><meta charset="utf-8">
<title>AI Telemetry — {project_safe}</title><style>
body{{font:14px/1.5 -apple-system,Segoe UI,sans-serif;background:#0d1117;color:#e6edf3;margin:0;padding:24px}}
h1{{font-size:20px;margin:0 0 4px}}h2{{font-size:15px;color:#7ee787;margin:28px 0 10px}}
.sub{{color:#8b949e;margin-bottom:18px}}
.cards{{display:flex;gap:12px;flex-wrap:wrap;margin-bottom:8px}}
.card{{background:#161b22;border:1px solid #30363d;border-radius:10px;padding:14px 18px;min-width:150px}}
.card .k{{color:#8b949e;font-size:12px;text-transform:uppercase}}.card .v{{font-size:22px;font-weight:600;margin-top:4px}}
table{{border-collapse:collapse;width:100%;margin-top:6px}}th,td{{text-align:right;padding:6px 10px;border-bottom:1px solid #21262d}}
th:first-child,td:first-child{{text-align:left}}td.path{{text-align:left;color:#8b949e;font-size:12px;max-width:520px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}}
.row{{display:flex;align-items:center;gap:10px;margin:3px 0}}
.day{{width:92px;color:#8b949e;font-variant-numeric:tabular-nums}}
.track{{flex:1;background:#161b22;border-radius:4px;height:16px;overflow:hidden}}
.fill{{display:block;height:100%;background:linear-gradient(90deg,#1f6feb,#7ee787)}}
.num{{width:120px;text-align:right;font-variant-numeric:tabular-nums}}
.adopt{{width:210px;color:#8b949e;font-size:12px}}
.controls{{display:flex;gap:6px;align-items:center;flex-wrap:wrap;margin:6px 0 10px}}
.controls .lbl{{color:#8b949e;font-size:12px;margin-left:12px}}
.controls button{{background:#161b22;color:#e6edf3;border:1px solid #30363d;border-radius:6px;padding:4px 11px;cursor:pointer;font-size:12px}}
.controls button.on{{border-color:#7ee787;color:#7ee787}}
.trow{{display:flex;align-items:center;gap:10px;margin:2px 0}}
.tlabel{{width:96px;color:#8b949e;font-size:12px;font-variant-numeric:tabular-nums}}
.tbar{{flex:1;display:flex;height:16px;border-radius:4px;overflow:hidden;background:#161b22}}
.tsav{{background:#238636}}.tmis{{background:#9e6a03}}
.tnum{{width:240px;text-align:right;font-size:12px;color:#8b949e;font-variant-numeric:tabular-nums}}
</style></head><body>
<h1>AI Telemetry — project “{project_safe}”</h1>
<div class="sub">Real Claude + Codex transcript usage. repowise/CRG/Graphify/RTK adoption. Generated locally, no network.</div>
<div class="cards">
<div class="card" style="border-color:#238636"><div class="k">💰 Saved by tools</div><div class="v" style="color:#7ee787">${model['realized_usd']:,.0f}</div><div class="k">{int(model['realized_tok']):,} tok avoided</div></div>
<div class="card" style="border-color:#9e6a03"><div class="k">🎯 Missed savings (raw bypass)</div><div class="v" style="color:#e3b341">${model['unclaimed_usd']:,.0f}</div><div class="k">{int(model['unclaimed_tok']):,} tok grepped/diffed raw</div></div>
</div>
<div class="sub" style="font-size:12px">Missed savings = graph/diff/read work that bypassed the 4 tools and ran raw ({int(model['grep_calls'])} greps + {int(model['gitdiff_calls'])} git-diffs + a fraction of {int(model['read_calls'])} reads). Assumes ~{int(model['price_per_mtok'])}$/1M tok, ~9k saved/graph-query, ~15k/diff — conservative, Opus ~5×.</div>
<div class="cards">
<div class="card"><div class="k">Total tokens</div><div class="v">{total:,}</div></div>
<div class="card"><div class="k">Sessions</div><div class="v">{sessions:,}</div></div>
<div class="card"><div class="k">Graph adoption</div><div class="v">{graph_sess}/{sessions} ({100*graph_sess/sessions:.0f}%)</div></div>
<div class="card"><div class="k">RTK adoption</div><div class="v">{rtk_sess}/{sessions} ({100*rtk_sess/sessions:.0f}%)</div></div>
<div class="card"><div class="k">Codex spawned agents</div><div class="v">{codex_spawns:,}</div><div class="k">{codex_waits} wait · {codex_sends} send · {codex_closes} close · {codex_resumes} resume</div></div>
</div>
<div class="sub" style="font-size:12px">Codex multi-agent calls are visible in parent transcripts, but local Codex JSONL does not expose child-agent token usage separately. Codex tokens remain parent-session totals; spawned agents are called out as orchestration.</div>
<h2>Per-agent totals</h2>
<table><tr><th>agent</th><th>sessions</th><th>tokens</th><th>graph sess</th><th>rtk sess</th><th>tool calls</th><th>spawned agents</th></tr>{agent_rows}</table>
<h2>Savings trend — is the missed number shrinking?</h2>
<div class="controls">
<span class="lbl">Window:</span><button data-win="7">7d</button><button data-win="30" class="on">30d</button><button data-win="90">90d</button><button data-win="9999">All</button>
<span class="lbl">Bucket:</span><button data-gran="day" class="on">Day</button><button data-gran="week">Week</button><button data-gran="month">Month</button>
</div>
<div id="winsum" class="sub" style="font-size:13px"></div>
<div id="trend"></div>
<div class="sub" style="font-size:11px;margin-top:6px"><span style="color:#238636">■</span> saved by tools &nbsp; <span style="color:#9e6a03">■</span> missed (raw bypass) — bar length = $ that period, split shows the ratio. Missed shrinking vs saved = adoption improving.</div>
<h2>By day — tokens &amp; tool adoption (all-time)</h2>
{''.join(bar(d) for d in days)}
<h2>Top {limit} heaviest sessions</h2>
<table><tr><th>agent</th><th>tokens</th><th>graph</th><th>rtk</th><th>tools</th><th>spawned</th><th>transcript</th></tr>{top_rows}</table>
{trend_js}
</body></html>"""


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--project", default="mesha", help="case-insensitive cwd/path filter")
    parser.add_argument("--codex-root", default="~/.codex")
    parser.add_argument("--claude-root", default="~/.claude")
    parser.add_argument("--limit", type=int, default=20)
    parser.add_argument("--by-day", action="store_true", help="print the per-day tokens + adoption trend")
    parser.add_argument("--html", default="", help="write a self-contained HTML report to this path")
    parser.add_argument("--price-per-mtok", type=float, default=3.0, help="USD per 1M tokens for the money estimate")
    parser.add_argument("--graph-save", type=int, default=9000, help="tokens saved per graph query (avoided grep+read)")
    parser.add_argument("--rtk-save", type=int, default=15000, help="tokens saved per RTK/distill diff pass")
    parser.add_argument("--read-replace-frac", type=float, default=0.3, help="fraction of file reads a graph lookup could replace")
    parser.add_argument("--since", default="", help="only count sessions on/after this YYYY-MM-DD (drops old history from the totals)")
    args = parser.parse_args()

    codex_root = Path(os.path.expanduser(args.codex_root))
    claude_root = Path(os.path.expanduser(args.claude_root))

    rows: list[SessionStats] = []
    rows.extend(analyze_codex(path, args.project) for path in codex_files(codex_root))
    rows.extend(analyze_claude(path, args.project) for path in claude_files(claude_root))
    rows = [row for row in rows if row.matched_project and (row.tokens or row.tool_events)]
    for row in rows:
        # Prefer the event timestamp captured during analysis; mtime/path only
        # as a fallback for transcripts that carried no timestamp.
        row.day = row.day or session_day(row.path)
    if args.since:
        rows = [row for row in rows if row.day and row.day >= args.since]
    rows.sort(key=lambda row: row.tokens, reverse=True)

    total_tokens = sum(row.tokens for row in rows)
    graph_sessions = sum(1 for row in rows if row.graph_events)
    rtk_sessions = sum(1 for row in rows if row.rtk_events)

    print(f"project={args.project}")
    print(f"sessions={len(rows)} total_tokens={total_tokens:,}")
    print(f"graph_sessions={graph_sessions} rtk_sessions={rtk_sessions}")
    if not rows:
        print("No matching transcript token records found.")
        return 0

    summary = agent_summary(rows)
    days = by_day(rows)
    model = savings_model(rows, args.graph_save, args.rtk_save, args.read_replace_frac, args.price_per_mtok)
    print()
    print_table(rows, args.limit)
    print_agent_summary(summary)
    print_codex_agent_summary(summary)
    if args.by_day:
        print_by_day(days)
    print_savings(model)

    if args.html:
        daily = daily_savings(rows, args.graph_save, args.rtk_save, args.read_replace_frac, args.price_per_mtok / 1_000_000.0)
        out = Path(os.path.expanduser(args.html))
        out.parent.mkdir(parents=True, exist_ok=True)
        out.write_text(render_html(args.project, summary, days, rows, args.limit, model, daily), encoding="utf-8")
        print(f"\nHTML report written: {out}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
