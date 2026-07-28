# Aggregate and Projection Correctness

Load this reference for any read model, projection, dashboard/card summary,
calendar grouping, reminder rail, or SQL/Go query that combines `JOIN` with
`COUNT`, `SUM`, `GROUP BY`, JSON aggregation, or pagination.

Also load `docs/architecture/operational-read-model-contract.md` whenever the
aggregate feeds Calendar, Control Tower, Action Center, Protocol Adherence,
Workflows, admin-web detail pages, Android execution/proof screens, reporting,
or a new vertical/module. Aggregates are not approved until their grain and
surface contract are explicit.

These changes are not approved because the arithmetic invariant holds or the
query compiles. The reviewer must prove the population, identity, cardinality,
and page boundary independently.

## Required proof before approval

0. **Operational read contract** — name the canonical write owner, row grain,
   summary grain, scope identity, time grain, status bucket semantics, and every
   consuming surface. Backend structs, OpenAPI, generated TS, Android DTOs,
   admin-web, and Android must move together. Summary totals are whole-result
   aggregates unless named `page_*`. See
   `docs/architecture/operational-read-model-contract.md`.

1. **Canonical membership** — name the source that decides which facts belong
   to the displayed group. Build both the event/card and its summary from that
   membership (for example, a drive/batch membership id). Do not reconstruct
   membership later from coincidentally-equal location/date fields.
   **An aggregate plan row is not per-goat membership.** When binding a fact
   back to a plan/assignment row, state the row's uniqueness grain and
   reproduce every dimension of it in the predicate.
   `vaccination_drive_assignments` is unique at batch + planned date + park +
   shed + physical shed + `partition_label` + operator, and carries
   `vaccine_rule_ids`; a lookup keyed on `tenant_id + batch_id + shed_id` with
   `LIMIT 1` is deterministically wrong for mixed-vaccine or partitioned
   batches. Reference implementation (Passport / Calendar list):
   `cardinality(assignment.vaccine_rule_ids) = 0 OR oi.rule_id = ANY(...)` at
   `backend/internal/obligation/adapters/postgres/sqlc/query.sql:48`. If the
   producer can emit two rows the reader cannot tell apart (a partition split
   across operators), `ORDER BY ... LIMIT 1` fabricates an answer; demand an
   exact membership source instead.
   **The same defect also lives in the `GROUP BY`.** A complete `WHERE` plus a
   collapsed aggregation key is one clause over, not compliant: in BUG-027 the
   split cohort deliberately spans several `planned_date` values, the numerator
   `SUM(a.animal_count)` respects that, and the denominator
   `GROUP BY a.operator_id` collapses capacity to one operator-day
   (`backend/internal/processintegrity/adapters/postgres/repository.go:920` vs
   `:938-948`), so two days of load are checked against one day of cap. Demand
   the written proof, not the principle — the rule below has existed throughout
   and five instances shipped regardless, so this is adherence, not coverage:
   (a) producer unique columns and consumer match/group columns side by side;
   (b) row multiplicity of each joined side; (c) for any ratio or cap check, the
   key set each of numerator and denominator ranges over, shown identical. If
   the author cannot write those three lines, do not approve. Both sub-shapes
   and all five sites: `docs/decisions/scale-anti-patterns.md` -> "Read-model
   grain is not the grain the consumer assumes".
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
   Vaccination shed proof has a hard write-path version of this rule: the hidden
   park/batch `sop_tasks` row is not the submitted/proof/verification grain for
   WF, CT, AC, Calendar, Android, verifier queues, proof drawers, or leadership
   sidebars. A shed-level submit may receive scan items collected from the shared
   parent task, but before inserting `sop_submission_items` it must filter by the
   completed proof's shed `subject_id` and the live goat `shed_id`. Do not approve
   a display/status fix unless the write path and the read projection both prove
   this shed grain.
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
- `...ShedProofFiltersOverBroadScanItems...` or `...SiblingShed...`: one shared
  vaccination parent task, two sheds, one completed shed-level video proof, and
  over-broad scan items containing both sheds. The proof shed must get items and
  completions; the sibling shed must get zero.

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
  than the first page. For anything reading `vaccination_drive_assignments`
  this specifically means: one vaccine in the batch, one partition in the shed,
  or one date in the cohort proves nothing. The single-date fixture is what let
  the `GROUP BY` sub-shape through a review that had already learned the
  predicate sub-shape.
- The predicate was fixed, so the aggregate is assumed fixed. Check the
  `GROUP BY` and the compared-against key set separately.
- A sibling surface (Passport, Calendar list) is correct, so the new surface is
  assumed correct — they are separate hand-copied predicates until a parity
  test proves one shared source.
- The test asserts a non-null DTO but not the exact membership and counts.
- A frontend fallback makes a missing backend summary look acceptable.
- A diff-scoped guard reports no files because the relevant work is unstaged or
  untracked.
- Unit tests pass while projection refresh work is repeated once per page; ask
  for query-count plus latency/EXPLAIN evidence.

Approval requires the source marker, the applicable adversarial tests, and the
reviewer's independent verification of all claims.
