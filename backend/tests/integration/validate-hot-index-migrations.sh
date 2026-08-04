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
    "identity_decisions",
    "goat_identifiers",
    "goat_location_history",
    "goat_ownership",
    "idempotency_keys",
    "inventory_stock",
    "inventory_stock_movements",
    "movement_commands",
    "notification_delivery_attempts",
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
    "vaccination_projection_dirty_scopes",
    "vaccination_shed_shard_state",
}

# Tables enforced ONLY by the unbounded-UPDATE/DELETE lock_timeout guard
# below (NEW-P1 rollout risk), not by the constraint/index checks above.
# weighing_observations/weighing_shed_observations are animal- and
# shed-grain tables the live capture/submit/verify write paths contend on
# continuously; verification_items is the shared cross-module verdict queue.
# These are intentionally NOT added to the general `hot_tables` set: doing so
# would retroactively re-flag years of already-merged, already-applied
# constraint/index DDL on these tables (000007-000073) that this task did not
# review and has no mandate to touch.
dml_lock_timeout_only_hot_tables = {
    "weighing_observations",
    "weighing_shed_observations",
    "verification_items",
}


def is_hot_table_for_dml(table: str) -> bool:
    return is_hot_table(table) or table in dml_lock_timeout_only_hot_tables

hot_table_prefixes = (
    "audit_log_",
    "goat_identity_events_",
    "obligation_status_events_",
)

# Migrations are numbered from 000001 (clean-slate baseline) onwards. The
# enforcement_floor determines which migrations are subject to the hot-table
# lock-safety check. Migrations below the floor are considered legacy and exempt
# (their unsafe patterns may be in deployed databases with checksum-tracked
# hashes, so we warn but do not fail); migrations at or above the floor must be
# lock-safe or declared in reviewed_applied_debt.
#
# After the squash to clean baseline (000001), enforcement_floor is set to 2:
# - 000001 (clean baseline CREATE SCHEMA) is naturally exempt via the
#   created_in_migration check (baseline creates its own tables).
# - 000002 and onwards are enforced on merit (lock-safe patterns only).
enforcement_floor = 2

# reviewed_applied_debt tracks migrations that were deployed with unsafe
# patterns but are explicitly reviewed and accepted as known debt. After the
# squash to clean baseline (2026-07-19), no pre-squash migrations remain in the
# codebase, so this set is empty. New unsafe patterns added to deployed
# migrations after this floor must be reviewed and added here before shipping.
reviewed_applied_debt = {
    # NEW-P1 rollout-risk guard (unbounded UPDATE/DELETE on a hot table
    # without SET lock_timeout): these migrations are already merged to main
    # and may already be applied in dev/stg. Rewriting their SQL to add a
    # lock_timeout would change their checksum and break every environment
    # that already ran them (see cmd/migrate/main.go's checksum-drift check,
    # and the forward-repair rationale documented in 000074-000077). They are
    # accepted as reviewed historical debt; the guard enforces the pattern on
    # every migration written from here on, including the forward repairs
    # for these same files (000074, 000075, 000076, 000077).
    "000035_death_upload_before_approval.sql: unbounded DELETE on hot table outbox_messages is missing a bounded 'SET lock_timeout' before the statement in goose Down",
    "000061_weighing_observations_submitted_at.sql: unbounded UPDATE on hot table weighing_observations is missing a bounded 'SET lock_timeout' before the statement",
    "000067_weighing_shed_observation_withdrawal.sql: unbounded UPDATE on hot table verification_items is missing a bounded 'SET lock_timeout' before the statement in goose Down",
    "000067_weighing_shed_observation_withdrawal.sql: unbounded DELETE on hot table weighing_shed_observations is missing a bounded 'SET lock_timeout' before the statement in goose Down",
    "000070_weighing_backfill_submitted_at_from_audit.sql: unbounded UPDATE on hot table weighing_observations is missing a bounded 'SET lock_timeout' before the statement",
    "000071_weighing_backfill_verification_status_from_items.sql: unbounded UPDATE on hot table weighing_observations is missing a bounded 'SET lock_timeout' before the statement",
    "000071_weighing_backfill_verification_status_from_items.sql: unbounded UPDATE on hot table weighing_shed_observations is missing a bounded 'SET lock_timeout' before the statement",
    "000073_weighing_observations_one_open_tag_uidx.sql: unbounded UPDATE on hot table weighing_observations is missing a bounded 'SET lock_timeout' before the statement",
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
    safe_constraints_out: set[str],
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
    elif direct_validation_constraint_re.search(body) and not_valid_re.search(body):
        # Constraint added with NOT VALID is part of the safe re-definition pattern.
        # Mark it as safe for subsequent VALIDATE operations on the same constraint.
        if constraint_name and constraint_name != "<unnamed>":
            safe_constraints_out.add(bare_name(constraint_name))


def classify_drop_constraint_risk(
    *,
    path_name: str,
    version: int,
    section: str,
    statement: str,
    created_in_migration: set[str],
    no_transaction: bool,
    will_be_re_added_constraints: set[str],
) -> None:
    table_match = alter_table_re.search(statement)
    if not table_match:
        return
    table = bare_name(table_match.group("table"))
    if table in created_in_migration or not is_hot_table(table):
        return
    # A section that runs NO TRANSACTION commits each statement independently, so a DROP CONSTRAINT
    # is its own brief catalog-only ACCESS EXCLUSIVE with no accumulated lock — lock-safe (VACC-REV-11).
    if no_transaction:
        return
    suffix = "" if section == "goose Up" else f" in {section} section"
    for match in drop_constraint_re.finditer(statement):
        constraint_name = bare_name(match.group("constraint"))
        # DROP CONSTRAINT followed by ADD CONSTRAINT ... NOT VALID is the safe re-definition pattern
        # for updating constraints. Don't flag drops that will be re-added with NOT VALID.
        if constraint_name in will_be_re_added_constraints:
            continue
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
    no_transaction: bool,
    safe_to_validate_constraints: set[str],
) -> None:
    table_match = alter_table_re.search(statement)
    if not table_match:
        return
    table = bare_name(table_match.group("table"))
    if table in created_in_migration or not is_hot_table(table):
        return
    # A VALIDATE CONSTRAINT inside a TRANSACTIONAL migration is safe if it was preceded by
    # ADD CONSTRAINT ... NOT VALID for the same constraint (the safe constraint re-definition pattern).
    # A NO TRANSACTION section also makes VALIDATE safe (each statement commits independently).
    if no_transaction:
        return
    suffix = "" if section == "goose Up" else f" in {section} section"
    for match in validate_constraint_re.finditer(statement):
        constraint_name = bare_name(match.group("constraint"))
        # If this constraint was added with NOT VALID in the same section, don't flag it.
        if constraint_name in safe_to_validate_constraints:
            continue
        classify_hot_lock_risk(
            version,
            f"{path_name}: {constraint_name} on hot table {table} uses direct VALIDATE CONSTRAINT{suffix}",
        )


def classify_lock_timeout_risk(
    *,
    path_name: str,
    version: int,
    section_text: str,
    no_transaction: bool,
) -> None:
    """
    Check that NO TRANSACTION migrations with hot-table constraint operations
    have bounded SET lock_timeout before each operation (VACC-REV-11 P1).

    A NO TRANSACTION migration is only lock-safe if each statement is bounded by
    a lock_timeout, preventing indefinite hangs on lock contention. This guard
    enforces that pattern for ADD/VALIDATE/DROP CONSTRAINT on hot tables.
    """
    if not no_transaction:
        return
    # The clean-slate baseline may be marked NO TRANSACTION because its folded
    # history contains concurrent index statements. It creates the hot tables
    # itself, so baseline constraint DDL is not a late hot-table operation.
    if version < enforcement_floor:
        return

    # Patterns for lock-timeout-requiring operations on hot tables
    constraint_ops = re.compile(
        r"\b(?:ADD|VALIDATE|DROP)\s+CONSTRAINT\b",
        re.I
    )

    # Pattern to detect SET lock_timeout in proximity (before the operation)
    lock_timeout_pattern = re.compile(r"SET\s+lock_timeout\s*=", re.I)

    # Split section into statements (roughly by semicolon, accounting for comments)
    statements = split_statements(strip_sql_comments(section_text))

    for i, stmt in enumerate(statements):
        # Check if this statement contains a hot-table constraint operation
        if not constraint_ops.search(stmt):
            continue

        # Extract table name from ALTER TABLE statement
        table_match = alter_table_re.search(stmt)
        if not table_match:
            continue

        table = bare_name(table_match.group("table"))
        if not is_hot_table(table):
            continue

        # Check if SET lock_timeout appears in this statement
        has_timeout = bool(lock_timeout_pattern.search(stmt))

        # If not found, look back in the last few statements (up to 5 prior statements)
        # to account for pattern: SET lock_timeout; ... other statements ... ; constraint op;
        if not has_timeout:
            for j in range(max(0, i - 5), i):
                if lock_timeout_pattern.search(statements[j]):
                    has_timeout = True
                    break

        if not has_timeout:
            suffix = "" if "goose Up" in section_text else "in goose Down"
            violations.append(
                f"{path_name}: NO TRANSACTION migration with hot-table constraint operation on {table} "
                f"is missing bounded 'SET lock_timeout' before the {constraint_ops.search(stmt).group().upper()} statement {suffix}"
            )


dml_update_re = re.compile(r"^\s*UPDATE\s+(?:ONLY\s+)?(?P<table>[a-zA-Z_][\w.]*)", re.I)
dml_delete_re = re.compile(r"^\s*DELETE\s+FROM\s+(?:ONLY\s+)?(?P<table>[a-zA-Z_][\w.]*)", re.I)
lock_timeout_stmt_re = re.compile(r"SET\s+(?:LOCAL\s+)?lock_timeout\s*=", re.I)


def classify_unbounded_dml_lock_timeout_risk(
    *,
    path_name: str,
    version: int,
    section: str,
    section_text: str,
    created_in_migration: set[str],
) -> None:
    """
    Any plain UPDATE/DELETE against a hot table (weighing_observations,
    weighing_shed_observations, verification_items, audit_log, etc.)
    contends for row locks with the live write paths those tables serve.
    This guard requires a bounded 'SET lock_timeout' somewhere before the
    statement in the same section, so a migration can fail fast on
    contention instead of blocking (or being blocked by) production traffic
    indefinitely. It applies regardless of NO TRANSACTION, since an ordinary
    transactional migration holds whatever row locks it takes for the life
    of its own transaction too.
    """
    if version < enforcement_floor:
        return
    statements = split_statements(strip_sql_comments(section_text))
    for i, stmt in enumerate(statements):
        match = dml_update_re.search(stmt) or dml_delete_re.search(stmt)
        if not match:
            continue
        table = bare_name(match.group("table"))
        if table in created_in_migration or not is_hot_table_for_dml(table):
            continue
        has_timeout = bool(lock_timeout_stmt_re.search(stmt))
        if not has_timeout:
            for j in range(0, i):
                if lock_timeout_stmt_re.search(statements[j]):
                    has_timeout = True
                    break
        if not has_timeout:
            verb = "UPDATE" if dml_update_re.search(stmt) else "DELETE"
            suffix = "" if section == "goose Up" else f" in {section}"
            classify_hot_lock_risk(
                version,
                f"{path_name}: unbounded {verb} on hot table {table} is missing a bounded "
                f"'SET lock_timeout' before the statement{suffix}",
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
    # Check for lock_timeout on hot-table constraint operations in NO TRANSACTION migrations (VACC-REV-11 P1)
    classify_lock_timeout_risk(
        path_name=path.name,
        version=version,
        section_text=up_raw,
        no_transaction=has_no_transaction(up_raw),
    )
    classify_lock_timeout_risk(
        path_name=path.name,
        version=version,
        section_text=down_raw,
        no_transaction=has_no_transaction(down_raw),
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
    # Check for unbounded UPDATE/DELETE on hot tables missing lock_timeout (NEW-P1 rollout risk)
    classify_unbounded_dml_lock_timeout_risk(
        path_name=path.name,
        version=version,
        section="goose Up",
        section_text=up_raw,
        created_in_migration=created_in_migration,
    )
    classify_unbounded_dml_lock_timeout_risk(
        path_name=path.name,
        version=version,
        section="goose Down",
        section_text=down_raw,
        created_in_migration=created_in_migration,
    )
    # Pre-process: collect constraints added with NOT VALID (safe for VALIDATE and DROP+re-add pattern)
    safe_constraints_up: set[str] = set()
    for statement in up_statements:
        classify_direct_constraint_risk(
            path_name=path.name,
            version=version,
            section="goose Up",
            statement=statement,
            created_in_migration=created_in_migration,
            safe_constraints_out=safe_constraints_up,
        )

    # Classify risks using the collected safe constraints
    for statement in up_statements:
        classify_drop_constraint_risk(
            path_name=path.name,
            version=version,
            section="goose Up",
            statement=statement,
            created_in_migration=created_in_migration,
            no_transaction=has_no_transaction(up_raw),
            will_be_re_added_constraints=safe_constraints_up,
        )
        classify_validate_constraint_risk(
            path_name=path.name,
            version=version,
            section="goose Up",
            statement=statement,
            created_in_migration=created_in_migration,
            no_transaction=has_no_transaction(up_raw),
            safe_to_validate_constraints=safe_constraints_up,
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

    # Pre-process: collect constraints added with NOT VALID (safe for VALIDATE and DROP+re-add pattern)
    safe_constraints_down: set[str] = set()
    for statement in down_statements:
        classify_direct_constraint_risk(
            path_name=path.name,
            version=version,
            section="goose Down",
            statement=statement,
            created_in_migration=created_in_migration,
            safe_constraints_out=safe_constraints_down,
        )

    # Classify risks using the collected safe constraints
    for statement in down_statements:
        classify_drop_constraint_risk(
            path_name=path.name,
            version=version,
            section="goose Down",
            statement=statement,
            created_in_migration=created_in_migration,
            no_transaction=has_no_transaction(down_raw),
            will_be_re_added_constraints=safe_constraints_down,
        )
        classify_validate_constraint_risk(
            path_name=path.name,
            version=version,
            section="goose Down",
            statement=statement,
            created_in_migration=created_in_migration,
            no_transaction=has_no_transaction(down_raw),
            safe_to_validate_constraints=safe_constraints_down,
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
