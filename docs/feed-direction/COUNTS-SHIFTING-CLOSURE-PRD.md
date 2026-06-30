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
| Counting DB reconstruction, `Counting DB - values only.xlsx`, and feed-relevant wiki/source findings | Fixture/reference shape for aggregate counts, comparison tabs, stage tags, source discrepancies, aliases, and import validation gaps; not runtime truth |
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

For any Counts/Shifting behavior that affects Feed Direction timing, Diff,
bridge handling, one-day projection, or physical count adoption,
`Feed, Shiftings and Count.docx` v1.1 is the controlling source. Legacy
Slack/App Script timings are audit/cutover evidence only and must not become
GoatOS schedules unless explicitly approved against that docx.

## 4. Readiness Gates

| ID | Gate | Required outcome |
| --- | --- | --- |
| CSG1 | Base Count anchor | Physical counts can create immutable anchors by tenant, park, shed, breed, counted_at, source, actor, and evidence hash |
| CSG2 | ShiftingEvent ledger | Movements are append-only, idempotent, and carry source/destination, category, priority, raised/effective time, authorization, proof, verification, and status |
| CSG3 | Structured impacts | Each event carries structured cohort/stage impact; missing or ambiguous impact fails closed into exception work |
| CSG4 | Horizon split | `count_as_of(time)` and `projected_count_for(target_date)` have separate rules for realized truth vs one-day authorized future-effective projection |
| CSG5 | Base Count adoption | A new physical Base Count becomes the next anchor immediately; discrepancy investigation does not block adoption |
| CSG6 | Unreported-shifting detection | Count mismatch or unexpected delta creates auditable process-exception work with reviewed resolve/dismiss closure, not silent correction |
| CSG7 | Alias normalization | Breed and stage aliases such as SIROHI->Beetal and Warmup/Fattening variants are reviewed before projection output |
| CSG8 | Idempotency and replay | Event ingestion, projection recompute, and source replay cannot double-apply movements |
| CSG9 | Projection API | Feed can consume bounded projection rows with source hash, anchor id, included-event hash, exception count, contract version, and ration-context resolution state |
| CSG10 | Scale and observability proof | Hot reads and projection queries are bounded by tenant, park, date, shed, breed, ration-context resolution state where materialized, and cursor where applicable; import, ingest, projection, retry, DLQ, exception, and query-plan metrics exist |

`CSG1`-`CSG10` roll up into Feed gate `G2`. The Feed readiness endpoint must
show the Counts/Shifting subgate statuses, owners, evidence pointers, and blocker
reasons beneath `G2` so Counts closure is auditable from the Feed launch gate.

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
4. Source evidence and fixture rows can be compared to canonical projections
   without becoming runtime truth.
5. SQL plan and synthetic-scale checks prove no full-herd or unbounded count
   scan is required for Feed generation.
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
