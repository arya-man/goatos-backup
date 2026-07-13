#!/usr/bin/env bash
# Installs a machine-local pre-push guard without replacing an existing hook.
# The installed guard is scoped to the vgoats/goatos origin remote and blocks
# every direct update to refs/heads/stg. Staging promotion happens in GitHub by
# merging a same-repository main -> stg pull request.
set -euo pipefail

repo="$(git rev-parse --show-toplevel)"
source_guard="$repo/tools/ci/check-stg-promotion.mjs"
if [ ! -f "$source_guard" ]; then
  echo "missing staging promotion guard: $source_guard" >&2
  exit 1
fi

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
installed_guard="$hooks_dir/goatos-check-stg-promotion.mjs"
marker="GOATOS_STG_PROMOTION_GUARD"

if [ -e "$hook" ] && ! grep -q "$marker" "$hook"; then
  if [ -e "$prior" ]; then
    echo "refusing to replace $hook: preserved hook already exists at $prior" >&2
    exit 1
  fi
  mv "$hook" "$prior"
fi

cp "$source_guard" "$installed_guard"
chmod 0755 "$installed_guard"

cat >"$hook" <<'HOOK'
#!/usr/bin/env bash
# GOATOS_STG_PROMOTION_GUARD
set -euo pipefail

hooks_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
prior="$hooks_dir/pre-push.before-goatos-stg-guard"
guard="$hooks_dir/goatos-check-stg-promotion.mjs"
payload="$(mktemp "${TMPDIR:-/tmp}/goatos-pre-push.XXXXXX")"
trap 'rm -f "$payload"' EXIT
cat >"$payload"

if [ -x "$prior" ]; then
  "$prior" "$@" <"$payload"
fi

origin="$(git remote get-url origin 2>/dev/null || true)"
case "$origin" in
  git@github.com:vgoats/goatos.git|ssh://git@github.com/vgoats/goatos.git|https://github.com/vgoats/goatos.git|https://github.com/vgoats/goatos)
    node "$guard" --pre-push <"$payload"
    ;;
esac
HOOK
chmod 0755 "$hook"

echo "Installed GoatOS direct-stg-push block: $hook"
