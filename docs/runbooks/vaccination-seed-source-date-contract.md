# Vaccination Seed Source Date Contract

This contract applies to every Goat OS vaccination seed or reseed: local, dev,
staging, production rehearsal, and production migration.

The identity-quality and clean-slate migration rules are defined in
`docs/protocol-engine/migration-and-cutover.md`; this contract adds the
vaccination-specific date/history and kernel ownership rules.

Deployment scale and read-model topology are governed by the accepted ADR
`docs/decisions/operational-kernel-5k-50k-scale-envelope.md`. For the current
5k-to-50k envelope that ADR drops the five named screen projection tables
(`calendar_event_projections`, `process_integrity_projection_rows`,
`vaccination_shed_projection_rows`, `vaccination_execution_projection_rows`,
`vaccination_operations_projection_rows`) and serves those screens from
canonical indexed SQL through one kernel worker, with the old split-worker
projection topology recoverable via the `kernel-split-workers-v1` tag. This
contract's date/history, anchor, ownership, constraint, generation, and test
rules are orthogonal to that change and are unchanged by it; only the
projection-backed verification wording below is reconciled to the ADR. Small
indexed summaries that the ADR does not list for removal — the vaccination
eligibility rollups and Counts summaries — may still survive and keep their
recompute step.

## Why This Flow Exists

A vaccination seed has two phases: write canonical source truth, then make the
live pages able to read it. Under the accepted 5k-to-50k ADR
(`docs/decisions/operational-kernel-5k-50k-scale-envelope.md`), the shed,
execution, operations, process-integrity, and Calendar screens read canonical
obligation/batch/SOP/proof/inventory tables through bounded, indexed SQL rather
than dedicated screen projection tables — so for those screens the second phase
is no longer a projector rebuild but the canonical-read APIs returning `200`
with the seeded canonical rows. The small indexed summaries the ADR does not
retire — vaccination eligibility rollups and Counts — still have a deterministic
recompute step. If a seed writes canonical rows but never proves the
canonical-read APIs serve them (and never recomputes the surviving summaries),
the database can contain correct canonical rows while the live pages are not
demonstrably usable.

HRMS is part of the same setup, not a later cosmetic step. Vaccination work is
routed by shed manager and backup ownership; without the roster, attendance/
leave windows, timetable-backed positions, strict shed-manager mapping, and
position duties, the system cannot know who owns a drive, who covers leave, or
whether a shed is executable.

The seed is also responsible for publishing the reviewed vaccination config.
Goat/vaccination source rows without the active `vaccination.matrix`, capacity
defaults, ownership duties, recomputed surviving summaries, and canonical-read
APIs that serve the vaccination screens are not a usable Goat OS environment.

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
- Birth time is clock-time provenance only. It must never satisfy DOB evidence
  or drive vaccination scheduling. When workbook extracts expose birth time as
  `HH:MM:SS`, normalize it to `HH:MM` for JSON/source drift checks and seed
  manifests; seconds are ignored.

`Pending` has a single writer: the vaccination kernel. The source importer must
not insert an open placeholder and then invoke generation for the same goat and
rule. Accepted completion evidence must be checked before a missing-DOB or
missing-entry-date blocker is materialized.

Missing scheduling-anchor checks are trigger-specific. `birth_age` rules need
DOB, `post_arrival` rules need entry date, and `after_previous_completion`
rules need accepted completion evidence. Do not treat an unrelated missing
field as a blocker for a rule that does not use that field.

### Authoritative Per-Vaccine Anchor Order

The kernel selects an anchor independently for each vaccine. The order is
binding for seed, runtime generation, identity correction, imported-history
replay, and dynamic recomputation:

1. **Latest accepted same-vaccine administration.** This is the authoritative
   anchor for that vaccine's next dose/repeat. Where the matrix defines a
   multi-dose course, use the accepted course completion required by that rule.
2. **Trusted DOB**, only when the vaccine has no accepted administration
   history and the animal is eligible to start an age-based course.
3. **Trusted herd-entry date**, only when the vaccine has no accepted
   administration history and the applicable path is procurement/adult primary.
4. **Adult catch-up/primary at the next compatible drive** when that vaccine has
   no accepted history and neither DOB nor entry date is available. Missing
   identity dates alone are not a clinical defer reason.

This is per vaccine, not per goat. An ET+TT date cannot anchor FMD, PPR, pox,
HS, or Blue Tongue. The kernel must never reverse-engineer or infer DOB from a
field vaccination date.

Identity corrections are recomputation signals, not authority to overwrite
completion history. When DOB or entry date is added or corrected:

- obligations anchored to accepted same-vaccine history keep the same due date
  and active identity;
- the kernel must not recreate a DOB/entry primary already satisfied or
  superseded by same-vaccine history;
- only obligations that actually depended on the prior DOB/entry value (or a
  no-date catch-up placeholder) may be replaced, and only while same-vaccine
  history remains absent;
- accepted completions and superseded rows remain auditable; recomputation must
  not delete history.

The required event-driven regression is: no DOB/entry + accepted same-vaccine
administration -> completion-anchored future obligation -> canonical DOB/entry
correction -> durable outbox/recheck -> unchanged due date, unchanged active
obligation, and no duplicate primary. A separate case must prove that a vaccine
with no history may use DOB/entry, while a vaccine with none of the three source
anchors enters adult catch-up instead of `missing_due_date` defer.

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

Before relying on a seed command after migration changes, run
`make seed-migration-guard`. Any migration that changes initial setup,
vaccination/protocol/SOP/HRMS/grant tables, or app-visible projection tables must
be paired with the seed command, test/E2E, or runbook update that handles the new
schema. See `docs/runbooks/initial-seed-migration-coupling.md`.

For local/dev rehearsals, the one-command source path is:

```bash
DATABASE_URL='postgres://postgres:goatos@127.0.0.1:5433/goatos?sslmode=disable' \
GOATOS_ENV=local \
GOATOS_TENANT_ID='00000000-0000-4000-8000-000000000001' \
GOATOS_VACCINATION_SOURCE_DIR='/Users/ravi/mesha/source-material/vgoats-seed' \
GOATOS_SHED_MANAGER_MAPPING='/Users/ravi/mesha/source-material/vgoats-seed/shed-manager-mapping.jul11-vaccination.csv' \
make seed-vaccination-source-full
```

For an already-seeded database after additive migrations, do not rerun source
seed just to fill derived tables. Apply the migrations, then run:

```bash
DATABASE_URL='postgres://postgres:goatos@127.0.0.1:5433/goatos?sslmode=disable' \
GOATOS_ENV=local \
GOATOS_TENANT_ID='00000000-0000-4000-8000-000000000001' \
make seed-closeout
```

`seed-closeout` runs deterministic projectors/backfills from canonical source
truth. It must never fabricate goats, owners, completion history, protocol
facts, notifications, audit rows, or derived statuses.

That target uses the source bundle at `GOATOS_VACCINATION_SOURCE_DIR` and the
strict shed-manager mapping at `GOATOS_SHED_MANAGER_MAPPING`. The `Makefile`
defaults to `../source-material/vgoats-seed` from the repo root, but shared
runbooks and handoffs should still show the explicit source paths so Claude,
Codex, or a human operator cannot accidentally seed from a different local
folder.

The current reviewed bundle must include:

- `goats.json`
- `vaccination.json`
- `roster-name-mapping.jun26-review.csv`
- `attendance-jun-26.json`
- `timetable-goats-team-v1.json`
- `shed-manager-mapping.jul11-vaccination.csv`

1. Seed founder/builder access grants for the tenant.
2. Seed HRMS from the reviewed source bundle: roster-name mapping, Jun-26
   attendance/leave, and timetable-backed workforce positions. This is not
   optional; vaccination ownership and escalation cannot be computed without it.
3. Import source goat identity/location rows and vaccination base-anchor dates.
   On a clean DB this creates the canonical shed locations that strict
   shed-owner positions attach to; do not seed owner positions against
   non-existent sheds.
4. Seed strict shed ownership and position duties from the reviewed
   shed-manager mapping. Hard stop on missing manager, missing backup,
   unreviewed shed owner, or source coverage drift.
5. Import source base-anchor dates when they are on or before backend business
   date and persist them only as trusted anchor history, never as open work on
   or before that business date.
6. Publish or reuse the intended `vaccination.matrix` version and its seed
   config/capacity defaults.
7. Reconcile evidence with matching active rules into accepted completions; keep
   unmatched evidence visible as config/review gaps.
8. Run the validated vaccination generation path to materialize only strictly
   future obligations from anchor history, DOB, entry date, stage, species, and
   current constraint state.
9. Refresh planner statistics for the freshly bulk-loaded canonical tables
   before any heavy projector or latency gate. At minimum, the source seed must
   `ANALYZE` locations, HRMS position tables, goats, identifiers, protocol
   tables, obligations, status events, and vaccination completions after the
   canonical source transaction commits. This is not business data; it prevents
   Postgres from planning projection rebuilds with stale empty-table estimates.
10. Recompute the surviving derived summaries that the UI reads through `make
   seed-closeout`. Under the accepted 5k-to-50k ADR
   (`docs/decisions/operational-kernel-5k-50k-scale-envelope.md`) the shed,
   execution, operations, process-integrity, and Calendar screens are served
   from canonical indexed SQL, so they no longer have a screen projection table
   to rebuild — their seed-green criterion moves to the canonical-read API check
   in step 11. What `seed-closeout` still owns is the small indexed summaries the
   ADR does not retire. The seed is not green until these deterministic recompute
   entry points have completed for the target tenant:
   - `vaccination-eligibility-rollup-recompute`
   - counts projectors when their owning command exists and the surface is
     visible/configured.

   Do not rely on a recompute command's defaults for multi-output commands. If a
   surviving summary command exposes a default-false `-project-*` flag for an
   app-visible read model, `seed-closeout` must pass the flag explicitly as
   `true` on that command's invocation, and `seed-migration-guard` must prove
   that through `tools/dev/seed-closeout.sh --dry-run` output; otherwise a seed
   can log that the command ran while leaving that summary empty.

   Where a pre-cutover checkout still contains the retired screen projectors
   (`process-integrity-projection-recompute`,
   `vaccination-shed-projection-recompute`,
   `vaccination-execution-projection-recompute`,
   `vaccination-operations-projection-recompute`, and the
   `calendar-vaccination-projector` upcoming/history passes), they may still be
   run for that checkout, but they are no longer the seed-green criterion for
   those screens; the canonical-read APIs in step 11 are. After the cutover in
   the ADR they are removed from `seed-closeout` entirely.
11. Verify Action Center buckets, shed status, execution rows, operations rows,
   next due dates, and capacity session splits through backend APIs using
   server-owned live time. Seed-green for these screens is the canonical-read
   APIs returning `200` with the seeded canonical rows: `/vaccination/sheds`,
   `/vaccination/execution`, and `/vaccination/operations` must serve from
   canonical indexed SQL, not report a missing/empty read model. A seed where any
   of those canonical-read APIs does not return the seeded rows is failed even if
   the canonical source tables contain rows.
12. Run the kernel seed/generation tests before pushing or seeding a shared
   environment.
13. Require the seed reconciliation to prove zero seed-owned Pending
   placeholders, zero duplicate active goat/rule pairs, zero active non-repeat
   work already satisfied by accepted history, zero schedulable open work on or
   before the business date, future-only repeat work, and zero normal active
   work for rules whose own trigger anchor is missing.

## Migration And Existing Data Rule

During development, existing seeded databases will receive new migrations. The
answer is not to invent seed rows for every new table.

- Source/canonical table changed: update the source importer and strict
  preflight, then rerun the relevant seed/import path from reviewed source.
- Derived/read-model table added or changed: at the 5k-to-50k envelope the
  default is a canonical indexed-SQL read with NO projector (see the paragraph
  below and `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`). Add an
  idempotent recompute/backfill and register it in `tools/dev/seed-closeout.sh`
  only for a summary that survives the envelope (for example
  `vaccination_eligibility_rollups`, Counts) or for a new projection introduced
  per measured hot read via the ADR scale-out ladder.
- Static catalog/config table added: migration may insert reviewed global rows;
  environment-specific values must come from source-backed seed/config.
- Operational/audit/event table added: leave it empty unless real runtime events
  or a deterministic replay fill it.

At the 5k-to-50k envelope the default for a new operator screen is a canonical
indexed-SQL read, not a new screen projection table
(`docs/decisions/operational-kernel-5k-50k-scale-envelope.md`). A dedicated
projection is added only for a specific measured hot read that canonical SQL
cannot serve within its latency/DB-pressure target, following that ADR's
scale-out ladder. When a projection is genuinely warranted, it must still ship
with its read-path indexes, freshness/version state, and an explicit
partitioning decision. Partition only when the data shape needs it, such as
append-only/time-windowed high-volume data. A small indexed summary table (the
surviving eligibility rollups and Counts) should stay a normal table with the
access pattern documented.

## Local Database Rule

Normal local laptop runtime has one Goat OS app database. API, admin-web, and
Android/mobile dev flows must resolve the same `DATABASE_URL`; they must not
silently split between host Postgres, a Docker Postgres, and an old kernel
database. The normal fallback is `postgres://postgres:goatos@127.0.0.1:5433/goatos?sslmode=disable`,
or the single detected `goatos-local-current` Docker database.

If more than one Goat OS app Postgres container is running, normal local
launchers must fail and ask the operator to stop the extra container or provide
an explicit `DATABASE_URL`. E2E/proof/load scripts must not silently use the
normal app DB or an old hidden DB. They must fail closed unless
`GOATOS_E2E_DATABASE_URL` or `DATABASE_URL` is explicitly passed. Read-only
checks may target the normal local app DB, and the `5433` URL must appear
explicitly in the command/evidence when they do. Mutating proof/load scripts
that create goats/proofs, replay outbox, insert history, or run migrations must
always refuse `5433`; there is no override for mutating the normal app DB from
E2E. Destructive/load tests must use an isolated DB with its own seed/cleanup,
such as the explicit GCP-kernel parity stack on `55432`. Separate E2E/load-test
databases must never become the default DB for laptop API/admin-web/mobile
runtime.

For E2E/proof/load scripts, choose the DB mode explicitly:

```bash
# Read-only check against the normal seeded local app DB.
DATABASE_URL='postgres://postgres:goatos@127.0.0.1:5433/goatos?sslmode=disable'
psql "$DATABASE_URL" -c 'select count(*) from goats'

# Mutating proof/load run: isolated production-shaped kernel stack only.
# Start it first, then target its DB.
make dev-local-kernel-up
GOATOS_E2E_DATABASE_URL='postgres://postgres:goatos@127.0.0.1:55432/goatos?sslmode=disable' \
tools/dev/high-scale-kernel-e2e-all.sh
```

The proof/load scripts source `tools/dev/e2e-db-env.sh` and fail if neither
`GOATOS_E2E_DATABASE_URL` nor `DATABASE_URL` is present. Mutating scripts also
fail on `5433` with no override. The port must come from the chosen runtime:
`5433` only for read-only checks against the single normal local app DB, or
`55432` for mutating proof/load work against the explicit local GCP-kernel
parity stack.

## Test Gates

At minimum, this contract is guarded by:

- `backend/cmd/seed-vaccination-real/main_test.go`: source dates on or before
  the business date import as trusted anchor history, while future business
  dates do not; open work materialized by the seed is strictly future-only.
- `backend/internal/vaccination/app/generation_test.go`: a recurring completion
  advances to a due date strictly after the backend business date, including
  when the preceding historical window would still be open; a later DOB/entry
  correction cannot replace or duplicate a same-vaccine completion-anchored
  obligation.
- Event-driven identity-recompute E2E: the canonical identity command emits its
  durable event and the registered vaccination recheck handler preserves a
  same-vaccine completion-anchored due date. A separate no-history fixture proves
  that DOB/entry may re-anchor only the fallback path.
- `backend/internal/calendar/adapters/postgres/repository_integration_test.go`:
  accepted historical doses remain listable and drillable without a retained
  hot projection row.
- Full kernel story suite: `go test ./backend/tests/e2e/... -run TestKernelStor -v`.

Do not push a seed change that bypasses these gates.
