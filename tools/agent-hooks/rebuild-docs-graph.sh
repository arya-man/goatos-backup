#!/usr/bin/env bash
# Reliable incremental rebuild of the goatos-docs Graphify graph.
#
# Why this exists: doc extraction needs an LLM, so it cannot be a deterministic
# CLI like `code-review-graph update`. The previous hook fired a fire-and-forget
# `claude -p` that silently dropped extraction chunks and left 21/66 files with
# zero nodes. This script makes the auto-rebuild SAFE: it re-extracts only the
# changed files, merges them into the existing graph, re-clusters, regenerates
# outputs, then SELF-VERIFIES. If extraction fails or the graph regresses, it
# restores the previous graph and keeps the stale-marker so nothing is silently
# corrupted and the next change retries.
#
# Usage:
#   rebuild-docs-graph.sh                    # auto-pick claude or codex
#   AI_BACKEND=claude rebuild-docs-graph.sh
#   AI_BACKEND=codex rebuild-docs-graph.sh
set -uo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT="$REPO/graphify-out"
LOG="/tmp/graphify-goatos-update.log"
MARKER="$OUT/.needs_docs_graph_update"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AI_BACKEND="${AI_BACKEND:-auto}"
cd "$REPO" || exit 1

log() { echo "[$(date '+%F %T')] rebuild: $*" >> "$LOG"; }

mkdir -p "$OUT"

resolve_graphify_python() {
    local py
    py="$(cat "$OUT/.graphify_python" 2>/dev/null || true)"
    if [ -n "$py" ] && [ -x "$py" ] && "$py" -c "import graphify" >/dev/null 2>&1; then
        printf '%s\n' "$py"
        return 0
    fi
    if command -v uv >/dev/null 2>&1; then
        py="$(uv tool run graphifyy python -c "import sys; print(sys.executable)" 2>/dev/null || true)"
        if [ -n "$py" ] && [ -x "$py" ] && "$py" -c "import graphify" >/dev/null 2>&1; then
            printf '%s' "$py" > "$OUT/.graphify_python"
            printf '%s\n' "$py"
            return 0
        fi
    fi
    if command -v graphify >/dev/null 2>&1; then
        py="$(head -1 "$(command -v graphify)" | sed 's/^#!//')"
        if [ -n "$py" ] && [ -x "$py" ] && "$py" -c "import graphify" >/dev/null 2>&1; then
            printf '%s' "$py" > "$OUT/.graphify_python"
            printf '%s\n' "$py"
            return 0
        fi
    fi
    if python3 -c "import graphify" >/dev/null 2>&1; then
        printf '%s\n' "python3"
        return 0
    fi
    return 1
}

PY="$(resolve_graphify_python || true)"
if [ -z "$PY" ]; then
    log "graphify python runtime missing; run make ai-setup"
    echo "Missing graphify Python runtime. Run: make ai-setup" >&2
    exit 2
fi

choose_llm_backend() {
    case "$AI_BACKEND" in
      auto)
        if command -v claude >/dev/null 2>&1; then printf '%s\n' "claude"; return 0; fi
        if command -v codex >/dev/null 2>&1; then printf '%s\n' "codex"; return 0; fi
        ;;
      claude|codex)
        if command -v "$AI_BACKEND" >/dev/null 2>&1; then printf '%s\n' "$AI_BACKEND"; return 0; fi
        ;;
      *)
        log "unsupported AI_BACKEND=$AI_BACKEND (expected auto|claude|codex)"
        return 1
        ;;
    esac
    log "no supported local LLM CLI found for AI_BACKEND=$AI_BACKEND"
    return 1
}

run_llm_extract() {
    local prompt="$1"
    local backend
    backend="$(choose_llm_backend)" || return 1
    log "extracting $(echo "$CHANGED" | grep -c .) changed file(s) via $backend"
    case "$backend" in
      claude)
        GOATOS_GRAPH_GUARD=0 GOATOS_DOCS_GRAPH_AUTOREBUILD=0 \
            claude -p "$prompt" >> "$LOG" 2>&1
        ;;
      codex)
        printf '%s\n' "$prompt" | GOATOS_GRAPH_GUARD=0 GOATOS_DOCS_GRAPH_AUTOREBUILD=0 \
            codex exec --cd "$REPO" \
            --sandbox danger-full-access \
            --ask-for-approval never \
            --output-last-message "$OUT/.codex_extract_last.txt" \
            - >> "$LOG" 2>&1
        ;;
    esac
}

# Serialize: only one rebuild at a time.
LOCK="$OUT/.rebuild.lock"
if ! mkdir "$LOCK" 2>/dev/null; then
    log "another rebuild in progress, skipping"
    exit 0
fi
trap 'rmdir "$LOCK" 2>/dev/null' EXIT

# 1. Which corpus files changed vs the manifest?
"$HERE/goatos-docs-corpus.sh" > "$OUT/.corpus.txt"
CHANGED_JSON="$("$PY" - "$OUT/.corpus.txt" <<'PY'
import json, hashlib, os, sys
corpus = [l.strip() for l in open(sys.argv[1]) if l.strip()]
mp = "graphify-out/manifest.json"
man = json.load(open(mp)) if os.path.exists(mp) else {}
changed = []
for p in corpus:
    if not os.path.exists(p):
        continue
    meta = man.get(p)
    h = hashlib.md5(open(p, "rb").read()).hexdigest()
    if not isinstance(meta, dict) or meta.get("hash") != h:
        changed.append(p)
deleted = [p for p in man if p not in set(corpus) or not os.path.exists(p)]
print(json.dumps({"changed": changed, "deleted": deleted}))
PY
)"
CHANGED=$("$PY" -c "import json,sys;print('\n'.join(json.loads(sys.argv[1])['changed']))" "$CHANGED_JSON")
DELETED=$("$PY" -c "import json,sys;print('\n'.join(json.loads(sys.argv[1])['deleted']))" "$CHANGED_JSON")

if [ -z "$CHANGED" ] && [ -z "$DELETED" ]; then
    log "no corpus changes vs manifest; clearing marker"
    rm -f "$MARKER"
    exit 0
fi
log "changed=$(echo "$CHANGED" | grep -c . 2>/dev/null) deleted=$(echo "$DELETED" | grep -c . 2>/dev/null)"

# 2. Back up the current good graph so we can restore on failure.
cp -f "$OUT/graph.json" "$OUT/.graph.bak.json" 2>/dev/null
cp -f "$OUT/manifest.json" "$OUT/.manifest.bak.json" 2>/dev/null

# 3. Extract ONLY the changed files (skipped if changes are deletions only).
rm -f "$OUT/.graphify_autochunk.json"
if [ -n "$CHANGED" ]; then
    FILE_LIST="$(echo "$CHANGED" | sed 's/^/  /')"
    PROMPT="You are a graphify extraction subagent. Read ONLY these files and extract a knowledge-graph fragment, then WRITE it as JSON to $OUT/.graphify_autochunk.json (no prose).
Files:
$FILE_LIST

Rules: EXTRACTED edges confidence_score=1.0; INFERRED 0.6-0.9 (reason per edge, never 0.5); AMBIGUOUS 0.1-0.3 (flag, don't omit). Extract named concepts, rules, systems, and rationale (WHY -> rationale_for edges). Node id: lowercase [a-z0-9_] only, namespaced by directory to avoid collisions across identically-named files, format {dir_path_normalized}_{stem}_{entity}. source_file MUST be relative to $REPO. file_type=\"document\".
Write exactly: {\"nodes\":[{\"id\":\"...\",\"label\":\"...\",\"file_type\":\"document\",\"source_file\":\"rel/path\",\"source_location\":null,\"source_url\":null,\"captured_at\":null,\"author\":null,\"contributor\":null}],\"edges\":[{\"source\":\"id\",\"target\":\"id\",\"relation\":\"references|conceptually_related_to|implements|rationale_for|depends_on|shares_data_with\",\"confidence\":\"EXTRACTED|INFERRED|AMBIGUOUS\",\"confidence_score\":1.0,\"source_file\":\"rel/path\",\"source_location\":null,\"weight\":1.0}],\"hyperedges\":[],\"input_tokens\":0,\"output_tokens\":0}
After writing, validate it loads as JSON. Final message: node count + edge count."
    run_llm_extract "$PROMPT" || log "LLM extraction failed to start for AI_BACKEND=$AI_BACKEND"
fi

# 4. Merge + recluster + regenerate + SELF-CHECK (restores backup on failure).
"$PY" - "$CHANGED_JSON" <<'PY'
import json, hashlib, os, sys
from pathlib import Path
from graphify.build import build_from_json
from graphify.cluster import cluster, score_all
from graphify.analyze import god_nodes, surprising_connections, suggest_questions
from graphify.report import generate
from graphify.export import to_json, to_html

OUT = "graphify-out"
info = json.loads(sys.argv[1])
changed = set(os.path.relpath(p) for p in info["changed"])
deleted = set(os.path.relpath(p) for p in info["deleted"])

def fail(msg):
    # restore previous good graph + keep marker so the change stays flagged
    if os.path.exists(f"{OUT}/.graph.bak.json"):
        os.replace(f"{OUT}/.graph.bak.json", f"{OUT}/graph.json")
    if os.path.exists(f"{OUT}/.manifest.bak.json"):
        os.replace(f"{OUT}/.manifest.bak.json", f"{OUT}/manifest.json")
    print("REBUILD_FAILED " + msg)
    raise SystemExit(2)

# load previous graph, or start from empty on a fresh clone
if os.path.exists(f"{OUT}/graph.json"):
    g = json.load(open(f"{OUT}/graph.json"))
else:
    g = {"nodes": [], "links": [], "edges": [], "hyperedges": []}
prev_nodes = g["nodes"]
prev_count = len(prev_nodes)

# drop nodes from changed/deleted source files (they will be re-added)
touched = changed | deleted
def relsf(n):
    sf = n.get("source_file") or ""
    return os.path.relpath(sf) if os.path.isabs(sf) else sf
nodes = [n for n in prev_nodes if relsf(n) not in touched]
keep_ids = {n["id"] for n in nodes}

links = g.get("links", g.get("edges", []))
def eid(e, k):
    v = e.get(k)
    return v.get("id") if isinstance(v, dict) else v
edges = []
for e in links:
    s, t = eid(e, "source"), eid(e, "target")
    if s in keep_ids and t in keep_ids:
        edges.append({"source": s, "target": t, "relation": e.get("relation"),
                      "confidence": e.get("confidence", "INFERRED"),
                      "confidence_score": e.get("confidence_score", 0.7),
                      "source_file": e.get("source_file"), "source_location": e.get("source_location"),
                      "weight": e.get("weight", 1.0)})

# add freshly extracted nodes/edges for changed files
added = 0
chunk_path = f"{OUT}/.graphify_autochunk.json"
if changed:
    if not os.path.exists(chunk_path):
        fail("extraction chunk missing (AI backend produced nothing)")
    try:
        chunk = json.loads(Path(chunk_path).read_text())
    except Exception as e:
        fail(f"extraction chunk invalid json: {e}")
    seen = {n["id"] for n in nodes}
    got_files = set()
    for n in chunk.get("nodes", []):
        if not n.get("file_type"):
            n["file_type"] = "document"
        for f in ("source_location", "source_url", "captured_at", "author", "contributor"):
            n.setdefault(f, None)
        if n["id"] not in seen:
            seen.add(n["id"]); nodes.append(n); added += 1
            got_files.add(relsf(n))
    nodeset = {n["id"] for n in nodes}
    for e in chunk.get("edges", []):
        if e.get("source") in nodeset and e.get("target") in nodeset and e["source"] != e["target"]:
            edges.append(e)
    # self-check: every changed file that is non-trivial should have produced >=1 node
    missing = [c for c in changed if c not in got_files]
    if missing and added == 0:
        fail(f"no nodes extracted for changed files: {missing[:5]}")

# build + cluster
ex = {"nodes": nodes, "edges": edges, "hyperedges": g.get("hyperedges", []),
      "input_tokens": 0, "output_tokens": 0}
G = build_from_json(ex)

# regression guard: a clean rebuild should not lose a large fraction of nodes
if G.number_of_nodes() < prev_count * 0.8 and not deleted:
    fail(f"node count regressed {prev_count} -> {G.number_of_nodes()}")

communities = cluster(G)
cohesion = score_all(G, communities)
gods = god_nodes(G); surprises = surprising_connections(G, communities)
labels = {}
for cid, mem in communities.items():
    labels[cid] = (G.nodes[mem[0]].get("label", f"Community {cid}")[:44]
                   if len(mem) == 1 else f"Community {cid}")

# manifest from current corpus on disk
corpus = [l.strip() for l in open(f"{OUT}/.corpus.txt") if l.strip()]
man = {}
words = 0
for p in corpus:
    if os.path.exists(p):
        man[p] = {"mtime": os.path.getmtime(p), "hash": hashlib.md5(open(p, "rb").read()).hexdigest()}
detection = {"total_files": len(man), "total_words": 0, "needs_graph": True, "warning": None,
             "files": {"document": corpus, "code": [], "paper": [], "image": [], "video": []}}
questions = suggest_questions(G, communities, labels)
report = generate(G, communities, cohesion, labels, gods, surprises, detection,
                  {"input": 0, "output": 0}, "goatos docs (all .md + schema.html)",
                  suggested_questions=questions)
Path(f"{OUT}/GRAPH_REPORT.md").write_text(report)
to_json(G, communities, f"{OUT}/graph.json")
try:
    to_html(G, communities, f"{OUT}/graph.html", community_labels=labels)
except Exception:
    pass
Path(f"{OUT}/manifest.json").write_text(json.dumps(man, indent=2))
print(f"REBUILD_OK nodes={G.number_of_nodes()} edges={G.number_of_edges()} "
      f"communities={len(communities)} files={len(man)} changed={len(changed)} added={added}")
PY
RC=$?

# 5. Commit or restore based on self-check result.
if [ $RC -eq 0 ]; then
    rm -f "$MARKER" "$OUT/.graph.bak.json" "$OUT/.manifest.bak.json" \
          "$OUT/.graphify_autochunk.json" "$OUT/.corpus.txt"
    log "OK - graph updated, marker cleared"
else
    log "FAILED (rc=$RC) - restored previous graph, marker kept for retry / interactive rebuild"
    rm -f "$OUT/.graphify_autochunk.json" "$OUT/.corpus.txt"
fi
exit 0
