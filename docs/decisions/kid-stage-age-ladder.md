# Kid stage ladder: age-triggered K0→K1→K2 shifting raises

**Maintainer decision 2026-08-20.** Status: accepted.

## The rule

- A kid is due to move **K0 → K1 at 2 days of age** and **K1 → K2 at 7 days of age**.
- Age is **business-day grain in Asia/Kolkata** (a kid born any time on day D is due K1 at the
  start of day D+2), keyed on `goats.dob` (falling back to `approx_dob`). Never hour arithmetic.
- The **system raises the shifting automatically** when the kid's park has **exactly one pen**
  whose resolved destination tag is the step's target (`K1`/`K2`), resolved through
  `counts/domain.ResolveShiftingDestinationPenStage` — the same resolver every manual raise uses.
- When the park has **zero or several** candidate pens, nothing is raised: the due group is
  surfaced as an **operator card** and the operator picks the destination ("operator will select
  any one"). Auto-picking one of two K1 pens would move animals to a pen nobody chose; refusing
  outright would hide due work.

## What this amends, and what it deliberately does not

This is a **scoped amendment of the 2026-08-05 "placement, not birthday" rule** (migration
`000109_stage_age_band`): the early milk ladder (K0→K1→K2) is clock-driven husbandry — colostrum
ends, bottle volumes change (`counts/domain.milkPreparationRuleForStage`) — so its RAISE is
automated. Everything the 2026-08-05 decision protects stays protected:

- **The ladder automates the RAISE, never the move.** The auto-raise produces the standard
  movement — a `pending` `shifting_events` row plus its counts approval request — so Park Head
  approval (approve-first, 2026-08-09), operator completion with the mandatory video, and
  verification all run unchanged. The stage still changes only when a completed shifting applies.
- **Later-life stages stay placement-driven.** K2→K3, F2, and adult cohorts get no age rule;
  the tag remains the authority and changes when the animal is shifted.
- **No DOB-derived reclassification.** Nothing writes `management_stage` or `age_band` from age.

## Mechanics

- Due set: `counts/adapters/postgres.ListKidStageDueGoats` — alive, unmerged, placed kids at the
  step's FromStage born on/before the cutoff date, **excluding kids already named in an in-flight
  movement** (approval `payload->goat_ids` joined to a `pending`/`authorized` event), so the sweep
  can run any number of times without double-raising.
- Raise: `counts/app.KidStageLadderRaiser.AutoRaise` — one movement per (park, step) group,
  priority `low`, category `growth`, `stage_mode=destination_stage`, impacts and source derived by
  the same service functions the app write path uses (mixed source pens degrade the stored source
  to absent per the 2026-08-20 multi-pen rule; groups are per park so a cross-farm set is
  impossible by construction). Raised by the fixed system actor
  `counts/app.KidStageLadderActorID`, which resolves to no workforce name on purpose — the
  approvals queue drops the unresolvable "Raised by" clause.
- Idempotency: deterministic key `auto-kid-stage:<park>:<stage>:<business date>:<goat-set digest>`
  — a crashed sweep replays onto the same row; a late-recorded birth grows the set and gets a new
  key while already-raised kids stay excluded.
- Runner: `backend/cmd/kid-stage-shifting-raise` (one-shot, tenant-scoped, `-dry-run` supported),
  the same shape as `feed-transport-issue`. Scheduling it daily on STG is an infra step tracked
  separately; it is safe at any cadence.

## Proof

- `counts/app.TestKidStageLadder*` — cutoff math is business-day grain; exactly-one-pen
  auto-raises with derived impacts and the approval payload carrying every kid; two pens and zero
  pens both become the operator card; nothing due is a clean no-op.
- Live run against the STG-clone DB (2026-08-20): 2 real K0 kids (born 2026-08-13) auto-raised to
  the K1-tagged Castro pen 2 as ONE pending movement + approval; immediate rerun raised nothing;
  K1→K2 groups with no tagged K2 pen surfaced as cards.
