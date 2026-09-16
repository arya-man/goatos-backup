# Configuration and SOP requirements — authoritative working reference

Updated 2026-09-16. Anonymous requirements from the twelve voice-note records, chat screenshots, and the maintainer's written clarifications through 20:14, including park/pen onboarding. This document distinguishes requirements from implemented product behavior. Earlier event-rule dashboards and procurement-first generic pages are rejected designs, not acceptance evidence.

## Product purpose

One understandable authoring experience to set up reusable items and settings, organise arbitrary categories/subcategories, share them between features, and define what operators must do through SOPs. A master SOP can contain smaller SOPs with dependencies between their steps. Generic means usable across departments and purposes; it does not mean forcing every feature's domain rules into a generic scalar table or inventing an event-automation product.

A CEO must understand the task and consequence of a control without an accompanying explanation. Use familiar names, business examples and progressive detail. Preserve the existing app shell, buttons, accordions, submenus and department screens. Keep generic authoring neutral. A procurement scenario belongs in a Procurement SOP or an explicitly selected example.

## Evidence and precedence

The latest written clarification is authoritative over earlier interpretations. voice-requirements.md preserves all twelve anonymous machine-transcribed note records, uncertainties and provenance. The original audio has not been independently relistened in the later review passes. Screenshots are source evidence, not permission to save or publish production data. Feature inventories must inspect current frontend, Android, backend and the actual goatos-stg Cloud SQL state. Source code, deployed schema, live UI and installed Android behavior are separate claims.

Every capability must be marked Implemented, Partly implemented, Proposed, or Unverified with evidence. Absence from an example is not absence from scope. An example is not proof of current implementation. Unverified is not the same as missing.

## Generic shared items and settings

- Create any item, with arbitrary categories and subcategories (examples: Electrical appliances / Fans; Clothes / Sarees; Supplies / Needles).
- Each feature family can also own entity registers, not just dropdown items. Park, pen, pen partition and animal identity are Counts-owned; vendors/sellers and trucks are Procurement-owned; buyers and sales policies are Sales-owned; feed catalogue and stock lots are Feed-owned; vaccines and batches remain Preventive Care-owned; symptoms, diseases and protocols remain Health-owned; people/roles and approval policies remain their respective operational owners. Generic configuration must make CRUD, parent/child relationships, archive guards and sharing visible without flattening those domain contracts.
- Choose one or many consuming departments at category, subcategory and item level. Show effective availability and why. These links do not grant actor permission, stock access, or authority to prescribe treatment.
- Reuse the same item in actual SOP questions, choices and actions. Keep stable identity, impact review, archive behavior and existing published instructions intact.
- Reuse typed values such as weights, prices, times, durations, capacities and percentages where applicable, preserving units, dimensions, ownership and effective periods.
- Inventory identity, stock/batches, feed catalogues, clinical protocol rules, sales policy and display text have different underlying contracts even if authoring is consistent.
- Configured settings can be read by another feature. Example: Sales eligibility/price settings informing Weighing views. Identify whether each requested rule is currently persisted, hardcoded, absent, or unverified.

## SOP composition and execution

The required concept is a master SOP built from smaller SOPs, with required prior work controlling which later steps may execute. Procurement can combine Animal procurement, Transit and Warm-up. Other departments must be able to compose their own work. Parallel preparation where specified must not be misrepresented as a purely sequential chain.

SOPs include questions/answers, item/config references, evidence, decisions, approval requests, repetition/time rules and child SOPs. Distinguish completion, submission for review, approval, rejection and rework. Use the real role/permission and verification architecture; do not equate an operator checking a box with a CEO approval. Active work retains the published definitions it started with. Missing children, recursive composition and inaccessible references must be handled.

A separate Workflow links / Run insights / generic event-rule product is not the requested solution. Trying a SOP belongs with its authoring, with plain-language pending/blocked/completed explanations. Real scheduling, reminders and dispatch must not be implied by a browser-only simulation.

## Procurement acceptance example — not an exhaustive specification

1. At the seller's farm, the operator may assess 100 offered animals even when the purchase target is 70. Capture each offered animal using the existing production Procurement Q&A; inspect that form rather than recreating it from assumptions.
2. Reviewers approve/reject candidates and finalise the selected cohort (example 70). Preserve candidate identity and decisions; rejected animals must not silently enter the selected group.
3. Tag selected animals. Use applicable vaccination/protocol work, including the supplied ET+TT/PPR examples, according to configured domain rules. The phrase resembling 'chocolate injection' is unresolved and must not be guessed into a vaccine or medication.
4. Keep the selected animals at the seller's farm for a configured holding period (example 15 days). Record the start and eligibility to proceed; do not treat a duration as elapsed merely because a step was clicked.
5. Before transport, repeat relevant inspection/review for the selected animals. Some previously approved animals may drop out. Only the newly approved subset boards; demonstrate lineage from offered to selected to loaded to received animals.
6. Sanitize the truck. Take sufficient familiar feed for configured travel days (example 3 or 4) plus configured warm-up days (example 10 or 14). Feed quantity must use the applicable ration and units; do not invent a universal per-animal quantity or merge different feed identities.
7. During travel, perform checks at configured intervals (example every 3 hours), with required photos/videos. Retain feature-specific proof requirements and explicit review/late/missed behavior. No global video cap.
8. After transit has started and before arrival, destination preparation includes emptying/sanitizing sheds and preparing water/ORS. This work can overlap travel. It must not begin before its prerequisite and required preparation must gate arrival.
9. At arrival, start the configured warm-up process. Gradually combine familiar carried feed with the farm's existing/experimental feed because the animals should not switch immediately. Mixture ratios and schedules were not specified here; use existing configured rules or mark the missing definition instead of inventing a clinical/feed schedule.

The values above are examples supplied by the maintainer, not verified live configuration defaults. This flow also draws on vaccination, feed, inventory, approvals and evidence features; it is still a Procurement SOP composition.

## Health acceptance example

A symptom set can lead to a disease assessment and an appropriate configured SOP/treatment/review path. Approximate '30+ symptoms' and '100–200 diseases' are scale expectations/examples, not verified catalogue counts. Inspect existing symptom/disease/protocol/rule code and real data. Preserve multi-symptom reasoning rules, clinical ownership, dose/route/duration, evidence and approval permissions. Do not fabricate diagnoses or treatment schedules. Admin/CEO/CXO/Director approval needs depend on configured roles and the existing access model, not a blanket claim that all roles approve every action.

## Sales and shared Weighing settings

Requested authoring includes eligibility selection and prices by species (goat/sheep), breed and sex, plus common minimum weight and permitted error margin (examples 35kg and 200g). These may be shared with Weighing for graphs/counts. Check current implementation per dimension. A reporting threshold is not automatically an enforced sale prohibition. A valuation rate is not a recorded sale price. Keep derived values, dated market observations and actual transactions separate.

## Vaccination screenshots and integration boundary

The existing Vaccination plan already has a specialised authoring world. Screenshots show vaccines, repeat schedules, procurement holding, age threshold, claimed prior-dose handling, breeding/fattening purpose, first/second waves by species, proof mode, publish impact/capacity and automatic safety rules. Read-only labels matter; do not turn them into arbitrary editable generic fields.

Visible screenshot examples: ET+TT/PPR/FMD/HS/Goat Pox/Sheep Pox/Z1+Z3/Blue Tongue labels; kid/adult boundary16weeks; prior doses Count them/Ignore them; breeding/fattening selections; first wave and4week wave gap; per-pen/per-animal video choices; current cap200/day; publishing affects future work while running work stays unchanged. The displayed draft v10 is not evidence that it is published.

Visible automatic rules: maximum3 vaccines per visit; live/live spacing4weeks; live/killed and killed/killed2weeks unless same-day rules allow; booster at least3weeks from first dose; pregnancy month4/5 defer and catch-up2weeks after delivery; sick/treatment/recovery/ICU/quarantine deferrals; batching up to1week with safety window taking priority. These are transcribed product settings, not independent veterinary recommendations. Preserve the authoritative protocol and validation rather than duplicating editable copies.

Decision to validate: share vaccine inventory identities and reference the versioned vaccination plan from an SOP while retaining specialised clinical configuration. Do not claim all vaccination logic can already be collapsed into the generic authoring model.

## Review contract

Before redesign: inventory ALL existing features across frontend/backend/Android and actual staging schema/data, including features not named above. Document evidence, constraints and uncertainties. Then implement a local prototype with honest boundaries. Conduct two independent review/fix passes covering:
- All twelve notes, screenshots and these clarified requirements; generic capability versus examples.
- Existing source contracts and real staging ownership; current source versus deployed state.
- CEO comprehension and existing visual/UI patterns on desktop/mobile.
- Actual operator paths: per-animal selection, approvals/rejections/rework, prerequisite/time/parallel gates, proof, immutable versions and source identity.
- Meaningful tests and browser interactions; do not equate passing narrow tests with complete requirement coverage.

No production database write, migration, push, merge or deployment is authorised by this prototype task. Keep all reference documentation anonymous and inside the prototype.

## Additional voice V12 and local management inspection

Customer onboarding must support adding parks and pens, including pen names and capacities. Make the setup simple enough to avoid a long configuration exercise. Reuse existing animal/pen/location CRUD where implemented. The user explicitly requested the actual application running locally against OCI to inspect those screens; this is separate from the real goatos-stg audit. No farm records should be fabricated by the design prototype.
