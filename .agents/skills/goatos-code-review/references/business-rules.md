# Business-Rule Fidelity — Vaccination, Obligations, Domains

Wrong medical/business rules are worse than wrong code — they ship silent harm.
The rule docs below are authoritative; the summaries here are a review aid, not a
replacement. **Always read the cited doc (or query the Graphify docs graph)
before judging domain logic**, and honor the maintainer-rule lock in `AGENTS.md`:
if a change encodes a new/contradicting rule, surface the conflict and require an
explicit decision — do not silently accept it.

Authoritative sources:
- Vaccination rules: `docs/preventive-care-vaccination/vaccination-rules.md` (+ `PRD.md`, `TRD.md`)
- Obligation engine: `docs/protocol-engine/obligation-engine.md`
- Calendar: `docs/decisions/calendar-ownership.md`
- Feed direction: `docs/feed-direction/PRD.md`, `TRD.md`, `DEPENDENCY-CLOSURE-TRD.md`
- Forms/SOP: `context/forms/final-forms-sop-engine.md`
- Org / species / shed base model: `context/source-findings/goats-and-parks-source-findings.md`

## Vaccination rules (verify against the doc — do not hardcode from memory)

Code: `backend/internal/vaccination/`, `backend/internal/protocol/`,
`backend/internal/obligation/`.

Key rules a reviewer checks (source: `vaccination-rules.md`):
- **Dose spacing:** live→live 4-week min gap; live→killed 2-week; killed→killed
  2-week; ET+TT kid booster 3-week. Bacterial+viral same-day allowed; live-viral +
  killed-viral same-day allowed. "Two live same day" is NOT a rule (that's the
  4-week gap).
- **Max 2 shots per animal per drive/visit.** Explicit allowed same-drive combos
  only. The examples here are non-exhaustive; review against the active source
  docs and `backend/internal/obligation/app/drive_planner_config.go` before
  flagging or approving a combo.
- **Warm-up hold:** 7 days from farm-entry date (not source dose date).
- **Species selection is real:** obligations select by species/age/stage. Goat Pox
  = goat-only; Sheep Pox / Blue Tongue = sheep-only; ET+TT / PPR / FMD / HS = all.
  Mixed-species: kids may share a drive when safe; adults run species-specific
  execution groups in the same park visit.
- **Defers are safety blocks, not planner skips:** ICU, quarantine, sick/under
  treatment, pregnancy months 4-5, post-breeding hold (≥1 month). On recovery,
  reopen the missed obligation from the recovery date and rejoin the nearest
  compatible same-park drive within 7 calendar days — else create a micro-drive.
  No recovered animal is left waiting more than a week. (Review this flow via
  `references/kernel-and-scale.md` → "Defer-recovery re-entry".)
- **Drive batching:** may hold a due shed/tag group up to 7 calendar days to
  combine with a compatible same-park group — ONE-TIME per obligation/dose cycle,
  no rolling postponement.
- **Missed-dose handling is cycle-relative**: if the next drive is a later cycle,
  vaccinate immediately; if the same-cycle drive is within 2 weeks, wait for it;
  if the same-cycle drive is more than 2 weeks away, vaccinate immediately.
  Trusted prior evidence (our supervised parks / procurement holding parks only)
  suppresses scheduled work; third-party/vendor claims do NOT.

### Forbidden — never accept:
- [ ] **Mother-vaccination status as a scheduling input** — explicitly rejected
      everywhere (config, seed, import, model, condition). Every kid uses the
      approved standard schedule; mothers are kept vaccinated operationally.
- [ ] Third-party/vendor vaccine claims used to suppress scheduled work
- [ ] Rolling/repeated postponement in drive batching (one-time 7-day hold only)
- [ ] Goat-only `CHECK` constraints that block mixed-species — species is an
      animal fact, not a DDL check
- [ ] Free-text SOP label instead of a bound published `sop_version_id` on an executable rule

### Always require:
- [ ] Idempotency key on generation / completion / deferred-state writes
- [ ] Obligation-level audit via the status-events ledger
- [ ] Proof/verification before the next-stage (booster) obligation is generated
- [ ] Mixed-species validation at animal create, stage assign, rule publish, generation
- [ ] Postgres as canonical truth (not Cloud Tasks, Pub/Sub, or frontend state)

## Obligation engine (source: `docs/protocol-engine/obligation-engine.md`)

- **Status vocabulary and transitions:** durable `obligation_instances.status`
  values are `scheduled`, `due`, `in_progress`, `deferred`, `completed`,
  `missed`, `waived`, `canceled`, and `superseded`. Do not invent `pending`,
  `assigned`, or British-spelled `cancelled` as durable statuses. This is not one
  linear chain: scheduled/due rows can defer, in-progress rows can miss, exited
  animals can cancel open/deferred/missed work, and explicit repair/regeneration
  paths may reopen or supersede the same logical work while preserving audit and
  status-event evidence.
- **Date semantics:** due dates, missed marking, recovery re-entry, and drive
  planned dates are calendar decisions. Review whether the code intentionally
  uses the animal/location calendar (locations default to `Asia/Kolkata`) instead
  of accidentally deriving the day from UTC.
- **Generation** is event-driven (birth / procurement / stage-change / prior-dose
  completion → booster). Deterministic idempotency key + duplicate-spawn guard.
- **Birth-age schedules require DOB truth:** `birth_age` rules must fail visibly
  or defer with reason when DOB/estimated DOB is missing; do not silently skip
  animals into no-work/no-gap states.
- **Re-scope (SM-2):** an animal-state change (pregnancy enter/exit, stage change)
  re-evaluates eligibility — may open new dues or suppress current ones.
- **Animal exit (death/sale/cull/lost/transfer):** open obligations/batches must
  be canceled or repaired, stock reservations released/reconciled, status/audit
  evidence emitted, and projections refreshed so exited animals do not remain
  overdue.
- **In-flight version/override changes:** activation previews open obligations
  and in-progress batches. Open future work may be canceled/superseded/reissued
  only through explicit repair with status events; in-progress batches and closed
  history stay on their original version unless a documented correction path says
  otherwise.
- **Authority:** vaccination config publish is CEO/COO/superadmin only
  (`protocol.publish.<category>`); rule versions are immutable and resolved by
  scope policy (`tenant_default_with_park_overrides`) + effective dates
  (non-overlap enforced per scope). Impact preview is required before activation.

## Vaccination execution edge cases

For vaccination, reviewers must check the complete execution loop, not just
generation:

- [ ] Proof policy and SOP form require the source-normalized fields where
      applicable: animal scan, vaccine/item, medicine batch or vial/lot, dose,
      administered date/time, cold-chain/quantity checks, proof media,
      adverse-reaction notes/follow-up, and verifier/park-head review
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
      and one-time batching hold without rolling postponement
- [ ] Completion proof acceptance, rejection/rework, duplicate submit, and
      replay each have tests or live proof evidence when that path changed

## Other domains — one-line rule + doc

- **Calendar** (`docs/decisions/calendar-ownership.md`): admits an event only if it
  has a due window, an owning executable role, and requires human action. Owner
  pills: pc / feed / breeding / parks / procurement / inventory. Backend owns the
  projection; not Cloud Tasks, not frontend.
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

## Scope state — know what SHOULD exist before flagging "missing"

Do not flag intentionally-deferred work as a bug, and do not approve scope creep
into parked areas. Confirm current state via git/migrations + the docs; treat this
as a snapshot, not gospel.

- **BUILT:** obligation engine + state machine, vaccination rules schema/domain,
  protocol config + authority, locations tree + profiles, mixed-species catalog,
  generic inventory/FEFO, SOP definitions/submissions + proof/verification.
- **DEFERRED (spec exists, kernel wiring pending):** feed-direction generation/Diff
  (awaits counts/shifting closure), generic calendar projection beyond the
  vaccination slice (do not confuse this with the shipped vaccination calendar
  projector/reminder/escalation workers), procurement intake saga, SM-2 re-scope
  wiring, missed-deadline escalation / SLA waterfall read models, vaccination
  coverage KPI projection, herd-animal Path B rename (`herd_animals`/`animal_id`).
- **PARKED / rejected (not V1 — reject as scope creep):** mother-vaccination status
  as input; breeding/genetics verticals; separate config rows for deworming /
  biosecurity / feed-water testing / sanitation / SOP-video / stock-checks;
  cold-chain excursion + quarantine automation; booster-chain interrupt policy;
  withdrawal-period sale-block automation.

When a change adds code for a PARKED area, or wires a DEFERRED area without the
maintainer reopening scope, that's a HIGH finding — surface it, don't wave it
through.
