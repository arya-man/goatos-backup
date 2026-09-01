# 2026-09-01 Vaccination Cleanup Progress

This note is the handoff/progress ledger for Codex, Claude, or a human operator
continuing the September 2026 vaccination cleanup. Follow the rules here before
changing STG data, running the sweeper, merging, or deploying.

## Current Binding Rules

- ET+TT: goat + sheep, killed bacterial/toxoid, dose 1 at 4w, booster at 7w,
  then every 6 months after course completion.
- PPR: goat + sheep, live viral, first dose at 16w, then every 3 years.
- FMD: goat + sheep, killed viral, first dose at 12w, then every 9 months.
- HS: goat + sheep, killed bacterial, first dose at 12w, then every 1 year.
- Goat Pox: goat only, live viral, first dose at 16w, then every 1 year.
- Sheep Pox: sheep only, live viral, first dose at 16w, then every 1 year.
- Blue Tongue: sheep only, killed viral, dose 1 at 16w, booster at 19w
  (21 days after dose 1), then every 1 year.
- Z1+Z3: goat + sheep, killed bacterial/toxoid, dose 1 at 4w, booster at 7w,
  then every 6 months after course completion.

## Operational Anchors Already Set Or Intended

- Sheep Pox, Coimbatore adult sheep: 2026-09-04.
- Blue Tongue, Coimbatore adult sheep: 2026-09-04.
- PPR/FMD/HS selected eligible cohort: 2026-09-08.
- Blue Tongue Coimbatore sheep follow-up cohort: 2026-09-22.
- Z1+Z3 all eligible goat + sheep, all kids and adults: 2026-10-15.

Anchor means base date. It is optional and used only when old/base history is
missing, unreliable, or intentionally reset. Once set, future boosters and
revaccinations must chain from the anchor or accepted completion. Open rows
before the anchor for that anchored scope must be canceled/suppressed; same-day
anchor rows must stay.

## Must Not Regress

- Never schedule an obligation in the past from a sweeper run.
- No active duplicate same animal + vaccine + due date rows.
- No assignment/card may mix unrelated vaccine lanes such as Z1+Z3 and ET+TT.
- No wrong species rows.
- No underage rows.
- No active Sep 1 vaccination rows.
- No operatorless drive cards.
- Operators must stay in their own park.
- Z1+Z3 and ET+TT must stay separate.
- Deferred/sick/ICU animals must not appear in active scan cards, and must
  return to vaccination scheduling when healthy.
- Operator packing cap is 200 distinct animals per operator/day.
- Operator/admin cap config must reject values above 200; do not accept a
  100000-style cap and rely on downstream cleanup.
- Keep whole sheds/partitions together; avoid splitting sibling parent
  partitions such as Mandela 1 parts where the cap allows.

## Current Code Fixes In Progress

- BT continuation window is 16w through 19w, not 20w.
- Stale open obligations that no longer match schedule path must be canceled by
  the generator instead of silently skipped.
- Stale booster/follow-up rows without accepted prior dose history must be
  canceled with `vaccine_primary_course_previous_dose_missing`.
- Drive-date override/replan must sync obligation due dates from assignment
  planned dates so UI cards and obligation rows do not disagree.
- Direct future `/vaccination/anchors` creates are rejected; future operational
  anchors must be configured in the vaccination plan draft and applied on
  publish.
- Generation must read active `vaccination_anchor_events`, including
  vaccine-level anchors where `dose_code IS NULL`. A `NULL` anchor dose code
  means "all doses for this vaccine family", not an unknown dose.
- Config anchors schedule real base work only (`birth_age`, `post_arrival`,
  `calendar`, `manual_campaign`). They must not create revac/follow-up rows
  without accepted completion history.
- Drive assignment identity must include the normalized vaccine-rule lane, so
  same operator/date/shed/partition work for different vaccine lanes does not
  collapse into one mixed card.
- Sweeper drive planning must fail closed rather than emit active no-operator
  vaccination cards. If no valid operator/cap exists, fix operator config or
  move/reshape the date; do not create `operator_id IS NULL` work.

## Verification Before Main/STG

1. Run focused Go tests for seed, vaccination app, vaccination HTTP, and
   obligation postgres adapters.
2. Run STG/OCI audit queries for duplicates, wrong species, underage, Sep 1
   rows, pre-anchor rows, unassigned active cards, and ET+TT/Z1+Z3 mixing.
3. Do not repeatedly dump all STG data into OCI. If STG has 1 million rows, do
   not copy 1 million rows for every retry. Use targeted delta repair, a scoped
   cohort, or cleanup of only the rows polluted by the failed test. Full
   STG-to-OCI refresh requires explicit maintainer approval using the words
   "full refresh" after being told the size and overwrite impact.
4. Run the sweeper/generator on the OCI clone first, then rerun the same audit.
5. Only after OCI is clean, push, merge to main, deploy STG, clean STG data if
   needed, and verify in Chrome.

## Current Verification Status

- 2026-09-01 size check: STG `goatos` is about 2.9 GB. OCI already contains
  multiple large DBs (`goatos` about 2.3 GB plus historical validation clones
  around 2.1 GB, 2.0 GB, 799 MB, and several 678 MB DBs). This is why full
  refresh loops are slow and expensive.
- 2026-09-01 targeted OCI validation rule: use
  `GOATOS_PGTEST_ADMIN_DSN=postgres://...@127.0.0.1:15432/postgres` so tests
  create migrated throwaway OCI databases without copying STG data. Do not run
  destructive proof/load scripts against the normal OCI `goatos` database.
- 2026-09-01 focused OCI validation passed against migrated throwaway OCI DBs:
  `go test -count=1 ./internal/obligation/adapters/postgres
  ./internal/vaccination/adapters/postgres ./migrations/postgres`.
- 2026-09-01 STG read-only safety audit passed: zero active Sep 1 rows, zero
  pre-Sep8 PPR/FMD/HS rows, zero pre-Oct15 Z1+Z3 rows, zero duplicate active
  animal/vaccine/date rows, zero wrong-species rows, and zero underage rows.
- Focused backend tests passed on 2026-09-01 after the BT 19w, stale-row
  cancellation, direct-anchor guard, Z1+Z3 dose-code migration, and drive-date
  sync changes.
- STG safety audit after cleanup showed zero active Sep 1 rows, zero pre-Sep8
  PPR/FMD/HS rows, zero pre-Oct15 Z1+Z3 rows, zero duplicate active
  animal/vaccine/date rows, zero wrong-species rows, and zero underage rows.
- OCI was refreshed from STG on 2026-09-01 and
  `bash tools/dev/check-oci-stg-db-parity.sh` passed.
- A generator replay on the OCI clone then recreated 192 pre-Oct15 Z1+Z3 adult
  rows, proving the generator did not use event-table vaccine-level anchors as a
  pre-generation blocker.
- Local fix now makes generation read active `vaccination_anchor_events`,
  normalizing vaccine code spelling (`Z1_Z3`, `z1_z3`, `Z1+Z3`) and treating
  `dose_code IS NULL` as a vaccine-level/all-dose anchor. Keep this covered by
  `TestManualVaccineAnchorsForGoatReadsFutureVaccineLevelAnchorEvent` and
  `TestManualVaccineAnchorsForGoatNormalizesVaccineCode` before push.
- 2026-09-01 later OCI targeted replay found two remaining code/data hazards:
  generation was still scanning procurement-excluded animals, and legacy/generic
  manual campaign aliases (`PPR`, `FMD`, `HS`) could leak into normal sweeper
  work before the Sep 8 anchor. Local fixes now exclude procurement-blocked
  animals from generation reads, cancel stale open work for exited or
  procurement-excluded animals including `in_progress`, and guard normal
  sweeper generation so generic manual aliases do not materialize as ordinary
  work.
- 2026-09-01 OCI cleanup canceled only the polluted active rows from validation:
  102 Sep 1/pre-anchor rows first, then 18 recreated generic PPR/FMD/HS rows
  after the targeted replay exposed the leak. Final OCI safety audit after the
  fix and another 60-animal targeted generation replay: zero active Sep 1 rows,
  zero pre-Sep8 PPR/FMD/HS rows, zero pre-Oct15 Z1+Z3 rows, zero duplicate
  active animal/vaccine/date rows, zero wrong-species rows, zero missed/overdue
  September rows, and zero active procurement-excluded vaccination rows.
