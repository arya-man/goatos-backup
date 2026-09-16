# Round 5 adversarial code review

Status: NOT READY until the two findings below are resolved or explicitly accepted. Read-only review; no application changes made in this pass. The review uses the eleven-voice ledger, architecture reports and staging-data-plan.md as context; the source semantics remain distinct from proposed local configuration. A passing packaged suite is not a substitute for these boundary checks.

## P1 — Used inventory item can silently become a configuration value

Evidence: typed-config.js:32 only prohibits configuration-to-catalogue conversion, while the editable item-type selector allows catalogue-to-configuration conversion. items.js:13 reviews archive, owner and share changes; generic-items.js:83 reviews lost module grants. Neither recognizes the loss of catalogue eligibility when itemKind changes.

Actual 26-script composition reproduction: attach an active catalogue item to an action via itemId; itemUsages returns one draft reference. Create otherwise-valid weight configuration metadata on that same item, retaining its module/category/grants. validateTypedConfig returns an empty error. commitItem persists the conversion without an impact dialog, and availableItems no longer contains the item. Readback: `{usage:1,error:'',impact:false,kind:'config_value',inCatalogue:false}`.

Impact: an existing action loses its selectable source through an apparently ordinary metadata save without the impact acknowledgement used for archive/unlink. Frozen publications should stay pinned, but draft editing/publication now needs repair. Minimal correction: prohibit conversion of used catalogue records with guidance to create a new configuration, or include conversion in the same acknowledgement and validation flow. Do not invent a production migration.

## P2 — Invalid optional Number input is silently treated as skipped

Evidence: questionnaire.js:16 handles optional empty values before numeric validity. Browser number inputs can expose `value=''` while `validity.badInput=true` (for example incomplete exponent input). The step preview advances and stores an empty answer instead of reporting invalid input. page-preview.js:12 sends only this.value, losing badInput; pageQuestionError at line7 then accepts the blank optional answer.

Actual 26-script reproduction: optional Number question, min/max null, input value empty and badInput true; simAnswer advances to end with answer empty. Readback: `{id:'end',answer:''}`. This is distinct from a deliberately blank optional answer, which should still skip. Minimal correction: propagate/check numeric badInput before optional-empty handling in both step and page paths; clear the error on genuine blank/corrected input. Browser retest should use an invalid numeric editing sequence, then clear it deliberately.

## Other exercised boundaries

The actual 26-script model probe also reconfirmed source revision pinning, Number string/numeric equality agreement, exact Text behavior from focused coverage, currency denominator rejection, missing paired proof-bound rejection, and direct literal binding detachment. These are regression observations, not independent approval of all previous work. Descriptive scope is still not a production policy engine. No source record, production price, schema, sync behavior or deployment changed.

## Verification scope

Repros ran through the actual script stack in index.html with minimal DOM stubs; this is code/model evidence and does not certify browser appearance, focus, or number-input implementation details. Parent/browser judge should reproduce the optional-number interaction in Chrome. No push, merge or deployment.

## Correction receipt

Both findings corrected after assignment: persisted catalogue/configuration type is locked with a create-new explanation; optional Number badInput is checked before skip and carried through page answer state. A deliberately cleared optional value still skips, and corrected zero remains valid. `judge-round5-boundaries.cjs` loads all 26 actual scripts and checks these paths. All 19 packaged judge scripts passed at this checkpoint. Independent requirements and browser retests remain separate gates.

## Independent cross-review and browser attribution

Independently reviewed the requirements judge's branch tester changes using the actual 26-script stack. Exact multiple-choice option `aa` does not match `contains a`; checked `a` does. Label `Alpha` is rejected as a stored option identity, deleted answer sources fail explicitly, and generated controls preserve checkbox values. The dedicated branch-tester judge also passes. All 20 judge scripts pass after integration.

Parent separately reports browser verification that a saved Reusable needle with two SOP uses displays disabled Item type and a create-new explanation. That browser evidence is attributed to the parent, not this code review.

Parent also caught that the earlier name-first ordering claim was too broad: Item type still preceded Item name because the name row was inserted only before the typed fields section. Corrected to prepend the name row to the form. Type changes do not reorder DOM. Fresh browser verification remains the parent/visual judge's responsibility.
