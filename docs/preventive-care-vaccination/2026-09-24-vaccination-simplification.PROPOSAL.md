# Vaccination simplification — proposal (2026-09-24)

Status: **proposal, not built**. Clickable prototype and old-vs-new walkthrough:
[artifacts/2026-09-24-vaccination-simplification.html](artifacts/2026-09-24-vaccination-simplification.html)
(live copy: https://claude.ai/artifact/6hy8tTA7cD2C1gnyXPdzWL).

## Problem

- One vaccine rule is stored in ~4 places: `protocol_versions.rule_dsl` →
  `protocol_rules` → `protocol_rule_dimensions` → `protocol_rule_lineage`, plus
  copies in `vaccination_anchor_events` and `vaccination_capacity_config`.
- Adult/bought animals are scheduled by three mechanisms: procurement waves,
  `manual_campaign` rules, and anchors, switched by `kids_normal_schedule_until_weeks`.
- Every change goes draft → publish → version; most bug classes of Jul–Sep 2026
  (orphaned work on publish, duplicate open obligations via 5 insert paths,
  25+ anchor fixes) live in that chain.

## Proposal

Vaccination becomes a group in Configuration → Items and settings:

| Config | Holds |
|---|---|
| Vaccines (`vaccines`) | name, live/killed, species, gap weeks, same-day yes/no, never-with, priority, late-by, ml, doses/vial, `item_id` |
| Vaccine programs (`vaccine_programs`, `program_doses`) | filters: origin, species, gender, tags, breed, min age; doses: vaccine · counted from (birth / arrival at buying spot / arrival at farm / previous dose) · weeks · booster · repeat |
| Procurement plans (`load_vaccine_plans`) | per load: doses at buying spot, stay, boosters at warm-up sheds, program joined after |
| Farm vaccine rules (`farm_vaccine_rules`) | max vaccines/animal/day, live↔live and killed gaps, pregnancy skip, hold-off states, arrival rest, operator cap, batching wait |
| Stock | existing `inventory_*` (doses, vials, FEFO lots) |

No versions: edits save directly with a preview of affected future doses and a
`config_change_log` entry.

**Planner** (one pure function per animal): match programs → drop given doses
(buying-spot doses count) → due = start date + weeks → push for hold-off,
pregnancy, arrival rest, gaps. Upserts `due_doses` (today `obligation_instances`)
by unique cause key `animal · vaccine · dose# · cycle#`.

**Sweeper**: one `sweep(scope = pen | park)` replacing the `SweepVersion*`
variants and `park_consolidation.go`; per-animal daily cap by priority,
operator cap, overflow to next safe day, stock reservation, late-by → missed.

## Guards carried over

- Unique cause key; only the planner writes due doses.
- Doses already on a drive are frozen against config edits.
- Dosed-awaiting-proof counts as given (no phantom missed).
- Doses in = doses out (overflow, never drop).
- Backend owns classification; one sweep for pen and park.

## Decisions

- Fattening (bought) gets PPR, ET+TT, sheep/goat pox — confirmed by Aryaman 2026-09-24.
- Open: Z1+Z3 appears twice in the live plan (1 vs 2 drive doses); check stg for
  duplicate open doses before migration.

## Rollout

1. New config tables, filled from live V9 by converter; read-only screens.
2. New planner in shadow mode; nightly diff vs current obligations.
3. Switch generation; enable editable config; retire protocol editor.
4. Merge sweeper paths; link stock by `item_id`; drop dead tables.
