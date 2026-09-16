# Configuration mock refinement — 16 September 2026

## Scope
Generic catalogue-first item authoring; global category/subcategory; explicit consuming-module links and real SOP selectors; reusable event-triggered workflow links, same-context prerequisite/completion simulation, and local-run insights. Current review covers all eleven audio notes and supplied screenshots. Static mock only.

## SHA / branch
Base and current HEAD: 967c3683b7e2e749a306f9851d6d062328d21efb. Local branch design/manju-mock-refinement-20260916, uncommitted edits in isolated worktree. No change to primary dirty checkout.

## Done
- New items belong to Shared catalogue, with arbitrary global taxonomy and no forced Health owner. Selected modules govern direct and source-backed selectors. Existing legacy records remain compatible.
- Item-first form, exact effective-visibility preview, inline taxonomy creation, saved revision across reload, shared registry/source filters.
- Generic workflow link event/prerequisite editor; same-context isolation; parallel required/optional activities; pinned run versions; duplicate event protection; next-stage gate; local metrics.
- Responsive dependency diagram and dark theme; preserve unsaved link form while simulating.
- Independent code/requirements and screenshot-based visual judges; findings fixed and tested.

## Exact verification
- sh run-checks.sh: all 14 judge-*.cjs scripts pass, final output evidence-manju/final-node-checks.txt.
- node --check on all 3 new runtime JS files; git diff --check pass.
- Browser: create central Needle — shared catalogue with Supplies / Needles, explicit Health + Preventive Care; save/reload; reopen/save/reload/reopen preserves exactly these two selections, not Counts.
- Browser: select that central item in actual Health and Preventive Care SOP action pickers; Health workflow validation passes. Sales action picker excludes it (only placeholder); restored test Sales action afterwards.
- Browser: transit start missing => 0 tasks; foreign context ignored; matching event => 3 tasks; duplicate ignored; arrival blocked with incomplete tasks; all 3 complete => arrival accepted. Unsaved link name survives simulator events; pinned run name unchanged.
- Browser: Run insights counts actual completed run: 1 completed, 0 blocked, 0 in progress, 3/3 activities.
- Browser: Director role disables definition editing. Existing Procurement List still renders 45 questions with source section counts intact.
- Desktop 1280x720 and phone 390x844 screenshots reviewed; document width equals viewport on phone. Existing Procurement List before/after screenshots independently compared.

## Findings fixed
Incorrect status buckets; lost unsaved workflow form edits; light panels causing low contrast; Common hierarchy missing; phantom Counts checkbox on editing Common item; foundation navigation disappearing on item-only render; raw Common labels in authoring.

## Before / after
Before: new items forced module ownership and module-scoped taxonomy; no generic workflow activation/completion-gate simulation. After: shared catalogue plus explicit module linking and generic tested dependency simulation. No latency/performance improvement claimed.

## Judge status
SUPERSEDED: prior code/scoped visual tests passed, but they do not certify full requirements or production architecture. The subsequent voice notes exposed missing category/subcategory inheritance and configurable values. Receipts EARLIER-CODE-JUDGE.md and EARLIER-ACCEPTANCE.md.

## Earlier pending status (superseded by final verification below)
Previous completion claim withdrawn. Source and real staging database discovery, all 11 voice notes, consolidated judge, refinement, and final judge/browser verification are pending. Earlier mock opened and verified in Chrome on http://127.0.0.1:4320/#Items/Registry. This remains browser-local, no database/cross-device sync, real task dispatch, backend, installed mobile, or production route E2E. SOP name links in orchestration are simulated activity references, not nested production execution. Insights cover the current test run only, not a custom analytics engine. Existing production failure strings are not claimed fixed. No remote media requests introduced.

## Deployment
No commit/push/merge/deploy performed or requested.

## Research restart
Pinned source revision: 397114d1d06baddb50dffc7d2c2f9df1d0497b7b. Three parallel audits cover backend, admin-web, and Android, plus parent read-only real Cloud SQL inspection. Prototype implementation paused until evidence synthesis and first judge. Latest 17:18:18 note reiterates one interface and one-to-many module relationships. No business rules or staging rows will be changed.

## First judge and refinement in progress
First consolidated judge: NOT READY; required inherited module links, typed values, separate reporting/valuation semantics, existing-product UI. See research/judge-before-refinement.md. Added production shell preserving 35 existing navigation leaves; shared category/subcategory/item resolver; typed config records; distinct ADG and Farm value consumers plus representative existing Feed/Health/Sales/PC surfaces. No live data mutations.

Verification failures caught during integrated browser testing: hidden type-inapplicable fields were still displayed due to CSS; existing weight edit was rejected because an irrelevant default INR currency did not match its saved empty currency. Fixes assigned before final judge. Earlier regression suite passes do not certify these browser paths. Final judge and browser save/reload/picker/visual verification pending.

## Final verification
- All 18 `judge-*.cjs` suites pass after final implementation edits; full output: `research/final-node-checks.txt`. Includes independent actual-stack compiler/inheritance integration.
- Fixed hidden irrelevant fields, weight-save currency validation, hierarchy checkbox rerender, effective module registry filtering, revoked consumer access, and legacy numeric branch references. Numeric branches now pin typed configuration identity/revision/value/unit and reject inaccessible or incompatible references.
- Browser save/reload checks: ADG threshold changes sample counts; Farm value rate 450→500 changes value independently; Feed rejects blank and preserves explicit zero. Restored demonstration defaults.
- Created shared Reusable needle with no direct module links, inherited Health from category and Preventive Care from subcategory. Reloaded and selected it in both modules' actual SOP action pickers, then saved local drafts. Independent compiler tests additionally cover inherited question sources and immutable snapshots.
- Production navigation audit: all 35 destinations visited; 12 loaded screens visually inspected, 22 DOM/accessibility-only, one live Vaccination error reference 1009743198. This is not full production pixel regression certification.
- Desktop/mobile visual report: `research/final-visual/judge.md`. Final Chrome tab reloaded current shell and visually checked ADG Analytics. Local server: http://127.0.0.1:4320/.
- All eleven anonymous voice-note records and source/DB/Android/frontend evidence remain inside prototype `research/`.
- Final independent judge receipt: `research/judge-final.md`; its verdict and explicit limits govern delivery.
- Production integration remains outside this local mock. Selected consumer surfaces use illustrative rows; other destinations are labelled reference previews. No latency claim, production data changes, push, merge or deployment.

## Additional independent review requested
Three fresh judges are reviewing requirements, integrated code and visual regression. Prior bounded pass is being challenged against all eleven voice records and actual authoring UI. Findings so far: arbitrary Weight/Rate configuration keys collide with legacy aliases; new Number-question unit choices omit typed per-unit prices; scope is descriptive metadata but label implied enforcement. Fixes and fresh browser validation are in progress. No promotion.

### Additional review closure
Fixed stable-ID branch binding, dynamic compatible Number units and immediate refresh, numeric SOP impact discovery, blank quantity units, and known weighing min/max consistency. Scope copy explicitly descriptive. Visual refinements: denser navigation, ADG Download separate row/Apply adjacent to input, Feed breadcrumb and existing filter/table structure. Also reset main and document scroll on route changes after browser found header obscuring filters.

Fresh browser flow: Rate525INR/kg create/save/reload → fresh SOP Number question INR/kg → shared configuration selection →525matches/450otherwise → usage inventory and archive impact dialog, then discard archive. Final ADG screenshot visually validated research/final-visual/adg-round3-after.png. Latest independent verdict research/round3-final.md. No production mutations or promotion.

## Round 4 review underway
User requested repeating the same review. Independent requirements, integrated-code and desktop/mobile visual judges restarted against the latest mock and all eleven voice-note records, architecture/staging evidence and prior findings. Previous pass is not treated as proof that no further defects exist. No production writes or promotion planned.

Round4 database refresh: real goatos-stg read-only confirmed2026-09-16T12:53UTC, migration317 unchanged. New staging-data-plan.md separates existing domain stores from proposed taxonomy/relations and domain policy migrations; independent persistence review concurs. No rows inserted. New findings: hidden direct-condition refs, numeric tester normalization, malformed rate denominator, missing proof counterpart, modern configuration deep links overwritten by legacy boot, and form name ordering. Fix/retest ongoing.

### Round4 verification
All18 suites pass after finaledits (research/round4-final-checks.txt). Parent browser verified workflowlinks directreload + transitcontext/duplicate/completion gates; directcondition sequential599 entrydetaches binding, preserves focus and599.0matches; restored shared525 example. Newitemname-first visually verified. Finalindependent reports plus fresh realSTG storage mapping are underresearch. No commit/push/merge/deploy orDBwrite.

## Round 5 review underway
Repeated independent requirements, actual-script code and visual reviews. New reproduced defects: saved catalogue-to-configuration conversion breaks consumers, optional invalid numeric input can skip, and multiple-choice branch testing loses exact option membership. Fixes and new regression checks in progress. Real staging mapping remains based on the read-only 18:23 IST query; no database writes or promotion.

### Round 5 closure
Fixed saved item type conversion, optional invalid-number handling, typed branch tester exact option identity, mobile active-tab visibility and actual name-first form ordering. Two code judges cross-reviewed each other's fixes. All20 suites and git diff --check pass after final edits (research/round5-final-checks.txt). Parent browser verified type lock, both new-item modes name-first and fresh mobile active tab visible; independent visual judge confirmed Chrome mobile. Summary research/round5-final.md. No DB writes or promotion; base SHA unchanged.

## Round 6 review underway
User requested another repeat. Fresh code, requirements and visual reviewers are challenging the round5 pass. Parent reviewing integration and served-browser behavior. No production mutations or promotion.

### Round 6 closure
Fixed stale item-confirmation role bypass, orphan archiving, source-backed category-move impact warning and shared dialog keyboard focus/semantics. Independent code judges cross-reviewed permission and lifecycle fixes; visual reviewer tested desktop/mobile dialog focus and corrected direct-render preview opener handling following cross-review. All23 suites pass; research/round6-final-checks.txt and round6-cross-review-checks.txt. Parent browser separately confirmed New item focus entry/Escape return and early-event/pinned workflow behavior (round6-browser.md). No production writes or promotion. User next asks a plain-language explanation of configuration.

## CEO clarity refinement
Hard constraint: a CEO must be able to find and understand items/settings, staff work instructions, and when related work starts without technical explanation. Preserve all eleven note requirements, category/subcategory/item department relationships, typed settings, actual SOP consumers and pinned instructions, and incoming-load prerequisites/gates. Existing35 navigation leaves remain. Run insights becomes in-context practice results; reusable lists/questions/actions remain secondary tools. Fresh realSTG19:33IST read-only query confirms migration317 and operational values; currentmain aa0577765 includes new Alerts migrations absent fromSTG. Source and deployment distinctions documented. Implementation/judges/browser verification underway; no DB writes or promotion.

## Current scope update
User authorised creating a PR against main after design reviews, followed by independent PR judges and further fixes. No merge or deployment authorised. SOP composition is now integrated; first integrated code and visual judges are active. Prior event-engine reviews are superseded.

## Expanded scope and latest checks
- Added V12 park/pen/name/capacity onboarding requirement from local machine transcript; original audio remains outside repo.
- Real goatos-stg configuration value audit expanded beyond counts, plus72 backend/DB setting groups and frontend/Android companion inventories.
- Actual OCI-backed Herd Register running separately on3307. Both Register pen and Register animal forms opened in Chrome; no submissions. OCI schema319 trails source326; not certified allroutes.
- V12 park/pen onboarding was broadened to browser-local entity registers for all feature-owned CRUD families: Counts park/pen/partition/animal, Procurement vendor/truck, Sales buyer, Feed catalogue/stock, Preventive Care vaccine/batch, Health symptom/disease/protocol, People/roles, approval policy and SOP template records. Focused entity judge plus full `sh run-checks.sh` pass after the change.
- Retired standalone orchestration implementation and its test removed after SOP composition replacement; no event-engine assets loaded.
- First/second judges found and fixed nestedstage bypass, staleapproval, downstreaminvalidation, typedperiodbinding, cohortprogress, multifeedmath, grantrevocation andmobilelayout. Final independent PR review is still pending.
- User rejected earlier master visual at20:17; refinedtypography/toolbar undergoing freshverification. Current24activeNodejudges passed beforeV12addition; finalfullsuite willrerun.
- No push, merge or deploy yet. User authorised featurebranchPR againstmain, thenpostPRjudges.
