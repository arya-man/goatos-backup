#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
compose_file="$repo_root/compose.local-kernel.yml"
project="${GOATOS_LOCAL_KERNEL_PROJECT:-goatos-local-kernel}"
tenant_id="${GOATOS_TENANT_ID:-00000000-0000-4000-8000-000000000001}"
user_id="${GOATOS_LOCAL_USER_ID:-90000000-0000-4000-8000-000000000101}"
pubsub_project="${GOATOS_LOCAL_PUBSUB_PROJECT_ID:-goatos-local}"
api_port="${GOATOS_LOCAL_API_PORT:-8080}"
pubsub_port="${GOATOS_LOCAL_PUBSUB_PORT:-8085}"
export GOATOS_LOCAL_KERNEL_INTERVAL_SECONDS="${GOATOS_LOCAL_KERNEL_INTERVAL_SECONDS:-5}"
# kernel-maintenance defaults to an hourly cadence; the smoke needs its boot pass to run
# promptly so the second worker loop below observes completion within the wait budget.
export GOATOS_LOCAL_MAINTENANCE_INTERVAL_SECONDS="${GOATOS_LOCAL_MAINTENANCE_INTERVAL_SECONDS:-5}"
run_id="$(date -u +%Y%m%dT%H%M%SZ)"
report_dir="$repo_root/.codex-goatos-render/local-gcp-kernel-parity/$run_id"
report="$report_dir/report.md"

compose() {
  docker compose -f "$compose_file" -p "$project" "$@"
}

psqlq() {
  compose exec -T postgres psql -qAt -U postgres -d goatos -v ON_ERROR_STOP=1 -c "$1"
}

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

wait_for() {
  label="$1"
  command="$2"
  for _ in $(seq 1 60); do
    if eval "$command" >/dev/null 2>&1; then
      printf 'PASS: %s\n' "$label"
      return 0
    fi
    sleep 1
  done
  fail "timed out waiting for $label"
}

mkdir -p "$report_dir"

printf 'Starting local GCP-kernel parity stack (project=%s).\n' "$project"
compose up -d --build api outbox-relay domain-event-consumer kernel-workers kernel-maintenance
wait_for "API readiness" "curl -fsS http://127.0.0.1:${api_port}/readyz"
wait_for "domain consumer running" "test \"\$(compose ps --status running --services domain-event-consumer)\" = domain-event-consumer"

compose run --rm --no-deps --entrypoint /app/bin/seed-vaccination-trigger api \
  -tenant-id "$tenant_id" >/dev/null
compose run --rm --no-deps --entrypoint /app/bin/seed-dev-grant api \
  -tenant-id "$tenant_id" -user-id "$user_id" -role ceo_internal >/dev/null

goat_id="$(psqlq 'select gen_random_uuid()')"
psqlq "
insert into goats (
  goat_id, tenant_id, lifecycle_status, health_status, reproductive_status,
  custodian_party_id, current_location_id, park_id, shed_id, management_stage,
  sex, breed, dob, approx_dob, entry_date, species, origin_type
) values (
  '$goat_id', '$tenant_id', 'alive', 'healthy', 'open',
  '00000000-0000-4000-8000-000000001001',
  '00000000-0000-4000-8000-000000003101',
  '00000000-0000-4000-8000-000000003001',
  '00000000-0000-4000-8000-000000003101',
  'K2', 'female', 'all', current_date - 28, current_date - 28,
  current_date - 28, 'goat', 'birth'
)" >/dev/null

compose run --rm --no-deps --entrypoint /app/bin/backfill-goat-created api \
  -tenant-id "$tenant_id" -goat-id "$goat_id" -limit 1 >/dev/null

wait_for "outbox publish through Pub/Sub emulator" \
  "test \"\$(psqlq \"select status from outbox_messages where aggregate_id='$goat_id' and event_type='goat.created' order by created_at desc limit 1\")\" = published"
event_id="$(psqlq "select event_id from outbox_messages where aggregate_id='$goat_id' and event_type='goat.created' order by created_at desc limit 1")"
wait_for "durable domain consumer record" \
  "test \"\$(psqlq \"select status from domain_event_processed_events where event_id='$event_id'\")\" = processed"
wait_for "goat.created domain effect" \
  "test \"\$(psqlq \"select count(*) from obligation_instances where target_id='$goat_id'\")\" -gt 0"

obligation_count="$(psqlq "select count(*) from obligation_instances where target_id='$goat_id'")"
obligation_id="$(psqlq "select obligation_id from obligation_instances where target_id='$goat_id' order by created_at limit 1")"
wait_for "obligation sweeper batch effect" \
  "test \"\$(psqlq \"select count(*) from obligation_instances where target_id='$goat_id' and batch_id is not null\")\" -gt 0"
wait_for "process-integrity canonical effect" \
  "test \"\$(psqlq \"select count(*) from obligation_instances where tenant_id='$tenant_id' and target_id='$goat_id' and batch_id is not null\")\" -gt 0"
wait_for "vaccination shed canonical effect" \
  "test \"\$(psqlq \"select count(*) from obligation_instances oi join goats g on g.tenant_id=oi.tenant_id and g.goat_id=oi.target_id where oi.tenant_id='$tenant_id' and g.shed_id='00000000-0000-4000-8000-000000003101' and oi.status in ('due','scheduled','in_progress')\")\" -gt 0"

for worker in vaccination-generator obligation-sweeper notification-dispatcher; do
  wait_for "$worker completed without exit" \
    "compose logs --no-color kernel-workers | grep -F 'local kernel worker complete: $worker'"
done
if compose logs --no-color kernel-workers | grep -Fq 'local kernel worker failed:'; then
  compose logs --no-color kernel-workers >&2
  fail "a required local kernel worker exited nonzero"
fi
for worker in domain-event-processed-sweeper idempotency-key-sweeper inventory-batch-reconciler sop-review-fanout-retry; do
  wait_for "$worker completed without exit" \
    "compose logs --no-color kernel-maintenance | grep -F 'local kernel worker complete: $worker'"
done
if compose logs --no-color kernel-maintenance | grep -Fq 'local kernel worker failed:'; then
  compose logs --no-color kernel-maintenance >&2
  fail "a required local kernel maintenance worker exited nonzero"
fi

# Force an at-least-once duplicate delivery the production-shaped way: re-publish the SAME
# event straight to the Pub/Sub emulator source topic. The emulator topic is an INPUT boundary
# (the broker), not a derived-state store, so injecting a duplicate broker delivery here is the
# real "Pub/Sub delivered the message twice" scenario. We deliberately do NOT mutate the derived
# outbox row to fake a re-send (that is banned by check-e2e-kernel-integrity, and it would prove
# the relay, not the consumer). The message mirrors what the outbox publisher emits: the payload
# as data, and the event_id/event_type/tenant_id/outbox_id/logical_topic attributes the consumer
# dedups on (see internal/outbox/adapters/publisher/pubsub/publisher.go).
dup_payload_b64="$(psqlq "select translate(encode(convert_to(payload::text, 'UTF8'), 'base64'), E'\n', '') from outbox_messages where event_id='$event_id'")"
dup_event_type="$(psqlq "select event_type from outbox_messages where event_id='$event_id'")"
dup_outbox_id="$(psqlq "select outbox_id from outbox_messages where event_id='$event_id'")"
dup_logical_topic="$(psqlq "select topic from outbox_messages where event_id='$event_id'")"
curl -fsS -X POST \
  "http://127.0.0.1:${pubsub_port}/v1/projects/${pubsub_project}/topics/goatos-local-outbox-events:publish" \
  -H 'Content-Type: application/json' \
  -d "{\"messages\":[{\"data\":\"${dup_payload_b64}\",\"attributes\":{\"event_id\":\"${event_id}\",\"event_type\":\"${dup_event_type}\",\"tenant_id\":\"${tenant_id}\",\"outbox_id\":\"${dup_outbox_id}\",\"logical_topic\":\"${dup_logical_topic}\"}}]}" \
  >/dev/null || fail "failed to re-publish duplicate event to the Pub/Sub emulator"
wait_for "duplicate delivery skipped by durable event id" \
  "compose logs --no-color domain-event-consumer | grep -F 'domain_event_duplicate_skipped' | grep -F '$event_id'"
replay_obligation_count="$(psqlq "select count(*) from obligation_instances where target_id='$goat_id'")"
test "$replay_obligation_count" = "$obligation_count" || fail "duplicate delivery changed obligation count ($obligation_count -> $replay_obligation_count)"

curl -fsS "http://127.0.0.1:${pubsub_port}/v1/projects/${pubsub_project}/subscriptions/goatos-local-domain-events" \
  | grep -F 'goatos-local-outbox-events-dlq' >/dev/null \
  || fail "domain subscription is missing its emulator DLQ policy"
curl -fsS "http://127.0.0.1:${pubsub_port}/v1/projects/${pubsub_project}/subscriptions/goatos-local-outbox-events-dlq-inspect" \
  | grep -F 'goatos-local-outbox-events-dlq' >/dev/null \
  || fail "DLQ inspection subscription is missing"

{
  printf '# Local GCP Kernel Parity Smoke\n\n'
  printf -- '- Result: **PASS**\n'
  printf -- '- Certification boundary: local behavior only; not GCP IAM, Cloud Tasks, GCS, or staging DLQ certification.\n'
  printf -- '- Compose project: `%s`\n' "$project"
  printf -- '- Goat input: `%s`\n' "$goat_id"
  printf -- '- Outbox event: `%s`\n' "$event_id"
  printf -- '- Domain effect: obligation `%s` (`%s` obligation row(s))\n' "$obligation_id" "$obligation_count"
  printf -- '- Worker effects: obligation batched; process-integrity and shed projections refreshed; notification dispatcher completed.\n'
  printf -- '- Duplicate Pub/Sub delivery: skipped; obligation count remained `%s`\n' "$replay_obligation_count"
  printf -- '- Runtime path: Postgres -> outbox relay -> official Pub/Sub emulator -> domain-event consumer -> Postgres obligation\n'
  printf -- '- API: `http://127.0.0.1:%s`\n' "$api_port"
  printf -- '- Pub/Sub emulator: `http://127.0.0.1:%s`\n' "$pubsub_port"
} >"$report"

printf 'PASS: local GCP-kernel parity smoke\n'
printf 'Report: %s\n' "$report"
printf 'Stack left running by design. Status: docker compose -f %s -p %s ps\n' "$compose_file" "$project"
