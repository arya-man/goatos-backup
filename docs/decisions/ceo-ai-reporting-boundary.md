# ADR: The `ceo_ai` reporting/assistant namespace must not sit between core BE ↔ FE ↔ Mobile layers

Status: Accepted (2026-07-23)

## Context

`ceo_ai` in this repo does double duty:

1. **Leadership-assistant namespace** — the CEO/CXO read-only chatbot
   (`POST /api/ceo-ai/ask`, `backend/internal/ceoai/**`, `apps/admin-web/features/ceo-ai*`).
2. **Reporting SQL schema** — `ceo_ai.*` read-only views/functions that Cube and
   the assistant read for governed leadership metrics (migrations 000024–000031).

The intended, correct data direction is one-way:

```
core BE (operator source of truth) ── read APIs / MCP / read-only SQL ──▶ assistant (ceoai) / Cube
```

The assistant and Cube **consume** core operational data. They must never sit
**between** the core Backend ↔ Frontend ↔ Mobile operator layers, and core layers
must never read `ceo_ai.*` for their own runtime data.

### What went wrong

The Control Tower / process-integrity query (`/control-tower/vaccination`,
`/vaccination/action-center*`) joined `ceo_ai.vaccine_label_for(...)` purely to
turn an internal dose code into a display label. That made a **core operator
read path depend on the reporting schema**. When the `ceo_ai` schema was absent
(dropped out-of-band; migrations still recorded as applied), the whole
command-room screen 500'd:

```
processintegrity: iterate projection rows:
ERROR: schema "ceo_ai" does not exist (SQLSTATE 3F000)
```

`/readyz` and the calendar data plane stayed green — only the ceo_ai-coupled
screens broke.

## Decision

1. **Vaccine label composition is core, backend-owned Go.** The single source of
   truth is `backend/internal/vaccination/domain.DoseDisplayLabel`. Control Tower
   composes its display label via
   `backend/internal/processintegrity/domain.ControlTowerDoseLabel` (antigen label
   + course/dose/booster qualifiers). The SQL `ceo_ai.vaccine_label_for()` join is
   removed from the process-integrity query; `dose_code` is emitted raw and
   labeled after scan (the on-the-wire contract is unchanged — `dose_code` has
   always carried the display label).

2. **Standing boundary rule.** No core operator layer may depend on the
   `ceo_ai` reporting/assistant namespace:
   - **Backend** (`backend/internal/**`, except `backend/internal/ceoai/**`) must
     not issue SQL against the `ceo_ai` schema or `ceo_ai_*` tables. Machine-gated
     by `make ceo-ai-boundary-guard`
     (`tools/agent-hooks/check-ceo-ai-core-boundary.mjs`), part of `make guardrails`
     and `make ci-local`.
   - **Frontend** (`apps/admin-web/**`) and **Mobile** (`apps/goatos-android/**`)
     render backend operator contracts. Core pages/screens must not route their
     data through `/api/ceo-ai/*` or import an assistant client module. The
     assistant UI bubble mounted globally in `MeshaShell` is allowed chrome
     (server-gated); it is a consumer surface, not a data path for other
     features. Machine-gated by the same `ceo-ai-boundary-guard` "client" scan
     (blocks `/api/ceo-ai` / `/ceo-ai/` route strings and assistant-module
     imports outside the assistant-owned dirs + global chrome).

3. **The assistant keeps reading core data** via Mesha read APIs, the MCP
   Toolbox `ceo_ai.*` tools, or read-only SQL — that direction is unchanged and
   correct. Cube legitimately reads `ceo_ai.*`. `backend/migrations/**` owns the
   schema definition. None of those are core runtime consumers.

## Consequences

- A dropped/absent `ceo_ai` schema can no longer take down Control Tower / Action
  Center. Proven on the real path: with `ceo_ai` dropped, both endpoints return
  200 with identical labels.
- Any future core read path that reaches into `ceo_ai.*` fails `ceo-ai-boundary-guard`
  in local CI and the PostToolUse nudge, with a clear remediation: move the logic
  to a core Go/SQL source, or (if genuinely assistant-owned) into
  `backend/internal/ceoai/**`.
- New shared display/derivation logic needed by both an operator screen and the
  assistant belongs in a neutral core package (e.g. `vaccination/domain`), read by
  both — never in the `ceo_ai` reporting schema as the primary source.

## Exception

A genuinely-justified core reference carries a COMPLETE inline directive:

```
ceo-ai-boundary:ignore: owner=<name> issue=<url|id> scope=<why> expiry=<YYYY-MM-DD>
```
