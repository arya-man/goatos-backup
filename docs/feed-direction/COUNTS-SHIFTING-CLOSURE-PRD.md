# Counts/Shifting Closure - PRD

**Status:** Draft v1, dependency closure for Feed Direction gate `G2`
**Date:** 2026-06-30
**Updated:** 2026-07-23 (operational shifting handoff and temporary-identifier implementation snapshot)
**Companions:** [Feed Direction Feature Closure Plan](./FEATURE-CLOSURE-PLAN.md),
[Feed Direction Dependency Closure PRD](./DEPENDENCY-CLOSURE-PRD.md),
[Counts/Shifting Closure TRD](./COUNTS-SHIFTING-CLOSURE-TRD.md), and
[Feed Direction Build-To-Done Goal](./BUILD-TO-DONE-GOAL.md)

## 1. Purpose

Feed Direction cannot build honestly until Counts/Shifting exposes the aggregate
contract that the source requires. This PRD scopes that work as its own closure
item instead of hiding it inside Feed.

The committed GoatOS repo now has source-row count sync/projection tables,
physical Base Count anchors, a realized ShiftingEvent ledger, bounded
one-day-horizon projection reads, fail-closed exception work, and per-goat
movement history. Source parity, owner-reviewed mappings, scale evidence, and
the remaining `CSG1`-`CSG10` readiness outcomes still govern whether this is
safe Feed input; table and API presence alone do not close `G2`. Reviewed
ration context is resolved from source-backed shed/cohort reference data or
reported as a blocker; it is not a separate physical Base Count grain.
Breed/tag constraint tables without shed placement are ration evidence, not
count truth.

Initial Counts/Shifting closure is aggregate-only at shed + breed grain.
RFID-to-shed per-animal association is a future replacement path, not hidden
work inside `G2`.

Use the safe-input boundary from
[FEATURE-CLOSURE-PLAN.md](./FEATURE-CLOSURE-PLAN.md). Counts/Shifting is done
enough for Feed when it can produce bounded target-date shed + breed + cohort
rows with reviewed source hashes, projection horizon, high-risk counters, and
ration-context state, or an explicit blocker row. It does not need to own
ration calculation, session splitting, packing obligations, frontend surfaces,
or full Feed E2E before Feed moves to the next product milestone.

High-risk shifted cohorts are part of the `G2` safety contract. If pregnant,
lactating, warm-up, or similar stage/risk rows move into a destination shed,
Counts/Shifting must project the destination count impact and surface unresolved
destination ration context as a blocker. Feed Direction must not quietly
underfeed those animals, and it must not overfeed as a hidden safety buffer; the
quantity/session/feed-safety policy closes through `G4`, `G5`, and `G9`.

### 1.1 Operational implementation snapshot (2026-07-23)

The branch also ships operator workflows adjacent to the aggregate Feed-input
contract:

- Managers review and authorize shifting requests in admin-web. Authorization
  records intent and does not move an animal.
- Android exposes a bounded, offline-first **Shifting -> Pending** queue with
  park and source-shed filters. The operator completes or cancels the authorized
  movement using stable outbox idempotency keys.
- Only completion calls the canonical atomic relocation path. Destination
  stage comes from the authorized destination shed profile; successful
  completion emits `goat.location.changed` and, where applicable,
  `goat.stage_changed`, followed by Vaccination rescope and eligibility
  re-evaluation.
- Birth submission creates every newborn with a provisional `temporary_tag`.
  There is no standalone **Awaiting RFID** queue: the final **Tag the kid** action
  in that child's Birth workflow opens the promotion form directly. Promotion
  atomically retires the temporary tag and attaches one required plus one
  optional distinct permanent RFID, with identifier domain events and
  idempotent replay; the workflow still requires its separate tagging video.

The temporary-tag workflow conflicts with the current canonical statement in
`context/product/glossary.md` that every accepted/canonical animal already has
`animal_identifier_1`. This PRD records what is implemented but does not retire
that business invariant. The maintainer must decide whether newborn temporary
records are a permitted exception or remain provisional/non-canonical until
promotion; the glossary and validation rules must then be updated together.

These per-animal operational workflows do not change the initial `G2`
Feed-consumption grain, which remains aggregate shed + breed/cohort projection
truth with explicit blockers.

## 2. Product Outcome

Counts/Shifting must answer these questions for Feed Direction:

- What is the current realized count by tenant, park, shed, and breed?
- Which physical Base Count anchor is authoritative for that grain?
- Which ShiftingEvents have changed realized truth?
- Which authorized future-effective ShiftingEvents should affect tomorrow's
  Feed Direction projection?
- Which movements are blocked from count impact because stage/cohort impact,
  authorization, proof, verification, or source quality is unresolved?
- Which discrepancies or unreported shiftings require process work?
- Which shifted high-risk cohorts require destination-shed ration-context
  review before Feed can generate safe quantities?

## 3. Source Inputs

| Source | Use |
| --- | --- |
| `Feed, Shiftings and Count.docx` v1.1 | Primary source for physical Base Count, append-only shifting ledger, one-day projection, Diff source semantics, and physical count adoption |
| Counting DB reconstruction, `Counting DB - values only.xlsx`, and feed-relevant wiki/source findings | Fixture/reference shape for aggregate counts, comparison tabs, stage tags, source discrepancies, aliases, and import validation gaps; `context/source-findings/goats-and-parks-source-findings.md` is the base source for shed-tag/cohort semantics; `context/source-findings/sheds-db-source-findings.md` is source evidence for shed profile tags/capacity/potential tags; not runtime truth |
| `Feed Directions Automation DB.xlsx` | Evidence for how old feed planning tried to join count rows to ration/session/formula tabs; not authoritative count truth or a target Counts schema |
| Shifting reports/source findings | Movement categories, priority, source/destination, proof and approval states |
| Legacy automation review | Dedup, retry, count-mismatch, alias-transform, and replay evidence |
| GoatOS identity/location tables | Future per-goat derivation option through `goat_identifiers` and `goat_location_history` after RFID-to-shed association is reliable; out of initial closure scope |

Legacy data is evidence and fixture material. Runtime truth must be Postgres
canonical state plus audit/outbox.
Workbook formula tabs, processed flags, and App Script transforms must not be
copied into Counts/Shifting. Counts owns physical aggregate count and movement
truth; Feed owns the reviewed resolver from those rows into nutrition/ration
cohorts.

Counts/Shifting must not invent shed-tag meanings. Use
`context/source-findings/goats-and-parks-source-findings.md` as the base source
for K0/K1/K2/K3, warm-up, pregnancy, lactation, mother, milking, fattening,
ICU/quarantine, buck, flushing, and breeding tag semantics. Unknown or
conflicting tags must become reviewed exceptions before Feed consumes them.
Use `context/source-findings/sheds-db-source-findings.md` for the legacy/manual
shed profile matrix: current tags, capacity-like values, and potential tags are
reviewed location-profile evidence only. GoatOS must replace manual Sheds DB
maintenance with effective-dated source-backed profile CRUD plus event-driven
birth, breeding, procurement, health, and ShiftingEvent workflows; direct sheet
values must not become Feed or Counts runtime truth.

Implementation status as of 2026-06-30: `backend/cmd/counts-source-import`
provides a reviewed typed JSONL import path for `base_count_anchor` and
`shifting_event` rows. It writes through the canonical Counts service, derives
deterministic source hashes/idempotency/fingerprints when omitted, requires
explicit shifting lifecycle state, and leaves ration context unresolved unless a
reviewed row supplies it. `count_source_import_runs` records each execute-mode
import batch with row counts, replay counts, failure counts, source reference,
status, and readiness evidence. `count_projection_recompute_runs` records
bounded recompute worker evidence with row counts, exception counts, projection
status, snapshot reference, timing, source contract, trace id, and failure
state. These are not raw XLSX parsers and do not approve workbook formulas,
`80/20` examples, or breed/tag constraint tables as runtime truth.
The import command has migrated-Postgres proof that replaying the same reviewed
Base Count + pregnant ShiftingEvent JSONL batch records replay evidence without
duplicating canonical anchors, movement headers, or structured impact rows.
The projection recompute command has migrated-Postgres proof that replaying the
same pregnant shifted-cohort projection records a second recompute audit run
while reusing the same immutable projection snapshot, without duplicating
projection rows or exceptions.
`backend/cmd/location-profile-source-import` provides the matching typed import
path for reviewed Location/Park profile evidence such as Sheds DB aliases,
capacity rows, and unresolved-label review items. Publishable rows must already
reference canonical `location_id`; otherwise the row stays review work instead
of becoming Feed/Counts runtime truth.
`backend/cmd/location-profile-coverage-check` verifies reviewed Sheds DB profile
coverage fixtures against canonical Location aliases and capacity records, and
can move `CSG7` to `blocked` or `pending`, never `ready`. Its migrated-Postgres
test proves canonical Locations service writes can satisfy the coverage check
and write CSG7 evidence, but this is not full Feed seeded E2E.
Feed readiness has seeded local integration proof that real Counts Base Count
and pregnant ShiftingEvent writes produce the ShiftingEvent outbox event, the
local projection handler recomputes both horizons, and the resulting
`destination_shortage` exception flows through the Feed readiness provider to
keep `G2` generation blocked. Full Feed generation from immutable projection
snapshots remains separate closure work.
`counts-source-parity-check` has migrated-Postgres proof that sanitized parity
fixtures can be compared against the canonical projection read path for
shed/breed/stage-age-sex rows with pregnant/lactating/warm-up counts and
ration-context state, then write `CSG10` evidence without turning the gate
ready. The executable high-risk sample fixture covers pregnant late gestation,
lactating/mother, pregnant warm-up, fattening male warm-up, and normal
non-pregnant rows; it is sanitized parity evidence, not a full workbook import
or owner-approved nutrition policy.
The canonical projection read model now includes page-bounded
`ShedBreedTotals` alongside detail rows, so Feed can see aggregate shed + breed
counts while retaining stage/age/sex and pregnant/lactating/warm-up counters for
safety review. The aggregate is evidence for the returned page only; full
workbook parity/no-cursor reads and seeded E2E remain open.

For any Counts/Shifting behavior that affects Feed Direction timing, Diff,
bridge handling, one-day projection, or physical count adoption,
`Feed, Shiftings and Count.docx` v1.1 is the controlling source. Legacy
Slack/App Script timings are audit/cutover evidence only and must not become
GoatOS schedules unless explicitly approved against that docx.

## 4. Readiness Gates

| ID | Gate | Required outcome |
| --- | --- | --- |
| CSG1 | Base Count anchor | Physical counts and reviewed typed source imports can create immutable anchors by tenant, park, shed, breed, counted_at, source, actor, and evidence hash |
| CSG2 | ShiftingEvent ledger | Manual/reviewed imported movements are append-only, idempotent, and carry source/destination, category, priority, raised/effective time, authorization, proof, verification, and status |
| CSG3 | Structured impacts | Each event carries structured cohort/stage impact; missing or ambiguous impact fails closed into exception work |
| CSG4 | Horizon split | `count_as_of(time)` and `projected_count_for(target_date)` have separate rules for realized truth vs one-day authorized future-effective projection |
| CSG5 | Base Count adoption | A new physical Base Count becomes the next anchor immediately; discrepancy investigation does not block adoption |
| CSG6 | Unreported-shifting detection | Count mismatch or unexpected delta creates auditable process-exception work with reviewed resolve/dismiss closure, not silent correction. This includes the live Base Count write hook, the bounded `counts-mismatch-scan` worker path for stale imported/historical anchors, and durable scan-run evidence before readiness can turn green. |
| CSG7 | Alias normalization | Breed, stage, shed-tag, age-class, and sex aliases from Counting DB, Feed Automation workbook, and Sheds DB are reviewed before Feed consumes projection output. The sanitized fixture `backend/testdata/counts/source-workbook-required-aliases.json` captures 121 required aliases, including SIROHI->Beetal-style breed review, Warmup/Fattening variants, pregnancy/lactation/mother/kid labels, sex labels, and dirty workbook spellings. Projection snapshots may move `CSG7` to `blocked` when `alias_conflict` exceptions exist, or `pending` when a non-empty snapshot has no alias conflicts; `ready` still requires source workbook parity, Sheds DB profile-tag coverage, and owner-approved alias review. |
| CSG8 | Idempotency and replay | Event ingestion, projection recompute, and source replay cannot double-apply movements |
| CSG9 | Projection API | Feed can consume bounded projection rows with source hash, anchor id, included-event hash, exception count, contract version, and ration-context resolution state |
| CSG10 | Scale and observability proof | Hot reads and projection queries are bounded by tenant, park, date, shed, breed, ration-context resolution state where materialized, and cursor where applicable; import, ingest, projection, retry, DLQ, exception, and query-plan metrics exist. `counts-workbook-mapping-check` validates the sanitized 117-column workbook/source map before parity work can rely on those columns, and `counts-workbook-source-scan` validates that the real local XLSX files expose those mapped sheets/headers without importing private rows. `counts-query-plan-check` proves index paths for Counts hot reads, including migrated synthetic Feed-target movement rows through park/date source and destination indexes plus projection-row hot reads with snapshot park/date filters, while `count_source_import_runs` and `count_projection_recompute_runs` provide typed import and recompute worker evidence. CSG10 remains pending until source parity, observability breadth, production-scale/load evidence, and seeded E2E are proven. |

`CSG1`-`CSG10` roll up into Feed gate `G2`. The Feed readiness endpoint must
show the Counts/Shifting subgate statuses, owners, evidence pointers, and blocker
reasons beneath `G2` so Counts closure is auditable from the Feed launch gate.
The current subgate row shows the latest status/evidence, while
`counts_shifting_readiness_evidence` keeps a write-by-write evidence ledger so
multi-proof gates such as `CSG10` do not lose mapping, source-scan,
source-parity, query-plan, import, mismatch-scan, or recompute evidence when a
later worker refreshes the same subgate. The readiness API exposes a bounded
recent-evidence summary under each CSG subgate so `G2` launch review can inspect
multi-proof closure without treating any single pending `CSG10` pointer as
complete.
Repository writes now promote only the subgates they directly prove: Base Count
anchors promote `CSG1`/`CSG5`, ShiftingEvents with impacts promote
`CSG2`/`CSG3`, projection snapshots promote `CSG9`, and both snapshot horizons
are required before `CSG4` is ready. This evidence does not make `G2` green by
itself. Projection snapshots now refresh `CSG7` evidence: any `alias_conflict`
keeps it `blocked`; a non-empty conflict-free snapshot can make it `pending`
only. The `counts-alias-coverage-check` worker checks sanitized required alias
fixtures, including the 121-alias Counting DB / Feed Automation / Sheds DB
source-workbook manifest and Sheds DB profile tags, against approved
`count_dimension_aliases` and can also move `CSG7` to `blocked` or `pending`
but never `ready`. The `location-profile-coverage-check` worker checks reviewed
Sheds DB profile fixtures against `location_aliases` and
`location_capacity_records` and can also move `CSG7` to `blocked` or `pending`,
but never `ready`. Counts readiness treats `CSG7` as composite: pending requires
no open `alias_conflict` projection exception plus non-blocked latest evidence
from both `counts-alias-coverage-check:*` and
`location-profile-coverage-check:*`; one passing checker cannot hide the other
missing evidence family.
The `counts-workbook-mapping-check` worker validates the
sanitized workbook column map, including count source, feed output, feed-vector,
supply planning, proof, transport, consumption/wastage, and Sheds DB profile
role groups, and can move `CSG10` to `blocked` or `pending`, but never `ready`.
The `counts-workbook-source-scan` worker checks local XLSX workbook structure
against the same sanitized map, including the Template sheet's `CBE`/`CPT`
section-header farm/park shape, and can move `CSG10` to `blocked` or `pending`,
but never `ready`; it does not import raw rows or approve formulas.
The `counts-source-parity-check` worker compares sanitized
source parity fixtures against canonical projection reads, including
shed/breed/stage, age class, sex, headcount, pregnant/lactating/warm-up counts,
and ration-context state, and can move `CSG10` to `blocked` or `pending`, but
never `ready`. The `counts-query-plan-check`
worker has migrated-Postgres proof for bounded movement/window and projection
row index paths and can move `CSG10` to `blocked` or `pending`, but never
`ready`. Canonical replay, source-import replay, and projection-recompute
replay evidence may move `CSG8` to `pending`, and both the typed import path
and projection recompute worker have migrated replay proof, but `CSG8` does not
become ready until full source workbook parity and seeded E2E prove no double
application across the full path. `CSG7`,
`CSG8`, `CSG10`, source parity, UI, and seeded E2E still have to close in
order.

Base Count cadence has source history moving from roughly weekly to roughly
monthly. The closure must expose cadence as reviewed policy or schedule config;
do not hardcode a weekly/monthly interval into the projection model.

## 5. Acceptance Criteria

Counts/Shifting closure is complete when:

1. Feed can call a documented fail-closed projection contract for realized and
   one-day projection horizons.
2. Physical Base Count anchors, ShiftingEvent application, and projection output
   are replay-safe and auditable.
3. Missing stage/cohort impact, unreported shiftings, count mismatch, and alias
   conflicts surface as owner-visible exception work that can be reviewed,
   resolved, or dismissed with actor, reason, optional source reference, and
   idempotent replay protection.
4. Source evidence, reviewed typed imports, and fixture rows can be compared to
   canonical projections without becoming runtime truth. The
   `counts-source-import` command covers typed Base Count/Shifting JSONL rows
   and records `count_source_import_runs` evidence; recompute paths record
   `count_projection_recompute_runs` evidence; the sanitized workbook column
   map has checker coverage and real local XLSX sheet/header scan proof, while
   full row parsing/import, owner-approved mapping review, Sheds DB typed
   profile fixtures, and full parity fixtures remain review work.
   Stale imported mismatches are surfaced by the bounded
   `counts-mismatch-scan` worker path.
5. SQL plan and synthetic-scale checks prove no full-herd or unbounded count
   scan is required for Feed generation. The first implementation artifact is
   `backend/cmd/counts-query-plan-check`, which checks the expected Postgres
   indexes for projection anchors, ShiftingEvent windows, projection snapshots,
   projection row reads, and stale/imported mismatch-scan paging.
6. `GET /feed-direction/readiness` can expose `CSG1`-`CSG10` breakdown under
   Feed gate `G2`.
7. Acceptance evidence proves initial output is aggregate shed + breed grain,
   with ration context either resolved from reviewed source-backed context or
   blocked with an explicit reason, and does not depend on RFID-to-shed per-goat
   derivation.
8. Shifted pregnant/lactating/warm-up cohorts create projected destination rows
   and fail closed when the destination shed ration context is unresolved,
   instead of letting Feed generate normal shed-average quantities.
9. The next build follows [BUILD-TO-DONE-GOAL.md](./BUILD-TO-DONE-GOAL.md) for
   source cross-check, seeded E2E proof, high-effort review, and push gating.
