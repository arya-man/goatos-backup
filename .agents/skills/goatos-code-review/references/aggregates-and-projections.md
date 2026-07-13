# Aggregate and Projection Correctness

Load this reference for any read model, projection, dashboard/card summary,
calendar grouping, reminder rail, or SQL/Go query that combines `JOIN` with
`COUNT`, `SUM`, `GROUP BY`, JSON aggregation, or pagination.

These changes are not approved because the arithmetic invariant holds or the
query compiles. The reviewer must prove the population, identity, cardinality,
and page boundary independently.

## Required proof before approval

1. **Canonical membership** — name the source that decides which facts belong
   to the displayed group. Build both the event/card and its summary from that
   membership (for example, a drive/batch membership id). Do not reconstruct
   membership later from coincidentally-equal location/date fields.
2. **Stable group key** — write the exact producer key and consumer join key
   side by side. Every component must have the same semantics and timezone.
   Derived execution dates, planned dates, window dates, and obligation due
   dates are not interchangeable.
3. **One-row grain** — state the intended grain before every aggregate. For
   every join, prove `1:1`, pre-aggregate the many side, use a semijoin, or
   explicitly deduplicate by the fact's stable id. A comment saying "one row per
   obligation" is not proof if a selector/dimension table can contain many rows.
4. **Hierarchy resolution** — resolve farm/park/shed/cohort with an explicit
   scope-type matrix. Generic `COALESCE(parent_id, self_id)` is invalid unless
   every supported scope has the same depth.
5. **Whole-result semantics** — totals, reminders, empty states, and group
   summaries must be computed by the backend over the full filtered result, not
   by filtering the current 20/100-row UI page.
6. **Bounded execution** — a keyset page loop must not rerun a tenant/date-wide
   aggregate for every page. Prove one bounded aggregation, a materialized
   projection, or an indexed per-page lookup with an EXPLAIN/latency test at the
   applicable scale bar.
7. **Backend-owned presentation contract** — labels, fallback text, and empty
   state copy returned by the contract must not be recreated as hardcoded
   frontend fallbacks.

## Mandatory adversarial tests

Use realistic fixtures and names that make the broken dimension obvious:

- `...OneToMany...` or `...MultipleDimensions...`: one fact joined to two
  selector/dimension rows still counts once.
- `...DateShift...`, `...ScheduledDate...`, or `...ExecutionDate...`: source due
  day D and planned/execution day D+7 still attach to the correct group.
- `...ScopeHierarchy...`, `...ParkScope...`, or `...CohortScope...`: cover each
  supported scope depth, not only shed scope.
- `...Pagination...`, `...PageBoundary...`, or `...MultiPage...`: a summary or
  reminder beyond page one remains visible and totals do not change with page
  size.
- `...StatusMatrix...`, `...EveryStatus...`, or `...StatusBuckets...`: when
  status buckets are present, exercise every live DB-constrained status and
  prove `total = sum(disjoint buckets)` without relying on duplicated rows.

The current status set must be read from the latest migration `CHECK`
constraint and state-machine docs. Never copy a remembered list into the test.

## Mechanical evidence marker

For a new or changed SQL aggregate/projection, add a nearby comment that records
the reviewed design:

```text
projection-review: membership=<canonical source>; group_key=<stable identity>; join_cardinality=<proof/dedup>; pagination=<whole-result and execution bound>; scope=<scope mapping or n/a>
```

`make aggregate-projection-guard` checks the marker and the changed adversarial
tests. The marker is a review aid, not a waiver: unsupported claims are a
finding, and the reviewer must verify them against migrations, query shape, and
tests.

## Reject these false greens

- Bucket counts add up only because every bucket was multiplied by the same
  fan-out.
- The fixture has one dimension row, one scope type, one date, or fewer rows
  than the first page.
- The test asserts a non-null DTO but not the exact membership and counts.
- A frontend fallback makes a missing backend summary look acceptable.
- A diff-scoped guard reports no files because the relevant work is unstaged or
  untracked.
- Unit tests pass while projection refresh work is repeated once per page; ask
  for query-count plus latency/EXPLAIN evidence.

Approval requires the source marker, the applicable adversarial tests, and the
reviewer's independent verification of all claims.
