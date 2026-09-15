# Chrome interaction evidence

Target: http://127.0.0.1:4318/ — local prototype, authenticated production reference inspected separately read-only.

## Passed interactively
- Sales changed from >35 kg / ₹450 to >34.5 kg / ₹460 using 0.5 kg allowed shortfall; review modal held published values unchanged until Publish locally.
- Weighing then displayed Sales v2, >34.5 kg and ₹460. Reload preserved publication. Final demonstration values restored via publication to >35 kg, no allowance, ₹450; Weighing displayed v3.
- Director role made module numeric controls disabled; CEO restored edit controls.
- Common medicine share unchecked for Procurement: action picker omitted Select medicine. Restored share: Select medicine appeared and exposed treatment A/B catalogue.
- Chose demonstration treatment A. Removing shared access showed dependency warning; dismiss restored checkbox. Confirmed removal made Review & publish show Workflow needs attention with missing Health catalogue access. Restoring share recovered validity.
- Fixed text-edit event handling after real browser found draft title not persisting. Final oninput handler visibly updated canvas node; Save draft and publication retained selected action title.
- Valid workflow published locally as v1.
- Final simulator: answer 103 followed Otherwise to completion; 104 followed Match to Select approved treatment reference and showed selected treatment A. No live actions sent.
- Operator modal visually inspected at 390 x 844: question/action card, continue button, path trace and restart control fit a single scrollable column.

## Automated logic evidence
node judge-functional-tests.cjs: 38/38 passed after final readonly-list edit.
node --check app.js and enhancements.js: pass.

## Final visual judge
Independent UX agent reviewing final Chrome screens. Final status will be in judge-ux.md / PROGRESS.md.

## Final completion checks
- Created reusable question `Operator's observation`, saved draft, opened its definition and published locally. Apostrophe name remained intact; appeared in Procurement answer type picker and could be imported.
- After final pointer handler fix, clicked existing nodes and verified inspector selection changes. Added Arrival observation, connected action → question → end, validation passed. Deleted question, validation correctly failed; repaired action → end, validation passed again.
- Reloaded final JS and selected the decision node successfully. Dragged its x position from 260 to 310 (DOM style readback), then restored to 260.
- Opened all eight module settings pages; each rendered its module heading and three proposed numeric fields. Others Milk, People / HRMS, Monitoring switched to distinct settings. Saved Milk rules successfully.
- Independent UX judge personally inspected native Chrome screenshots for narrow operator preview, narrow sharing matrix and desktop graph. Final UX PASS after pointer source review.
- Final functional judge: 39/39 including executable pointer-event regression.
- Final Chrome tab retained as deliverable at http://127.0.0.1:4318/, Procurement graph with decision inspector, 80% canvas zoom, normal browser viewport restored.

## V2 refinement — 2026-09-15
- Created `Approved care kit` via new-item form; chose Health → Medicines → Approved treatments, purpose/description, shared with Procurement. Reload retained record.
- Procurement configured-item selector listed the new item by name alongside real registry-backed options. Selected it and renamed action to Prepare approved care supplies.
- Operator draft test with temperature104 selected Match branch, rendered care-kit purpose/unit/revision, and acknowledged to completion.
- Published local version2; item drawer then displayed exact action usage for draft and Published v2.
- Renamed item to Approved care kit · updated; stable reference retained, revision advanced2.
- Independent visual judge passed desktop expanded registry and upper item drawer. Narrow390px check caught .modal.wide overriding drawer width; fixed with .modal.item-drawer width:min(600px,100vw). Final DOM rect x0 width390, document scrollWidth390. Narrow native visual check in progress.
- Operator preview of Procurement Published v2 still showed original Approved care kit revision1 after master rename to revision2, confirming immutable published item snapshot in the actual UI.
- Health Adults/Kids Fever: switched group, opened connected flow, added a local Day4/Afternoon action, verified new group, removed test action restoring16 source steps. Published Adults snapshot v1; Operator saw published16 steps and disabled editing/publish controls.
- Independent UX judge passed narrow390 drawer sharing, exact linked Draft/Published action references, snapshot wording, reachable sticky footer. Responsive override reset afterward.
- Final Health desktop/mobile independent visual PASS: connected day/session columns, source attribution, raw medication fields, responsive controls, bounded horizontal course canvas. Narrow override restored and final Health flow left visible in Chrome.
- Final executable test totals:39 +29 +21 =89/89. Browser console two generic async message-channel listener errors observed, no app stack; no failed product interactions attributable to them.
