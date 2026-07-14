#!/usr/bin/env bash
# tools/e2e/business-chain-driver.sh — `make e2e-business-chain` (Phase 2a).
#
# Drives the REAL Goat OS operational kernel chain end-to-end against the Phase-1
# deploy/e2e/docker-compose.e2e.yml stack (real goatos-backend:e2e image, real Postgres,
# real Pub/Sub emulator, the consolidated api + kernel-worker topology). NOTHING downstream
# of the ingress is hand-seeded: this driver seeds only INPUT FACTS (authored protocol
# config via the real seed-vaccination-trigger CLI, a role grant via seed-dev-grant, and a
# single goat row — an allowed initial fixture) and then produces the goat.created domain
# event through the REAL identity outbox path (backfill-goat-created CLI, which writes the
# outbox row inside the identity module's own transaction — it does NOT raw-insert a derived
# row). Every hop after that (Pub/Sub publish, domain-event consumption, obligation
# generation, sweep/batch, Calendar read) is produced by the running kernel-worker + api and
# ASSERTED from the resulting canonical rows / real HTTP responses.
#
# This obeys tools/agent-hooks/check-e2e-kernel-integrity.sh: it never INSERT/UPDATE/DELETEs
# a kernel-produced table (domain_events, outbox_messages, domain_event_processed_events,
# obligation_instances, obligation_batches, notification_requests, calendar_*). The ONLY
# writes are: INSERT INTO goats (allowed initial input fixture) and the seed CLIs' authored
# config. Duplicate delivery is exercised the production-shaped way — re-publishing the SAME
# message to the Pub/Sub emulator source topic (the broker is an INPUT boundary), never by
# faking a derived outbox/processed-event row.
#
# Scenarios (all RUN, none stubbed):
#   A. Business chain: input -> outbox -> Pub/Sub -> consumer -> obligation -> sweep/batch
#      -> Calendar API read (each hop polled with a strict timeout; fails naming the hop).
#   B. Duplicate delivery / idempotency (P0): re-publish the same event to the topic; assert
#      domain_event_duplicate_skipped is logged AND the obligation count is unchanged.
#   C. Restart catch-up: stop the kernel-worker, create a backlog event while it is down,
#      restart it, assert it catches up and generates the obligation.
#   D. Two-worker concurrency: scale kernel-worker to 2 (advisory locks via
#      pg_try_advisory_lock), drive a backlog, assert EACH item is processed exactly once
#      (one obligation per goat, no duplicate side effects).
#
# Reminder/notification hop: see REMINDER_NOTE below and the Phase-2a report. In this build a
# freshly generated obligation's drive is always planned for a FUTURE date (drive-planner
# batching hold + catch-up clamp), and SweepDueReminders only queues a notification_requests
# row when a drive's due_at <= now()+1h. So a notification row is NOT deterministically
# producible in a seconds-long run without wall-clock travel or seeding a derived drive date
# (banned). The driver drives the REAL reminder sweep and asserts on its real behaviour +
# proves the remindable drive is visible via the Calendar API, rather than fabricating a row.
#
# Docker-unavailable handling: SKIPS (exit 0, loud warning — NOT a silent pass), same posture
# as tools/dev/e2e-smoke.sh and pgtest.SkipIfNoDocker. Set GOATOS_E2E_BUSINESS_CHAIN_SKIP=1 to
# opt out explicitly. Tears the stack + volumes down on ANY exit path.

set -uo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
compose_file="$repo_root/deploy/e2e/docker-compose.e2e.yml"
project="${GOATOS_E2E_PROJECT:-goatos-e2e-bizchain}"
api_port="${GOATOS_E2E_API_PORT:-8092}"
pubsub_port="${GOATOS_E2E_PUBSUB_PORT:-8097}"
pg_port="${GOATOS_E2E_PG_PORT:-55534}"
tenant_id="${GOATOS_E2E_TENANT_ID:-00000000-0000-4000-8000-000000000001}"
user_id="${GOATOS_E2E_USER_ID:-90000000-0000-4000-8000-000000000101}"
pubsub_project="${GOATOS_E2E_PUBSUB_PROJECT_ID:-goatos-e2e}"
export GOATOS_E2E_IMAGE="${GOATOS_E2E_IMAGE:-goatos-backend:e2e}"
export GOATOS_E2E_API_PORT="$api_port" GOATOS_E2E_PUBSUB_PORT="$pubsub_port" GOATOS_E2E_PG_PORT="$pg_port"
export GOATOS_E2E_PUBSUB_PROJECT_ID="$pubsub_project"

# Fixed input locations/protocol seeded by seed-vaccination-trigger (kid ET+TT schedule).
park_id="00000000-0000-4000-8000-000000003001"
shed_id="00000000-0000-4000-8000-000000003101"
custodian_id="00000000-0000-4000-8000-000000001001"

chain_timeout="${GOATOS_E2E_CHAIN_TIMEOUT:-120}"
fast_lane_timeout="${GOATOS_E2E_FAST_TIMEOUT:-80}"   # outbox relay runs on the 1-minute fast cadence

if [ "${GOATOS_E2E_BUSINESS_CHAIN_SKIP:-0}" = "1" ]; then
  echo "!! e2e-business-chain: SKIPPED — GOATOS_E2E_BUSINESS_CHAIN_SKIP=1 set explicitly. NOT a pass." >&2
  exit 0
fi
if ! command -v docker >/dev/null 2>&1; then
  echo "!! e2e-business-chain: SKIPPED — docker CLI not available. NOT a pass." >&2
  exit 0
fi
if ! docker info >/dev/null 2>&1; then
  echo "!! e2e-business-chain: SKIPPED — docker daemon not reachable. NOT a pass." >&2
  exit 0
fi

compose() { docker compose -f "$compose_file" -p "$project" "$@"; }
psqlq() { compose exec -T postgres psql -qAt -U postgres -d goatos -v ON_ERROR_STOP=1 -c "$1"; }
run_cli() { # entrypoint, args...
  local ep="$1"; shift
  compose run --rm --no-deps --entrypoint "/app/bin/$ep" api "$@"
}

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
pass() { printf "${GREEN}PASS:${NC} %s\n" "$*"; }
info() { printf "${BLUE}INFO:${NC} %s\n" "$*"; }
warn() { printf "${YELLOW}NOTE:${NC} %s\n" "$*"; }
step() { printf "\n${BLUE}════ %s${NC}\n" "$*"; }
fail() { printf "${RED}FAIL:${NC} %s\n" "$*" >&2; exit 1; }

cleanup() {
  local status=$?
  echo ""
  echo "── e2e-business-chain: tearing down (project=$project, exit=$status)"
  compose logs --no-color --tail=120 >"$repo_root/.e2e-business-chain-last-logs.txt" 2>&1 || true
  compose down --volumes --remove-orphans >/dev/null 2>&1 || true
  exit "$status"
}
trap cleanup EXIT

# poll <label> <predicate-cmd> <budget> — succeeds when predicate exits 0; fails naming the hop.
poll() {
  local label="$1" pred="$2" budget="${3:-$chain_timeout}" i
  for i in $(seq 1 "$budget"); do
    if eval "$pred" >/dev/null 2>&1; then pass "$label (${i}s)"; return 0; fi
    [ $((i % 20)) -eq 0 ] && info "...waiting ${i}/${budget}s for: $label"
    sleep 1
  done
  fail "chain STALLED at: $label (timed out after ${budget}s)"
}

insert_goat() { # goat_id, dob_offset_days
  psqlq "INSERT INTO goats (goat_id, tenant_id, lifecycle_status, health_status, reproductive_status,
      custodian_party_id, current_location_id, park_id, shed_id, management_stage, sex, breed,
      dob, approx_dob, entry_date, species, origin_type)
    VALUES ('$1','$tenant_id','alive','healthy','open','$custodian_id','$shed_id','$park_id','$shed_id',
      'K2','female','all', current_date-$2, current_date-$2, current_date-$2, 'goat','birth')" >/dev/null
}

# Emit goat.created through the REAL identity outbox path (writes the outbox row inside the
# identity module's transaction — this is a producer, not a raw derived-row insert).
emit_goat_created() { run_cli backfill-goat-created -tenant-id "$tenant_id" -goat-id "$1" -limit 1 >/dev/null 2>&1; }

# ─────────────────────────────────────────────────────────────────────────────
step "Bring up the ephemeral e2e stack (real image, real Pub/Sub)"
compose config --quiet || fail "compose config invalid"
# --build rebuilds ONLY the `migrate` service (the sole service with a build: section) so its
# migrations image is always current — the api/kernel-worker use the pre-built goatos-backend:e2e
# image (no build: section, untouched by --build). This prevents the migrate-image-vs-backend-image
# drift where a stale cached migrate image stops the DB one migration behind the binary and the API
# waits forever on /readyz (migration_drift_binaryahead_transient).
compose up -d --build postgres migrate pubsub pubsub-bootstrap api kernel-worker \
  || fail "compose up failed — is $GOATOS_E2E_IMAGE built? (make e2e-image-build)"

poll "migrate completed (exit 0)" \
  'test "$(docker inspect -f "{{.State.Status}}:{{.State.ExitCode}}" "$(compose ps -a -q migrate)" 2>/dev/null)" = "exited:0"' 60
poll "API /readyz healthy" "curl -fsS http://127.0.0.1:${api_port}/readyz" 90
poll "kernel-worker running with domain consumer enabled" \
  "compose logs --no-color kernel-worker | grep -F 'domain_consumer_enabled\":true'" 40

# ─────────────────────────────────────────────────────────────────────────────
step "Scenario A — Business chain: input → outbox → Pub/Sub → consumer → obligation → batch → Calendar"

info "Seed authored protocol config (real seed-vaccination-trigger CLI)"
run_cli seed-vaccination-trigger -tenant-id "$tenant_id" >/dev/null 2>&1 || fail "seed-vaccination-trigger failed"
pass "protocol/locations/inventory seeded"

info "Grant the read actor a role (real seed-dev-grant CLI) for the Calendar API read"
run_cli seed-dev-grant -tenant-id "$tenant_id" -user-id "$user_id" -role ceo_internal >/dev/null 2>&1 || fail "seed-dev-grant failed"
pass "role grant seeded"

goat_a="$(psqlq 'select gen_random_uuid()')"
info "Seed input goat $goat_a (age 28d) and emit goat.created via the real outbox path"
insert_goat "$goat_a" 28
emit_goat_created "$goat_a"
pass "goat.created produced through identity outbox path"

# Hop 1: outbox row (produced by the identity txn)
poll "hop1 outbox_messages row exists" \
  "test -n \"\$(psqlq \"select outbox_id from outbox_messages where aggregate_id='$goat_a' and event_type='goat.created' limit 1\")\"" 30
event_a="$(psqlq "select event_id from outbox_messages where aggregate_id='$goat_a' and event_type='goat.created' order by created_at desc limit 1")"
info "event_id=$event_a"

# Hop 2: Pub/Sub publish (kernel-worker fast-lane outbox relay; 1-min cadence)
poll "hop2 outbox relayed & published to Pub/Sub emulator" \
  "test \"\$(psqlq \"select status from outbox_messages where event_id='$event_a'\")\" = published" "$fast_lane_timeout"

# Hop 3: domain-event consumption (kernel-worker continuous consumer)
poll "hop3 domain-event consumed (processed)" \
  "test \"\$(psqlq \"select status from domain_event_processed_events where event_id='$event_a'\")\" = processed" 40

# Hop 4: obligation generation (NewGoatCreatedHandler -> generation service)
poll "hop4 obligation_instances generated" \
  "test \"\$(psqlq \"select count(*) from obligation_instances where target_id='$goat_a'\")\" -gt 0" 40
obl_count_a="$(psqlq "select count(*) from obligation_instances where target_id='$goat_a'")"
info "obligations for goat_a = $obl_count_a"

# Hop 5: operational sweep -> batch. The operational cadence ran its immediate boot catch-up
# BEFORE this obligation existed; bounce the worker to trigger a fresh immediate operational
# catch-up now that the obligation is present (real stage, real work — not a shortcut).
info "Bounce kernel-worker to trigger an immediate operational sweep catch-up"
compose restart kernel-worker >/dev/null 2>&1
poll "hop5 obligation swept into a drive batch (batch_id set)" \
  "test \"\$(psqlq \"select count(*) from obligation_instances where target_id='$goat_a' and batch_id is not null\")\" -gt 0" 40
batch_a="$(psqlq "select batch_id from obligation_instances where target_id='$goat_a' and batch_id is not null limit 1")"
planned_a="$(psqlq "select planned_date from obligation_batches where batch_id='$batch_a'")"
info "batch_id=$batch_a planned_date=$planned_a"

# Hop 6: canonical Calendar API read shows the drive.
poll "kernel-worker healthy after bounce" "curl -fsS http://127.0.0.1:${api_port}/readyz" 40
token="$(run_cli mint-dev-token -tenant-id "$tenant_id" -user-id "$user_id" -ttl 1h 2>/dev/null | tr -d '[:space:]')"
[ -n "$token" ] || fail "mint-dev-token produced no token"
cal_from="$(psqlq "select to_char(current_date - 5, 'YYYY-MM-DD')")"
cal_to="$(psqlq "select to_char(current_date + 35, 'YYYY-MM-DD')")"
cal_json="$(curl -fsS "http://127.0.0.1:${api_port}/calendar/vaccination/events?date_from=${cal_from}&date_to=${cal_to}&limit=20&include_date_markers=true" -H "Authorization: Bearer $token")"
[ -n "$cal_json" ] || fail "hop6 Calendar API returned empty"
drive_count="$(printf '%s' "$cal_json" | python3 -c 'import sys,json; d=json.load(sys.stdin); its=d.get("items") or []; print(sum(1 for e in its if isinstance(e,dict) and e.get("event_type")=="vaccination_drive"))' 2>/dev/null || echo 0)"
[ "$drive_count" -gt 0 ] 2>/dev/null || fail "hop6 Calendar API returned no vaccination_drive events (drives=$drive_count)"
pass "hop6 Calendar API shows $drive_count vaccination_drive event(s)"
printf '%s' "$cal_json" | python3 -c 'import sys,json;d=json.load(sys.stdin);its=[e for e in (d.get("items") or []) if isinstance(e,dict) and e.get("event_type")=="vaccination_drive"];
[print("      drive:",{k:e.get(k) for k in ("event_id","status","due_at","target_count") if k in e}) for e in its[:4]]' 2>/dev/null || true

# Reminder / notification hop — real sweep, honest assertion (see REMINDER_NOTE in header).
step "Scenario A (cont.) — Reminder sweep (real) and remindable-drive verification"
compose restart kernel-worker >/dev/null 2>&1
poll "reminder sweep executed on the operational cadence" \
  "compose logs --no-color kernel-worker | grep -F 'obligation_sweep_stage_reminders'" 60
reminders_queued="$(compose logs --no-color kernel-worker 2>/dev/null | grep -F 'obligation_sweep_stage_reminders' | tail -1 | sed -E 's/.*"queued":([0-9]+).*/\1/')"
notif_rows="$(psqlq "select count(*) from notification_requests")"
if [ "${reminders_queued:-0}" -gt 0 ] 2>/dev/null; then
  poll "notification_requests row produced by reminder sweep" \
    "test \"\$(psqlq 'select count(*) from notification_requests')\" -gt 0" 20
  pass "reminder sweep queued ${reminders_queued} reminder(s); notification_requests=$(psqlq 'select count(*) from notification_requests')"
else
  warn "reminder sweep ran (queued=${reminders_queued:-0}); no notification_requests row yet."
  warn "This is EXPECTED and honest, not a stall: SweepDueReminders only queues a notification"
  warn "when a vaccination DRIVE's due_at <= now()+1h. The generated drive is planned for"
  warn "planned_date=${planned_a} (future), so the reminder is not yet due in this seconds-long"
  warn "run. The remindable drive IS present in the canonical Calendar read above (hop6)."
fi
pass "Scenario A complete: full chain input→…→batch→Calendar verified end-to-end"

# ─────────────────────────────────────────────────────────────────────────────
step "Scenario B — Duplicate delivery / idempotency (P0)"
obl_before="$(psqlq "select count(*) from obligation_instances where target_id='$goat_a'")"
dup_b64="$(psqlq "select translate(encode(convert_to(payload::text,'UTF8'),'base64'), E'\n','') from outbox_messages where event_id='$event_a'")"
dup_type="$(psqlq "select event_type from outbox_messages where event_id='$event_a'")"
dup_outbox="$(psqlq "select outbox_id from outbox_messages where event_id='$event_a'")"
dup_topic="$(psqlq "select topic from outbox_messages where event_id='$event_a'")"
info "Re-publishing the SAME event to the Pub/Sub emulator source topic (real redelivery)"
curl -fsS -X POST "http://127.0.0.1:${pubsub_port}/v1/projects/${pubsub_project}/topics/goatos-local-outbox-events:publish" \
  -H 'Content-Type: application/json' \
  -d "{\"messages\":[{\"data\":\"${dup_b64}\",\"attributes\":{\"event_id\":\"${event_a}\",\"event_type\":\"${dup_type}\",\"tenant_id\":\"${tenant_id}\",\"outbox_id\":\"${dup_outbox}\",\"logical_topic\":\"${dup_topic}\"}}]}" \
  >/dev/null || fail "failed to re-publish duplicate to Pub/Sub emulator"
poll "duplicate delivery skipped by durable event id" \
  "compose logs --no-color kernel-worker | grep -F 'domain_event_duplicate_skipped' | grep -F '$event_a'" 30
sleep 2
obl_after="$(psqlq "select count(*) from obligation_instances where target_id='$goat_a'")"
[ "$obl_before" = "$obl_after" ] || fail "idempotency broken: obligation count $obl_before -> $obl_after on redelivery"
pass "idempotency HELD: obligation count unchanged ($obl_before -> $obl_after) after duplicate delivery"

# ─────────────────────────────────────────────────────────────────────────────
step "Scenario C — Restart catch-up"
info "Stop kernel-worker, create a backlog event while it is DOWN, then restart"
compose stop kernel-worker >/dev/null 2>&1
poll "kernel-worker stopped" "test \"\$(compose ps -a kernel-worker --format '{{.State}}')\" = exited" 20
goat_c="$(psqlq 'select gen_random_uuid()')"
insert_goat "$goat_c" 28
emit_goat_created "$goat_c"    # outbox row is written by the CLI txn even with the worker down
# The outbox row (the ingress) is created by the CLI even with the worker down; capture its
# event_id directly (matching hop1/hop3) so the catch-up assertion needs no fragile join.
poll "backlog outbox row exists (created while worker down)" \
  "test -n \"\$(psqlq \"select outbox_id from outbox_messages where aggregate_id='$goat_c' and event_type='goat.created' limit 1\")\"" 20
event_c="$(psqlq "select event_id from outbox_messages where aggregate_id='$goat_c' and event_type='goat.created' order by created_at desc limit 1")"
backlog_unpub="$(psqlq "select status from outbox_messages where event_id='$event_c'")"
info "backlog event $event_c created while worker down (outbox status=$backlog_unpub)"
compose start kernel-worker >/dev/null 2>&1
poll "kernel-worker restarted (running)" \
  "test \"\$(compose ps kernel-worker --format '{{.State}}')\" = running" 30
poll "restart catch-up: backlog outbox relayed & published after restart" \
  "test \"\$(psqlq \"select status from outbox_messages where event_id='$event_c'\")\" = published" "$fast_lane_timeout"
poll "restart catch-up: backlog event consumed after restart" \
  "test \"\$(psqlq \"select status from domain_event_processed_events where event_id='$event_c'\")\" = processed" 40
poll "restart catch-up: obligation generated for backlog goat" \
  "test \"\$(psqlq \"select count(*) from obligation_instances where target_id='$goat_c'\")\" -gt 0" 40
pass "restart catch-up HELD: worker drained the backlog and converged after restart"

# ─────────────────────────────────────────────────────────────────────────────
step "Scenario D — Two-worker concurrency (advisory locks via pg_try_advisory_lock)"
info "Scale kernel-worker to 2 instances"
compose up -d --scale kernel-worker=2 --no-recreate kernel-worker >/dev/null 2>&1 || fail "compose scale to 2 failed"
poll "two kernel-worker instances running" "test \"\$(compose ps kernel-worker -q | wc -l | tr -d ' ')\" = 2" 30
declare -a conc_goats=()
for _ in 1 2 3; do
  g="$(psqlq 'select gen_random_uuid()')"; conc_goats+=("$g")
  insert_goat "$g" 28
  emit_goat_created "$g"
done
info "Backlog of 3 events driven with 2 workers active; waiting for convergence"
for g in "${conc_goats[@]}"; do
  poll "concurrency: obligation generated for $g" \
    "test \"\$(psqlq \"select count(*) from obligation_instances where target_id='$g'\")\" -gt 0" "$fast_lane_timeout"
done
conc_fail=0
for g in "${conc_goats[@]}"; do
  n="$(psqlq "select count(*) from obligation_instances where target_id='$g'")"
  if [ "$n" != "1" ]; then
    printf "${RED}  duplicate side effect: goat %s has %s obligations (expected 1)${NC}\n" "$g" "$n" >&2
    conc_fail=1
  else
    info "goat $g processed exactly once (1 obligation)"
  fi
done
[ "$conc_fail" = "0" ] || fail "concurrency broken: an item was processed more than once under 2 workers"
proc_dupes="$(psqlq "select count(*) from (select event_id, count(*) c from domain_event_processed_events group by event_id having count(*)>1) d")"
[ "${proc_dupes:-0}" = "0" ] || fail "concurrency broken: domain_event_processed_events has ${proc_dupes} duplicated event_id(s)"
pass "two-worker concurrency HELD: each backlog item executed exactly once; 0 duplicated processed events"
info "advisory-lock mechanism: worker.AcquireStageLock uses pg_try_advisory_lock (non-blocking);"
info "the losing instance skips the stage (stage_lock_already_held @ DEBUG). Outcome proof above."

# ─────────────────────────────────────────────────────────────────────────────
step "e2e-business-chain: ALL SCENARIOS PASSED"
echo "  A business chain : outbox → Pub/Sub → consumer → obligation → batch → Calendar drive read"
echo "  B idempotency    : duplicate Pub/Sub delivery skipped; obligation count unchanged"
echo "  C restart        : backlog created while worker down, drained + converged after restart"
echo "  D concurrency    : 2 workers, each backlog item executed once (advisory locks)"
echo "  reminder note    : real reminder sweep ran; notification row is drive-due-window gated (see logs above)"
echo ""
echo "  totals: obligations=$(psqlq 'select count(*) from obligation_instances') processed_events=$(psqlq 'select count(*) from domain_event_processed_events') batches=$(psqlq 'select count(*) from obligation_batches')"
exit 0
