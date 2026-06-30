# Feed Direction Build-To-Done Goal

**Status:** Draft, owner-started goal charter for the next Feed Direction build
session
**Date:** 2026-06-30
**G1 status update:** Reopened for Feed Direction build as of 2026-06-30. The
older pause tied to PHC/Vaccination UI and foundation review is satisfied by
the local Goal 1 vaccination/kernel closure documented in
`context/execution/operational-kernel-stability-closure-handoff.md`. Google dev
rollout, clean-slate seeding, and Google E2E remain a separate Goal 2 gate; they
must not be claimed as done, but they do not block starting Feed Direction.
**Companions:** [Feed Direction PRD](./PRD.md),
[Feed Direction TRD](./TRD.md),
[Dependency Closure PRD](./DEPENDENCY-CLOSURE-PRD.md),
[Dependency Closure TRD](./DEPENDENCY-CLOSURE-TRD.md),
[Counts/Shifting Closure PRD](./COUNTS-SHIFTING-CLOSURE-PRD.md), and
[Counts/Shifting Closure TRD](./COUNTS-SHIFTING-CLOSURE-TRD.md)

## 1. Goal

Build Feed Direction to real GoatOS closure, not a planning-only or mock-only
finish. The next implementation session must start by closing `G2`-`G17` in
order from the dependency PRD/TRD, recording status, owner, evidence, blocker
reason, and any explicit owner decision at each gate before using that gate as
implementation truth.

Do not mark the goal complete because the docs are cleaner, the UI renders with
mock data, or a partial backend path exists. Closure means the process-integrity
chain works through Postgres, API, generated clients, workers, proof,
verification, read models, visual UI, tests, and pushed source.

## 2. Source Priority

Use this order whenever sources disagree:

1. `Feed, Shiftings and Count.docx` v1.1 is the primary Feed Direction business
   source. It owns Base Count, append-only Shifting ledger, one-day projection,
   physical count adoption, `09:00`/`13:30`/`15:00` timing, Diff semantics,
   manual bridge SOP behavior, as-fed quantities, and RationTable/solver
   constraints.
2. Other feed-relevant wiki/source documents and source findings refine that
   primary doc: Counting DB reconstruction, Shifting reports, Feed Director ops,
   `context/source-findings/goats-and-parks-source-findings.md` for base
   goat/park/stage/shed-tag/feed-safety semantics, transport consolidation,
   Warmup/K0/K1/Experiment evidence, and feed-stock/procurement boundaries.
3. `Counting DB - values only.xlsx` and `Feed Directions Automation DB.xlsx`
   are workbook/tab/formula evidence for import mapping, parity fixtures, known
   validation gaps, ration tables, session templates, processed flags, and
   execution/proof stages. They are not runtime truth or target schema.
   Workbook dimensions such as breed, shed tag/stage, age, pregnancy/warm-up,
   energy/feed vectors, feed factors, and weight thresholds must become typed
   reviewed config families, not copied formulas.
4. Legacy Slack/App Script workflows are parity and cutover evidence for stage
   behavior, retries, proof, rejection, reset/re-send, dedupe, notifications, and
   security cleanup. They are never runtime authority.
5. GoatOS protocol/kernel docs and committed code define the implementation
   shape: protocol versions, obligations, batches, SOP proof, inventory,
   idempotency, audit, outbox, reminders, read models, and RBAC.
6. The admin-web mock controls UI/UX anatomy only. Older mock timing or old
   Feed panels do not override the primary Feed source.

Hard rule for this slice: if legacy Slack/App Script trigger times, older mocks,
prior GoatOS notes, or implementation inventories disagree with
`Feed, Shiftings and Count.docx`, follow the docx unless the Feed Director
explicitly reopens the business rule. Legacy trigger times are audit/cutover
evidence only; they are not GoatOS product schedules by default.

Hard base-source rule: `Goats and Parks.docx`, as committed in
`context/source-findings/goats-and-parks-source-findings.md`, is the base source
for goat identity, park/shed scope, shed tags, lifecycle/stage, pregnancy,
lactation, warm-up, fattening, milking, handling, weighing, feed roles, and feed
safety. The Feed build must use it before creating or changing templates,
resolver logic, config screens, stage obligations, read models, or UI copy that
touch those semantics.

Hard Sheds DB rule: `wiki/Sheds DB.xlsx`, as summarized in
`context/source-findings/sheds-db-source-findings.md`, is evidence for manual
ground-reality shed tags, capacity-like values, and potential tags. Do not copy
it as a runtime table. Build governed Location/Park profile CRUD with review,
effective dates, source evidence, and impact preview; birth, breeding,
procurement, health/ICU/quarantine, and ShiftingEvent workflows should update
or schedule profile/placement work instead of relying on manual sheet edits.

Hard workbook rule: old workbook tabs and formulas are evidence only. Do not
copy `Count-DB`, `CPT Validation`, `CBE Validation`, `Feed-Energy-Protein`,
`Supply Planning`, `Template`, `Feed Packing Form`, `Feed Transport Form`, or
`Feed Consumption & Wastage` as GoatOS runtime tables/modules. Build typed
imports, governed CRUD/review/publish config, immutable generation snapshots,
stage obligations, proof/rework, audit/outbox, and bounded Postgres read models.

## 3. Stop Rules

The build goal remains open until all of these are true:

- `G1`-`G17` and `CSG1`-`CSG10` have status, owner, evidence pointer, blocker
  reason, and implementation evidence where required.
- Counts/Shifting exposes the accepted projection contract or a fail-closed
  adapter that truthfully blocks Feed generation.
- Ration provenance enforces approved source metadata and publish capability.
- Shifted pregnant, lactating, warm-up, and other high-risk cohorts are covered
  as safety-critical cases: destination-shed feed must be recalculated before
  serving, underfeeding/shortage blocks with escalation, overfeeding/wastage
  triggers exception work, and moist/unsafe leftover feed cannot be treated as
  harmless surplus. This is herd-growth protection, not optional optimization.
- `G2`-`G17` are closed in order, or a gate has a narrow owner decision that
  defines a fail-closed adapter/disabled capability and the exact work that may
  continue. A broad "owner-deferred" note must not green-light hidden runtime
  scope.
- Generation, Diff, manual bridge logging, stage obligations, durable
  `stage_kind`, inventory unit binding, transport map, thresholds, reminders,
  notifications, missed/recovery, audit, observability, and cursor read models
  are implemented only after their gates are closed or bounded by that narrow
  fail-closed decision.
- Bridge implementation is limited to manual SOP proof/logging unless the owner
  reopens the superseded design. Do not build the rejected `07:30` next-morning
  system Diff for high-priority post-cutoff additions.
- If `G10` security closeout is deferred, the Slack/App Script bridge remains
  disabled. No Slack overlap, Slack-delivered execution, or Slack bridge reuse
  counts as done until security closeout proves API-only ingress.
- Backend APIs, OpenAPI/generated clients, admin-web/operator client contracts,
  seeds, local E2E, and frontend visual proof all agree.
- Feed Direction UI may be built under the reopened `G1`, but active exposure
  still requires backend-owned contracts, source-backed or clearly marked
  local-dev data, mock-fidelity, rendered visual proof, and no fake production
  claims. Google dev vaccination rollout is not a Feed Direction blocker and is
  not Feed Direction completion evidence.
- Review agents have re-reviewed source parity, backend correctness, frontend
  fidelity, security, E2E, and docs after fixes.
- `git diff --check`, relevant backend/frontend tests, E2E/seed proof, visual
  smoke, and final clean-tree checks pass.
- The final commit is pushed to `main` through the Mesha/VGoats
  `MESHA_GITHUB_PAT` path (`git mesha-push main`), and `HEAD == origin/main`.

## 4. Backend Build Gates

Start by closing `G2`-`G17` in the dependency order below unless a new source
review proves a safer order. Do not wait for every later gate to be green before
starting the first gate, and do not skip ahead using broad owner-deferral
language.

1. `G2` Counts/Shifting projection closure, including Base Count anchor,
   ShiftingEvent ledger, horizon split, fail-closed exceptions with bounded
   list/read queue, transactional outbox fanout, reviewed resolve/dismiss audit,
   idempotency, reviewed typed source import through
   `backend/cmd/counts-source-import` for Base Count/Shifting JSONL rows with
   durable `count_source_import_runs` evidence,
   durable `count_projection_recompute_runs` evidence for every bounded
   recompute path,
   stale/imported mismatch scanning through the bounded `counts-mismatch-scan`
   worker path with durable scan-run evidence, dedicated mismatch-scan anchor
   indexes, `counts-alias-coverage-check` proof for source/workbook-required
   Counting DB, Feed Automation workbook, and Sheds DB aliases,
   `counts-workbook-mapping-check` proof for the sanitized workbook column map,
   `counts-workbook-source-scan` proof that the real local XLSX files still
   expose the mapped sheets and headers without importing private rows,
   `counts-source-parity-check` proof for sanitized
   workbook/source parity fixtures that compare shed, breed, stage/tag,
   age class, sex, headcount, pregnant/lactating/warm-up counts, and
   ration-context resolution state, `counts-query-plan-check` index-path proof
   for Counts hot reads with migrated synthetic movement and projection-row
   fixtures, observability, aggregate breed-level count output,
   reviewed ration-context resolution state for each shed + breed projection row, and
   `GET /feed-direction/readiness` subgate roll-up.
   The Feed readiness path now has seeded local integration proof that real
   Counts Base Count + ShiftingEvent writes emit the ShiftingEvent outbox event,
   the in-process projection handler recomputes both horizons, and Feed
   readiness consumes that evidence under `G2` while keeping generation blocked
   when a pregnant destination shed has an open `destination_shortage`
   exception. This is readiness/outbox consumption proof only; full Feed
   generation consumption of immutable projection snapshots still remains.
   `counts-source-parity-check` now also has migrated-Postgres proof against a
   canonical projection snapshot/read path for the sanitized
   `source-parity-high-risk-sample.json` fixture, including pregnant late
   gestation, lactating/mother, pregnant warm-up, fattening male warm-up, and
   normal non-pregnant rows with pregnant/lactating/warm-up counts, and writes
   `CSG10` source-parity evidence as pending, not ready.
   `counts-source-import` now has migrated-Postgres proof that replaying the
   same typed Base Count + pregnant ShiftingEvent JSONL batch records replay
   evidence, does not duplicate canonical rows/impacts, and keeps `CSG8`
   pending, not ready.
   `counts-projection-recompute` now has migrated-Postgres proof that rerunning
   the same pregnant shifted-cohort projection creates a second recompute audit
   run while reusing the same immutable projection snapshot, does not duplicate
   projection rows or exceptions, keeps the destination `destination_shortage`
   blocker visible, and keeps `CSG8` pending, not ready.
   `counts-query-plan-check` now has migrated-Postgres proof that Feed-target
   ShiftingEvent movement reads use park/date-bounded source and destination
   indexes, projection rows carry the snapshot park/date filters needed for the
   hot read index, and `CSG10` query-plan evidence stays pending, not ready.
   `counts-workbook-mapping-check` now validates a sanitized 117-column mapping
   across 16 source-role groups for Counting DB, Feed Automation workbook, and
   Sheds DB evidence, and writes `CSG10` mapping evidence as pending, not ready.
   `counts-workbook-source-scan` now validates the actual local Counting DB,
   Feed Directions Automation DB, and Sheds DB XLSX files for 117 required
   headers across 14 sheets / 3 workbooks, including the Template sheet's
   `CBE`/`CPT` section-header shape, and writes `CSG10` source-scan evidence as
   pending, not ready.
   The Counts projection read model now also exposes page-bounded
   `ShedBreedTotals` for the returned projection rows so Feed can inspect
   aggregate shed + breed counts without losing stage/age/sex and
   pregnant/lactating/warm-up detail rows; full no-cursor parity and seeded E2E
   still remain before `G2` is green.
   Full row parsing/import, full parity fixture breadth, and owner-approved
   mapping review remain source-review work; workbook formulas or examples must
   not bypass typed validation/import.
   Typed import run evidence, projection recompute run/replay evidence, alias
   coverage evidence, including the 121-alias sanitized
   `source-workbook-required-aliases.json` manifest, and query-plan evidence may
   move subgates from blocked to pending, but they cannot turn `G2` green
   without full source workbook parity,
   full observability, owner-approved alias review, and seeded local E2E.
   Breed/tag constraint tables without shed placement must block until reviewed
   context resolves the nutrition cohort. RFID-to-shed per-animal association
   is out of scope for the initial Feed Direction build.
2. `G3` clock and legacy trigger inventory sign-off: confirm the docx-owned
   default clocks first (`09:00`, `13:30`, `13:30-13:45`, `15:00`, and
   next-day default serving slots `09:00`/`15:00`). `G5` owns any approved
   session-slot changes beyond that default. Legacy installed trigger functions,
   retry/archive/watchdog behavior, and proof/stock checks are audit-only
   cutover evidence requiring retain/retire/replace decisions; they do not
   become GoatOS schedules without explicit Feed Director approval against the
   docx.
3. `G4` Ration approval/provenance: solver/import output, source hashes,
   `review_status='approved'`, `approved_by`, `approved_at`, effective date, and
   `protocol.publish.feed_direction` capability checks. Initial solver scope is
   feed-type-level constraints only: hard floor/ceiling, structural ratio,
   category floor, and quantity floor. Item-level feed ceilings and palatability
   modeling are deferred. The `60:40` structural ratio applies only to Milking
   and Fattening tags, roughage floor numeric values are examples until confirmed
   per tag, cost minimization means no paired overshoot ceiling is needed, and a
   refreshed solve replaces the lookup table wholesale with no versioned blend.
   Feed Transfer KT-style uploaded breed/tag/energy sheets are ration/constraint
   source evidence only; they must map into reviewed nutrition cohort keys and do
   not prove shed placement. KT examples such as feed vectors/energy capacity,
   `80/20`, grain vs dry/green leaf groups, `400-500g`, `600g`, `F1`
   `11-15kg`, and `F2` `15-20kg` must be approved, rejected, or draft-only
   before generation can depend on them. They are variable parameter rows by
   breed, shed tag/stage, age/stage alias, kid weight band/ADG,
   pregnancy/lactation/warm-up policy, feed vector family, farm/source context,
   and effective version; they are never global constants. `G4` must also close
   the typed importer/admin CRUD path for source tables, feed vectors, costs,
   constraints, aliases, eligibility, dimension keys, ratio policy,
   quantity/weight thresholds, validation checks, calculation outputs, source
   hashes, row validation, calculation preview, dry-run parity preview,
   repair/DLQ, review, approval, publish, and audit.
4. `G5` Eligibility and stage-tag/session policy: Warmup, K0/K1, Experiment, ICU,
   Quarantine, Flushing, Breeding, pregnancy, F2/Fattening, breed aliases, and
   versioned session-slot/feed-set policy are approved. The docx default is two
   slots with 50/50 split, but admins may add, disable, reorder, or reweight
   slots only through approved effective-dated Feed Direction protocol config
   with validation that active slot weights cover the full daily as-fed quantity
   and explicit supersession behavior for already-generated dates. KT pregnant
   windows such as `12:30-15:00`/`14:00-15:00` are session-policy candidates
   only; they do not override the docx clocks unless approved. A realized or
   future-effective shifting into a destination shed must re-resolve pregnancy,
   lactation, warm-up, age/stage, breed alias, and shed tag before generation or
   Diff. Missing resolver context, destination-shed shortage, or stale ration
   context blocks Feed generation for the affected rows instead of applying a
   normal shed average.
5. `G6` Quantity and precision boundary: deterministic whole base units into the
   current inventory app port, exact persistence to SQL `numeric + quantity_unit`,
   or app-port decimal widening before fractional feed use.
   FeedDirection, Diff, packing, and field instructions carry as-fed gross
   quantities only. `wastage_factor` and `DM_factor` are internal
   nutrient-accounting inputs and must never surface to the field/packing team.
6. `G7` Generation, Diff, and stage model: immutable count snapshots, full run,
   affected-shed restatement, stale-obligation cancel/supersede, source-facing
   net correction, and separate stage obligations or equivalent typed stage
   records with durable indexed `stage_kind` before bucket APIs.
7. `G8` Transport map and checklist/list equivalent where legacy overlap
   preserves that work shape.
8. `G9` Packing/wastage thresholds and typed rework/re-issue policy, including
   explicit approval/rejection of KT-style `90-95%` shed/pack/breed/tag/energy
   matching and warm-up allowance. `G9` must also define feed-safety exception
   thresholds for overpacking, leftover/moist feed, refusal to eat, sickness
   risk, and destination-shed shortage. The system must choose visible rework,
   remove/replace, or supervisor approval; it must not hide waste as nutrition
   buffer.
9. `G10` Slack security closeout; if bounded instead of closed, Slack bridge is
   disabled and cannot count as done.
10. `G11` Reminder/escalation SLA.
11. `G12` NotificationGateway routing.
12. `G13` Missed/recovery events.
13. `G14` Business audit and observability.
14. `G15` Command-lens field mapping.
15. `G16` Query-plan coverage.
16. `G17` Cursor read models with stable cursor pagination and bounded filters
    for all hot Feed Direction lists.

Generation and Diff implementation must include immutable count snapshots,
affected-shed restatement, stale-obligation cancel/supersede, and source-facing
net correction. Bridge work is manual 2x-ration SOP proof plus logging of the
minimum business fields (`shed_id`, `animal_id` or approved aggregate reference,
`timestamp`, `quantity`) plus proof reference, source event/logical shifting
reference where known, and reconciliation state; no system-generated bridge
Diff.

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
- Counts projection exception work already uses the shared process-integrity
  reader as `category=feed_direction` for top-level Action Center/Workflows;
  use that path as evidence, then finish assignment policy, Feed-specific
  visual UX, and the remaining command-lens mappings instead of creating a
  second Feed-owned command room.
- Before any frontend push or handoff, run
  `npm --prefix apps/admin-web run check:mock-fidelity`, capture rendered visual
  proof for relevant desktop and mobile widths, and compare the implemented
  Feed Direction surfaces against the mock anatomy.

## 6. Seeds And E2E Proof

The next build must include source-backed or clearly marked local-dev seeds for:

- tenant, park, sheds, direction-shed to transport-shed map, and Feed roles;
- Base Count anchors and ShiftingEvents covering realized and one-day projection
  horizons at aggregate shed + breed grain;
- approved Feed Direction protocol/ration rules with provenance and approval
  metadata;
- feed inventory items, stock, reservation, consume, and release paths;
- packing, transport, consumption/wastage, manual bridge logging, rejection, and
  rework cases;
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
  -> manual bridge logging without a generated 07:30 Diff
  -> reminders/missed/rework/notifications where applicable
  -> read models / command buckets
  -> admin-web or operator surface visual proof under reopened G1
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

## 8. Wiki Section 13 Do-Not-Resolve Rules

The primary wiki source has explicit open items. Do not silently resolve these in
code, docs, seeds, or UI:

- Bridge remains a current manual SOP: high-priority post-cutoff additions get a
  manual 2x ration at the destination shed with video proof and no source-shed
  claw-back. Initial GoatOS work logs the top-up only. Do not build the
  superseded `07:30` next-morning system Diff for this case.
- Counts are aggregate breed-level by shed. RFID-to-shed per-animal association
  is planned but not implemented, and must not be introduced as hidden initial
  Feed scope.
- Ration lookup needs a reviewed resolver from shed + breed count rows to
  nutrition cohort keys. Uploaded breed/tag/energy constraint sheets do not carry
  authoritative shed placement; missing resolver context is a blocker, not a
  default-ration fallback.
- Field-facing FeedDirection, Diff, and packing outputs are as-fed gross
  quantities only. `wastage_factor` and `DM_factor` remain internal
  nutrient-accounting fields.
- The 50/50 two-session split is the docx default, not a code limit. Model
  sessions as versioned admin config so a Feed Director can draft, and COO/CEO
  can publish, additional slots or different split weights with source evidence,
  validation, and effective dates.
- Feed parameters vary by reviewed cohort dimensions such as breed, shed
  tag/stage, kid weight band/ADG, energy/feed vector policy, pregnancy, warm-up,
  and explicit exclusions. KT numbers (`80/20`, `400-500g`, `600g`, F1/F2
  weight ranges, pregnant windows, `90-95%` matching) are source candidates only
  until typed template rows pass validation, calculation preview, repair/DLQ for
  wrong data, and reviewed `feed_direction_config_pack` publish or rejection.
- Base Count cadence moved from roughly weekly to roughly monthly and may change
  again. Store cadence as reviewed policy or ops schedule; do not hardcode it.
- Initial RationTable solver scope excludes item-level feed ceilings and
  palatability modeling. Confirm roughage-floor numbers per tag before
  hardcoding, apply `60:40` only to Milking/Fattening tags, rely on cost
  minimization rather than a paired overshoot ceiling, and replace the solve
  output wholesale on cost/feed-set changes.
