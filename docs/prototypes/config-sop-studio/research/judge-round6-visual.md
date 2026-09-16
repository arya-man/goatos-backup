# Round 6 visual and interaction judge

Fresh Chrome tab on localhost4320, versioned production-shell.js?v=active-tab-2 confirmed from rendered script element. Review-only implementation scope. Desktop and390×844. Compared saved production shell conventions and prior visual findings; challenged keyboard use rather than repeating only pointer happy paths.

## New actionable defect

**P2 — Item dialog does not contain keyboard focus.** Open Items Config → New item. Focus remains on background +New item. Press Tab: focus moves to background All items20, with visible background focus ring beneath overlay, rather than entering the drawer. DOM confirms focused element outside .modal; modal has neither role=dialog nor aria-modal. Escape does not close. This makes keyboard navigation misleading and permits interaction with obscured background controls. Add dialog semantics, move focus into the dialog on open, contain Tab/Shift+Tab, restore opener focus on close, and define Escape dismissal behavior. This is a concrete observed defect, not inferred accessibility compliance.

## Passing checks

- Fresh modern Run insights deep link renders correct content.
- Versioned shell mobile active tab is visible: x302.68 onward within strip ending376; document overflow false and main scrollTop0. Prior round5 active-tab defect independently resolved.
- Mobile header/rail fit and Run insights cards wrap without overlap.
- Desktop item drawer title, close, item name/unit first row, item type, module selection and footer remain readable. Final name-first ordering is observed in current served source.

## Evidence and bounds

`research/round6-visual/insights-mobile.png` and `item-dialog-desktop.png` captured and visually inspected. The latter includes the background focus ring demonstrating the defect. No item created/saved; no live mutations. Viewport reset completed before desktop dialog test. Other operational routes, all mobile forms, light theme and exhaustive keyboard paths were not certified. Previous layout parity limitations remain; this round provides fresh bounded evidence, not whole-frontend certification.

## Authorized fix and verification

Added shared modal-accessibility.js wrapper (loaded last): labelled dialog semantics, focus entry, Tab/Shift+Tab containment, background inert while open, Escape delegates existing Close, opener restoration after existing close wrappers. Existing Close already discards unsaved forms; no new save/submit behavior. Direct-render preview replacements are handled by overlay observation, including opener capture. Summary controls participate in keyboard order. No generic/item permission rules changed.

Actual Chrome desktop: New item focus Close → Shift+Tab Create item → Tab Close → Escape removes dialog and restores +New item. Mobile hierarchy: Shift+Tab moves to +Add category within modal, Escape restores Manage hierarchy. `hierarchy-keyboard-mobile.png` visually inspected, showing final control focus inside bounded scrollable dialog. Viewport reset. Parent independently reports fresh IAB role dialog/focus Close/Escape restore passes.

`node judge-modal-accessibility.cjs` PASS: direct-render preview opener, replaced preview retention, forward/reverse trap, Escape and close restore, inert cleanup after direct teardown. Syntax check passes. Independent reviewer identified direct-preview capture gap and missing summary; both corrected before this result. Broader shared harness execution coordinated by parent; earlier missing MutationObserver test stub was a harness integration failure, not dismissed as green.
