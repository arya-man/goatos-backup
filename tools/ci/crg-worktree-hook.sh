#!/usr/bin/env bash
# Make code-review-graph's pre-commit hook run in linked worktrees by default.
# PR branches are worktrees; `code-review-graph install` skips them unless
# CRG_HOOK_WORKTREES=1. We flip the default so CRG_HOOK_WORKTREES=0 opts out.
# Idempotent; a no-op when the CRG hook is not installed.
set -euo pipefail
hook="${1:-$(git rev-parse --git-common-dir)/hooks/pre-commit}"
[ -f "$hook" ] || exit 0
grep -q 'CRG_HOOK_WORKTREES" != "1"' "$hook" || exit 0
tmp="$hook.crg.$$"
sed 's/\[ "\$CRG_HOOK_WORKTREES" != "1" \]/[ "${CRG_HOOK_WORKTREES:-1}" != "1" ]/' "$hook" >"$tmp"
sh -n "$tmp"
cat "$tmp" >"$hook" && rm -f "$tmp"
echo "code-review-graph: pre-commit hook now runs in linked worktrees (CRG_HOOK_WORKTREES=0 opts out)"
