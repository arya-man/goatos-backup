#!/usr/bin/env bash
set -euo pipefail

# Self-test for the local GCP kernel parity guard.
#
# The guard renders `compose.local-kernel.yml` with a RENDER-ONLY placeholder for
# GOATOS_TENANT_ID (otherwise `docker compose config` aborts on the `:?`
# required-var form and the lint checks nothing — BUG-038). Because the guard now
# supplies that value itself, the runtime fail-fast can no longer rely on this
# lint aborting, so the guard also asserts the `:?` form is still in the source.
#
# Adversarial fixture: replace `:?` with a plain `:-` default. That renders
# perfectly and would silently let `docker compose up` sweep a fabricated tenant.
# The guard MUST fail on it.

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
guard="$repo/tools/agent-hooks/check-local-gcp-kernel-parity.sh"
real_compose="$repo/compose.local-kernel.yml"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

run_case() {
  local name="$1" expected="$2" compose="$3" actual
  # Deliberately unset GOATOS_TENANT_ID: the guard must not depend on the
  # environment of whoever runs CI.
  if env -u GOATOS_TENANT_ID GOATOS_LOCAL_KERNEL_COMPOSE_FILE="$compose" \
    bash "$guard" >/dev/null 2>&1; then
    actual=pass
  else
    actual=fail
  fi
  if [[ "$actual" != "$expected" ]]; then
    printf 'local-gcp-kernel-parity self-test %s: got %s, want %s\n' "$name" "$actual" "$expected" >&2
    exit 1
  fi
}

# 1. The committed compose file must PASS with no GOATOS_TENANT_ID exported.
run_case committed_compose_no_env pass "$real_compose"

# 2. Adversarial: required-var `:?` downgraded to a plain default `:-`.
plain_default="$tmp/compose.plain-default.yml"
sed -E 's/GOATOS_TENANT_ID: \$\{GOATOS_TENANT_ID:\?[^}]*\}/GOATOS_TENANT_ID: ${GOATOS_TENANT_ID:-00000000-0000-4000-8000-000000000001}/' \
  "$real_compose" > "$plain_default"
if ! grep -Fq 'GOATOS_TENANT_ID:-00000000-0000-4000-8000-000000000001}' "$plain_default"; then
  printf 'local-gcp-kernel-parity self-test: fixture build failed (no :- downgrade applied)\n' >&2
  exit 1
fi
if grep -Fq 'GOATOS_TENANT_ID:?' "$plain_default"; then
  printf 'local-gcp-kernel-parity self-test: fixture build failed (:? form still present)\n' >&2
  exit 1
fi
run_case required_var_downgraded_to_default fail "$plain_default"

# 3. Adversarial: interpolation removed entirely, tenant hardcoded.
hardcoded="$tmp/compose.hardcoded.yml"
sed -E 's/GOATOS_TENANT_ID: \$\{GOATOS_TENANT_ID:\?[^}]*\}/GOATOS_TENANT_ID: 00000000-0000-4000-8000-000000000001/' \
  "$real_compose" > "$hardcoded"
run_case tenant_hardcoded fail "$hardcoded"

# 4. Adversarial sibling-block fixture: the `:?` form is present, but on a
# DIFFERENT service. A block-unaware grep would false-green here.
sibling="$tmp/compose.sibling-block.yml"
python3 - "$real_compose" "$sibling" <<'PY'
import re, sys
src, dst = sys.argv[1], sys.argv[2]
text = open(src).read()
# kernel-workers loses the required form...
text = re.sub(
    r'GOATOS_TENANT_ID: \$\{GOATOS_TENANT_ID:\?[^}]*\}',
    'GOATOS_TENANT_ID: ${GOATOS_TENANT_ID:-00000000-0000-4000-8000-000000000001}',
    text,
)
# ...while a sibling service gains it.
text = text.replace(
    '      GOATOS_LOCAL_MAINTENANCE_INTERVAL_SECONDS:',
    '      GOATOS_TENANT_ID: ${GOATOS_TENANT_ID:?set GOATOS_TENANT_ID}\n'
    '      GOATOS_LOCAL_MAINTENANCE_INTERVAL_SECONDS:',
    1,
)
open(dst, 'w').write(text)
PY
grep -Fq 'GOATOS_TENANT_ID:?' "$sibling" || {
  printf 'local-gcp-kernel-parity self-test: sibling fixture build failed\n' >&2
  exit 1
}
run_case required_var_only_on_sibling_service fail "$sibling"

# 5. Non-adversarial control: a topology break must still be caught, proving the
# earlier failures are not just the guard crashing on every fixture.
missing_service="$tmp/compose.missing-service.yml"
python3 - "$real_compose" "$missing_service" <<'PY'
import sys
src, dst = sys.argv[1], sys.argv[2]
lines = open(src).read().splitlines(keepends=True)
out, skipping = [], False
for line in lines:
    if line.startswith('  domain-event-consumer:'):
        skipping = True
        continue
    if skipping:
        if line.startswith('  ') and not line.startswith('    ') and line.strip():
            skipping = False
        else:
            continue
    out.append(line)
open(dst, 'w').writelines(out)
PY
run_case missing_domain_event_consumer fail "$missing_service"

printf 'local-gcp-kernel-parity self-test: ok\n'
