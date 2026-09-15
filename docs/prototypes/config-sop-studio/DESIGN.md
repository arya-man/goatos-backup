# Configuration and SOP Studio — design proposal

## What this is
A local interactive proposal for Mesha's CEO/CXO experience. It combines module-owned business configuration, shared resource governance, reusable SOP blocks, and a visual question/decision/action editor. No live backend or mobile deployment is part of this mock.

## Information architecture
Each module keeps its operational pages and adds Business rules and SOPs. Existing Sales Config transaction entry is preserved in the implementation plan. Common Config manages references and resource availability. Common SOP Library manages reusable questions and actions; executable SOPs remain owned by a module.

Modules: Counts; Weighing; Sales; Feed; Preventive Care / Vaccination; Procurement; Health; Others (Milk, People/HRMS, monitoring). Approvals, Verify, Tasks, Work Board remain operational destinations rather than new business modules.

## Ownership and sharing
- A configuration has a name, type, unit, owner, draft/published version, effective time, and consumers.
- Sales owns minimum sellable weight, comparison, tolerance, and target price. Weighing consumes references for analytics; it cannot silently override them.
- Built-in dependencies differ from optional sharing. Both must show owner and impact.
- A catalogue grants discovery/selection. It does not grant authority to prescribe, administer, or deduct inventory.
- Share question definitions separately from downstream behavior. The same temperature question can route differently in Health and Procurement.
- Common library distinguishes configuration references, question blocks, catalogue lookups, and action capabilities.

## Workflow UX
Canvas: Start -> Question -> Decision -> Action or next question -> End. Inspector edits the selected node. A number question owns input validity (unit/min/max); a decision owns business routing (> / >= / < / <= / equality / fallback). Every decision needs a fallback. Operator simulation uses the actual draft graph.

Use explicit boundaries: >35 kg excludes exactly 35; >=35 includes it. A tolerance must state its calculation rather than merely saying 'error margin'. The illustrative 103°F threshold is supplied by the user and is not a clinical protocol recommendation. Treatment labels are fictional; actual protocols require the existing clinical governance path.

## Publish and runtime contract
Proposed production architecture: immutable published graph and configuration versions; ongoing runs pin their initial versions. New runs take effective published versions. Catalogue availability is resolved within tenant/park/role permissions. Offline execution uses a supported, versioned phone definition and submits through existing outbox/module commands. Sharing must never become direct cross-module database writes.

Validation should catch disconnected nodes, missing destinations, invalid number ranges, absent fallback, cycles, revoked resources, missing action authority and unsupported phone field/action capabilities. Changes show dependent SOPs before publication.

## Verified current state
Read-only audit of current checkout and live Chrome on 15 Sep 2026:
- Existing SOP form builder supports conditional visibility, comparisons, input ranges and proof fields.
- Arbitrary branch/action execution is not implemented; backend tests reject branch_to/calculated_value/validation_rule.
- Medicine/vaccine option sources are metadata without complete live catalogue endpoints.
- Health already has immutable protocol versions, pinned by active cases.
- Live navigation includes Health and Procurement SOP entries beyond this checkout's nav snapshot.
- Exact current dark-theme tokens reused: background #0E1512, panel #161F1A, text #E9F1EA, muted #94A89A, border #26332B, lime #7CCB45.

## Comparable products
Salesforce Flow combines screens, decisions, actions and data elements:
https://trailhead.salesforce.com/content/learn/modules/flow-basics/meet-flow-builder
Jira workflow editor combines a graph with conditions, validators and post-functions:
https://support.atlassian.com/jira-cloud-administration/docs/what-is-the-new-workflow-editor/
The ecommerce attribute analogy fits sharing typed fields across categories, but does not cover execution, approvals, side effects or versioned operator runs.

## Feature-specific configuration candidates
These are proposed authoring categories, not claims that every backend setting exists today.

| Module | Owned configuration | SOP examples | Shared inputs |
|---|---|---|---|
| Counts | reconciliation tolerance, required evidence by event, discrepancy escalation | birth registration, death reporting, pen movement | animal identity, pens, proof questions, approvals |
| Weighing | units, measurement validity, reweigh tolerance, session evidence | identify animal, capture weight, exception/reweigh | Sales eligibility and price for analytics; animal identity |
| Sales | eligibility comparator/weight/tolerance, target rate, validity, approval thresholds | sale eligibility, negotiation approval, dispatch evidence | latest valid weight, buyers, withdrawal/health clearance |
| Feed | ration plans, ingredient units, wastage bounds, delivery tolerances | mixing, packing, distribution, leftovers | feed inventory, pens, weight-derived inputs |
| Preventive Care / Vaccination | program timing, scheduling capacity, cold-chain limits, proof policies | pre-check, batch selection, administration evidence, exception | approved vaccine/medicine catalogue, animal identity, stock |
| Procurement | intake quality limits, weight variance, supplier requirements, price approvals | purchase inspection, quarantine routing, receipt confirmation | medicine catalogue selection, approved Health referral action, vendors |
| Health | approved protocols, age bands, triage rules, medicine catalogue | assessment, protocol selection, referral, approved treatment recording | animal identity, weight, shared evidence questions |
| Others / Milk | mixing ratios, temperature ranges, feeding quantities, evidence | preparation checks, distribution, exception | ingredients, pens, temperature/evidence questions |
| Others / People | role and responsibility mappings, escalation assignment | request, manager approval, acknowledgement | people catalogue and generic approvals; keep HR visibility separate |
| Others / Monitoring | sensor thresholds, stale-reading window, notification routing | inspect alert, confirm physical reading, escalate | sensor inventory and generic follow-up action |

Operational Approvals/Verify/Tasks/Work Board consume workflow outcomes; they should not each own a competing copy of the same business rule.

## Refinement decisions

1. Separate master Items Config from module policy values and SOP logic. Fixed verticals match existing sidebar modules; categories/subcategories are editable. Stable item references avoid name-change breakage.
2. Shared item availability and action authorization are separate. A shared inventory reference populates selectors; it does not authorize stock movement or treatment.
3. Publish snapshots item/config/catalogue definitions for repeatable operator runs. Drafts see current masters. Removed sharing/archive invalidates new publication while preserving historical references.
4. Health SOPs require an ordered course layer: cohort, condition, day, session, action/medication rows, and source provenance. The verified Fever example adds this layer. Branching Q&A remains in the general workflow editor. Nested SOP execution and real scheduled task creation are integration work beyond this local prototype.
5. Existing Health import snapshot is not a complete substitute for the current source sheet. The reference judge verified live Adults/Kids tabs; Fever source is retained in health-fever-source.json. Missing dose numerator units remain unspecified, and no dose calculation is performed.

## Canvas-first authoring (reopened after user feedback)
The primary operation is now intended to be drawing a process, not filling connection selectors. Acceptance is demonstrated by adding a question on an existing path, adding a decision after it, inserting actions into both branches, reconnecting via ports and testing both answers. A new step must retain the downstream destination; no disconnected node is created silently. Inspector fields configure content. Destination dropdowns are a secondary advanced method, governed by the same validation as canvas connections.
Critical usability: persistent toolbar, useful canvas area, readable branch labels, keyboard click-to-connect alternative, cancel with Escape, fit-to-content, pan, and scoped undo/redo. Published definitions stay immutable. Deleting a question used by a decision must not silently leave a broken answer reference.

## Published procurement source and questionnaire views
The local Procurement editor starts from a reconciled production Animal Purchase Inspection v5 snapshot:7 load questions plus38 animal questions. `procurement-source-provenance.md` distinguishes live visible field verification from repository seed keys. Two newly authored load fields have local placeholder keys. The source baseline is copied separately; editing the local graph never changes it or production.

List and Flow operate on the same nodes and edges. Section navigation moves the viewport without rearranging question order. Imported `only_if` conditions become explicit decision/skip/rejoin paths; ancestor conditions are retained so a male animal cannot reach a lactation-dependent check. Existing Yes/No fields do not automatically acquire invented treatment actions. Authors can create answer branches and connect them to questions/actions. Multi-select generated routes use explicitly stated first-match priority; compound ALL/ANY decision checks support combinations. Multiple actions can be chained along a route; this prototype does not claim concurrent task orchestration.

Question options store stable values separately from labels, including commas in labels. Source numeric bounds, omitted units, optional answers, proof kind/count limits, and Other free text survive import. Catalogue questions and shared-resource actions use module-visible configured sources; local Vendor preview records are labelled demo. No live inventory lookup is implied.

`rule-options.js` supports equality, inequality, numeric comparisons, inclusive ranges, membership/exclusion, answered/unanswered, and ALL/ANY clauses. Validation checks source ordering, stale options and malformed ranges. This extends ordinary workflow rules; it does not replace the separate Health diagnosis algorithm with a tree.

## Mobile page navigation
Read Android origin/main03ebe281 (not proof of installed APK) before matching preview semantics. The load questionnaire is collected once per load; animal inspection has five sections/pages. The preview keeps a pinned definition for the run. Previous does not validate; Next checks the currently visible page; final completion checks all applicable fields. An answer change prunes inapplicable scalar, multi-choice, Other and local proof metadata repeatedly until stable, preventing stale hidden answers from selecting later branches. Source pages and run-time graph routes are separate from spatial canvas positions.

## Question-owned answer branches (2026-09-16)
The question inspector owns ordered IF / THEN rows plus OTHERWISE. CEO chooses comparator, value or unit-compatible shared Sales configuration, additional ALL/ANY checks, optional NOT, and existing destination or creates a custom operator instruction. Each row is an ordinary persisted condition node; ordered no paths are managed together so the inspector and simulator agree. Missing input cannot satisfy a negated numeric rule. Min/max are collapsed under Answer validation, separate from outcomes. Existing immediate source conditions can be adopted explicitly without inventing new outcomes. Test these branches exercises only the selected question rules, no actions execute.

Shared RHS values resolve into the compile snapshot; published and running definitions do not read changing settings. Local graph remains prototype-only. Inserting steps belongs on matching outcome paths; generated question-to-condition and no-chain connections are managed through the question panel. Independent judge-question-rules.cjs: 10/10 passed including boundaries, NOT/AND/OR, page traversal parity, cycle rejection, snapshot pinning, custom-action reconnect.
