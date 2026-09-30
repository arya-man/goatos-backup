#!/usr/bin/env bash
# check-push-hook-freshness.sh — the installed pre-push hook must BE the gate,
# not a snapshot of an older gate.
#
# WHY THIS EXISTS (the incident):
#   tools/agent-hooks/install-stg-push-guard.sh `cp`s the guards into the hooks
#   dir. The hook then runs THOSE COPIES. So every improvement to
#   tools/ci/check-local-ci-evidence.mjs is inert for `git push` and
#   `git mesha-push` until someone re-runs the installer — only `make land-main`
#   refreshes them. Measured drift at the time this was written:
#       installed copy 17,147 bytes   repo file 34,333 bytes
#       computeBaseAncestry: repo 10 / installed 0
#       SCREENSHOT_BLOCKING: repo  3 / installed 0
#       screenshots:         repo 46 / installed 0
#   Same receipt, repo checker exit=1, installed checker exit=0. Two "closed"
#   gate holes were closed only for people who type `make land-main`.
#
# THE SHIM (J1B P0-2, 2026-09-30) replaced the copies. Every worktree shares one hooks dir, so a
# copied hook was whatever branch last ran `make ai-setup` (review/pr-307's stg/main-only hook
# silently replaced PR #294's admin-web gate, and 2d43dee4a reached the PR with no visual pass).
# The installed pre-push is now tools/agent-hooks/pre-push.shim, which runs the PUSHING worktree's
# committed tools/agent-hooks/pre-push.hook (+ tools/agent-hooks/pre-push.bundle), or the legacy
# stg/main guard for a branch without one. A branch therefore judges itself with its own committed
# gate; what keeps main honest is the exact-SHA ci-local receipt, the goatos/land-main-receipt status
# the GitHub main ruleset requires, and land-main's push-gate receipt check.
# This guard proves, on THIS machine:
#   1. the installed pre-push IS the shim (byte-identical to tools/agent-hooks/pre-push.shim);
#   2. the legacy-fallback snapshots exist beside it;
#   3. run in THIS worktree, the shim resolves THIS worktree's committed pre-push.hook;
#   4. REAL test pushes (`git push` into a throwaway bare repo, through the installed shim, with a
#      vgoats origin URL): main without a ci-local receipt is refused; an admin-web change pushed to
#      a feature branch is refused by this branch's admin-web push gate; and a branch with no
#      checked-in hook still gets the legacy stg/main guard (feature allowed, main refused);
#   5. this branch's push gate lists every required lane.
#
# FAIL-CLOSED: a missing installed guard is a FAILURE, not a skip. "The hook
# isn't installed" means the push gate is not enforcing at all, which is worse
# than drift, not better.
set -uo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo"

# Skip hook checks on CI runners where hooks are never installed.
# This guard is only meaningful on developer machines with a local clone.
if [ "${CI:-}" = "true" ] || [ "${GITHUB_ACTIONS:-}" = "true" ] || [ "${CI_ENVIRONMENT:-}" != "" ]; then
  echo "push-hook-freshness: skipping on CI runner (hook installation not required in CI)"
  exit 0
fi

# Resolve the same hooks dir the installer resolves.
hooks_path="$(git config --get core.hooksPath || true)"
if [ -n "$hooks_path" ]; then
  case "$hooks_path" in
    /*) hooks_dir="$hooks_path" ;;
    *) hooks_dir="$repo/$hooks_path" ;;
  esac
else
  hooks_dir="$(git rev-parse --git-path hooks)"
fi

# ABSOLUTE, ALWAYS. `git rev-parse --git-path hooks` answers RELATIVE to the cwd
# ('.git/hooks' from the repo root), and the hook probe below runs its `cp` AFTER
# `cd`-ing into a throwaway sandbox — where '.git/hooks/pre-push' does not exist.
# That made the probe die with `cp: .git/hooks/pre-push: No such file or directory`
# and score as exit 97 = GUARD FAILURE, blocking `make guardrails` on every clone
# whose hooks dir is the default. The cmp loop above never noticed because it runs
# before the cd. The core.hooksPath arm already absolutises itself; this covers the
# rev-parse arm and is a no-op for a path that is absolute already.
case "$hooks_dir" in
  /*) ;;
  *) hooks_dir="$repo/$hooks_dir" ;;
esac

shim_src="tools/agent-hooks/pre-push.shim"
hook="$hooks_dir/pre-push"
# Lanes this branch's admin-web push gate must run on every push (Ravi 2026-09-30, J1 P0-4).
REQUIRED_LANES="design-guard typecheck unit-tests next-build visual-gate"
# The files the shim stages for the branch hook (must match tools/agent-hooks/pre-push.bundle).
BUNDLE_FILES="tools/agent-hooks/pre-push.hook tools/agent-hooks/pre-push.bundle tools/ci/check-local-ci-evidence.mjs
tools/ci/check-stg-promotion.mjs tools/ci/step-input-digest.mjs tools/ci/admin-web-visual-gate.sh tools/ci/admin-web-push-gate.sh
tools/ci/goatos-skip-ledger.sh tools/ci/admin-web-push-receipt.mjs"
FALLBACK_SNAPSHOTS="goatos-check-stg-promotion.mjs goatos-check-local-ci-evidence.mjs step-input-digest.mjs"

fail=0
for f in $shim_src $BUNDLE_FILES; do
  [ -f "$f" ] || { echo "!! push-hook-freshness: repo source missing: $f" >&2; fail=1; }
done

# ── 1. the installed pre-push IS the shim ────────────────────────────────────
if [ ! -f "$hook" ]; then
  echo "!! push-hook-freshness: no pre-push hook installed at $hook" >&2
  echo "!! No push gate is enforcing on this machine. Fix: make push-hooks-install (or make ai-setup)" >&2
  fail=1
elif [ -f "$shim_src" ] && ! cmp -s "$shim_src" "$hook"; then
  echo "!! push-hook-freshness: THE INSTALLED pre-push IS NOT THE SHIM" >&2
  echo "!!   repo shim : $shim_src ($(wc -c <"$shim_src" | tr -d ' ') bytes)" >&2
  echo "!!   installed : $hook ($(wc -c <"$hook" | tr -d ' ') bytes)" >&2
  if grep -q 'GOATOS_PUSH_GUARDS\|GOATOS_STG_PROMOTION_GUARD' "$hook" 2>/dev/null; then
    echo "!! It is a COPIED branch hook (pre-shim installer): every worktree runs whichever branch installed last." >&2
  fi
  echo "!! Fix: make push-hooks-install   (then re-run this check)" >&2
  fail=1
fi

# ── 2. legacy-fallback snapshots ─────────────────────────────────────────────
for f in $FALLBACK_SNAPSHOTS; do
  [ -f "$hooks_dir/$f" ] || { echo "!! push-hook-freshness: legacy-fallback snapshot NOT INSTALLED: $hooks_dir/$f (Fix: make push-hooks-install)" >&2; fail=1; }
done

# ── 3. in THIS worktree the shim resolves THIS worktree's committed hook ─────
if [ -f "$hook" ]; then
  which_out="$(cd "$repo" && GOATOS_PUSH_SHIM_WHICH=1 bash "$hook" </dev/null 2>/dev/null || true)"
  if git cat-file -e HEAD:tools/agent-hooks/pre-push.hook 2>/dev/null; then
    want="branch-hook $repo/tools/agent-hooks/pre-push.hook@HEAD"
    if [ "$which_out" != "$want" ]; then
      echo "!! push-hook-freshness: in $repo the installed pre-push does not run this worktree's hook" >&2
      echo "!!   expected: $want" >&2
      echo "!!   got     : ${which_out:-<nothing: not the shim>}" >&2
      fail=1
    fi
  fi
fi

# ── 4. REAL test pushes through the installed shim ───────────────────────────
# SANDBOX SAFETY (this probe once DESTROYED the live repo, 2026-08-05: an empty mktemp path made
# `cd ""` a no-op and a `git add -A && git commit` swallowed 49 dirty files of other sessions): the
# sandbox path is validated before anything runs, every setup step is fatal (exit 97 = GUARD
# FAILURE, never proof), global/system config is /dev/null, and rm is guarded.
probe() { # name -> runs in a fresh sandbox; prints the push output and PUSH_EXIT=<rc> / REMOTE=<sha|none>
  local name="$1" sb
  sb="$(mktemp -d "${TMPDIR:-/tmp}/goatos-hookpush.XXXXXX" 2>/dev/null || true)"
  if [ -z "$sb" ] || [ ! -d "$sb" ]; then echo "SANDBOX_FAIL"; return 97; fi
  (
    cd "$sb" || exit 97
    [ "$(pwd -P)" = "$(cd "$sb" && pwd -P)" ] || exit 97
    export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GOATOS_PUSH_RECEIPT_PUBLISH=0
    export GOATOS_SKIP_LEDGER="$sb/ledger.tsv" GOATOS_PUSH_RECEIPT_DIR="$sb/receipts"
    unset GOATOS_PUSH_SHIM_ACTIVE GOATOS_SKIP_ADMIN_WEB_VISUAL_GATE GOATOS_SKIP_REASON
    git init -q --bare remote.git || exit 97
    mkdir -p hooks work || exit 97
    cp "$hook" hooks/pre-push || exit 97
    for f in $FALLBACK_SNAPSHOTS; do [ -f "$hooks_dir/$f" ] && { cp "$hooks_dir/$f" "hooks/$f" || exit 97; }; done
    chmod +x hooks/* 2>/dev/null
    cd work || exit 97
    git init -q -b probe . >/dev/null 2>&1 || exit 97
    git config user.email guard@local && git config user.name guard && git config commit.gpgsign false || exit 97
    git config core.hooksPath "$sb/hooks" || exit 97
    git remote add origin git@github.com:vgoats/goatos.git || exit 97
    # a REAL push: the hook sees the vgoats origin, the objects go to the throwaway bare repo
    git config url."$sb/remote.git".pushInsteadOf git@github.com:vgoats/goatos.git || exit 97
    for f in $BUNDLE_FILES; do
      [ -f "$repo/$f" ] || continue
      mkdir -p "$(dirname "$f")" && cp "$repo/$f" "$f" || exit 97
    done
    echo seed >seed.txt || exit 97
    [ "$name" = legacy ] && rm -f tools/agent-hooks/pre-push.hook
    git add -A >/dev/null && git commit -qm base || exit 97
    case "$name" in
      main) refspec="HEAD:refs/heads/main" ;;
      feature|legacy)
        mkdir -p apps/admin-web && echo 'export const probe = 1;' >apps/admin-web/probe.tsx || exit 97
        git add -A >/dev/null && git commit -qm probe || exit 97
        refspec="HEAD:refs/heads/probe-feature" ;;
      legacy-main) rm -f tools/agent-hooks/pre-push.hook; git add -A >/dev/null; git commit -qm legacy || exit 97; refspec="HEAD:refs/heads/main" ;;
    esac
    git push origin "$refspec" 2>&1
    echo "PUSH_EXIT=$?"
    echo "REMOTE=$(git --git-dir="$sb/remote.git" rev-parse -q --verify "${refspec#HEAD:}" 2>/dev/null || echo none)"
  )
  local rc=$?
  rm -rf "$sb"
  return "$rc"
}
probe_fail() { # label output
  echo "!! push-hook-freshness: REAL TEST PUSH FAILED — $1" >&2
  printf '%s\n' "$2" | tail -10 | sed 's/^/!!     /' >&2
  fail=1
}
if [ -f "$hook" ]; then
  out="$(probe main)"; rc=$?
  if [ "$rc" = 97 ]; then probe_fail "the main probe could not build its sandbox (GUARD FAILURE, not a pass)" "$out"
  elif ! printf '%s\n' "$out" | grep -q '^REMOTE=none$' || printf '%s\n' "$out" | grep -q '^PUSH_EXIT=0$'; then
    probe_fail "a push of refs/heads/main with NO local-CI receipt was ACCEPTED: the installed hook does not gate main" "$out"
  fi
  out="$(probe feature)"; rc=$?
  if [ "$rc" = 97 ]; then probe_fail "the feature probe could not build its sandbox (GUARD FAILURE, not a pass)" "$out"
  elif ! printf '%s\n' "$out" | grep -q "running this worktree's checked-in hook"; then
    probe_fail "THE INSTALLED pre-push DID NOT RUN THE PUSHING WORKTREE'S CHECKED-IN HOOK" "$out"
  elif printf '%s\n' "$out" | grep -q '^PUSH_EXIT=0$' || ! printf '%s\n' "$out" | grep -q 'admin-web-push-gate: BLOCKED'; then
    probe_fail "THE BRANCH HOOK DOES NOT RUN THE ADMIN-WEB LANES ON A FEATURE-BRANCH PUSH (an admin-web change to refs/heads/probe-feature was not refused by the admin-web push gate)" "$out"
  fi
  out="$(probe legacy)"; rc=$?
  if [ "$rc" = 97 ]; then probe_fail "the legacy probe could not build its sandbox (GUARD FAILURE, not a pass)" "$out"
  elif ! printf '%s\n' "$out" | grep -q 'running the legacy stg/main guard' || ! printf '%s\n' "$out" | grep -q '^PUSH_EXIT=0$'; then
    probe_fail "a branch with NO checked-in pre-push.hook did not get the legacy stg/main guard (feature push should pass through it)" "$out"
  fi
  out="$(probe legacy-main)"; rc=$?
  if [ "$rc" = 97 ]; then probe_fail "the legacy-main probe could not build its sandbox (GUARD FAILURE, not a pass)" "$out"
  elif printf '%s\n' "$out" | grep -q '^PUSH_EXIT=0$' || ! printf '%s\n' "$out" | grep -q '^REMOTE=none$'; then
    probe_fail "a branch with NO checked-in pre-push.hook pushed refs/heads/main with no receipt: the legacy fallback does not gate main" "$out"
  fi
fi

# ── 5. every admin-web lane is present in THIS branch's push gate ────────────
if [ -f tools/ci/admin-web-push-gate.sh ]; then
  lanes="$(bash tools/ci/admin-web-push-gate.sh --list-lanes 2>/dev/null | tr '\n' ' ')"
  for lane in $REQUIRED_LANES; do
    case " $lanes " in
      *" $lane "*) ;;
      *)
        echo "!! push-hook-freshness: this branch's admin-web push gate is MISSING LANE '$lane' (has: ${lanes:-none})" >&2
        echo "!! Fix: restore the lane in tools/ci/admin-web-push-gate.sh" >&2
        fail=1
        ;;
    esac
  done
fi

if [ "$fail" -eq 0 ]; then
  echo "push-hook-freshness: shared pre-push is the shim; it runs this worktree's committed hook; real test pushes: main refused without a receipt, admin-web feature push refused by the admin-web lanes (${REQUIRED_LANES}), no-hook branch gets the legacy stg/main guard"
fi

exit "$fail"
