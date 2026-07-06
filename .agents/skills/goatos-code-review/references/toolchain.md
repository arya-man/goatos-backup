# Review Toolchain — CRG · Graphify · RTK · repowise

Four complementary tools drive a Goat OS review. CRG, the docs graph, and
repowise refresh on edits through hooks. RTK is different: it routes large
display diffs and noisy command output before the tool call; it has no index to
refresh. All are set up by `make ai-setup`; verify portability with
`make ai-doctor`. All outputs are gitignored and machine-local.

Golden order: **graph-first, files-last.** One graph query replaces many
grep/read cycles. Native Grep/Read is the fallback for graph blind spots.

---

## 1. CRG (code-review-graph) — code structure

Persistent Tree-sitter graph. First pass for callers/callees, blast radius,
architecture, dead code, and test coverage. `repo_root` = your goatos checkout
(`git rev-parse --show-toplevel`).

Load the review tools:

```
ToolSearch "select:mcp__code_review_graph.get_minimal_context_tool,mcp__code_review_graph.detect_changes_tool,mcp__code_review_graph.get_review_context_tool,mcp__code_review_graph.get_impact_radius_tool,mcp__code_review_graph.query_graph_tool,mcp__code_review_graph.get_architecture_overview_tool,mcp__code_review_graph.semantic_search_nodes_tool"
```

Route by task shape (do not run all of them every time):

| Question | Tool |
|---|---|
| Cold start on a diff/PR | `get_minimal_context_tool` then `detect_changes_tool` |
| What changed + risk + test gaps | `detect_changes_tool` |
| Source snippets for the changed area | `get_review_context_tool` |
| Blast radius of a change | `get_impact_radius_tool` |
| Which kernel flows are touched | `detect_changes_tool` flow/risk output, then targeted `query_graph_tool` |
| Who calls / depends on / tests X | `query_graph_tool` (callers_of / callees_of / imports_of / tests_for) |
| Find code by keyword/domain | `semantic_search_nodes_tool` |
| High-level shape / coupling | `get_architecture_overview_tool` |

CRG blind spots — fall through to Grep/Read: HTTP route strings, middleware wired
by reflection/string keys, config/env constants, SQL query strings, uncommitted
code, and any `callers_of = 0` that looks wrong (verify with grep). If the graph
returns nothing for an obviously-existing symbol, the file may not be parsed yet —
run `make ai-rebuild-code` or fall back to native.

---

## 2. Graphify — business / product context (docs graph)

Query the docs graphs to check a change against the rules and specs, not the
code. Two graphs; query both when relevant.

**goatos-docs graph** (locally generated — TRDs, ADRs, protocol/obligation
engine, vaccination rules, frontend scope, skill refs):

```bash
uvx --from 'graphifyy[mcp]==0.8.44' graphify query "QUESTION" \
  --graph ./graphify-out/graph.json
```

If missing, run `make ai-rebuild-docs` or read the source doc directly.

**mesha_docs_graph** (maintainer-local wiki — farm SOPs, vaccination protocol,
org model; lives outside this repo, cannot be rebuilt here):

```bash
uvx --from 'graphifyy[mcp]==0.8.44' graphify query "QUESTION" \
  --graph /Users/ravi/mesha/graphify-out/graph.json
```

Use MCP `mesha_docs_graph` / `mesha_visual_graph` if configured; otherwise skip.

When to use in review: verify vaccination timing/gaps, obligation state
transitions, defer/re-scope semantics, feed/calendar rules, and org/species
model against the authoritative doc before judging domain logic. Cite the doc the
graph names (see `references/business-rules.md` for the doc map).

---

## 3. RTK — diff compression

Keeps large `git diff` / `git show` output from flooding review context. The
`tools/agent-hooks/pre-rtk-git-diff.sh` PreToolUse hook auto-routes any display
diff larger than the size gate through the `rtk` binary (installed by
`make ai-setup`; `brew install rtk`).

```bash
git diff main...HEAD        # auto-routed through rtk if large
rtk git diff main...HEAD    # explicit
```

Controls:
- `GOATOS_RTK=0` or `GOATOS_RTK_GIT_DIFF=0` — disable routing for this command
- `GOATOS_RTK_MIN_BYTES=0` — route every safe display diff (default gate: 50000)

The hook only touches human-display diffs; programmatic/piped/tiny diffs pass
through untouched.

---

## 4. repowise — code health, defect risk, dead code

Quality/health/risk layer. Run from the repo root (needs the local `.repowise/`
index; built by `make ai-setup`, refreshed by `make ai-rebuild-repowise` and the
auto-update hook).

```bash
repowise risk <commit-or-range>   # defect risk of a change — run on the reviewed range
repowise health                   # code-health scores (complexity/CCN, maintainability)
repowise dead-code                # dead / unused code the change may add or leave
repowise serve                    # web UI at http://localhost:3000 (health/risk/graph/coverage tabs)
repowise saved                    # tokens / $ saved by graph-augmented calls
```

Optional: populate the dashboard Coverage tab with `make ai-repowise-coverage`
(runs Go coverage; needs the local dev DB up).

Use in review to flag: a change landing in a high-defect-risk hotspot, rising
complexity in a touched function, and dead code introduced or left behind. Treat
these as signals that raise scrutiny, not automatic blocks.

---

## 5. One integrated pass (5-15 min for a typical change)

1. **Scope** — CRG `get_minimal_context_tool` → `detect_changes_tool` (changed
   symbols, affected flows, test gaps).
2. **Diff** — `git diff main...HEAD` (RTK auto-routes if large).
3. **Impact** — CRG `get_impact_radius_tool`; targeted `query_graph_tool`
   (`callers_of`, `callees_of`, `tests_for`) for coverage and flow checks.
4. **Health/risk** — repowise `risk` / `health` / `dead-code`.
5. **Business cross-check** — Graphify docs graph for the touched rules; read the
   authoritative doc.
6. **Read suspects** — Grep/Read the flagged files for blind spots (SQL, routes,
   constants, migrations, uncommitted code).

Then apply the layer checklist (`kernel-and-scale.md`, `backend.md`,
`frontend.md`, `business-rules.md`) and report by severity.

---

## Auto-update (no manual rebuild during review)

Edits refresh CRG, Graphify, and repowise in the background via PostToolUse
hooks; repowise also installs a post-commit refresh hook. RTK has no graph/index.
Before trusting an empty graph answer (`callers_of=0`, no tests, no importers),
confirm the graph reflects the reviewed revision or verify with grep. If a graph
looks stale for freshly-changed code: `make ai-rebuild-code` (CRG),
`make ai-rebuild-docs` (Graphify), `make ai-rebuild-repowise` (repowise), or
`make ai-rebuild` for all three. Verify the clone is wired and graph artifacts
stay untracked with `make ai-doctor`.
