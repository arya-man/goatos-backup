# Performance & Memory — Goat OS Mobile (Android)

Operators run cheap Android phones (target 2–3 GB RAM, weak GPU, old SoC, spotty
network). Performance and memory are **product requirements**, not polish. This
doc sets budgets and the rules/gates that hold them.

## 1. Budgets (measured on a target low-end device / emulator profile)

```text
Cold start (to interactive)        ≤ 2.5 s
Warm start                         ≤ 1.0 s
Scan tap → haptic + UI feedback    ≤ 120 ms
Shed list / scan list scroll       0 dropped frames (no jank), 60fps where panel allows
Screen transition                  ≤ 300 ms, no white flash
Submit round-trip (online)         ≤ 2 s p50 (excludes media upload, which is async)
APK/AAB base download              ≤ ~15 MB (no bundled fonts/heavy libs)
Steady-state RAM (scan open)       within low-end budget; no growth across sheds
Battery                            a full drive (hours of scanning) must not drain
                                   a low-end battery abnormally (BLE + screen only)
```

Gates: **Macrobenchmark** (cold start, scroll) + **Baseline Profiles** in CI on a
hosted emulator; regressions fail the build.

## 2. Compose performance rules

- **Stable state**: `@Immutable`/`@Stable` UI models; pass primitives/stable
  types to composables; avoid unstable `List`→ use `ImmutableList` (kotlinx
  collections) so Compose can skip recomposition.
- **No allocation in composition/draw**: hoist lambdas (`remember`), no `Modifier`
  chains rebuilt per frame, no object creation in `draw`/scroll.
- **`LazyColumn` with stable keys** for shed list, scan list, roster; never render
  a full list eagerly. Content-type set for heterogeneous rows.
- **derivedStateOf** for computed UI (group progress, ring fraction) so scroll
  and scans don't recompose the whole tree.
- **Ring/canvas**: draw with `Canvas`/`drawWithCache`; animate dash via a single
  `Animatable`, not per-frame recomposition.
- **Defer heavy work**: parse/diff on `Default`, IO on `IO`; UI thread only
  recomposes. Images via Coil with explicit target size (no full-res bitmaps).
- **Baseline Profile** shipped for the hot path: launch → today's sheds → scan.
- Enable Compose **strong-skipping**; run the **Compose compiler metrics** report
  in CI and fail on newly-unstable hot composables.

## 3. Memory-leak prevention (checklist — enforced)

Root causes we explicitly design out:

- **No `Context`/`Activity`/`View` in ViewModels or singletons.** ViewModels take
  use cases only; use `Application` context where unavoidable, injected via Hilt.
- **Lifecycle-scoped coroutines only**: `viewModelScope`, `repeatOnLifecycle`,
  WorkManager. No `GlobalScope`. Collectors cancel with the nav entry.
- **Release hardware handles deterministically**:
  - CameraX: bind to lifecycle; unbind on stop; close `ImageCapture`/recorder.
  - BLE/RFID: `RfidReaderPort.disconnect()` in `onStop`/scope cancellation;
    unregister SDK callbacks; close the `callbackFlow` (`awaitClose`).
- **No static references** to Activities, Views, Bitmaps, or Fragments; no
  long-lived listeners without unregister.
- **Bounded caches**: Coil memory cache capped; roster/scan lists bounded; live
  feed trimmed (mock trims to 14). No unbounded in-memory accumulation across
  sheds — persist to Room, don't hold.
- **Bitmaps/video**: capture at a bounded resolution/bitrate; never decode
  full-res into memory; recycle/close streams; upload from file, not memory.
- **Flows**: `WhileSubscribed(5s)` for screen state; cold flows for one-shot.
- **DB/cursors/files**: use Room (manages cursors); close any manual streams.

Tooling:
- **LeakCanary** in debug — any retained Activity/ViewModel/Fragment is a build
  smell to fix before merge.
- **CI leak assertion**: an instrumented run of the hot path asserts no retained
  destroyed lifecycle owners after navigation.
- **StrictMode** in debug (disk/network on main thread, leaked closables) → log
  and fix.
- Periodic **Android Studio Profiler / heap dump** review on the scan flow (open
  → scan 50 → submit → back, repeated) to confirm flat memory.

## 4. Network & battery on bad connectivity

- Reads cached in Room; UI never blocks on network.
- Sync/upload via **WorkManager** with network + battery constraints, exponential
  backoff + jitter; coalesce; respect Doze/standby.
- No polling loops; push (FCM) + on-open refresh + manual pull-to-refresh.
- BLE scanning bounded and stopped when not scanning; screen-on time minimized
  (the scan screen is the battery cost, not background work).

## 5. What we will NOT do

- No bundled custom fonts, no heavy chart lib, no WebView for product UI, no
  reflection-heavy libs, no `GlobalScope`, no full-list eager rendering, no
  full-res bitmaps in memory, no background polling, no unbounded in-memory
  queues (everything durable goes to Room/WorkManager).

## 6. Verification before any release

```text
[ ] Macrobenchmark cold-start + scroll within budget on low-end profile
[ ] Baseline Profile regenerated for the hot path
[ ] Compose stability report: no new unstable hot composables
[ ] LeakCanary clean on scan→submit→back loop
[ ] CI leak assertion green
[ ] Heap flat across repeated shed cycles
[ ] Crashlytics crash-free ≥ 99.5% on staged rollout before promote
[ ] Firebase Performance traces within budget on real devices
```
