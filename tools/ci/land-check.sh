#!/usr/bin/env bash
# land-check.sh — `make land-check`: run exactly the ci-local that `make land-main`
# would run, WITHOUT pushing, so the landing itself mostly reuses passes.
#
#   fetch origin/main -> temp worktree at HEAD -> rebase onto fresh origin/main
#   -> the same ci-local target land-main picks (ci-local / ci-local-screenshots)
#
# Every PASS lands in the input-keyed step cache in the git COMMON dir, which the
# real `make land-main` shares. Because land-main rebases the same commits onto
# the same origin/main, its tree is identical and every unchanged step is reused;
# only steps whose inputs moved (a fix commit, or main moving meanwhile) re-run.
# It never pushes, never touches your worktree, and writes no receipt that can
# authorize main (the receipt stays in the temp worktree and is deleted with it).
set -euo pipefail

src="$(git rev-parse --show-toplevel 2>/dev/null || true)"
[ -n "$src" ] || { echo "land-check: run inside a Git worktree" >&2; exit 2; }
cd "$src"
[ -z "$(git status --porcelain --untracked-files=all)" ] || {
  echo "land-check: worktree is dirty; commit first (the check certifies committed content)" >&2; exit 2; }

git fetch -q origin main
base="$(git rev-parse origin/main)"
head="$(git rev-parse HEAD)"
wt="$(mktemp -d "${TMPDIR:-/tmp}/goatos-land-check.XXXXXX")"
cleanup() { git -C "$src" worktree remove --force "$wt" >/dev/null 2>&1 || rm -rf "$wt"; git -C "$src" worktree prune >/dev/null 2>&1 || true; }
trap cleanup EXIT

git worktree add -q --detach "$wt" "$head"
cd "$wt"
if ! git merge-base --is-ancestor "$base" HEAD; then
  if ! git -c core.hooksPath=/dev/null rebase -q "$base" >/dev/null 2>&1; then
    git rebase --abort >/dev/null 2>&1 || true
    echo "land-check: rebase onto origin/main $(git rev-parse --short "$base") CONFLICTS — rebase your branch first" >&2
    exit 1
  fi
fi
# Reuse the source worktree's installed deps rather than `npm ci` from scratch.
if [ -d "$src/apps/admin-web/node_modules" ] && [ ! -e apps/admin-web/node_modules ]; then
  ln -s "$src/apps/admin-web/node_modules" apps/admin-web/node_modules  # ignored by apps/admin-web/.gitignore
fi

ci_target="ci-local"
# Same detection land-main uses (android_ui_diff_against).
if ( changed_since_base() { git diff --name-only "$base...HEAD"; }
     . tools/ci/android-ui-diff.sh; android_ui_diff_detected ) >/dev/null 2>&1; then
  ci_target="ci-local-screenshots"
fi
echo "land-check: $(git rev-parse --short HEAD) (= $(git rev-parse --short "$head") rebased on origin/main $(git rev-parse --short "$base")) -> make ${ci_target}"
t0=$SECONDS
if make "$ci_target"; then
  echo "land-check: GREEN in $((SECONDS - t0))s. Passes are cached by input; now run: make land-main"
else
  echo "land-check: RED in $((SECONDS - t0))s. Fix, commit, re-run the printed single step, then make land-check / make land-main." >&2
  exit 1
fi
