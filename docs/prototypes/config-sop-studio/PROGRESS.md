# Config / SOP prototype progress

## Scope
Standalone interactive local design prototype. Modules + common configuration + shared SOP building blocks + visual branching editor + operator simulator. No production code edits, merge, push, or deploy.

## Done
- Inspected supplied screenshots and live authenticated Chrome SOP library, full sidebar, and Health Config.
- Two independent agents inspected design system, SOP/Android/backend contracts and relevant last month history.
- Recorded design and module configuration inventory in DESIGN.md.
- Started isolated static server: http://127.0.0.1:4318.

## Pending
- Builder completion.
- Independent source/logic and UX judges; fix findings.
- Chrome interaction tests and visual inspection after final edits.
- Final handoff with mock open in Chrome.

## Exact verification so far
Read-only live routes: /procurement/sops?scope_mode=company and /health/config?scope_mode=company. Loaded real UI with CEO/CXO session. Expanded all sidebar modules; confirmed named module leaves. No live configuration changed.

## Known limitations
Production SOP runtime currently supports conditional forms rather than arbitrary action graphs; live medicine option-source endpoints are incomplete. Mock simulation cannot certify phone/backend integration. Settings are proposed design examples.

## Metrics
Performance claim: none. Backend/API latency comparison: not applicable to isolated static prototype.

## Judge status
UX and functional judges pending implementation and browser evidence.

## Source / deployment state
Reference checkout: goatos at c09c95643 with pre-existing changes preserved. Prototype outside repository in tmp/config-sop-studio. Deployment: local HTTP server only, bound to 127.0.0.1:4318.

## Completion
All planned prototype work complete. All eight module config pages, separate Others features, common sharing/libraries, visual graph editing, branching simulation and local publication delivered.

Final proof: 39/39 independent functional checks including pointer events; syntax checks pass; real Chrome E2E and independent mobile/desktop visual inspection pass. Details: BROWSER-QA.md, judge-functional.md, judge-ux.md. Browser-discovered text-persistence and node-selection defects fixed and retested.

Judge status: Functional PASS; UX PASS. No remaining blocking mock defects. Remaining limitations are production backend/Android/catalogue integration and richer centrally versioned reusable field schemas.

Pending: none for the requested local prototype. No main/staging promotion requested or performed. Local server remains running at 127.0.0.1:4318; final mock is open in Chrome.

## Refinement v2 — active
User clarified missing standalone Items Config hierarchy. Scope reopened: full local item registry editor (Vertical/Category/Subcategory/Item, descriptions, purpose, unit, sharing), stable item references in SOP actions and catalogue questions, validated executable local workflow definitions, honest compiled-definition preview/export, and independent code/visual regression judges. Prior v1 pass did not cover these requirements. Baseline source copied to baseline-v1; existing browser drafts preserved.
Pending: v2 registry + integration, code judges, Chrome CRUD/share/graph/simulation regression, visual judge.

## Refinement v2 verification update
- Standalone Items registry and stable linked actions implemented. Catalogue questions store stable item IDs; published definitions and running simulations retain resource snapshots.
- Judges found and fixed: catalogue-only unshare impact warning; unavailable catalogue condition selection; outdated action library name; exact item-to-SOP step links; misleading archive wording; hierarchy create prefill.
- Local tests: legacy regression 39/39 pass; v2 functional 29/29 pass. Source UX judge pass. Independent desktop registry visual pass; drawer/mobile final check ongoing.
- Chrome demonstrated creation of Approved care kit under Health/Medicines/Approved treatments, sharing Procurement, selection in workflow action, >103 branch at104, completion, local publication v2, and visible draft/published item usages.

## Health reference scope addition
User provided live Health DB Google Sheet Adults SOP and Kids SOP. Source judge verified both tabs through connector. Existing repository normalized snapshot dated2026-07-30 is incomplete for long courses and must not be used as full live fidelity. Add bounded live Fever example (Adults/Kids), day/session schedule, ordered instruction/medication rows, stable item references, and alternate course-flow view. Clinical values kept as source data, not inferred instructions. Pending this addition, fresh code and visual judges, final Chrome handoff. No production operations.

## Final refinement receipt
Status: requested local refinement complete. Items registry, shared item/config availability, branching Q&A/actions, immutable local compiled definitions, and verified Adults/Kids Fever course authoring are implemented.
Tests after final code changes:39 legacy +29 registry/compiler +21 Health =89/89 passed. JavaScript syntax passed. Independent functional judges PASS; independent desktop/mobile UX judges PASS including Health source course.
Browser final proof: desktop course flow is visible in Chrome at #Health/Treatment%20courses; viewport restored; tab marked deliverable. No known blocking mock defect. Browser console contained two asynchronous message-channel listener errors without an app stack; all tested product interactions succeeded. No product runtime failure strings observed.
Remaining scope: live backend/Android integration, actual task scheduling, nested SOP execution, and other Health diseases beyond bounded Fever example. No deployment or production change performed. Source reference checkout SHA remains c09c95643 (not modified); prototype files outside Git tracked by judge receipt SHA256s.

## V3 active — diagnosis engine and reusable data sources
User requested design revision after inspecting the supplied health-sop reference repository. Health must expose a parallel rule engine (four class packs, ranked multi-problem proposal, evidence, emergencies and Director confirmation), distinct from the general single-path SOP graph. Source pinned1db838d7e01642b0b62f478be100270dbe3c2279. Builder and code judge independently ran reference oracle260/260; this is source proof, not production integration.
Root scope: reusable data-source library (item catalogues and non-inventory records), source access, source-backed question/action selectors, validation and versioned snapshots. Include vaccines→Health and non-health examples vendors/pens. Preserve existing drafts and explicit item access.
Agents: mock_builder Health implementation; sop_audit independent code/architecture and tests; ui_audit independent source/desktop/mobile UX.
Pending: implement, code proof, cross-module browser workflows, visual regression, final Chrome. No production edits/deploy.

## V3 combined implementation and functional receipt
Implemented Health Diagnosis with four class packs, concurrent source proposals/evidence, visual AND/OR rule draft editor, local Director decisions and review queue. Added reusable named item/record sources with owner/type filters, source access, question/action selectors and snapshot preservation. Adopted Claude's owned/read-from organization with real company/source metadata. Comparison decisions recorded separately, including deferred ideas and rejected unsupported claims.
Independent functional judge:122/122 assertions (39 legacy,29 registry/compiler,21 courses,33 v3), plus independent source result fidelity260/260. Source SHA1db838d7e01642b0b62f478be100270dbe3c2279. Prototype is outside production Git; per-file hashes in judge receipt. No production deployment or API mutation. Visual judge desktop diagnosis and mobile rule editor passed; Sources/Procurement final review pending.
Health scenario replay is not an arbitrary-input diagnostic evaluator. Changed rules require acceptance evidence outside this prototype. Production sync, Android execution and scheduling remain integration work.

## V3 final combined receipt
Final functional rerun after inherited visibility fix:125/125 assertions passed; reference oracle fidelity260/260. Code judge PASS. Independent UX judge scoped PASS: desktop Diagnosis/editor and Procurement;390px rule editor, Sources and Procurement table. Root verified horizontal keyboard scroll0→40 and restored desktop viewport. Final loaded Health Diagnosis screenshot inspected; correct screen, no loading/login/error, tab marked deliverable in Chrome.
Final fixes: inherited collection access now appears in item tags/counts; diagnostic confidence badge labeled separately from Director confirmation; responsive dependency table has keyboard focus and scroll hint. See judge-v3-functional.md and judge-ux-v3.md. No known blocking defect in reviewed mock scopes. No production deploy, merge or Git promotion.

## Canvas usability rebuild — user rejection reopens scope
The user correctly rejected detached-step creation and dropdown-based wiring as unlike Jira/draw.io. Previous functional and visual receipts do not establish direct manipulation usability. New scope: canvas input/output ports, connection dragging and click alternative, path-local insertion preserving routes, readable condition labels, automatic source assignment when inserting decisions, safe deletion/repair, undo/redo and pan/zoom/fit. Keep existing user drafts and immutable publications. Root simplifying inspector wording; builder owns new canvas renderer; independent code and actual-browser UX judges required before final handoff.

## Canvas implementation verification in progress
Builder implemented direct ports, click/drag connections, accessible HTML path-plus buttons, connected insertion, safe deletion, undo/redo scoped per SOP, pan/zoom/fit, persistent toolbar and compact navigation. Root added readable comparisons/choice values and selectable validation findings. Functional judge caught a syntax error in root validation-template code; fixed and node --check passed before browser reload. Initial canvas judge15 assertions passed; event tests/final UX pending.
Browser root created separate Counts draft 'Daily count • canvas review', preserving existing user drafts. Clicked Otherwise path plus, inserted action, renamed Recount the pen and validated successfully without destination selectors. Final direct reconnection/deletion/operator-path verification delegated to UX judge.

## Canvas functional judge receipt
Independent canvas18/18 tests passed; prior suites125/125 rerun passed, combined143/143. Tests exercise actual bound pointer handlers as well as mutations: connected Next/Match/Otherwise insertion, ports, cycle rejection, readonly, source dependency protection, deletion repair, per-SOP undo/redo and pinned publications. Remaining unused branch nodes after removing a decision are intentionally preserved and validation-flagged; no silent user-data deletion. Final live-browser UX signoff still pending; this is not yet a usability completion receipt.

## Canvas desktop final receipt
Independent desktop UX PASS: direct click-port reconnect and Undo, accessible edge-plus insertion, deletion rejoining path, validation, Fit and both operator paths to completion. Root additionally verified real CUA pointer drag: Recount node left325→618.245px; dragged Otherwise output to approval input, observed changed edge label, then Undo restored recount branch. Root adjusted only test-draft wording/positions for a meaningful count-check example and validation remained green. Final desktop screenshot inspected and Chrome tab marked deliverable at #Counts/Editor; original user workflows preserved. Root390px screenshot inspected but mobile authoring usability is NOT signed off (toolbar is tall); desktop is the reviewed delivery scope. No production changes.

## Drag smoothness fix
User screenshots exposed card moving while arrows stayed at old position. Root cause: canvasUpdateWirePositions destination callback shadowed the source-node variable, comparing candidate id to its own outgoing reference. Corrected explicit source→destination lookup. Drag now caches incident edge elements and scale at pointerdown, coalesces pointer events with requestAnimationFrame, uses translate3d, updates only adjacent edges, flushes final pointer/cancel position, and avoids rebuilding graph on release. One undo transaction per drag; touch-action disabled on cards.
Independent regression: old code reproduced missing edge update, new code verifies exact endpoints/hit paths/plus positions. Deterministic120 input moves queue1 paint;0 path writes before frame,2 adjacent path-pair updates after; unrelated edge unchanged. These are work-count metrics, not measured browser FPS.23 canvas+125 legacy=148/148 passed. Browser actual Finish drag on user's Others route verified all3 incoming endpoints matched moved Finish; Undo restored user's layout. Chrome error log empty; current tab reloaded and marked deliverable. No production changes.

## 2026-09-15 — Published procurement questionnaire represented as editable graph
Scope: replace generic procurement demo with live published Animal Purchase Inspection v5: 7 load fields and 38 animal fields. Root read production library/detail/editor in Chrome without saving; seed supplies backend keys not visible in editor. Exact live DSL unavailable, so provenance distinguishes reconciled snapshot from byte-exact export. Two extra load keys are local placeholders. Face/body/udder options and conditional controls verified in live UI. Preserve awkward quantity Yes/No field as authored.
Implementation in progress: same-document List/Flow, typed question insertion, structured options, shared-source dropdowns, proof constraints, only-if skip/rejoin, compound decision checks. Existing drafts preserved. Production is untouched. No push/merge/deployment requested.
Tests: prechange148 passing; new questionnaire8 passing. Source judge currently found missing-unit false positives and nested condition dominance false positive; fixes pending. Visual/browser final pending. Judges: mock_builder implements; sop_audit source/contracts; ui_audit interaction/rule review. No final signoff yet.

Verification update: all seven existing/new VM suites green after rule validation fixes (23 canvas +39 base +21 health +23 questionnaire/source +9 compound rules +29 registry +36 shared/health =180 assertions). Browser local: source45 fields rendered; List rows55 include graph gates/start/end; section navigation readable85%; Validate reports Ready to test; required empty load answer rejected; Vendor dropdown explicitly labels demo records. Production returned to library and re-read still publishedv5/38questions/7steps. No production values edited/saved/published. Final independent visual judge and editable proof settings pending.

Visual corrections: section List navigation now scrolls its actual canvas container; graph SVG expands to contain long production questionnaire (old fixed6000px clipped final connectors). Canvas23 regression checks pass after SVG change. Proof settings now editable (capture kind/min/max) with required/optional coherence; targeted judge running. Production library readback unchanged publishedv5; no production save/publish performed.

Final functional receipt: questionnaire28/28 including new proof controls; rules9/9 rerun; canvas23/23 after SVGextent fix; base39+health21+registry29+shared36 =185 focused assertions total. Source and functional judges approve tested local scope. Chrome checks use local mock only; source library read-only. No backend connection/publication or deployment. Visual judge final receipt pending momentarily.

Final visual judge: PASS scoped desktop imported List/Flow and section selector. Verified verdict List fields and connected Flow after SVG fix; Udder85% shows female conditional split/rejoin and proof properties. Chrome mock retained as deliverable. Remaining limitations: local prototype only, demo reference records, two production keys not exposed in UI; no full byte-exact production DSL export. No live changes.

## Page-based mobile preview correction
User identified question-by-question Back does not match Android. Previous completion claim covered local graph tests, not mobile parity. Read-only inspected origin/main03ebe28194bc86777c5d24d811e8910e60fb9f63 git objects (dirty/stale working checkout untouched). Actual Android AnimalPurchaseViewModels.kt945–1001,1369–1390,1559–1575 and AnimalPurchaseLoadDetailScreens.kt246–325: separate load form, five animal pages; Next validates current visiblepage; Previous retains withoutvalidation; changes prune hidden answers recursively inclOther/multi; Save checksall and jumps failure. No installed-device test performed. Builder implementing page-preview.js; independent UIagent tests planned; production not touched.

### Page preview final verification
- Final `node judge-page-preview.cjs`: 14/14 passed; `node --check page-preview.js` passed.
- Browser checked local Procurement preview: five animal pages, separate load details, Previous disabled on first page, sticky navigation footer visually inspected.
- Browser selected Female -> pregnant Yes -> Male -> Female: conditional field disappeared and returned empty, confirming hidden answer pruning.
- Chrome connection became unavailable; final visual verification used in-app browser at the same localhost route. Existing user Chrome run was not refreshed.
- Page mode validates grouped questions; Run full graph tests authored actions separately and Return restores page answers. No live actions execute.
- Android parity verified from origin/main source, not an installed APK. Production unchanged; no push/deploy.

## 2026-09-16 CEO editor usability closure
Scope: question-owned branch rules, distinct answer validation, searchable/create question selection, full question/shared-list insertion, canvas regression, retained List/Flow and page/back behavior. Local mock only; no production mutation or promotion.
Baseline: all nine judge-*.cjs scripts passed before changes (canvas23, core39, health21, pages14, back, questionnaire28, operators9, v2 29, v3 36).
Owners: branch_fix implements branch authoring; usability_fix picker/insertion; judge independently audits canvas and final integration; root browser E2E/visual inspection and integration receipt.
Pending: final code review, boundary tests and Chrome walkthrough after edits. No claim of completion until final result tested.

## 2026-09-16 — Authoring discoverability refinement
- Local-only `usability-picker.js/css`: question title at insertion, searchable question types and available shared lists, clear Condition / Branch operator label, searchable existing-question source picker with inline custom question creation.
- Custom questions are wired before the selected condition / owner question; existing successors remain intact. Page/scope metadata inherits from the surrounding source node. New numeric questions have no invented 0–100 limits.
- Focused proof: `node --check usability-picker.js`; `node judge-usability-picker.cjs` — 4/4 passed (menu/shared sources, custom numeric defaults, source-question splice, search/create affordance).
- Final integrated browser/visual review owned by root and independent judge. No production change or promotion.
- Visual judge Chrome tab 456171051 at 1728×823: custom-question search modal and empty-search/Create custom affordance verified. Found singleton source selector omitted searchable control; fixed. Insertion modal previously hid workflow buttons below fold; CSS now uses 850px width, four-column question types and three-column source cards; screenshot visually verified all 10 types, shared lists and Condition/Action visible without scrolling.
- Root E2E caught rejected canvas insertion mutating the selected existing question. Guarded both `insertTypedQuestion` and `authorInsert` with existing-node identity checks plus managed-port precheck. Focused tests now 7/7 including refused insertion preserving existing question exactly.

### Final reviewed local preview receipt
- Code judge signed off scoped rule/picker/canvas paths after loop, stale config, generated-order, fallback deletion and canceled-drag fixes. Final independent rules15/15, canvas27, picker7, pages14 and Back pass. All11 judge scripts passed during integration; final rule deletion guard rerun by judge.
- Browser Chrome E2E on isolated4319 origin: created custom numeric temperature question, set Fahrenheit, >103 and <=103 branches, created custom ActionA/B, verified104->A,103/102->B, empty->fallback. Confirmed saved103 survives reload, List/Flow samegraph, node drag endpoint attachment and zero captured browser errors.
- Visual judge checked searchable question picker (including emptyresults/create), insertmenu at1728x823 and flagged belowfold controls. Fixed layout and collapsed answer settings for questions with branches. Root visually inspected final410px inspector with operators visible.
- Browser testing caught refused-insertion clobber; fixed in both insertTypedQuestion and authorInsert, regression preserves existing selected node. Protected internal branch links now open branch editor; outcome paths remain insertable.
- Existing4318 local data left intact during final walkthrough; isolated test data lives4319. Both use same files. No production changes, no push/deploy. InstalledAndroid not tested; no whole-app visual/performance certification claimed.

### PR packaging
Clean branch design/config-sop-studio from origin/main03ebe28194bc86777c5d24d811e8910e60fb9f63. Mock added under docs/prototypes/config-sop-studio. All11 focused Node scripts pass from packaged directory; local assets resolve; credential-pattern scan clear. Optional health oracle now takes HEALTH_SOP_SOURCE. No production integration, merge, or deployment.

## 2026-09-16 — Box connection drop interaction
- Scope: draw from an output handle onto the destination box; snap preview to input and highlight valid destinations; enlarge handle hit area. Click-handle then box also connects. Existing node movement and cycle restrictions remain supported.
- Done: changed canvas-editor.js/CSS and synchronized the standalone local preview copy.
- Tests: check-connection-drag.cjs runs real Chromium pointer input on Procurement/Editor at 1440x1000 and 760x1000. Box drop, highlight, undo, click-connect and Escape pass. Original HEAD fails the target-highlight assertion. run-checks.sh passes all existing judge scripts, including 27 canvas checks.
- Before/after: original drop lookup accepts only data-input; new lookup accepts the valid destination node body and snaps the preview. No performance claim.
- Known failures: none in current focused checks. Browser test requires PLAYWRIGHT_MODULE when dependencies are external to this checkout.
- Judge: automated regression/browser checks passed; no separate agent review.
- Base SHA: 1590f87515766d8f35a34b3afefe114a69df7979; changes are local and uncommitted.
- Pending: maintainer review of interaction; no push or promotion requested. Deployment: local preview only, served on 127.0.0.1:4318; no STG deployment.

### Follow-up: existing arrow drag (02:00 report)
- Previous fix did not implement dragging existing arrows; user correctly reported this missing interaction.
- Added pointer dragging from existing edge paths, central insert controls, and unambiguous input endpoints. Drag threshold preserves ordinary line selection and + insertion clicks. Destination changes use existing validation and undo. Canvas disables browser text selection.
- Actual Chromium tests at 1440px and 760px pass line/endpoint/+ drag onto destination box and undo, plus prior output-handle/click/Escape cases. Before-fix JS fails the line-drag destination assertion. Existing run-checks.sh passes.
- Synced local preview JS/CSS; still uncommitted, not pushed or deployed.

### PR push requested
- User authorized pushing the completed fix to the existing mock PR (#275, design/config-sop-studio).
- Final verification: run-checks.sh and check-connection-drag.cjs both passed after final code edits; git diff --check passed. Browser checks cover 1440px and 760px.
- Scope: the two canvas files, focused browser regression script, and this receipt. No main merge or staging deployment.

### Referenced question deletion
- Removed deletion hard block: deleting a question clears matching primary and compound decision sources and detached rule ownership. Decisions remain editable; validation blocks incomplete source checks. Undo restores the complete graph.
- All run-checks.sh suites passed. Chromium tests at 1440px and 760px passed actual X deletion, both source clearances, invalidation and exact graph undo, plus arrow regression checks.
- Preview server initially returned empty response; restarted owned server with redirected logs. Local preview synchronized. Changes uncommitted; no push/deploy requested.

### Existing Chrome tabs retained old script
- Screenshot still showed removed guard. Live HTTP script was verified to contain new deletion behavior; added script version query to index.html and synchronized standalone preview index.
- Reloaded all existing localhost:4318 Chrome tabs through Chrome's reload command; preserved browser localStorage drafts. No new tab opened.

### Main landing requested
- Scope: arrow reconnection, box drops, referenced-question deletion, preview script cache refresh, regression checks.
- Done: focused mock and Chromium checks passed at 1440px/760px.
- Pending: commit final deletion fix, isolated rebase, exact-SHA make land-main receipt and remote verification.
- Known failures: none remaining in focused checks; preview server restart resolved empty response. Performance metrics: not applicable. Judge: focused automated checks only. Base candidate: 3e8ed42c6. Deployment: local preview; no STG deployment requested.
