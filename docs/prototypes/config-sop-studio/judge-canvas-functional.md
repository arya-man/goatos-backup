# Connected canvas judge

Status: PASS for executed local canvas scope. Browser interaction/visual checks remain root/UI judge responsibility.

Required acceptance:

- Insert question/action on selected Next, Match, or Otherwise edge preserves predecessor and successor; no orphan nodes from toolbar insertion.
- Decision insertion creates explicit complete Match and Otherwise paths; original downstream route preserved.
- Port reconnection refuses self-loop, cycle, missing target, and invalid output type. Existing connection survives refused edit.
- Delete linear step reconnects all incoming references; decision deletion handles branch ambiguity explicitly and never silently loses a subtree.
- Undo/redo restores complete graph structure and positions; fresh mutation clears redo; histories remain scoped to SOP.
- Read-only cannot insert, reconnect, delete, move, or restore editable history.
- Persisted draft and compile path use final connections; published version remains immutable.
- Rendered canvas exposes actionable ports and insertion controls with labels; event handlers execute same tested functions. Root tests pointer/keyboard in Chrome.

## Executed receipt

`node judge-canvas-functional.cjs`:18/18, including actual bound node pointer drag/select+undo, output/input click rewire, readonly pointer refusal. Integrated compiler and inspector wrappers loaded. `window` aliases VM global as browser semantics require.

Legacy rerun: base39 + v2 29 + course21 + v3 36 =125 passing. Combined143/143. Two discovered defects fixed before this receipt: inspector-language template syntax failure; canvas node drag lacked readonly guard.

Decision deletion explicitly asks which branch to keep; unused other branch nodes remain and validation flags disconnection. Inserting a condition before any available question requires author to select an appropriate source before validation can pass. These are explicit repair workflows, not silent valid publication.

Final reviewed hashes:
- `canvas-editor.js`: `741a36fd8ddc521b7cbb1bd19f8ffdfc596f3c1bbd415619ce171700688b1290`
- `canvas-editor.css`: `fff7ef261a6af6230f6956de65734c868dfbd0be0e4892c1242c55f1ee43e828`
- `inspector-language.js`: `e178bacdf12f93dd925987e2ed47234d278c6f3f83dde50fd4ecc3603ccab373`

## Drag endpoint regression receipt

Reproduced old shadowed destination lookup: live edge path stayed unset after source/destination movement. Corrected implementation passes exact expected Bezier endpoints plus wire hit path and insert button position mid-drag.

Canvas now23/23; legacy125/125 rerun; combined148/148. Scheduler test sends120 pointermove events before one animation frame: exactly1 pending paint,0 wire writes before paint,2 adjacent-edge writes after paint, unrelated third edge untouched. Release flushes final coordinates/cancels pending frame; pointercancel and lostpointercapture flush latest queued point and detach handlers, one history entry. This is deterministic scheduling evidence, not wall-clock browser FPS measurement.

Final canvas-editor.js SHA256: `4e77fab468ee4733d1ee0a5daa0f7ffe1b1f492448e847632f52cb0bf1463cac`.
