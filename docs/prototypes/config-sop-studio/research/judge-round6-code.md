# Round 6 code review

Reviewed the current 26-script local prototype against the eleven-voice ledger and current architecture/staging-data research. No production edits, schema writes, push or deployment.

## New finding: deferred item confirmation bypassed current role

P1 within the local permission preview. `items.js` checked role in saveItem, but its confirmation callback called commitItem without a final permission check. The actual integrated stack accepted a previously staged item update with canEdit false under Operator, persisted a changed name and archived the item. Adjacent source/hierarchy confirmation boundaries already guard their final mutations. This is a local application permission inconsistency, not evidence of a production backend authorization flaw.

Assigned correction adds a canEdit/null guard at the final base commitItem boundary. `judge-deferred-permissions.cjs` loads all 26 scripts and verifies deferred updates remain unchanged for Operator and Director, and the same callback works when authorized as CEO / CXO. The test exercises the final composed callback, not a substitute implementation. Permission checks on saveItem alone would not pass this regression.

## Scope and limitations

Prior numeric/source/snapshot checks were revisited without treating earlier suite success as complete proof. The requirements judge is independently examining orphan archive/category-move lifecycle findings; their changes require separate cross-review. This report does not certify actual browser role switching through a modal, production auth, visual behavior, or Android sync.

## Independent cross-review

Reviewed the requirements judge's orphan-archive and category-move changes. Ran their lifecycle checks and an additional independent source-backed action case in an archived workflow: moving the selected resource reports the affected workflow; a different selected resource does not produce a false impact. Source membership changes now receive acknowledgement even when effective module grants stay unchanged. Ordinary direct references do not acquire a category dependency. Existing orphan records can archive while active/new records retain module-link validation.

Reviewed the visual judge's shared modal wrapper separately. Found direct operator/page previews bypass modal(), so initial implementation failed to capture their opener; also noted summary keyboard targets were missing. Both corrected in modal-accessibility.js. Replacement dialogs retain the original opener; close delegates to previous wrappers, retaining sim-clearing behavior. Final browser focus restoration for direct previews is assigned to the parent/visual judge. Model harnesses use a no-op MutationObserver stub and do not claim keyboard certification.

Final integrated source now contains 27 external scripts after adding modal accessibility. Deferred permission checks load this full stack. No production permission or accessibility certification is implied.
