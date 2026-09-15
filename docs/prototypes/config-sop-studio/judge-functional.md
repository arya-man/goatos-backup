# Independent functional and architecture judge

Status: awaiting implementation. No sign-off yet.

## Required checks

- Sales eligibility uses explicit > versus >= at 35 kg, with a disclosed margin formula and no NaN/negative configuration.
- Numeric SOP branches handle equality, minimum/maximum, invalid/empty input, and an explicit fallback without overlapping routes.
- Graph validation catches dangling edges, missing terminal outcomes, unreachable nodes, and cycles.
- Shared catalog selection follows the sharing state; revoking sharing invalidates affected drafts and cannot silently keep an unauthorized selector.
- Shared catalog read permission does not imply treatment execution permission.
- Draft edits do not alter previously published versions; publication is a local mock operation only.
- Shared sales values in Weighing reference the published source; editing a draft does not change the published consumer.
- All routes use local data only; no production mutation endpoints.

Evidence and findings will be added after independent code inspection and executable logic checks.

## First executable pass

Command: `node judge-functional-tests.cjs`

21 assertions executed: 18 passing, 3 failing. Sign-off withheld pending fixes and rerun.

Failing:
1. Numeric equality compares formatted strings: `103 = 103.0` incorrectly returns false.
2. Revoked medicine catalogue access blocks medicine actions but not Catalogue questions.
3. A merged decision is checked only on its first visited path. A second incoming branch can reach it without collecting its source answer.

Passing: initial strict Sales boundary, unsaved source isolation, invalid/blank Sales inputs, publication confirmation boundary, default Health graph, dangling edges, missing fallback, cycles, disconnected steps, reversed range, > and >= equality boundaries, revoked medicine action, published snapshot versus later draft edit, empty/out-of-range operator input, minimum input boundary and fallback.

Additional source findings sent to builder: custom action rendering/consumption, shared question definition type resolution, photo/approval sharing, destructive New SOP replacement, apostrophes in inline library handlers, cancelled sharing checkbox readback, cold-chain min/max relation.

## Final independent executable pass

**Functional/architecture sign-off for the local mock: PASS.**

Executed `node judge-functional-tests.cjs` against the final builder revision: **32 / 32 passing**. The three first-pass failures were fixed and independently rerun. Additional checks verified previous publication preservation, invalid numeric equality thresholds, custom question/action share revocation, and detaching a shared dependency when changing to a local answer type.

Scope of sign-off: prototype logic and source architecture only. Chrome visual and click-through sign-off is owned by the root/UI judge; this report does not claim production backend/mobile integration or medical protocol validity.

Confirmed boundaries: localStorage state, local simulation, no fetch/XMLHttpRequest/WebSocket/sendBeacon call in app.js, fictional treatment catalog labels, published snapshot independent of mutable draft. Earlier versions remain in `versions`, and creating another SOP preserves the existing workflow in the module list.

Source fixes reviewed: resources honor photo/approval sharing, Catalogue questions honor Health sharing, custom library actions appear, custom numeric definitions resolve to real numeric inputs, cold-chain range guard, index-based library handlers avoid apostrophe breakage, close-modal refreshes sharing readback, and create/delete/drag persistence matches autosave copy.

## Final post-Chrome-fix receipt

**PASS: 38 / 38 assertions** after title-oninput and unavailable-selected-action fixes. Added executable checks for built-in temperature import (Number, °F, 90–115), temperature sharing revocation, Others/Milk percentage >100 and negative-value rejection, policy isolation from HRMS, and read-only save prevention. Inspected `enhancements.js`: separate feature keys; local persistence only.

SHA-256 of tested files:
- app.js: `f00dcba1deb8727351c68d2eb25886f213110e5d27a9082857d4fd07bdd9a29a`
- enhancements.js: `f8818210636c6ced1fc6f77946a95355d07c2efce4a84eddcaaffb8353d61c68`

Command: `node judge-functional-tests.cjs` (exit 0).

## Pointer selection final receipt

**PASS: 39 / 39 assertions** after the Chrome-discovered pointer selection fix. Added an executable DOM-stub event check: dispatch pointerdown/pointerup for the action node, verify `selected === 'action'` and refreshed inspector contains the Action type control. Root independently verifies actual Chrome clicks.

Tested app.js SHA-256: `d899c82e806e75e6edf38b1d6e25f021acf54909f859992c6dbec91ea6b17e70`.
Enhancements hash unchanged: `f8818210636c6ced1fc6f77946a95355d07c2efce4a84eddcaaffb8353d61c68`.

Final functional sign-off: PASS for the local prototype. No remaining reported functional defects.
