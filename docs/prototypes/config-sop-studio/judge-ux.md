# Independent UX judge

Status: initial source review; not signed off. Graph implementation is still arriving.

## Design proposal
DESIGN.md distinguishes proposed settings from shipped capabilities and preserves module ownership, immutable versions and operator execution boundaries. No unsupported clinical protocol or existing-backend claim found in the proposed module table.

## Initial findings sent to builder
1. resourcesFor ignores photo and approval sharing, making checkboxes misleading.
2. Custom actions are omitted from Action library after creation.
3. New SOP replaces a module's sole workflow including published history; use multiple workflows.
4. Canceling medicine-access removal leaves checkbox visually stale until rerender.
5. Custom reusable definitions need sharing controls and actual module-specific availability.
6. Names containing apostrophes break inline JavaScript handlers; use stable IDs instead of user text.
7. Other settings mix Milk, HR and audit scopes; group by feature.
8. Module validation must reject cold-chain minimum above maximum and percentages above 100.
9. Common tabs require horizontal overflow on narrow screens.
10. Non-Sales rule save/publication semantics should match what read-only roles see.

## Required final evidence
All final source fixes inspected; root browser verifies final render, module navigation, Sales publication/Weighing propagation, shared-resource revoke/restore, node edit/connect/delete, operator branch outcomes and narrow layout. Source-only review is not visual signoff.

## Full graph source review findings
11. Operator preview hardcodes two columns inline, defeating narrow-screen stacking.
12. Canvas/SVG dimensions are fixed while node drag coordinates are unbounded; distant nodes can become unreachable.
13. Shared custom question types simulate as plain text instead of importing their declared answer type/configuration.
14. Read-only roles can inspect the current draft editor even though simulation uses a published snapshot.
15. Any record() event persists the entire state, including nominally unsaved graph changes; copy claiming explicit Save controls persistence is misleading.

These findings were sent directly to builder. No final visual signoff yet.

## Final source re-review
Confirmed repaired: checkbox-driven resource availability; custom action visibility; preserved workflow list; cancelled unshare rerender; custom definition sharing; safe indexed event arguments plus duplicate-name rejection; Others Milk/HRMS/Monitoring separation; numeric range checks; scrollable tabs; dynamic graph bounds; stacked narrow operator preview; imported shared answer types; displayed shared provenance; read-only draft hiding; automatic draft persistence copy.

Final remaining finding sent to builder: read-only workflow list must include published snapshots in archivedSops, not only the active draft's published snapshot. Creating a new draft currently hides previous published SOPs from directors/operators.

Root reports live Chrome checks passed for Sales-to-Weighing publication, director disabled editing, medicine share hide/show, revocation warning cancel restoration and invalid publication. Await final screenshots and final read-only list fix before complete signoff.

## Source signoff
PASS for reviewable local prototype. Final read-only list fix inspected: active and archived published snapshots are enumerated, and preview resolves the chosen published workflow. All concrete source UX blockers raised in this review are resolved.

Visual signoff remains pending final desktop graph and narrow operator-preview evidence from root. This is not production/backend/mobile certification. Reusable definitions currently cover name/type/guidance and instance-level conditions, not a complete centrally versioned schema authoring system.

## Final independent visual and interaction signoff
PASS for the local interactive prototype. UX judge independently inspected native Chrome screenshots at 390x844 (operator modal and Common Sharing) and normal desktop (Procurement graph at 80%). Phone/action/path stack, contained table/tab scrolling, graph branches, selection and medicine inspector were readable and usable. All raised source blockers resolved. Final pointerup fix inspected: selection occurs before redraw/inspector refresh, 4px drag threshold prevents click jitter. Root final Chrome E2E confirmed selection, new-node connection, deletion failure and repair. Nonblocking polish: some module glyphs repeat.

Limit: local simulation; centrally reusable definitions expose name/type/guidance, while richer instance properties are authored in each SOP. Production execution/inventory integration remains future work.
