# Round 6 requirements and lifecycle challenge

Discovery verdict: **refinement required**. Two new lifecycle defects were independently reproduced against the actual combined application/model/compiler stack. The same reviewer was subsequently assigned their implementation, so the other code reviewer must independently approve the corrections.

Requirements remain grounded in all11 voice records and screenshot context: central arbitrary items and typed values, category/subcategory/item applicability, real SOP consumers and separately scoped event dependencies. The source/Android/staging mapping still requires stable domain identities and published execution snapshots. This round examines what happens after relationships change, not only initial creation. The staging-data plan remains a read-only proposal; no fresh DB query or write was performed here.

## P2 — An existing orphan item cannot be archived

`generic-items.js` save wrapper required an effective module link regardless of requested status. Removing the last hierarchy grant is allowed and leaves an active Common record with zero consumers. Opening it, choosing Archived and saving therefore failed with `Link at least one module on this item, its category or subcategory.` Actual-stack reproduction confirmed active=true/access0 after the failed archive.

Correction now proposed: existing archived saves may pass the access check; new records and active saves still require a link. Existing impact acknowledgement and immutable published snapshots remain intact. `judge-item-lifecycle.cjs` covers archive confirmation, old pinned active snapshot, rejected reactivation without a link and rejected unlinked new record.

## P2 — Moving an item between categories silently breaks source-backed SOP references despite unchanged module access

The inherited-access commit guard checked only lost modules. Moving from categoryA to categoryB with identical Health/Preventive Care/Procurement availability bypassed all impact review. A real Catalogue question bound to source-categoryA then lost its selected item; its condition failed compilation with unavailable catalogue/answer choice errors. Actual-stack readback showed pending=false and all three module grants retained. Another item remained in sourceA, so this was not merely an empty-source warning. Published snapshots remained unchanged.

Correction now proposed: identify draft question/source and selected shared-resource references that lose the moved record from their exact source collection, independently of module availability. Require an impact acknowledgement naming workflow, node, module and source. Direct item actions that retain effective access must not trigger a false collection warning. The new actual-stack test covers delayed move until Apply, post-move validation failure of the old draft, retained pinned catalogue and direct-only same-access moves without false warnings.

## Cross-review scope

The other code reviewer found an additional final-commit permission gap and added a role/null guard in the base `items.js` commit boundary. This judge independently inspected that guard: it blocks stale confirmation after role downgrade at the final mutation point while keeping CEO commits valid. Full-stack regression reruns and the other reviewer's approval of this reviewer's lifecycle patch are recorded in the final cross-review receipt.

Parent separately reports a two-event transit browser check: active run retainedv1 after saved configv2; early activation kept tasks Waiting; upstream start activated three tasks; arrival remained blocked until preparation. Saved defaults were restored afterward. This is attributed local simulation evidence, not production dispatch.

The review does not certify production persistence, live/mobile integration, full generic analytics or all frontend pixels. No push, merge or deploy occurred.

## Final cross-review receipt

**PASS for the bounded local corrections after cross-review.** This reviewer implemented orphan archiving and source-membership impact, so their own focused test is not the independent approval: the other code reviewer independently replayed an archived-workflow selected shared-resource move and confirmed it is affected, while a different selected resource is not falsely flagged. The actual-stack lifecycle suite also passes orphan archive acknowledgement, pinned snapshots, preserved active/new link requirements, catalogue move acknowledgement and direct-action moves without false impact.

This reviewer independently inspected the other reviewer's final permission guard and reran its actual27-script test: Operator and Director cannot apply a pending mutation; CEO confirmation remains functional. Reviewed the shared dialog keyboard implementation's dialog/label semantics, background inertness, Tab trap and Escape delegation; exact browser focus/opener behavior is owned by parent/visual judge. A direct-preview opener correction was additionally cross-reviewed by the other code reviewer.

The final packaged suite contains23 judge files and passes, exit0 (`round6-cross-review-checks.txt`). That run followed the lifecycle/permission corrections and visual harness adjustment for MutationObserver. Browser evidence, including separate activation/upstream events and pinned active-run version, is separately recorded in `round6-browser.md`. No new DB query/write, production execution or deployment is implied.
