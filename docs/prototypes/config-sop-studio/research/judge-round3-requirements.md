# Round 3 — independent requirements and integration challenge

2026-09-16. **Verdict at this review point: refinement required.** The prior bounded pass was too optimistic about end-to-end generic typed configuration. All 18 packaged Node suites pass, but fresh adversarial checks found gaps not covered by those suites. This report records the state when discovered; fixes being made concurrently require a subsequent receipt.

Reviewed all eleven anonymous transcript entries and both screenshot descriptions again, plus frontend/backend/Android architecture, real staging evidence, previous acceptance matrix, current application/item/source/typed/branch/compiler layers and workflow simulation. Audio evidence remains machine-transcribed, with uncertainty preserved. No implementation edits, live data changes, or production interactions were made during this round.

## Why these requirements arose

The recordings repeatedly correct a module-first approach. Configuring procurement, transit or warm-up separately would duplicate the same medicines, needles, prices and other reusable definitions. V02/V03/V05/V06 therefore ask for the generic foundation first. V07/V08/V11 make the desired journey concrete: define an arbitrary item once, link one or many modules, and select that same item in their SOPs. A registry without an actual downstream picker does not complete that journey.

V09's e-commerce analogy explains why taxonomy must describe the item rather than force it under a business module. V10 reduces repeated administration further: a whole category or subcategory can supply module applicability, with direct item relations for additional cases. Union semantics are a reasonable explicit proposal, not a verbatim requirement; excluded/override grants and deeper nesting were not specified.

V10's purchase/selling-price examples also mean that “item” cannot be only a physical stock label. A usable configuration needs a value, unit, stable identity and consumer. Source research then prevents accidental semantic collapse: inventory, feed catalogue, clinical protocol, authored ration, reporting threshold, valuation assumption, observed market price and actual transaction price have different owners and lifecycles. One interface does not mean one untyped storage model or one global scalar for all contexts.

The screenshot's transit example adds a different requirement: item sharing determines availability; event dependencies determine when work starts and whether later work can proceed. Shed emptying, disinfection and water/ORS preparation must not activate before the matching transit context starts. Existing backend follow-up and Android pinned execution/outbox concepts inform eventual implementation. The current simulator accurately labels itself as local activity acknowledgement; it is not dispatching or executing linked published SOPs.

## Findings

### P1 — Valid arbitrary configuration keys can silently bind another value

**Evidence:** `question-rules.js:2` unconditionally maps `weight` to `reporting_weight_35` and `rate` to `valuation_fattening_rate`. `typed-config.js` generates a key from a new item's name and accepts those keys. `qrConfigs` can therefore offer a newly authored Weight record, while `qrResolve` and compilation resolve the legacy alias instead.

**Independent reproduction:** loaded actual application, item/source/generic/typed and branch compiler layers using the existing integration harness; added a valid Common configuration with key `weight`, value 42 kg and Weighing applicability. `qrConfigs('Weighing').weight.value` was 42, but `qrResolve` and compiled `configReferences.reporting_weight_35.value` were 0, the separately edited built-in threshold in the fixture. This is a wrong-reference result, not merely missing UI text.

**Required correction:** distinguish legacy reference migration from new stable references. Prefer stable configuration IDs, or perform explicit legacy migration before authoring new records. An exact real key must not be hijacked by an alias. A fallback-only alias still needs consideration if an old reference and a new alias-named record coexist.

**Acceptance:** create arbitrary Weight and Rate records, choose them in a compatible module's actual picker, publish, verify their own IDs/values/revisions, edit them, and verify old snapshots stay pinned. Also prove intentional old-reference migration still preserves meaning.

### P1 — Price/quantity/percentage authoring is not reachable end to end through the actual unit control

**Evidence:** `app.js:26` builds the Number-question Unit menu from °F, °C, kg, g, ml, ₹, %, count. `questionnaire.js:43` adds no-unit and cm. `procurement-questionnaire.js:7` preserves an existing imported unit; it does not let a new question choose arbitrary new units. Typed configuration uses INR/kg, g/head, percent, animals/day and other units. `question-rules.js` filters compatible configuration by unit and only normalizes the rupee prefix.

**Impact:** a newly authored price may save successfully, but a fresh SOP Number question cannot choose INR/kg or INR/head through the UI. Percentage questions use %, while typed percent records use percent, so the record is filtered out. Unit tests inject compatible question units directly and do not prove the real authoring journey. This directly weakens V04/V08 plus V10.

**Required correction:** expose appropriate units from applicable typed sources or a controlled custom-unit path, and normalize equivalent representations consistently. Do not silently convert incompatible dimensions or currency. Keep unit semantics and immutable publication snapshots.

**Acceptance:** from the visible editor, create a fresh question and bind a newly authored per-unit price, a percentage and a quantity. Show incompatible units excluded; demonstrate save/reload and compiled value/reference without changing state through a test-only API.

### P2 — Typed branch dependencies are absent from usage and impact review

**Evidence:** `items.js:5` discovers physical item/action/catalogue references but not `valueRef`, condition clauses, `configSnapshot`, or compiled `configReferences`. Item archive/direct-link removal and inherited-link review use this function.

**Independent reproduction:** after creating both a draft Number branch reference and a published definition referencing `config-reporting_weight_35`, `itemUsages('config-reporting_weight_35').length` returned **0** in the actual combined-model harness.

**Impact:** the form can claim no SOP usage, and archiving or unlinking a consumed numeric configuration does not explain the affected SOPs before the change. Subsequent compilation correctly fails and old snapshots remain pinned, so this is an impact/discovery gap rather than mutation of historical execution.

**Required correction:** discover typed references in primary conditions and clauses, drafts and published versions, using stable identity and pinned metadata. Deduplicate consistently with existing usage behavior; show actual consumer references in the item editor and impact acknowledgement.

**Acceptance:** usage contains both draft and published branch references; archive/removal identifies affected drafts before commit; compile then fails closed; pinned publication retains its prior value and identity.

### P2 — Scope/effective period is metadata but looks operational

**Evidence:** `typed-config.js` accepts a free-text Scope / effective period string. `configValueByKey` resolves one active globally unique key. `consumer-surfaces.js` uses that key, while park scope only filters fixed sample rows. Neither free-text effective dates nor item/animal-group context changes resolution.

**Impact:** changing a value's scope to CPT or to a future date can appear to configure applicability, while the same value continues affecting the global sample consumer. The source/DB evidence says real ration and schedule settings are effective-dated and park/workflow-specific. Audio did not specify a full scope-resolution engine.

**Smallest warranted correction:** label the field as descriptive metadata, explicitly not applied by this preview, and explain the fixed sample applicability at the consumer. Do not imply scoped pricing or automatically add a complex evaluator from ambiguous audio. A later implementation design needs structured dimensions/effective dates and overlap resolution.

**Acceptance:** changing this descriptive note never suggests a functional scope change; consumer limitations are clear. Any future functional scope control must have matching resolver and tests.

## What remains sound

- Shared category/subcategory/item grant union, provenance and effective physical-item availability are implemented. Actual compiler tests cover inherited action/question sources, revocation and immutable snapshots.
- The prior consumer-access and registry module-filter bugs were fixed. Module exclusion must continue to apply to both physical items and typed configuration.
- Reporting thresholds and valuation assumptions are separately modelled in current consumer pages; no sale prohibition should be inferred. Actual transactions and market observations remain separate.
- Transit prerequisites, context isolation, duplicate-event safety and required/optional completion gates pass model tests. The SOP-name-only simulation boundary is explicit.
- Production-like navigation retains 35 existing leaves and adds configuration. This is not a full replica of every page; several bodies and controls remain reference-only/sample previews.
- All eleven notes are accounted for. Database persistence and a complete generic analytics product remain outside the local mock; mentioning those limitations is correct, not evidence that the requests are implemented in production.

## Test receipt and evidence limits

`sh run-checks.sh` completed exit 0 (18 Node files) at the start of this challenge. Independent ad-hoc combined-model runs reused `judge-final-integration.cjs` and printed the exact alias collision (42 offered, 0 resolved) and missing usage count (0). These demonstrate why the green suite did not establish complete authoring behavior. No new implementation was edited by this judge.

The current review uses source and prior validated visual artifacts; it did not replay all browser paths or relisten to all audio. Parent/other agents are handling new browser and visual proof. The existing live Vaccination render error remains separately documented; this mock review neither diagnosed nor fixed it. No push, merge or deployment occurred.

## Independent recheck after corrections

The four findings above describe discovery state and are retained for traceability. Subsequent code now uses `config:<stable item ID>` for new typed branch references, discovers compatible units, normalizes `%`/`percent`, includes typed branch dependencies in draft/published usage, and labels scope as descriptive only. Independent actual-stack assertions pass for all of these model/compiler behaviors. Blank quantity units and the known weighing proof minimum/maximum pair are additionally validated without imposing a global proof cap.

A follow-up UI detail was identified during recheck: changing a Number question's unit does not currently request inspector refresh, leaving an already-visible Compare with list stale until another rerender. This was corrected by preserving the original onchange handler and refreshing the inspector once; independent final suite rerun passed. Browser evidence and final test receipt must be read from `round3-final.md`, not inferred from the old verdict above.
