# Every load chart names its pens

> **Superseded in part, 2026-09-24:** the weighing charts no longer read pens from
> `weighing_shed_load_tags`. A load is its animals, followed through every pen move, and the pens
> in the bracket are where its live animals are today. See [load-follows-its-animals.md](load-follows-its-animals.md).
> The bracket itself, and its shape, stand.

**Maintainer request, 2026-09-22.** Any graph on the dashboard that names a LOAD carries that
load's PENS in a bracket beside the name:

```text
131 (CPT Castro 1, CPT Castro 2)
Load 129 · Krishnamorrthy (CPT Godel 2 - Part 1, CPT Godel 2 - Part 2)
```

## Why

A load number says which invoice the animals arrived on. It does not say where to walk. Every
question a load chart raises — "why is this load growing slower", "why did this one cost more per
kilo" — is answered by going to look at the animals, and the reader could not get from the bar to
the pen without opening another screen.

This generalises the 2026-09-21 change that put pens on the ADG Load-wise tab's two charts, and
supersedes that change's SHAPE: those pens rode a SUB-LINE under the group heading, and are now a
bracket on the heading itself, so every load chart on the dashboard labels a load the same way.
The head counts the sub-line carried stay in that tab's Pens TABLE column, which has the room an
axis does not.

## The four charts, and what a bracket MEANS on each

There are two honest sources of "which pens is this load in", and they answer different questions.
They are not reconciled and must not be.

| Surface | Chart | Pen source | A pen is in the bracket when |
|---|---|---|---|
| `/weighing/weights` | Load weight + load gain bars | weighing `by_load[].placements` | it is TAGGED to the load and carries a weigh in the window |
| `/weighing/analytics` → Load-wise | Purchase-vs-latest bars, value bars | the same `placements` | as above |
| `/sales/loads` | All five load columns | the same `placements`, over a 365-day window | as above |
| `/counts/breakdown` | Purchased loads columns | counts `loads[].pens` (added here) | one of the load's FILTERED LIVE animals sits there now |

The weighing-backed three answer **"where was this load weighed"**. They read
`weighing_shed_load_tags`, so a pen holding two loads is attributed to neither, and an animal that
walked into an untagged pen is absent. Counts answers **"where do this load's animals live now"**,
straight off the herd register, so that animal IS there.

**The two therefore disagree on purpose, and the gap is real information.** On 2026-09-22 the
Load-wise chart showed load 130 as 73 animals in `CBE Castro 2` against 77 bought: three of its
animals had moved into CBE Yashoda 1 and Yashoda 10, which carry no load tag, and a fourth was
never registered at all. The maintainer read the reconciliation and decided to leave both gaps as
they are. Do not "fix" either side by teaching it the other's source.

## Rules

1. **One composition.** `apps/admin-web/lib/load-pens.ts` — `loadPenNames`, `loadPenBracket`,
   `withLoadPens`, `pensFromPlacements`. Every chart calls it. A hand-rolled bracket in each
   feature would drift in separator, order and overflow rule, which is the OL-7 defect class one
   noun over. It generalises the `penList` helper that lived privately in
   `load-comparison-tab.tsx`; that helper survives for the TABLE cell, which keeps head counts.
2. **The park is part of the pen name** (maintainer, 2026-09-05). `Castro 1` is a real pen in BOTH
   parks. An unqualified bracket on a chart that mixes parks names a pen that could be either —
   the OL-1 name-merge defect. A pen whose park could not be resolved is still named, never
   dropped.
3. **The bracket is never invented.** No known pen means no bracket at all: not an empty pair of
   parentheses, not the park, not the vendor. A principal without the weighing read (the
   `weights_current_average_series` capability on `/sales/loads`), a load with no tagged pen, and
   a load whose animals have all left each render exactly as they did before this existed.
4. **Axis short, tooltip full.** The axis label spells out two pens and then COUNTS the rest
   (`+3`); tooltips and table cells pass `Number.POSITIVE_INFINITY` and spell out every one. A
   bracket that silently showed two of five pens would read as the whole answer.
5. **A sold-out load on the value chart is named without its pens.** Its animals are gone, so
   naming the pens they used to sit in points a reader at a pen that no longer holds them. This
   predates the bracket and survives it, and is the one place two charts on a page label the same
   load differently — deliberately.
6. **Pen names come from the backend composed.** `operational_location_display`, never a
   client-side join of shed + partition, and never `normalized_label` — the scrubbed matching key
   renders as "Mandela 2 - 3", which shipped once already.
7. **Never key or group a load's pens by shed NAME.** Group by `(park, shed_id, partition)`.
8. **`locations.name` is not a parent shed name.** The counts decoder resolves through
   `oploc.ResolveComposedName`, not the bare composer, because this tenant carries legacy per-pen
   ALIAS rows: a goat's `shed_id` can point at a location already named `Mandela 1 - Part 1` while
   its `goat_shed_partitions` row stores `Part 1`. Composing those two renders
   `Mandela 1 - Part 1 - Part 1`. The first version of this change did exactly that and was caught
   by `make operational-location-guard` (`composed-name-into-composer`), not by review.

## Where it is enforced

- `apps/admin-web/lib/load-pens.test.mjs` — park qualification, the same pen name in two parks,
  the `+N` overflow, and the never-invented rule.
- `apps/admin-web/features/weighing/load-comparison-pens.test.mjs` — both charts read one
  heading, and the sold-out load is named without pens.
- `backend/internal/counts/adapters/postgres/counts_breakdown_integration_test.go` →
  `TestCountsBreakdownLoadsNameThePensTheirAnimalsSitIn` — pen grain (never rolled up to the
  shed), same-named sheds in two parks, the human partition label, and `sum(pens) == on_farm`
  under the page filters, plus four adversarial subtests: one-to-many fan-out, page boundary
  (the loads read is whole-result and its pens must not move between pages), park scope, and a
  dead animal leaving the bracket with the count. Mutation-tested: rolling a pen up to its shed
  turns it red.

## Adding a new chart that names loads

Call `withLoadPens(label, pens)`. If the read behind it has no pens, add them as a list of
`{park, shed, partition, animals}` composed through `oploc.Display()` — and say in the contract
which of the two questions above it answers, because a reader will compare it with the others.

## Layout note

`.gclab` (the `GroupedColumns` axis label) is clamped to TWO lines rather than held on one. A
single `white-space:nowrap` line pushed the bracket out of the column entirely at eight loads
across, which is the same as not rendering it. A label with no bracket still occupies one line.
