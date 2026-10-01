# Procurement journey

The end-to-end purchase of animals: a CXO's request → vendor sourcing → stock verification at the
vendor → selection and health decision → load approval → warm-up at source → transport
preparation → loading → transit → arrival and park warm-up → payments. Status: **proposed
2026-10-01, not built**; the decision and its open conflicts are in
`docs/decisions/procurement-journey.md`.

**Engine:** the journey runs on Temporal with a saga, scoped to the new `procurement_journey`
module only; the task kernel keeps every human-visible clock, lateness and escalation. See
`docs/decisions/procurement-journey-orchestration-engine.md` and the
[animated explainer of the engine](engine-explainer.html).

Reading order:

1. `../../decisions/procurement-journey.md` — what was asked, the nine decisions, the conflicts
   that need the maintainer before stage 2 is built.
2. `journey-map.md` — the ten stages, every seeded step with owner, proof and schedule, and what
   gates the next stage.
3. `configuration.md` — the four homes of configuration and the complete list of what the farm
   can change; what is deliberately fixed.
4. `data-model.md` — tables, events, engine hooks, routes, permissions, scale shape.
5. `views.md` — every admin-web page and phone screen, state by state, and the pushes.
6. `engine-gaps.md` — the nine capabilities the journey needs (chaining, day/anchor schedules,
   step-bounded series and lateness are closed by the `procurement_journey` Temporal workflow;
   owner lists and picked people, completion guards, per-step review, Work Board source and studio
   editing stay build work in tasks/SOP).
7. `delivery-plan.md` — ten slices in order, each with tests, guards and proof.

What this builds on, unchanged: the SOP studio (`docs/decisions/sop-studio.md`), the sale as a
workflow (`docs/decisions/sales-sop.md`), the SOP-driven procurement documents
(`docs/decisions/procurement-sop-driven.md`), the animal purchase inspection
(`docs/decisions/animal-purchases.md`, `procurement-sop.md`), the vendor register
(`vendor-register-two-sides.md`), landed cost (`load-landed-cost-and-growth.md`), designation
audiences (`notification-designation-audiences.md`) and the task kernel lock.
