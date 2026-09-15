# UX judge v2 — Items Config and linked SOP actions

Status: acceptance criteria prepared. No signoff until full implementation and final browser evidence.

## Complete acceptance checklist

### Standalone information architecture
- Items Config has a named, discoverable standalone entry alongside Common Config/SOP resources.
- All four levels are explicit: Vertical → Category → Subcategory → Item.
- A user can navigate, create and edit each supported level without confusing an item for a category.
- Persistent breadcrumbs identify the selected owner and hierarchy; back navigation preserves the parent context.
- Empty levels explain the next action; filters and search indicate their scope and clear correctly.

### Item definition
- Each item has a stable identity, readable name, owner vertical, category/subcategory, unit where applicable and active/archive status.
- Duplicate names are rejected or disambiguated inside the appropriate parent; identity does not depend on mutable labels.
- Edit, save, cancel and archive actions behave as labelled; invalid/missing fields remain visible with useful errors.
- Item details show module sharing and linked SOP actions; linking uses explicit selections rather than free-text names.
- Items Config is the single mock source for catalogue entries across Common and module SOP pickers.

### Linked SOP actions
- Action authoring can select an available item through Vertical → Category → Subcategory → Item context or equivalent clear picker.
- Linked action references survive renaming; item details list the actual SOP/module/node consumers.
- Selecting an item propagates to the draft graph and operator simulation.
- Unlinking removes the reference; archiving/revoking a consumed item reports impact and prevents invalid publication/execution.
- Shared item availability is restricted to the owning and selected consumer modules; sharing selection never implies permission to administer/deduct stock.
- Owner and consumer boundaries are visible; no independent duplicate catalogue copies remain.

### Existing scope and roles
- Existing module Config/SOP placement remains intact, including Others feature separation.
- CEO/CXO can author; Director/Operator cannot edit configuration or inspect unpublished workflows.
- Published workflow snapshots remain distinct from draft edits.
- Existing saved mock state migrates without losing workflows, sharing or Sales values.
- All visible buttons, tabs, selectors, dialogs and navigation routes produce intended outcomes.

### Regression and visual proof
- Sales rule publication still propagates to Weighing; comparison and allowance semantics remain explicit.
- Common resource/library sharing still controls editor availability.
- Graph select, add, connect, edit, drag, delete, validate and publish remain functional.
- Operator simulation still covers valid/invalid numeric input and both decision branches.
- Desktop and narrow screenshots show readable hierarchy, actionable details, reachable dialogs and contained scrolling.
- Browser console has no feature-caused errors during tested paths.

## Highest-impact improvements
1. Persistent owner/hierarchy breadcrumbs in all item pickers.
2. Visible Used by N SOP actions and impact review before availability changes.
3. One Items Config catalogue source replacing repeated static medicine lists everywhere.

## Scope honesty
Mock-only operations must remain labelled. Production inventory, dispensing, tenant authorization and clinical protocols are not implied by editable example data.
