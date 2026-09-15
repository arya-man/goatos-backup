# V2 independent functional judge

Status: pending implementation and executable verification. Prior v1 sign-off does not cover this refinement.

Acceptance:
- Stable master item identities survive rename, and hierarchy parent/owner remains valid.
- Inactive or unshared items cannot be newly compiled into a draft.
- Catalogue option values are stable item IDs, labels only presentation.
- Published compiled definitions copy graph, referenced items, catalogue choices and config. Registry edits must not mutate historical snapshots.
- Draft simulations compile current registry; published historical simulation uses its pinned definition.
- Shared reads do not confer inventory mutation or medical execution permissions.
- Compiler schema explicitly identifies a local preview format, not production API compatibility.
- No live network mutations.

Tests and final hashes will follow.

## Executed first integrated pass

`node judge-v2-functional.cjs`: **25 / 25 PASS**.

Coverage: master IDs/hierarchy, sharing, inactive item exclusion, category rename/create/duplicates, item create/rename/revision/owner validation, role save restriction; compiler missing/inactive/unshared references; copied graph/item/config definitions; draft current item versus active-run copy versus published historical snapshot; catalogue scope and stable answer IDs; operator invalid option rejection; empty catalogue; pinned catalogue usage impact; no network APIs.

One additional source-level finding sent to root: Common Sharing's `setItemSharing` confirmation counted only direct `n.itemId` and could omit a Catalogue-only dependency. Registry item save already uses expanded `itemUsages`. Pending fix and final rerun/hash before final sign-off.

## Final V2 functional receipt

**PASS — 29 / 29 independently executed assertions.**

Command: `node judge-v2-functional.cjs` (exit 0; final line `V2 COMPLETE: 29 passed`).

The Common Sharing catalogue-only impact omission is fixed and now covered. Additional final tests verify direct read-only sharing mutation prevention, owner access retention, and invalid catalogue decision option rejection. Catalogue decisions accept only equality/inequality with a currently available stable option ID.

Tested SHA-256:
- app.js: `d899c82e806e75e6edf38b1d6e25f021acf54909f859992c6dbec91ea6b17e70`
- items.js: `6d5af8620adb2585da424cc1fe71f289a65de67dec2c25789f3edf230d2668a4`
- studio-v2.js: `4fa737707e56c98d7440fa4aecfa999a377541ca19cefef277ba005703615bd1`
- enhancements.js: `f8818210636c6ced1fc6f77946a95355d07c2efce4a84eddcaaffb8353d61c68`

Scope: local registry/compiler/simulator behavior, not live Android/backend integration. Export explicitly uses `mesha.workflow.preview.v1`, and published simulation intentionally uses historical snapshots. No live network APIs found in items.js or studio-v2.js. Chrome click-through and visual sign-off remain independently owned by root/UI judge.

Final usage-detail refinement rerun: **29 / 29 PASS**. Updated items.js shows individual SOP step usages; no assertion regressed.
- items.js SHA-256: `2f50e8fa943deef9758aa50de52770fa76328fda43583a80c4aac732d065a007`
- studio-v2.js SHA-256: `21300fa9f2f51f2d0ce50f828065c43f001026e2f8bf0c44cce3a5e3ad48dc26`
