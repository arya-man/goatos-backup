#!/usr/bin/env bash
# admin-web-visual-gate.sh — `make admin-web-visual-gate` / `npm --prefix apps/admin-web run visual:gate`.
#
# Builds admin-web from THIS checkout, starts it on a free port against the local API, runs
# apps/admin-web/scripts/r2-visual-audit.mjs over every app/(admin) route (1440 dark, 1440 light,
# 390 dark + tab/filter clicks, drawers, skeleton-vs-loaded) and exits non-zero on a P0 pattern:
# full-page flash / document reload on a tab or filter change, bright background in dark mode,
# off-palette colour, drawer clipping or no backdrop, skeleton block IoU < 0.8, tap target < 44px
# at 390, page sideways scroll, crash / HTTP >= 400.
#
# P0 debt is a shrink-only ratchet (apps/admin-web/scripts/r2-visual-audit-baseline.json): a P0
# pattern that is NEW or reaches MORE routes fails. GOATOS_VISUAL_GATE_STRICT=1 fails on every P0.
# Never grow the baseline to land a change; fix the pattern, then --write-baseline to shrink it.
#
# Modes:
#   (default)      build + start own server + audit + stop own server
#   --pre-push     read the pre-push payload on stdin; run the gate only when the pushed commits
#                  touch admin-web UI (app/components/features/lib/styles/stories, css, package.json)
#
# Env:
#   GOATOS_SKIP_ADMIN_WEB_VISUAL_GATE=1  explicit opt-out (printed loudly; say why in the PR)
#   GOATOS_ADMIN_WEB_BASE_URL            audit this running app instead of building one
#   GOATOS_API_BASE_URL                  local API for the built app (default http://127.0.0.1:8080)
#   GOATOS_WEB_ENV_FILE                  optional env file sourced before `next start` (auth/API env)
#   GOATOS_VISUAL_GATE_TEMPLATE_URL      template for side-by-sides (default http://127.0.0.1:3480)
#   GOATOS_VISUAL_GATE_ARGS              extra args for r2-visual-audit.mjs (e.g. "--only sales")
set -euo pipefail

mode="${1:-run}"
repo="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
web="$repo/apps/admin-web"
audit="$web/scripts/r2-visual-audit.mjs"

skip() { echo "admin-web-visual-gate: $*"; exit 0; }

if [ "${GOATOS_SKIP_ADMIN_WEB_VISUAL_GATE:-}" = "1" ]; then
  echo "!! admin-web-visual-gate: SKIPPED by GOATOS_SKIP_ADMIN_WEB_VISUAL_GATE=1 — state why in the PR/handoff" >&2
  exit 0
fi
[ -f "$audit" ] || skip "no $audit in this checkout; nothing to gate"

if [ "$mode" = "--pre-push" ]; then
  zero="0000000000000000000000000000000000000000"
  touched=0
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
    if git diff --name-only "$range" -- \
        'apps/admin-web/app' 'apps/admin-web/components' 'apps/admin-web/features' \
        'apps/admin-web/lib' 'apps/admin-web/styles' 'apps/admin-web/package.json' \
        | grep -v -E '\.(test|spec)\.m?[jt]sx?$|\.md$' | grep -q .; then
      touched=1
    fi
  done
  [ "$touched" = "1" ] || skip "push does not touch admin-web UI; skipped"
  echo "admin-web-visual-gate: push touches admin-web UI -> running the visual gate (opt out: GOATOS_SKIP_ADMIN_WEB_VISUAL_GATE=1)"
fi

gate_args=(--gate)
[ "${GOATOS_VISUAL_GATE_STRICT:-}" = "1" ] && gate_args+=(--strict)
tpl="${GOATOS_VISUAL_GATE_TEMPLATE_URL:-http://127.0.0.1:3480}"
# shellcheck disable=SC2206
extra=(${GOATOS_VISUAL_GATE_ARGS:-})

if [ -n "${GOATOS_ADMIN_WEB_BASE_URL:-}" ]; then
  echo "admin-web-visual-gate: auditing running app $GOATOS_ADMIN_WEB_BASE_URL"
  exec node "$audit" --base "$GOATOS_ADMIN_WEB_BASE_URL" --template "$tpl" "${gate_args[@]}" "${extra[@]}"
fi

# One gate build at a time on this machine (RAM). Not a wait loop: a held lock fails fast.
lock="$(git rev-parse --git-common-dir)/goatos-admin-web-visual-gate.lock"
if ! mkdir "$lock" 2>/dev/null; then
  holder="$(cat "$lock/pid" 2>/dev/null || true)"
  if [ -n "$holder" ] && kill -0 "$holder" 2>/dev/null; then
    echo "!! admin-web-visual-gate: another gate (pid $holder) is building; retry when it finishes" >&2
    exit 1
  fi
  rm -rf "$lock"; mkdir "$lock"
fi
echo $$ >"$lock/pid"

api="${GOATOS_API_BASE_URL:-http://127.0.0.1:8080}"
server_pid=""
cleanup() {
  # stop ONLY the server this gate started
  [ -n "$server_pid" ] && kill "$server_pid" 2>/dev/null || true
  rm -rf "$lock"
}
trap cleanup EXIT

if ! curl -fsS -o /dev/null --max-time 5 "$api/readyz" && ! curl -fsS -o /dev/null --max-time 5 "$api/healthz"; then
  echo "!! admin-web-visual-gate: local API $api is not healthy; start it (make dev-local-service-start) or set GOATOS_API_BASE_URL / GOATOS_ADMIN_WEB_BASE_URL" >&2
  exit 1
fi

port="$(node -e 'const s=require("net").createServer();s.listen(0,"127.0.0.1",()=>{console.log(s.address().port);s.close()})')"
log="$(mktemp "${TMPDIR:-/tmp}/admin-web-visual-gate.XXXXXX")"
echo "admin-web-visual-gate: building admin-web ($(git -C "$repo" rev-parse --short HEAD)) -> port $port, API $api, log $log"
(
  cd "$web"
  [ -n "${GOATOS_WEB_ENV_FILE:-}" ] && . "$GOATOS_WEB_ENV_FILE"
  export GOATOS_API_BASE_URL="$api"
  npm run build >"$log" 2>&1
) || { echo "!! admin-web-visual-gate: next build failed (tail of $log):" >&2; tail -30 "$log" >&2; exit 1; }

(
  cd "$web"
  [ -n "${GOATOS_WEB_ENV_FILE:-}" ] && . "$GOATOS_WEB_ENV_FILE"
  export GOATOS_API_BASE_URL="$api"
  exec npx next start -H 127.0.0.1 -p "$port" >>"$log" 2>&1
) &
server_pid=$!
for _ in $(seq 1 60); do
  curl -fsS -o /dev/null --max-time 2 "http://127.0.0.1:$port/login" && break
  kill -0 "$server_pid" 2>/dev/null || { echo "!! admin-web-visual-gate: next start exited (tail of $log):" >&2; tail -30 "$log" >&2; exit 1; }
  sleep 1
done

node "$audit" --base "http://127.0.0.1:$port" --template "$tpl" "${gate_args[@]}" "${extra[@]}"
