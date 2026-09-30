#!/usr/bin/env bash
# install-stg-push-guard.sh — installs the machine-local pre-push guards without
# replacing an existing hook. Scoped to the vgoats/goatos origin remote. Two guards
# are chained into one pre-push hook:
#   1. staging promotion guard (check-stg-promotion.mjs): blocks every direct update to
#      refs/heads/stg. Staging promotion happens in GitHub by merging a same-repository
#      main -> stg pull request.
#   2. exact-SHA local-CI evidence guard (check-local-ci-evidence.mjs): blocks every update
#      to refs/heads/main unless `make ci-local` recorded a SHA-bound receipt for the exact
#      commit. Scoped receipts are revalidated against the exact remote-main base and full
#      classifier-selected job set. See docs/runbooks/local-release-evidence.md.
#   3. admin-web push gate (tools/ci/admin-web-push-gate.sh --pre-push): on EVERY push to ANY
#      branch whose commits change an admin-web input, runs design:guard, typecheck, npm test,
#      next build and the admin-web visual gate, then writes + publishes the gate receipt.
#
# SHIM, NOT COPIES (J1B P0-2, 2026-09-30). Every worktree shares <git-common-dir>/hooks, and this
# installer used to COPY the installing branch's hook + guards there, so whichever branch last ran
# `make ai-setup` decided what EVERY branch's push ran (review/pr-307's old stg/main-only hook
# silently replaced PR #294's admin-web gate; 2d43dee4a reached the PR with no visual pass). Now it
# installs tools/agent-hooks/pre-push.shim as <hooks>/pre-push. The shim holds no gate logic: on each
# push it runs the pushing worktree's own committed tools/agent-hooks/pre-push.hook (+ the files in
# tools/agent-hooks/pre-push.bundle), or, for a branch without one, the legacy stg/main guard. Each
# branch enforces its own rules; installing from any branch never downgrades another branch.
# Beside the shim it leaves snapshots of the stg/main guards (goatos-check-*.mjs + step-input-digest)
# for the legacy fallback when a work tree has none of its own. Copies left by the old installer
# (goatos-admin-web-*.sh, goatos-skip-ledger.sh) are removed: nothing runs them any more.
# tools/ci/check-push-hook-freshness.sh proves the shim is installed and that the current branch's
# hook runs, with real test pushes.
# Historically this file only installed guard 1; the name is kept so `make ai-setup` /
# `make stg-promotion-guard-install` / `make push-hooks-install` keep working.
set -euo pipefail

repo="$(git rev-parse --show-toplevel)"
shim_src="$repo/tools/agent-hooks/pre-push.shim"
for g in "$shim_src" "$repo/tools/agent-hooks/pre-push.hook" "$repo/tools/agent-hooks/pre-push.bundle" \
  "$repo/tools/ci/check-stg-promotion.mjs" "$repo/tools/ci/check-local-ci-evidence.mjs" "$repo/tools/ci/step-input-digest.mjs"; do
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
  # The SHARED hooks dir of every worktree (a linked worktree's --git-path hooks resolves here too).
  hooks_dir="$(cd "$(git rev-parse --git-common-dir)" && pwd -P)/hooks"
fi
mkdir -p "$hooks_dir"

hook="$hooks_dir/pre-push"
prior="$hooks_dir/pre-push.before-goatos-stg-guard"

# Ours: the shim, or a pre-shim GoatOS hook (current and legacy stg-only markers). Anything else is
# foreign and is preserved ONCE as the prior hook, which the shim chains to.
is_ours() { grep -qE 'GOATOS_PUSH_SHIM|GOATOS_PUSH_GUARDS|GOATOS_STG_PROMOTION_GUARD' "$1" 2>/dev/null; }
if [ -e "$hook" ] && ! is_ours "$hook"; then
  if [ -e "$prior" ] && ! is_ours "$prior"; then
    echo "refusing to replace $hook: preserved hook already exists at $prior" >&2
    exit 1
  fi
  mv "$hook" "$prior"
fi
# An older branch's installer chains a shim it found as ITS prior; with the shim back in front that
# prior would only re-run the shim, so drop it (a foreign prior is kept).
if [ -e "$prior" ] && is_ours "$prior"; then rm -f "$prior"; fi

# Legacy-fallback snapshots (used only for a work tree with no tools/ci guards of its own).
cp "$repo/tools/ci/check-stg-promotion.mjs" "$hooks_dir/goatos-check-stg-promotion.mjs"
cp "$repo/tools/ci/check-local-ci-evidence.mjs" "$hooks_dir/goatos-check-local-ci-evidence.mjs"
# The evidence guard IMPORTS ./step-input-digest.mjs: install it beside the snapshot.
cp "$repo/tools/ci/step-input-digest.mjs" "$hooks_dir/step-input-digest.mjs"
chmod 0755 "$hooks_dir/goatos-check-stg-promotion.mjs" "$hooks_dir/goatos-check-local-ci-evidence.mjs"
# Copies from the pre-shim installer: branch-owned now, staged per push from the branch itself.
rm -f "$hooks_dir/goatos-admin-web-push-gate.sh" "$hooks_dir/goatos-admin-web-visual-gate.sh" "$hooks_dir/goatos-skip-ledger.sh"

tmp="$hook.goatos.$$"
cp "$shim_src" "$tmp"
chmod 0755 "$tmp"
mv -f "$tmp" "$hook"

echo "Installed the GoatOS pre-push shim: $hook runs each worktree's own tools/agent-hooks/pre-push.hook (legacy stg/main guard for branches without one)"
bash "$(dirname "$0")/../ci/crg-worktree-hook.sh"
