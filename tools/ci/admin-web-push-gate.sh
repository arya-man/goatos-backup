#!/usr/bin/env bash
# admin-web-push-gate.sh — the admin-web lanes of the pre-push hook, on EVERY push to ANY branch.
#
# Ravi 2026-09-30 ("update skills, guardrails, enforce local CI/CD strictly"; judge J1 P0-4 / CI
# gaps 1-3): pushes to PR branches ran no design:guard, no npm test and no visual gate, because
# the only push-time CI guard (check-local-ci-evidence.mjs) gates refs/heads/main alone, and the
# hook skipped the visual gate silently when its script was missing. This script closes that:
#
#   installed by  tools/agent-hooks/install-stg-push-guard.sh (`make ai-setup`) as
#                 <hooks>/goatos-admin-web-push-gate.sh, called by <hooks>/pre-push
#                 (tools/agent-hooks/pre-push.hook) for every pushed ref;
#   guarded by    tools/ci/check-push-hook-freshness.sh (run-local-ci common job): the installed
#                 copy must equal this file, --list-lanes must name every lane below, and the
#                 installed hook is executed against a feature-branch push that must be BLOCKED.
#
# Lanes (all must pass for the pushed commit; FAIL CLOSED on anything missing):
#   design-guard   npm --prefix apps/admin-web run design:guard   (self-test + shrink-only ratchets)
#   typecheck      npm --prefix apps/admin-web run typecheck
#   unit-tests     npm --prefix apps/admin-web test
#   next-build     npm --prefix apps/admin-web run build           (machine build slot)
#   visual-gate    admin-web-visual-gate.sh --pre-push             (5 shell routes + touched routes,
#                  scan + interactions + skeleton-on-touched 1440/390; reuses the next-build output)
#   storybook-build npm --prefix apps/admin-web run build-storybook (machine build slot) when the
#                  pushed commits change stories/, .storybook/, components/, theme/ or the app's
#                  package.json / the lockfile (STORYBOOK_INPUTS). J1B P0-1: deleting the legacy
#                  stylesheets broke `storybook build` (preview.css still imported them) and no push
#                  lane built Storybook, so the story visual lanes of run-local-ci were dead unseen.
#                  Not applicable otherwise (printed); `--run storybook-build` always builds.
#
# Scope: the lanes apply when the pushed commits change an admin-web input (ADMIN_WEB_INPUTS);
# a push with no such change prints "not applicable" and passes. A branch with no merge base is
# treated as touching everything. Deleting a ref is not gated.
#
# Reuse: a lane that passed for the same admin-web INPUT TREE (digest of the committed trees of
# ADMIN_WEB_INPUTS at the pushed commit) is not re-run, so a rebase that did not change admin-web
# inputs, or a retried push, costs seconds. Run the lanes before pushing, one command each (the
# agent watchdog kills a single command after 10 minutes):
#     tools/ci/admin-web-push-gate.sh --run design-guard
#     tools/ci/admin-web-push-gate.sh --run typecheck
#     tools/ci/admin-web-push-gate.sh --run unit-tests
#     tools/ci/admin-web-push-gate.sh --run next-build
#     tools/ci/admin-web-push-gate.sh --run visual-gate
#   then `git push` reuses every PASS recorded for that input tree.
#
# The lanes judge the WORKING TREE, so the pushed commit must be HEAD and the admin-web inputs
# must be clean (commit or stash first). There is no skip flag for these lanes; the visual gate's
# own GOATOS_SKIP_ADMIN_WEB_VISUAL_GATE needs GOATOS_SKIP_REASON and lands in the skip ledger.
#
# RECEIPT (J1B P0-2 / CI gap 2): every --pre-push run, applicable or not, writes a gate receipt per
# pushed ref (tools/ci/admin-web-push-receipt.mjs: lanes, pass / reused-pass / skip + reason, SHA,
# digest, skip-ledger rows, the commits the push covers) and publishes it on the PR in the
# background as the goatos/push-gate commit status (red with the reason when a lane was skipped).
# `make land-main` refuses a range with a commit no receipt covers. To cover commits that never went
# through this gate (pushed elsewhere, or before the gate existed), run every lane on HEAD and record
# a receipt for base..HEAD:
#     tools/ci/admin-web-push-gate.sh --certify origin/main
# GOATOS_PUSH_RECEIPT_PUBLISH=0 keeps the receipt local (tests); GOATOS_PUSH_RECEIPT_DIR moves it.
set -euo pipefail

LANES=(design-guard typecheck unit-tests next-build visual-gate storybook-build)
STORYBOOK_INPUTS='^(apps/admin-web/(stories/|\.storybook/|components/|theme/|package\.json$)|package-lock\.json$)'
ADMIN_WEB_INPUTS='^(apps/admin-web/|docs/design/|package\.json$|package-lock\.json$|tools/ci/admin-web-|tools/ci/goatos-skip-ledger\.sh$|backend/internal/adminui/)'
INPUT_PATHS=(apps/admin-web docs/design package.json package-lock.json tools/ci/admin-web-visual-gate.sh tools/ci/admin-web-push-gate.sh tools/ci/admin-web-push-receipt.mjs tools/ci/goatos-skip-ledger.sh backend/internal/adminui)

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=tools/ci/goatos-skip-ledger.sh
if [ -f "$here/goatos-skip-ledger.sh" ]; then . "$here/goatos-skip-ledger.sh"; else
  echo "admin-web-push-gate: BLOCKED — $here/goatos-skip-ledger.sh is missing (run make ai-setup)" >&2
  exit 1
fi

block() {
  echo "admin-web-push-gate: BLOCKED — $*" >&2
  exit 1
}
# The receipt writer beside this file: the staged hook-bundle copy, else the repo source.
receipt_tool="$here/goatos-admin-web-push-receipt.mjs"
[ -f "$receipt_tool" ] || receipt_tool="$here/admin-web-push-receipt.mjs"
[ -f "$receipt_tool" ] || block "the push-gate receipt writer is missing ($here/goatos-admin-web-push-receipt.mjs / admin-web-push-receipt.mjs)"

mode="${1:-}"
[ -n "$mode" ] && shift || true
case "$mode" in
  --list-lanes) printf '%s\n' "${LANES[@]}"; exit 0 ;;
  --pre-push|--run) ;;
  --certify)
    certify_base="${1:-}"
    [ -n "$certify_base" ] || { echo "usage: $0 --certify <base-rev>   (e.g. origin/main)" >&2; exit 2; }
    shift
    ;;
  *) echo "usage: $0 --pre-push [remote url] (payload on stdin) | --run [lane...] | --certify <base> | --list-lanes" >&2; exit 2 ;;
esac

repo="$(git rev-parse --show-toplevel 2>/dev/null)" || block "not inside a git work tree"
cd "$repo"
web="$repo/apps/admin-web"
# The visual gate beside this file: the installed copy in the hooks dir, else the repo source.
visual_gate="$here/goatos-admin-web-visual-gate.sh"
[ -f "$visual_gate" ] || visual_gate="$here/admin-web-visual-gate.sh"

zero="0000000000000000000000000000000000000000"
payload="$(mktemp "${TMPDIR:-/tmp}/admin-web-push-gate.XXXXXX")"
results="$(mktemp "${TMPDIR:-/tmp}/admin-web-push-gate-results.XXXXXX")"
push_remote="${2:-${1:-origin}}" # pre-push hook args: <remote name> <remote url>
slot=""
cleanup() {
  [ -n "$slot" ] && { rm -f "$slot/owner"; rmdir "$slot" 2>/dev/null || true; }
  rm -f "$payload" "$results"
}
trap cleanup EXIT

head_sha="$(git rev-parse HEAD)"

# write_receipts APPLICABLE — one receipt per pushed ref (pre-push) or for base..HEAD (certify).
# Fail closed: a push whose receipt cannot be written is refused (land-main would refuse it later).
write_receipts() {
  local applicable="$1" out rc
  local ledger; ledger="$(goatos_skip_ledger_path)"
  if [ "$mode" = "--certify" ]; then
    out="$(node "$receipt_tool" write --kind certify --sha "$head_sha" --ref certify --base "$certify_base" \
      --digest "${digest:-}" --results "$results" --ledger "$ledger" --applicable "$applicable")" || block "could not write the gate receipt"
    echo "admin-web-push-gate: receipt $out"
    return 0
  fi
  while read -r _lref lsha rref rsha; do
    [ -n "${lsha:-}" ] || continue
    [ "$lsha" = "$zero" ] && continue
    local app="$applicable" dg="${digest:-}"
    [ "$lsha" = "$head_sha" ] || { app=0; dg=""; } # a non-admin-web ref pushed from another commit
    rc=0
    out="$(node "$receipt_tool" write --kind pre-push --sha "$lsha" --ref "$rref" --remote-sha "${rsha:-$zero}" \
      --remote "$push_remote" --digest "$dg" --results "$([ "$app" = 1 ] && echo "$results" || echo /dev/null)" \
      --ledger "$ledger" --applicable "$app")" || rc=$?
    [ "$rc" = 0 ] || block "could not write the gate receipt for ${rref} (see above); the push is refused"
    echo "admin-web-push-gate: receipt $out"
    if [ "${GOATOS_PUSH_RECEIPT_PUBLISH:-1}" != "0" ]; then
      local log; log="$(dirname "$out")/publish.log"
      nohup node "$receipt_tool" publish --receipt "$out" --remote "$push_remote" >>"$log" 2>&1 </dev/null &
      echo "admin-web-push-gate: publishing it on the PR as the goatos/push-gate status once ${lsha:0:12} is on ${rref} (log: $log)"
    fi
  done <"$payload"
}

declare -a want_lanes=()
if [ "$mode" = "--pre-push" ]; then
  cat >"$payload"
  needs=0
  storybook_needed=0
  while read -r _lref lsha rref rsha; do
    [ -n "${lsha:-}" ] || continue
    [ "$lsha" = "$zero" ] && continue # deleting a ref: nothing to build
    if [ "${rsha:-$zero}" != "$zero" ] && git cat-file -e "$rsha^{commit}" 2>/dev/null; then
      changed="$(git diff --name-only "$rsha" "$lsha" 2>/dev/null || echo '<diff-failed>')"
    else
      base="$(git merge-base "$lsha" origin/main 2>/dev/null || true)"
      if [ -n "$base" ]; then
        changed="$(git diff --name-only "$base" "$lsha" 2>/dev/null || echo '<diff-failed>')"
      else
        changed="<no-merge-base>" # unrelated history: treat as touching everything
      fi
    fi
    if [ "$changed" = "<no-merge-base>" ] || [ "$changed" = "<diff-failed>" ] || printf '%s\n' "$changed" | grep -Eq "$ADMIN_WEB_INPUTS"; then
      echo "admin-web-push-gate: ${rref:-?} changes admin-web inputs -> lanes: ${LANES[*]}"
      [ "$lsha" = "$head_sha" ] || block "the push sends ${lsha:0:12} to ${rref:-?} but this work tree's HEAD is ${head_sha:0:12}. The lanes judge the work tree: push from the worktree whose HEAD is the pushed commit."
      needs=1
      if [ "$changed" = "<no-merge-base>" ] || [ "$changed" = "<diff-failed>" ] || printf '%s\n' "$changed" | grep -Eq "$STORYBOOK_INPUTS"; then storybook_needed=1; fi
    fi
  done <"$payload"
  if [ "$needs" = "0" ]; then
    echo "admin-web-push-gate: the pushed commits change no admin-web input; admin-web lanes not applicable"
    write_receipts 0
    exit 0
  fi
  want_lanes=("${LANES[@]}")
elif [ "$mode" = "--certify" ]; then
  git rev-parse --verify -q "$certify_base^{commit}" >/dev/null || block "--certify base '$certify_base' is not a commit"
  want_lanes=("${LANES[@]}")
else
  if [ "$#" -gt 0 ]; then
    want_lanes=("$@")
    storybook_needed=1 # a lane named on the command line always runs
  else
    want_lanes=("${LANES[@]}")
    sb_base="$(git merge-base HEAD origin/main 2>/dev/null || true)"
    if [ -z "$sb_base" ] || git diff --name-only "$sb_base" HEAD 2>/dev/null | grep -Eq "$STORYBOOK_INPUTS"; then storybook_needed=1; else storybook_needed=0; fi
  fi
  for l in "${want_lanes[@]}"; do
    case " ${LANES[*]} " in *" $l "*) ;; *) echo "unknown lane: $l (lanes: ${LANES[*]})" >&2; exit 2 ;; esac
  done
fi

# ── preflight: a missing input FAILS, it never skips ─────────────────────────
[ -f "$web/package.json" ] || block "apps/admin-web/package.json is missing in $repo"
[ -d "$web/node_modules" ] || [ -d "$repo/node_modules" ] || block "no node_modules: run npm ci at the repo root first"
for s in design:guard typecheck test build build-storybook; do
  node -e 'const p=require(process.argv[1]); if(!p.scripts||!p.scripts[process.argv[2]]) process.exit(1)' "$web/package.json" "$s" \
    || block "apps/admin-web/package.json has no \"$s\" script"
done
[ -f "$visual_gate" ] || block "the admin-web visual gate script is missing ($here/goatos-admin-web-visual-gate.sh / admin-web-visual-gate.sh); run make ai-setup"
[ -f "$web/scripts/r2-visual-audit.mjs" ] || block "apps/admin-web/scripts/r2-visual-audit.mjs is missing"
dirty="$(git status --porcelain --untracked-files=normal -- "${INPUT_PATHS[@]}" 2>/dev/null || true)"
[ -z "$dirty" ] || block "uncommitted admin-web inputs (the lanes judge the work tree, so the result would not describe the pushed commit):
$dirty
commit or stash them, then push again"

digest="$(git ls-tree -r "$head_sha" -- "${INPUT_PATHS[@]}" | git hash-object --stdin)"
common="$(cd "$(git rev-parse --git-common-dir)" && pwd -P)"
markers="$common/goatos-push-gate/$digest"
mkdir -p "$markers"

take_build_slot() {
  local slots="${GOATOS_ADMIN_BUILD_SLOTS:-/tmp/admin-build-slot-1 /tmp/admin-build-slot-2}"
  [ "$slots" = "none" ] && return 0
  for _ in $(seq 1 120); do
    for s in $slots; do
      if mkdir "$s" 2>/dev/null; then slot="$s"; echo "push-gate $$ $repo" >"$s/owner"; return 0; fi
    done
    sleep 10
  done
  block "no admin-web build slot free after 20 min ($slots): $(cat $(printf '%s/owner ' $slots) 2>/dev/null | tr '\n' ' ')"
}

run_lane() { # lane
  case "$1" in
    design-guard) npm --prefix "$web" run design:guard ;;
    typecheck) npm --prefix "$web" run typecheck ;;
    unit-tests) npm --prefix "$web" test ;;
    next-build)
      take_build_slot
      local rc=0
      npm --prefix "$web" run build || rc=$?
      if [ -n "$slot" ]; then rm -f "$slot/owner"; rmdir "$slot" 2>/dev/null || true; slot=""; fi
      return "$rc"
      ;;
    storybook-build)
      take_build_slot
      local rc=0
      npm --prefix "$web" run build-storybook || rc=$?
      if [ -n "$slot" ]; then rm -f "$slot/owner"; rmdir "$slot" 2>/dev/null || true; slot=""; fi
      return "$rc"
      ;;
    visual-gate)
      if [ "${GOATOS_SKIP_ADMIN_WEB_VISUAL_GATE:-}" = "1" ]; then
        goatos_require_skip_reason GOATOS_SKIP_ADMIN_WEB_VISUAL_GATE "admin-web push gate: visual-gate" || return 1
        return 99 # skipped with a recorded reason: not a PASS, no marker
      fi
      if [ "$mode" = "--pre-push" ]; then bash "$visual_gate" --pre-push <"$payload"; else bash "$visual_gate" --fast; fi
      ;;
  esac
}

echo "admin-web-push-gate: ${head_sha:0:12} input tree ${digest:0:12} -> ${want_lanes[*]}"
for lane in "${want_lanes[@]}"; do
  if [ "$lane" = "storybook-build" ] && [ "${storybook_needed:-1}" = "0" ]; then
    echo "admin-web-push-gate: storybook-build — not applicable (no change under stories/, .storybook/, components/, theme/, package.json)"
    printf '%s\tnot-applicable\t0\t\n' "$lane" >>"$results"
    continue
  fi
  if [ -f "$markers/$lane.pass" ]; then
    echo "admin-web-push-gate: ${lane} — reused PASS (same admin-web input tree, $(cut -f1 "$markers/$lane.pass"))"
    printf '%s\treused-pass\t0\t\n' "$lane" >>"$results"
    continue
  fi
  echo "── admin-web-push-gate: ${lane}"
  t0=$SECONDS
  rc=0
  run_lane "$lane" || rc=$?
  if [ "$rc" = "99" ]; then
    echo "admin-web-push-gate: ${lane} — SKIPPED with a recorded reason (not a pass; the receipt and the PR status carry the reason)"
    printf '%s\tskip\t%s\t%s\n' "$lane" "$((SECONDS - t0))" "$(printf '%s' "${GOATOS_SKIP_REASON:-}" | tr '\t\n' '  ')" >>"$results"
    continue
  fi
  if [ "$rc" != "0" ]; then
    goatos_print_skip_ledger "$head_sha"
    block "lane ${lane} failed (exit ${rc}). Fix it, then re-run just this lane: tools/ci/admin-web-push-gate.sh --run ${lane}"
  fi
  printf '%s\t%s\t%ss\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$head_sha" "$((SECONDS - t0))" >"$markers/$lane.pass"
  printf '%s\tpass\t%s\t\n' "$lane" "$((SECONDS - t0))" >>"$results"
  echo "admin-web-push-gate: ${lane} — PASS ($((SECONDS - t0))s)"
done
goatos_print_skip_ledger "$head_sha"
if [ "$mode" != "--run" ]; then write_receipts 1; fi
echo "admin-web-push-gate: OK ${head_sha:0:12} (${want_lanes[*]})"
