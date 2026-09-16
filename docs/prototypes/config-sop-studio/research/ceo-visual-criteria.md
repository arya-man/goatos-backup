# Current acceptance: SOP composition

This replaces the earlier connected-work rule design. Configuration has two destinations: Items and settings, and Work instructions. Combining procedures happens inside an existing SOP through Stages and approvals. Show the whole stage sequence and parallel dependencies before editable detail. No separate event-rule directory or Run insights page. Procurement is an optional example.

## Visual and interaction requirements

- Retain production58px header,256px expanded navigation,62px narrow rail, independent accordion groups, existing production leaf labels, green selected indicator, existing panel/control/table style.
- New page heading matches selected navigation. On phone, the selected destination remains discoverable without horizontally hunting through hidden technical tabs.
- Prefer a short business summary before editable detail. One primary action per section; avoid competing save/test/publish controls with unclear consequences.
- Item/setting name comes before value details. Label units next to values. Hide internal IDs, keys and provenance mechanics in optional details.
- Explain configuration scope in business terms. Do not imply live changes, actual staff dispatch or production execution. One concise local-preview notice is sufficient.
- Form labels remain visible, error appears beside invalid input, entered values survive validation errors, focus stays inside dialogs and returns to opener on close.
- At390×844: no document horizontal overflow, visible close/footer actions, readable fields, no title behind header; tables may scroll within their own container. Avoid using most of the first viewport for introductions before any business action.

## Independent verification procedure

After implementation-ready notice, open a fresh browser route and verify actual loaded titles/navigation. Attempt each scenario using visible controls, record ambiguous choices or required prior knowledge, and save desktop/mobile screenshots only after visual inspection. Check a non-happy path: wrong unit or missing value, unmet preparation condition, blocked stage, changed approval, and independent practice runs. Verify old navigation labels remain unchanged. Clearly separate observed UI behavior, model-test support and untested production integration. Report failures before refinement; do not claim comprehension merely from attractive screenshots.

Baseline references: frontend-visual-baseline.md and visual-baseline/weighing-analytics.png, feed-config.png, procurement-sop.png, sop-detail.png. Production datasets are not required in local sample rows; existing surface anatomy and business meaning are required.

