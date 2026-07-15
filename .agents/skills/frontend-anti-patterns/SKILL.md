---
name: frontend-anti-patterns
description: >-
  Use when writing OR reviewing admin-web (apps/admin-web/**) — pages, SSR data
  reads, nav, labels, dashboards. Covers the backend-owns-the-contract golden
  rule, no SSR full-table request reads, selected-window fetch=render, mock
  fidelity, and projection-backed dashboards. Thin entrypoint: detailed rules live
  in the canonical chapters linked below. Invoke before touching an admin-web
  page/route/data-read and before pushing. Machine gates: npm run check:mock-fidelity
  + make admin-web-request-reads-guard.
---

# Frontend (admin-web) anti-patterns — lens entrypoint

Admin-web is a RENDERER, not a product-truth owner. The backend contract owns
what is visible; the client owns layout and local UI state.

This skill is a **table of contents**, not the rulebook. Open the canonical
chapters below; do not review from the summary.

## When this lens applies
- Any change under `apps/admin-web/**` (pages, SSR data reads, nav, labels,
  drawers, dashboards) or `packages/ui` / `packages/rbac` / `packages/forms-dsl`.
- Any date-range / week / month picker feeding a fetch and a render.
- Any dashboard that slices by month/date/breed/farm/shed/status/etc.

## Canonical detail (read these — do NOT duplicate here)
- **Review chapter:** [`.agents/skills/goatos-code-review/references/frontend.md`](../goatos-code-review/references/frontend.md) (+ [`.agents/skills/goatos-code-review/references/mobile.md`](../goatos-code-review/references/mobile.md) for the mobile twin).
- **Selected-window fetch=render + reminder completeness:** [`docs/decisions/calendar-ownership.md`](../../../docs/decisions/calendar-ownership.md).
- **Projection-backed dashboards:** [`docs/decisions/high-scale-dashboard-projections.md`](../../../docs/decisions/high-scale-dashboard-projections.md).
- **Over-fetch anti-patterns (twin):** [`docs/decisions/mobile-data-fetch-anti-patterns.md`](../../../docs/decisions/mobile-data-fetch-anti-patterns.md).
- **Nav composition:** [`docs/decisions/role-module-nav-composition.md`](../../../docs/decisions/role-module-nav-composition.md) + the `nav-composition` skill.
- **Contract authority:** AGENTS.md golden frontend rule · [`context/frontend/current-admin-web-scope.md`](../../../context/frontend/current-admin-web-scope.md) · the mock `mock/goatos-dashboard-mock.html`.

## Machine gates
- `npm --prefix apps/admin-web run check:mock-fidelity` — mandatory before any
  frontend push (IA guard, UI-contract literals, serial-await, request-plan, mock).
- `make admin-web-request-reads-guard` — no SSR full-table request read.
- `node tools/agent-hooks/check-refresh-binding.mjs` — selected-window binding.
  All registered in `tools/ci/guardrail-manifest.json`.

## At a glance (detail in the links above)
- **Backend owns the contract:** nav, titles, labels, filter/sort/page-size,
  chips, row-click params, drawer/action labels, empty/error copy, disabled
  reasons, summary-vs-detail — never hardcoded in a page.
- **No SSR full-table request reads:** don't drain a paginated endpoint cursor-by-
  cursor into one array (the `searchAllGoats` walk); read a projection/summary.
- **Selected-window drives fetch AND render:** one window for both; no implicit
  `now`/`today` substituted server-side; render the actual-returned window.
- **Reminder/candidate completeness:** a paginated reminder loop must reach every
  candidate or clearly mark "partial" — no silent page-size truncation.
- **Dashboards → projection:** slice-by-dimension is projection-backed, not
  compute-on-read.
- **Command-lens authority:** Control Tower / Action Center / Calendar / Protocol
  Adherence / Workflows are top-level only; feed via `?domain=`/`?category=`.
