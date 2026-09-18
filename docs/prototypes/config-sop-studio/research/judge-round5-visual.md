# Round 5 visual / UX judge

Fresh Chrome local review,2026-09-16. Challenged prior passes with Workflow links direct URL, mobile active tab visibility, scoped Farm value empty state, and typed form ordering. Compared production baseline conventions and earlier reports. No live service mutations.

## Finding and authorized refinement

P3: At390px the Common horizontal tab strip starts at Data sources, leaving selected Workflow links/Run insights outside the visible strip. The selected tab is not discoverable without horizontal scrolling. Parent authorized a narrow fix: production-shell.js now adjusts only the tab strip scrollLeft after layout, with no scrollIntoView/page movement. Shell judge passes, including deep-link bootstrap guard. Chrome after reload still measured active Run insights at594..668 versus strip76..376 and scrollLeft0, so this subagent **does not certify the fix in served browser**; parent IAB verification requested. No document horizontal overflow and main scrollTop0 observed. Possible stale script/timing issue remains unproven.

## Fresh passing observations

- Workflow links fresh deep link renders correct content and selected desktop sidebar leaf.
- Desktop dependency diagram fits and clearly distinguishes activation, parallel activities, completion gate and browser-only simulation.
- Mobile workflow content stacks without horizontal document overflow; text and diagram remain readable. Long introductory content pushes actual form below first viewport, an existing density tradeoff.
- Farm value CPT scope gives0 sample animals/weight/value, clear sample labels, no fabricated live data. Exact Sales submenu labels remain intact.
- Mobile configuration form captured at390×844 shows Item type → Item name → Value type → Value → Unit, with visible name field and footer. Parent separately reports further name-first ordering refinement; this screenshot only certifies name before typed values, not before Item type.

## Evidence

All usable images visually inspected under `research/round5-visual`: workflow-desktop.png, workflow-mobile.png, farm-cpt-desktop.png, insights-mobile-after.png (documents unresolved active-tab visibility), name-mobile-final.png. `workflow-mobile-after.png` contains a transient scaled browser capture and is **not proof**. Viewport reset completed. No item was created or saved.

Bounded verdict: no new blocking layout defect in inspected content; current active-tab discoverability remains pending final browser confirmation. This is representative visual/UX review, not all-route parity, pixel-diff automation, or full functional certification. Production-shell.js is the only implementation file changed by this round; no unrelated styling changes.

## Final active-tab repair verified

Earlier pending finding is resolved. Reveal now runs synchronously after render, with an optional animation-frame repeat and a resize listener; it does not depend on background-tab animation scheduling. Fresh Chrome390×844 measurement: tab-strip scrollLeft292, active Run insights x302.68..376.01 within strip76..376, document overflow false, main scrollTop0. `round5-visual/active-tab-final.png` visually inspected: selected Run insights and adjacent Workflow links visible, no page jump. Shell judge and syntax pass. Viewport reset completed. This supersedes the unresolved tab verdict above; overall broader coverage limits remain unchanged.
