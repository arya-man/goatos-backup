#!/usr/bin/env bash
# land-main.sh — the only supported Codex/Claude path for landing on main.
#
# Order is mechanical, not conversational:
#   fetch origin/main -> rebase candidate -> install push guards -> ci-local
#   -> fetch origin/main again -> retry if main moved -> push HEAD:main -> verify.
set -euo pipefail

repo="$(git rev-parse --show-toplevel 2>/dev/null || true)"
[ -n "$repo" ] || { echo "land-main: run inside a Git worktree" >&2; exit 2; }
cd "$repo"

die() {
  echo "land-main: $*" >&2
  exit 1
}

short_sha() {
  printf '%.12s' "$1"
}

is_clean() {
  [ -z "$(git status --porcelain --untracked-files=all)" ]
}

fetch_main() {
  git fetch --quiet origin main
  git rev-parse refs/remotes/origin/main
}

test_mode="${GOATOS_LAND_TEST_MODE:-0}"
origin_url="$(git remote get-url origin 2>/dev/null || true)"
if [ "$test_mode" != "1" ]; then
  case "$origin_url" in
    git@github.com:vgoats/goatos.git|ssh://git@github.com/vgoats/goatos.git|https://github.com/vgoats/goatos.git|https://github.com/vgoats/goatos) ;;
    *) die "origin must be the Mesha/VGoats vgoats/goatos repository; got ${origin_url:-<missing>}" ;;
  esac
fi

git rev-parse --verify HEAD >/dev/null 2>&1 || die "HEAD does not resolve to a commit"
is_clean || die "worktree is dirty; commit the scoped change and run this from a clean isolated worktree"

for state in rebase-merge rebase-apply MERGE_HEAD CHERRY_PICK_HEAD REVERT_HEAD; do
  if [ -e "$(git rev-parse --git-path "$state")" ]; then
    die "Git operation is already in progress (${state}); finish or abort it first"
  fi
done

max_attempts="${GOATOS_LAND_MAX_ATTEMPTS:-3}"
case "$max_attempts" in
  ''|*[!0-9]*) die "GOATOS_LAND_MAX_ATTEMPTS must be a positive integer" ;;
  0) die "GOATOS_LAND_MAX_ATTEMPTS must be at least 1" ;;
esac

echo "land-main: repository vgoats/goatos"
echo "land-main: candidate $(short_sha "$(git rev-parse HEAD)") on $(git symbolic-ref --short -q HEAD || echo detached-HEAD)"

attempt=1
while [ "$attempt" -le "$max_attempts" ]; do
  echo "land-main: attempt ${attempt}/${max_attempts} — refresh origin/main"
  base_before="$(fetch_main)"

  if ! git merge-base --is-ancestor "$base_before" HEAD; then
    echo "land-main: rebasing candidate onto origin/main $(short_sha "$base_before")"
    if ! git rebase "$base_before"; then
      git rebase --abort >/dev/null 2>&1 || true
      die "automatic rebase conflicted and was aborted; resolve the conflict in an isolated worktree, then run make land-main again"
    fi
  else
    echo "land-main: candidate already contains origin/main $(short_sha "$base_before")"
  fi

  is_clean || die "rebase left tracked or untracked changes; refusing to certify or push"
  candidate_sha="$(git rev-parse HEAD)"

  if [ "$candidate_sha" = "$base_before" ]; then
    echo "land-main: candidate already equals current origin/main; nothing to push"
    exit 0
  fi

  if [ "$test_mode" = "1" ]; then
    test_ci="${GOATOS_LAND_TEST_CI_COMMAND:-}"
    [ -n "$test_ci" ] || die "GOATOS_LAND_TEST_CI_COMMAND is required in test mode"
    "$test_ci"
  else
    bash tools/agent-hooks/install-stg-push-guard.sh
    make ci-local
  fi

  [ "$(git rev-parse HEAD)" = "$candidate_sha" ] || die "HEAD changed while ci-local ran; refusing to push uncertified code"
  is_clean || die "ci-local changed the worktree; commit or remove generated changes and rerun"

  echo "land-main: CI green at $(short_sha "$candidate_sha"); checking main again"
  base_after="$(fetch_main)"
  if [ "$base_after" != "$base_before" ]; then
    if [ "$base_after" = "$candidate_sha" ] || git merge-base --is-ancestor "$candidate_sha" "$base_after"; then
      echo "land-main: origin/main already contains candidate $(short_sha "$candidate_sha")"
      exit 0
    fi
    echo "land-main: origin/main moved $(short_sha "$base_before") -> $(short_sha "$base_after"); rebasing and rerunning CI"
    attempt=$((attempt + 1))
    continue
  fi

  git merge-base --is-ancestor "$base_after" "$candidate_sha" || die "candidate is not based on the refreshed origin/main"

  if [ "$test_mode" = "1" ]; then
    echo "land-main: test mode verified rebase-before-CI at $(short_sha "$candidate_sha"); push skipped"
    exit 0
  fi

  echo "land-main: pushing certified $(short_sha "$candidate_sha") to main"
  if git mesha-push HEAD:main; then
    landed="$(fetch_main)"
    if [ "$landed" = "$candidate_sha" ] || git merge-base --is-ancestor "$candidate_sha" "$landed"; then
      echo "land-main: LANDED $(short_sha "$candidate_sha"); origin/main is $(short_sha "$landed")"
      exit 0
    fi
    die "push returned success, but origin/main does not contain $(short_sha "$candidate_sha")"
  fi

  newest="$(fetch_main)"
  if [ "$newest" != "$base_after" ]; then
    echo "land-main: push raced with main $(short_sha "$base_after") -> $(short_sha "$newest"); retrying from fresh main"
    attempt=$((attempt + 1))
    continue
  fi
  die "push failed without origin/main moving; check the Mesha token and push-guard output"
done

die "origin/main moved during every attempt; rerun make land-main when the landing queue settles"
