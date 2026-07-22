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

## Exclusion row format (in coverage-matrix.md)

```
| <table/API/feature> | EXCLUDED | <one-line concrete reason> |
```

The scaffold emits this for you:

```bash
node tools/ceo-ai/scaffold-coverage.mjs <name> --exclude "operator-only picker; not a leadership aggregate"
```
