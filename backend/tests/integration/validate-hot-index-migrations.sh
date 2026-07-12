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
    "calendar_event_identities",
    "calendar_event_projections",
    "calendar_snoozes",
    "count_base_anchors",
    "count_mismatch_scan_runs",
    "count_projection_exception_resolutions",
    "count_projection_exceptions",
    "count_projection_recompute_runs",
    "count_projection_snapshot_rows",
    "count_projection_snapshots",
    "counts_shifting_readiness_evidence",
    "domain_event_processed_events",
    "feed_direction_completions",
    "goats",
    "goat_custody_history",
    "goat_identity_counter_processed_events",
    "goat_identity_counter_projection_state",
    "goat_identity_counters",
    "goat_identity_events",
    "goat_identifiers",
    "goat_location_history",
    "goat_ownership",
    "idempotency_keys",
    "inventory_stock",
    "inventory_stock_movements",
    "movement_commands",
    "notification_requests",
    "obligation_batches",
    "obligation_escalations",
    "obligation_goat_shift_watermarks",
    "obligation_instances",
    "obligation_status_events",
    "outbox_dlq_actions",
    "outbox_messages",
    "proof_artifacts",
    "shifting_event_impacts",
    "shifting_events",
    "sop_submission_items",
    "sop_submissions",
    "sop_task_review_fanouts",
    "sop_task_submission_fanouts",
    "sop_tasks",
    "vaccination_completions",
    "vaccination_generation_runs",
}

hot_table_prefixes = (
    "audit_log_",
    "goat_identity_events_",
    "obligation_status_events_",
)

# Older migrations are already checksum-tracked in deployed databases. They stay
# runnable, but this guard prints every historical hot-table lock risk so scale
# certification cannot pretend those migrations are safe on populated tables.
enforcement_floor = 141

# Migration 000152 was applied to shared staging before this guard caught late
# hot-table CHECK rollout shape. Do not edit that checksum-tracked migration.
# Keep it visible as reviewed debt while preserving failure behavior for any new
# unsafe hot-table migration.
reviewed_applied_debt = {
    "000152_obligation_window_check.sql: obligation_instances_window_check on hot table obligation_instances uses direct CHECK/FOREIGN KEY constraint without NOT VALID",
    "000152_obligation_window_check.sql: obligation_instances_window_check on hot table obligation_instances uses direct DROP CONSTRAINT in goose Down section",
    # 000166 (C35-024) relaxes the domain_event_processed_events status CHECK to add the
    # 'effects_committed' value. Changing a CHECK's allowed set unavoidably drops the old
    # constraint; the DROP is catalog-only (brief ACCESS EXCLUSIVE, no scan) and the re-add is
    # NOT VALID + VALIDATE (concurrent). Explicitly reviewed as acceptable hot-table debt.
    "000166_domain_event_processed_effects_committed_status.sql: domain_event_processed_events_status_check on hot table domain_event_processed_events uses direct DROP CONSTRAINT",
    "000166_domain_event_processed_effects_committed_status.sql: domain_event_processed_events_status_check on hot table domain_event_processed_events uses direct DROP CONSTRAINT in goose Down section",
    "000166_domain_event_processed_effects_committed_status.sql: domain_event_processed_events_status_check on hot table domain_event_processed_events uses direct VALIDATE CONSTRAINT",
    "000166_domain_event_processed_effects_committed_status.sql: domain_event_processed_events_status_check on hot table domain_event_processed_events uses direct VALIDATE CONSTRAINT in goose Down section",
    # 000171 relaxes the notification_requests notification_type CHECK to add
    # 'verification_pending' and 'rework' (the vaccination verification/rework notification kernel).
    # Same shape as 000166: the DROP is catalog-only (brief ACCESS EXCLUSIVE, no scan) and the re-add
    # is NOT VALID + VALIDATE (concurrent). Explicitly reviewed as acceptable hot-table debt.
    "000171_vaccination_verification_notification_kernel.sql: notification_requests_type_check on hot table notification_requests uses direct DROP CONSTRAINT",
    "000171_vaccination_verification_notification_kernel.sql: notification_requests_type_check on hot table notification_requests uses direct DROP CONSTRAINT in goose Down section",
    "000171_vaccination_verification_notification_kernel.sql: notification_requests_type_check on hot table notification_requests uses direct VALIDATE CONSTRAINT",
    "000171_vaccination_verification_notification_kernel.sql: notification_requests_type_check on hot table notification_requests uses direct VALIDATE CONSTRAINT in goose Down section",
}

create_table_re = re.compile(r"\bCREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?P<table>[a-zA-Z_][\w.]*)", re.I)
create_index_re = re.compile(
    r"\bCREATE\s+(?:UNIQUE\s+)?INDEX\s+(?P<concurrently>CONCURRENTLY\s+)?"
    r"(?:IF\s+NOT\s+EXISTS\s+)?(?P<index>[a-zA-Z_][\w.]*)\s+ON\s+"
    r"(?P<table>[a-zA-Z_][\w.]*)",
    re.I | re.S,
)
drop_index_re = re.compile(
    r"\bDROP\s+INDEX\s+(?P<concurrently>CONCURRENTLY\s+)?"
    r"(?:IF\s+EXISTS\s+)?(?P<index>[a-zA-Z_][\w.]*)",
    re.I | re.S,
)
alter_add_constraint_re = re.compile(
    r"\bALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?"
    r"(?P<table>[a-zA-Z_][\w.]*)\s+ADD\s+"
    r"(?:CONSTRAINT\s+(?P<constraint>[a-zA-Z_][\w.]*)\s+)?(?P<body>.*)",
    re.I | re.S,
)
alter_table_re = re.compile(
    r"\bALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?(?P<table>[a-zA-Z_][\w.]*)",
    re.I | re.S,
)
drop_constraint_re = re.compile(
    r"\bDROP\s+CONSTRAINT\s+(?:IF\s+EXISTS\s+)?(?P<constraint>[a-zA-Z_][\w.]*)",
    re.I | re.S,
)
validate_constraint_re = re.compile(
    r"\bVALIDATE\s+CONSTRAINT\s+(?P<constraint>[a-zA-Z_][\w.]*)",
    re.I | re.S,
)
direct_index_constraint_re = re.compile(r"\bUNIQUE\b|\bPRIMARY\s+KEY\b|\bEXCLUDE\b", re.I | re.S)
direct_validation_constraint_re = re.compile(r"\bCHECK\b|\bFOREIGN\s+KEY\b|\bREFERENCES\b", re.I | re.S)
not_valid_re = re.compile(r"\bNOT\s+VALID\b", re.I | re.S)
using_index_attach_re = re.compile(r"\b(?:UNIQUE|PRIMARY\s+KEY)\s+USING\s+INDEX\b", re.I | re.S)
concurrent_index_statement_re = re.compile(
    r"\b(?:CREATE\s+(?:UNIQUE\s+)?INDEX|DROP\s+INDEX)\s+CONCURRENTLY\b",
    re.I,
)
no_transaction_re = re.compile(r"(?im)^\s*--\s*\+goose\s+NO\s+TRANSACTION\s*$")


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


def down_section(sql: str) -> str:
    lines = []
    in_down = False
    for line in sql.splitlines():
        stripped = line.strip()
        if stripped.startswith("-- +goose Down"):
            in_down = True
            continue
        if in_down:
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


def is_hot_table(table: str) -> bool:
    return table in hot_tables or any(table.startswith(prefix) for prefix in hot_table_prefixes)


def uses_concurrent_index(section_sql: str) -> bool:
    return bool(concurrent_index_statement_re.search(strip_sql_comments(section_sql)))


def has_no_transaction(section_sql: str) -> bool:
    return bool(no_transaction_re.search(section_sql))


def classify_hot_lock_risk(version: int, message: str) -> None:
    if message in reviewed_applied_debt:
        warnings.append("reviewed-applied-debt: " + message)
        return
    if version < enforcement_floor:
        warnings.append("legacy-pre-floor: " + message)
        return
    violations.append(message)


def classify_direct_constraint_risk(
    *,
    path_name: str,
    version: int,
    section: str,
    statement: str,
    created_in_migration: set[str],
) -> None:
    match = alter_add_constraint_re.search(statement)
    if not match:
        return
    table = bare_name(match.group("table"))
    if table in created_in_migration or not is_hot_table(table):
        return
    body = match.group("body")
    constraint_name = match.group("constraint") or "<unnamed>"
    suffix = "" if section == "goose Up" else f" in {section} section"
    if direct_index_constraint_re.search(body):
        if using_index_attach_re.search(body):
            return
        classify_hot_lock_risk(
            version,
            f"{path_name}: {constraint_name} on hot table {table} uses direct UNIQUE/PRIMARY KEY/EXCLUDE constraint{suffix}",
        )
        return
    if direct_validation_constraint_re.search(body) and not not_valid_re.search(body):
        classify_hot_lock_risk(
            version,
            f"{path_name}: {constraint_name} on hot table {table} uses direct CHECK/FOREIGN KEY constraint without NOT VALID{suffix}",
        )


def classify_drop_constraint_risk(
    *,
    path_name: str,
    version: int,
    section: str,
    statement: str,
    created_in_migration: set[str],
) -> None:
    table_match = alter_table_re.search(statement)
    if not table_match:
        return
    table = bare_name(table_match.group("table"))
    if table in created_in_migration or not is_hot_table(table):
        return
    suffix = "" if section == "goose Up" else f" in {section} section"
    for match in drop_constraint_re.finditer(statement):
        constraint_name = bare_name(match.group("constraint"))
        classify_hot_lock_risk(
            version,
            f"{path_name}: {constraint_name} on hot table {table} uses direct DROP CONSTRAINT{suffix}",
        )


def classify_validate_constraint_risk(
    *,
    path_name: str,
    version: int,
    section: str,
    statement: str,
    created_in_migration: set[str],
) -> None:
    table_match = alter_table_re.search(statement)
    if not table_match:
        return
    table = bare_name(table_match.group("table"))
    if table in created_in_migration or not is_hot_table(table):
        return
    suffix = "" if section == "goose Up" else f" in {section} section"
    for match in validate_constraint_re.finditer(statement):
        constraint_name = bare_name(match.group("constraint"))
        classify_hot_lock_risk(
            version,
            f"{path_name}: {constraint_name} on hot table {table} uses direct VALIDATE CONSTRAINT{suffix}",
        )


violations: list[str] = []
warnings: list[str] = []
index_owner: dict[str, str] = {}

for path in sorted(migration_dir.glob("*.sql")):
    version_match = re.match(r"(?P<version>\d+)_", path.name)
    version = int(version_match.group("version")) if version_match else 0
    raw = path.read_text(encoding="utf-8")
    up_raw = up_section(raw)
    down_raw = down_section(raw)
    if uses_concurrent_index(up_raw) and not has_no_transaction(up_raw):
        violations.append(
            f"{path.name}: concurrent index statement in goose Up section is missing -- +goose NO TRANSACTION"
        )
    if uses_concurrent_index(down_raw) and not has_no_transaction(down_raw):
        violations.append(
            f"{path.name}: concurrent index statement in goose Down section is missing -- +goose NO TRANSACTION"
        )
    up_sql = strip_sql_comments(up_raw)
    down_sql = strip_sql_comments(down_raw)
    up_statements = split_statements(up_sql) if up_sql.strip() else []
    down_statements = split_statements(down_sql) if down_sql.strip() else []
    migration_index_owner: dict[str, str] = {}
    created_in_migration = {
        bare_name(match.group("table"))
        for match in create_table_re.finditer(up_sql)
    }
    for statement in up_statements:
        classify_direct_constraint_risk(
            path_name=path.name,
            version=version,
            section="goose Up",
            statement=statement,
            created_in_migration=created_in_migration,
        )
        classify_drop_constraint_risk(
            path_name=path.name,
            version=version,
            section="goose Up",
            statement=statement,
            created_in_migration=created_in_migration,
        )
        classify_validate_constraint_risk(
            path_name=path.name,
            version=version,
            section="goose Up",
            statement=statement,
            created_in_migration=created_in_migration,
        )

        drop_match = drop_index_re.search(statement)
        if drop_match:
            index_name = bare_name(drop_match.group("index"))
            owner_table = index_owner.get(index_name)
            if owner_table and is_hot_table(owner_table) and not drop_match.group("concurrently"):
                classify_hot_lock_risk(
                    version,
                    f"{path.name}: {index_name} on hot table {owner_table} uses non-concurrent DROP INDEX",
                )
            index_owner.pop(index_name, None)

        match = create_index_re.search(statement)
        if not match:
            continue
        table = bare_name(match.group("table"))
        index_name = bare_name(match.group("index"))
        migration_index_owner[index_name] = table
        index_owner[index_name] = table
        if not is_hot_table(table):
            continue
        if table in created_in_migration:
            continue
        if match.group("concurrently"):
            continue
        classify_hot_lock_risk(
            version,
            f"{path.name}: {index_name} on hot table {table} uses non-concurrent CREATE INDEX",
        )

    for statement in down_statements:
        classify_direct_constraint_risk(
            path_name=path.name,
            version=version,
            section="goose Down",
            statement=statement,
            created_in_migration=created_in_migration,
        )
        classify_drop_constraint_risk(
            path_name=path.name,
            version=version,
            section="goose Down",
            statement=statement,
            created_in_migration=created_in_migration,
        )
        classify_validate_constraint_risk(
            path_name=path.name,
            version=version,
            section="goose Down",
            statement=statement,
            created_in_migration=created_in_migration,
        )

        drop_match = drop_index_re.search(statement)
        if drop_match:
            index_name = bare_name(drop_match.group("index"))
            owner_table = migration_index_owner.get(index_name) or index_owner.get(index_name)
            if owner_table and is_hot_table(owner_table) and not drop_match.group("concurrently"):
                classify_hot_lock_risk(
                    version,
                    f"{path.name}: {index_name} on hot table {owner_table} uses non-concurrent DROP INDEX in goose Down section",
                )

        match = create_index_re.search(statement)
        if not match:
            continue
        table = bare_name(match.group("table"))
        index_name = bare_name(match.group("index"))
        if not is_hot_table(table):
            continue
        if match.group("concurrently"):
            continue
        classify_hot_lock_risk(
            version,
            f"{path.name}: {index_name} on hot table {table} uses non-concurrent CREATE INDEX in goose Down section",
        )

if warnings:
    print("Historical hot-table migration lock warnings:", file=sys.stderr)
    for warning in warnings:
        print(warning, file=sys.stderr)
    sys.stderr.flush()

if violations:
    print("Hot-table migration index safety violations:", file=sys.stderr)
    for violation in violations:
        print(f"  - {violation}", file=sys.stderr)
    print(
        "Use CREATE INDEX CONCURRENTLY / DROP INDEX CONCURRENTLY with a no-transaction "
        "migration for late indexes on populated hot tables. For hot-table uniqueness, "
        "or primary keys, create the index concurrently first, then attach it with "
        "ALTER TABLE ADD CONSTRAINT ... UNIQUE/PRIMARY KEY USING INDEX. For hot-table "
        "CHECK/FOREIGN KEY constraints, add NOT VALID first and validate through an "
        "explicitly reviewed rollout. For hot-table DROP CONSTRAINT, use an explicitly "
        "reviewed no-lock rollout.",
        file=sys.stderr,
    )
    sys.exit(1)

if warnings:
    print(f"Hot-table index migration safety guard passed with {len(warnings)} historical warning(s).")
else:
    print("Hot-table index migration safety guard passed.")
PY
