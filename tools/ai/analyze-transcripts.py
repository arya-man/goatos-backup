#!/usr/bin/env python3
"""Summarize local Claude/Codex transcript token usage for AI routing checks."""

from __future__ import annotations

import argparse
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


def analyze_codex(path: Path, project: str) -> SessionStats:
    stats = SessionStats(agent="codex", path=path)
    max_total = 0
    saw_incremental = False
    for event in load_jsonl(path):
        payload = event.get("payload") if isinstance(event.get("payload"), dict) else {}

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
                if is_graph_marker(marker):
                    stats.graph_events += 1
                if is_rtk_marker(marker):
                    stats.rtk_events += 1

    if not saw_incremental:
        stats.tokens = max_total
    return stats


def analyze_claude(path: Path, project: str) -> SessionStats:
    stats = SessionStats(agent="claude", path=path)
    seen_usage: set[str] = set()
    for event in load_jsonl(path):
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
        usage = usage or event.get("usage")
        if usage:
            usage_key = json.dumps(usage, sort_keys=True)
            request_key = str(event.get("requestId") or event.get("uuid") or "")
            key = f"{request_key}:{usage_key}"
            if key not in seen_usage:
                seen_usage.add(key)
                stats.tokens += token_total(usage)

    return stats


def print_table(rows: list[SessionStats], limit: int) -> None:
    shown = rows[:limit]
    print("agent  tokens      graph  rtk  tools  transcript")
    print("-----  ----------  -----  ---  -----  ----------")
    for row in shown:
        rel = str(row.path).replace(str(Path.home()), "~")
        print(
            f"{row.agent:<5}  {row.tokens:>10,}  {row.graph_events:>5}  "
            f"{row.rtk_events:>3}  {row.tool_events:>5}  {rel}"
        )


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--project", default="mesha", help="case-insensitive cwd/path filter")
    parser.add_argument("--codex-root", default="~/.codex")
    parser.add_argument("--claude-root", default="~/.claude")
    parser.add_argument("--limit", type=int, default=20)
    args = parser.parse_args()

    codex_root = Path(os.path.expanduser(args.codex_root))
    claude_root = Path(os.path.expanduser(args.claude_root))

    rows: list[SessionStats] = []
    rows.extend(analyze_codex(path, args.project) for path in codex_files(codex_root))
    rows.extend(analyze_claude(path, args.project) for path in claude_files(claude_root))
    rows = [row for row in rows if row.matched_project and (row.tokens or row.tool_events)]
    rows.sort(key=lambda row: row.tokens, reverse=True)

    total_tokens = sum(row.tokens for row in rows)
    graph_sessions = sum(1 for row in rows if row.graph_events)
    rtk_sessions = sum(1 for row in rows if row.rtk_events)

    print(f"project={args.project}")
    print(f"sessions={len(rows)} total_tokens={total_tokens:,}")
    print(f"graph_sessions={graph_sessions} rtk_sessions={rtk_sessions}")
    if rows:
        print()
        print_table(rows, args.limit)
    else:
        print("No matching transcript token records found.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
