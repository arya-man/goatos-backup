# Vaccination Seed Source Date Contract

This contract applies to every Goat OS vaccination seed or reseed: local, dev,
staging, production rehearsal, and production migration.

The identity-quality and clean-slate migration rules are defined in
`docs/protocol-engine/migration-and-cutover.md`; this contract adds the
vaccination-specific date/history and kernel ownership rules.

## Binding Rule

Vaccination dates present in the source sheet are base schedule anchors, not
open due work.

- A source date before or on the backend business date is a trusted historical
  anchor. In the current Goat OS schema, the seed may persist that anchor as
  accepted/completed history so future recurrence has a durable start point,
  but it must never materialize open work on or before the backend business
  date from that anchor.
- Recurring doses anchored by imported history must advance to the next due date
  strictly after the backend business date. Non-recurring rows whose due date
  would land on or before the business date are suppressed from open-work
  materialization during the seed backfill.
- A source date after the backend business date is future source intent or
  source error. Do not mark it late. Do not treat it as completed history.
  Import it only through an explicit reviewed future-schedule path, or block it
  from production seed until Preventive Care approves the meaning.
- Blank, `NA`, and `Pending` are not historical anchors. They must become
  skipped, future open work, or blocked review according to the seeder's
  documented source-value mapping. `Pending` must not backfill synthetic late
  work from a past anchor date.

`Pending` has a single writer: the vaccination kernel. The source importer must
not insert an open placeholder and then invoke generation for the same goat and
rule. Accepted completion evidence must be checked before a missing-DOB or
missing-entry-date blocker is materialized.

Missing scheduling-anchor checks are trigger-specific. `birth_age` rules need
DOB, `post_arrival` rules need entry date, and `after_previous_completion`
rules need accepted completion evidence. Do not treat an unrelated missing
field as a blocker for a rule that does not use that field.

Example with backend business date `2026-07-11`:

- `2026-06-21` in the sheet means the vaccine was already administered on
  June 21, 2026. It is done history. The next due item is calculated from
  June 21 plus the active schedule.
- `2026-07-09` in the sheet means the vaccine was already administered on
  July 9, 2026. It is done history. It must not create an overdue card on
  July 11.
- `2026-07-18` is not late on July 11. If it is a generated next due date,
  it is live due/scheduled according to the open window. If it came directly
  from a sheet cell, it needs reviewed future-date semantics before seed.

## Ownership Boundaries

- Source sheets own animal identity, location facts, and trusted vaccination
  base-anchor dates/history.
- The published `vaccination.matrix` owns vaccine timing, kid versus adult path,
  repeat intervals, compatibility gaps, defer rules, and catch-up behavior.
- The obligation kernel owns generated future obligations. Seeders must not
  hand-roll future schedules that duplicate or bypass the kernel.
- Calendar owns a bounded read of accepted completion history for the selected
  date range. Closed doses do not have to remain in the hot projection merely
  to appear as completed history. Its bounded date-marker summary must preserve
  every completed and future/open date even when goat-level rows exceed the
  event page limit. Completed-only dates use the purple history color, while
  dates containing both history and open work use the mixed state.
- `vaccination_capacity_config` owns session splitting and needs-review
  classification. Changing cap, buffer, scope, or overflow policy requires
  regenerating or re-reading future planning state against the new config.
- Passport/Vaccination/Action Center must surface old trusted dates as history
  or explicit config/review gaps even before the matching rule exists. They must
  not silently hide the old source date because configuration is incomplete.

## Constraint Model

A trusted past date is only the scheduling anchor. Open/future work must still
respect the full current rule and operations model:

- active `vaccination.matrix` scope and rule availability;
- species, breed, sex, DOB/age, stage/tag, current park and shed;
- lifecycle state, including dead, sold, missing, and active filtering;
- health defer states such as sick, ICU, and quarantine;
- pregnancy and lactation holds;
- procurement warm-up and holding-park trust rules;
- minimum dose gaps, cross-vaccine gaps, and latest safe date;
- inventory/FEFO availability and cold-chain/proof requirements;
- manager/backup ownership and operator availability;
- daily capacity, max buffer days, session split policy, and capacity exception
  classification.

If any required constraint data is missing, the seed must produce a blocker or
review/config gap. It must not convert uncertainty into a clean due schedule.

## Required Reseed Flow

For a destructive staging rebuild, also follow
`docs/runbooks/staging-vaccination-clean-slate.md`.

1. Import source base-anchor dates when they are on or before backend business
   date and persist them only as trusted anchor history, never as open work on
   or before that business date.
2. Publish or reuse the intended `vaccination.matrix` version.
3. Reconcile evidence with matching active rules into accepted completions; keep
   unmatched evidence visible as config/review gaps.
4. Run the validated vaccination generation path to materialize only strictly
   future obligations from anchor history, DOB, entry date, stage, species, and
   current constraint state.
5. Recompute process-integrity and vaccination execution projections.
6. Verify Action Center buckets, shed status, next due dates, and capacity
   session splits through backend APIs using server-owned live time.
7. Run the kernel seed/generation tests before pushing or seeding a shared
   environment.
8. Require the seed reconciliation to prove zero seed-owned Pending
   placeholders, zero duplicate active goat/rule pairs, zero active non-repeat
   work already satisfied by accepted history, zero schedulable open work on or
   before the business date, future-only repeat work, and zero normal active
   work for rules whose own trigger anchor is missing.

## Test Gates

At minimum, this contract is guarded by:

- `backend/cmd/seed-vaccination-real/main_test.go`: source dates on or before
  the business date import as trusted anchor history, while future business
  dates do not; open work materialized by the seed is strictly future-only.
- `backend/internal/vaccination/app/generation_test.go`: a recurring completion
  advances to a due date strictly after the backend business date, including
  when the preceding historical window would still be open.
- `backend/internal/calendar/adapters/postgres/repository_integration_test.go`:
  accepted historical doses remain listable and drillable without a retained
  hot projection row.
- Full kernel story suite: `go test ./backend/tests/e2e/... -run TestKernelStor -v`.

Do not push a seed change that bypasses these gates.
