# Round 5 independent requirements challenge

**Verdict: refinement required.** Two new concrete user-flow defects remain despite all18 packaged Node suites passing (`round5-independent-checks.txt`, exit0). This review concerns the browser-local prototype only. No implementation or database writes were made by this judge.

The eleven-note ledger remains the source of scope: arbitrary shared catalogue/configuration, category/subcategory/item applicability, one-to-many module consumers and actual SOP pickers; later notes do not reduce this to separate module forms. The screenshot dependency example remains distinct event gating. Prior architecture/staging plan distinguishes stock catalogue identities from typed business settings and task-pinned published definitions. The new findings challenge those lifecycle and consumer boundaries, rather than treating previous green tests as completion.

## P1 — Changing a used catalogue item into a configuration silently invalidates its SOP references

**Evidence:** `typed-config.js:61` disables Item type only when the item already has a configKey, so a saved physical item can be converted. `validateTypedConfig` at31–35 forbids the reverse conversion but accepts physical→config. `generic-items.js:82–89` reviews effective module loss; conversion leaves those grants intact. The typed item/source filters at93–96 exclude config values from physical action/catalogue consumers.

**Independent actual-stack reproduction:** used an existing inherited Health catalogue item in a real Use configured item action and compiled successfully. There was one SOP reference. Constructed the same metadata produced by the available Item type control with valid currency20INR/kg, key, owner and scope. `validateTypedConfig(next,old)` returned an empty error; commit changed itemKind to config_value with neither pending item nor inherited-access impact review. Recompiling the unchanged SOP failed: `Request veterinary review: Judge item is not shared with Health.` Module links had not changed; its kind made it disappear from the consumer. The prior published definition remained intact, but the current draft is unexpectedly broken and the error misdescribes why.

**Required correction:** preserve saved kind/identity (create a separate record for another kind), or implement explicit conversion impact with consumer migration/acknowledgement. Locking saved kind is the smallest consistent correction and matches the existing one-way type restriction. Do not change a physical catalogue into a numeric policy merely because the authoring page is shared.

**Acceptance:** edit a used and unused saved physical item; kind remains physical unless an explicit reviewed conversion flow is provided. Shared/module references and published snapshots remain valid. New-item authoring must still permit either kind. Validation and UI agree; bypassing the disabled UI through the save function must also reject the unsupported conversion.

## P2 — Multiple-choice branch tester uses substring matching instead of selected-option membership

**Evidence:** `question-rules.js:27–28` renders every non-Number source as one text input and reads its raw string. Actual operator multiple-choice answers are arrays. `compare(...,'contains',...)` intentionally distinguishes array membership from text substring matching. Thus a valid rule is evaluated differently by the tester.

**Independent actual-stack reproduction:** Multiple choice has option values `a` and `aa`; branch checks `contains a`. Tester text `aa` reported `Matched Check passed? → Request supervisor approval`. The same condition with the real operator shape `{q1:['aa']}` returnedfalse. This persists after round4 numeric normalization because it is an answer-shape/identity issue, not numeric formatting.

**Required correction:** use type-aware tester controls and extraction: checkboxes/selected arrays for Multiple choice and actual valid stable option values for Single choice, Catalogue and Yes/No. Preserve empty/unanswered; do not comma-split labels or treat free text as a substitute for catalogue identity. Text questions may retain text controls.

**Acceptance:** a and aa remain distinct; selecting aa alone does not match contains a; selecting both does; not_contains and unanswered agree with operator simulation. Catalogue and choice tests use the same scoped IDs/options as authoring and cannot accept nonexistent IDs as valid choices.

## Retained evidence and limits

The actual combined-stack probes used application/item/source/generic/typed/compiler/rule layers with visual hooks stubbed. They do not establish browser rendering. Existing18 suites still pass hierarchy inheritance, module exclusion, snapshot pinning, direct/numeric fixes, usage review and transit model tests. Parent/visual judge own fresh browser evidence. No new defect was established here in transit context/duplicate/required-completion gates.

`staging-data-plan.md` remains a compatible proposal with no new persistence assertion: existing typed domains retain IDs, validations, effective dates and lifecycles; generic hierarchy/reference contracts are proposed. No browser data should be inserted directly into staging. Production integration, generic analytics, real dispatch and mobile synchronization remain outside this mock; prior live Vaccination error remains separate.
