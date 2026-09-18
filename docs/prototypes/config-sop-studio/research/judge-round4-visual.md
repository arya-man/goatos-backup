# Round 4 visual judge

Fresh Chrome review 2026-09-16. IAB unavailable in this subagent. Chrome initially timed out, recovered via dedicated blank tab then navigation; later intermittent timeouts returned. No production changes. Latest local desktop and390×844 typed form inspected against saved production baseline and round3 evidence.

## Results

- **Pass:** ADG Apply now beside weight input; Download on its own right row. Header/sidebar/cards/table fit desktop. Exact app parity remains partial because sample tabs and filters are disabled.
- **Pass:** scroll main ADG downward then navigate Items Config resets to heading/top controls. Actual screenshot `items-scroll-reset.png` shows the intended top-of-page state.
- **Pass:** modern direct `#/configuration/workflow-links` navigation and reload retain workflow page (`Try a scoped run` in rendered DOM), after parent fix.
- **Pass:** new typed configuration drawer desktop/mobile has accessible close, labelled value/unit fields and fixed actions, no horizontal clipping.
- **Pass with limitations:** desktop numeric branch panel shows comparison/operator/destination controls without overlap. Changed local sample question Yes/No to Number only to inspect, then restored Yes/No; no publish. Existing Yes branch literal remains after type change—potential validation concern already sent parent, not classified as a proven execution failure.

## Remaining findings

1. P3 ADG density still differs from production. Filter strip spacing and card/table padding push pen table header to about790px versus686px in baseline. Reducing top filter padding and text/card gaps would improve comparable first-viewport information density; do not remove explanatory semantics.
2. P2/P3 typed form asks for value before visible item name. In current observed mobile form, Item name starts at877px below viewport; Create item is visible at bottom. Parent authorized moving name row after Item type before typed fields. One scoped DOM reorder was added in typed-config.js and syntax checked. The subsequent captured `config-mobile-after.png` still showed old order, so **visual fix not certified** (possible cached script or concurrent changes). Parent must verify final served ordering; do not present this image as a successful after proof.
3. Existing editor can scroll heading partly behind sticky header while navigating branch inspector; canvas and inspector remain usable but heading is clipped in `numeric-branch-desktop.png`. Separate route scroll reset does not prevent legitimate inspector-driven main scroll.

## Visually inspected evidence

Under `research/round4-visual/`: adg-desktop.png, items-scroll-reset.png, config-desktop.png, config-mobile.png, numeric-branch-desktop.png, config-mobile-after.png. Every file was rendered and inspected. Last after image documents unresolved ordering rather than success.

Feed/hierarchy latest states were not freshly captured this round; round3 evidence remains. Numeric mobile editor, all routes, light theme and full end-to-end publication not certified. Viewport reset completed. Scope of edits: single name-row ordering statement in typed-config.js only, authorized after initial read-only phase. No visual green claim for unobserved states.

## Final bounded verdict

Name-first refinement is now **visually verified**. Parent independently confirmed1280×720 desktop. This judge then opened a fresh Chrome tab and inspected `round4-visual/config-mobile-final.png` at390×844: Item type → Item name → Value type → Value → Unit. Name is fully visible near the top; close and footer actions remain reachable without overlap. This supersedes the earlier unresolved cached after capture. Viewport reset succeeded. No further styling edits made. Remaining density differences are minor; broader unsupported/sample surfaces and untested states remain explicitly outside this bounded pass.
