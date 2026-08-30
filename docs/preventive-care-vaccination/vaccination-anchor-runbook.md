# Vaccination Anchor Date Runbook

This runbook is for Codex, Claude, and human operators when the maintainer says
to add a vaccine drive, anchor date, campaign date, base date, baseline date, or
"start this vaccine from this date."

An anchor date is optional. Normal vaccination scheduling still comes from the
published vaccine rules, date of birth, accepted same-vaccine history, and
previous completed doses. Use an anchor only when old/base vaccination history
is missing, unreliable, or intentionally reset by operations.

## What An Anchor Means

An anchor is vaccine timeline history for a selected animal scope. It is not a
one-off manual obligation insert.

When an anchor is published:

- Treat the anchor date as the first known/base date for that vaccine or dose
  line for the selected eligible animals.
- Suppress open same-vaccine catch-up rows strictly before the anchor date.
- Preserve same-day rows on the anchor date.
- Chain boosters, follow-up doses, and revaccination from the anchor or accepted
  completion date.
- Keep age eligibility, species eligibility, vaccine compatibility, safe gaps,
  and operator-day packing rules active.

Do not create anchor behavior by editing only `vaccination_drive_assignments` or
by inserting a single open obligation row. That loses the future scheduling
semantics and the sweeper can recreate noisy rows.

## Where To Configure It

Anchor dates belong in the vaccination plan draft/version config, on the same
per-vaccine/per-rule rows where timings and repeat rules are edited.

The draft editor may show optional anchor/base date fields and preview counts.
It must not perform an immediate operational create from the draft screen. The
anchor becomes active when the vaccination plan version is published.

The published/read-only plan view should show the active anchor/base dates for
each rule. A separate emergency/data-repair admin command may exist, but it
must use the same backend validation and kernel path as the published config.

## Current Rule Snapshot

Use the canonical matrix in `docs/preventive-care-vaccination/vaccination-rules.md`.
As of this handoff, the maintainer-confirmed rules are:

| Vaccine | Species | Type/class | First timing | Follow-up/revac |
|---|---|---|---|---|
| Z1+Z3 | goat + sheep | killed bacterial/toxoid | 4 weeks | booster 7 weeks, then every 6 months |
| PPR | goat + sheep | live viral | 16 weeks | every 3 years |
| FMD | goat + sheep | killed viral | 12 weeks | every 9 months |
| HS | goat + sheep | killed bacterial | 12 weeks | every 1 year |
| Goat Pox | goat only | live viral | 16 weeks | every 1 year |
| Sheep Pox | sheep only | live viral | 16 weeks | every 1 year |
| Blue Tongue | sheep only | killed viral | 16 weeks | booster 19 weeks, then every 1 year |

Blue Tongue dose 2 is 21 days after dose 1. That means a 16-week dose 1 has a
19-week booster, not 20 weeks.

## Safety And Packing Rules

Before publishing or manually applying an anchor, preview and verify:

- Live animal scope: park, shed, partition, species, sex/stage where relevant,
  and actual RFID/tag identifiers for drilldown.
- Accepted same-vaccine history and already scheduled future obligations.
- Dose chain: whether the anchor is dose 1, booster/dose 2, or revaccination.
- Cross-vaccine safety: live/live, live/killed, killed/live, killed/killed.
- Course gaps: Z1+Z3 dose 1 to dose 2 = 21 days; Blue Tongue dose 1 to dose 2 =
  21 days.
- Maximum vaccines per animal per session from the active config.
- Operator packing: default cap is 200 animals per operator-day, counted by
  distinct animals, not vaccine rows or doses.
- Whole-shed packing: keep complete sheds first. If complete buckets total 180
  and the next whole shed would exceed 200, keep 180 and carry the next shed.
- Parent partition grouping: sibling partitions under the same parent shed, such
  as `Mandela 1 - Part 1` through `Mandela 1 - Part 8`, should stay together
  where the cap allows.
- Operator park scope: never assign an operator from one park to another park's
  drive.

## OCI/STG Proof Required Before Claiming Done

For any anchor engine change, prove it on the maintainer OCI Postgres clone
before pushing or deploying:

1. Bring OCI data to STG parity by delta repair only, unless the maintainer
   explicitly approves a full refresh.
2. Insert or configure the anchor.
3. Cancel stale pre-anchor open rows that should be suppressed.
4. Run the same sweeper/generator command path used by production.
5. Confirm no older pre-anchor rows reappear.
6. Confirm same-day anchor rows remain.
7. Confirm boosters/revacs chain after the anchor/completion date.
8. Confirm underage or species-mismatch animals are excluded.
9. Report counts by park, shed/partition, vaccine, date, age bracket, and reason.

On Ravi's laptop, use the OCI DB/tunnel path when available. Do not require
laptop Docker or Colima for these proofs.

## Session Examples To Preserve

These examples came from the August 2026 anchor cleanup session and should be
used as reference behavior for future agents.

### Z1+Z3 October 15 Anchor

Maintainer instruction: all kids and adults should start Z1+Z3 from October 15.

Meaning:

- Vaccine/program is `Z1+Z3`, not separate `Z1`, `Z2`, or `Z3` stage buckets.
- Scope is selected live goat + sheep animals, kids and adults included, subject
  to the active eligibility/config scope.
- October 15 is the anchor/base date.
- Old open Z1+Z3 rows before October 15 should not be recreated by the sweeper
  for that anchored scope.
- Z1+Z3 booster/dose 2 is due 21 days after the anchor dose 1.
- Z1+Z3 6-month revaccination starts only after course completion.

### PPR/FMD/HS September 8 Cohort Anchor

Maintainer instruction: move the relevant cohort into a September 8 anchor
instead of scattered August 31 to September 7 catch-up rows.

Meaning:

- PPR follows its 16-week rule.
- FMD follows its 12-week rule.
- HS follows its 12-week rule.
- The September 8 anchor is for animals intended to be covered by that cohort,
  including animals reaching the required rule age by September 8.
- Open PPR/FMD/HS rows before September 8 for that anchored cohort should be
  suppressed/canceled.
- Animals not eligible by the rule age should stay on their correct future DOB
  schedule, not be forced into the anchor.
- Future revaccination dates must chain from the September 8 anchor or accepted
  completion, not from the stale pre-anchor dates.

## What To Report Back

When an anchor is previewed or applied, report in plain language:

- Which vaccine/rule was anchored and on what date.
- How many eligible animals were included, split by park and shed/partition.
- How many were excluded and why: underage, wrong species, unhealthy/deferred,
  existing accepted history, or operator-cap overflow.
- What old rows were canceled, with date range and reason.
- What stayed on the anchor date.
- What moved later, with exact reason and next date.
- Next booster or revaccination date created by the anchor.

Use actual RFID/tag values for drilldown. Do not report only internal goat ids.
