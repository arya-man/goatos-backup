#!/usr/bin/env bash
# land-main.sh — the only supported Codex/Claude path for landing on main.
#
# Order is mechanical, not conversational:
#   fetch origin/main -> rebase candidate -> install push guards -> ci-local
#   -> fetch origin/main again -> retry if main moved -> push HEAD:main -> verify.
# Set GOATOS_BYPASS_LOCAL_CI=1 for an explicit fatigue/incident bypass of the
# local CI run and exact-SHA evidence guard. The script still rebases on fresh
# origin/main and keeps the staging promotion push guard installed.
set -euo pipefail

repo="$(git rev-parse --show-toplevel 2>/dev/null || true)"
[ -n "$repo" ] || { echo "land-main: run inside a Git worktree" >&2; exit 2; }
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$repo"

die() {
  echo "land-main: $*" >&2
  exit 1
}

short_sha() {
  printf '%.12s' "$1"
}

patch_id_against() {
  local base="$1"
  local head="${2:-HEAD}"
  git diff --binary "$base...$head" | git patch-id --stable | awk '{print $1}'
}

selected_jobs_against() {
  local base="$1"
  local head="${2:-HEAD}"
  if [ ! -f tools/ci/ci-scope.mjs ]; then
    echo "common,backend,query-plans,admin-web,android"
    return
  fi
  node tools/ci/ci-scope.mjs --base "$base" --head "$head" --format github \
    | sed -n 's/^selected_jobs=//p'
}

is_clean() {
  [ -z "$(git status --porcelain --untracked-files=all)" ]
}

fetch_main() {
  git fetch --quiet origin main
  git rev-parse refs/remotes/origin/main
}

local_ci_evidence_script() {
  if [ -f tools/ci/check-local-ci-evidence.mjs ]; then
    echo "tools/ci/check-local-ci-evidence.mjs"
  else
    echo "$script_dir/check-local-ci-evidence.mjs"
  fi
}

test_mode="${GOATOS_LAND_TEST_MODE:-0}"
bypass_local_ci="${GOATOS_BYPASS_LOCAL_CI:-0}"
case "$bypass_local_ci" in
  0|1) ;;
  *) die "GOATOS_BYPASS_LOCAL_CI must be 0 or 1" ;;
esac
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
  candidate_patch_id="$(patch_id_against "$base_before" "$candidate_sha")"

  if [ "$candidate_sha" = "$base_before" ]; then
    echo "land-main: candidate already equals current origin/main; nothing to push"
    exit 0
  fi

  if [ "$test_mode" = "1" ]; then
    test_ci="${GOATOS_LAND_TEST_CI_COMMAND:-}"
    if [ "$bypass_local_ci" = "1" ]; then
      echo "land-main: GOATOS_BYPASS_LOCAL_CI=1; test-mode CI command skipped"
    else
      [ -n "$test_ci" ] || die "GOATOS_LAND_TEST_CI_COMMAND is required in test mode"
      "$test_ci"
    fi
  else
    bash tools/agent-hooks/install-stg-push-guard.sh
    if [ "$bypass_local_ci" = "1" ]; then
      echo "land-main: GOATOS_BYPASS_LOCAL_CI=1; skipping make ci-local and exact-SHA local-CI receipt"
    else
      make ci-local
    fi
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
    echo "land-main: origin/main moved $(short_sha "$base_before") -> $(short_sha "$base_after"); rebasing"
    if git rebase "$base_after"; then
      is_clean || die "rebase left tracked or untracked changes; refusing to reuse CI evidence"
      rebased_sha="$(git rev-parse HEAD)"
      rebased_patch_id="$(patch_id_against "$base_after" "$rebased_sha")"
      if [ -n "$candidate_patch_id" ] && [ "$candidate_patch_id" = "$rebased_patch_id" ]; then
        rebased_jobs="$(selected_jobs_against "$base_after" "$rebased_sha")"
        if [ -n "$rebased_jobs" ] && node "$(local_ci_evidence_script)" \
          --reuse-after-rebase \
          --old-sha "$candidate_sha" \
          --new-sha "$rebased_sha" \
          --new-base "$base_after" \
          --jobs "$rebased_jobs"; then
          echo "land-main: reused green CI receipt after patch-identical rebase $(short_sha "$candidate_sha") -> $(short_sha "$rebased_sha")"
          candidate_sha="$rebased_sha"
          base_before="$base_after"
          if [ "$test_mode" = "1" ]; then
            echo "land-main: test mode verified patch-identical rebase receipt reuse at $(short_sha "$candidate_sha"); push skipped"
            exit 0
          fi
          if [ "$bypass_local_ci" = "1" ]; then
            echo "land-main: pushing local-CI-bypassed $(short_sha "$candidate_sha") to main"
          else
            echo "land-main: pushing certified $(short_sha "$candidate_sha") to main"
          fi
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
        fi
      fi
      echo "land-main: rebased patch changed or CI scope changed; rerunning CI"
    else
      git rebase --abort >/dev/null 2>&1 || true
      die "automatic rebase conflicted and was aborted; resolve the conflict in an isolated worktree, then run make land-main again"
    fi
    attempt=$((attempt + 1))
    continue
  fi

  git merge-base --is-ancestor "$base_after" "$candidate_sha" || die "candidate is not based on the refreshed origin/main"

  if [ "$test_mode" = "1" ]; then
    echo "land-main: test mode verified rebase-before-CI at $(short_sha "$candidate_sha"); push skipped"
    exit 0
  fi

  if [ "$bypass_local_ci" = "1" ]; then
    echo "land-main: pushing local-CI-bypassed $(short_sha "$candidate_sha") to main"
  else
    echo "land-main: pushing certified $(short_sha "$candidate_sha") to main"
  fi
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
