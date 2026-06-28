#!/usr/bin/env bash
# PreToolUse guard (Claude AND Codex): one-time graph-first speed bump for
# direct sessions launched inside the Goat OS repo.
#
# This is intentionally repo-local. Do not depend on /Users/ravi/mesha workspace
# tooling from the committed Goat OS hook configuration.
set -u
[ "${GOATOS_GRAPH_GUARD:-1}" = "0" ] && exit 0

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
LOG="/tmp/goatos-graph-guard.log"

INPUT="$(cat 2>/dev/null || true)"

IFS=$'\t' read -r TOOL TARGET CWD SID <<EOF
$(INPUT_JSON="$INPUT" python3 - <<'PY' 2>/dev/null || true
import json, os
raw = os.environ.get("INPUT_JSON", "") or "{}"
try:
    d = json.loads(raw)
except Exception:
    d = {}
if not isinstance(d, dict):
    d = {}

tool = d.get("tool_name") or d.get("tool") or d.get("name") or ""
ti = d.get("tool_input") or d.get("input") or d.get("arguments") or {}
if not isinstance(ti, dict):
    ti = {}
if not ti:
    try:
        ti = json.loads(os.environ.get("CLAUDE_TOOL_INPUT", "{}")) or {}
    except Exception:
        ti = {}

cwd = d.get("cwd") or ti.get("cwd") or os.getcwd()
sid = (d.get("session_id") or d.get("session") or d.get("conversation_id")
       or os.environ.get("CLAUDE_SESSION_ID") or "")

target = ""
if tool in ("Grep", "Glob"):
    target = ti.get("path") or ti.get("glob") or cwd
elif tool == "Read":
    target = ti.get("file_path") or ""
else:
    cmd = (ti.get("command") or ti.get("cmd") or d.get("command")
           or (" ".join(d.get("argv")) if isinstance(d.get("argv"), list) else "")
           or "")
    tool = tool or ("Bash" if cmd else "")
    target = cmd

def clean(s):
    return str(s).replace("\n", " ").replace("\t", " ").strip()

print("\t".join([clean(tool), clean(target), clean(cwd), clean(sid)]))
PY
)
EOF

[ -z "${TOOL:-}" ] && exit 0

is_search=0
case "$TOOL" in
  Grep|Glob|Read) is_search=1 ;;
  Bash|"")
    # CONTENT search/read verbs only. find/ls (file location, dir trees) and
    # head/tail (pipe-truncation / log follow) are EXCLUDED as structural
    # blind spots the graph can't answer; a real code sweep in the same command
    # still trips on its grep/cat/rg/sed/awk token.
    if printf '%s' "$TARGET" | grep -Eq '(^|[;&|[:space:]])(rg|grep|egrep|fgrep|ag|ack|cat|sed|awk)([[:space:]]|$)'; then
      is_search=1
    fi
    ;;
esac
[ "$is_search" = "1" ] || exit 0

SCAN="$TARGET"
[ -z "$SCAN" ] && SCAN="$CWD"

matched_repo=""
case "$SCAN " in *"$REPO"*) matched_repo="$REPO" ;; esac
if [ -z "$matched_repo" ]; then
  case "$CWD/" in "$REPO/"*) matched_repo="$REPO" ;; esac
fi
[ -z "$matched_repo" ] && exit 0

sid="${SID:-$PPID}"
FLAG="${TMPDIR:-/tmp}/goatos-graph-guard.$(printf '%s' "$sid" | tr -c 'A-Za-z0-9._-' '_').flag"
[ -f "$FLAG" ] && exit 0
if ! ( set -C; : > "$FLAG" ) 2>/dev/null; then
  exit 0
fi

printf '%s\n' "[$(date '+%F %T')] bump tool=$TOOL repo=$matched_repo sid=$sid" >>"$LOG" 2>/dev/null || true

cat >&2 <<MSG
GRAPH-FIRST GUARD ($matched_repo is a graphed repo)

You reached for $TOOL before querying the knowledge graphs. Per AGENTS.md lookup order:
  1. CRG (code structure)  -> code-review-graph MCP: query_graph / semantic_search_nodes / get_impact_radius
  2. Graphify (docs)       -> graphify query against $matched_repo/graphify-out/graph.json
  3. THEN Grep/Read        -> only for blind spots graphs can't see.

If this search IS a blind spot (HTTP route strings, config constants, reflective
wiring, or uncommitted code) -> just re-run the exact command; this guard fires
only once per session and will now pass.
MSG
exit 2
