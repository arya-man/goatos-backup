#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"

python3 - "$repo_root" <<'PY'
from __future__ import annotations

import re
import sys
from pathlib import Path

repo_root = Path(sys.argv[1])
migration_dir = repo_root / "backend" / "migrations" / "postgres"

hot_tables = {
    "audit_log",
    "calendar_event_projections",
    "count_projection_snapshot_rows",
    "count_projection_snapshots",
    "goats",
    "idempotency_keys",
    "inventory_stock_movements",
    "notification_requests",
    "obligation_batches",
    "obligation_instances",
    "outbox_messages",
    "sop_submissions",
    "sop_tasks",
    "vaccination_completions",
}

# These were committed before this guard existed. They are tracked in
# docs/protocol-engine/high-scale-kernel-validation-plan.md as historical risks
# that must be applied before hot tables grow to 1M scale or replaced by a
# no-lock rollout path before certification.
grandfathered = {
    ("000115_protocol_repeat_and_obligation_dedup.sql", "obligation_instances_open_logical_due_idx"),
    ("000137_obligation_missed_deadline_in_progress_index.sql", "obligation_instances_missed_deadline_idx"),
    ("000140_obligation_unbatched_due_version_index.sql", "obligation_instances_unbatched_due_version_idx"),
}

enforcement_floor = 141

create_table_re = re.compile(r"\bCREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?P<table>[a-zA-Z_][\w.]*)", re.I)
create_index_re = re.compile(
    r"\bCREATE\s+(?:UNIQUE\s+)?INDEX\s+(?P<concurrently>CONCURRENTLY\s+)?"
    r"(?:IF\s+NOT\s+EXISTS\s+)?(?P<index>[a-zA-Z_][\w.]*)\s+ON\s+"
    r"(?P<table>[a-zA-Z_][\w.]*)",
    re.I | re.S,
)


def strip_sql_comments(sql: str) -> str:
    sql = re.sub(r"/\*.*?\*/", " ", sql, flags=re.S)
    return "\n".join(line.split("--", 1)[0] for line in sql.splitlines())


def up_section(sql: str) -> str:
    lines = []
    in_up = False
    for line in sql.splitlines():
        stripped = line.strip()
        if stripped.startswith("-- +goose Up"):
            in_up = True
            continue
        if stripped.startswith("-- +goose Down"):
            break
        if in_up:
            lines.append(line)
    return "\n".join(lines)


def split_statements(sql: str) -> list[str]:
    statements: list[str] = []
    current: list[str] = []
    in_single = False
    in_double = False
    dollar_tag: str | None = None
    i = 0
    while i < len(sql):
        ch = sql[i]
        current.append(ch)
        if dollar_tag:
            if sql.startswith(dollar_tag, i):
                current.extend(sql[i + 1 : i + len(dollar_tag)])
                i += len(dollar_tag) - 1
                dollar_tag = None
        elif in_single:
            if ch == "'" and sql[i + 1 : i + 2] == "'":
                current.append("'")
                i += 1
            elif ch == "'":
                in_single = False
        elif in_double:
            if ch == '"':
                in_double = False
        else:
            if ch == "'":
                in_single = True
            elif ch == '"':
                in_double = True
            elif ch == "$":
                match = re.match(r"\$[A-Za-z_][A-Za-z0-9_]*\$|\$\$", sql[i:])
                if match:
                    tag = match.group(0)
                    current.extend(sql[i + 1 : i + len(tag)])
                    i += len(tag) - 1
                    dollar_tag = tag
            elif ch == ";":
                statement = "".join(current).strip()
                if statement:
                    statements.append(statement)
                current = []
        i += 1
    tail = "".join(current).strip()
    if tail:
        statements.append(tail)
    return statements


def bare_name(name: str) -> str:
    return name.split(".")[-1].strip('"').lower()


violations: list[str] = []
warnings: list[str] = []

for path in sorted(migration_dir.glob("*.sql")):
    version_match = re.match(r"(?P<version>\d+)_", path.name)
    version = int(version_match.group("version")) if version_match else 0
    raw = path.read_text(encoding="utf-8")
    up_sql = strip_sql_comments(up_section(raw))
    if not up_sql.strip():
        continue
    created_in_migration = {
        bare_name(match.group("table"))
        for match in create_table_re.finditer(up_sql)
    }
    for statement in split_statements(up_sql):
        match = create_index_re.search(statement)
        if not match:
            continue
        table = bare_name(match.group("table"))
        if table not in hot_tables:
            continue
        if table in created_in_migration:
            continue
        index_name = bare_name(match.group("index"))
        key = (path.name, index_name)
        if match.group("concurrently"):
            continue
        message = f"{path.name}: {index_name} on hot table {table} uses non-concurrent CREATE INDEX"
        if key in grandfathered:
            warnings.append("grandfathered: " + message)
            continue
        if version < enforcement_floor:
            continue
        violations.append(message)

if warnings:
    for warning in warnings:
        print(warning, file=sys.stderr)

if violations:
    print("Hot-table migration index safety violations:", file=sys.stderr)
    for violation in violations:
        print(f"  - {violation}", file=sys.stderr)
    print(
        "Use CREATE INDEX CONCURRENTLY / DROP INDEX CONCURRENTLY with a no-transaction "
        "migration for late indexes on populated hot tables.",
        file=sys.stderr,
    )
    sys.exit(1)

print("Hot-table index migration safety guard passed.")
PY
