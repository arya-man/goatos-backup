# AI Tooling Setup

Goat OS ships the rules and scripts for token-efficient AI work, but it does
not ship generated graph databases. Every developer builds graphs locally from
their own checkout.

## What Is Committed

- Agent routing rules: `AGENTS.md`, `CLAUDE.md`, `.agents/skills/`, `.claude/skills/`,
  `.cursor/rules/`, `.cursor/hooks.json`, and `docs/ai/agent-tool-routing.md`.
- Hooks for Claude and Codex: `.claude/settings.json` and `.codex/hooks.json`.
- Portable scripts: `tools/agent-hooks/*.sh`, `tools/agent-hooks/*.mjs`, and
  `tools/ai/analyze-transcripts.py`.
- Graph ignore rules: `.gitignore` and `graphify-out/.gitignore`.

## What Is Local Only

- CRG database: `.code-review-graph/`.
- Graphify generated files: `graphify-out/graph.json`,
  `graphify-out/GRAPH_REPORT.md`, `graphify-out/graph.html`,
  `graphify-out/manifest.json`, `graphify-out/cost.json`,
  `graphify-out/cache/`, and temporary `.graphify_*` files.
- Transcript telemetry from Claude/Codex sessions.
- The multi-repo **workspace-root `/code-review` delegator** (needed only when a
  maintainer opens Claude Code at a parent directory containing this `goatos`
  checkout, not at the repo root). It lives outside this repo; recreate it from
  the version-backed template in
  [`workspace-code-review-delegator.md`](workspace-code-review-delegator.md).

`make ai-doctor` fails if generated graph artifacts become tracked.

## Fresh Clone Setup

Setup is agent-enforced, not optional-by-silence. On a clone that has never run
`make ai-setup`, the committed hooks make the FIRST agent prompt bootstrap the
stack before doing the actual work:

- `tools/agent-hooks/ai-setup-guard.sh` (PreToolUse, Claude + Codex) blocks the
  first real tool call with instructions to run `make ai-setup`, then lets the
  session resume the original task. Setup commands themselves always pass.
- Claude also gets a `SessionStart` notice so the instruction appears before any
  tool call.
- The guard goes silent once `code-review-graph` is installed and
  `.code-review-graph/` exists, or after one setup attempt on the clone
  (marker `<git-dir>/goatos-ai-setup-attempted`), so an offline machine cannot
  deadlock. Disable with `GOATOS_AI_SETUP_GUARD=0`.
- `make ai-setup` also installs a machine-local Git `pre-push` hook scoped to
  `vgoats/goatos`. It blocks every direct update to remote `stg`, including
  `HEAD:stg`, `main:stg`, local `stg`, deletions, and forced updates. Human and
  agent pushes are both covered. The only staging promotion path is a merged
  same-repository `main -> stg` pull request in GitHub.

Run from the repository root:

```bash
make ai-setup
make ai-rebuild AI_BACKEND=auto
make ai-doctor
```

`ai-setup` installs or verifies:

- `code-review-graph` for code structure.
- `graphify` for local docs graph generation/querying.
- `rtk` when Homebrew is available, otherwise it prints the install instruction.
- the Goat OS direct-`stg` pre-push guard without replacing an existing
  machine hook.

`ai-rebuild` creates local generated artifacts:

- `ai-rebuild-code`: builds the CRG database with `code-review-graph build --repo`.
- `ai-rebuild-docs`: rebuilds `graphify-out/` with the configured local AI backend.

The docs graph backend is selected with `AI_BACKEND`:

```bash
make ai-rebuild-docs AI_BACKEND=claude
make ai-rebuild-docs AI_BACKEND=codex
```

`AI_BACKEND=auto` chooses `claude` if installed, then `codex`.

## Why This Exists

The stack reduces waste at different layers:

- CRG saves tokens on graph-shaped code questions: callers, callees, impact,
  tests, architecture, and review context.
- Graphify saves tokens on docs/product questions by querying a graph instead
  of opening many raw docs.
- RTK compresses noisy shell output such as large diffs, test logs, typecheck
  output, and JSON.
- The pre-search guard blocks the first naive broad grep/read and reminds the
  agent to use the graph when the question is graph-shaped.

Generated graphs are intentionally ignored because they are machine-local,
large, stale quickly, and may encode local extraction state. The committed
contract is the repeatable setup, not the generated output.

## Routing Table

Use the cheapest path that matches the task:

| Task shape | First move |
| --- | --- |
| **Which agent tool? (human)** | Read `docs/ai/agent-tool-routing.md` |
| Cold orientation, review, or diff | `get_minimal_context_tool`, then one targeted graph query |
| Known symbol traversal | `query_graph_tool` with `callers_of`, `callees_of`, `imports_of`, or `tests_for` |
| Keyword/domain lookup | `semantic_search_nodes_tool`, then `query_graph_tool` |
| Docs/product question | Graphify query against the local `graphify-out/graph.json` |
| Single file/function body | Read the file directly; use graph only for impact |
| HTTP routes, config, SQL strings, uncommitted code | Native grep/read; these are graph blind spots |

## Agent Notes

Claude:

- Uses `.claude/settings.json` for the pre-search guard, RTK hooks, CRG update,
  and docs graph update.
- Use `AI_BACKEND=claude` for local docs graph rebuilds.

Codex:

- Uses `.codex/hooks.json` for the same guard/RTK/post-edit hooks.
- Uses `tools/agent-hooks/agentmemory-codex-hook.mjs` to pin repo-local
  agentmemory captures to the stable `goatos` project namespace. This is needed
  because Codex can be launched from the Mesha workspace root, which is not the
  Goat OS git root.
- The Codex hook matchers include both the classic `Bash` tool name and the
  newer shell aliases (`exec_command` / `unified_exec`), and the shared hook
  scripts classify shell payloads themselves before blocking or auto-routing.
- Trust the repo hooks when Codex prompts for trust.
- When using agentmemory MCP tools for Goat OS, pass `project: "goatos"` on
  saves and recalls whenever the tool exposes a project argument. Do not rely on
  a workspace-root fallback such as `mesha`.
- Use `AI_BACKEND=codex` for local docs graph rebuilds.

Cursor:

- Uses `.cursor/rules/` for routing and area-scoped guidance (`goatos-core.mdc`,
  `backend.mdc`, `admin-web.mdc`, `contracts.mdc`, `ai-graph-routing.mdc`).
- Uses `.cursor/hooks.json` for project hooks that reuse the same
  `tools/agent-hooks/` scripts as Claude/Codex (graph-first guard, RTK,
  write-scope, post-edit format/boundary/contract-drift, CRG/docs graph update).
  Restart Cursor after editing hooks if they do not reload automatically.
- Run `make ai-doctor` and `make ai-rebuild` manually after setup or major
  doc/code changes.

## Telemetry

Terminal summary:

```bash
make ai-telemetry
```

HTML report (opens in your browser — saved-vs-missed $ model, per-agent
totals, by-day adoption trend, top sessions):

```bash
make ai-telemetry-ui
```

Both call `tools/ai/analyze-transcripts.py`, which reads local Claude/Codex
transcripts (never network) and reports:

- **Token consumption** per agent and per day for Mesha workspace sessions
  (where Goat OS agents are normally launched); Heva sessions are excluded by
  the `mesha` path/cwd filter. Use `--project goatos` only when analyzing
  clone-rooted sessions. Claude subagent/workflow transcripts are counted as a
  separate `claude-sub` agent — they are real additional spend (each subagent
  gets its own context window), so they are never folded into the main `claude`
  line where the cost would be invisible. Codex multi-agent orchestration is
  called out by counting parent-session `spawn_agent` / `wait_agent` /
  `send_input` / `close_agent` calls. Local Codex JSONL does not expose
  child-agent token usage as separate transcript files, so Codex token totals
  remain parent-session totals with spawned agents reported as orchestration,
  not as separately attributable child spend.
- **Graph / RTK adoption** — how many sessions and calls actually routed through
  code-review-graph / Graphify / RTK vs bypassing them with raw grep/read/git-diff.
- **A savings model** — two headline numbers: *saved by tools* (tokens + USD the
  4 tools avoided by absorbing graph/diff calls) and *missed savings (raw
  bypass)*, the grep/read/git-diff work that skipped the stack. The missed
  number is the adoption gap to close, not a missing tool.
  Per-call savings are conservative constants from this repo's eval (~9k tok/graph
  query, ~15k/diff); every assumption is a flag:

  ```bash
  # tune pricing + per-call savings for the money estimate
  python3 tools/ai/analyze-transcripts.py --by-day \
      --price-per-mtok 15 --graph-save 9000 --rtk-save 15000 \
      --html ai-telemetry.html
  ```

The HTML report also has a **savings trend** — a saved-vs-missed bar per period
with **Window** (7d / 30d / 90d / All) and **Bucket** (Day / Week / Month)
toggles, so the numbers show movement instead of an ever-growing all-time pile.
Watch whether the amber *missed* portion shrinks against green *saved* over time —
that is adoption improving. To drop old history from the terminal totals too,
pass `--since YYYY-MM-DD`.

The point is the **missed savings** number: it is an adoption gap, not a missing
tool — route greps through the graph and diffs through RTK to claim it.
It is a local measurement aid; the generated `ai-telemetry.html` is gitignored
and transcript data must never be committed.
