# Business-Rule Fidelity — Vaccination, Obligations, Domains

Wrong medical/business rules are worse than wrong code — they ship silent harm.
The rule docs and committed migrations/config are authoritative; the summaries
here are a review aid, not a replacement. **Always read the cited source (doc,
migration, or seeded config) — or query the Graphify docs graph — before judging
domain logic.** Every value or list quoted below is illustrative and may have
drifted; verify it against the named source at review time. Honor the
maintainer-rule lock in `AGENTS.md`: if a change encodes a new/contradicting
rule, surface the conflict and require an explicit decision — do not silently
accept it.

Authoritative sources (verify against these — do not trust the prose here):
- Vaccination rules: `docs/preventive-care-vaccination/vaccination-rules.md` (+ `PRD.md`, `TRD.md`, `APPROVED-SCHEDULE-MATRIX.md`)
- Drive-planner allowed combos / thresholds (seeded): `backend/internal/obligation/app/drive_planner_config.go`
- Obligation engine + state machine: `docs/protocol-engine/obligation-engine.md`, `docs/protocol-engine/state-machines.md`
- Obligation status CHECK (durable truth): the LATEST migration that alters `obligation_instances_status_check` under `backend/migrations/postgres/`
- Calendar: `docs/decisions/calendar-ownership.md`
- Legacy replacement parity/import replay: `context/architecture/operational-kernel-system-design.md`, feature PRD/TRD, source findings
- Feed direction: `docs/feed-direction/PRD.md`, `TRD.md`, `DEPENDENCY-CLOSURE-TRD.md`
- Forms/SOP: `context/forms/final-forms-sop-engine.md`
- Org / species / shed base model: `context/source-findings/goats-and-parks-source-findings.md`

## Vaccination rules (verify against the doc + seeded config — never from memory)

Code: `backend/internal/vaccination/`, `backend/internal/protocol/`,
`backend/internal/obligation/`.

**The numeric gaps, same-day combo set, shot caps, and hold windows below are
illustrative only.** A reviewer must not treat this prose as exhaustive: confirm
the full allowed same-day combo set and every numeric threshold against
`docs/preventive-care-vaccination/vaccination-rules.md` AND the seeded
`backend/internal/obligation/app/drive_planner_config.go` before flagging OR
approving a combo/threshold. If the code and either source disagree, that is a
finding.

Rule shapes a reviewer checks (illustrative values — verify against source):
- **Dose spacing:** live→live, live→killed, killed→killed, and booster gaps each
  have a minimum-gap rule (the doc/config own the actual week counts). Confirm
  the code reads the gap from config, not a hardcoded literal that can silently
  diverge from the doc. "Two live same day" is NOT a shortcut for the live→live
  gap — verify the gap is still enforced.
- **Per-animal shot cap per drive/visit + explicit allowed same-drive combos.**
  The combo list here is non-exhaustive; the allowed set is whatever the active
  doc + `drive_planner_config.go` define. Never approve/reject a combo from this
  file's examples alone.
- **Warm-up / entry hold:** a hold measured from farm-entry date (not the source
  dose date). Verify the anchor date the code uses matches the rule doc.
- **Species selection is real (verify against config, not prose):** obligations
  select by species/age/stage. Species-specific vaccines must be species-locked
  in the seeded eligibility config — a goat-only vaccine must never target sheep
  and a sheep-only vaccine must never target goats. Check the species tag on each
  preset in `drive_planner_config.go` / the eligibility selector, not the example
  mapping here (which may drift). Mixed-species: kids may share a drive when the
  config says it is safe; adults run species-specific execution groups in the
  same park visit.
- **Defers are safety blocks, not planner skips:** ICU, quarantine, sick/under
  treatment, pregnancy window, post-breeding hold. On recovery, reopen the missed
  obligation from the recovery date and rejoin the nearest compatible same-park
  drive within the batching window — else create a micro-drive. No recovered
  animal is left waiting past the window. (Verify window values against the doc;
  review this flow via `references/kernel-and-scale.md` → "Defer-recovery
  re-entry".)
- **Clinical defer set is MANDATORY, not an authored subset (C35-010):** the four
  clinical states `sick`, `under_treatment`, `quarantine`, `icu` are non-optional
  safety blocks. A published rule's `eligibility.defer_states` may only ADD states,
  never drop one of these. REJECT any review where:
  - publish/validation accepts a present, non-empty `defer_states` that omits a
    mandatory clinical state (an empty list is fine — it maps to the safe default),
    or silently rewrites an out-of-range value instead of failing;
  - the generator treats a partial authored list as authoritative, so a
    sick/under-treatment animal falls out of eligibility and its open work is
    **cancelled/left scheduled instead of deferred** (wrong medical action = P0);
  - the mandatory set is re-hardcoded in a new place instead of routing through the
    single source of truth `protocol/domain.MandatoryClinicalDeferStates`
    (`EffectiveClinicalDeferStates` / `MissingMandatoryClinicalDeferStates`); the
    existing SQL siblings (`vaccination_eligibility_rollups` usable flag,
    `ListRecoverableDeferredVaccinationGoatIDs`) must stay in sync with it.
  Mechanical backstop: `make clinical-defer-guard`
  (`tools/agent-hooks/check-clinical-defer-states.mjs`, required in CI). Source:
  `docs/preventive-care-vaccination/vaccination-rules.md`.
- **Drive batching:** may hold a due shed/tag group up to the batching window to
  combine with a compatible same-park group — ONE-TIME per obligation/dose cycle,
  no rolling postponement.
- **Missed-dose handling is cycle-relative** (thresholds illustrative — verify):
  the decision to vaccinate immediately vs wait for the same-cycle drive depends
  on a distance threshold defined in the rule doc/config. Trusted prior evidence
  (our supervised parks / procurement holding parks only) suppresses scheduled
  work; third-party/vendor claims do NOT.

### Timezone / day-boundary correctness (verify the calendar decision)

Medical schedules mistime when a due/missed day is derived from UTC instead of
the animal/location calendar. Goat OS currently operates on the IST business
calendar by default (`Asia/Kolkata` via `locations.timezone`; verify the current
default in migrations/config). Reviewer checks:
- [ ] Due-date, missed-marking, recovery-re-entry, and drive-planned-date day
      boundaries resolve against the location timezone / IST business day, not a
      raw `time.Now()` UTC date. Sweepers that compute `.UTC().Date()` for a
      medical day boundary are a finding.
- [ ] A dose due "on day N" in `Asia/Kolkata` is not marked missed/early by a
      worker running in UTC crossing midnight differently.
- [ ] Audit/event timestamps stored as `timestamptz` are converted to
      `Asia/Kolkata` before any operator-facing display, business-date filter,
      reminder key, due bucket, calendar grouping, or report dimension is
      computed. UTC is a physical instant representation, never the Goat OS
      business calendar.

### Forbidden — never accept:
- [ ] **Mother-vaccination status as a scheduling input** — explicitly rejected
      everywhere (config, seed, import, model, condition). Every kid uses the
      approved standard schedule; mothers are kept vaccinated operationally.
- [ ] Third-party/vendor vaccine claims used to suppress scheduled work
- [ ] Rolling/repeated postponement in drive batching (one-time hold only)
- [ ] **Species-lock leak** — a species-locked vaccine reaching the wrong species
      (goat-only → sheep, sheep-only → goat). Verify the lock lives in the
      eligibility config, not a comment.
- [ ] Goat-only `CHECK` constraints that block mixed-species — species is an
      animal fact, not a DDL check
- [ ] Free-text SOP label instead of a bound published `sop_version_id` on an executable rule

### Always require:
- [ ] Idempotency key on generation / completion / deferred-state writes
- [ ] Obligation-level audit via the status-events ledger
- [ ] Proof/verification before the next-stage (booster) obligation is generated
- [ ] Mixed-species validation at animal create, stage assign, rule publish, generation
- [ ] Postgres as canonical truth (not Cloud Tasks, Pub/Sub, or frontend state)

## Obligation engine (source: `docs/protocol-engine/obligation-engine.md` + latest migration)

- **Status vocabulary — verify against the migration, do not trust this list.**
  The durable allowed set for `obligation_instances.status` lives in the LATEST
  migration under `backend/migrations/postgres/` that alters
  `obligation_instances_status_check`. Read that CHECK constraint at review time.
  As an illustrative snapshot (may drift): `scheduled` (default), `due`,
  `in_progress`, `deferred`, `completed`, `missed`, `waived`, `canceled`,
  `superseded`. **`pending` and `assigned` are NOT durable statuses** — they do
  not appear in any status CHECK constraint or state transition; `pending`
  appears only as a computed read-model view label (e.g. `proof_pending`) in the
  HTTP work-state mapping, never as a stored value. British `cancelled` is also
  not a durable status. If code writes a status not present in the current CHECK
  constraint, that is a finding (the DB will reject it or a stale enum is drifting
  from the migration).
- **Transitions are a state machine, not a linear chain (verify against
  `state-machines.md`).** Confirm each transition the code performs is legal and
  that terminal states stay immutable:
  - Terminal states (`completed`, `missed`, `waived`, `canceled`, `superseded`)
    are immutable history — no automatic forward progression out of them; only
    explicit correction/rework paths (with status events) may reopen or supersede
    the same logical work.
  - `scheduled`/`due` rows can defer; `due`/`in_progress` rows can miss on window
    close; `deferred` rows recover to `scheduled` (from ICU/quarantine/sick exit)
    or cancel on animal exit; exited animals cancel open/deferred/missed work.
  - A change that adds a new transition edge or a new status must cite the doc +
    migration change together — code and CHECK constraint must move as one.
- **Date semantics:** see the timezone section above — due dates, missed marking,
  recovery re-entry, and drive planned dates are location-calendar decisions, not
  UTC-day accidents.
- **Rule DSL stores policy, not herd facts (verify on authoring/generation
  changes):** `protocol_versions.rule_dsl` may contain matrix dimensions,
  selectors, dose/schedule rows, compatibility/gap rules, defer/blocked rules,
  proof/SOP binding, and catch-up policy. It must NOT embed herd-animal required
  field checklists, current animal snapshots, copied shed rows, vaccination
  history lists, or per-animal JSON payloads. Current animal/location/procurement/
  completion facts come from canonical tables or indexed target-facts read
  models (verify the current schema name before citing it). If SQL-selectable
  predicates are needed, published cells compile to derived dimension rows in the
  `protocol_rule_dimensions` table; those rows are derived from `rule_dsl`, not
  the authoring source.
  Source: `docs/preventive-care-vaccination/PRD.md` "Rule JSON is policy, herd
  facts are database facts" and `TRD.md` "Rule JSON vs. herd facts boundary".
- **Generation** is event-driven (birth / procurement / stage-change / prior-dose
  completion → booster). Deterministic idempotency key + duplicate-spawn guard.
- **Trigger anchor + null-DOB trap (verify):** `birth_age`-anchored rules require
  DOB truth and **silently skip animals with null/estimated-missing DOB** — those
  animals fall into no-work/no-gap states. A `post_arrival` (or equivalent
  arrival-anchored) trigger does not need DOB. Reviewer checks:
  - [ ] `birth_age` rules fail visibly or defer-with-reason on missing DOB — never
        silently produce zero obligations for null-DOB animals.
  - [ ] Generation for a cohort that includes null-DOB animals is reconciled
        (counted / flagged), not silently short.
- **Re-scope (SM-2):** an animal-state change (pregnancy enter/exit, stage change)
  re-evaluates eligibility — may open new dues or suppress current ones.
- **Animal exit / cull cascade (dead / sold / culled / transferred / lost):** a
  single exit must fan out completely — verify all of:
  - [ ] Open + deferred + due obligations for that animal canceled (or repaired)
        with status/audit events, so the animal never remains overdue.
  - [ ] In-flight batch membership reconciled; stock reservations released and
        reconciled back to the inventory ledger.
  - [ ] Calendar events / projected rows for that animal dropped or closed so it
        stops surfacing in action/adherence views.
  - [ ] Projections refreshed. A cull that cancels obligations but leaks a
        reservation, a calendar event, or an overdue projection row is a finding.
- **In-flight version/override changes (park-override mid-cycle cutover):**
  activation must **preview** open obligations and in-progress batches before it
  cuts over. Verify the finish-vs-cancel/reissue choice is explicit:
  - [ ] Only **open future** obligations are superseded/reissued to the new
        version; the reviewer confirms the code makes an explicit
        finish-vs-cancel decision for **in-progress** batches rather than silently
        wiping them.
  - [ ] Closed/completed history stays on the ORIGINAL version — a cutover must
        not rewrite audit history to the new rule.
  - [ ] Every supersede/cancel/reissue emits status events.
- **Publish authority is enforced SERVER-SIDE, not asserted (verify):**
  vaccination config publish is restricted (e.g. CEO/COO/superadmin via
  `protocol.publish.<category>`) — confirm the RBAC check runs in the backend
  route table (`backend/internal/permissions/routes.go`) and fails closed, not
  merely hidden in the UI. Additional checks:
  - [ ] A park-scoped actor cannot publish a tenant-default rule.
  - [ ] A scope cannot be **widened** (park override must not silently become a
        tenant-wide rule).
  - [ ] Rule versions are immutable; resolution is scope policy
        (tenant-default-with-park-overrides) + effective dates with non-overlap
        enforced per scope. Impact preview required before activation.
  - [ ] **Every publishable category has an explicit capability seed — there is no
        `protocol.publish.*` wildcard.** Adding a new `protocol_definitions.category`
        must ship a migration granting `protocol.publish.<category>` to the
        publishing roles; without it the category is silently un-publishable
        (`docs/protocol-engine/obligation-engine.md`). Flag a new category added
        without its publish-capability seed.

## Legacy replacement parity and import/replay

When a change replaces a Slack, Sheets, App Script, source-doc, or legacy
dashboard workflow, reviewers must verify the policy pack or feature doc declares
the legacy capability parity floor, known legacy gaps to close, and import/replay
mapping. Parity means preserving useful source signals while adding GoatOS
validation and evidence; it is not bug-for-bug row copying and not fabricated
business truth.

Review checkpoints:
- [ ] The PRD/TRD or policy-pack contract names the legacy capability parity
      floor and any known legacy gaps intentionally closed by GoatOS validation.
- [ ] Import/replay maps source evidence to canonical GoatOS records with
      `source_ref`/confidence/proof semantics where applicable; untrusted or
      weak source rows create review/catch-up work, not completed facts.
- [ ] The migration/cutover path can replay or reconcile imported source records
      idempotently without duplicate obligations, fabricated completions, or
      loss of useful proof/review signals.

## No fabricated business truth (always on — code, seeds, imports, migrations, generated data)

This is not limited to legacy replacement above. ANY change that supplies a
business fact without a real source is a finding. Ownership, assignments,
mappings, counts, statuses, owners, and completions must trace to a reviewed
source; a missing source becomes a reviewed mapping, a clearly-labeled
provisional fixture a preflight can reject, or an explicit blocker — never a
silent invention.

Review checkpoints:
- [ ] **No runtime/apply path invents data.** Round-robin, even-spread, modulo
      distribution, "pick the first/any plausible owner", or hardcoded default
      owners/managers/assignees in service, worker, importer, migration, or
      request-handling code is a finding. Such strategies are allowed ONLY in a
      one-time data-fill tool that writes a reviewable artifact and touches no
      live tables.
- [ ] **Provisional data is labeled and rejectable.** Seed/fixture rows that are
      not reviewed carry provenance (`assignment_source`/`source_ref`/`confidence`/
      `needs_review`) and a `--strict`/preflight mode fails non-zero on gaps AND on
      unreviewed-provisional rows. "reviewed" must require an explicit signal set,
      not merely "not tagged provisional".
- [ ] **Missing mapping surfaces as a gap, never auto-filled.** A shed/goat/task/
      record with no explicit owner reads as an honest gap in the UI and blocks a
      production preflight, rather than being back-filled by code.
- [ ] Reference pattern to match: `backend/cmd/seed-shed-positions`
      (`-generate-provisional` → artifact; `-mapping` → apply-only; `-strict` → block).

## Vaccination execution edge cases

For vaccination, reviewers must check the complete execution loop, not just
generation:

- [ ] **Vaccine-matrix acceptance fields + cold-chain/proof parity** — the proof
      policy / SOP form requires the source-normalized fields where applicable:
      animal scan, vaccine/item, medicine batch or vial/lot, dose, administered
      date/time, cold-chain/quantity checks, proof media, adverse-reaction
      notes/follow-up, and verifier/park-head review. A matrix cell accepted
      without its required acceptance fields (or without cold-chain evidence where
      the rule demands it) is a finding.
- [ ] FEFO stock is reserved/consumed/released through the generic inventory
      ledger; missing stock, stock shortfall, expired lot, quarantined lot, and
      cold-chain failure create visible block/rework/repair state
- [ ] Trusted vaccination history is accepted only from our supervised parks or
      procurement holding parks with SOP/proof/validator evidence; vendor or
      third-party claims remain untrusted and must not suppress due work
- [ ] Reliable imported history reconciles into `vaccination_completions` and
      schedules boosters from actual `administered_at`; missing/untrusted older
      history creates one safe catch-up/review action, not fabricated completions
      and not every old dose as same-day work
- [ ] Scan-day reconciliation handles missing animals, shifted-in extras,
      shifted-out animals, newly sick/pregnant/quarantined animals, deaths/sales,
      unreadable tags, proof rejection, stock shortfall, and cold-chain failure
      as explicit cancel/defer/rework/replan actions
- [ ] Same-day drive compatibility enforces species grouping, live/killed gaps,
      same-vaccine min gaps, per-animal shot cap, proof/worker/verifier gates,
      and one-time batching hold without rolling postponement (all read from the
      seeded config, not hardcoded)
- [ ] Completion proof acceptance, rejection/rework, duplicate submit, and
      replay each have tests or live proof evidence when that path changed

## Other domains — one-line rule + doc

- **Calendar** (`docs/decisions/calendar-ownership.md`): admits an event only if it
  has a due window, an owning executable role, and requires human action. Owner
  pills: pc / feed / breeding / parks / procurement / inventory. Backend owns the
  projection; not Cloud Tasks, not frontend. For vaccination drive planning, when
  due rows attach to an `obligation_batches` drive, Calendar projects the drive
  row as active operations work and suppresses the batched per-animal
  `vaccination_dose_due` / `dose_due` rows from active Calendar; per-animal rows
  stay visible in Passport, Protocol Adherence, vaccination detail, and audit
  surfaces.
- **Feed direction** (`docs/feed-direction/*`): "how much of which feed each shed
  gets, each session, each day," recomputed on count/shifting changes; reuses the
  generic obligation + inventory engine — no parallel `feed_*` execution tables.
- **Forms/SOP** (`context/forms/final-forms-sop-engine.md`): Goat OS-owned Form DSL
  + native Android runner + Go backend validation; typed field/rule set; execution
  path validates + idempotency + proof + audit before projections.
- **Org / shed / species** (`goats-and-parks-source-findings.md`): `locations`
  self-FK tree (farm/park/shed/cohort) + profiles — no separate `parks`/`sheds`
  tables. Animal identity requires species + canonical sex (unknown/blank is
  BLOCKING) + two life-unique IDs. Stages come from `animal_stage_lookup`, not
  hardcode. Any feature touching herd-animal identity/species/park/shed/stage must
  start from this source and not invent conflicting semantics.
- **Inventory**: generic `inventory_items` / `inventory_stock` / `_movements`
  (FEFO), reserve at batch level, consume on SOP completion, release on
  cancellation — no `vaccine_stock` table.

## Scope state — verify current state before flagging "missing" or "scope creep"

Do not flag intentionally-deferred work as a bug, and do not approve scope creep
into parked areas. **This is a snapshot that drifts — verify current state before
relying on it:** check `backend/migrations/postgres/`, the `backend/cmd/`
directory (which worker binaries actually exist), the `Makefile`, and the docs.
Do not treat any BUILT/DEFERRED/PARKED label below as gospel.

- **BUILT (verify via migrations + `backend/cmd/`):** obligation engine + state
  machine, vaccination rules schema/domain, protocol config + authority,
  locations tree + profiles, mixed-species catalog, generic inventory/FEFO, SOP
  definitions/submissions + proof/verification. The vaccination **calendar
  workers SHIP** — the calendar projector, reminder sweeper, and escalation
  sweeper exist as real `backend/cmd/` binaries (verify by listing the directory;
  e.g. `calendar-vaccination-projector`, `calendar-reminder-sweeper`,
  `calendar-escalation-sweeper`). Do NOT flag legitimate work on those calendar
  workers as scope creep, and do NOT label them "deferred."
- **DEFERRED (spec exists, wiring pending — verify a binary/migration does NOT
  yet exist before asserting this):** feed-direction generation/Diff (awaits
  counts/shifting closure), **generic** calendar projection beyond the shipped
  vaccination slice, procurement intake saga, remaining SM-2 re-scope wiring,
  broader missed-deadline SLA waterfall read models, vaccination coverage KPI
  projection, herd-animal Path B rename (`herd_animals`/`animal_id`). If a cmd
  binary or migration for one of these now exists, it is no longer deferred —
  update this list rather than flagging the code.
- **PARKED / rejected (not V1 — reject as scope creep):** mother-vaccination
  status as input; breeding/genetics verticals; separate config rows for
  deworming / biosecurity / feed-water testing / sanitation / SOP-video /
  stock-checks; cold-chain excursion + quarantine automation; booster-chain
  interrupt policy; withdrawal-period sale-block automation.

When a change adds code for a PARKED area, or wires a genuinely DEFERRED area
without the maintainer reopening scope, that's a HIGH finding — surface it, don't
wave it through. But first confirm against `backend/cmd/` + migrations that the
area is actually still deferred and not already shipped (calendar workers are the
common false positive).
