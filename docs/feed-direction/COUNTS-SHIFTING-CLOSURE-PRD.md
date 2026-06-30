# Counts/Shifting Closure - PRD

**Status:** Draft v1, dependency closure for Feed Direction gate `G2`
**Date:** 2026-06-30
**Companions:** [Feed Direction Dependency Closure PRD](./DEPENDENCY-CLOSURE-PRD.md),
[Counts/Shifting Closure TRD](./COUNTS-SHIFTING-CLOSURE-TRD.md), and
[Feed Direction Build-To-Done Goal](./BUILD-TO-DONE-GOAL.md)

## 1. Purpose

Feed Direction cannot build honestly until Counts/Shifting exposes the aggregate
contract that the source requires. This PRD scopes that work as its own closure
item instead of hiding it inside Feed.

The committed GoatOS repo already has source-row count sync/projection tables
and per-goat movement history. It does not yet have the physical Base Count
anchor, realized ShiftingEvent ledger, one-day horizon projection, and
fail-closed exception model that Feed Direction needs at shed + breed grain.
Reviewed ration context is resolved from source-backed shed/cohort reference
data or reported as a blocker; it is not a separate physical Base Count grain.
Breed/tag constraint tables without shed placement are ration evidence, not count
truth.

Initial Counts/Shifting closure is aggregate-only at shed + breed grain.
RFID-to-shed per-animal association is a future replacement path, not hidden
work inside `G2`.

High-risk shifted cohorts are part of the `G2` safety contract. If pregnant,
lactating, warm-up, or similar stage/risk rows move into a destination shed,
Counts/Shifting must project the destination count impact and surface unresolved
destination ration context as a blocker. Feed Direction must not quietly
underfeed those animals, and it must not overfeed as a hidden safety buffer; the
quantity/session/feed-safety policy closes through `G4`, `G5`, and `G9`.

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
Feed readiness has seeded local integration proof that real Counts Base Count,
pregnant ShiftingEvent, dual-horizon projection recompute, and the resulting
`destination_shortage` exception flow through the Feed readiness provider and
keep `G2` generation blocked. Full Feed generation from immutable projection
snapshots remains separate closure work.
`counts-source-parity-check` has migrated-Postgres proof that sanitized parity
fixtures can be compared against the canonical projection read path and can
write `CSG10` evidence without turning the gate ready.

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
| CSG7 | Alias normalization | Breed, stage, shed-tag, and Sheds DB profile-tag aliases such as SIROHI->Beetal, Warmup/Fattening variants, pregnancy/lactation/mother/kid labels, and dirty workbook spellings are reviewed before Feed consumes projection output. Projection snapshots may move `CSG7` to `blocked` when `alias_conflict` exceptions exist, or `pending` when a non-empty snapshot has no alias conflicts; `ready` still requires source workbook parity, Sheds DB profile-tag coverage, and owner-approved alias review. |
| CSG8 | Idempotency and replay | Event ingestion, projection recompute, and source replay cannot double-apply movements |
| CSG9 | Projection API | Feed can consume bounded projection rows with source hash, anchor id, included-event hash, exception count, contract version, and ration-context resolution state |
| CSG10 | Scale and observability proof | Hot reads and projection queries are bounded by tenant, park, date, shed, breed, ration-context resolution state where materialized, and cursor where applicable; import, ingest, projection, retry, DLQ, exception, and query-plan metrics exist. `counts-query-plan-check` proves index paths for Counts hot reads, including migrated synthetic Feed-target movement rows through park/date source and destination indexes plus projection-row hot reads with snapshot park/date filters, while `count_source_import_runs` and `count_projection_recompute_runs` provide typed import and recompute worker evidence. CSG10 remains pending until source parity, observability breadth, production-scale/load evidence, and seeded E2E are proven. |

`CSG1`-`CSG10` roll up into Feed gate `G2`. The Feed readiness endpoint must
show the Counts/Shifting subgate statuses, owners, evidence pointers, and blocker
reasons beneath `G2` so Counts closure is auditable from the Feed launch gate.
Repository writes now promote only the subgates they directly prove: Base Count
anchors promote `CSG1`/`CSG5`, ShiftingEvents with impacts promote
`CSG2`/`CSG3`, projection snapshots promote `CSG9`, and both snapshot horizons
are required before `CSG4` is ready. This evidence does not make `G2` green by
itself. Projection snapshots now refresh `CSG7` evidence: any `alias_conflict`
keeps it `blocked`; a non-empty conflict-free snapshot can make it `pending`
only. The `counts-alias-coverage-check` worker checks sanitized required alias
fixtures, including Sheds DB profile tags, against approved
`count_dimension_aliases` and can also move `CSG7` to `blocked` or `pending`
but never `ready`. The `location-profile-coverage-check` worker checks reviewed
Sheds DB profile fixtures against `location_aliases` and
`location_capacity_records` and can also move `CSG7` to `blocked` or `pending`,
but never `ready`. The `counts-source-parity-check` worker compares sanitized
source parity fixtures against canonical projection reads and can move `CSG10`
to `blocked` or `pending`, but never `ready`. The `counts-query-plan-check`
worker has migrated-Postgres proof for bounded movement/window and projection
row index paths and can move `CSG10` to `blocked` or `pending`, but never
`ready`. Canonical replay and source-import replay evidence may move `CSG8` to
`pending`, but it does not become ready until full source replay parity,
projection replay proof, and seeded E2E prove no double application. `CSG7`,
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
   `count_projection_recompute_runs` evidence; raw workbook/XLSX mapping,
   Sheds DB typed profile fixtures, and parity fixtures remain review work.
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
