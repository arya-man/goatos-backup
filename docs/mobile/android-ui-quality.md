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
- Do not render the container for an absent status. A blank sync/state label must remove its whole
  banner, including background, padding, and status icon; a lone dot or empty strip is a release
  failure and needs an unanswered-draft screenshot regression.
- Render task identity and summary copy only from the backend `TaskPresentation` contract. Raw
  `title`, `description`, UUID `scope_id`, and other transport facts are not display fallbacks.
  Omit summary rows whose label or value is empty; constrain both columns so long localized text
  wraps inside the card instead of pushing its peer off-screen.
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

## Mandatory proof

Run from the repository root:

```bash
node tools/agent-hooks/check-android-ui-foundations.mjs --self-test
node tools/agent-hooks/check-android-ui-foundations.mjs
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
