# A newborn is placed in its park's K0 pen

**Maintainer decision, 2026-08-20.** Status: implemented.

## The defect

The birth form offered the FULL park → shed → pen cascade — the same catalog the shifting form
uses — and `RecordBirthEvent` validated nothing about the shed. It read `park_id` only, to derive the
`CBE-`/`CPT-` provisional tag prefix, and forwarded `shed_id` verbatim to identity, whose create path
checks that the shed is active, sits under the park, and that a named pen belongs to the shed.

None of those checks ask what the pen is FOR. A K0 kid could therefore be recorded into a Buck shed,
an F2 pen, or an ICU pen, and nothing downstream noticed: the newborn's `management_stage` is pinned
`K0` by the handler regardless of where it lands (maintainer decision 2026-08-13), so the animal read
as a kid while physically filed with the bucks.

## The rule

1. **A newborn is placed in its park's K0 pen.** A "K0 pen" is a pen whose CONFIGURED tag is `K0` —
   `shed_partitions.animal_stage_id` resolving to the active `animal_stage_lookup` row `K0`
   (migration `000161`), or the shed's own `shed_profiles` row for a shed with no pens.
2. **Exactly one K0 pen** → the form shows it read-only; the operator picks the park, not the pen.
3. **More than one K0 pen** → the operator picks, and the picker offers ONLY K0 pens
   (maintainer decision: the operator chooses; the backend does not rank pens — see *Rejected
   alternatives*).
4. **No K0 pen in that park** → the birth is still recorded. The operator picks freely from the full
   cascade, and the kid's care workflow carries a **Record shed** step whose completion places the
   animal and tags that pen `K0`, so the park's next birth places automatically.
5. **A pen that is not a K0 pen is REJECTED, never silently corrected** — `400
   invalid_newborn_placement`. Redirecting the kid behind the operator's back would file the animal
   somewhere they never saw and never tell them.

## Why the pen's AUTHORED tag, not its residents

A pen kept ready for kids is normally EMPTY. A resident-derived answer would fail to recognise
exactly the pens this rule exists to find, and it would flip as animals move in and out. The
authored tag is what somebody decided the pen is for; it is also what the Counts Breakdown Stage
cell shows and edits, and it is already what a shifting movement adopts (maintainer decision
2026-08-14).

## One implementation, two readers

`protocol/domain.NewbornPenStage` / `IsNewbornPen` is the single answer to "is this a kid pen". It
lives beside `IsClinicalManagementStage` because it is the same kind of question and has to be asked
from two modules that must not import each other:

| Reader | Question |
|---|---|
| `counts/domain.ResolveBirthPlacement` | which pens may this park's birth name |
| `tasks/adapters/postgres.needsShedPlacement` | does this kid's workflow need the Record shed step |

The form contract and the write validation resolve through the SAME function against the SAME
catalog rows, so the pens the operator is offered are byte-for-byte the pens the write accepts.

## Contract

`GET /app/counts/shifting/destinations` carries `birth_placement` per park:

```
mode    automatic | choose | record_later
notice  farm-worded copy, rendered VERBATIM
pens    the park's kid pens — one entry in automatic, several in choose, EMPTY in record_later
```

It rides on the destinations response rather than a route of its own because the birth form already
fetches and caches that catalog, and the pens it names are rows of that very catalog. A second
endpoint would be a second copy of the same bounded configuration, cached separately and able to
disagree with the picker beside it.

**Clients must not re-derive the mode** by filtering the park's sheds on `destination_stage`. The
mode also governs whether the WRITE accepts a freely chosen pen, so a client that computed its own
answer could offer a pen the birth then refuses.

`RecordBirthEventRequest` now declares `park_id` and `shed_id` as `required`. Both always were —
the handler 400s without either — but the schema promised a request the server refuses.

## The Record shed fallback

`tasks/domain.ActionKeyRecordShed`, added to `TemplateBirthKidAt` only when the kid is not already in
a K0 pen. Presence is derived from GROUND TRUTH — where the kid actually is — rather than from
whether the park had a K0 pen at raise time, which makes it self-healing in both directions.

Its answer is `"<shed_id>|<partition_label>"`, the same key the pen picker is built on. Completing it
runs in ONE transaction: the action row, the relocation
(`identity.RelocateGoatsToShedInTx` — identity event, location history, `goat_shed_partitions`
upsert, and the per-animal `goat.location.changed` outbox row), and the pen's `K0` tag. A failure
anywhere rolls the step back to pending.

**It is a placement completion, NOT a shifting.** The legacy shifting steps were deliberately removed
from this template because shifting is its own gated module (park-head approval, mandatory video,
verifier review). This step finishes recording where a kid ALREADY is, which is not a movement
anybody approves. Do not reintroduce a shifting dependency here.

**A pen that already carries a tag is left alone.** Overwriting one would let a placement action
silently re-purpose a pen somebody deliberately configured for another cohort.

## Rejected alternatives

- **Ranking multiple K0 pens automatically** (least-occupied / lowest-numbered). An `ORDER BY … LIMIT
  1` over rows that can legitimately tie fabricates an answer, and a least-occupied ranking flips as
  animals move. The operator picks.
- **Blocking the birth when no K0 pen exists.** A birth is a real event that already happened;
  losing it over missing pen configuration would make the herd register lie.
- **Silently correcting a wrong pen to the K0 pen.** See rule 5.
- **A dedicated `/app/counts/birth/placements` route.** See *Contract*.

## Known boundaries

- **Birth approval still cannot correct placement.** Goats are created at SUBMIT; approval only
  flips `goat_births.count_status`. Out of scope by maintainer decision; recorded here so it is a
  known boundary rather than an oversight.
- **`goats.age_band` is still left NULL for newborns** (`admin_goat_create.go`), unlike the
  reclassify and relocate paths which write it alongside the tag. Downstream survives via the `'K%'`
  prefix fallback in `herd_register_is_kid` — a silent dependency on the literal `K0` spelling.
  Filed separately; deliberately not fixed here.

## Machine enforcement

`make operational-location-guard`, `domain-event-architecture-guard`, `idempotency-writes-guard`,
`telemetry-guard`, `mobile-guard`, `offline-first-guard`, `scale-guard`.

Pinned by `TestRecordBirthEventRejectsANonKidPenWhenTheParkHasOne` (the adversarial case — it fails
on pre-change code), `TestRecordBirthEventNeverAcceptsAKidPenFromAnotherPark`,
`TestShiftingDestinationsCarriesTheBirthPlacementContract`,
`TestRecordShedStepAppearsOnlyWhenPlacementIsOwed`,
`TestRecordShedStepKeepsTheKidTrackOrdering`, and the Android `AddBirthPlacementTest`.
