# CPT Operator-Drive Vaccination Source Packet

Status: committed seed source for local/dev rehearsal before staging.

Business start date: `2026-07-23`.

This packet preserves the exact CPT source files supplied for the operator-cap
vaccination drive rehearsal and adds a small machine-readable roster that states
the intended operator/director/grant meaning without requiring an agent to
reverse-engineer the whole timetable workbook.

## Files

| File | Meaning |
|---|---|
| `raw/CPT-Adult-goats.json` | Supplied CPT animal source rows. |
| `raw/CPT-Adult-vaccination.json` | Supplied CPT vaccination history/source rows. |
| `raw/CPT_Nuanced Timetable.xlsx` | Supplied CPT timetable workbook. |
| `cpt-operator-roster.json` | Normalized seed contract for operators, director, capacity, week-offs, and CEO/CXO grants. |

The `Adult` filename is a source label only. The seed must still use the
published Goat OS vaccination rule engine for kids, adults, boosters, sick/ICU,
pregnancy, death, cull/sale, combo spacing, route/site, safe start/end date, and
the `+1 week` buffer. Do not hardcode an adult-only scheduler because these
files happen to be named adult.

## Scope

- Park/center: CPT / Channapatna only.
- Forbidden in this rehearsal: CBE, Coimbatore, or synthetic CBE owners.
- Operator capacity unit: unique animals per operator per business date.
- Default operator cap: `200` animals/day/operator.
- Dose count is display/workload only; it is not the scheduling cap.
- Drive start date for open work: `2026-07-23`.
- No open drive work may be materialized on `2026-07-22` or earlier during a
  fresh `2026-07-23` seed.

## People To Seed

| Person | Seed role | Execute vaccination | Week off | Animal cap |
|---|---|---:|---|---:|
| Amit Kumar | Vaccination Operator | yes | Friday | 200 |
| Darshan Talwar | Vaccination Operator | yes | Sunday | 200 |
| Sagar Mahoor | Vaccination Operator | yes | Saturday | 200 |
| Chandrakant | Preventive Care Director | no by default | none | 0 |

All three operators are equal field operators. Do not seed Amit as support, do
not seed Sagar as support/backup-only, and do not infer park-head ownership from
old HRMS labels. Their timetable removes them from capacity only on their
week-off or an explicit dated leave row.

Chandrakant is director/monitoring scope. He may see multiple parks such as CPT
and CBE, but he does not add field execution capacity unless a separate explicit
operator assignment is created.

The five founder/CXO accounts must be granted tenant-scoped `ceo_internal` and
`cxo` workforce hint:

- `ravi@mesha.sg`
- `manohark@mesha.sg`
- `manju@mesha.sg`
- `abhishek@mesha.sg`
- `aryaman@mesha.sg`

## Shed And Partition Contract

Shed labels in the raw goat source are physical shed plus optional partition.
They must be normalized before canonical DB writes:

| Raw source label | Physical shed | Partition |
|---|---|---|
| `Gandhi 1` | `Gandhi` | `1` |
| `Gandhi 2` | `Gandhi` | `2` |
| `Gandhi 3` | `Gandhi` | `3` |
| `Godel 1 - Part 1` | `Godel 1` | `Part 1` |
| `Godel 1 - Part 3` | `Godel 1` | `Part 3` |
| `Godel 1 - Part 4` | `Godel 1` | `Part 4` |
| `Godel 2 - Part 4` | `Godel 2` | `Part 4` |
| `Mandela 2 - Part 8` | `Mandela 2` | `Part 8` |
| `Old Yashoda 5` | `Old Yashoda` | `5` |

Physical sheds own goat placement, owner validation, list totals, and read-model
totals. Partitions are preserved on drive assignment rows so operators can be
given whole partitions where possible.

## Expected Fresh-Seed Shape

For a fresh local/dev seed with `AS_OF=2026-07-23`, the first open drive should
start on `2026-07-23`, not `2026-07-22`. The planner should use all three
available operators on Thursday and split by physical shed/partition at animal
capacity grain.

The source packet contains 324 animal rows. The planner may schedule fewer open
animals on a given date depending on accepted history, booster eligibility,
combo-spacing rules, clinical exclusions, and date overrides, but it must never
drop adult ET+TT booster obligations or quietly move safe-window breaches beyond
their latest safe date.

The updated goat source has no active health defers: health status counts are
261 blank, 62 `Closed`, and 1 `Fine`. `Closed` is resolved history and `Fine` is
explicit healthy status; neither may be seeded as `recovering`,
`under_treatment`, or any other vaccination defer.

## Seed/Verify Checklist For Local Or Dev

1. Start from the latest `main` commit containing this packet.
2. Use `2026-07-23` as the backend business date / `AS_OF` for this rehearsal.
3. Normalize these raw files into the canonical seed bundle or use the
   CPT-specific seed command that consumes this packet.
4. Run the DB-free source audit before any DB write.
5. Seed the five CEO/CXO pending email grants with `role=ceo_internal`.
6. Seed only the three CPT vaccination operators plus Chandrakant as director.
7. Seed operator cap `200` as animals/operator/day.
8. Generate obligations through the vaccination rule engine, not from hardcoded
   frontend tables.
9. Run the operator drive planner and verify:
   - no CBE/Coimbatore source rows exist;
   - open drive dates are `2026-07-23` or later;
   - all three operators appear when all are available;
   - Friday removes Amit, Saturday removes Sagar, Sunday removes Darshan;
   - physical sheds are grouped while partitions remain visible in assignments;
   - all 324 animals are considered against vaccination rules, with zero
     source-health exclusions from this packet;
   - ET+TT adult booster, PPR, Blue Tongue, HS, FMD, kid/adult rules, combo
     spacing, sick/ICU/pregnancy/terminal exclusions, and `+1 week` buffer all
     come from backend rules.
10. Verify admin-web and mobile from backend APIs: no frontend hardcoded park,
    operator, shed, cap, or schedule fallback may be needed.

Do not replicate this to staging until the local/dev DB shows the expected CPT
scope, HRMS roster, date start, operator capacity split, booster obligations,
and vaccine-date override recalculation from backend data.
