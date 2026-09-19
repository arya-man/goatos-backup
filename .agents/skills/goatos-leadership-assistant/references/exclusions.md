# Documented exclusions — what is legitimately NOT leadership-relevant

Not every table/API/feature needs a Cube metric or a `ceo_ai` view. When a change
is genuinely not a leadership read surface, satisfy the guard by adding an
**exclusion row** in `docs/ceo-ai/coverage-matrix.md` with a concrete reason.

## Legitimate exclusion categories

| Category | Examples | Why excluded |
|---|---|---|
| Operator-only pickers / form metadata | shifting destinations, task option-values, SOP version render | App write-flow inputs, not a leadership metric |
| Per-record detail | `GET /goats/{id}`, passport, timeline, single-event/history | Leadership answers stay aggregate; detail tools are per-animal |
| Infra probes | `/healthz`, `/livez`, `/readyz`, `/version` | No tenant scope, no business data |
| Client/session bootstrap | `/app/config`, `/app/bootstrap`, `/app/me`, `/admin-web/bootstrap` | RBAC/chrome config, not analytics |
| Binary/media retrieval | proof download / signed URL | Not an aggregate |
| Device / app-session fleet | `workforce_member_devices`, `app_sessions` | Ops-admin telemetry, not a leadership KPI |
| PII-only surfaces | staff personal identifiers | Return role/position/active status only; never personal detail |
| Operator-scoped app reads | `/app/vaccination/*`, `/app/tasks`, `/app/roster/*` | Self-scoped operator views; leadership uses the admin equivalents |

## What is NOT a valid exclusion

- "No time to add the view" — add a `ceo_ai` view with typed `NULL -- TODO`
  placeholders instead.
- "The number is hard to compute" — draft the Cube base view; mark exploratory.
- A domain leadership would plausibly ask about (breeding pipeline, notification
  delivery health, feed adherence, mortality rate) — these are **gaps to close**,
  not exclusions. Add at least a draft view + GenAI intent (or a scoped-refusal
  copy so the bot says "not covered yet" rather than inventing).

## Exclusion row format (in coverage-matrix.md) — TYPED since 2026-09-19

```
| <table/API/feature> | EXCLUDED:<category> | <one-line concrete reason> |
```

| Category | Use for |
|---|---|
| `EXCLUDED:config` | authoring screens, vocabularies, settings, ration grids, protocol editors |
| `EXCLUDED:write` | mutation routes, their handlers/validators/idempotency helpers |
| `EXCLUDED:pii` | a person's private data (phone, address, identifiers) |
| `EXCLUDED:detail` | operator execution detail behind an aggregate the assistant already covers |
| `EXCLUDED:infra` | probes, telemetry, device fleet, migration bookkeeping, event plumbing |

A bare `| EXCLUDED |` fails `make leadership-assistant-coverage-guard` unless the
surface cell is listed in `tools/agent-hooks/leadership-assistant-exclusion-baseline.txt`
(the rows written before the rule). That file only shrinks: type an old row and
delete its baseline line; never add a line to land a new bare exclusion.

**A read the page shows cannot be excluded.** If an admin-web page contract
declares a `kpi.*`/`chart.*` key for it, the page-contract drift guard requires a
covered or `PLANNED:P<n>` row, not an exclusion (except a typed `config`/`write`
row whose numbers are covered elsewhere and say so — e.g. `/sales/config`).

The scaffold emits this for you:

```bash
node tools/ceo-ai/scaffold-coverage.mjs <name> --exclude "operator-only picker; not a leadership aggregate"
```
