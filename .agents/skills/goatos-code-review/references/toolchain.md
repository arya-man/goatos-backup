# Review Toolchain — CRG · Graphify · RTK · repowise

Four complementary tools drive a Goat OS review. Three of them carry an index
that refreshes on edits through hooks — **CRG, the docs graph, and repowise**.
RTK is different: it is a PreToolUse diff/output **router** with no index. It
reshapes large display diffs and noisy command output before the tool call; it
never rebuilds anything and never goes stale. All four are set up by
`make ai-setup`; verify portability with `make ai-doctor`. All outputs are
gitignored and machine-local.

Golden order: **graph-first, files-last.** One graph query replaces many
grep/read cycles. Native Grep/Read is the fallback for graph blind spots.

> Verify tool/target/table names against committed source at review time. The
> make targets, `cmd/*` binaries, tables, and status values named below are the
> ones confirmed to exist as of writing; treat any that no longer resolve as
> drift and check the current `Makefile`, `apps/admin-web/package.json`,
> `backend/cmd/`, and migrations rather than trusting this list.

---

## 1. CRG (code-review-graph) — code structure

Persistent Tree-sitter graph. First pass for callers/callees, blast radius,
architecture, dead code, and test coverage. `repo_root` = your goatos checkout
(`git rev-parse --show-toplevel`).

**Tool namespace differs by harness — verify the live prefix, don't hardcode
one.** The MCP server exposes the same tools under a harness-specific prefix:

- Claude harness: `mcp__code-review-graph__<tool>` (hyphens)
- Codex harness: `mcp__code_review_graph__<tool>` (underscores)

Resolve the actual prefix from your loaded tool list before calling. To load the
review tools, `ToolSearch` for them by role (names below are stable across
harnesses; only the server prefix changes):

```
ToolSearch "get_minimal_context detect_changes get_review_context get_impact_radius query_graph get_affected_flows get_architecture_overview semantic_search_nodes"
```

Route by task shape (do not run all of them every time):

| Question | Tool (role) |
|---|---|
| Cold start on a diff/PR | `get_minimal_context_tool` then `detect_changes_tool` |
| What changed + risk + test gaps | `detect_changes_tool` |
| Source snippets for the changed area | `get_review_context_tool` |
| Blast radius of a change | `get_impact_radius_tool` |
| Which kernel flows a change touches | `get_affected_flows_tool` (verified to exist), then targeted `query_graph_tool` |
| Who calls / depends on / tests X | `query_graph_tool` (callers_of / callees_of / imports_of / tests_for) |
| Find code by keyword/domain | `semantic_search_nodes_tool` |
| High-level shape / coupling | `get_architecture_overview_tool` |

CRG blind spots — fall through to Grep/Read: HTTP route strings, middleware wired
by reflection/string keys, config/env constants, SQL query strings, uncommitted
code, and any `callers_of = 0` / empty `tests_for` that looks wrong (verify with
grep AND the freshness check below). If the graph returns nothing for an
obviously-existing symbol, the file may not be parsed yet — run
`make ai-rebuild-code` or fall back to native.

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
org model; lives outside this repo, cannot be rebuilt here). This is the one
allowed absolute path, since the wiki graph is not repo-relative:

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

## 3. RTK — diff / output router (no index, never stale)

Keeps large `git diff` / `git show` output from flooding review context. The
`tools/agent-hooks/pre-rtk-git-diff.sh` PreToolUse hook auto-routes any display
diff larger than the size gate through the `rtk` binary (installed by
`make ai-setup`; `brew install rtk`). This is pure display reshaping **at call
time** — RTK has no graph or index, nothing to rebuild, and no auto-update
concern.

```bash
git diff main...HEAD        # auto-routed through rtk if large
rtk git diff main...HEAD    # explicit
```

Controls (verified):
- `GOATOS_RTK=0` or `GOATOS_RTK_GIT_DIFF=0` — disable routing for this command
- `GOATOS_RTK_MIN_BYTES=0` — route every safe display diff (default gate: 50000)

The hook only touches human-display diffs; programmatic/piped/tiny diffs pass
through untouched.

---

## 4. repowise — code health, defect risk, dead code

Quality/health/risk layer. Run from the repo root (needs the local `.repowise/`
index; built by `make ai-setup`, refreshed by `make ai-rebuild-repowise` and the
on-edit auto-update hook).

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
2. **Diff** — `git diff main...HEAD` (RTK auto-routes the display if large).
3. **Impact** — CRG `get_impact_radius_tool`; `get_affected_flows_tool` for
   kernel flows; targeted `query_graph_tool` (`callers_of`, `callees_of`,
   `tests_for`) for coverage and flow checks.
4. **Health/risk** — repowise `risk` / `health` / `dead-code`.
5. **Business cross-check** — Graphify docs graph for the touched rules; read the
   authoritative doc.
6. **Read suspects** — Grep/Read the flagged files for blind spots (SQL, routes,
   constants, migrations, uncommitted code).

Then apply the layer checklist (`kernel-and-scale.md`, `backend.md`,
`frontend.md`, `business-rules.md`) and report by severity.

---

## Auto-update — what refreshes, and the stale-graph freshness gate

**Only three of the four tools carry an index that auto-updates. RTK is a
router with no index.**

| Tool | Auto-updates on edit? | Trigger | Refresh manually |
|---|---|---|---|
| CRG (code graph) | Yes | PostToolUse on `Write`/`Edit`/`MultiEdit` (incremental `code-review-graph update`) | `make ai-rebuild-code` |
| Graphify (docs graph) | Yes | PostToolUse when a tracked `.md` changes (re-extract changed docs; `GOATOS_DOCS_GRAPH_AUTOREBUILD=0` to disable) | `make ai-rebuild-docs` |
| repowise | Yes — **on edit**, not commit-only | PostToolUse on `Write`/`Edit`/`MultiEdit` (`repowise update`) | `make ai-rebuild-repowise` |
| RTK | N/A (no index) | PreToolUse display **router** — reshapes output at call time, nothing to rebuild | — (nothing to refresh) |

`make ai-rebuild` refreshes all three indexed tools at once. Verify the clone is
wired and graph artifacts stay untracked with `make ai-doctor`.

### STALE-GRAPH FRESHNESS GATE (do not trust a stale zero)

Auto-update runs **asynchronously in the background** after an edit. During an
active review the graph can lag the working tree by seconds to minutes, or be
built against an older revision than the one you are reviewing. A stale graph
reports **false negatives**: `callers_of = 0`, empty `tests_for`, empty
`imports_of`, or "symbol not found" for code that in fact exists.

Before you treat any empty/zero CRG result as ground truth in a review finding:

1. **Confirm the graph reflects the reviewed revision.** A background refresh may
   not have landed yet for freshly changed or freshly checked-out code. If in
   doubt, force it: `make ai-rebuild-code` (CRG), `make ai-rebuild-docs`
   (Graphify), or `make ai-rebuild` (both indexed graphs), then re-query.
2. **Cross-check a zero with grep.** A `callers_of = 0` / empty `tests_for` on a
   symbol you expect to be used is a graph-freshness signal, not evidence of dead
   code or missing tests. Confirm with `grep`/`rg` before writing it up.
3. **Never escalate a stale-graph zero to a finding.** "No callers", "no tests",
   "unused", and "no importers" claims must survive both a rebuild and a grep
   cross-check first.

RTK output never needs this gate (it is live display reshaping, not an index).
