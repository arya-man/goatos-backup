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
- The Partitions **Pen type** field is a reference to the register. A code nobody authored, or one
  the farm archived, is refused on that field; a pen already carrying an archived type keeps it until
  someone changes it. A type still given to a pen cannot be removed -- archive it instead.

## Where pen types are read, and how each takes its names

| Surface | Grouping key | Names and order |
|---|---|---|
| Weighing -> ADG Analytics -> Pen-wise ("Daily gain by pen type") | `shed_partitions.shed_type` code, returned by `weight_demographics.go` | the page contract's `pen_types` option group, compiled from `pen_types` by adminui |
| Health Analytics -> "Health problems by pen type" | same code | `pen_types` read in the same batch as the problems query; "Pen type not set" last |
| Configuration -> Partitions | FK | the register's options |

**Weighing never reads `pen_types`.** It is not one of weighing's four allowed org tables, and adding
it is a maintainer decision. Weighing returns the code only; the page contract carries the names.
This is the same injection path feed items and parks use.

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
