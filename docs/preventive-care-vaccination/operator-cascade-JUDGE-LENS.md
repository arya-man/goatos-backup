# Operator-Config Cascade — Standing JUDGE LENS (run continuously while building)

Purpose: when building the Vaccination Operators HRMS + cascade (see
`vaccination-operators-BUILD-INSTRUCTIONS.md`), a **judge agent must run this lens
after EVERY commit/phase** and block if any check fails. It exists because the
last-30 commits kept re-opening the SAME circles: operator-cap honoring, capacity
SQL, partial-attach ledger scope, execution-date/alias carry through grouped rows,
and aggregate/projection grain+label+scope-column consistency. Do NOT let a
cap/leave/N/default change on the HRMS screen silently break obligation cleanup or
cross-screen data.

How to use: spawn a dedicated judge subagent per phase with THIS file as its lens.
It must verify on the REAL production path (fresh build at the exact SHA — never
the stale :8080/:3300 binary), adversarially try to REFUTE "it works", and return
CONFIRMED/PLAUSIBLE/REJECTED per check. Ledger of open gaps:
`operator-config-cascade-gap-ledger.md`.

---

## 0. The recurring circles (last-30-commit evidence — do NOT re-open these)

| Commit | Circle that kept breaking |
|---|---|
| `d99d2910` honor HRMS operator caps · `2986e7f7` clear caps + prove SQL · `349750a2` remaining-cap + partial-attach · `2986e7f7`/ledger | **Operator cap must actually bound scheduling.** Cap changes must re-bound drives; full operators must NOT get work; remaining-cap (not configured) drives assignment. |
| `a9779fad` carry execution date through grouped rows | **Grouped read alias sync** (execution_due_at vs due_at) — producer↔consumer aliases both exist or CT/AC 500s on a fresh build. Memory `goatos-ctac-alias-contract-a9779fad`. |
| `9ecaaba2`/`9873ff77`/`b6f69419`/`273d631a` counts lifecycle labels + shed subtotal | **Aggregate/projection grain + human labels + subtotal correctness** on all-parks vs park scope. |
| `470148fc` stop dropping park_label/shed_id | **Scope columns (park/shed) must survive** through reader/projection layers. |
| `story_an_drive_date_override_capacity_clinical_test.go` | **Drive-date override must preserve capacity + clinical rules** end-to-end. |
| `partial-attach ledger scope` (F2) | **Only attached obligations' drive rows persist** — no stale rows after partial attach/reschedule. |

If a check below maps to one of these, it is a REGRESSION, not a new feature — fix root cause.

---

## 1. Obligation-cleanup invariant (after cap/N/leave/default/week-off change)

For the affected tenant/park/future window, assert on RAW DB (`obligation_instances`,
`obligation_batches`, `vaccination_drive_assignments`):
- [ ] No obligation LOST: every pre-change open obligation is still present (unbatched or in a valid batch).
- [ ] No obligation DUPLICATED: no obligation in two active batches; no duplicate `vaccination_drive_assignments` row for the same obligation/shed/partition.
- [ ] No STALE drive row: no `vaccination_drive_assignments` row for an obligation that did not attach / was released (F2).
- [ ] No ORPHAN operator row: after N shrink or default swap, no `conducted_by`/operator row that isn't in the new plan (DEFOP-4/N-03 set-reconcile).
- [ ] In-progress/completed history PRESERVED (never released/rewritten).
- [ ] Re-plan is IDEMPOTENT: replay the same config-change event → identical DB state, no new side effects.

## 2. Capacity honoring (the core of "cap changes must matter")

- [ ] `Daily capacity` a park can schedule == `N × common_cap` for that date, minus week-off/leave-unavailable operators.
- [ ] When all eligible operators are at cap (0 remaining) → NO new batch created/locked (F1: `totalVaccinationOperatorCap` returns 0, not fallbackCap).
- [ ] Full operator is NEVER set as `conducted_by`.
- [ ] Common cap is sourced from `vaccination_capacity_config`, NOT `MAX(per-position)` (OPS-CASCADE-2).
- [ ] Lowering cap re-batches existing future drives down to the new cap; raising cap allows more per drive — both proven on raw rows.

## 3. Leave / week-off honoring

- [ ] Adding leave on date range D → operator removed from availability for D; drives on D re-scoped/fallback (N=1) or slice-reduced (N>1); drives NOT cancelled; obligations preserved.
- [ ] Removing leave → reversed (operator regains D; re-optimise).
- [ ] Never two operators on leave the same date (FE + BE reject; OPS-CASCADE-3).
- [ ] Week-off: sole-operator's off-day is not scheduled (WKOFF-3); empty candidate set → defer/flag, not a zero-operator batch at base cap (WKOFF-4).
- [ ] Availability window uses `valid_from <= date` (not `+1 day`) so a seat starting tomorrow isn't counted today (OPS-CASCADE-4).

## 4. Vaccination-rule fidelity (re-plan MUST route through clinical guards)

Any re-batch/reschedule preserves ALL of (`drive_planner.go`):
- [ ] Kid vs adult path selection (from live schedule-path helper, not stale source tags).
- [ ] Boosters, live/killed spacing (28d live-to-live after PPR), ET+TT two-dose course (dose-2 = 21d after dose-1, before the 182d repeat).
- [ ] Per-animal shot cap (hard) + safe-window (soft caps only on last safe day).
- [ ] Clinical-defer set sick/under_treatment/quarantine/icu ALWAYS deferred (never cancelled/scheduled; C35-010).
- [ ] Combo/ET+TT dose-2 pairing (`AlignComboDrivesAsOf`) intact after re-plan.
- [ ] Drive batching stays park-level animal-first safe-window-bound; shed count never a merge constraint.

## 5. Cross-screen consistency (same numbers everywhere after a change)

For the same tenant/park/date/scope, the operator/capacity/obligation change must show consistently — no screen stale, no 500, no dropped scope column:
- [ ] **Control Tower** — gaps/adherence reflect the re-planned state; no raw census KPI; grouped-read aliases synced (no fresh-build 500).
- [ ] **Action Center** — work-state queues reflect released/re-batched obligations.
- [ ] **Protocol Adherence** — expected/actual/gap grain correct after re-plan.
- [ ] **Workflows** — chain/lifecycle for affected batches updated.
- [ ] **Calendar** — day markers + due-work counts match raw obligations for the window (inclusive-vs-exclusive date coverage; freshness TTL > projector schedule).
- [ ] **Vaccination L1 (day list) / L2 (sheds) / L3 (vaccine-capture) / L4** — admin-web AND mobile: counts, operator, capacity, dates all match raw DB; mobile keyset ≤~20/page, Room SSOT, no stale wall.
- [ ] park_label/shed_id and every scope column survive through readers/projections (470148fc).
- [ ] Aggregate/projection grain + human labels + subtotals correct on all-parks vs single-park (counts circle).

## 6. Goat-lifecycle stitching

- [ ] Shed move / stage change (`goat.location.changed` / `goat.stage_changed`) re-scopes operator-driven drives too (rescope open shed-scoped work; re-evaluate eligibility) — the cascade consumer must compose with the existing shift rescope, not bypass it.
- [ ] Transferred/sold terminal exit removes the goat's open drive work; no cross-park move (goats never move parks).
- [ ] Movement never fabricates clinical facts.

## 7. Scale (no regression from the cascade)

- [ ] No N+1 fan-out on operator availability — one shared `(tenant,park,date,cap)` session cache.
- [ ] Re-plan queries keyset/`FOR UPDATE SKIP LOCKED`, SARGable, query-plan proof at ~500k rows (no Seq Scan).
- [ ] Hot CT/AC/WF/Calendar/L1-L4 reads p95/p99 < 500ms; admin-web SSR no full-table request reads; mobile bounded memory + off-main parse.

## 7b. Frontend UX & API integrity (must stay intact — no regressions)

The HRMS rebuild + cascade must NOT degrade any existing screen behaviour:
- [ ] **Backend-driven contract intact** — visible labels/nav/route availability/filter-sort-page-size/chips/row-click params/drawer labels/empty-error copy come from `/admin-web/bootstrap`, not hardcoded; generated client only, no raw URLs/hand DTOs/local route mutations.
- [ ] **No page-load jerkiness** — SSR bootstrap loads once, no layout shift / flash of local defaults replaced by async config; business UI blocks until bootstrap succeeds; hot loads < 500ms (skeleton/prefetch is NOT the fix — fix serving shape).
- [ ] **Sidebar stable** — no reflow/jerk on navigation; active nav highlight stays on the correct module after row-click/drawer open; hover/active states match the mock (`var(--sidebar-2)`, not a faint brand color-mix); no jump when a drawer opens.
- [ ] **List-item / row / matrix-cell clicks intact** — every roster/list row is tap-to-open the correct record drawer (`LocalOverlayLink` + client-local state); one-click open; Back/Esc/outside/X close; NO route/RSC/document request or global route-pending flash on open/close; row-click keeps the right sidebar module selected; opened detail shows only the scoped real records.
- [ ] **Same-page overlays are client-local** — leave calendar dialog + leave drawer + goat-passport drawer don't navigate or re-run the route Server Component; `make admin-web-local-overlay-guard` at zero legacy baseline.
- [ ] **Table rendering** — no right-edge/status-column clipping, no horizontal page overflow (table owns its scroll), chips truncate without character-splitting, dense-table cells nowrap.
- [ ] **Backend APIs healthy** — every read the screen needs returns real data or a visible error state (never an empty array read as "no data"); `route_not_registered`/API errors surface as an alert, not silent empty UI; new grouped reads keep producer↔consumer aliases synced (no fresh-build 500).
- [ ] **Mock fidelity** — `npm --prefix apps/admin-web run check:mock-fidelity` + rendered visual QA (`smoke:visual:live`, open screenshots) before push; port mock anatomy (not a plainer substitute); un-backed controls disabled-with-reason, never a bare pill.

## 8. Proof discipline (per repo rules)

- [ ] Red test reproduces the EXACT failure BEFORE the fix; green on the real production caller AFTER.
- [ ] Verify on a FRESH build at the exact pushed SHA (stale binary hides drift).
- [ ] `make ci-local` GREEN on the exact SHA before `make land-main`.
- [ ] Real E2E per trigger (cap/N/default/leave) published to the CI report site, asserting raw DB + all read screens.
- [ ] Independent judge (this lens) re-verifies each sub-agent claim — no "already fixed" without failing-then-passing evidence.

---

Judge output each pass: per-section CONFIRMED/PLAUSIBLE/REJECTED + the exact
failing check + file:line + raw-DB/screenshot evidence. Any REJECTED/failed check
blocks the phase from landing.
