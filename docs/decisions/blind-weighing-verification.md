# Blind weighing verification

**Maintainer decision, 2026-09-21.** Supersedes the *optional-correction* half of the
2026-08-17 verifier weight-correction decision and of the 2026-08-20 "the approve carries the
number" decision — for **weighing only**. Everything those decisions say about *how* the number
rides the verdict is unchanged and still load-bearing.

## The rule

> Whatever operators give, that will be the weighing until verifier gives. Whatever the verifier
> gives, that will be the final weighing of that animal or that shed.

Two consequences, and the second is the new one:

1. The operator's weight is the **working** weight while the proof waits for review. This is
   already what happens — `CorrectObservationWeight` overwrites `weight_kg` in place, and every
   read model reads that one column — so no read model changed.
2. The verifier **must** record her own reading, and she is **not shown** the operator's. She
   watches the video, reads the scale, and types what she sees. That becomes the recorded weight.

It applies to **both grains**: an individual scanned animal and a whole-pen lump sum. They share
one verification category, and the decision names both.

## Why blind

The subject label was originally widened to carry the weight for a good reason: before that, every
individual weighing item read the hardcoded literal `individual animal weight`, so fifteen items
from one pen rendered byte-identical and a verifier had nothing to tell them apart — an operator
who typed 120 kg for 12 kg produced a perfectly ordinary video of a goat on a scale that nobody
could catch.

Showing her the number solved the wrong half of that. **A reader who has already been told the
answer confirms it.** An anchored verifier is precisely the one who waves the 120-kg typo through,
because 120 is what the screen says and the video is a goat on a scale. Hiding the number is what
makes her reading an independent second measurement instead of a rubber stamp.

The fix the widening delivered still survives, because the **scanned tag stays on the label**. The
tag is what keeps the rows distinguishable; the weight was never what did that work.

## What the label says now

`weighing/domain.CorrectedSubjectLabel` composes the one sentence both surfaces render:

| Grain | Label |
|---|---|
| Individual | `Castro 2 · Tag 9010123` |
| Lump sum | `Godel 1 - Part 3 · 31 goats` |
| Pen unresolved (lump sum) | `Whole pen` |
| Neither pen nor tag resolved | `Individual weigh` |

The `weightKg` **parameter is gone**, not passed-and-ignored. A composer that can still be handed a
weight is one edit away from printing it again, and that edit would look like a bugfix.

**The head count stays.** It is snapshotted from the herd register at submit and frozen
(maintainer decision 2026-08-24) — it is not a number the operator typed, so it anchors nobody. It
tells the verifier how many animals the pen total she is about to read covers.

## The three moving parts

| Part | Where | Note |
|---|---|---|
| No weight on the label | `weighing/domain.CorrectedSubjectLabel` | Composed at enqueue *and* recomposed after a correction, from one function, so the two cannot drift |
| Approve requires her number | `verificationcatalog.Weighing` → `RequiredForApprove: true` | Both grains |
| Blank approve is refused | `weighing/adapters/verificationbridge.MeasurementApplier.HasRecordedMeasurement` | See below |

**Any one of these alone is worse than none of them.** A required field beside a visible operator
weight is a rubber stamp with extra typing. A hidden weight with an optional field is an approve
that silently keeps a number nobody on the verifying side ever saw.

## The sharpest trap: `HasRecordedMeasurement`

It used to `return true` flat. That was **correct** while weighing's measurement was optional,
because verification only consults it for a `RequiredForApprove` category — so it was never asked.

Left as it was, it would have waved through every unmeasured weighing item, because **every
weighing row carries a weight from the moment the operator captured it.** "Does this row have a
weight" is true before any verifier has watched anything.

The real question is whether a **verifier** set the weight, and weighing answers it from
`operator_weight_kg IS NOT NULL` — written on the first correction and never again, so its presence
*is* the fact.

It is asked only when an approve arrives carrying no number, which preserves the escape hatch: an
item already measured through the standalone `.../weight-correction` route (an installed APK still
showing its own save button) stays approvable in one tap. A producer that cannot answer returns an
error and the approve is **refused**, never let through on an assumption.

## It locks weighing sampling at 100%

`CategoryDefinition.SamplingWaivable()` is derived from `RequiredForApprove`, never a hardcoded
list — so weighing became non-waivable the moment the flag flipped. That is correct: auto-approving
an unwatched weighing video would complete a bucket with no verifier reading at all.

But it opens a real hole. `samplingsql.InSample` knows nothing about waivability and still reads
whatever row `verification_sampling_policies` holds. A tenant whose CEO had set weighing to 40%
would keep a queue narrowed to 40% while the other 60% is settled by **nobody** — the closeout no
longer touches weighing — leaving those items pending forever, each holding its weighing bucket
open against a close gate that is unconditional by design (ledger D-5).

Migration `000382_weighing_sampling_locked.sql` **deletes** the stored weighing rows (rather than
rewriting them to 100, which would read to the next author as a setting rather than a lock). With
no row, the predicate's own `COALESCE(..., 100)` draws everything, and `SetSamplingPolicy` refuses
new weighing rows with `sampling_not_available`.

> **General rule this records:** making a category non-waivable is not complete until its stored
> `verification_sampling_policies` rows are removed in the same change. The write path stops new
> ones; only a forward migration clears the ones already written.

## Both surfaces, from one contract

Neither the admin-web verify drawer nor the Android verify screen decides any of this. Both render
`subject_label` verbatim and both honour `required_for_approve` generically, so this single backend
change reaches the web and the app together. **Do not add a client-side weighing branch** — that is
how the two surfaces come to disagree about a business number, which AGENTS.md bans outright.

## What did NOT change

- **Reject never carries a number.** A reading that cannot be taken off the clip is exactly the
  case that must be sent back, so Reject is never held on the measurement.
- **The head count is still nobody's to edit**, the verifier included (2026-08-24).
- **Free-flow capture is untouched.** Nothing here resolves a scanned tag to an animal, reads a
  roster, or gates a scan. The one weighing business rule — no scanning an animal twice in the same
  bucket before submit — is unchanged.
- **Read models are unchanged.** The corrected weight was always written to the live `weight_kg`
  column every read model already reads.

## Pinned by

- `verificationcatalog.TestWeighingApproveRequiresTheVerifiersOwnWeightReading`
- `verificationcatalog.TestWeighingSubjectLabelShowsTheVerifierNoOperatorWeight`
- `verificationcatalog.TestWeighingSamplingIsLockedBecauseTheVerifierIsTheDataSource`
- `weighing/domain.TestSubjectLabelNeverCarriesTheWeightAtEitherGrain`
- `weighing/domain.TestLumpSumLabelKeepsTheFrozenHeadCount`
- `weighing/app.TestRecordAnimalObservationVerificationSubjectNamesTagButNeverTheWeight`
- `weighing/app.TestRecordShedObservationVerificationSubjectNamesPenAndCountButNeverTheWeight`
- `weighing/adapters/verificationbridge.TestUnverifiedWeighingProofReportsNoRecordedMeasurement`
  and its two siblings

Each was mutation-tested when written: reverting the spec flag, restoring the weight in the label,
and restoring the flat `return true` each turn one of them red.
