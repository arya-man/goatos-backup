#!/usr/bin/env bash
# Goat OS work belongs under ~/mesha (maintainer rule). Refuses to run land-main,
# ci-local, or ai-setup from a clone/worktree outside $GOATOS_WORKSPACE_ROOT
# (default $HOME/mesha): clones elsewhere escaped the shared landing lock and
# slowed landings. GOATOS_ALLOW_OUTSIDE_WORKSPACE=1 is for the self-hosted
# GitHub runner only (its checkout lives under the runner _work dir).
set -euo pipefail
[ "${GOATOS_ALLOW_OUTSIDE_WORKSPACE:-}" = "1" ] && exit 0
root="${GOATOS_WORKSPACE_ROOT:-$HOME/mesha}"
top="$(git rev-parse --show-toplevel)"
root_real="$(cd "$root" 2>/dev/null && pwd -P || printf '%s' "$root")"
top_real="$(cd "$top" && pwd -P)"
case "$top_real/" in
  "$root_real"/*) exit 0 ;;
esac
echo "Goat OS work belongs under ~/mesha; this clone is at $top" >&2
echo "(workspace root: $root; create worktrees as ~/mesha/goatos-wt-<topic>)" >&2
exit 1
