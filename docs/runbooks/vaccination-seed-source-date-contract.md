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

HRMS role normalization is mandatory. CEO/CXO/full-access people use the
`ceo_internal` grant role and `cxo` workforce hint. Do not create a separate
admin person role when seeding local, staging, or developer fixtures.

The seed is also responsible for publishing the reviewed vaccination config.
Goat/vaccination source rows without the active `vaccination.matrix`, capacity
defaults, ownership duties, recomputed surviving summaries, and canonical-read
APIs that serve the vaccination screens are not a usable Goat OS environment.

Capacity defaults are operator animal capacity defaults, not dose limits. The
seeded default is the number of unique animals one available operator can handle
on one business date; one animal due for multiple vaccines consumes one slot.
Drive assignment rows are generated from this default plus timetable/leave
availability. When the safe buffer would be missed, the generated work is marked
over-cap required so the drive is finished by available staff instead of being
quietly pushed beyond the latest-safe date.
Assignment rows are operator/date/shed/partition metadata and never replace
obligation membership; read models must collapse them before counting animals,
proofs, completion, due, or overdue buckets.

Source shed labels with trailing partition numbers must be normalized before
canonical DB writes. `Gandhi 1` means physical shed `Gandhi`, partition `1`;
`Godel 1 - Part 3` means physical shed `Godel 1`, partition `Part 3`. The raw
partition label may appear in source/audit output, but active goat placement and
goat obligation scope must point to the physical shed.
Fixture validation, source validation, and shed-owner checks all aggregate by
that physical shed. Partition rows may split operator drive work, but they must
not create separate buildings, duplicate owner coverage requirements, duplicate
animal counts, or independent read-model totals.

CPT-only operator-drive rehearsal data is valid when the source center is CPT
only and the reviewed roster contains exactly the three vaccination
operators/managers required for that seed. Do not synthesize Coimbatore/CBE
owners just because the full production fixture also covers CBE.
The three CPT vaccination operators are Amit, Darshan, and Sagar as
manager-tier vaccination operators. Do not infer "support", park-head, or backup
ownership from their old HRMS seat labels in this rehearsal. Their recurring
week-offs come from the CPT timetable: Amit = Friday, Darshan = Sunday,
Sagar = Saturday.
For source-backed rehearsals, strict shed-position validation is scoped to
active sheds that contain live goats from the selected source. Empty baseline
catalog sheds created by migrations are not vaccination drive truth and must not
force unrelated owners/operators into the run.

The committed CPT operator-drive rehearsal packet lives at
`fixtures/vaccination-cpt-operator-drive-2026-07-23/`. Its source start date is
`2026-07-23`; any fresh local/dev/staging rehearsal seed from that packet must
generate open drive work only on `2026-07-23` or later. The packet includes
`cpt-operator-roster.json`, which explicitly seeds Amit Kumar, Darshan Talwar,
and Sagar Mahoor as equal vaccination operators at `200` unique animals per
operator per day, plus Chandrakant as director-only monitoring scope. Its
`default_operator_assignment` is scheduler-consumed: N=1, Darshan is the default
drive operator, Sagar is the primary fallback when Darshan is unavailable or on
weekly off, and Amit remains in HRMS as a secondary fallback rather than being
removed from the roster. It also reuses the founder/CXO `ceo_internal` grant cohort documented in
`docs/runbooks/auth.md`: `ravi@mesha.sg`, `manohark@mesha.sg`,
`manju@mesha.sg`, `abhishek@mesha.sg`, and `aryaman@mesha.sg`.
The packet also includes `expected-drive-schedules.json`, a machine-readable
post-seed validation sample. For the discussed 2026-07-24 drive, the expected
final input is one active operator/day, default Darshan, 200 animals/day,
ET+TT-only on `2026-07-24`, and PPR moved to `2026-08-07`. That produces ET+TT
rows of 200 animals on `2026-07-24` and 124 animals on `2026-07-25`, plus PPR
rows of 200 animals on `2026-08-07` and 124 animals on `2026-08-08`. Darshan is
available Friday/Saturday and off Sunday; dose counts must not inflate animal
capacity. The roster's `weekly_capacity_examples.total_capacity_animals` is raw
HRMS availability math (`available_operators.length * 200`, so 2 available
operators = 400 and 3 available operators = 600). It is not the final drive
assignment cap; with `active_operators_per_day=1`, the final scheduled drive cap
is 200 unique animals/day.

For local DB reseed proof, the packet's exhaustive contract is
`fixtures/vaccination-cpt-operator-drive-2026-07-23/LOCAL_DB_RESEED_VALIDATION.md`.
Follow it before staging. It explicitly rejects the failure shapes observed in
the 2026-07-24 validation attempt: stale checkout validation, throwaway
two-operator roster overrides, Amit missing cap/shift config, Sagar week-off
changed to Monday, PPR or other vaccine families mixed into the final
`2026-07-24` ET+TT-only drive, planned operator/date rows over 200 distinct
animals, and using post-cascade superseded empty batches as clean seed proof.

Animal placement is seed truth too. In this build phase, source extracts can be
incomplete, so seed/import must deterministically complete missing goat placement
into an explicit seed-intake park/shed instead of leaving a live animal without a
shed. Do not invent vaccination history to make schedules look clean, but do
fill missing placement facts needed for the system to operate. Every live goat
must end seed/closeout with `park_id`, `shed_id`, and `current_location_id`
pointing at the real active shed. Every open goat vaccination obligation must be
`scope_type='shed'` and `scope_id = goats.shed_id`; park and tenant scopes are
drive grouping/policy scopes, not goat-obligation fallbacks. `seed-closeout`
runs `tools/dev/check-goat-shed-integrity.sh` to prove this after seed,
generation, and sweeper.

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

Seed import must not carry a private kid/adult classifier. The published
`vaccination.matrix` procurement policy and the live vaccination scheduler own
that decision. In particular, `origin_type=birth` and `K1`/`K2` stage tags are
not enough to force a kid-course mapping when trusted DOB/age proves the goat is
past the configured kid finish window. A raw source vaccination cell also cannot
prove its own kid/adult path: do not pre-map the same sheet row as `_kid_`
history and feed it back into classification. Classify first from independent
DOB, stage, entry-date, and already-accepted history evidence. Then keep the
source vaccination date as history under the selected rule family and let kernel
generation create only future work from that history.

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

Multi-dose course history is not the same as a completed repeat anchor. If seed
imports an accepted ET+TT dose 1, GoatOS must schedule ET+TT dose 2 from that
source date plus 21 days for both kid and adult courses. The 182-day ET+TT
repeat starts only after accepted ET+TT dose 2/course completion. Blue Tongue
kid dose 2 stays 28 days after Blue Tongue kid dose 1. Single-dose vaccines
such as FMD, HS, PPR, Goat Pox, and Sheep Pox repeat from their accepted
same-vaccine administration because they have no second course dose.

Seed closeout must prove that adult ET+TT dose 2 exists. A DB with accepted
`et_tt_adult_w1` completions and no same-goat `et_tt_adult_w2` obligation or
completion is broken and must be reset/reseeded before any drive table is
reported as final.

### Sanitized mock-fixture exception

The runtime/kernel rule above remains strict: it never derives DOB from a
vaccination and production ingestion returns contradictions to the data owner.
The committed synthetic local/dev fixture has a narrower, reviewed repair rule
because its vaccination dates are the test truth the fixture exists to exercise.
During `tools/dev/build-vaccination-hrms-fixture.mjs` only:

- every vaccination cell is immutable, including dated, `Pending`, `NA`, blank,
  and `-` values;
- vaccine-specific species evidence repairs mock breed/species metadata, while
  an animal with both goat-only and sheep-only vaccine history is rejected;
- an existing mock DOB that makes purchase, vaccination minimum age, delivery,
  abortion, health, weight, or lifecycle history impossible is moved earlier to
  the earliest required bound; a missing DOB remains null;
- valid K1/K2 distinctions remain, but a trusted DOB past the configured
  20-completed-week finish cutoff persists as Adult;
- a mock death/sale contradicted by later vaccination is removed and the animal
  is restored to Alive; the vaccination date is never changed.

Every repair is counted in the committed `corrections.json`. This deterministic
fixture preparation is not a kernel scheduling anchor and is not an authority
to rewrite production identity or lifecycle history. The complete source-intake
workflow is `docs/runbooks/source-seed-data-validation.md`.

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
  June 21 plus the active schedule; past missed cards before July 11 are not
  materialized by the seed.
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
- Health case-log source vocabulary is normalized before scheduling decisions:
  `Open -> sick`, `Extended -> under_treatment`, `Closed -> healthy`, and
  `Fine -> healthy`. `Closed` means resolved history and `Fine` means explicit
  healthy status; neither may defer an otherwise eligible animal.
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
- health defer states such as sick, ICU, quarantine, and explicit
  under-treatment states; resolved `Closed` and explicit `Fine` source cases
  are not health defers;
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
make seed-vaccination-source-full
```

After seeding this CPT rehearsal packet, validate the resulting DB schedule
against
`fixtures/vaccination-cpt-operator-drive-2026-07-23/expected-drive-schedules.json`
before accepting the seed as correct.

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
defaults both to the committed `fixtures/vaccination-hrms-source-full` bundle.
The selected directory itself is audited as the first recipe step, before email
grants, HRMS, or vaccination can write. A private `GOATOS_VACCINATION_SOURCE_DIR`
override is therefore expected to fail until it has been reviewed and converted
to the sanitized fixture; never bypass that failure by invoking a seed binary
directly.

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

   The retired screen projector commands are not a seed-green criterion and
   must not be run to make dashboard pages look healthy. The canonical-read
   APIs in step 11 are the proof.
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

## Protocol schedule metadata

Every seeded vaccination matrix schedule row must publish
`route_site=subcutaneous`. This is protocol/config metadata used by schedule and
option read paths. It is not an operator-entered SOP answer, and it must not
reintroduce the retired vaccination form field.

## False-DOB disposition (`-null-false-dob`)

`cmd/seed-vaccination-real` validates `dob <= entry_date` for every source
animal, where `entry_date` resolves `purchase_date -> stage_entry_date` for
purchased/imported animals, and `stage_entry_date` ONLY for birth-origin
animals (a birth-origin row's `purchase_date` is the DAM's purchase, not the
kid's — see the incident below). A purchased/imported-origin animal that still
fails this gate carries a provably-false DOB, most often a bulk-fill error in
the upstream sheet rather than a genuine data defect. The `-null-false-dob`
flag (default `false`) lets the seed null that animal's DOB in-memory before
insert instead of hard-failing the whole run: the animal is counted
(`dob_nulled=<n>` on stdout; gate the exact count with
`-expect-null-false-dob=<n>`), one entry is appended to a per-run audit
sidecar `<source-dir>/seed-dob-disposition-<YYYY-MM-DD>.json` (RFID, origin
type, original DOB, resolved entry date + its source column, delta days,
disposition, run timestamp), and the goat is inserted with `dob = NULL`. No
substitute date (purchase-minus-365 or similar) is ever written — nulling to
unknown is the only accepted disposition. Source files
(`goats.json`/`vaccination.json`) are never modified. Because
`upsertSeedGoats` is a full-refresh upsert (`dob=EXCLUDED.dob` on every
reseed), a future rerun automatically restores the real DOB the moment the
upstream source is corrected — no manual cleanup is required on this side.
The separate local trigger seed may also create the reviewed synthetic RFID
`CBE-RFID-0001` for emulator scan E2E; it is fixture-only and never carries
private source-date authority.
Birth-origin animals are NEVER eligible for this flag: a birth-origin
DOB-after-own-`stage_entry_date` violation stays a hard failure regardless of
`-null-false-dob`, because it is a genuine data defect rather than the
purchased/imported bulk-fill pattern.

### 2026-07-19 false-DOB incident

The `dob <= entry_date` gate (added in commit `c8df4e57`) failed 45 of 1311
source animal rows on first run against
`/Users/ravi/mesha/source-material/vgoats-seed`. Tracing the lineage
(`goats.json` -> `Demo DB.xlsx` -> the `goatos-sheets` BigQuery export) showed
all three carry the identical corrupted values — there is no truer upstream
source to recover a correct DOB from for the affected rows.

Cluster breakdown of the 45:

- **28x identical bulk-fill pair** — DOB `2025-06-12` against purchase date
  `2025-05-24`, repeated verbatim across 28 distinct animals. Consistent with a
  spreadsheet fill-down/copy error rather than 28 independently mistyped
  dates.
- **6x ~1-year shift** — DOB one calendar year after the true purchase window
  (year-digit transposition pattern).
- **7x 3-day reversal** — DOB 2-4 days after purchase date, consistent with a
  swapped or off-by-a-few-days manual entry.
- All 41 of the above are **adult purchased** animals per independent evidence
  (shed tags, purchase weights, and the `vaccination.json` age column all
  agree with "adult," not "newborn/kid") — the false DOB is a data-entry
  defect, not a genuine birth-timing anomaly.
- **3x birth-origin K2 kids** whose "violation" was not a data defect at all:
  the pre-fix validator compared the kid's own DOB against its **dam's**
  `purchase_date` (the only `purchase_date` present on that source row,
  because a birth-origin animal was never itself purchased). Fixed in code —
  `buildEntryDateMapping` now resolves a birth-origin animal's entry date from
  its OWN `stage_entry_date` only, and never consults `purchase_date` for
  birth-origin rows.

Judge-ruled disposition:

- **Purchased false DOB -> NULL** via `-null-false-dob` (the 42 adult
  purchased rows across the 3 clusters above). These animals are
  history-anchored: `vaccination.json` carries real accepted vaccine
  administrations for all of them, so scheduling continues from that trusted
  history and the DOB loss has minimal scheduling impact. Originals are
  preserved in the sidecar `seed-dob-disposition-<YYYY-MM-DD>.json`, never
  discarded.
- **Birth-origin kids keep their DOB** — no nulling; the validator fix alone
  resolves all 3.
- Total 45 failures at commit `c8df4e57` = 42 nulled (purchased) + 3 fixed
  by the birth-origin validator correction (0 remaining failures).

Remaining maintainer debt: the root cause lives upstream in `Demo DB.xlsx`
(and its `goatos-sheets` BigQuery mirror), which still carries the corrupted
DOB values for these 42 animals. Fixing the origin file is out of scope for
this seed change; the sidecar file is the worklist for that follow-up. Until
that upstream fix lands and this seed is rerun, the pre-existing local
`:5433` dev database is tainted for these 42 animals (it may hold rows seeded
before this fix, with the false DOB still present) and should be treated as
stale for this data until reseeded with `-null-false-dob`.

## Test Gates

At minimum, this contract is guarded by:

- Fixture manifest proof contract: the committed source bundle declares
  `proof_mode=shed_level_video`, shed proof subject, one required shed video
  with a maximum of five, and camera + gallery capture sources. This does not
  change source vaccination dates; per-goat scan timestamps remain the
  administration-time truth and must be persisted/displayed for scanned animals.
- `backend/cmd/seed-vaccination-real/main_test.go`: source dates on or before
  the business date import as trusted anchor history, while future business
  dates do not; open work materialized by the seed is strictly future-only.
  The same file covers the false-DOB disposition above: birth-origin entry
  dates never resolve from a dam's `purchase_date`; a purchased/imported false
  DOB hard-fails without `-null-false-dob` and is nulled + counted (never
  replaced with a substitute date) with the flag; a birth-origin DOB defect
  hard-fails even with the flag; and the audit sidecar preserves the original
  DOB verbatim.
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

<!-- Coupling review 2026-07-20: the counts (approval, department_module_grants) and feed_direction migrations 000009-000015 plus the seed-roster-real department-module-grants write were reviewed against the vaccination HRMS seed source. They are orthogonal to it (counts/feed tables, not the vaccination roster source), so no fixture/source-data change is required. Recorded in fixtures/vaccination-hrms-source-full/manifest.json -> seed_contract_coupling_reviews. -->
<!-- Coupling review 2026-07-22: adult ET+TT dose-2 post-seed invariant and shed partition name-pattern normalization do not change raw fixture bytes. They change transform/generation validation: partition-bearing shed labels normalize to physical shed + partition metadata, and accepted et_tt_adult_w1 must have same-goat et_tt_adult_w2 work before handoff. -->
<!-- Coupling review 2026-07-22: ceo_ai reporting migrations 000024-000027 create `ceo_ai.*` read-only views that query canonical vaccination/procurement/obligation/workforce tables. They do not modify the seed source contract, HRMS roster schema, vaccination protocol, or SOP configuration, so no fixture/source-data change is required. -->

<!-- Coupling review 2026-07-23: seed-roster-real gained an operator-roster overlay. When a source dir ships cpt-operator-roster.json it is the authoritative field capacity: the park's resolved seats are recast into equal per-person vaccination_operator_<name> positions (manager tier, not backup) with contract week-offs, the strict PC-manager/backup/park-head requirement is waived for that park, and seed-vaccination-source-full skips shed-manager seeding for the operator-roster park. The committed jun-26 fixture ships no such file, so its behavior is unchanged. -->
<!-- Coupling review 2026-07-23: workforce_positions.vaccination_daily_animal_cap is now the HRMS source of truth for per-operator vaccination animal capacity. Operator-roster rehearsal sources may set animal_cap_per_day; seed-roster-real validates it and writes it to HRMS positions. Runtime scheduling must read that HRMS position cap before tenant/default capacity, so changing an operator's cap changes future drive assignment splitting without changing raw vaccination dates or fixture bytes. -->
<!-- Coupling review 2026-07-23: migration 000035 introduces vaccination_operator_shift_config and vaccination_operator_assignment_config tables for operator shift scheduling (shift_label/shift_start_minute/shift_end_minute) and default assignment rules (default_operator_assignment.active_operators_per_day, default_operator_assignment.default_operator_code). These rows are consumed by the drive scheduler when selecting daily operators and do not change raw animal/vaccination source bytes; the validator and seed-roster-real accept and seed these fields when present in cpt-operator-roster.json. -->

<!-- 2026-07-23 operator-config auto-cascade: migration 000036 adds obligation_operator_config_replan_watermarks, an operational idempotency-watermark table (no seed data / no HRMS-source rows; consumer-only). No fixture bytes change. -->
