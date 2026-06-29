# Feed Direction Build-To-Done Goal

**Status:** Draft, owner-started goal charter for the next Feed Direction build
session
**Date:** 2026-06-30
**Companions:** [Feed Direction PRD](./PRD.md),
[Feed Direction TRD](./TRD.md),
[Dependency Closure PRD](./DEPENDENCY-CLOSURE-PRD.md),
[Dependency Closure TRD](./DEPENDENCY-CLOSURE-TRD.md),
[Counts/Shifting Closure PRD](./COUNTS-SHIFTING-CLOSURE-PRD.md), and
[Counts/Shifting Closure TRD](./COUNTS-SHIFTING-CLOSURE-TRD.md)

## 1. Goal

Build Feed Direction to real GoatOS closure, not a planning-only or mock-only
finish. The goal ends only when the blocking backend, frontend, integration,
seed, E2E, review, documentation, and GitHub push gates below are closed or
explicitly deferred by the owner with evidence.

Do not mark the goal complete because the docs are cleaner, the UI renders with
mock data, or a partial backend path exists. Closure means the process-integrity
chain works through Postgres, API, generated clients, workers, proof,
verification, read models, visual UI, tests, and pushed source.

## 2. Source Priority

Use this order whenever sources disagree:

1. `Feed, Shiftings and Count.docx` v1.1 is the primary Feed Direction business
   source. It owns Base Count, append-only Shifting ledger, one-day projection,
   physical count adoption, `09:00`/`13:30`/`15:00` timing, Diff semantics,
   bridge logging, as-fed quantities, and RationTable/solver constraints.
2. Other feed-relevant wiki/source documents and source findings refine that
   primary doc: Counting DB reconstruction, Shifting reports, Feed Director ops,
   Goats & Parks stage tags, transport consolidation, Warmup/K0/K1/Experiment
   evidence, and feed-stock/procurement boundaries.
3. Legacy Slack/App Script workflows are parity and cutover evidence for stage
   behavior, retries, proof, rejection, reset/re-send, dedupe, notifications, and
   security cleanup. They are never runtime authority.
4. GoatOS protocol/kernel docs and committed code define the implementation
   shape: protocol versions, obligations, batches, SOP proof, inventory,
   idempotency, audit, outbox, reminders, read models, and RBAC.
5. The admin-web mock controls UI/UX anatomy only. Older mock timing or old
   Feed panels do not override the primary Feed source.

## 3. Stop Rules

The build goal remains open until all of these are true:

- `G1`-`G17` and `CSG1`-`CSG10` have status, owner, evidence pointer, blocker
  reason, and implementation evidence where required.
- Counts/Shifting exposes the accepted projection contract or a fail-closed
  adapter that truthfully blocks Feed generation.
- Ration provenance enforces approved source metadata and publish capability.
- Generation, Diff, bridge, stage obligations, durable `stage_kind`, inventory
  unit binding, transport map, thresholds, reminders, notifications,
  missed/recovery, audit, observability, and cursor read models are implemented
  or explicitly owner-deferred.
- Backend APIs, OpenAPI/generated clients, admin-web/operator client contracts,
  seeds, local E2E, and frontend visual proof all agree.
- Feed Direction UI remains hidden until `G1` reopens, or the owner explicitly
  reopens it and the UI passes the mock-fidelity gates below.
- Review agents have re-reviewed source parity, backend correctness, frontend
  fidelity, security, E2E, and docs after fixes.
- `git diff --check`, relevant backend/frontend tests, E2E/seed proof, visual
  smoke, and final clean-tree checks pass.
- The final commit is pushed to `main` through the Mesha/VGoats
  `MESHA_GITHUB_PAT` path (`git mesha-push main`), and `HEAD == origin/main`.

## 4. Backend Build Gates

Implement in this order unless a new source review proves a safer order:

1. Counts/Shifting projection closure, including Base Count anchor,
   ShiftingEvent ledger, horizon split, fail-closed exceptions, idempotency,
   observability, and `GET /feed-direction/readiness` subgate roll-up.
2. Ration approval/provenance: solver/import output, source hashes,
   `review_status='approved'`, `approved_by`, `approved_at`, effective date, and
   `protocol.publish.feed_direction` capability checks.
3. Generation and Diff: immutable count snapshots, full run, affected-shed
   restatement, stale-obligation cancel/supersede, source-facing net correction,
   and bridge events.
4. Stage execution: separate stage obligations or equivalent typed stage records
   with durable indexed `stage_kind` before bucket APIs.
5. Inventory unit boundary: deterministic whole base units into the current app
   port, exact persistence to SQL `numeric + quantity_unit`, or app-port decimal
   widening before fractional feed use.
6. Transport map, checklist/list equivalent where needed, packing/wastage
   thresholds, reminder/escalation SLA, NotificationGateway routing,
   missed/recovery, audit, and observability.
7. Command/read models with stable cursor pagination and bounded filters for all
   hot Feed Direction lists.

## 5. Frontend And UI/UX Gates

Any frontend work that unblocks or exposes Feed Direction must follow the same
admin-web UI law as the current PHC/Vaccination work:

- `mock/goatos-dashboard-mock.html` is the only admin-web UI/UX source of truth.
- If the current mock does not contain enough Feed Direction detail, update the
  mock first or document the exact mock gap before implementing the screen.
- Port anatomy, not just colors: layout, density, typography scale, spacing,
  tables, drawers, filters, pagination, icon buttons, button sizing, text
  casing, empty/error states, hover/active/focus states, disabled states, tags,
  cards, and proof/status surfaces must match the mock/Mesha system.
- Frontend does not own product truth. Labels, columns, filters, options,
  disabled reasons, buckets, pagination semantics, and action availability come
  from backend contracts/OpenAPI/generated clients.
- Feed command work feeds top-level Control Tower, Action Center, Calendar,
  Protocol Adherence, Workflows, Config, and SOP Library through filters or
  domain/category context. Do not create nested Feed-owned command/authority
  routes.
- Before any frontend push or handoff, run
  `npm --prefix apps/admin-web run check:mock-fidelity`, capture rendered visual
  proof for relevant desktop and mobile widths, and compare the implemented
  Feed Direction surfaces against the mock anatomy.

## 6. Seeds And E2E Proof

The next build must include source-backed or clearly marked local-dev seeds for:

- tenant, park, sheds, direction-shed to transport-shed map, and Feed roles;
- Base Count anchors and ShiftingEvents covering realized and one-day projection
  horizons;
- approved Feed Direction protocol/ration rules with provenance and approval
  metadata;
- feed inventory items, stock, reservation, consume, and release paths;
- packing, transport, consumption/wastage, bridge, rejection, and rework cases;
- thresholds, notification channel policy, reminders, missed/recovery, audit,
  and read-model buckets.

E2E proof must exercise the real local chain:

```text
seeded Postgres truth
  -> API/generated client
  -> generation worker / local scheduler equivalent
  -> obligations and stage_kind records
  -> inventory reserve/consume/release
  -> SOP proof and verification
  -> reminders/missed/rework/notifications where applicable
  -> read models / command buckets
  -> admin-web or operator surface visual proof after G1 reopens
```

Do not count fixture-only UI rows, typecheck, lint, or mock-fidelity alone as E2E
proof.

## 7. Review Agents And Push Gate

Before pushing the final Feed Direction implementation to GitHub, run a
high-effort adversarial review pass. The owner shorthand for this is "5.5 extra
high"; use the strongest available reviewer-agent setup that matches that
intent.

Minimum review legs:

- primary source and wiki parity against `Feed, Shiftings and Count.docx`;
- legacy Slack/App Script parity and secret/cutover safety, without reproducing
  secrets;
- backend architecture, migrations, idempotency, inventory, kernel, API, and
  scale/query-plan review;
- frontend mock-fidelity, accessibility, responsive behavior, hover/focus,
  pagination, buttons, text, typography, and visual screenshot review;
- seed/E2E proof review against the real local Postgres/API/worker/UI chain;
- docs sync review so PRD/TRD/closure docs match the code that actually landed.

Fix all confirmed findings, rerun the relevant tests and visual checks, then
push through the verified Mesha/VGoats path:

```bash
git mesha-push main
```

Do not rely on an ambient `gh` account for GoatOS authority.
