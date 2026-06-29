# Counts/Shifting Closure - PRD

**Status:** Draft v1, dependency closure for Feed Direction gate `G2`
**Date:** 2026-06-30
**Companions:** [Feed Direction Dependency Closure PRD](./DEPENDENCY-CLOSURE-PRD.md),
[Counts/Shifting Closure TRD](./COUNTS-SHIFTING-CLOSURE-TRD.md)

## 1. Purpose

Feed Direction cannot build honestly until Counts/Shifting exposes the aggregate
contract that the source requires. This PRD scopes that work as its own closure
item instead of hiding it inside Feed.

The committed GoatOS repo already has source-row count sync/projection tables
and per-goat movement history. It does not yet have the physical Base Count
anchor, realized ShiftingEvent ledger, one-day horizon projection, and
fail-closed exception model that Feed Direction needs at shed + breed +
stage/tag grain.

## 2. Product Outcome

Counts/Shifting must answer these questions for Feed Direction:

- What is the current realized count by tenant, park, shed, breed, and stage/tag?
- Which physical Base Count anchor is authoritative for that grain?
- Which ShiftingEvents have changed realized truth?
- Which authorized future-effective ShiftingEvents should affect tomorrow's
  Feed Direction projection?
- Which movements are blocked from count impact because stage/cohort impact,
  authorization, proof, verification, or source quality is unresolved?
- Which discrepancies or unreported shiftings require process work?

## 3. Source Inputs

| Source | Use |
| --- | --- |
| `Feed, Shiftings and Count.docx` v1.1 | Physical Base Count, append-only shifting ledger, one-day projection, Diff source semantics |
| Counting DB reconstruction | Fixture/reference shape for aggregate counts and comparison tabs |
| Shifting reports/source findings | Movement categories, priority, source/destination, proof and approval states |
| Legacy automation review | Dedup, retry, count-mismatch, and alias-transform evidence |
| GoatOS identity/location tables | Future per-goat derivation option through `goat_identifiers` and `goat_location_history` after RFID-to-shed association is reliable |

Legacy data is evidence and fixture material. Runtime truth must be Postgres
canonical state plus audit/outbox.

## 4. Readiness Gates

| ID | Gate | Required outcome |
| --- | --- | --- |
| CSG1 | Base Count anchor | Physical counts can create immutable anchors by tenant, park, shed, breed, stage/tag, counted_at, source, actor, and evidence hash |
| CSG2 | ShiftingEvent ledger | Movements are append-only, idempotent, and carry source/destination, category, priority, raised/effective time, authorization, proof, verification, and status |
| CSG3 | Structured impacts | Each event carries structured cohort/stage impact; missing or ambiguous impact fails closed into exception work |
| CSG4 | Horizon split | `count_as_of(time)` and `projected_count_for(target_date)` have separate rules for realized truth vs one-day authorized future-effective projection |
| CSG5 | Base Count adoption | A new physical Base Count becomes the next anchor immediately; discrepancy investigation does not block adoption |
| CSG6 | Unreported-shifting detection | Count mismatch or unexpected delta creates process-exception work, not silent correction |
| CSG7 | Alias normalization | Breed and stage aliases such as SIROHI->Beetal and Warmup/Fattening variants are reviewed before projection output |
| CSG8 | Idempotency and replay | Event ingestion, projection recompute, and source replay cannot double-apply movements |
| CSG9 | Projection API | Feed can consume bounded projection rows with source hash, anchor id, included-event hash, exception count, and contract version |
| CSG10 | Scale and observability proof | Hot reads and projection queries are bounded by tenant, park, date, shed, breed, stage/tag, and cursor where applicable; import, ingest, projection, retry, DLQ, exception, and query-plan metrics exist |

`CSG1`-`CSG10` roll up into Feed gate `G2`. The Feed readiness endpoint must
show the Counts/Shifting subgate statuses, owners, evidence pointers, and blocker
reasons beneath `G2` so Counts closure is auditable from the Feed launch gate.

## 5. Acceptance Criteria

Counts/Shifting closure is complete when:

1. Feed can call a documented fail-closed projection contract for realized and
   one-day projection horizons.
2. Physical Base Count anchors, ShiftingEvent application, and projection output
   are replay-safe and auditable.
3. Missing stage/cohort impact, unreported shiftings, count mismatch, and alias
   conflicts surface as owner-visible exception work.
4. Source evidence and fixture rows can be compared to canonical projections
   without becoming runtime truth.
5. SQL plan and synthetic-scale checks prove no full-herd or unbounded count
   scan is required for Feed generation.
6. `GET /feed-direction/readiness` can expose `CSG1`-`CSG10` breakdown under
   Feed gate `G2`.
