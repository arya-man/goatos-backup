#!/usr/bin/env bash
# admin-web-visual-gate.sh — `make admin-web-visual-gate` / `npm --prefix apps/admin-web run visual:gate`.
#
# Builds admin-web from THIS checkout (or reuses a clean `npm run build` of HEAD), starts it on a
# free port against the local API, runs apps/admin-web/scripts/r2-visual-audit.mjs and exits
# non-zero on a gate failure: full-page flash / document reload on a tab or filter change, bright
# background in dark mode, off-palette colour, drawer clipping or no backdrop, skeleton block
# IoU < 0.8, tap target < 44px at 390, page sideways scroll, crash / HTTP >= 400, and the SHELL
# checks (scripts/r2-audit-checks/shell.mjs: stylesheet failed, sidebar column/padding vs template,
# header IconButton background, filter overlapping tabs, sort header rendered as a link).
#
# Ratchet (apps/admin-web/scripts/r2-visual-audit-baseline.json, shrink-only):
#   - a P0 pattern that is NEW or reaches MORE routes fails;
#   - on the shell routes and on every route the change touched, ANY failure pattern the route's
#     baseline entry does not list fails (P0 or not).
# GOATOS_VISUAL_GATE_STRICT=1 fails on every P0. Never grow the baseline to land a change; fix the
# pattern, then run a FULL audit with --write-baseline to shrink it.
#
# Modes:
#   (default)          full run: every app/(admin) route, all checks (land-check / ci-local)
#   --fast             pre-push lane (~2-3 min): 5 shell routes + the routes the change touched
#                      (import graph of the changed files vs the upstream base), scan + interactions
#   --pre-push         --fast, with the touched files taken from the pre-push payload on stdin; skipped
#                      when the pushed commits do not touch admin-web UI
#   any further args   passed to r2-visual-audit.mjs (e.g. --routes /verify,/approvals  --only sales)
#
# Env:
#   GOATOS_SKIP_ADMIN_WEB_VISUAL_GATE=1  explicit opt-out (printed loudly; say why in the PR)
#   GOATOS_ADMIN_WEB_BASE_URL            audit this running app instead of building one
#   GOATOS_WEB_ENV_FILE                  env file sourced FIRST (API URL, local auth; on Ravi's laptop
#                                        ~/mesha/goatos-wt-manual/.manual-logs/web-env.sh)
#   GOATOS_API_BASE_URL                  local API for the built app (default http://127.0.0.1:8080)
#   GOATOS_VISUAL_GATE_BASE              git ref the change is measured against (--fast); default the
#                                        upstream, else the origin branch HEAD is closest to
#   GOATOS_VISUAL_GATE_TEMPLATE_URL      template for side-by-sides (default http://127.0.0.1:3480)
#   GOATOS_VISUAL_GATE_ARGS              extra args for r2-visual-audit.mjs
#   GOATOS_ADMIN_BUILD_SLOTS             machine build slots to take before `next build`
#                                        (default "/tmp/admin-build-slot-1 /tmp/admin-build-slot-2";
#                                        "none" disables)
set -euo pipefail

mode="run"
case "${1:-}" in --pre-push|--fast|run) mode="$1"; shift ;; esac
passthrough=("$@")
repo="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
web="$repo/apps/admin-web"
audit="$web/scripts/r2-visual-audit.mjs"

skip() { echo "admin-web-visual-gate: $*"; exit 0; }

if [ "${GOATOS_SKIP_ADMIN_WEB_VISUAL_GATE:-}" = "1" ]; then
  echo "!! admin-web-visual-gate: SKIPPED by GOATOS_SKIP_ADMIN_WEB_VISUAL_GATE=1 — state why in the PR/handoff" >&2
  exit 0
fi
[ -f "$audit" ] || skip "no $audit in this checkout; nothing to gate"
if [ -n "${GOATOS_WEB_ENV_FILE:-}" ]; then
  # shellcheck disable=SC1090
  . "$GOATOS_WEB_ENV_FILE"
fi

UI_PATHS=('apps/admin-web/app' 'apps/admin-web/components' 'apps/admin-web/features' 'apps/admin-web/lib'
  'apps/admin-web/layouts' 'apps/admin-web/theme' 'apps/admin-web/styles' 'apps/admin-web/package.json')
touched_file="$(mktemp "${TMPDIR:-/tmp}/admin-web-visual-gate-touched.XXXXXX")"
server_pid="" lock="" slot=""
cleanup() {
  # stop ONLY the server this gate started; release only what this gate took
  [ -n "$server_pid" ] && kill "$server_pid" 2>/dev/null || true
  [ -n "$slot" ] && { rm -f "$slot/owner"; rmdir "$slot" 2>/dev/null || true; }
  [ -n "$lock" ] && rm -rf "$lock"
  rm -f "$touched_file"
}
trap cleanup EXIT

ui_files() { git diff --name-only "$1" -- "${UI_PATHS[@]}" | grep -v -E '\.(test|spec)\.m?[jt]sx?$|\.md$|\.stories\.tsx$' || true; }

if [ "$mode" = "--pre-push" ]; then
  zero="0000000000000000000000000000000000000000"
  while read -r _lref lsha _rref rsha; do
    [ -n "${lsha:-}" ] || continue
    [ "$lsha" = "$zero" ] && continue # delete
    if [ "$rsha" != "$zero" ] && git cat-file -e "$rsha^{commit}" 2>/dev/null; then
      range="$rsha..$lsha"
    else
      base="$(git merge-base "$lsha" origin/main 2>/dev/null || true)"
      [ -n "$base" ] || continue
      range="$base..$lsha"
    fi
    ui_files "$range" >>"$touched_file"
  done
  [ -s "$touched_file" ] || skip "push does not touch admin-web UI; skipped"
  echo "admin-web-visual-gate: push touches admin-web UI ($(sort -u "$touched_file" | wc -l | tr -d ' ') files) -> fast visual gate (opt out: GOATOS_SKIP_ADMIN_WEB_VISUAL_GATE=1)"
  mode="--fast"
elif [ "$mode" = "--fast" ]; then
  base_ref="${GOATOS_VISUAL_GATE_BASE:-}"
  if [ -z "$base_ref" ]; then
    base_ref="$(git rev-parse --abbrev-ref --symbolic-full-name '@{u}' 2>/dev/null || true)"
  fi
  if [ -z "$base_ref" ]; then
    # detached rebase workflow: the origin branch HEAD is fewest commits ahead of
    best=""; best_n=999999
    while read -r ref; do
      mb="$(git merge-base HEAD "$ref" 2>/dev/null || true)"; [ -n "$mb" ] || continue
      n="$(git rev-list --count "$mb..HEAD")"
      if [ "$n" -lt "$best_n" ]; then best="$ref"; best_n="$n"; fi
    done < <(git for-each-ref --sort=-committerdate --count=40 --format='%(refname:short)' refs/remotes/origin | grep -v '/HEAD$')
    base_ref="$best"
  fi
  if [ -n "$base_ref" ]; then
    mb="$(git merge-base HEAD "$base_ref")"
    { ui_files "$mb..HEAD"; git diff --name-only HEAD -- "${UI_PATHS[@]}"; } >>"$touched_file"
    echo "admin-web-visual-gate: --fast vs $base_ref ($(git rev-list --count "$mb..HEAD") commits, $(sort -u "$touched_file" | grep -c . || true) UI files touched)"
  else
    echo "admin-web-visual-gate: --fast without a base ref: shell routes only"
  fi
fi

gate_args=(--gate)
[ "${GOATOS_VISUAL_GATE_STRICT:-}" = "1" ] && gate_args+=(--strict)
if [ "$mode" = "--fast" ]; then
  sort -u -o "$touched_file" "$touched_file"
  gate_args+=(--fast --touched-files-from "$touched_file")
fi
tpl="${GOATOS_VISUAL_GATE_TEMPLATE_URL:-http://127.0.0.1:3480}"
# shellcheck disable=SC2206
extra=(${GOATOS_VISUAL_GATE_ARGS:-})
extra+=("${passthrough[@]+"${passthrough[@]}"}")

if [ -n "${GOATOS_ADMIN_WEB_BASE_URL:-}" ]; then
  echo "admin-web-visual-gate: auditing running app $GOATOS_ADMIN_WEB_BASE_URL"
  node "$audit" --base "$GOATOS_ADMIN_WEB_BASE_URL" --template "$tpl" "${gate_args[@]}" "${extra[@]+"${extra[@]}"}"
  exit $?
fi

# One gate server at a time per checkout family. Not a wait loop: a held lock fails fast.
lock="$(git rev-parse --git-common-dir)/goatos-admin-web-visual-gate.lock"
if ! mkdir "$lock" 2>/dev/null; then
  holder="$(cat "$lock/pid" 2>/dev/null || true)"
  if [ -n "$holder" ] && kill -0 "$holder" 2>/dev/null; then
    lock=""
    echo "!! admin-web-visual-gate: another gate (pid $holder) is running; retry when it finishes" >&2
    exit 1
  fi
  rm -rf "$lock"; mkdir "$lock"
fi
echo $$ >"$lock/pid"

api="${GOATOS_API_BASE_URL:-http://127.0.0.1:8080}"
if ! curl -fsS -o /dev/null --max-time 5 "$api/readyz" && ! curl -fsS -o /dev/null --max-time 5 "$api/healthz"; then
  echo "!! admin-web-visual-gate: local API $api is not healthy; start it (make dev-local-service-start), set GOATOS_WEB_ENV_FILE / GOATOS_API_BASE_URL, or audit a running app with GOATOS_ADMIN_WEB_BASE_URL" >&2
  exit 1
fi

log="$(mktemp "${TMPDIR:-/tmp}/admin-web-visual-gate.XXXXXX")"
head_sha="$(git -C "$repo" rev-parse HEAD)"
reuse=0
if [ -f "$web/.next/local-build-provenance.json" ] && [ -f "$web/.next/BUILD_ID" ]; then
  reuse="$(node -e '
    const fs = require("fs"); const [p, id, sha] = process.argv.slice(1);
    const j = JSON.parse(fs.readFileSync(p, "utf8"));
    console.log(j.git_sha === sha && j.clean_source === true && j.build_id === fs.readFileSync(id, "utf8").trim() ? 1 : 0);
  ' "$web/.next/local-build-provenance.json" "$web/.next/BUILD_ID" "$head_sha" 2>/dev/null || echo 0)"
fi
if [ "$reuse" = "1" ] && [ -z "$(git -C "$repo" status --porcelain -- "${UI_PATHS[@]}")" ]; then
  echo "admin-web-visual-gate: reusing the clean npm run build of ${head_sha:0:9} in apps/admin-web/.next"
else
  # take a machine build slot (RAM): bounded wait, then fail with the holders named
  slots="${GOATOS_ADMIN_BUILD_SLOTS:-/tmp/admin-build-slot-1 /tmp/admin-build-slot-2}"
  if [ "$slots" != "none" ]; then
    for _ in $(seq 1 120); do
      for s in $slots; do if mkdir "$s" 2>/dev/null; then slot="$s"; echo "visual-gate $$" >"$s/owner"; break 2; fi; done
      sleep 10
    done
    if [ -z "$slot" ]; then
      echo "!! admin-web-visual-gate: no build slot free after 20 min ($slots): $(cat $(printf '%s/owner ' $slots) 2>/dev/null | tr '\n' ' ')" >&2
      exit 1
    fi
  fi
  echo "admin-web-visual-gate: building admin-web (${head_sha:0:9}) with npm run build, log $log"
  ( cd "$web" && export GOATOS_API_BASE_URL="$api" && npm run build >"$log" 2>&1 ) \
    || { echo "!! admin-web-visual-gate: next build failed (tail of $log):" >&2; tail -30 "$log" >&2; exit 1; }
  if [ -n "$slot" ]; then rm -f "$slot/owner"; rmdir "$slot" 2>/dev/null || true; slot=""; fi
fi

port="$(node -e 'const s=require("net").createServer();s.listen(0,"127.0.0.1",()=>{console.log(s.address().port);s.close()})')"
(
  cd "$web"
  export GOATOS_API_BASE_URL="$api"
  # local bearer mode: SSR self-mints a short-lived token per request (lib/api/local-dev-token.ts);
  # the proxy only needs to know a token source exists, or every route bounces to /login.
  if [ "${GOATOS_ENV:-}" = "local" ] && [ "${GOATOS_AUTH_MODE:-}" = "bearer" ]; then
    export GOATOS_AUTH_HS256_SECRET="${GOATOS_AUTH_HS256_SECRET:-goatos-local-dev-secret-32-bytes-min}"
    export GOATOS_BEARER_TOKEN="${GOATOS_BEARER_TOKEN:-local-self-mint}"
  fi
  exec npx next start -H 127.0.0.1 -p "$port" >>"$log" 2>&1
) &
server_pid=$!
for _ in $(seq 1 60); do
  curl -fsS -o /dev/null --max-time 2 "http://127.0.0.1:$port/login" && break
  kill -0 "$server_pid" 2>/dev/null || { echo "!! admin-web-visual-gate: next start exited (tail of $log):" >&2; tail -30 "$log" >&2; exit 1; }
  sleep 1
done
if curl -sS -o /dev/null -w '%{redirect_url}' --max-time 60 "http://127.0.0.1:$port/verify" | grep -q '/login'; then
  echo "!! admin-web-visual-gate: the built app redirects to /login — no local auth. Set GOATOS_WEB_ENV_FILE (GOATOS_ENV=local, GOATOS_AUTH_MODE=bearer, GOATOS_API_BASE_URL) or GOATOS_ADMIN_WEB_BASE_URL" >&2
  exit 1
fi

node "$audit" --base "http://127.0.0.1:$port" --template "$tpl" "${gate_args[@]}" "${extra[@]+"${extra[@]}"}"
