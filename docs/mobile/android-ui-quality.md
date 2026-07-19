# Android UI quality foundation

This is the release floor for every Goat OS Android surface, regardless of role or module.
The app must behave like one Android application: navigation, state restoration, lifecycle,
accessibility, geometry, typography, color, loading/empty/error states, and screenshots are
system concerns rather than per-screen polish.

## Required implementation rules

- Compose the cold-start destination from the first backend-visible supported root. Root-tab
  navigation must use single-top plus save/restore state; child routes use the hosted NavHost and
  Back/Up rather than drawing one screen over another.
- Persist business progress before presenting it as complete. Process recreation must restore
  scan/proof state from Room; transient ViewModel state may only be an immediate overlay.
- Bind CameraX use cases to a lifecycle and unbind them on both view release and composition
  disposal. Proof capture is an exclusive full-screen surface; no BLE/RFID screen may remain
  visible or interactive behind it.
- Use Material components where possible. Every custom interactive target is at least 48x48dp,
  has a meaningful content description/action and role, and does not rely on color alone.
- Use design-system colors, type, spacing and shapes. Peer cards and buttons keep equal geometry
  across state changes; copy changes must not resize one item differently from its peers.
- Render cached content during refresh. Loading, empty, error, offline and disabled states must be
  explicit and screenshot-tested at compact and expanded widths.

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
- [Configure Android lint](https://developer.android.com/studio/write/lint)
- [Compose performance](https://developer.android.com/develop/ui/compose/performance)
- [Compose stability](https://developer.android.com/develop/ui/compose/performance/stability)
- [Macrobenchmark overview](https://developer.android.com/topic/performance/benchmarking/macrobenchmark-overview)
- [Baseline Profiles](https://developer.android.com/topic/performance/baselineprofiles/overview)
- [Kotlin coroutines guide](https://kotlinlang.org/docs/coroutines-guide.html)
- [LeakCanary](https://square.github.io/leakcanary/)

Context7 may retrieve version-specific AndroidX/Material/CameraX snippets, but these Google pages
and the corresponding AndroidX source remain authoritative.
