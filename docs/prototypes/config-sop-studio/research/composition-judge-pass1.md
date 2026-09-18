# Integrated composition judge — pass 1

2026-09-16. **Changes requested. No approval.** Independent read-only review of the actual index script stack, sop-composition.js, business-examples.js, sales-policy-preview.js and procurement-current-source.js against requirements-consolidated.md and feature-requirement-matrix.md. Source and actual staging evidence remain distinct; backend index at aa057 remains applicable to frontend main e06d27 because backend diff is empty. No implementation changes or production writes performed.

## Actionable findings

### P1 C01 — a nested child’s own stages are silently skipped

`sop-composition.js:33–36` enters a child by its graph entryNodeId and treats graph End as complete. It never executes or gates on that child definition’s stagePlan. Therefore a master can include another composed SOP containing mandatory holding/approval/preparation stages and return successfully without performing any of those stages. The same omission applies when compositionRunChild at line67 launches a staged child. Compilation pins stagePlan but pinning alone does not enforce it.

Actual-stack repro using the existing composition harness: child graph Start→End plus mandatory child stage; parent Follow another SOP→End; reach child End and renderSim. Result is parent End and childResults recorded, with the mandatory stage never started. This contradicts S01/S03/S04. Make the runner execute child stages or explicitly reject nested staged children until supported; never silently certify them completed. Parent procurement graph is also only one instruction, so ordinary Try SOP can finish without its stage plan. Integrate ordinary try with the stages or explicitly prevent that misleading success path.

### P1 C02 — rerunning a child retains approval of the old answers

`sop-composition.js:50–51` childFinished overwrites result and sets childDone but does not clear approved/approvedBy. The UI keeps Follow child SOP enabled while status is started, including after approval. Complete child→approve→follow child again with different answers→complete stage succeeds under the earlier approval. Actual model repro returned approved=true and the changed result. Reset approval when a result is replaced, or prevent replacement without explicit reopen/review. Test reject/rework and reviewed-result identity, not only the role string.

### P1 C03 — requested animal cohort progression remains explanatory text

`business-examples.js:10–16,23` uses Instruction nodes for tagging, reinspection/boarding, arrival, feed and warm-up. “Animal groups & settings” shows static100→70→subset text; there is no per-candidate selectable cohort, rejected exclusion, reinspection attempt, boarding subset, or received identity reconciliation in this mock. The imported inspection asks one animal’s form; it does not supply the requested multi-animal progression. The disclaimer accurately says connected handoff is proposed, but does not satisfy the user’s requested interactive example (P02/P03/P04/P08).

Add a bounded local cohort demonstration using stable fictional IDs, separate recommendation/reviewer decision and successive selected/boarded/received sets. Preserve rejection/history and show the integration boundary. Do not invent a production bridge or imply observed62/60 staging counts are the requested100/70 example.

### P2 C04 — shared example durations are disconnected duplicate settings

`business-examples.js:3–4` inserts four typed config values. Line17 separately hardcodes15/3/10/3 into stage wait/check fields, and line20 merely records exampleSettings keys. Neither compile nor run resolves those item values. Editing Seller holding period in shared configuration therefore does not change the plan, while editing the plan does not change the item. This is exactly the shared-config→SOP consumer behavior the voice notes requested. Bind stage fields to stable typed config IDs and pin resolved values, or remove the unused duplicate entries and visibly limit the example to per-plan settings. Test changing the shared setting, starting a new run and preserving a prior run.

### P2 C05 — proof/feed/clinical requirements are mostly descriptions, not enforced example inputs

Only periodic transit checks use a file control (names/type only, explicitly local). Truck sanitation, vaccination evidence, shed readiness and warm-up proof are Instruction text; advancing the child acknowledges them without collecting the stated evidence. The feed carry formula is displayed but no ration/feed identity/quantity inputs or computed output exist. Health links to existing instructions/configuration and Vaccination links the real plan, but no selected immutable plan reference is stored in the example. Clinical non-invention is correct; still distinguish references actually bound from prose requesting future binding. P05/P06/P08/H01/V01 remain partial/unverified, not pass. Implement local structured references/required proof metadata and feed calculation only from explicitly supplied ration/unit inputs; retain specialized domain ownership and unresolved mix rules.

## What is concretely implemented

- Stable workflow IDs and child definition copies exist; published child v2 remains pinned after draft edits. Missing child and simple indirect-cycle checks work in current harness.
- Stage-plan started/completed/approved dependencies, parallel transit/preparation, wait clock and periodic check-window enforcement exist. These are local practice, with explicit no-production-jobs/no-upload wording.
- Specific approval role selection and edit-role guards exist. This does not yet model submission/rejection/rework or production capability authorization.
- Current procurement reference is separate from old seed and the example imports it explicitly, retaining previous SOP in the archive. The authoritative exported reference has7 load fields and40 animal questions; no live candidate answers were imported.
- Sales has interactive species/breed/sex rows, common/per-group minimum, tolerance, proposed price, duplicate validation, save and a shared Weighing test. It correctly separates reporting cards, valuation and actual sale prices. It is a separate local proposal, not current production enforcement or a generic configuration-ID consumer.
- Generic authoring is neutral; department examples are deliberately selected. Existing specialized Health/Vaccination and other inventoried production features must remain preserved, not recreated.

## Matrix disposition

| IDs | Pass-1 disposition |
|---|---|
| G01–G03 | Existing focused tests pass; category/item reuse remains present. No fresh browser proof in this pass. |
| G04/A02 | General typed-value tests pass; example stage settings fail integration C04. Sales sharing demonstrates a separate local policy object, not universal shared-ID binding. |
| G05 | Generic landing/example separation improved. CEO comprehension and responsive visual proof pending independent visual review. |
| S01–S04 | Partial; child graph execution/pins and stage primitives pass focused tests, nested-stage omission and stale approval block acceptance. |
| P01 | Imported current source present; exact browser load/animal form preservation still needs pass-2 proof. |
| P02–P04 | Partial; inspection reused, cohort and reinspection interactive behavior missing, wait primitive exists. |
| P05–P06 | Partial; formula/instructions and periodic metadata checks, not full feed/proof/scheduling path. |
| P07 | Stage model demonstrates start-dependent parallel readiness and completion-gated warm-up; no bound load identity/cohort runtime. |
| P08/H01/V01 | Domain ownership respected in wording; concrete composed consumer references and requested execution remain partial/unverified. |
| A01 | Interactive local dimensioned proposal present; production enforcement expressly not claimed. |
| X01 | Existing permission tests pass; C02 invalidates approval-result integrity. |
| X02 | Source/STG boundaries documented; no backend/Android integration or production adoption claim accepted. |
| X03 | Focused suite below; visual/browser verification belongs in separate attributed receipt. No production availability certification. |

## Tests and evidence

Executed every24 judge*.cjs from the final integrated directory at this review point:23 pass,1 fails. Receipt: composition-pass1-checks.txt. judge-orchestration.cjs fails because it assumes the removed standalone orchestration module is loaded; decide whether to retire/replace that obsolete product test, not reintroduce the rejected screen. Focused judge-sop-composition.cjs passes but misses C01/C02. Independent adversarial actual-stack probe `/tmp/judge-composition-adversarial.cjs` reproduces both; temporary probe is evidence, not a packaged regression fix.

No browser interaction or new screenshot certification performed by this reviewer in this pass. No real backend/Android runtime tests were run; known mobile failure strings therefore remain unverified rather than silently passed. Require second independent pass after fixes, final-stack tests and impacted browser paths, with remaining partial requirements stated plainly.
