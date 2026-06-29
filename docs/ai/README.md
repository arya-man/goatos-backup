# AI Tooling Setup

Goat OS ships the rules and scripts for token-efficient AI work, but it does
not ship generated graph databases. Every developer builds graphs locally from
their own checkout.

## What Is Committed

- Agent routing rules: `AGENTS.md`, `CLAUDE.md`, `.agents/skills/`, `.claude/skills/`,
  and `.cursor/rules/`.
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

`make ai-doctor` fails if generated graph artifacts become tracked.

## Fresh Clone Setup

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
- Trust the repo hooks when Codex prompts for trust.
- Use `AI_BACKEND=codex` for local docs graph rebuilds.

Cursor:

- Uses `.cursor/rules/ai-graph-routing.mdc` for routing guidance.
- Cursor does not provide the same deterministic hook wall here, so run
  `make ai-doctor` and `make ai-rebuild` manually after setup or major doc/code
  changes.

## Telemetry

Run:

```bash
make ai-telemetry
```

This summarizes local Claude/Codex transcript token usage, graph-tool signals,
and RTK signals for Mesha workspace sessions, which is where Goat OS agents are
normally launched. Use `--project goatos` only when analyzing clone-rooted
sessions. It is a local measurement aid; do not commit transcript data.
