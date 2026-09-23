#!/usr/bin/env bash
# refresh-data-map.sh: keep the Mesha data map (.agents/skills/mesha-data-map +
# tools/ask-mesha-agent/data-map-core.md) current with origin/main + goatos-stg.
#
# Idempotent. Never touches the caller's checkout: works in its own worktree of
# origin/main under $STATE_DIR. Opens a PR on chore/data-map-refresh-YYYYMMDD;
# NEVER merges and NEVER pushes to main (the PR must land via `make land-main`).
#
#   tools/ask-mesha-agent/refresh-data-map.sh            # real run
#   tools/ask-mesha-agent/refresh-data-map.sh --dry-run  # detect + report only
#
# Env:
#   GOATOS_REPO       repo to fetch from (default: repo containing this script)
#   STATE_DIR         default ~/.local/state/mesha-data-map (worktree, state, log)
#   PGENV_FILE        default /Users/raviteja/airnd/agent-local/.pgenv (read-only DB user)
#   CLAUDE_BIN        default claude
set -euo pipefail

DRY_RUN=0
for a in "$@"; do
  case "$a" in
    --dry-run) DRY_RUN=1 ;;
    -h|--help) sed -n '2,17p' "$0"; exit 0 ;;
    *) echo "unknown arg: $a" >&2; exit 2 ;;
  esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GOATOS_REPO="${GOATOS_REPO:-$(git -C "$SCRIPT_DIR" rev-parse --show-toplevel)}"
STATE_DIR="${STATE_DIR:-$HOME/.local/state/mesha-data-map}"
PGENV_FILE="${PGENV_FILE:-/Users/raviteja/airnd/agent-local/.pgenv}"
CLAUDE_BIN="${CLAUDE_BIN:-claude}"
WT="$STATE_DIR/main-worktree"
STATE_FILE="$STATE_DIR/last-seen"      # lines: sha=<main sha>, objects=<hash of ceo_ai object set>
LOG="$STATE_DIR/refresh.log"
DATE="$(date +%Y%m%d)"
BRANCH="chore/data-map-refresh-$DATE"
mkdir -p "$STATE_DIR"

log() { printf '%s %s\n' "$(date '+%Y-%m-%dT%H:%M:%S%z')" "$*" >>"$LOG"; [ "$DRY_RUN" = 1 ] && printf '%s\n' "$*" >&2 || true; }
die() { log "ERROR: $*"; echo "refresh-data-map: $*" >&2; exit 1; }
# Lock: flock where available (Linux); macOS has no flock, so fall back to an atomic
# mkdir lock with stale-PID recovery. Without this, launchd runs were unlocked.
if command -v flock >/dev/null 2>&1; then
  exec 9>"$STATE_DIR/lock"
  flock -n 9 || { log "another run holds the lock; exiting"; exit 0; }
else
  LOCKDIR="$STATE_DIR/lock.d"
  if ! mkdir "$LOCKDIR" 2>/dev/null; then
    OLD_PID="$(cat "$LOCKDIR/pid" 2>/dev/null || true)"
    if [ -n "$OLD_PID" ] && kill -0 "$OLD_PID" 2>/dev/null; then log "another run ($OLD_PID) holds the lock; exiting"; exit 0; fi
    log "removing stale lock (pid ${OLD_PID:-?})"; rm -rf "$LOCKDIR"; mkdir "$LOCKDIR" || die "cannot take lock"
  fi
  echo $$ >"$LOCKDIR/pid"
  trap 'rm -rf "$LOCKDIR"' EXIT
fi

log "=== start (dry_run=$DRY_RUN repo=$GOATOS_REPO)"

# --- 1. fetch + clean worktree of origin/main --------------------------------
git -C "$GOATOS_REPO" fetch --quiet origin main || die "git fetch failed"
MAIN_SHA="$(git -C "$GOATOS_REPO" rev-parse origin/main)"
if [ -d "$WT/.git" ] || [ -f "$WT/.git" ]; then
  git -C "$WT" checkout --quiet --detach "$MAIN_SHA"
  git -C "$WT" reset --quiet --hard "$MAIN_SHA"
  git -C "$WT" clean -qfdx -e node_modules
else
  git -C "$GOATOS_REPO" worktree prune
  git -C "$GOATOS_REPO" worktree add --quiet --detach "$WT" "$MAIN_SHA" || die "worktree add failed"
fi
log "worktree $WT at origin/main $MAIN_SHA"

# Generator: prefer main's copy; fall back to this script's copy pointed at main.
if [ -f "$WT/tools/ask-mesha-agent/gen-data-map.mjs" ]; then GEN=("node" "$WT/tools/ask-mesha-agent/gen-data-map.mjs")
else GEN=("env" "GEN_DATA_MAP_ROOT=$WT" "node" "$SCRIPT_DIR/gen-data-map.mjs"); log "gen-data-map.mjs not on main yet; using $SCRIPT_DIR copy"; fi
HAS_MAP=0; [ -f "$WT/.agents/skills/mesha-data-map/SKILL.md" ] && HAS_MAP=1

# --- 2. DB env (read-only) ---------------------------------------------------
if [ -f "$PGENV_FILE" ]; then set -a; . "$PGENV_FILE"; set +a; else log "WARN: $PGENV_FILE missing; generator will use repo fallback"; fi
export PGOPTIONS="-c default_transaction_read_only=on"
DB_OBJECTS=""
if command -v psql >/dev/null && DB_OBJECTS="$(psql -X -A -t -c "SELECT table_name FROM information_schema.tables WHERE table_schema='ceo_ai' ORDER BY 1" 2>>"$LOG")"; then :; else DB_OBJECTS=""; log "WARN: DB not reachable; object-set detection uses repo only"; fi
OBJ_HASH="$(printf '%s' "$DB_OBJECTS" | shasum | cut -c1-16)"

# --- 3. change detection -----------------------------------------------------
LAST_SHA=""; LAST_OBJ=""
[ -f "$STATE_FILE" ] && { LAST_SHA="$(sed -n 's/^sha=//p' "$STATE_FILE")"; LAST_OBJ="$(sed -n 's/^objects=//p' "$STATE_FILE")"; }
REASONS=()
SRC_PATHS=(backend/internal/ceoai/reporting/schema_cards.go backend/migrations/postgres)
if [ -z "$LAST_SHA" ]; then
  REASONS+=("first run (no state)")
elif git -C "$WT" cat-file -e "$LAST_SHA" 2>/dev/null; then
  CHANGED_FILES="$(git -C "$WT" diff --name-only "$LAST_SHA" "$MAIN_SHA" -- "${SRC_PATHS[@]}" || true)"
  CARD_CHANGE="$(printf '%s\n' "$CHANGED_FILES" | grep -c 'schema_cards.go' || true)"
  NEW_VIEWS="$(git -C "$WT" diff "$LAST_SHA" "$MAIN_SHA" -- backend/migrations/postgres | grep -oiE '^\+.*VIEW[[:space:]]+ceo_ai\.[a-z0-9_]+' | grep -oiE 'ceo_ai\.[a-z0-9_]+' | sort -u | tr '\n' ' ' || true)"
  [ "$CARD_CHANGE" != "0" ] && REASONS+=("schema_cards.go changed since ${LAST_SHA:0:9}")
  [ -n "$NEW_VIEWS" ] && REASONS+=("ceo_ai views created/replaced in migrations: $NEW_VIEWS")
else
  REASONS+=("last-seen sha ${LAST_SHA:0:9} not in history")
fi
[ -n "$DB_OBJECTS" ] && [ -n "$LAST_OBJ" ] && [ "$OBJ_HASH" != "$LAST_OBJ" ] && REASONS+=("live ceo_ai object set changed")
if [ "$HAS_MAP" = 1 ]; then
  CHECK_OUT="$( (cd "$WT" && "${GEN[@]}" --check) 2>&1 )" || REASONS+=("views.generated.md stale: $CHECK_OUT")
  log "generator check: $CHECK_OUT"
else
  log "data map not on origin/main yet (.agents/skills/mesha-data-map missing)"
fi

save_state() { printf 'sha=%s\nobjects=%s\n' "$MAIN_SHA" "$OBJ_HASH" >"$STATE_FILE"; }

if [ "${#REASONS[@]}" = 0 ]; then
  log "no changes; done"
  [ "$DRY_RUN" = 1 ] || save_state
  exit 0
fi
log "changes detected: ${REASONS[*]}"

if [ "$DRY_RUN" = 1 ]; then
  log "DRY RUN: would regenerate views.generated.md, run headless Claude review, 5 psql proof queries, commit on $BRANCH, push, gh pr create (never merge). State not updated."
  exit 0
fi
[ "$HAS_MAP" = 1 ] || die "data map files not on origin/main yet; land the ask-mesha data map first"
command -v "$CLAUDE_BIN" >/dev/null || die "claude CLI not found"
command -v gh >/dev/null || die "gh CLI not found"
# Goat OS GitHub authority is the Mesha token path, never whichever gh account is active.
if [ -n "${MESHA_GITHUB_PAT:-}" ]; then export GH_TOKEN="$MESHA_GITHUB_PAT"; else log "WARN: MESHA_GITHUB_PAT unset; gh uses the active account"; fi
case "$(git -C "$GOATOS_REPO" config user.email || true)" in *@mesha.sg) ;; *) die "git user.email is not @mesha.sg";; esac

# --- 4. regenerate -----------------------------------------------------------
cd "$WT"
git checkout --quiet -B "$BRANCH" "$MAIN_SHA"
"${GEN[@]}" >>"$LOG" 2>&1 || die "generator failed"

# --- 5. headless Claude review ----------------------------------------------
REVIEW_PROMPT="You are maintaining the Mesha data map in this goatos checkout (a clean worktree of origin/main).
Why you were invoked: ${REASONS[*]}
Previous reviewed main SHA: ${LAST_SHA:-none}. Current: $MAIN_SHA.

Files you own (edit ONLY these):
- .agents/skills/mesha-data-map/SKILL.md (routing table + gotchas)
- tools/ask-mesha-agent/data-map-core.md (always-loaded cheat-sheet, keep it under ~25 lines)
Source of truth (read, never edit): .agents/skills/mesha-data-map/references/views.generated.md (just regenerated),
backend/internal/ceoai/reporting/schema_cards.go, backend/migrations/postgres/*.sql.

Steps:
1. Compare every ceo_ai object in views.generated.md with the routing rows in SKILL.md and data-map-core.md.
   Add a routing row for each new object (topic -> view -> key columns -> date column), fix rows whose view,
   columns or date column changed, remove rows for objects that no longer exist. Keep existing wording/style;
   do not rewrite unrelated rows or the 'Business notes' section.
2. Prove the map works: run exactly these 5 leadership questions as ONE psql query each, using only the map
   (read-only DB, env already set; use ONLY: tools/ask-mesha-agent/ro-sql.sh \"<sql>\"):
   a) Headcount now by park.  b) When did we last weigh each park (completed/closed).  c) Animals sold this month.
   d) Deaths in the last 30 days by park.  e) Feed directed vs fed yesterday by park.
   Every query must succeed and return plausible rows. If one fails, fix the map (not the DB) and rerun.
3. Run: node tools/ask-mesha-agent/gen-data-map.mjs --check  (must exit 0).
4. Finish with a short report: routing rows added/changed/removed, and each of the 5 queries with its row count.
Never write to the database, never run git, never touch other files, never run the server."
set +e
"$CLAUDE_BIN" -p "$REVIEW_PROMPT" \
  --allowedTools "Read" "Grep" "Glob" \
    "Edit(.agents/skills/mesha-data-map/SKILL.md)" "Edit(tools/ask-mesha-agent/data-map-core.md)" \
    "Bash(tools/ask-mesha-agent/ro-sql.sh:*)" "Bash(node tools/ask-mesha-agent/gen-data-map.mjs --check)" \
  >"$STATE_DIR/review-$DATE.md" 2>>"$LOG"
RC=$?
set -e
cat "$STATE_DIR/review-$DATE.md" >>"$LOG"
[ "$RC" = 0 ] || die "claude review failed (rc=$RC)"

# Guard: only the three data-map files may change.
UNEXPECTED="$(git status --porcelain | awk '{print $2}' | grep -vE '^(\.agents/skills/mesha-data-map/|tools/ask-mesha-agent/data-map-core\.md$)' || true)"
[ -z "$UNEXPECTED" ] || die "review touched unexpected files: $UNEXPECTED"
"${GEN[@]}" --check >>"$LOG" 2>&1 || die "post-review --check failed"

if [ -z "$(git status --porcelain)" ]; then
  log "nothing to commit after regeneration + review"; save_state; exit 0
fi

# --- 6. commit, push branch, open PR (never merge) ---------------------------
git add .agents/skills/mesha-data-map tools/ask-mesha-agent/data-map-core.md
git commit --quiet -m "chore(data-map): refresh Mesha data map for ${MAIN_SHA:0:9}

${REASONS[*]}"
[ "$BRANCH" != "main" ] || die "refusing to push main"
git push --quiet --force-with-lease origin "HEAD:refs/heads/$BRANCH" >>"$LOG" 2>&1 || die "push failed"
PR_BODY="Automated refresh of the Mesha data map (tools/ask-mesha-agent/refresh-data-map.sh).

Trigger: ${REASONS[*]}
Base: origin/main @ $MAIN_SHA

Headless review report:

$(cat "$STATE_DIR/review-$DATE.md")

**This PR must land via \`make land-main\` (exact-SHA CI receipt). Do not use \`gh pr merge\` or push to main.**"
if gh pr view "$BRANCH" --json number >/dev/null 2>&1; then
  log "PR for $BRANCH already exists; branch updated"
else
  gh pr create --base main --head "$BRANCH" --title "chore(data-map): refresh Mesha data map ($DATE)" --body "$PR_BODY" >>"$LOG" 2>&1 || die "gh pr create failed"
fi
save_state
log "=== done: PR on $BRANCH"
