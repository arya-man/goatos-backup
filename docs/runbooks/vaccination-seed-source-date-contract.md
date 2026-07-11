# Vaccination Seed Source Date Contract

This contract applies to every Goat OS vaccination seed or reseed: local, dev,
staging, production rehearsal, and production migration.

## Binding Rule

Vaccination dates present in the source sheet are administration facts, not
future due dates.

- A source date before or on the backend business date is completed vaccination
  history. If a matching published rule exists, seed it as an
  accepted/verified administration, then let the vaccination kernel compute the
  next open obligation from that administered date and the published schedule.
  If no matching rule exists yet, keep the date as visible history/config-gap
  evidence and do not fabricate a rule, obligation, or completion.
- A source date after the backend business date is future source intent or
  source error. Do not mark it late. Do not treat it as completed history.
  Import it only through an explicit reviewed future-schedule path, or block it
  from production seed until Preventive Care approves the meaning.
- Blank, `NA`, and `Pending` are not history. They must become skipped,
  pending/open work, or blocked review according to the seeder's documented
  source-value mapping.

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
  administration history.
- The published `vaccination.matrix` owns vaccine timing, kid versus adult path,
  repeat intervals, compatibility gaps, defer rules, and catch-up behavior.
- The obligation kernel owns generated future obligations. Seeders must not
  hand-roll future schedules that duplicate or bypass the kernel.
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

1. Import source administration dates as history evidence when they are on or
   before backend business date.
2. Publish or reuse the intended `vaccination.matrix` version.
3. Reconcile evidence with matching active rules into accepted completions; keep
   unmatched evidence visible as config/review gaps.
4. Run the validated vaccination generation path to materialize future
   obligations from history, DOB, entry date, stage, species, and current
   constraint state.
5. Recompute process-integrity and vaccination execution projections.
6. Verify Action Center buckets, shed status, next due dates, and capacity
   session splits through backend APIs using server-owned live time.
7. Run the kernel seed/generation tests before pushing or seeding a shared
   environment.

## Test Gates

At minimum, this contract is guarded by:

- `backend/cmd/seed-vaccination-real/main_test.go`: source dates on or before
  the business date import as completed history, while future business dates do
  not.
- Full kernel story suite: `go test ./backend/tests/e2e/... -run TestKernelStor -v`.

Do not push a seed change that bypasses these gates.
