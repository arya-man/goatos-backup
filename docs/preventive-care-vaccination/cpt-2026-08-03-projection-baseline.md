# CPT Vaccination Projection Baseline - 2026-08-03

This note captures the local/default-db projection baseline used by the admin-web
Vaccination Command Board on 2026-08-03. These dates are provisional planning
outputs: when vaccination rules, compatibility windows, spacing, capacity, or
operator rosters change, the scheduler should recompute the drive rows and the
UI should reflect the new schedule.

## Current Anchor

- Scope: Channapatna / CPT.
- Population: 324 adult animals.
- ET+TT dose-2 history is treated as the booster anchor for this planning view.
- ET+TT dose-2 completed population: 324 animals.
- ET+TT dose-2 source split:
  - 2026-07-24: 114 animals.
  - 2026-07-25: 163 animals.
  - 2026-07-26: 47 animals.

## Projected Drive Summary

| Drive | Animals | Doses | Projection basis | Projected drive date |
| --- | ---: | ---: | --- | --- |
| FMD dose 1 + FMD revaccination | 274 | 648 | FMD history plus no-history adult catch-up/repeat cohort | 2027-01-06 |
| ET+TT revaccination | 161 | 161 | Six months after accepted ET+TT dose-2 completion for the eligible cohort in this command view | 2027-01-24 |
| HS dose 1 + HS revaccination | 274 | 648 | HS history plus no-history adult catch-up/repeat cohort | 2027-04-07 |
| Blue Tongue dose 1 + Sheep Pox dose 1 | 229 | 458 | Adult sheep pox/blue tongue campaign paired on one visit | 2027-07-24 |
| Goat Pox dose 1 | 95 | 95 | Adult goat-only pox campaign | 2027-07-26 |
| ET+TT dose 2 completed history | 324 | 324 | Completed booster history, not future work | 2026-07-24 to 2026-07-26 |

## UI Contract

The command-board drive dropdown is a selector for the tables below it:

- `All common drives` shows the whole command-board program.
- A selected drive narrows the KPI cards, `Vaccine x Shed Status`, `Future vaccination drives`, and `Cohort Vaccine Matrix` to that drive.
- `Vaccination by shed` remains the broader shed operational summary until the backend exposes a narrower shed-summary read for a selected drive.

The UI must avoid showing a skinny drive option as `0 animals` or `undefined
animals`. When the backend returns a drive option without counts, the frontend
may derive display counts from the command-board matrix:

- Future planned counts use scheduled matrix rows and due dates.
- Completed-history counts use verified matrix rows and administered dates.
- Multi-vaccine same-visit drives display distinct animals as headcount and
  summed rows as doses.

