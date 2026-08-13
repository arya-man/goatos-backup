#!/usr/bin/env bash
# Self-test for check-parallel-dispatch-cleanup.sh.
#
# The ONLY thing that makes that guard worth having is that it goes RED when the
# traps are gone. So this builds a sabotaged copy of parallel-dispatch.sh with
# every _dispatch_install_traps call neutered, points the guard at it, and
# requires a failure. If this self-test ever passes trivially, the guard is inert.
set -uo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo"
fails=0
# F-F: the sabotage copy lives in a temp dir, NOT tools/ci/ — stray fixtures in
# the tree have already been left behind once by this session.
sab_dir="$(mktemp -d "${TMPDIR:-/tmp}/goatos-dispatch-selftest.XXXXXX")"
sab="$sab_dir/dispatch-sabotage.sh"
# F-D: `pkill -f "sleep 120"` is MACHINE-WIDE — it would kill an unrelated
# `sleep 120` belonging to the maintainer or another session. Never pattern-kill
# across the machine from a test; the guard already reaps what it can see.
cleanup() { [ -n "$sab_dir" ] && rm -rf "$sab_dir"; }
trap cleanup EXIT

# (a) the real dispatch must PASS
bash tools/ci/check-parallel-dispatch-cleanup.sh >/dev/null 2>&1
if [ $? -eq 0 ]; then echo "ok    (a) real dispatch cleans up"; else
  echo "FAIL  (a) real dispatch left orphans or a leaked run dir" >&2; fails=$((fails+1)); fi

# (b) trap-less dispatch must FAIL — the guard's whole reason to exist
sed 's/^\( *\)_dispatch_install_traps$/\1: # sabotage/' tools/ci/parallel-dispatch.sh > "$sab"
if ! grep -q '# sabotage' "$sab"; then
  echo "FAIL  (b) could not sabotage the traps — the call site was renamed; update this self-test" >&2
  fails=$((fails+1))
else
  GOATOS_DISPATCH_UNDER_TEST="$sab" bash tools/ci/check-parallel-dispatch-cleanup.sh >/dev/null 2>&1
  if [ $? -ne 0 ]; then echo "ok    (b) trap-less dispatch REJECTED"; else
    echo "FAIL  (b) guard passed a dispatch with NO traps — it is inert" >&2; fails=$((fails+1)); fi
fi

[ "$fails" -eq 0 ] || { echo "check-parallel-dispatch-cleanup.test.sh: ${fails} case(s) FAILED" >&2; exit 1; }
echo "check-parallel-dispatch-cleanup.test.sh: all cases ok"
