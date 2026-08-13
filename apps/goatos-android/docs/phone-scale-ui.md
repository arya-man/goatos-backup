# Phone-scale UI rules

A real park is ~100 sheds x ~70-90 animals/shed — roughly 7,000-8,000 rows in a single park,
picked from thousands more parks/operators/dates. A phone viewport shows ~7-10 rows at once. Any
screen, list, or picker that renders/holds "all of it" instead of a bounded, windowed slice will
work fine against a 5-goat dev fixture and then jank, OOM, or crash the moment it meets real farm
cardinality. This anti-pattern class has shipped three times in this app (unbounded lazy-list
rendering, chip pickers over an unbounded dimension, and a full-screen spinner replacing already-
rendered content) — this doc is the standing rule so it does not ship a fourth time.

Canonical rulebooks this doc composes with (read these too, do not contradict them):
`docs/decisions/mobile-data-fetch-anti-patterns.md` (fetch/pagination), `docs/mobile/android-ui-quality.md`
(loading-state UI floor), `.agents/skills/mobile-anti-patterns/SKILL.md` (agent-facing summary).
Machine gate: `make android-compose-lists-guard` (`tools/agent-hooks/check-android-compose-lists.mjs`).

## 1. Windowing — never render an unbounded list without a `Lazy*` + pagination

Every list of real farm cardinality (sheds, animals, obligations, operators) renders through
`LazyColumn`/`LazyRow`/`LazyVerticalGrid` with a stable `key`, paginated as a **keyset page of
~20**, never more — this is the same pagination contract as
`docs/decisions/mobile-data-fetch-anti-patterns.md` rule 2 ("Every drill level paginates"); this
doc does not loosen or restate a different number. ~10 rows are typically visible at once; ~20 is
the outer page-size cap, not a target to render eagerly.

**Correct**
```kotlin
LazyColumn {
    items(sheds, key = { it.shedId }, contentType = { "shed_row" }) { shed ->
        ShedRow(shed)
    }
}
// sheds comes from a Paging/keyset-window ViewModel state — never the whole park's sheds at once.
```

**Incorrect**
```kotlin
Column(modifier = Modifier.verticalScroll(rememberScrollState())) {
    state.sheds.forEach { shed -> ShedRow(shed) } // renders all ~100 sheds up front, no windowing
}
```

Also banned: a `LazyColumn`/scrollable `Column` nested directly inside another list's `items()`
row lambda (two scrollables sharing one measure/scroll axis — an infinite-constraint or
double-scroll bug). Hoist the inner list to its own destination/sheet.

Machine-checked: `lazy-list-missing-key`, `lazy-list-entity-id-key`,
`nested-scroll-in-lazy-items`, and `column-foreach-unbounded` in
`check-android-compose-lists.mjs`.

## 2. Chips are for a small, fixed set — not an unbounded dimension

A chip row is fine for ~3-8 fixed choices (a status filter, a severity tone). It is **never** the
picker for an unbounded dimension — sheds, animals, operators, or dates — because a chip row has
no search, no pagination, and grows the layout linearly with row count (100 shed chips does not
fit, does not scroll sanely, and is unusable to tap). Those dimensions use a **searchable
selector**: a compact trigger row plus a modal dialog with a search field and a paginated/filtered
list.

The reference implementation is `FilterSelectorRow` + `SearchablePickerDialog` in
`apps/goatos-android/feature/feature-weighing/src/main/kotlin/sg/mesha/goatos/feature/weighing/WeightHistoryChartScreen.kt`
— `FilterSelectorRow` renders the current selection as a compact tappable trigger (not one chip
per option), which opens `SearchablePickerDialog`: a `TagSearchField` plus a filtered `PickerRow`
list. New shed/animal/operator/date pickers should follow this same shape rather than reinventing
one.

**Correct** — unbounded dimension behind a searchable trigger:
```kotlin
FilterSelectorRow(label = "Shed", selected = selectedShed?.name, onClick = { showShedPicker = true })
if (showShedPicker) {
    SearchablePickerDialog(options = sheds, onSelect = { selectedShed = it }, onDismiss = { showShedPicker = false })
}
```

**Incorrect** — one chip per shed:
```kotlin
Row(modifier = Modifier.horizontalScroll(rememberScrollState())) {
    sheds.forEach { shed -> FilterChip(selected = shed == selectedShed, onClick = { selectedShed = shed }, label = { Text(shed.name) }) }
}
```

Machine-checked in the narrow shape shown above: `chip-row-unbounded-dimension` in
`check-android-compose-lists.mjs` flags `<state-or-domain-chain>.forEach { ... FilterChip/AssistChip
... }` where the chain matches a state/domain keyword allowlist (state, list, sheds, animals,
operators, dates, ...). Whether a chip row is backed by an unbounded collection at runtime cannot
be fully decided from the call site by static analysis, so this is deliberately conservative: a
`listOf(...)` literal or `.entries` receiver is never flagged, and any indirection through a
variable/helper defined elsewhere is a false negative by design. A chip row built a different way
(no `.forEach`, or the collection passed through a helper) is not caught by the guard — still
enforced at review time via `.agents/skills/mobile-anti-patterns/SKILL.md`.

## 3. Skeleton, not spinner — never discard already-rendered content

A cold load with no cache yet may show a skeleton/shimmer placeholder. A refresh over content the
user has already seen must **never** replace that content with a full-screen spinner or a
"Loading…" wall — it annotates the existing content (`Syncing…`, `Offline · updated…`, a retry
affordance) and keeps rendering it. This is the existing release-floor rule in
`docs/mobile/android-ui-quality.md` ("Cold-load content areas must render skeleton/shimmer
placeholders... A refresh/retry over cached data must annotate the existing content... instead of
replacing it with 'Loading…'"); this doc does not add a second, different rule — it names the
anti-pattern for the phone-scale context (an operator scanning 80 animals in a shed loses their
place if a mid-scan refresh blanks the screen).

**Correct**
```kotlin
if (state.isInitialLoading && state.rows.isEmpty()) {
    ShedListSkeleton()
} else {
    ShedList(state.rows, isSyncing = state.isRefreshing) // keeps rendering rows during refresh
}
```

**Incorrect**
```kotlin
if (state.isLoading) {
    CircularProgressIndicator() // discards already-rendered rows on every refresh
} else {
    ShedList(state.rows)
}
```

Machine-checked in a narrow shape: `spinner-replaces-cached-content` in
`check-android-compose-lists.mjs` flags a `when { }` branch guarded by a BARE loading flag (no
`&&` compounding it with a cache-emptiness check) rendering only `CircularProgressIndicator(...)`,
alongside a sibling `when` branch that renders non-empty cached state. The vaccination-sheds screen
also has a dedicated, stricter check in `check-android-ui-foundations.mjs`. Both are deliberately
narrow — distinguishing "content existed and was thrown away" from "nothing has loaded yet" needs
runtime/data-flow knowledge a static grep does not fully have, so a plain `if (loading) {...} else
{...}` chain (no `when {}`, no sibling-branch proof of a cache) is a false negative by design.
Enforced at review time for those shapes via `.agents/skills/mobile-anti-patterns/SKILL.md`.
