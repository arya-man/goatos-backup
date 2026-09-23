# Read-First List, Code Navigation and Agent Tooling

> Moved verbatim from `AGENTS.md` (split 2026-09-24 to keep session start small).
> These rules are as binding as `AGENTS.md` itself. Only file location changed.

Read first:

- `context/README.md`
- `SKILLS.md`
- `.agents/skills/goatos-build/SKILL.md`
- `context/architecture/final-architecture.md`
- `context/frontend/final-frontend-mobile-backend-architecture.md`
- `docs/mobile/README.md` (Goat OS Android app — one common role-aware app for field operator + leadership; native Kotlin + Compose, `apps/goatos-android/`, app id `sg.mesha.goatos`; read before any mobile work)
- `context/forms/final-forms-sop-engine.md`
- `context/analytics/final-analytics-infra.md`
- `context/agents/ai-agent-context-and-protocols.md`

Android verification is not allowed to stop at a missing inherited `JAVA_HOME`
or unavailable USB phone. Run `make android-doctor`; repo tooling resolves the
pinned JDK/SDK itself. Run `make android-dev-run`; it prefers an authorized
physical phone and otherwise starts/waits for the configured emulator. See
`docs/runbooks/android-dev-device.md`. JDK 21 runs Gradle/AGP; app bytecode
compatibility remains Java/Kotlin 17.

Historical planning/archive docs were removed from the active tree. If a human
explicitly asks for archaeology, use git history or source material rather than
normal build docs.

Code navigation (graph-first):

- For code-structure questions (callers, callees, dependencies,
  blast-radius/impact, diff review, architecture, hub/dead-code), query the
  `code-review-graph` MCP tools before broad file scans. Read files for what the
  graph cannot see: constants, config values, HTTP route strings, error text,
  and uncommitted code.
- Route by task shape, not ritual:
  - cold/review/diff: `get_minimal_context_tool` first, then one targeted graph
    query;
  - known symbol: go straight to `query_graph_tool`;
  - keyword/domain lookup: `semantic_search_nodes_tool`, then targeted graph;
  - single file/function read: read the file, then graph only for impact.
- Graph is the fast first pass for traversal; native Grep/Read is the fallback
  for graph blind spots. One graph query replaces many grep/read cycles when the
  question is graph-shaped.
- Framework/library docs routing: for implementation, debugging, dependency
  upgrades, or review that depends on third-party APIs/framework behavior, use
  the local Context7 docs cache before relying on model memory. `make ai-setup`
  and `make ai-doctor` run `tools/agent-docs/ensure-context7.sh`, which fetches
  the Context7 key from Secret Manager when local Mesha `gcloud` auth is
  available and registers Context7 MCP for Codex/Claude when those CLIs exist.
  Query order is: repo code graph and Goat OS docs first; then local framework
  docs under `agent-docs/context7/` or `.agent-docs/context7/`; then live
  Context7 for missing/stale topics. Fetch narrow topic docs only (for example
  "Next.js route handlers caching" or "Room migration testing"); never load
  entire library documentation into the model context.
- Setup is per-machine and agent-enforced on fresh clones: if `make ai-setup`
  has never run on this clone, the committed `ai-setup-guard` hook blocks the
  first real tool call with bootstrap instructions — run `make ai-setup` first,
  then resume the task (see `docs/ai/README.md`). The graph DB (`.code-review-graph/`) and
  Graphify outputs (`graphify-out/graph.json`, reports, cost files, cache) are
  gitignored and regenerated locally. To enable the portable setup, run
  `make ai-setup`; to rebuild local graphs, run `make ai-rebuild`; to verify the
  clone is wired without committed graph artifacts, run `make ai-doctor`.
- Maintainer-local only: the Graphify Mesha wiki/doc/visual graphs
  (`mesha_docs_graph`, `mesha_visual_graph`) are built from sources outside this
  repo and cannot be reproduced here. Use them if already configured; otherwise
  skip and use Grep/Read.
- **Agent tool choice (human)**: before a non-trivial task, read
  `docs/ai/agent-tool-routing.md` — Cursor for admin-web UI and small fixes;
  Claude Code (terminal `claude` in repo root) for contracts, backend engine,
  migrations, and multi-module work. No second IDE required.
