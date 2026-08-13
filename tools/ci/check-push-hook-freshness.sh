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
# WHY cmp AND NOT exec-from-repo:
#   Making the hook `exec` the repo file would always be current, but it would
#   also let any branch modify the gate that is judging that same branch. The
#   copy is the tamper-resistant choice; this guard is the price of keeping it.
#   So: copy stays authoritative at push time, and CI refuses to go green while
#   the copy differs from the source of truth.
#
# FAIL-CLOSED: a missing installed guard is a FAILURE, not a skip. "The hook
# isn't installed" means the push gate is not enforcing at all, which is worse
# than drift, not better.
set -uo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo"

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

# repo source -> installed copy
pairs="
tools/ci/check-local-ci-evidence.mjs:goatos-check-local-ci-evidence.mjs
tools/ci/check-stg-promotion.mjs:goatos-check-stg-promotion.mjs
"

fail=0
checked=0

while IFS=: read -r src installed; do
  [ -n "$src" ] || continue
  checked=$((checked + 1))
  target="$hooks_dir/$installed"

  if [ ! -f "$src" ]; then
    echo "!! push-hook-freshness: repo source missing: $src" >&2
    fail=1
    continue
  fi

  if [ ! -f "$target" ]; then
    echo "!! push-hook-freshness: the pre-push guard is NOT INSTALLED: $target" >&2
    echo "!! The exact-SHA push gate is not enforcing on this machine." >&2
    echo "!! Fix: make ai-setup" >&2
    fail=1
    continue
  fi

  if ! cmp -s "$src" "$target"; then
    echo "!! push-hook-freshness: INSTALLED PUSH GUARD IS STALE" >&2
    echo "!!   repo source : $src ($(wc -c <"$src" | tr -d ' ') bytes)" >&2
    echo "!!   installed   : $target ($(wc -c <"$target" | tr -d ' ') bytes)" >&2
    echo "!! The hook runs the INSTALLED copy, so changes to the repo source are" >&2
    echo "!! inert for 'git push' and 'git mesha-push'." >&2
    echo "!! Fix: make ai-setup   (then re-run this check)" >&2
    echo "!! Inspect: diff '$target' '$src'" >&2
    fail=1
  fi
done <<EOF
$pairs
EOF

if [ "$checked" -eq 0 ]; then
  echo "!! push-hook-freshness: no guard pairs checked — this guard is inert" >&2
  exit 1
fi

# ── the HOOK ITSELF, proven BY EXECUTION ──────────────────────────────────────
# cmp'ing the two payload .mjs copies is not the same as proving the gate runs.
# The installer also WRITES $hooks_dir/pre-push, and that file was unguarded:
# deleting its one `node "$evidence_guard" --pre-push` line disables the
# exact-SHA main gate outright, while this guard still printed
# "2 installed push guard(s) match their repo source". Demonstrated during the
# 2026-08-05 CI audit.
#
# NOT a grep for that line: a comment containing it would satisfy a regex. The
# hook is EXECUTED in a throwaway repo whose origin is the vgoats remote (the
# only origin its `case` arms on) and which has NO receipt, against a payload
# that updates refs/heads/main. A hook that still gates must exit non-zero. Any
# edit that stops routing to the evidence guard — a deleted call, a widened
# `case`, an added `|| true`, a swapped ref test — makes this go red.
hook="$hooks_dir/pre-push"
if [ ! -f "$hook" ]; then
  echo "!! push-hook-freshness: no pre-push hook installed at $hook" >&2
  echo "!! The exact-SHA push gate is not enforcing on this machine." >&2
  echo "!! Fix: make ai-setup" >&2
  fail=1
else
  # ── SANDBOX SAFETY — this block once DESTROYED the live repo ────────────────
  # If mktemp failed, $hook_sandbox was EMPTY, `cd ""` is a NO-OP that leaves the
  # subshell in the REAL repo, and the `git add -A && git commit` below then
  # swallowed 49 dirty files of other sessions' in-flight work into a stray commit
  # on the real HEAD (observed 2026-08-05: commits 285bfe498 / 36bbf6dc9 "base").
  # The same run also wrote `user.email=guard@local` into the repo's LOCAL config,
  # clobbering the maintainer identity. Both are guarded now:
  #   * the sandbox path is validated BEFORE anything runs;
  #   * every setup step is fatal (exit 97) so nothing proceeds half-built;
  #   * setup failure (97) scores as GUARD FAILURE, never as proof — a guard that
  #     cannot build its fixture has proven nothing;
  #   * git config writes are confined by GIT_CONFIG_GLOBAL/SYSTEM=/dev/null and
  #     by only ever running inside the validated sandbox;
  #   * the rm is guarded against an empty variable.
  hook_sandbox="$(mktemp -d "${TMPDIR:-/tmp}/goatos-hookexec.XXXXXX" 2>/dev/null || true)"
  if [ -z "$hook_sandbox" ] || [ ! -d "$hook_sandbox" ]; then
    echo "!! push-hook-freshness: could not create a sandbox under \${TMPDIR:-/tmp}; refusing to run the hook probe in the live repo" >&2
    fail=1
    hook_probe_rc=97
  else
    (
      cd "$hook_sandbox" || exit 97
      # Belt and braces: never proceed unless we are demonstrably in the sandbox.
      [ "$(pwd -P)" = "$(cd "$hook_sandbox" && pwd -P)" ] || exit 97
      export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
      git init -q -b main . >/dev/null 2>&1 || exit 97
      git config user.email guard@local || exit 97
      git config user.name guard || exit 97
      git config commit.gpgsign false || exit 97
      git remote add origin git@github.com:vgoats/goatos.git || exit 97
      mkdir -p hooks || exit 97
      cp "$hook" hooks/pre-push || exit 97
      for f in goatos-check-local-ci-evidence.mjs goatos-check-stg-promotion.mjs; do
        [ -f "$hooks_dir/$f" ] && { cp "$hooks_dir/$f" "hooks/$f" || exit 97; }
      done
      chmod +x hooks/* 2>/dev/null
      echo seed >seed.txt || exit 97
      git add -A >/dev/null || exit 97
      git commit -qm base || exit 97
      head="$(git rev-parse HEAD)" || exit 97
      # A main update with NO receipt anywhere: the evidence gate must refuse it.
      printf 'refs/heads/main %s refs/heads/main %s\n' "$head" "$head" \
        | bash hooks/pre-push origin git@github.com:vgoats/goatos.git >/dev/null 2>&1
    )
    hook_probe_rc=$?
  fi
  if [ "$hook_probe_rc" = "97" ]; then
    echo "!! push-hook-freshness: the hook probe could not build its sandbox — this is a GUARD FAILURE, not a pass. The installed hook was NOT proven to gate main." >&2
    fail=1
  elif [ "$hook_probe_rc" -eq 0 ]; then
    echo "!! push-hook-freshness: THE INSTALLED pre-push HOOK DOES NOT GATE MAIN" >&2
    echo "!!   $hook allowed a refs/heads/main update with NO local-CI receipt." >&2
    echo "!! The .mjs guards may be byte-identical to their sources and still never" >&2
    echo "!! be invoked — the hook body is what routes to them." >&2
    echo "!! Fix: make ai-setup   (reinstalls the hook from tools/agent-hooks/install-stg-push-guard.sh)" >&2
    fail=1
  fi
  [ -n "$hook_sandbox" ] && [ -d "$hook_sandbox" ] && rm -rf "$hook_sandbox"
fi

if [ "$fail" -eq 0 ]; then
  echo "push-hook-freshness: ${checked} installed push guard(s) match their repo source; installed pre-push hook proven to gate main"
fi

exit "$fail"
