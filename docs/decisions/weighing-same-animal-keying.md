# Weighing keys an animal by the animal, not by the string that was scanned

**Maintainer decision, 2026-09-07.** Recorded as the FIFTH file-scoped herd exception in the
weighing isolation lock, and the narrowest of the five.

## The report

> "Some animals have two RFIDs. Are you considering both? Sometimes they don't scan the primary,
> they scan the secondary. First week they scanned one RFID, second week another. Will it show
> ADG?"

It did not. And that is the shape of the defect worth naming: **the number was not wrong, it was
absent.** A wrong number gets questioned. A missing one looks like an animal that was simply not
weighed twice.

## What was actually happening

An animal on this farm can carry two RFIDs — `goat_identifiers.identifier_type` is literally
`animal_identifier_1` or `animal_identifier_2` — and an operator scans whichever tag they can read
off the ear that day. Every reporting read on the Weights and Growth surfaces identified an animal
by the RAW SCANNED STRING:

```sql
lower(btrim(wo.scanned_identifier)) AS animal_key
```

So an animal weighed on its primary tag in week 1 and its secondary in week 2 was **two animals
with one weigh each**, and the two errors ran in opposite directions at the same time:

| | before | after |
|---|---|---|
| ADG pairs for that animal | 0 — it vanished from the headline, the gain-by-breed/sex/stage charts, the band board's moved-up/held/slipped-back, and the losing-animals list | 1 |
| animals counted | 2, each "weighed once" | 1, weighed twice |
| its latest weight | two different ones, in two shed-table rows | one |

The farm cannot scan its way out of this. Both tags are on the animal and both are legitimate.
Only the herd register knows they are one animal.

## The fix

One new file, `backend/internal/weighing/adapters/postgres/identity_scope.go`, answers exactly one
question — *which of these scanned strings are the same animal* — and hands every consuming read an
**opaque pair of parallel arrays**: scanned tag, and the canonical tag it keys under. Those reads
(`growth.go`, `shed_weights.go`, `load_weights.go`, `weight_demographics.go`, and the Growth
Director's band board and trust panel) still name no herd table and still know nothing about
animals; they receive strings.

This is the shape `sex_scope.go` and `origin_scope.go` already established, for the reason recorded
there: the alternative was five more files each joining `goat_identifiers`, which is the 2026-08-04
leak repeated five times over.

## Why it is the narrowest of the five exemptions

It reads **`goat_identifiers` and nothing else** — not `goats`, not `goat_shed_partitions`, not a
procurement table. It never learns an animal's sex, breed, stage, pen or origin. The guard's table
allowlist is keyed PER FILE precisely so this stays true: reading `goats` inside this same exempt
file is still a finding, and there is an adversarial self-test that proves it. Withholding the base
herd table set here is the point — a file that cannot read `goats` cannot quietly grow into a second
demographics resolver, which is how a keying helper becomes a herd dependency.

## Four properties that keep it safe

1. **READ-ONLY and REPORTING-ONLY.** No capture, submit, close or verdict path calls it. In
   particular **the one weighing business rule — an animal may not be scanned twice in the same
   bucket before submit — still compares RAW STRINGS and is deliberately untouched.** Making that
   identity-aware would gate a scan on the herd register, which is banned outright. An operator can
   still scan an animal on both its tags in one bucket; the register does not get a vote.
2. **A single-tag animal is NEVER remapped.** The map carries rows only for animals holding two or
   more active identifiers, so a farm, a window, or a test with no double-tagged animal gets an
   EMPTY map and every read runs the query it ran before this file existed, key for key. That is
   what bounds the blast radius of the exception to the animals it is about.
3. **An unresolvable tag keeps its own string.** Free-flow accepts a scan the register has never
   heard of; it is still recorded, still counted, and still pairs with itself.
4. **The canonical key is one of the animal's OWN tags — its `animal_identifier_1` — never a
   goat_id.** `animal_key` is rendered verbatim to a reader as `ScannedIdentifier` in the
   losing-animals list, so a uuid there would put a database key on a farm screen. Choosing the
   primary tag (rather than whichever was scanned first, or last) also keeps the merged history
   reporting under a stable name from one window to the next.

## The cost, stated plainly

Weighing now depends on herd identity data being right — the exact dependency `growth.go`'s header
comment refused in 2026-08-04. **If the register wrongly attaches animal B's tag to animal A, their
weights merge and the gap between them reports as growth that never happened.**

Two narrowings hold that down, and both are load-bearing and pinned by tests:

- Only `status = 'active'` identifiers are read. `disputed`, `duplicate` and `invalid` are the
  register's own way of saying "do not trust this row", and they are precisely the rows that would
  fuse two animals.
- A tag is remapped only when the SAME goat carries another one.

The residual exposure is a double-tagged animal with an active, undisputed, wrong second tag. That
was accepted as the price of reporting a number at all, against the status quo of reporting none.

A genuine RE-TAG (old tag retired, new tag issued) still splits an animal's history, and that stays
honest: weighing only ever knew the tag that was scanned.

## Scope, and why it is this wide

Fixing only the ADG read would have left a page whose headline counts an animal once and whose shed
table beside it counts the same animal twice — a cross-surface disagreement about a business number,
which is a maintainer question rather than an implementation detail. So the map reaches every
reporting read that treats a tag as an animal, and `TestShedTableAndHeadlineAgreeOnADoubleTaggedAnimal`
pins that agreement.

Deliberately NOT changed: the write path, the "no scanning twice in a bucket" rule, and the
operator-facing per-campaign captured counts, which are scan-grain by design.

## Proof

`backend/internal/weighing/adapters/postgres/identity_scope_integration_test.go`, five tests on the
real production read path, each mutation-tested when written:

| test | mutation that turns it red |
|---|---|
| `TestTwoRFIDsOnOneAnimalPairIntoOneADG` | map merges nothing (the pre-fix behaviour) — `PairCount=0` |
| `TestMergedAnimalIsReportedUnderItsPrimaryRFID` | same, plus any change reporting the last-scanned tag |
| `TestShedTableAndHeadlineAgreeOnADoubleTaggedAnimal` | same — shed table counts 2 |
| `TestDisputedSecondIdentifierDoesNotMergeAnimals` | dropping `status = 'active'` |
| `TestSingleTaggedAndUnknownTagsAreUntouched` | any change that remaps animals it was not asked about |

Machine enforcement: `make weighing-free-flow-guard`, whose per-file allowlist carries this file
with `goat_identifiers` alone, plus a self-test asserting that reading `goats` in that same file is
still a finding.
