# Leadership Assistant — Developer Guide

How any developer, on any laptop, with Claude **or** Codex, extends and runs the
Mesha leadership assistant (the CEO/CXO read-only chatbot) while keeping coverage
complete. This guide is the human companion to the machine guard and the skill.

- **Skill (HOW-TO):** `.agents/skills/goatos-leadership-assistant/SKILL.md`
  (+ `references/coverage-howto.md`, `architecture.md`, `exclusions.md`).
- **Guard:** `make leadership-assistant-coverage-guard`
  (`tools/agent-hooks/check-leadership-assistant-coverage.mjs`).
- **Scaffold:** `node tools/ceo-ai/scaffold-coverage.mjs <module> [--kpi] [--exclude "…"]`.
- **Backfill baseline:** `docs/ceo-ai/coverage-matrix.md`.
- **Plan / build state:** `docs/ceo-ai/ceo-chatbot-purpose-and-build-plan.md`,
  `docs/ceo-ai/mcp-toolbox-plan.md`, `docs/ceo-ai/mcp-toolbox-tools.yaml`.

## The coverage contract

Every leadership-relevant **table, read API, OpenAPI contract, admin-web route,
mobile workflow, reporting/projection table, domain event, or official KPI**
must resolve to exactly one of: a Cube governed metric, a `ceo_ai.*` view, an MCP
Toolbox tool, a mapped Mesha read API, or a documented exclusion. The guard fails
any trigger-path change that lacks a matching `docs/ceo-ai/**` coverage update.

## Read-path routing hierarchy (Cube-first)

1. **Cube** — official leadership KPIs (governed formulas; never model-invented
   SQL when a metric exists).
2. **Mesha read APIs** — operational/app-shaped reads.
3. **MCP Toolbox** — curated `ceo_ai.*` tools.
4. **Read-only SQL fallback** — only when no metric/API/tool exists, via
   `mesha_ceo_readonly` + SQL guard (single SELECT, tenant predicate, `LIMIT<=100`,
   `ceo_ai.*` allowlist).

Cube is a separate Cube Core service (not Vertex, not MCP). The browser never
calls Cube/Toolbox/Postgres directly — only the backend does, server-side.

## Adding coverage for a new feature (quick path)

```bash
# 1. Generate stubs
node tools/ceo-ai/scaffold-coverage.mjs <module>            # covered feature
node tools/ceo-ai/scaffold-coverage.mjs <module> --kpi      # + Cube metric
node tools/ceo-ai/scaffold-coverage.mjs <module> --exclude "reason"

# 2. Paste each stub into its real file (view migration, tools.yaml, query-space,
#    eval golden, coverage-matrix) and fill the derivations. Map EVERY leadership
#    column to a real source or a typed NULL::type -- TODO placeholder.

# 3. Add/adjust the coverage-matrix row, then verify:
make leadership-assistant-coverage-guard
node tools/ceo-ai/scaffold-coverage.mjs --self-test
```

Full per-tier detail: `references/coverage-howto.md`.

## Architecture map

Server-owned Go brain (`backend/internal/ceoai`, hexagonal): planner
(Vertex/Gemini) → 4-tier router → guards (injection, moderation, sqlguard) →
adapters (vertex, cube, toolbox, postgres, cache) → persistence + SSE streaming.
Gemini never holds DB creds, executes SQL, or decides permissions. Tenant + role
come from the session; all user/tool text is data. Response fields are exactly
`answer, source, mode, request_id, (tokens), citations, conversation_id` — step
traces are internal only. See `references/architecture.md`.

## Running locally

- DB: docker `goatos-local-current` at `127.0.0.1:5433` (db `goatos`).
- API `:8080`, admin-web `:3300`, MCP Toolbox `:5001`, Cube `MESHA_CUBE_URL`
  (default `127.0.0.1:4000`).
- Vertex via ADC, project `goatos-stg`, `asia-south1`, `gemini-2.5-flash`.
- Secrets/config come from Google Secret Manager (project `goatos-stg`) + GitHub
  Actions secrets (repo `vgoats/goatos`), pulled into a gitignored
  `.env.ceo-ai.local` by the documented gcloud fetch. Never commit a secret
  value. Verify `account=ravi@mesha.sg`, `project=goatos-stg` before any secret
  write.

## Eval

`tools/ceo-ai/eval` runs the golden question set (census, vaccination,
feed/shifting/procurement, ops/workforce, adversarial) with SQL oracles + a
grounding assertion against a seeded local DB, wired into `make ci-local`. Add a
golden Q whenever you add a query-class.

## Fresh-clone pickup

A new clone gets everything: the skill (source + Claude symlink), the guard
(registered in the manifest, `make guardrails`, `run-local-ci.sh`), the scaffold,
this guide, the coverage matrix, and the PostToolUse reminder in both
`.claude/settings.json` and `.codex/hooks.json`. Nothing is gitignored. The guard
enforces coverage from the fully-covered baseline in `coverage-matrix.md`.
