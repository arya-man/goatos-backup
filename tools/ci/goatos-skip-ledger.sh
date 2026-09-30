#!/usr/bin/env bash
# goatos-skip-ledger.sh — the ONE way a local CI / push-gate lane may be skipped.
#
# Sourced by tools/ci/run-local-ci.sh, tools/ci/admin-web-visual-gate.sh and
# tools/ci/admin-web-push-gate.sh (and by their installed copies in the git hooks dir,
# where this file is installed under the same name, so `$(dirname "$0")` resolves it in
# both places).
#
# Ravi 2026-09-30: "update skills, guardrails, enforce local CI/CD strictly." A lane that
# is skipped by a flag (GOATOS_SKIP_ADMIN_WEB_VISUAL_GATE, GOATOS_FAST_LOCAL_CI,
# GOATOS_CI_ONLY_STEP, GOATOS_ADMIN_WEB_BASE_URL pointed at an already-running app) must:
#   1. carry a written reason: GOATOS_SKIP_REASON="..." (>= 12 characters). No reason =
#      the command FAILS; the skip never happens silently;
#   2. be appended to the skip ledger (default <git-common-dir>/goatos-ci/skip-ledger.tsv,
#      override GOATOS_SKIP_LEDGER): time, sha, branch, flag, lane, user, reason;
#   3. be printed loudly, and again in the run-local-ci / push-gate summary.
# A lane that cannot run for a missing prerequisite (no live app for the route lanes) is
# not a flag skip, but it is still recorded (goatos_record_skip) and printed as SKIP,
# never an `echo` alone.

goatos_skip_ledger_path() {
  if [ -n "${GOATOS_SKIP_LEDGER:-}" ]; then
    printf '%s\n' "$GOATOS_SKIP_LEDGER"
    return 0
  fi
  local common
  common="$(git rev-parse --git-common-dir 2>/dev/null || true)"
  [ -n "$common" ] || common="${TMPDIR:-/tmp}"
  common="$(cd "$common" 2>/dev/null && pwd -P || printf '%s' "$common")"
  printf '%s\n' "$common/goatos-ci/skip-ledger.tsv"
}

_goatos_skip_append() { # flag lane reason
  local ledger sha branch
  ledger="$(goatos_skip_ledger_path)"
  mkdir -p "$(dirname "$ledger")" 2>/dev/null || true
  sha="$(git rev-parse HEAD 2>/dev/null || echo unknown)"
  branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)"
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$sha" "$branch" "$1" "$2" "${USER:-unknown}" \
    "$(printf '%s' "$3" | tr '\t\n' '  ')" >>"$ledger" 2>/dev/null || {
    echo "!! skip-ledger: could not write $ledger" >&2
    return 1
  }
}

# goatos_require_skip_reason FLAG LANE — returns 0 (skip allowed, recorded) or 1 (refused).
goatos_require_skip_reason() {
  local flag="$1" lane="$2"
  local reason
  reason="$(printf '%s' "${GOATOS_SKIP_REASON:-}" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
  if [ "${#reason}" -lt 12 ]; then
    {
      echo "!! ${lane}: REFUSED — ${flag} skips a gate lane and needs a written reason."
      echo "!!   Re-run with GOATOS_SKIP_REASON=\"<why this lane cannot run now, >= 12 chars>\"."
      echo "!!   The skip is then recorded in $(goatos_skip_ledger_path) and printed by CI."
    } >&2
    return 1
  fi
  _goatos_skip_append "$flag" "$lane" "$reason" || return 1
  {
    echo "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
    echo "!! SKIPPED LANE: ${lane}"
    echo "!!   flag   : ${flag}"
    echo "!!   reason : ${reason}"
    echo "!!   ledger : $(goatos_skip_ledger_path)"
    echo "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
  } >&2
  return 0
}

# goatos_record_skip LANE WHY — a lane that could not run (missing prerequisite, not a flag).
goatos_record_skip() {
  _goatos_skip_append "prerequisite" "$1" "$2" || true
  echo "!! SKIPPED LANE (recorded): $1 — $2" >&2
}

# goatos_print_skip_ledger [SHA] — loud summary of the ledger rows for SHA (default HEAD).
goatos_print_skip_ledger() {
  local ledger sha rows
  ledger="$(goatos_skip_ledger_path)"
  sha="${1:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
  [ -f "$ledger" ] || return 0
  rows="$(awk -F'\t' -v s="$sha" '$2 == s' "$ledger")"
  [ -n "$rows" ] || return 0
  echo ""
  echo "!!!!!!!! SKIP LEDGER @ ${sha:0:12} ($(printf '%s\n' "$rows" | grep -c .) skipped lane(s)) !!!!!!!!"
  printf '%s\n' "$rows" | awk -F'\t' '{ printf "!!  %s  %-34s %-40s by %s: %s\n", $1, $4, $5, $6, $7 }'
  echo "!!  ledger: ${ledger}"
  echo "!!  A skipped lane proves nothing. Run it before calling this change done."
}
