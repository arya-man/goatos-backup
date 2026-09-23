# Vaccination Agent Rules

> Moved verbatim from `AGENTS.md` (split 2026-09-24 to keep session start small).
> These rules are as binding as `AGENTS.md` itself. Only file location changed.

## Vaccination Anchor Date Rule

When the maintainer tells Codex, Claude, or any other agent to add a vaccination
drive, anchor date, campaign date, baseline date, or "start from this date" for
one or more vaccines, treat that date as a **vaccine timeline anchor**, not as a
manual one-off obligation insert.

Read the detailed operational runbook before changing anchor code, config, or
data: `docs/preventive-care-vaccination/vaccination-anchor-runbook.md`.

For any live vaccination drive where scheduled work disappears, operators lose
rows mid-drive, completed proof/videos no longer close the drive, or the
maintainer says the kernel/sweeper destroyed vaccination rows, first read:
`docs/runbooks/vaccination-live-drive-schedule-and-restore.md`.

Start the code investigation in these areas before guessing:

- `backend/internal/kernelstages/generation.go`
- `backend/internal/vaccination/app/generation.go`
- `backend/internal/obligation/adapters/postgres/repository.go`
- `backend/internal/obligation/adapters/postgres/visit_shot_lock.go`
- `backend/internal/vaccinationexecution/adapters/postgres/repository.go`
- `backend/internal/vaccinationexecution/adapters/postgres/live_tracker_repository.go`

The known failure mode is: generation cancels/deferred vaccination obligations,
then `obligation-sweep` rebuilds `vaccination_drive_assignments` from the
remaining open obligation set and physically removes active assignment rows.
Never assume canceled assignment rows are harmless during a same-day drive if
proof artifacts, scans, completions, or operator work already exist.

Required behavior:

1. Resolve the exact vaccine/program name the maintainer used. For example,
   `Z1+Z3` is the vaccine/program label, not separate `Z1`, `Z2`, or `Z3`
   management stages.
2. Resolve the intended animal set from live herd scope: park, shed,
   partition, species, sex, current stage, and explicit RFID/tag identifiers
   where relevant. Report animal identifiers as actual RFID/tag values, not
   internal goat ids.
3. Clear, cancel, or supersede bad old obligations only when asked, and keep
   that separate from the new anchor. Old missed rows are history; do not assume
   deleting or canceling them will make the sweeper invent a new campaign.
4. Create or configure the anchor through the vaccination generation/kernel path
   so future boosters and revaccination are derived from the anchor date.
   Do not blind-insert a single drive row unless the maintainer explicitly asks
   for a one-off data repair and accepts the loss of future-rule semantics.
5. Before claiming a date is scheduled, verify same-day and cross-vaccine
   safety: live/live, live/killed, killed/live, killed/killed, maximum vaccines
   per session, booster gaps, existing future obligations, and accepted vaccine
   history. If another vaccine lands on the requested date, the backend/kernel
   must either keep a medically compatible pair or push the lower-priority /
   overflow work forward by the configured safe-gap rules.
6. Respect operator-day packing: default cap is 200 animals per operator-day,
   counted by animals, not doses. Fill with complete sheds first. For partitioned
   sheds with a common parent, such as `Mandela 1 - Part 1` through
   `Mandela 1 - Part 8`, keep sibling partitions together before mixing
   unrelated sheds when they fit safely under the cap. If complete buckets total
   180 and the next whole shed would exceed 200, keep 180 and carry the next
   shed/group forward instead of splitting it.
7. Never invent an operator fallback. A vaccination drive assignment's
   `operator_id` must be an active workforce member whose
   `primary_location_id` is the same park as the assignment's `park_id`.
   If `vaccination_operator_assignment_config` is missing for a park, stop and
   fix the park config; do not use another park's default operator. Any manual
   SQL repair must include a pre-commit check that no assigned operator belongs
   to a different park.
8. After generation, report what actually happened: animals scheduled on the
   requested anchor date, animals pushed to another date, the reason for each
   push, remaining missing work, and next booster/revaccination dates.

For the current Goat OS vaccination rules, `Z1+Z3` is goat + sheep, killed,
bacterial/toxoid, first course at 4 weeks with booster at 7 weeks, and
revaccination every 6 months. If the maintainer says "all kids and adults Oct
15", that means anchor all selected live animals on October 15 and let the
kernel apply compatibility and future scheduling from there.

- Adult animals with no accepted history for a vaccine automatically join that
  vaccine's normal adult drive. Do not require or render a separate manual
  campaign; `repeat` versus `initial/catch-up` is per-animal dose status inside
  the same logical drive. Overlapping repeat safe windows must coalesce on their
  latest shared ready date, and that date applies to both history-backed and
  blank-history obligations. A physical shed at or below the full per-operator cap
  is indivisible and must carry to the next operator-day when residual capacity
  is insufficient. Verification/director closure timestamps never replace the
  operator submission's `administered_at` medical anchor.
- Vaccination drive batching is park-level, animal-first, and safe-window-bound.
  Shed count is never a merge constraint; it is display/proof detail. A 1-2
  animal drive is valid only after proving no compatible same-park animal group
  can join between that group's due/ready date and binding safe-until date.
  Normal per-drive animal caps are soft on the last safe day, but the per-animal
  shot cap remains hard. Reseed/local proof must run
  `make vaccination-drive-clubbing-db-proof` after sweeper closeout; without it,
  Calendar/Full Schedule screenshots are not batching evidence.
- Vaccination source dates are base history anchors, not open due work. A seed
  or reseed must preserve trusted past dates as accepted history, suppress any
  seed-created open work on or before the backend business date, and let the
  vaccination kernel generate only future obligations from that base. Seed code
  must not hand-roll kid/adult path selection; it must use the live vaccination
  schedule-path helper/config so stale source tags such as `origin=birth` or
  `K1/K2` cannot force old kid-course work. Raw source vaccination cells also
  must not be pre-mapped as kid-course history to prove their own schedule path:
  classify first from independent evidence, then persist the source date as the
  selected rule family's history anchor. The concrete checklist lives in
  `docs/runbooks/vaccination-seed-source-date-contract.md`.
