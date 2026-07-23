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
   Vaccination operator drives have a stricter source order: when
   `vaccination_drive_assignments` exists, assignment `planned_date` is the
   producer key for scheduled execution and all Calendar / Action Center /
   Protocol Adherence / Control Tower / Workflow / execution consumers must use
   that before batch `planned_date` or obligation `due_at`. A date move or
   override that only updates Full Schedule is incomplete if L1/L2/L3 Calendar
   or process-integrity reads still key from stale dates.
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

## Effective-state validation on partial updates

When a change allows a partial update to a multi-field record (date change,
status change, rule/version/config change, or scope reassignment), the update
must re-validate the WHOLE effective locked record, not just the changed field.
A partial edit that only re-validates the changed field can leave the record in
an impossible state.

Review checkpoints:
- [ ] A date correction validates date ordering, eligibility rules, and conflict
      windows using the NEW full record, not only the new date
- [ ] A status transition re-validates the whole record's state for legal
      transitions (a vaccination status change must re-check eligibility, defer
      rules, protocol version, location scope — not only that the status change
      is legal in isolation)
- [ ] A version/rule/config change to an active record triggers recompute of
      affected derived fields (capacity, window, obligation dates, projections);
      the edit and the recompute are atomic in the same transaction
- [ ] A scope reassignment (move a goat to a different shed/park) re-validates
      all scope-scoped facts (capacity, permits, privacy, ownership, location
      rules, drive membership, existing obligations) and updates them atomically
- [ ] Tests adversarially corrupt a record into a state that would be impossible
      with re-validation (e.g. a future date where a delay window exists, a status
      that violates the machine, conflicting fields), then verify the edit rolls
      back or fails with a clear error

## Page-size business completeness (no silent truncation)

When a paginated list or reminder rail truncates at a page size (20, 50, 100
rows), the business metric or count derived from that page must not change with
the page size. A silent change in a summary or total when the page size changes
is a hidden bug.

Review checkpoints:
- [ ] A paginated feed that shows "today's work" displays ALL work for the day,
      paginated; if today has 25 items and the page size is 20, the "done/pending
      count for today" on the page must account for items beyond page one, not
      just the visible 20
- [ ] A reminder rail that shows "overdue items" uses a full query for the "overdue
      count" badge (not just the visible page) and explains pagination to the user
      (e.g. "15 more overdue" if not shown)
- [ ] A summary card that groups drive-capacity usage, vaccination completion by
      vaccine, or adherence buckets by status must sum across ALL matching records,
      not only the displayed page. If the display is paginated, the totals are
      computed server-side over the full result set and returned in the contract
- [ ] Tests cover pagination boundary cases: a summary with counts beyond page one,
      a metric that changes when page size changes (HIGH finding), and truncated
      display with correct total in the footer

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
