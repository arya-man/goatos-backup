# Shifting Rewrite: THE SHIFT TYPE DECIDES THE TAG

Maintainer decisions 2026-08-20. This document is the canonical prose for the typed-shifting
rulebook; the code cites it from `backend/internal/counts/domain/shifting_type.go`, the raise
handler, the apply path, and migration `000177_shifting_type_tag_rules.sql`.

**What changed:** the raiser no longer chooses tag behaviour. The 2026-08-15 `stage_mode` toggle
(keep_current / destination_stage) is retired for typed raises. Every raise carries a `category`
naming WHY the animals move, and the category — the shift TYPE — carries a fixed rule for what
happens to the animals' tag. A raise a rule refuses is rejected at RAISE time with a farm-worded,
backend-owned reason: before the approval request, before the park head reads it, and before any
video is shot.

**What did NOT change:** the client still sends no stage of its own. `target_management_stage`
remains rejected as an unknown field; the server resolves every tag from the same catalog the form
renders plus the animals' canonical facts. Approve-first, mandatory completion video, the
unconditional close gate, and the feed projection coupling are all untouched.

## The six types and their tag rules

| Type | Tag rule |
|------|----------|
| `health` | Destination pen's tag on BOTH legs — the one type allowed to stamp a **clinical state** (`ICU`, `Quarantine`, …). A health shifting IS the health team acting, superseding the 2026-08-15 clinical raise-time lock FOR THIS TYPE ONLY. The return leg is just another health shift into a normal pen; there is no memory of the pre-ICU tag. |
| `growth` | Destination pen's tag, **FORWARD ONLY** along the authored lifecycle ladder (below). Backward or sideways is refused — "forward only" only means something if the system refuses. A sexed destination stage refuses animals of the other or unknown sex. |
| `breeding` | The tag **never changes** — a visitor placed with a mate. Any destination is acceptable because nothing is stamped; the pen briefly holding a foreign tag is an accepted temporary mixed-tag case. |
| `delivery` | Destination pen's tag, EXCEPT it never stamps the newborn stage (`K0` belongs to the kids — a mother entering the kidding pen keeps her own tag). Into an EMPTY untagged pen (the one-day recovery shed) she keeps her tag and the pen ADOPTS it ("mother only at that time"). |
| `spacing` | The tag **travels with the animals**; the WHOLE source pen moves ("half-half is not an option") and is left empty. The destination must already carry the same tag, or be EMPTY — an empty pen ADOPTS the group's tag. Anything else refuses at raise. |
| `flushing` | Non-pregnant females move onto flushing ration and adopt the **Flushing** tag. Females only. Destination must be empty (it becomes a flushing pen, adopting the tag) or already flushing — including an unconfigured pen whose residents are all flushing in fact. |

Three shapes underneath the six: **PROGRESSION** (growth, flushing — the animal changed, take the
destination tag), **TEMPORARY RESIDENCE** (breeding, delivery, health — a visitor; health is the
exception because being in ICU IS a change), and **CAPACITY** (spacing — nothing changed, the tag
travels and the PEN adapts).

## The growth ladder (authored data, not sort_order)

```text
K0 -> K1 -> K2 -> K3 -+- F2 -+- F2-Male   -> Buck
                      |      +- F2-Female -> Non-Pregnant <-> Pregnant
                      +------ F2-Male / F2-Female (K3 may split by sex directly)
```

- `animal_stage_lookup.sort_order` is DISPLAY order (it runs Mother → Milking → M0 → Pregnant →
  Non-Pregnant) and must never drive this rule; the ladder is its own explicit edge set in
  `growthForwardEdges`.
- The ONE permitted reverse edge is `Pregnant -> Non-Pregnant`: a pregnancy that does not hold, or
  completes, returns her.
- Sexed stages: `F2-Male`/`Buck` are male; `F2-Female`/`Non-Pregnant`/`Pregnant` are female. An F2
  pen's mixed group splits by sex across TWO raises, one per sexed destination.
- **Open decisions (deliberate absences):** `Mother`, `Milking`, `M0`, and `Warmup` are real
  vocabulary stages with NO growth edges. A growth raise touching them refuses until the maintainer
  places them on the ladder. `F2-Male -> Buck` is present per the maintainer's 2026-08-20
  confirmation; most fattening males exit by sale instead, which is not a shifting.

## Pen-tag adoption ("pen tags follow occupancy")

A spacing / delivery / flushing movement into an EMPTY pen tags THAT PEN with the arriving group's
tag. The decision is snapshotted at raise into `shifting_events.adopt_pen_tag` (migration 000177),
so the park head approves the exact pen configuration the apply will write. At apply,
`identity.ConfigureAdoptedShedCohortInTx` re-validates the promise under the shifting row lock —
the pen must still be unconfigured-or-matching and hold no disagreeing live occupant — and FAILS
THE WHOLE APPLY CLOSED (`ErrDestinationPenChanged`) when the pen changed between approval and
completion, instead of silently creating the mixed pen the raise-time check exists to prevent.
An adopted tag is a pen configuration, never a clinical state: health stamps the ANIMAL's state
and never re-tags the pen.

## Raise-time refusals (backend-owned farm copy)

The rulebook (`domain.ResolveShiftTypeDecision`) is pure Go — unit-testable without a database.
Exactly one of (decision, refusal) is meaningful. Every refusal carries a machine code and
farm-worded copy rendered VERBATIM by the phone (golden frontend rule — the client never composes
a reason). Codes: `invalid_category`, `destination_not_in_catalog`, `destination_tag_missing`,
`destination_tag_mixed`, `destination_tag_not_applicable`, `growth_stage_unknown`,
`growth_not_next_stage`, `growth_sex_mismatch`, `spacing_source_unresolved`,
`spacing_partial_group`, `spacing_destination_occupied`, `spacing_destination_mismatch`,
`flushing_requires_female`, `flushing_destination_mismatch`, `group_stage_unknown`,
`group_stage_mixed`.

Destination tag resolution reuses the same order the catalog advertises: the pen's AUTHORED tag
first, else the residents' single shared non-clinical stage, else refusal — never `rows[0]`, never
an invented cohort (agree-or-go-bare).

## Legacy compatibility

- A raise that OMITS `category` (a client predating the rewrite) is governed by the legacy
  `stage_mode` toggle, unchanged: absent means `destination_stage`, a present-but-invalid value is
  rejected (`invalid_stage_mode`), and a pen that cannot supply a tag falls back to keep-current.
- When `category` is present, `stage_mode` is ignored — the type decides.
- Rows raised as `spacing`/`flushing` keep their stored category forever (they record what really
  happened); the migration's Down re-adds the narrower CHECK `NOT VALID` and deliberately does not
  validate it.

## Where the pieces live

- Rulebook (pure Go) — `backend/internal/counts/domain/shifting_type.go` (+ `shifting_type_test.go`)
- Raise wiring — `backend/internal/counts/adapters/http/app_write_handler.go` (typed block runs
  AFTER the canonical idempotency hash and AFTER the source backfill, so derived facts never leak
  into idempotency identity; reuses the SAME destinations catalog the form renders)
- Goat facts read — `ShiftingGoatFacts` on the counts postgres repository (stage, sex, placement)
- Apply — `backend/internal/counts/adapters/postgres/shifting_execution.go` (pen adoption through
  the identity seam BEFORE the relocation, same transaction; `AllowClinicalDestinationTag` derived
  from the STORED category so a hand-crafted completion cannot widen it)
- Pen adoption — `backend/internal/identity/adapters/postgres/shed_cohort_adopt.go` (same write as
  the Counts Breakdown Stage editor; different guard)
- Schema — `backend/migrations/postgres/000177_shifting_type_tag_rules.sql` (category vocabulary
  +spacing +flushing; `adopt_pen_tag` snapshot column)
- Contract — `contracts/openapi/app-api.yaml` `ShiftingEventRequest.category` / `stage_mode`
- Android — `feature-counts/ShiftingScreen.kt` (six category chips; tag toggle removed, replaced by
  read-only backend-owned destination-tag context)

## Relationship to standing locks

- SUPERSEDES the 2026-08-15 tag-toggle rule on WHO decides for typed raises (the type decides, not
  the raiser); the toggle survives only as the legacy path for category-less raises.
- SUPERSEDES the 2026-08-15 clinical raise-time refusal FOR `health` MOVEMENTS ONLY; every other
  type still refuses clinical states, and an ADOPTED pen tag is never clinical.
- Leaves untouched: approve-first (2026-08-09), actions lead time, mandatory completion video, the
  unconditional close gate, feed projection coupling (2026-08-10/2026-07-27), and the second-gate
  atomic apply with `goat.location.changed` / `goat.stage_changed` events and their vaccination
  consumers.
