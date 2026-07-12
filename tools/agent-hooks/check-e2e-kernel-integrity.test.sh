#!/usr/bin/env bash
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
guard="$repo/tools/agent-hooks/check-e2e-kernel-integrity.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

run_case() {
  local name="$1"
  local expected="$2"
  local rel="$3"
  local content="$4"
  local root="$tmp/$name"
  mkdir -p "$root/backend/tests/e2e/$(dirname "$rel")"
  printf 'package e2e\n' > "$root/backend/tests/e2e/baseline_test.go"
  printf '%s\n' "$content" > "$root/backend/tests/e2e/$rel"
  if GOATOS_E2E_INTEGRITY_ROOT="$root" bash "$guard" >/dev/null 2>&1; then
    actual=pass
  else
    actual=fail
  fi
  if [[ "$actual" != "$expected" ]]; then
    printf 'e2e-kernel-integrity self-test %s: got %s, want %s\n' "$name" "$actual" "$expected" >&2
    exit 1
  fi
}

run_case safe_input pass fixtures.go 'package e2e; const seed = `INSERT INTO goats (goat_id) VALUES ($1)`'
run_case compiled_helper fail helpers/derived.go 'package helpers; const seed = `INSERT INTO obligation_instances (obligation_id) VALUES ($1)`'
run_case nested_sql fail fixtures/nested/seed.sql 'UPDATE public.vaccination_completions SET status = '\''accepted'\'''
run_case quoted_schema fail fixtures/quoted.sql 'DELETE FROM "public"."calendar_event_projections" WHERE tenant_id = $1'
run_case direct_repository fail helpers/bypass.go 'package helpers; func bad(r Repo) { r.CreateBatch(ctx, input) }'
run_case direct_record_completion fail story_record_completion_test.go 'package e2e; func bad(s Service) { s.RecordCompletion(ctx, input) }'
run_case direct_accept_existing fail story_accept_existing_test.go 'package e2e; func bad(s Service) { s.AcceptExisting(ctx, input) }'
run_case direct_reject_existing fail story_reject_existing_test.go 'package e2e; func bad(s Service) { s.RejectExisting(ctx, input) }'
run_case direct_accept_obligation fail story_accept_obligation_test.go 'package e2e; func bad(f Fixture) { f.AcceptObligation(id, goat, key, now) }'
run_case direct_reschedule fail story_reschedule_test.go 'package e2e; func bad(r Repo) { r.RescheduleObligationByID(ctx, input) }'
run_case nil_task_creator_sop fail story_nil_task_test.go 'package e2e; func bad() { s := NewSweeperService(repo, nil, inv); s.SweepVersion(ctx, tenant, version, SweepConfig{SOPVersionID: sop}, now) }'
run_case nil_task_creator_submit fail story_nil_submit_test.go 'package e2e; func bad() { NewSweeperService(repo, nil, inv); service.SubmitTask(ctx, command, trace) }'
run_case nil_task_creator_sweeper_only pass story_nil_missed_test.go 'package e2e; func safe() { NewSweeperService(repo, nil, inv).MarkMissed(ctx, tenant, now) }'
run_case real_task_creator_submit pass story_real_submit_test.go 'package e2e; func safe() { NewSweeperService(repo, creator, inv); service.SubmitTask(ctx, command, trace) }'
run_case identity_bypass fail fixtures/move.sql 'UPDATE public.goats SET shed_id = $1 WHERE goat_id = $2'
run_case outbox_bypass fail fixtures/replay.sql 'UPDATE outbox_messages SET status = '\''pending'\'' WHERE outbox_id = $1'
run_case copy_bypass fail fixtures/copy.sql 'COPY public.vaccination_completions FROM '\''/tmp/fake.csv'\'' CSV'
run_case merge_bypass fail fixtures/merge.sql 'MERGE INTO obligation_instances AS target USING staged AS source ON false WHEN NOT MATCHED THEN INSERT DEFAULT VALUES'
run_case truncate_bypass fail fixtures/truncate.sql 'TRUNCATE TABLE "public"."calendar_event_projections"'

# Browser/data-plane artifacts are scanned recursively by filename, not a fixed glob.
browser_root="$tmp/browser_nested"
mkdir -p "$browser_root/backend/tests/e2e" "$browser_root/apps/admin-web/scripts/nested"
printf 'package e2e\n' > "$browser_root/backend/tests/e2e/baseline_test.go"
printf 'await sql(`INSERT INTO process_integrity_projection_rows (tenant_id) VALUES ($1)`)\n' > "$browser_root/apps/admin-web/scripts/nested/calendar-e2e-helper.mjs"
if GOATOS_E2E_INTEGRITY_ROOT="$browser_root" bash "$guard" >/dev/null 2>&1; then
  printf 'e2e-kernel-integrity self-test browser_nested: nested script was not rejected\n' >&2
  exit 1
fi

# A safe browser artifact must pass. This prevents a guard crash from making every
# negative fixture look correctly rejected.
safe_browser_root="$tmp/browser_safe"
mkdir -p "$safe_browser_root/backend/tests/e2e" "$safe_browser_root/apps/admin-web/scripts/nested"
printf 'package e2e\n' > "$safe_browser_root/backend/tests/e2e/baseline_test.go"
printf 'await api.createGoat({ displayId: "fixture" })\n' > "$safe_browser_root/apps/admin-web/scripts/nested/calendar-E2E-helper.mjs"
if ! GOATOS_E2E_INTEGRITY_ROOT="$safe_browser_root" bash "$guard" >/dev/null 2>&1; then
  printf 'e2e-kernel-integrity self-test browser_safe: safe browser artifact failed (guard may have crashed)\n' >&2
  exit 1
fi

neutral_browser_root="$tmp/browser_neutral_helper"
mkdir -p "$neutral_browser_root/backend/tests/e2e" "$neutral_browser_root/apps/admin-web/scripts/nested"
printf 'package e2e\n' > "$neutral_browser_root/backend/tests/e2e/baseline_test.go"
printf 'await sql(`INSERT INTO process_integrity_projection_rows (tenant_id) VALUES ($1)`)\n' > "$neutral_browser_root/apps/admin-web/scripts/nested/db-helper.mjs"
if GOATOS_E2E_INTEGRITY_ROOT="$neutral_browser_root" bash "$guard" >/dev/null 2>&1; then
  printf 'e2e-kernel-integrity self-test browser_neutral_helper: neutral helper under apps/admin-web/scripts was not rejected\n' >&2
  exit 1
fi

nested_root="$tmp/nested_path"
mkdir -p "$nested_root/backend/tests/e2e" "$nested_root/tools/dev/E2E/fixtures"
printf 'package e2e\n' > "$nested_root/backend/tests/e2e/baseline_test.go"
printf 'DELETE FROM sop_tasks WHERE tenant_id = $1\n' > "$nested_root/tools/dev/E2E/fixtures/seed.sql"
if GOATOS_E2E_INTEGRITY_ROOT="$nested_root" bash "$guard" >/dev/null 2>&1; then
  printf 'e2e-kernel-integrity self-test nested_path: neutral filename under E2E path was not rejected\n' >&2
  exit 1
fi

tools_dev_go_root="$tmp/tools_dev_go"
mkdir -p "$tools_dev_go_root/backend/tests/e2e" "$tools_dev_go_root/tools/dev/helpers"
printf 'package e2e\n' > "$tools_dev_go_root/backend/tests/e2e/baseline_test.go"
printf 'package helpers; const seed = `INSERT INTO obligation_instances (obligation_id) VALUES ($1)`\n' > "$tools_dev_go_root/tools/dev/helpers/seed.go"
if GOATOS_E2E_INTEGRITY_ROOT="$tools_dev_go_root" bash "$guard" >/dev/null 2>&1; then
  printf 'e2e-kernel-integrity self-test tools_dev_go: compiled Go helper under tools/dev was not rejected\n' >&2
  exit 1
fi

printf 'e2e-kernel-integrity self-test: ok\n'
