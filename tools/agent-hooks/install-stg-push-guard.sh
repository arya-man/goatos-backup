#!/usr/bin/env bash
# install-stg-push-guard.sh — installs the machine-local pre-push guards without
# replacing an existing hook. Scoped to the vgoats/goatos origin remote. Two guards
# are chained into one pre-push hook:
#   1. staging promotion guard (check-stg-promotion.mjs): blocks every direct update to
#      refs/heads/stg. Staging promotion happens in GitHub by merging a same-repository
#      main -> stg pull request.
#   2. exact-SHA local-CI evidence guard (check-local-ci-evidence.mjs): blocks every update
#      to refs/heads/main unless a FULL green `make ci-local` recorded a SHA-bound receipt
#      for the exact commit being pushed. See docs/runbooks/local-release-evidence.md.
# Historically this file only installed guard 1; the name is kept so `make ai-setup` /
# `make stg-promotion-guard-install` keep working, but it is now a general push-guard installer.
set -euo pipefail

repo="$(git rev-parse --show-toplevel)"
stg_guard="$repo/tools/ci/check-stg-promotion.mjs"
evidence_guard="$repo/tools/ci/check-local-ci-evidence.mjs"
for g in "$stg_guard" "$evidence_guard"; do
  if [ ! -f "$g" ]; then
    echo "missing push guard: $g" >&2
    exit 1
  fi
done

hooks_path="$(git config --path core.hooksPath 2>/dev/null || true)"
if [ -n "$hooks_path" ]; then
  case "$hooks_path" in
    /*) hooks_dir="$hooks_path" ;;
    *) hooks_dir="$repo/$hooks_path" ;;
  esac
else
  hooks_dir="$(git rev-parse --git-path hooks)"
fi
mkdir -p "$hooks_dir"

hook="$hooks_dir/pre-push"
prior="$hooks_dir/pre-push.before-goatos-stg-guard"
installed_stg_guard="$hooks_dir/goatos-check-stg-promotion.mjs"
installed_evidence_guard="$hooks_dir/goatos-check-local-ci-evidence.mjs"
marker="GOATOS_PUSH_GUARDS"

# Preserve a foreign (non-marker) pre-push hook once, so we chain rather than clobber.
# Recognize both the current and the legacy stg-only marker as ours.
if [ -e "$hook" ] && ! grep -qE "$marker|GOATOS_STG_PROMOTION_GUARD" "$hook"; then
  if [ -e "$prior" ]; then
    echo "refusing to replace $hook: preserved hook already exists at $prior" >&2
    exit 1
  fi
  mv "$hook" "$prior"
fi

cp "$stg_guard" "$installed_stg_guard"
cp "$evidence_guard" "$installed_evidence_guard"
chmod 0755 "$installed_stg_guard" "$installed_evidence_guard"

cat >"$hook" <<'HOOK'
#!/usr/bin/env bash
# GOATOS_PUSH_GUARDS
set -euo pipefail

hooks_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
prior="$hooks_dir/pre-push.before-goatos-stg-guard"
stg_guard="$hooks_dir/goatos-check-stg-promotion.mjs"
evidence_guard="$hooks_dir/goatos-check-local-ci-evidence.mjs"
payload="$(mktemp "${TMPDIR:-/tmp}/goatos-pre-push.XXXXXX")"
trap 'rm -f "$payload"' EXIT
cat >"$payload"

if [ -x "$prior" ]; then
  "$prior" "$@" <"$payload"
fi

origin="$(git remote get-url origin 2>/dev/null || true)"
case "$origin" in
  git@github.com:vgoats/goatos.git|ssh://git@github.com/vgoats/goatos.git|https://github.com/vgoats/goatos.git|https://github.com/vgoats/goatos)
    node "$stg_guard" --pre-push <"$payload"
    node "$evidence_guard" --pre-push <"$payload"
    ;;
esac
HOOK
chmod 0755 "$hook"

echo "Installed GoatOS push guards (direct-stg block + exact-SHA main-CI evidence): $hook"
