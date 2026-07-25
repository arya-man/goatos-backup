# Mesha Cube Core — governed metric layer

Cube is the **governed metric service** for the Mesha leadership assistant. It
owns the official KPI formulas and is the single source of truth for each
number. It is **not** Vertex/Gemini (the planner) and **not** MCP Toolbox (the
DB tool server).

Read-path routing hierarchy the assistant follows:

1. **Cube** — any official leadership KPI/trend/comparison (this layer).
2. Mesha read APIs — operational/app-shaped reads.
3. MCP Toolbox curated `ceo_ai.*` views.
4. Read-only SQL fallback — only when nothing above covers it.

## Layout

```
analytics/cube/
  cube.js                 # config: tenant security (queryRewrite), no secrets
  METRICS.md              # metric registry + draft/approved governance
  model/
    cubes/                # one file per domain — the metric FORMULAS (SSOT)
      animals.yml         # active_animal_count (approved), mortality_rate (draft)
      vaccination.yml     # due / overdue / compliance (draft)
      feed.yml            # feed_cost (draft/blocked — no price source)
      procurement.yml     # procurement_cost (draft/blocked — no cost source)
      workforce.yml       # operator_completion_rate (draft)
    views/
      leadership.yml      # kpi_animals / kpi_vaccination / ... stable public names
```

## Metric governance

Every metric carries `status: draft | approved`, an owner, and a source note in
`METRICS.md`. Only **approved** metrics serve official leadership numbers; a
**draft** metric is usable in dev but the assistant labels it "draft metric —
pending business sign-off". Metrics whose business meaning is unclear stay draft
with an explicit AMBIGUITY note. See `METRICS.md`.

## Security model (mandatory)

- `tenant_id` is **never** taken from user text. The Mesha backend signs a
  short-lived JWT (HS256, `MESHA_CUBE_API_SECRET`) whose security context carries
  `{ tenant_id }` from the server session.
- `cube.js` `queryRewrite` injects `tenant_id = <session tenant>` on every cube a
  query references and **rejects** any query with no tenant context.
- The **browser never calls Cube directly** — only the Mesha backend does, via
  `backend/internal/ceoai/cubeclient`.

## Source today vs migration path

Cubes currently read canonical `public.*` tables (verifiable now against a SQL
oracle). The measure formulas are the governed contract; when the `ceo_ai.*`
reporting views (and later BigQuery/dbt marts) land, only each cube's `sql:`
FROM changes — the metric numbers do not.

## Run it locally

```bash
export MESHA_CUBE_API_SECRET=<any-local-secret>
export MESHA_CUBE_DB_PASSWORD=<goatos-local-db-password>
tools/dev/run-cube-local.sh          # starts Cube on http://127.0.0.1:4000
tools/dev/run-cube-local.sh status   # health probe
tools/dev/run-cube-local.sh logs     # follow logs
tools/dev/run-cube-local.sh stop     # tear down
```

Full local workflow + SQL-oracle verification: `docs/runbooks/cube-local.md`.
Staging (Cloud Run `mesha-cube-stg`): `deploy/cube/README.md`.
