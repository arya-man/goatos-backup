# Round 3 visual regression judge

2026-09-16. Fresh local Chrome tab at localhost:4320. No implementation edits or production writes. Compared actual opened screenshots `visual-baseline/weighing-analytics.png` and `feed-config.png`, baseline inventory/report, and the two attached WhatsApp screenshots (requirements, not UI styling references). Later user screenshot files were not available to this subtask; no independent visual claim for those images.

## Verdict

The prototype now uses the existing shell and extends configuration inside it. Desktop/mobile shell and item dialogs are usable. **Exact existing UI parity is not yet met.** Remaining differences are observable control omissions, spacing and preview-only destinations; do not call this whole-frontend visual regression green.

## Actionable findings

1. **P2 — Feed Config drops existing scope and filter controls.** Production has FEED / RATION GRID breadcrumb, park-specific scope explanation, Park/Breed/Pen tag/Feed item/Grams filters, Apply and Effective to column. Current preview starts directly at the title and one-row ration table, omitting these controls. Restore their layout around illustrative rows; park ownership must be visible because the global All parks selector otherwise implies a scope that this ration row does not represent. Preserve the existing inline Edit rate pattern.
2. **P2 — Analytics interaction coverage remains partial.** Six tabs and the Park/Period/Weighing/Sex/Origin controls are disabled. This is honestly exposed, but it does not reproduce the existing analytics UX. Local sample interactions or clearly scoped acceptance are required before whole-page parity. Download and Last weighed are now visible, resolving their earlier omission.
3. **P3 — Navigation spacing and icon fidelity differ.** At desktop, Weighing appears around y326 versus y302 in baseline with comparable preceding groups; mock top-level/group spacing accumulates. Production icons and notification bell differ from mock symbols. Match original shell spacing/icon assets rather than approximate glyphs; exact Sales submenu labels and independent expanded state already pass.
4. **P3 — ADG control positioning and card density differ.** Production Download occupies the right end of a second filter row; mock shares the first row. Production average-weight Apply sits immediately after the number field; mock puts it at far right. Current cards are taller and push the pen table lower. Match original control and padding rules. Omitted CPT sample card is a data-coverage difference, not a reason to invent values.
5. **P3 — New item form is text-heavy on narrow screens.** Module sharing explanations occupy most of the first viewport; category/subcategory appear below the fold. No clipping or blocked control observed, and fixed footer actions remain accessible. A compact explanation/details disclosure would retain semantics while making item hierarchy easier to reach.

## Passing checks

- Exact Sales leaf sequence: Summary, Farm value, Load wise, Market analytics, Buyer analytics, Vendors, Sales Config.
- Weighing and Sales accordions stay expanded independently; Feed opens without closing them.
- Existing 58px header/256px navigation structure and accent/panel palette are recognizable. Added Configuration uses matching leaf treatment.
- Fresh ADG screenshot contains Download, weight filter/Apply and Last weighed.
- Hierarchy modal is centered and bounded desktop, fits390px narrow width, visible close action; no title hidden by header.
- New item form at390px has readable controls, visible close and fixed Cancel/Create footer, no horizontal clipping.
- Mobile rail opens module navigation and selecting ADG closes the drawer. Header park scope/account fit.
- Mobile ADG stacks filters and wraps tabs without horizontal clipping in the captured viewport.

## Fresh visual evidence and coverage

Every screenshot below was emitted and visually inspected after capture under `research/round3-visual/`:

| File | Viewport / state |
|---|---|
| adg-desktop.png | 1728px desktop analytics, latest controls |
| hierarchy-desktop.png | desktop open hierarchy modal |
| hierarchy-mobile.png |390×844 hierarchy modal |
| item-mobile.png |390×844 new catalogue item form |
| adg-mobile.png |390×844 analytics filter/card layout |
| feed-desktop.png | desktop Feed Config |

Viewport reset completed. No form value was changed or saved. Existing local judge-created sample categories were present; no live data copied. This round did not freshly inspect Farm value, SOP editor, light theme, every route, or lower scrolled mobile tables; prior evidence exists but is not a current full certification. No automated pixel-diff suite was run. Source and production deployment SHA parity is still qualified by the baseline report.

## Authorized refinement follow-up

After the read-only report, parent authorized scoped changes. Added Feed breadcrumb, explicit park-specific sample notice, existing filter anatomy (disabled and explained where sample filtering is unsupported), Effective to column. Adjusted navigation group/row spacing, compacted KPI padding, moved ADG Download to its own right-aligned row and Apply beside its weight input. No configuration or permission logic changed.

`round3-visual/feed-desktop-after.png` was freshly captured and visually inspected: breadcrumb/filter row/Effective to fit with no clipping. Weighing navigation position improved from about y326 to307, versus baseline302. JS syntax and shell judge pass. ADG after capture was blocked by repeat Chrome CDP timeouts; parent should complete current browser E2E before marking that final visual change certified. Earlier mobile evidence predates these scoped changes.

Parent follow-up: captured and visually inspected final ADG at1280×720 in the in-app browser after Chrome interaction deadlines. Download has its own right row; top filters visible. Apply adjacency observed on earlier scrolled view. Navigation resets both main/document scroll after discovering a hidden-header offset. Evidence: final-visual/adg-round3-after.png. This is representative visual comparison, not automated pixel-diff certification.
