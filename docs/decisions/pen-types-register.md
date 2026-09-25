# Pen types are the farm's own list

Maintainer instruction, 2026-09-25:

> "I want to add one more pen type ... after park, before pens, we need pen type ... then I will go
> to partitions and map those to that pen type. Weighing or anywhere -- mobile, app, web -- wherever
> I see these pen types, it should not be hard coded, it should come from there."

## What changed

- **Configuration -> Items and settings -> Farm places** now reads Parks, **Pen types**, Pens,
  Partitions. Pen types is an ordinary register (`pen_types`, migration `000428`): name, code
  (made from the name when left blank, immutable once saved), description, order. Rows are renamed,
  archived and re-ordered on screen.
- **The grain did not move.** A pen type is still set per PARTITION (maintainer, 2026-09-22,
  reaffirmed 2026-09-25): Castro 1 and Castro 2 may be built differently. `shed_partitions.shed_type`
  keeps its name and its codes and is now a FOREIGN KEY into `pen_types`, replacing the
  `elevated | non_elevated` CHECK. Every existing pen kept its value; each tenant was seeded with the
  two kinds the CHECK allowed.
- The list shows how many pens HOLD each type, so the mapping is visible at a glance.
- The Partitions **Pen type** field is a reference to the register. A code nobody authored, or one
  the farm archived, is refused on that field. A type still given to any pen can be neither removed
  nor archived (the rule every register follows): move those pens first. So an archived type is
  never on a pen; the charts' archived-type handling below is defensive only.
- **Bulk mapping**: download Partitions, fill the Pen type column by name or code, upload. The
  Partitions sheet carries row version 0 (shed_partitions keeps none), which the upload used to
  refuse for every register -- so a downloaded Partitions sheet could never go back up. Partitions
  is now marked `unversioned`; every other register still refuses 0.

## Deploy and reseed keep the mapping

- **Deploying onto an existing database** (STG, the OCI clone): migration `000428` seeds the two
  pen types for every tenant and turns the column into a foreign key; no pen's value changes.
  Proven on a clone of the farm: 117 active pens kept 100 Elevated / 17 Non-elevated.
- **A fresh database or a reseed** creates pens after migrations run, so no migration can classify
  them. `fixtures/pen-types/pen-type-map.sql` -- the mapping read from goatos-stg on 26/09/2026,
  130 rows keyed by park code + pen name + partition label -- is applied by `tools/dev/seed-closeout.sh`
  right after the pens are seeded. It creates the two base pen types if missing and fills ONLY pens
  with no type, so it never overwrites a choice made on screen and is safe to re-run. Proven on the
  clone: wipe every type, apply, and the result is row-for-row identical to STG; a second run
  changes nothing. Pinned by `TestSeedCloseoutAppliesThePenTypeMap` and
  `TestPenTypeMapNamesOnlySeededPenTypes`.
- A pen type the farm adds later is not in that file. Refresh the file from STG when the farm's
  mapping changes materially, the same way the other `fixtures/` snapshots are refreshed.

## Where pen types are read, and how each takes its names

| Surface | Grouping key | Names and order |
|---|---|---|
| Weighing -> ADG Analytics -> Pen-wise ("Daily gain by pen type") | `shed_partitions.shed_type` code, returned by `weight_demographics.go` | the page contract's `pen_types` option group, compiled from `pen_types` by adminui |
| Health Analytics -> "Health problems by pen type" | same code | `pen_types` read in the same batch as the problems query; "Pen type not set" last |
| Configuration -> Partitions | FK | the register's options |

**Weighing never reads `pen_types`.** It is not one of weighing's four allowed org tables, and adding
it is a maintainer decision. Weighing returns the code only; the page contract carries the names.
This is the same injection path feed items and parks use.

**A rename reaches the charts within about two minutes** (the page contract is cached 60 s in the
API and 60 s in admin-web), the same as feed items and parks. Configuration itself shows it at once.

No surface names a pen type. Guarded by `TestHealthNamesNoPenTypeOfItsOwn`,
`TestWeighingNamesNoPenTypeOfItsOwn`, `features/weighing/pen-type-series.test.mjs`, and
`TestPartitionsTakeTheirPenTypeFromThePenTypesRegister`. `TestShedTypeDecodersKeepAPenTypeTheFarmAdded`
pins the defect this closed: weighing's decoders dropped any code other than the two, so a third
kind of pen would have been weighed and grouped, and then silently vanished from the chart.

The Android app shows no pen type today. If it ever does, it takes the names from the backend the
same way.

## Chart behaviour with more than two types

- Weighing: one series per pen type, in register order. Active types are always in the legend; an
  archived type appears only while its pens still count. Six bar tones exist (`.s0`-`.s5`).
- Health: every active type is a bucket even at zero, in register order. An archived type appears
  while cases sit in its pens. A code the register does not name is still counted, so the headline
  total never loses a case.
