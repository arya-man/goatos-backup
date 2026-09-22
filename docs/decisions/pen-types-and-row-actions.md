# Pen types are configured, and a row's actions are on the row

Maintainer instruction, 2026-09-22, in two halves that arrived together:

> "Once check items and configurations completely — I don't think there is an option now to edit
> or change status or remove any row."
>
> "Like if you want to edit or remove one pen, make it inactive, or if you want to remove one
> medicine — like that it should be very nice, simple, easy. It's too random and too messy."
>
> "Like if you see, currently we have elevated sheds and ground sheds. These are not mapped in
> items and configuration. That should be also mapped: create one type of pens — we have elevated
> and ground — and map those sheds which are there to these."

## 1. How a pen is built is now a stored fact

Until this change, **elevated vs ground was not stored anywhere.** The Weights page's
"Elevated vs ground pens" comparison worked it out in SQL, per request, from a keyword regex over
`shed_profiles.notes` / `context` and then a hardcoded list of pen NAMES:

```sql
WHEN lower(loc.name) ~ '\m(gandhi|castro|ho chi minh|old yashoda|yashoda old)\M' THEN 'ground'
WHEN lower(loc.name) ~ '\m(mandela|godel|sumathi|new yashoda|yashoda new|yashoda)\M' THEN 'elevated'
```

That answered correctly for the eighteen pens the farm had when it was written, and it could not be
corrected by anyone who is not a developer. A pen built elevated next month reports as whatever its
name happens to match; a pen renamed changes type silently; a new farm gets nonsense.

So the farm now says it:

- **Pen types** is a register under Farm places — a per-tenant code vocabulary in the same shape
  species and gender use. **Elevated** and **Ground** are `is_builtin`: renameable, never made
  inactive or removed, because the comparison names those two codes. A third type may be added and
  pens mapped to it; it is simply not one of the two bars that chart draws.
- **The assignment is PER PARTITION**, by instruction ("assignment will be per partition only, not
  pen"). A partition *is* the pen the farm works — Castro 1, Mandela 1 - Part 3 — and one building
  can hold pens that were built differently. It is a `pen_type_code` column on `shed_partitions`
  with a foreign key to the vocabulary, a column on the Partitions register, and a filter.

### Why the code lives on `shed_partitions` and not on a table of its own

Weighing is ISOLATED (AGENTS.md → "Weighing Is ISOLATED"). `shed_partitions` is already on the
read allowlist for every weighing file — it is the ORG catalog of the pens that physically exist,
with no per-animal data. Carrying the code there means the Weights read swapped a guess for a
stored fact **while reading no table it could not read before**, so the isolation lock needed no
widening at all.

It went the other way instead: `weight_demographics.go`'s exemption **gave `shed_profiles` back**,
because the guess was the only thing that needed it. An exemption that stops being needed is
removed, not left standing as a permission nobody uses.

### The chart reads the configured type ONLY

Maintainer's choice, offered against a fall-back-to-the-guess alternative. One source of truth, and
a pen nobody has classified is left out rather than guessed at — which is what that chart's caption
has always promised readers.

Migration `000385` backfills every pen with exactly what the retired regex answered, so **no bar
moved on the day this landed**. Proven on the live herd, not asserted:

| | |
|---|---|
| Partitions classified by the backfill | **117 of 117** — 100 elevated, 17 ground, none left blank |
| Breed × pen-type buckets, guess vs stored | **14 of 14 identical** |
| "Which pens count as this" members | **72 of 72 identical** |

Seven weighing buckets lose a type: all seven are **inactive locations with zero observations and
zero residents** (CBE/CPT Godel 1 Part 9-10, CBE Mandela 1 Part 8-10 — duplicate-name rows from an
old layout). The three live pens with that name shape, which carry 271 scans between them, resolve
through their alias catalog row and are unaffected.

## 2. A row's actions are on the row

Edit, archive and delete all already worked — `configuration-actions.ts` had every server action and
every store implemented all three. **None of them could be found.** The only route to them was:
click a row's name, open the drawer, scroll past a form of up to fourteen inputs, and read two
buttons labelled "Archive" and "Delete" — words the farm does not use. A capability nobody can find
is, on screen, a capability that does not exist. That is the whole content of "it's too random and
too messy".

Each row now carries the same three, in the same place, on every register: **Edit** beside a `⋯`
menu holding **Make inactive / Make active** and **Remove**, with the removal confirmed in place.
The drawer keeps them too — someone already inside a record should not have to close it to act on
it — and both surfaces post the SAME server actions, so there is one write path and one refusal
sentence, not two that can drift.

The vocabulary moved with it, in the backend page contract where all of this page's copy lives:
`Archived` → **Inactive**, `Archive`/`Restore` → **Make inactive**/**Make active**, `Delete` →
**Remove**.

### The refusals, and what they may not promise

`Make inactive` and `Remove` are gated on the SAME usage check: a row something still uses can be
neither. Maintainer confirmed this stands ("when no animals then you can inactive or remove"), so
the rule did not change — but the copy had to. The Remove confirmation used to advise
"make it inactive instead", a path that is refused for exactly the same reason. It now says what is
true:

> Remove this for good? It cannot be undone. A record something still uses can be neither removed
> nor made inactive.

The backend's own sentence is shown verbatim beside the row: *"In use by 54 animals. Move or change
those first."* The client composes no part of it.

A **built-in** row offers Edit only. The backend refuses the other two for it, and offering a
button that always fails is a worse answer than not offering it.

Swept across every register on the live console, each writable one answers all three and each
refusal is one of exactly two honest kinds — `in_use`, or `builtin_row`. Read-only registers
(Roles, Breeds, Animals, Status definitions) name the screen that owns them instead.
