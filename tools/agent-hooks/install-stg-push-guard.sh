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
#      next build and the admin-web visual gate (tools/ci/admin-web-visual-gate.sh --pre-push:
#      5 shell routes + touched routes, scan + interactions + skeleton-on-touched). Lane passes
#      are reused per admin-web input tree. No silent skip: a missing guard file refuses the push;
#      GOATOS_SKIP_ADMIN_WEB_VISUAL_GATE needs GOATOS_SKIP_REASON and lands in the skip ledger
#      (tools/ci/goatos-skip-ledger.sh).
# The hook body is tools/agent-hooks/pre-push.hook, copied verbatim; check-push-hook-freshness.sh
# fails local CI while any installed file differs from its repo source.
# Historically this file only installed guard 1; the name is kept so `make ai-setup` /
# `make stg-promotion-guard-install` keep working, but it is now a general push-guard installer.
set -euo pipefail

repo="$(git rev-parse --show-toplevel)"
stg_guard="$repo/tools/ci/check-stg-promotion.mjs"
evidence_guard="$repo/tools/ci/check-local-ci-evidence.mjs"
hook_src="$repo/tools/agent-hooks/pre-push.hook"
for g in "$stg_guard" "$evidence_guard" "$hook_src" "$repo/tools/ci/step-input-digest.mjs" \
  "$repo/tools/ci/admin-web-push-gate.sh" "$repo/tools/ci/admin-web-visual-gate.sh" "$repo/tools/ci/goatos-skip-ledger.sh"; do
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
# The evidence guard IMPORTS ./step-input-digest.mjs. It is installed beside it under its own
# name, or every push dies with ERR_MODULE_NOT_FOUND (2026-09-26: the guard gained the import and
# this installer kept copying only the guard, so re-running it broke pushes on that machine).
installed_digest_helper="$hooks_dir/step-input-digest.mjs"
installed_visual_gate="$hooks_dir/goatos-admin-web-visual-gate.sh"
installed_push_gate="$hooks_dir/goatos-admin-web-push-gate.sh"
installed_skip_ledger="$hooks_dir/goatos-skip-ledger.sh"
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
cp "$repo/tools/ci/step-input-digest.mjs" "$installed_digest_helper"
cp "$repo/tools/ci/admin-web-visual-gate.sh" "$installed_visual_gate"
cp "$repo/tools/ci/admin-web-push-gate.sh" "$installed_push_gate"
cp "$repo/tools/ci/goatos-skip-ledger.sh" "$installed_skip_ledger"
chmod 0755 "$installed_stg_guard" "$installed_evidence_guard" "$installed_visual_gate" "$installed_push_gate"

cp "$hook_src" "$hook"
chmod 0755 "$hook"

echo "Installed GoatOS push guards (direct-stg block + exact-SHA main-CI evidence + admin-web push gate on every branch): $hook"
bash "$(dirname "$0")/../ci/crg-worktree-hook.sh"
