#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
compose_file="$repo_root/compose.local-kernel.yml"

docker compose -f "$compose_file" config --quiet

config="$(docker compose -f "$compose_file" config)"
for service in postgres migrate pubsub pubsub-bootstrap api outbox-relay domain-event-consumer kernel-workers kernel-maintenance; do
  grep -Eq "^  ${service}:" <<<"$config" || {
    echo "local GCP kernel parity guard: missing service $service" >&2
    exit 1
  }
done

grep -Fq 'GOATOS_OUTBOX_PUBLISHER: pubsub' <<<"$config" || {
  echo "local GCP kernel parity guard: relay is not configured for Pub/Sub" >&2
  exit 1
}
grep -Fq 'PUBSUB_EMULATOR_HOST: pubsub:8085' <<<"$config" || {
  echo "local GCP kernel parity guard: backend does not target the emulator" >&2
  exit 1
}
if grep -Eq 'GOATOS_OUTBOX_PUBLISHER: (eventbus|logging)|GOATOS_OUTBOX_ALLOW_NONDURABLE' <<<"$config"; then
  echo "local GCP kernel parity guard: non-durable publisher leaked into parity stack" >&2
  exit 1
fi

grep -Fq 'goatos-local-outbox-events-dlq' "$repo_root/tools/dev/pubsub-emulator-bootstrap.sh"

echo "local GCP kernel parity guard: PASS"
