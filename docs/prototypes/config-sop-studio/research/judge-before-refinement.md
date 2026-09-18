# Independent judge before refinement

**Verdict: NOT READY / refinement required.** This is a requirements and architecture judgement, not a production defect report or promotion receipt. Evaluated all eleven supplied voice transcripts, both screenshot requirements, the latest user instruction to preserve the existing frontend UI/UX/buttons/titles/accordions/submenus, the current local mock, current source research, and read-only real staging evidence. Source revision: `397114d1d06baddb50dffc7d2c2f9df1d0497b7b`. Live DB migration level is separately recorded as `000317_verification_items_media_meta`; do not claim main and deployment are identical.

## Evidence considered

- `voice-requirements.md`: all eleven recordings, uncertainties and screenshot dependency case, anonymously documented.
- `frontend-architecture.md`, `backend-architecture.md`, `android-architecture.md`: real existing typed stores, editors, runtime consumers and sync/versioning boundaries.
- `staging-database.md`: real Cloud SQL read-only snapshot, no mutations.
- Actual mock `generic-items.js`, `items.js`, `shared-sources.js`, `studio-v2.js`, `policy-ownership.js`, existing workflow simulation and previous browser evidence.
- Independently opened live baseline `visual-baseline/feed-config.png`, `feed-edit.png`, `health-config.png`, `procurement-sop.png`. Existing production uses expandable module navigation with indented named leaves, compact page/section headings, thin-bordered cards/tables, small row actions, in-place edit/Apply/Cancel and explicit draft/publication surfaces. Matching only dark colors is not matching the requested UI. The live Procurement card displays 40 questions / 7 steps / published v7, while the earlier mock regression used its 45-question source snapshot; retain provenance and do not label that earlier snapshot as an exact current production copy.

## Prioritized required corrections

### P1 — Add hierarchy-level module relations; individual item sharing is incomplete

**Evidence:** recording V10 explicitly allows category, subcategory or item relation to modules. Current `generic-items.js:3-4,47,54-58` discards inheritance for Common items and requires an individual checked module. `items.js:16-18` category editor only names groups. `shared-sources.js:2,4` groups root categories, not subcategory module grants. This is the clearest unfulfilled requirement.

**Correction:** one central interface must author module relations at all three levels, display effective modules with the granting level, and use the same effective availability in registry filters, preview, action picker, source-backed question picker and publication validation. Adopt an explicit proposed rule (a union of grants is the smallest consistent design) and label it as a design decision, since exclusion/override semantics were not specified. Do not force an individual checkbox when category/subcategory already supplies access. Preserve legacy ownership and existing published snapshots.

**Acceptance:** category Medicines → Health; subcategory Supplies → Preventive Care; an item inherits both and can additionally link another module. Show origin of each grant. Removing an inherited grant updates every new picker, warns about impacted drafts, preserves pinned publications; reload preserves hierarchy and item identity. Actual browser picker tests must cover category-only and subcategory-only access, not just direct Needle shares.

### P1 — Configuration must carry typed values and consumers, not only arbitrary item labels

**Evidence:** V10 explicitly includes prices. Backend/DB/Android research also demonstrates quantities, percentages, times, booleans, enums, clinical documents, effective dates, proof policies and derived outputs. Earlier mock item shape `items.js:13` is name/category/unit/purpose/description/active/shares only; naming an item “selling price” supplies no value or executable reference.

**Correction:** keep **one generic configuration authoring surface** while rendering appropriate typed fields. At minimum demonstrate numeric/currency/per-unit price and numeric kg threshold, plus representative boolean/time/percentage/enum configurations from research. Show scope, unit, source owner, status/revision and consumers where meaningful. Distinguish current persisted setting, hardcoded source value, fallback/default, proposed editable setting and derived read-only result. A common frontend does not imply one interchangeable backend model.

**Acceptance:** save/reload an explicitly proposed currency/unit-bearing price; its consumer preview reads that same stable config ID and revision. Zero remains zero and missing remains missing. Reject invalid numbers and incompatible units. Display representative existing families (Feed quantity, removal time, vaccination capacity, Health versioned protocol, proof policy) with honest source/type classification; do not invent clinical changes or claim all families now integrate with production.

### P1 — Correct sale threshold versus valuation versus transaction semantics everywhere

**Evidence:** current `policy-ownership.js:9` calls the combined `state.sales` policy “Sale eligibility & valuation,” owned by Sales; `studio-v2.js:47` claims one published Sales policy powers eligibility and value. Backend source instead places 30/35kg reporting thresholds in Weighing constants and valuation assumptions in Sales SQL. Real sale price is a transaction/derived result. No evidence establishes a universal prohibition on selling below 35kg.

**Correction:** separate Weighing reporting threshold from Sales valuation-rate assumptions and actual purchase/sale observations. Clearly mark changing hardcoded values in this local mock as a proposed configurable capability. Preserve production page meanings: Sales Config is operational entry plus market configuration; Farm value consumes valuation assumptions. Remove false owner/dependency copy from all legacy screens, not just the new page.

**Acceptance:** changing proposed 35kg reporting threshold affects a labelled reporting/count preview only; changing proposed 450/kg valuation rate affects a valuation preview only. Actual recorded sale or market-observation fields are not rewritten. Trace both to distinct config IDs and declared consumers. Search old “eligibility”/“one Sales policy” paths to ensure contradictory claims are gone.

### P1 — Match the existing product UI, not an alternate studio navigation

**Evidence:** latest explicit user instruction requires the same frontend UI/UX, button/title treatment, accordions and submenus. Baseline screenshots show expanded module groups and specific leaves (Feed Config, Feed Analytics, Feed SOP; Health Analytics/Health Config; Procurement SOP), row-sized Edit controls, inline Apply/Cancel and compact section headers. The old mock's flat module buttons, generic “Business rules” subpage and separate Common studio vocabulary are visibly different.

**Correction:** preserve the current shell, module accordion/leaf naming, typography, card/table anatomy, button sizing and existing form interaction patterns. Add central configuration as a coherent entry using those patterns. Existing module pages should remain recognizable and expose shared config references in context. Use representative production screenshots as visual authority; do not expose implementation jargon as product navigation.

**Acceptance:** screenshot side-by-side comparison at same viewport for shell, expanded module submenus, configuration list, inline edit, category/subcategory management and module consumer. Test accordion open/close, active leaf, keyboard/focus and narrow-screen behavior. Keep existing SOP flow/list editor functionality. Dark theme consistency alone is insufficient.

### P2 — Reconcile actual catalogues and typed policy sources; avoid invented unified production identity

**Evidence:** staging inventory has seven active vaccine/dose items; feed catalogue separately has seventeen rows/four active; Health medicines are course text; SOP category table contains commodity/problem/event/action/equipment. These are not all the same category tree. DB `sop_definitions` contains category/subcategory/triggers although frontend comments claim no category field. Existing frontend/API exposure is incomplete, not proof of no schema.

**Correction:** annotate representative records with their actual source and identity family, distinguish proposed central grouping from existing SOP taxonomy, and map consumers. Preserve raw null semantics, effective dates, active/retired state and published versions. Do not seed generic medicine/needle examples as if already present in staging. Do not use frontend stale comments to deny persisted DB metadata.

**Acceptance:** research/source panel or documentation maps inventory/feed/Health/SOP/protocol families without claiming migration performed. Displayed live examples reproduce read-only evidence accurately; fictitious demo records clearly labelled. Main-only calendar settings are not labelled deployed while staging still has SOP weights_pages.

### P2 — Preserve runtime/version semantics while demonstrating generic workflow linking

**Evidence:** actual backend follow-up already supports timing, dependencies, task registry, engine hooks and pinned SOP versions; Android uses scoped cached state and durable idempotent outbox. The mock's string event and SOP-name links acknowledge tasks locally only.

**Correction:** retain the useful local trigger/prerequisite example but explicitly connect its concepts to existing typed task/SOP contracts in research. Do not present it as implemented arbitrary cross-module production orchestration. Keep item visibility separate from permission to execute; preserve context isolation, immutable run version, required/optional completion and repeat-event safety.

**Acceptance:** before start no preparation; foreign context ignored; matching start activates once; required work blocks next stage; optional does not; old run pinned after edits. Render “simulation” clearly. No real sends/downloads/production mutations.

### P2 — Do not substitute local counters for unspecified generic analytics

**Evidence:** V05 mentions analytics without defining metrics. Real admin-web already has domain-specific read models with units, populations, grouping and computed series. Current Run insights counts one local simulator run.

**Correction:** label local counters as simulation insights only. If showing a reusable analytics concept, identify source/config dependencies, measurement grain, units and read-only computed results. Keep current production analytics naming and layout; do not claim a complete generic analytics engine.

**Acceptance:** users can distinguish authored values from computed metrics and see how a proposed config impacts a preview without editing derived totals directly. Final report explicitly records analytics scope still unspecified.

## Consolidated acceptance matrix

| Requirement | Pre-refinement status | Required final evidence |
|---|---|---|
| One interface, arbitrary item/category/subcategory | Partial pass locally | Existing-product UI plus generic arbitrary taxonomy journey |
| One-to-many module relation | Direct-item pass only | Category, subcategory and item grants all demonstrated |
| Inheritance semantics and provenance | Fail | One effective resolver + granting-level display, save/reload/revoke tests |
| Module SOP picker uses same item | Direct-link pass only | Real browser action and question picker for inherited access; excluded module absent |
| Typed price/config values | Fail | Value/type/unit/scope/revision plus source-linked consumer preview |
| Correct threshold/valuation ownership | Fail | Distinct reporting threshold and valuation assumption; no sale-prohibition claim |
| Same production shell/buttons/titles/accordions/submenus | Fail against latest instruction | Independent baseline comparison and navigation interaction proof |
| Preserve old identities and published execution | Partial code evidence | Regression tests across changed inheritance/value paths and existing SOP editor |
| Real database architecture mapping | Research now available | Accurate labels; no false production integration claim |
| Generic workflow dependency example | Local bounded pass | Retain current gate/idempotency behavior and explicit simulation boundary |
| Generic analytics | Unspecified / illustrative only | Explicitly limited scope; no complete coverage claim |
| Production runtime integration | Not implemented; outside mock | Remain clearly labelled, no database/API/deploy mutations |
| All eleven audio notes and two screenshots accounted for | Research pass | Final judge maps each ledger requirement to evidence or explicit remaining scope |

## Final-judge conditions after refinement

1. Independently inspect all changed code and run focused meaningful tests plus packaged regression suite after final edits.
2. Browser-test category/subcategory/item effective access, actual SOP selectors, typed save/reload/consumer changes, invalid value handling and legacy publication integrity.
3. Compare final screenshots with the real product baseline, not only the previous mock. Inspect artifacts before citing them.
4. Confirm no misleading sale-eligibility ownership, no hidden auto-consumer grants and no stale all-audio completion claims.
5. Update local progress with scope, done/pending, exact tests, failures, before/after behavior, judge status, SHA and deployment state. No promotion is requested.

This judgement authorizes no extra business scope. The smallest safe refinement is a shared frontend authoring interface over typed, source-aware concepts with hierarchical module linking, preserving the current product interaction design. **Do not certify pass before these corrections are implemented and independently checked.**
