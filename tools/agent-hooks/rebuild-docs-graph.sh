#!/usr/bin/env bash
# Main-aware rebuild of the goatos-docs Graphify graph.
#
# The graph is a projection of the docs on origin/main, read from a clean detached
# worktree pinned to origin/main (never the live/dirty/stale local checkout). This
# script:
#   1. materializes the clean origin/main corpus (docs-graph-lib + corpus.sh),
#   2. decides FULL vs INCREMENTAL (full when there is no prior graph/basis, when
#      --full is given, or when the prior basis is not an ancestor of current main
#      i.e. history diverged/rewound),
#   3. extracts the changed docs with the local LLM backend (claude|codex),
#   4. assembles + clusters + regenerates outputs into the CANONICAL graphify-out
#      with main-RELATIVE paths and an origin/main basis stamp (docs-graph-assemble.py),
#   5. self-verifies; on any failure it restores the previous graph and keeps the
#      stale marker so nothing is silently corrupted and the next run retries.
#
# Outputs always land in the canonical checkout's graphify-out regardless of which
# worktree runs this (resolved via git-common-dir), so every session reads one graph.
#
# Usage:
#   rebuild-docs-graph.sh                 # incremental if possible, else full
#   rebuild-docs-graph.sh --full          # force a from-scratch rebuild
#   AI_BACKEND=claude rebuild-docs-graph.sh
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
. "$HERE/docs-graph-lib.sh"

AI_BACKEND="${AI_BACKEND:-auto}"
FORCE_FULL=0
[ "${1:-}" = "--full" ] && FORCE_FULL=1

LOG="/tmp/graphify-goatos-update.log"
log() { echo "[$(date '+%F %T')] rebuild: $*" >> "$LOG"; }

OUT="$(dg_out)" || { echo "not inside goatos repo" >&2; exit 1; }
MARKER="$OUT/.needs_docs_graph_update"
mkdir -p "$OUT"

PY="$(dg_graphify_python || true)"
if [ -z "$PY" ]; then
    log "graphify python runtime missing; run make ai-setup"
    echo "Missing graphify Python runtime. Run: make ai-setup" >&2
    exit 2
fi

MAIN_WT="$(dg_ensure_main_worktree)" || { log "cannot resolve clean origin/main worktree"; exit 2; }
# Basis = the SHA the worktree is ACTUALLY at (captured atomically here), not a
# fresh `git rev-parse origin/main`: origin/main can advance mid-run, and the
# graph must be stamped with the commit whose docs it indexed, not a later one.
SHA="$(git -C "$MAIN_WT" rev-parse HEAD)" || { log "cannot resolve worktree HEAD"; exit 2; }
CANON="$(dg_canonical_checkout)"
# Pin this prepared worktree for every corpus.sh call in this run (no re-reset).
export DOCS_GRAPH_PINNED_WT="$MAIN_WT"

# Serialize: only one rebuild at a time.
LOCK="$OUT/.rebuild.lock"
if ! mkdir "$LOCK" 2>/dev/null; then
    log "another rebuild in progress, skipping"
    exit 0
fi
trap 'rmdir "$LOCK" 2>/dev/null' EXIT

# --- Decide FULL vs INCREMENTAL -------------------------------------------------
BASIS="$(dg_basis_sha || true)"
MODE=incremental
if [ "$FORCE_FULL" = "1" ]; then
    MODE=full
elif [ ! -f "$OUT/graph.json" ] || [ ! -f "$OUT/manifest.json" ] || [ -z "$BASIS" ]; then
    MODE=full
elif ! git -C "$CANON" merge-base --is-ancestor "$BASIS" "$SHA" 2>/dev/null; then
    # history diverged or was rewound relative to the basis -> incremental merge
    # is unsafe; rebuild clean.
    MODE=full
fi
log "mode=$MODE basis=${BASIS:-<none>} origin/main=$SHA"

# --- Corpus (clean origin/main) + changed/deleted vs manifest -------------------
CORPUS="$OUT/.corpus.txt"
"$HERE/goatos-docs-corpus.sh" > "$CORPUS"

CHANGED_LIST="$OUT/.changed-docs.txt"   # main-relative
DELETED_LIST="$OUT/.deleted-docs.txt"   # main-relative
"$PY" - "$CORPUS" "$MAIN_WT" "$OUT/manifest.json" "$MODE" "$CHANGED_LIST" "$DELETED_LIST" <<'PY'
import json, hashlib, os, sys
corpus_file, main_root, manifest_path, mode, changed_out, deleted_out = sys.argv[1:7]
corpus_abs = [l.strip() for l in open(corpus_file) if l.strip()]
def rel(p): return os.path.relpath(p, main_root)
man = json.load(open(manifest_path)) if os.path.exists(manifest_path) else {}
changed, deleted = [], []
if mode == "full":
    changed = [rel(p) for p in corpus_abs]
else:
    corpus_rel = {rel(p) for p in corpus_abs}
    for p in corpus_abs:
        r = rel(p)
        meta = man.get(r)
        h = hashlib.md5(open(p, "rb").read()).hexdigest()
        if not isinstance(meta, dict) or meta.get("hash") != h:
            changed.append(r)
    deleted = [k for k in man if k not in corpus_rel]
open(changed_out, "w").write("\n".join(changed) + ("\n" if changed else ""))
open(deleted_out, "w").write("\n".join(deleted) + ("\n" if deleted else ""))
print(f"changed={len(changed)} deleted={len(deleted)}")
PY
CHANGED="$(cat "$CHANGED_LIST" 2>/dev/null || true)"
DELETED="$(cat "$DELETED_LIST" 2>/dev/null || true)"

if [ -z "$CHANGED" ] && [ -z "$DELETED" ] && [ "$MODE" = incremental ]; then
    log "no corpus changes vs manifest; stamping basis + clearing marker"
    printf '%s\n' "$SHA" > "$OUT/.docs_graph_basis"
    rm -f "$MARKER"
    exit 0
fi

# --- Back up current good graph -------------------------------------------------
cp -f "$OUT/graph.json"    "$OUT/.graph.bak.json"    2>/dev/null || true
cp -f "$OUT/manifest.json" "$OUT/.manifest.bak.json" 2>/dev/null || true
cp -f "$OUT/.docs_graph_basis" "$OUT/.basis.bak"     2>/dev/null || true

# --- Extract changed docs via LLM backend into fragment JSONs -------------------
FRAG_DIR="$OUT/.graphify_frags"
rm -rf "$FRAG_DIR"; mkdir -p "$FRAG_DIR"
extract_failed=0
if [ -n "$CHANGED" ]; then
    # chunk the changed (main-relative) paths
    "$PY" - "$CHANGED_LIST" "$FRAG_DIR" "${GOATOS_DOCS_GRAPH_CHUNK_SIZE:-12}" <<'PY'
from pathlib import Path
import sys
changed = [l.strip() for l in Path(sys.argv[1]).read_text().splitlines() if l.strip()]
frag = Path(sys.argv[2]); size = max(1, int(sys.argv[3]))
for i in range(0, len(changed), size):
    (frag / f"chunk_{i//size+1:03d}.txt").write_text("\n".join(changed[i:i+size]) + "\n")
PY

    backend=""
    case "$AI_BACKEND" in
      auto) command -v claude >/dev/null 2>&1 && backend=claude || { command -v codex >/dev/null 2>&1 && backend=codex; } ;;
      claude|codex) command -v "$AI_BACKEND" >/dev/null 2>&1 && backend="$AI_BACKEND" ;;
    esac
    if [ -z "$backend" ]; then
        log "no LLM backend for AI_BACKEND=$AI_BACKEND; marker kept"
        rm -rf "$FRAG_DIR"; exit 0
    fi

    for chunk_file in "$FRAG_DIR"/chunk_*.txt; do
        [ -f "$chunk_file" ] || continue
        cname="$(basename "$chunk_file" .txt)"
        cout="$FRAG_DIR/$cname.json"
        FILE_LIST="$(sed "s#^#  $MAIN_WT/#" "$chunk_file")"
        PROMPT="You are a graphify extraction subagent. Read ONLY these files and extract a knowledge-graph fragment, then WRITE it as JSON to $cout (no prose).
Files:
$FILE_LIST

Rules: EXTRACTED edges confidence_score=1.0; INFERRED 0.6-0.9 (reason per edge, never 0.5); AMBIGUOUS 0.1-0.3 (flag, don't omit). Extract named concepts, rules, systems, decisions/ADRs, invariants and rationale (WHY -> rationale_for edges). Node id: lowercase [a-z0-9_] only, namespaced by directory to avoid collisions, format {dir_path_normalized}_{stem}_{entity}. source_file MUST be RELATIVE to $MAIN_WT (e.g. docs/decisions/foo.md). file_type=\"document\".
Write exactly: {\"nodes\":[{\"id\":\"...\",\"label\":\"...\",\"file_type\":\"document\",\"source_file\":\"rel/path\",\"source_location\":null,\"source_url\":null,\"captured_at\":null,\"author\":null,\"contributor\":null}],\"edges\":[{\"source\":\"id\",\"target\":\"id\",\"relation\":\"references|conceptually_related_to|implements|rationale_for|depends_on|shares_data_with\",\"confidence\":\"EXTRACTED|INFERRED|AMBIGUOUS\",\"confidence_score\":1.0,\"source_file\":\"rel/path\",\"source_location\":null,\"weight\":1.0}],\"hyperedges\":[],\"input_tokens\":0,\"output_tokens\":0}
After writing, validate it loads as JSON. Final message: node count + edge count."
        case "$backend" in
          claude)
            GOATOS_GRAPH_GUARD=0 GOATOS_DOCS_GRAPH_AUTOREBUILD=0 \
                claude -p "$PROMPT" >> "$LOG" 2>&1 || extract_failed=1 ;;
          codex)
            printf '%s\n' "$PROMPT" | GOATOS_GRAPH_GUARD=0 GOATOS_DOCS_GRAPH_AUTOREBUILD=0 \
                codex exec --cd "$MAIN_WT" --sandbox danger-full-access - >> "$LOG" 2>&1 || extract_failed=1 ;;
        esac
    done
    if [ "$extract_failed" -ne 0 ]; then
        log "FAILED - an extraction backend call failed; marker kept for retry"
        rm -rf "$FRAG_DIR"; exit 0
    fi
fi

# --- Assemble + cluster + regenerate + self-verify ------------------------------
"$PY" "$HERE/docs-graph-assemble.py" \
    --fragments-dir "$FRAG_DIR" \
    --corpus-file "$CORPUS" \
    --main-root "$MAIN_WT" \
    --out "$OUT" \
    --basis "$SHA" \
    --mode "$MODE" \
    --changed-file "$CHANGED_LIST" \
    --deleted-file "$DELETED_LIST" >> "$LOG" 2>&1
RC=$?

if [ $RC -eq 0 ]; then
    rm -f "$MARKER" "$OUT/.graph.bak.json" "$OUT/.manifest.bak.json" "$OUT/.basis.bak" \
          "$CORPUS" "$CHANGED_LIST" "$DELETED_LIST"
    rm -rf "$FRAG_DIR"
    log "OK - graph updated to origin/main $SHA, marker cleared"
else
    # restore previous good graph; keep marker for retry
    [ -f "$OUT/.graph.bak.json" ]    && mv -f "$OUT/.graph.bak.json"    "$OUT/graph.json"
    [ -f "$OUT/.manifest.bak.json" ] && mv -f "$OUT/.manifest.bak.json" "$OUT/manifest.json"
    [ -f "$OUT/.basis.bak" ]         && mv -f "$OUT/.basis.bak"         "$OUT/.docs_graph_basis"
    rm -f "$CORPUS" "$CHANGED_LIST" "$DELETED_LIST"
    rm -rf "$FRAG_DIR"
    log "FAILED (rc=$RC) - restored previous graph, marker kept"
fi
exit 0
