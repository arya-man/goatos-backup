# PC Care Fumigation: the pen spray (maintainer instruction 2026-09-30)

Status: accepted · Owner: pccare + pccaresop + permissions + workforce + penvisits + admin-web + Android

## What was asked

"In preventive care add one more category ... it should also be SOP driven ... this one can be
created today also by park heads, breeding director, health director and CXOs ... We mix 5 ml
Virufix liquid per litre water. This is the dosage, and spray in the shed ... they will assign the
task today or tomorrow or any time to that particular operator ... they will upload two videos per
shed ... one is of mixing and the second is of spraying ... there is nothing of feed and water."

Follow-up answers the same day: the category is called **Fumigation**; the Health Director "will
also have preventive care access from now on"; a fumigated pen **owes the next-day pen visit**; the
**verifier** reviews the two videos.

## Decision

`fumigation` is a sixth PC Care work category.

| Rule | Answer |
| --- | --- |
| Grain | PER PEN. Nothing is scanned: capture mode `task_proof`, the shape the vaccine-fridge check and the feed & water removal card already use. The pen's captures live in `pc_care_task_proofs`. A scan or a per-animal slot write on a fumigation task is refused (`422 not_animal_task`). |
| Captures | SOP-driven: the `fumigation` card of the published `pc_care.tasks` document. The seed is two compulsory live-camera videos, **Mixing video** then **Spraying video**, and the instruction "Mix 5 ml of Virufix liquid into every litre of water and spray the whole pen". The farm may rename, add, or swap a capture for a photo on `/pc-care/sops`, like every other card. |
| Feed & water removal | Never. The SOP validator refuses a removal applied to pen work; a create asking for one is refused (`422 feed_removal_not_applicable`). |
| When | Any date, today included -- there is no evening cutoff to miss. It appears on the assignee's Fumigation tab on its day, like every PC Care task. |
| Who plans | `pc_care.plan_fumigation`, category-scoped in the `pc_care.plan_trimming` shape: **park heads** (their own park, by grant scope), the **Breeding Director**, the **Health Director**. The CEO/CXO plans it through `pc_care.plan`. No planner gets `pc_care.execute` from this -- planning is not filming. The per-person access row is `pc_fumigation` (View / Configure). |
| Who does it | The assigned operator(s), `pc_care.execute` + named on the task, as for every PC Care task. |
| Review | The tenant verifier, verification category `pc_fumigation` under the one PC Care Verify tab. Approve completes; reject reworks the task (re-shoot, resubmit). |
| Pen visit | Owed. `fumigation` is in `PenVisitCategories`; the materializer maps `pc_fumigation` to the reason "Fumigation". The task's kernel clock closes when the visit is verified too. |
| Phone | A sixth PC tab, **Fumigation** (one word, four locales), spray-bottle icon. The task face is the generic served-slot list the removal card uses -- never the fridge pair. Plan / close / start again are offered only on a tab whose category the person may plan (the planner catalog's list), so a park head sees Plan on Fumigation alone. |
| Items | Virufix is a Configuration item (Consumables, ml) carrying the dosage in its notes. |
| Web | A Fumigation card on `/pc-care/sops` ("What the operator records, for the pen"; the removal's "work it applies to" never lists it) and a Fumigation column on Care Coverage. |

## The Health Director change

This REPLACES the standing line that `health_director` gets "NO Preventive Care permission". It now
holds `pc_care.monitor` (the PC Care board) and `pc_care.plan_fumigation`, and is offered the
Preventive Care module. It still does not hold `pc_care.plan`, execute, stock approval, or
`ProtocolWrite` (vaccination protocol authoring). `docs/agent-rules/business-medical-rules.md` is
updated in the same change.

## Storage

Migration `000457_pc_care_fumigation.sql`, every change additive:

1. `pc_care_tasks` / `pc_care_rounds` category CHECKs admit `fumigation`.
2. The seeded fumigation card is added IN PLACE to every stored `pc_care.tasks` version that lacks
   it (the weighing `000315` shape): no task is pinned to fumigation before this migration, so no
   task's behaviour moves. `000386`'s day-one document is kept verbatim as
   `sopseed/pc_care_v1.json`.
3. The `pc_fumigation` (and, for park heads and the Health Director, `pc_care` View) ticks for every
   person the access cutover already migrated, keyed on the role grant -- without them they would
   403 on the planner routes (the `000454` / `000245` defect). Ledgered, so Down removes exactly
   these rows.
4. The same ticks as the park head / Health Director / Breeding Director job defaults.
5. **Virufix** in Configuration › Items & categories: one item per tenant under the root Consumables
   list, unit `ml`, code `ITM-VIRUFIX` (the code the screen itself makes), the dosage in its notes.
   An existing Virufix is left alone; ledgered for an exact Down.

## Not linked yet (recorded, not done)

The SOP card's dosage is authored TEXT; it does not reference the Virufix catalogue item, and a
fumigation does not draw Virufix stock. Linking a card to an item and deducting stock per pen is a
separate decision (it would need the litres sprayed per pen, which nobody records today).

## Pinned by

- `pccare/domain`: `TestFumigationIsPenWorkWithTwoVideos`, `TestMigrationEmbedsTheSeededFumigationCard`.
- `pccare/app`: `TestFumigationIsPlannedByParkHeadsAndTheBreedingAndHealthDirectors`,
  `TestFumigationCarriesNoFeedRemovalAndMayBePlannedForToday`,
  `TestFumigationPlannerIsParkScopedByItsOwnGrant`,
  `TestPlannerCatalogOffersFumigationDesksOnlyFumigation`.
- `permissions`: the capability parity / CEO-floor / breeding-director count tests.
- Android `PcCareFumigationTaskTest`; admin-web `pc-care-model.test.mjs`.
