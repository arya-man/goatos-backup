#!/usr/bin/env bash
set -euo pipefail

# local GCP kernel parity guard
#
# Static lint over the rendered `compose.local-kernel.yml` topology: every kernel
# service must exist, the relay must publish to Pub/Sub, the backend must target
# the emulator, and no non-durable publisher may leak into the parity stack.
#
# RENDER-ONLY TENANT PLACEHOLDER
# `compose.local-kernel.yml` deliberately declares the kernel-worker tenant as a
# hard-required interpolation (`${GOATOS_TENANT_ID:?...}`) so a real
# `docker compose up` fails fast instead of silently sweeping a fabricated
# tenant. `docker compose config` evaluates `:?` at RENDER time, so this lint
# aborted before checking anything on every machine that has not exported the
# variable (nothing in the Makefile, tools/, .github/, or any committed env file
# does) — which made `make ci-local` unsatisfiable repo-wide.
# The guard therefore supplies its OWN placeholder for the render only; it never
# starts, seeds, or sweeps anything. To keep the runtime fail-fast a real
# invariant rather than something accidentally load-bearing on this lint
# aborting, the guard additionally asserts against the compose SOURCE text that
# the `:?` required-var form is still present on the kernel-workers tenant.
#
# Blind spots (self-test: check-local-gcp-kernel-parity.test.sh):
# - Text/topology lint only. It does not start the stack, so it cannot prove the
#   emulator/relay/consumer work end to end; that is
#   `tools/dev/local-gcp-kernel-parity-smoke.sh`.
# - The `:?` assertion is a source-text check scoped to the `kernel-workers`
#   service block. A `GOATOS_TENANT_ID: ${GOATOS_TENANT_ID:?...}` declared only
#   in another service or anchor would not satisfy it, and a required-var
#   contract expressed by some future non-textual mechanism is not recognised.
# - It asserts the form of the requirement, not that the value an operator later
#   supplies is a real tenant; the kernel-worker binary's own empty-value
#   fail-fast remains the runtime check.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
compose_file="${GOATOS_LOCAL_KERNEL_COMPOSE_FILE:-$repo_root/compose.local-kernel.yml}"

# Render-only placeholder. Never used to run, seed, or sweep anything.
render_tenant_id="${GOATOS_TENANT_ID:-00000000-0000-4000-8000-0000000f0038}"

kernel_workers_block="$(awk '
  /^  kernel-workers:$/ { inblock = 1; next }
  inblock && /^  [A-Za-z0-9_.-]+:$/ { inblock = 0 }
  inblock { print }
' "$compose_file")"

if [[ -z "$kernel_workers_block" ]]; then
  echo "local GCP kernel parity guard: could not locate the kernel-workers service block in $compose_file" >&2
  exit 1
fi

if ! grep -Eq 'GOATOS_TENANT_ID: \$\{GOATOS_TENANT_ID:\?' <<<"$kernel_workers_block"; then
  echo "local GCP kernel parity guard: kernel-workers GOATOS_TENANT_ID must stay a required interpolation (\${GOATOS_TENANT_ID:?...}) so the worker fails fast instead of sweeping a fabricated tenant" >&2
  exit 1
fi

GOATOS_TENANT_ID="$render_tenant_id" docker compose -f "$compose_file" config --quiet

config="$(GOATOS_TENANT_ID="$render_tenant_id" docker compose -f "$compose_file" config)"
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
grep -Fq 'domain_event_duplicate_skipped' "$repo_root/tools/dev/local-gcp-kernel-parity-smoke.sh"

echo "local GCP kernel parity guard: PASS"
