# Android UI quality foundation

This is the release floor for every Goat OS Android surface, regardless of role or module.
The app must behave like one Android application: navigation, state restoration, lifecycle,
accessibility, geometry, typography, color, loading/empty/error states, and screenshots are
system concerns rather than per-screen polish.

## Required implementation rules

- Compose the cold-start destination from the first backend-visible supported root. Root-tab
  navigation must use single-top plus save/restore state; child routes use the hosted NavHost and
  Back/Up rather than drawing one screen over another.
- Deep-link an executable workflow only with its complete execution identity. Vaccination scan,
  proof, and submit routes require both `shed_id` and `task_id`; a shed-only FCM/calendar target
  must return to the Vaccination task list instead of rendering an ambiguous 0/N task that cannot
  persist or submit. Calendar task events preserve the task id from their canonical
  `calendar:<task_id>` identity.
- Persist business progress before presenting it as complete. Process recreation must restore
  scan/proof state from Room; transient ViewModel state may only be an immediate overlay.
- Bind CameraX use cases to a lifecycle and unbind them on both view release and composition
  disposal. Proof capture is an exclusive full-screen surface; no BLE/RFID screen may remain
  visible or interactive behind it.
- Use Material components where possible. Every custom interactive target is at least 48x48dp,
  has a meaningful content description/action and role, and does not rely on color alone.
- Use design-system colors, type, spacing and shapes. Peer cards and buttons keep equal geometry
  across state changes; copy changes must not resize one item differently from its peers.
- Apply system-bar insets exactly once. When a Material `Scaffold` supplies `innerPadding`, the
  shell must both apply that padding and call `consumeWindowInsets(innerPadding)` before composing
  a child route. A route may keep `safeDrawing` padding so it is safe when rendered standalone;
  consumed parent insets then reduce that child padding to zero instead of doubling the status-bar
  and gesture/navigation-bar gaps.
- Render cached content during refresh. Loading, empty, error, offline and disabled states must be
  explicit and screenshot-tested at compact and expanded widths.
- Cold-load content areas must render skeleton/shimmer placeholders, not a text-only loading card
  or a full-screen loading wall. The app shell and bottom navigation stay structurally visible
  while route content loads. A refresh/retry over cached data must annotate the existing content
  (`Syncing…`, `Offline · updated…`, retry affordance, etc.) instead of replacing it with
  “Loading…”. Any route that introduces `isLoading`/`isRefreshing` needs a screenshot regression
  proving the skeleton path and a device/E2E observation when it is part of a release flow.
- Do not render the container for an absent status. A blank sync/state label must remove its whole
  banner, including background, padding, and status icon; a lone dot or empty strip is a release
  failure and needs an unanswered-draft screenshot regression.
- Render task identity and summary copy only from the backend `TaskPresentation` contract. Raw
  `title`, `description`, UUID `scope_id`, and other transport facts are not display fallbacks.
  Omit summary rows whose label or value is empty; constrain both columns so long localized text
  wraps inside the card instead of pushing its peer off-screen.
- Never render raw vaccination config/protocol tokens as API presentation copy
  or user copy. Values such as
  `et_tt`, `et_tt_adult_w2`, `ppr_booster`, `blue_tongue_first`, `goat_pox`,
  `Preventive Care Vaccination Matrix`, or any other backend/config key are
  identifiers, not labels. Android screens, cards, rows, chips, empty states,
  alerts, Paparazzi screenshot fixtures, screenshot galleries, and logs visible
  to an operator must map them to human labels first: `ET+TT`, `PPR · Booster`,
  `Blue Tongue`, `Goat Pox`, etc. Backend endpoints must do the same for
  presentation fields such as `driveName`, `vaccineLabel`, `vaccine_labels`,
  card titles/subtitles, and alerts. Raw tokens may appear only in backend
  config, raw storage/contracts, DTOs, non-UI tests, or a dedicated
  display-mapping function.
  `make ui-vaccine-labels-guard` enforces this and runs through local CI.
- Keep each scan-proof row as one responsive information/action group: animal identity and vaccine
  copy together, then proof state and its Material action. Compact widths stack the action; wider
  widths may align it beside status. `MISSING`, `UPLOADING`, `SYNCED`, and `FAILED` all require
  screenshot coverage, with retry/camera targets at least 48dp.
- Treat the SOP form compatibility declaration as an executable client contract. Every advertised
  field type must have a real renderer (`select` and `date_time` cannot fall through to an
  unsupported placeholder), every backend-authored conditional rule must be evaluated, and an
  explicit boolean `No` is an answered value rather than an absent value. Required booleans use a
  nullable Yes/No choice so `false` can be recorded without the operator toggling twice. Picker
  menus match the field width and timestamps submit RFC 3339 values while displaying localized
  device time.
- Submit resolved goat UUIDs in `goat_ids`; RFID strings are lookup input, not medical-record
  identity. Per-goat proof `subject_id` and each submitted goat identifier must use the same
  canonical ID or backend proof validation will correctly reject the record.
- Vaccination operator screens must treat the RFID scan timestamp as the exact
  vaccination time. The timestamp must be persisted Room-first, displayed back in
  the scan/proof UI in India/local operator time, draft-synced to the backend,
  and used by backend vaccination completion fan-out as `administered_at`.
  Server submit time is not allowed to replace a valid scan timestamp.
- Pagination on mobile work surfaces is viewport-driven. If a list has another
  page, start fetching automatically near the end of the visible list and show a
  spinner/progress footer only. Do not ship visible "Load more" buttons on
  operator queues, scan rosters, verification queues, alerts, or side drawers.

## Mandatory proof

Run from the repository root:

```bash
node tools/agent-hooks/check-android-ui-foundations.mjs --self-test
node tools/agent-hooks/check-android-ui-foundations.mjs
make ui-vaccine-labels-guard
make mobile-guard
make ci-local JOB=android
```

The Android CI job compiles and runs unit tests, Android lint, and the committed Paparazzi golden
suite. A changed screen must update or add a semantic test and the relevant golden intentionally.
For navigation, camera, RFID/BLE, keyboard-wedge scanning, process recreation and system insets,
also exercise the real route on a physical device and retain the evidence.

## Primary sources

- [Test Navigation Compose](https://developer.android.com/guide/navigation/testing/compose)
- [Compose accessibility defaults and 48dp targets](https://developer.android.com/develop/ui/compose/accessibility/api-defaults)
- [Compose semantics](https://developer.android.com/develop/ui/compose/accessibility/semantics)
- [CameraX architecture and lifecycle](https://developer.android.com/media/camera/camerax/architecture)
- [Saving UI state](https://developer.android.com/develop/ui/compose/state-saving)
- [Test different screen sizes](https://developer.android.com/training/testing/different-screens)
- [Screenshot testing](https://developer.android.com/training/testing/ui-tests/screenshot)
- [Material 3 in Compose](https://developer.android.com/develop/ui/compose/designsystems/material3)
- [Material 3 system insets](https://developer.android.com/develop/ui/compose/system/material-insets)
- [Compose inset consumption](https://developer.android.com/develop/ui/compose/system/insets-ui#inset-consumption)
- [Configure Android lint](https://developer.android.com/studio/write/lint)
- [Compose performance](https://developer.android.com/develop/ui/compose/performance)
- [Compose stability](https://developer.android.com/develop/ui/compose/performance/stability)
- [Macrobenchmark overview](https://developer.android.com/topic/performance/benchmarking/macrobenchmark-overview)
- [Baseline Profiles](https://developer.android.com/topic/performance/baselineprofiles/overview)
- [Kotlin coroutines guide](https://kotlinlang.org/docs/coroutines-guide.html)
- [LeakCanary](https://square.github.io/leakcanary/)

Context7 may retrieve version-specific AndroidX/Material/CameraX snippets, but these Google pages
and the corresponding AndroidX source remain authoritative.

## Phone-scale UI rules (machine: `make android-compose-lists-guard`)

A park holds ~100 sheds x ~70-90 animals; a full task export is ~8,000 rows. A phone screen shows
~10, never more than ~20, and every drill level paginates ~20 at a time (see
[`mobile-data-fetch-anti-patterns.md`](../decisions/mobile-data-fetch-anti-patterns.md)). These
three shapes have each shipped at least once and are now guarded/reviewed against before a screen
is written:

**1. Chips are for a small FIXED set only.** A chip row is one pill per element — correct for 2-3
literal values (an individual/lump-sum toggle), wrong for anything that grows with farm size
(sheds, animals, operators, dates, parks). The guard's `chip-row-unbounded-dimension` rule flags a
`<state-chain>.forEach { FilterChip(...) }` shape; a directly-chained `listOf(...)` literal or
`.entries`/`.values()` is exempt.

```kotlin
// WRONG — a park has ~100 sheds; five chips already wrap and push content off-screen
Row {
    state.sheds.forEach { shed -> FilterChip(selected = false, onClick = { onSelectShed(shed.id) }, label = { Text(shed.name) }) }
}

// CORRECT — one-line selector -> searchable bottom sheet (FilterSelectorRow +
// SearchablePickerDialog, feature-weighing/WeightHistoryChartScreen.kt)
FilterSelectorRow(label = state.shedSelectorLabel, options = state.shedChips, allLabel = state.allShedsLabel, onSelect = onSelectShed)

// FINE — a small fixed set (2-3 literal values)
Row {
    listOf("Individual", "Lump-sum").forEach { mode -> FilterChip(selected = mode == state.mode, onClick = { onSelectMode(mode) }, label = { Text(mode) }) }
}
```

**2. Render everything up front (no windowing).** A `forEach` that emits composables inside a
scrollable `Column`/`Row`, or a `Lazy*` list nested inside another list's `items()` row, inflates
and measures every row at once — fine for a handful of fixed rows, silently unbounded once fed
state/domain data at real park scale. Guarded by `column-foreach-unbounded` and
`nested-scroll-in-lazy-items`.

```kotlin
// WRONG — state.tasks can be ~8,000 rows; forEach inflates all of them
Column(modifier = Modifier.verticalScroll(rememberScrollState())) {
    state.tasks.forEach { task -> TaskRow(task) }
}

// CORRECT — lazy + keyset pagination (~20/page)
LazyColumn {
    items(state.tasks, key = { it.id }) { task -> TaskRow(task) }
}
```

**3. A full-screen spinner must not tear down content the screen already has cached.** Guard by
`spinner-replaces-cached-content`: a `when { }` branch guarded by a bare loading flag (not
compounded with a cache-emptiness check) whose only content is `CircularProgressIndicator`, next
to a sibling branch that renders non-empty cached state, replaces rendered rows with a spinner on
every refresh instead of overlaying/annotating them.

```kotlin
// WRONG — refresh discards the already-rendered gallery
when {
    state.loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
    state.sheds.isNotEmpty() -> Gallery(state.sheds)
    else -> EmptyMessage()
}

// CORRECT — WeighingLeadershipVideosScreen.kt: spinner only when there is nothing cached to read
when {
    state.loading && state.sheds.isEmpty() -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
    state.sheds.isNotEmpty() -> Gallery(state.sheds)
    else -> EmptyMessage()
}
```

All three are deliberately conservative — see the header comment in
`tools/agent-hooks/check-android-compose-lists.mjs` for the exact allowlists and by-design false
negatives (indirection through a variable, non-`when` if/else chains, first-load-only screens with
no cache concept, etc.). A genuinely-bounded case may append `compose-guard:ignore: <reason>` on
the line.

## Compose lazy-list keys (machine: `make android-compose-lists-guard`)

Every `LazyColumn`/`LazyRow`/`LazyVerticalGrid` item needs a stable key that is
**unique per rendered row**, not per domain entity. Keying a per-row list (one row
per obligation) by a per-entity id (`goatId`) crashes on any entity that owns two
rows — a goat with two due vaccines produced
`IllegalArgumentException: Key "<uuid>" was already used` in the LazyList measure
pass and popped the screen (shipped `0.1.6-stg`, fixed `a9c35a1d`). Key the unique
per-row id (`obligationId`) or a composite (`"${it.goatId}|${it.vaccineLabel}"`),
and always pass a `key` to `items()`/`itemsIndexed()` over a collection. Rule +
guard details live in
[`docs/decisions/mobile-data-fetch-anti-patterns.md`](../decisions/mobile-data-fetch-anti-patterns.md)
→ "Compose lazy-list key correctness".
