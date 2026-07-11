#!/usr/bin/env bash
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo"

# E2E may seed source/input fixtures (tenant, herd animal, location, workforce,
# stock, and authored configuration). It must never seed the derived business
# result it claims to prove. Those rows must be produced by the same
# service/API/event/worker path used in production.
story_globs=(
  "backend/tests/e2e/*_test.go"
  "backend/tests/e2e-hrms/*_test.go"
)

files=()
for glob in "${story_globs[@]}"; do
  while IFS= read -r file; do
    files+=("$file")
  done < <(compgen -G "$glob" || true)
done

if ((${#files[@]} == 0)); then
  printf 'e2e-kernel-integrity: no story files found\n' >&2
  exit 1
fi

derived_table_pattern='(obligation_instances|obligation_batches|obligation_status_events|vaccination_completions|calendar_event_projections|process_integrity_projection_rows|notification_requests|notification_deliveries|sop_tasks|sop_submissions)'
failed=0

if rg -n -U -i "(insert[[:space:]]+into|update|delete[[:space:]]+from)[[:space:]]+${derived_table_pattern}" "${files[@]}"; then
  printf '\ne2e-kernel-integrity: E2E story directly mutates derived business state.\n' >&2
  failed=1
fi

if rg -n '\.InsertObligation\(' "${files[@]}"; then
  printf '\ne2e-kernel-integrity: E2E story bypasses generation by inserting an obligation directly.\n' >&2
  failed=1
fi

if rg -n -U -i 'update[[:space:]]+goats[[:space:]]+set|delete[[:space:]]+from[[:space:]]+goats' "${files[@]}"; then
  printf '\ne2e-kernel-integrity: E2E story bypasses the identity mutation/outbox path.\n' >&2
  failed=1
fi

# Shell/browser E2E must enter through APIs/commands. Direct SQL writes to a
# derived table make the browser proof a readback test, not end to end.
script_files=(
  tools/dev/*e2e*.sh
  apps/admin-web/scripts/*e2e*.mjs
  apps/admin-web/scripts/smoke-*-live.mjs
)
existing_scripts=()
for glob in "${script_files[@]}"; do
  while IFS= read -r file; do
    existing_scripts+=("$file")
  done < <(compgen -G "$glob" || true)
done
if ((${#existing_scripts[@]} > 0)) && rg -n -U -i "(insert[[:space:]]+into|update|delete[[:space:]]+from)[[:space:]]+${derived_table_pattern}" "${existing_scripts[@]}"; then
  printf '\ne2e-kernel-integrity: browser/data-plane E2E directly mutates derived business state.\n' >&2
  failed=1
fi

if ((failed != 0)); then
  cat >&2 <<'EOF'

Allowed: seed external/input facts such as a goat, shed, workforce member,
inventory lot, or draft protocol configuration.

Forbidden: seed or directly update the obligation, completion, batch, SOP,
notification, Calendar, or process-integrity result being asserted. Drive those
outcomes through the production service/API/event/worker path instead.

During a story, goat health, reproductive, move, and exit transitions must use
the production identity command and its emitted outbox event. Direct goat rows
are allowed only for initial fixture creation.
EOF
  exit 1
fi

printf 'e2e-kernel-integrity: passed\n'
