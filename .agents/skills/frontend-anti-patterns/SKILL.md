---
name: frontend-anti-patterns
description: >-
  Use when writing OR reviewing admin-web (apps/admin-web/**) — pages, SSR data
  reads, nav, labels, dashboards. Enforces the backend-owns-the-contract golden
  rule, no SSR full-table request reads, mock-fidelity, and projection-backed
  dashboards. Invoke before touching an admin-web page/route/data-read and before
  pushing. Machine gates: npm run check:mock-fidelity + make admin-web-request-reads-guard.
---

# Frontend (admin-web) anti-patterns

Admin-web is a RENDERER, not a product-truth owner. Canonical: AGENTS.md golden
frontend rule + `context/frontend/current-admin-web-scope.md` + the mock
`mock/goatos-dashboard-mock.html`.

## Backend owns the contract (no client-hardcoding)
The backend OpenAPI/`/admin-web/bootstrap` contract owns: navigation, route
availability, page titles, section/table labels, filter/sort/page-size, chips/tabs,
row-click params, drawer/action labels, empty/error copy, disabled reasons,
summary-vs-detail fields. The client owns layout/CSS/density/local UI state only.
- Do NOT hardcode a visible label/action/disabled-reason in a page — move it into
  the contract (or `admin_ui_config_entries`) or document a temp exception in
  `context/frontend/`. Machine-checked by `check-ui-contract-literals`.
- Nav is module-grant-composed (see `nav-composition` skill), never a hardcoded
  per-role/per-module list.

## No SSR full-table request reads (scale)
The admin-web twin of compute-on-read: a Next.js SSR helper draining a paginated
endpoint cursor-by-cursor into one array to compute a KPI (the `searchAllGoats`
full-herd walk, removed in `810bc1b3`) is BANNED. Read a projection/summary
endpoint instead. Machine-blocked by `make admin-web-request-reads-guard`.
Also flagged: serial awaits in a loop, unbounded request fan-out
(`check-serial-await`, `check-request-plan-fanout`, action-center/calendar
request-plan checks).

## Mock-fidelity (the ONLY UI source of truth)
The mock `mock/goatos-dashboard-mock.html` is the UI/UX source of truth — PORT its
structure/tables/empty-states/icons/density. NEVER reuse/recolor the old admin UI.
Un-backed control = mock look + disabled-with-reason. Mandatory before any frontend
push: `npm --prefix apps/admin-web run check:mock-fidelity` (IA guard, UI-contract
literals, serial-await, request-plan, mock-fidelity) + rendered visual QA
(`smoke:visual:live`). Build passing ≠ UI ships.

## Dashboards slice by dimension → projection
Any dashboard slicing by month/date/breed/farm/shed/status/etc. uses the canonical
rule in `docs/decisions/high-scale-dashboard-projections.md` — projection-backed,
not compute-on-read.

## Command-lens / authority guardrail
Control Tower, Action Center, Calendar, Protocol Adherence, Workflows are top-level
screens only; Config + SOP Library are Admin/Data-Ops authority screens only. Do
not nest them under a vertical; feed them via `?domain=`/`?category=` filters.
