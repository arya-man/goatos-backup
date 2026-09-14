# Android Field Operator App Domain — Technical Documentation

**Module:** GoatOS Android Field Operator Application (`apps/goatos-android`)
**Document Version:** 1.0
**Date:** 2026-09-13

---

## 1. Overview

The Android Field Operator App Domain is the native Android client of the GoatOS platform, purpose-built for on-farm workers who perform daily operational tasks — RFID-based animal identification, feed distribution, preventive care, vaccination execution, weighing, census counts, and leadership task follow-up. It is the primary interface between the physical farm and the GoatOS backend, and it is explicitly designed as an **offline-first** system: every field capture must succeed and be preserved even when the device has no connectivity, and must reliably reach the backend once connectivity is restored.

Architecturally, this domain is a **presentation domain** in GoatOS's overall modular-monolith landscape, but internally it applies its own strict layered decomposition — a multi-module Gradle project with clear dependency direction (`feature → core → device`, never `feature → feature`) — mirroring, at a smaller scale, the same discipline the Go backend applies with its hexagonal `domain/app/ports/adapters` structure.

Key responsibilities of this domain:

- **App bootstrap and session orchestration** — gating startup behind force-update, authentication, and a backend-composed navigation state machine.
- **Bluetooth HID (keyboard-wedge) RFID integration** — the field mechanism for identifying animals.
- **Offline-first local data & sync layer** — Room-backed outbox pattern mirroring the backend's own transactional outbox.
- **Push notification handling** — Firebase Cloud Messaging (FCM) for operational alerts and deep-linking.
- **Telemetry and analytics** — dual-channel (Firebase + backend-mirrored) event tracking, crash reporting, and performance tracing.
- **Force-update enforcement** — Firebase Remote Config gate blocking outdated builds before any other UI renders.
- **Feature workflows** — 20+ Gradle feature modules implementing task-specific capture flows (feed, health, vaccination, weighing, counts, pen visits, leadership tasks, etc.).

---

## 2. Module Structure & Build Topology

### 2.1 Multi-module Gradle layout

The project (`rootProject.name = "goatos-android"`) is organized into three module families, enforced by `settings.gradle.kts` and by convention (never by a hard Gradle dependency-boundary linter mentioned in-repo):

| Layer | Modules | Dependency rule |
|---|---|---|
| **`app`** | The thin shell: `MainActivity`, `GoatOsApplication`, DI wiring (`di/`), navigation shell (`ui/GoatOsShell`), boot state machine (`boot/`), RFID activity-level capture (`rfid/`), push (`push/`), sync services (`sync/`), analytics adapters (`analytics/`) | Depends on all `core-*` and `feature-*` modules |
| **`core`** | `core-model`, `core-common`, `core-designsystem`, `core-ui`, `core-network`, `core-media`, `core-datastore`, `core-data`, `core-database`, `core-analytics`, `core-notifications`, `core-permissions`, `core-testing` | `core-model`/`core-common` are Android-framework-free; other core modules may depend on each other but never on `feature-*` |
| **`feature`** | 20 modules: `feature-auth`, `feature-calendar`, `feature-clock`, `feature-counts`, `feature-feed`, `feature-health`, `feature-pccare`, `feature-sheds`, `feature-scan`, `feature-submit`, `feature-record`, `feature-profile`, `feature-timetable`, `feature-leadership-tasks`, `feature-pen-visits`, `feature-toxin`, `feature-vendors`, `feature-vaccination`, `feature-verify`, `feature-weighing`, `feature-workboard` | May depend only on `core-*`; **feature-to-feature dependencies are forbidden** |
| **`device`** | `device-rfid`, `device-camera`, `device-feedback` | Vendor SDK integrations live ONLY here, always behind a port interface; each ships a fake/test double |

This layering keeps hardware/vendor concerns (`device-*`), reusable infrastructure (`core-*`), and business capture flows (`feature-*`) cleanly separated, letting individual features be built, tested, and reasoned about independently while sharing one Room database and one sync engine.

### 2.2 Build flavors and variants

The `app` module defines a single `env` flavor dimension with three flavors, all sharing one `applicationId` base (`sg.mesha.goatos`) — role differentiation is a runtime concern, not a build-time one (an explicit ADR decision recorded in code comments):

| Flavor | Purpose | Auth mode | API base | Telemetry |
|---|---|---|---|---|
| `dev` | Local development against `http://localhost:8080/` (via `adb reverse`) | Local HS256 dev bearer token (`AuthMode.DEV_BEARER`) | Configurable, defaults to `localhost:8080` | `TELEMETRY_ENABLED` configurable, default `true`, reports into the `goatos-stg` Firebase project |
| `stg` | Staging environment | Firebase Auth (`AuthMode.FIREBASE`) | `https://stg-api.dashboard.mesha.sg/` | `TELEMETRY_ENABLED=true`, confirmed Firebase project |
| `prod` | Production | Firebase Auth | production API (TBD in build config) | Firebase project not yet confirmed at time of writing |

A dev-only `SCAN_SCOPE_PREFIX` build-config flag namespaces RFID tag reads per shed for testers who reuse a small pool of physical tags across many sheds — this is compiled out entirely in `stg`/`prod`, preventing it from ever leaking into a real deployment.

Plugins wired into `app/build.gradle.kts` include: Android application, Kotlin Compose compiler, **Hilt** + **KSP** (dependency injection and annotation processing), Paparazzi (screenshot testing), Firebase App Distribution, Google Services, Crashlytics, and Performance Monitoring — reflecting a mature, telemetry-conscious CI/CD posture. `multiDexKeepProguard` is set from `multidex-startup-rules.pro`, indicating the app has crossed the single-dex method limit and needs curated multidex keep rules for startup-critical classes (e.g., Hilt-generated `Application` subclasses).

---

## 3. App Entry, Bootstrap & Session Lifecycle

### 3.1 `GoatOsApplication` — DI root and cold-start orchestration

`GoatOsApplication` (`@HiltAndroidApp`) is deliberately kept thin per the project's TRD, but it owns several critical startup responsibilities:

- **`Configuration.Provider`** — supplies a Hilt-aware `HiltWorkerFactory` to WorkManager. The manifest explicitly removes WorkManager's default `androidx.startup` initializer (`tools:node="remove"` on `WorkManagerInitializer`) so this on-demand configuration is guaranteed to win; otherwise WorkManager could try to construct a `@HiltWorker` before Hilt is wired.
- **`ImageLoaderFactory`** — builds Coil's app-wide `ImageLoader` over the *same* authenticated OkHttp client used for proof-video playback (`proofMediaClient`, from `MediaModule`). This was a deliberate bug fix: building a bare OkHttp client for images caused proof photos (which require a bearer token) to 401 while adjacent proof videos played fine over the authenticated client.
- **Cold-start telemetry** — starts a custom performance trace (`APP_COLD_START`) as early as possible in `onCreate()`; it is stopped later in `MainActivity.onResume()`, exactly once, using a nullable `var` handle shared between `Application` and `Activity` without a second DI lookup.
- **Synchronous session/launch analytics** — `APP_OPEN` and `SESSION_START` fire synchronously before any suspending work, specifically so a process killed moments after launch never silently drops these events, and so a background-coroutine-driven event can never race ahead of them.
- **Notification channel creation** (`pushNotifications.ensureChannels()`) — must happen before the first notification is posted, since Android silently drops a `notify()` call on a channel that was never created.
- **Deferred, off-critical-path work** — `connectivitySyncTrigger.start()`, `syncWorkScheduler.schedule()` (WorkManager periodic outbox drain), and a one-time execution-cache version purge (`ExecutionCacheVersionGate.purgeIfVersionChanged`) are all launched on the app-scoped `CoroutineScope` rather than blocking first frame.

### 3.2 `MainActivity` — single entry point and layered gating

`MainActivity` (`ComponentActivity`, `@AndroidEntryPoint`) is the app's only Activity (`launchMode="singleTop"`, locked to `portrait`). Its Compose content tree is structured as a strict sequence of **gates**, each of which must pass before the next is evaluated:

1. **Force-update gate** (`UpdateGateViewModel` / `UpdateGateUiState`) — sits *above* authentication. An out-of-date build is blocked whether or not anyone is signed in, and the gate fails open (renders the app normally) if the environment (e.g. `dev`) has no Remote Config wired.
2. **Auth gate** (`SessionViewModel.isAuthed: StateFlow<Boolean?>`) — a **three-state** flag (`null` = not yet read, `true`/`false`) specifically to avoid flashing a login screen at an already-signed-in operator during the brief window before the persisted session is read.
3. **Bootstrap gate** (`BootstrapViewModel.state: StateFlow<BootstrapUiState>`) — loads the backend-composed navigation state once authentication is confirmed.
4. **Navigation shell** (`GoatOsShell`) — rendered only once bootstrap reaches `Ready`.

This layered gating is a deliberate architectural choice recorded directly in code comments: for example, the auth-transition logic explicitly resets and reloads `BootstrapViewModel` on every `authed` transition to `true`, because `BootstrapViewModel` is Activity-scoped (not session-scoped) and would otherwise survive a logout with the *departing* user's navigation state still cached — a defect class the team labels "C35-001" and treats as a first-class regression risk across the whole session lifecycle.

### 3.3 Bootstrap state machine

`BootstrapUiState` is a sealed interface with three states:

```kotlin
sealed interface BootstrapUiState {
    data object Loading : BootstrapUiState
    data class Ready(val navState: NavState) : BootstrapUiState
    data class Error(val errorType: BootstrapErrorType) : BootstrapUiState
}
```

`BootstrapViewModel.load()` calls `BootstrapRepository.loadNavState()`, which is implemented offline-first by `DefaultBootstrapRepository`:

- On success: persists the fresh bootstrap DTO to a Room-backed cache (`BootstrapCache`), reconciles the device registration/heartbeat (best-effort, never blocking nav load), and converts the DTO to `NavState`.
- On failure: maps the throwable through `asBootstrapError()` into one of three domain errors:
  - `AuthSessionExpired` (401) — **never** falls back to cache; a stale shell must never mask an expired/rejected token. Propagated up as `BootstrapErrorType.AUTH_SESSION_EXPIRED`, and the UI's only action is "Sign in again" (`sessionViewModel.signOut()`).
  - `AccessNotProvisioned` (403, valid sign-in but no roster grant) — never falls back to cache either, but the recovery action is **Retry only** — signing out here would wipe unsynced offline work for no benefit, since the account itself is not the problem.
  - `ConnectivityFailure` — the only branch permitted to fall back to the cached bootstrap; if no cache exists, the error propagates and the UI shows a retryable "Retry" action.

Analytics identity (`applyAnalyticsIdentity()`) is deliberately set **before** the `BOOTSTRAP_LOADED` event fires, guaranteeing `setUserId()`/user properties are attached from the very first event of the session — and it is set even on bootstrap failure, so failure events still carry user context.

A companion `NavStateRefreshSignal` (a coalescing, app-scoped `MutableSharedFlow`) lets any screen-level ViewModel — which cannot reach the Activity-scoped `BootstrapViewModel` directly — request a **quiet** re-read of the backend-composed nav state (e.g., to refresh a Leadership Tasks badge count) without transitioning through `Loading`, which would unmount the `NavHost` and destroy the back stack.

### 3.4 Session management and logout

`SessionViewModel` implements a dual authentication strategy selected at compile time by flavor (`authModeForFlavor(BuildConfig.FLAVOR)`):

- **`AuthMode.DEV_BEARER`** — the `dev` flavor's local backend validates a baked HS256 token rather than Firebase ID tokens. On a reinstalled dev APK, `devSessionNeedsRefresh`/`devSessionNeedsWipe` detect whether the newly baked token belongs to a *different* principal (`tenant_id:sub` pair decoded from the JWT payload) and, if so, run the same clean-slate wipe as an explicit logout before adopting the new token — preventing a role/tenant leak across dev-build reinstalls.
- **`AuthMode.FIREBASE`** — `stg`/`prod` use `FirebaseAuthRepository`, which wraps email/password sign-in, Google Sign-In via Credential Manager (`GetGoogleIdOption` → `GoogleIdTokenCredential` → `FirebaseAuthProvider`), and password reset. Only a non-sensitive `FIREBASE_SESSION_MARKER` sentinel is persisted in DataStore — never the live short-lived ID token — because the network layer re-derives a fresh ID token per request via `currentFirebaseIdTokenBlocking()`.

**Logout** is centralized in `LogoutCoordinator` (`core-data`), invoked identically from both `SessionViewModel` and `ProfileViewModel` to prevent divergent partial wipes. Its sequencing is deliberate and defends against several distinct classes of data leakage:

1. `deregisterDeviceBestEffort()` — must run **before** vendor sign-out, since the network's bearer interceptor needs the still-live session to authenticate the device-deregistration call itself.
2. `signOutVendorAuth()` (Firebase/dev-bearer sign-out, injected as a callback since `core-data` cannot depend on `:app`'s `AuthRepository`).
3. `clearPushAndAnalyticsIdentity()` — decouples FCM token binding and Firebase Analytics identity from the departing principal (delegated to `PushLogoutCleanup` in `:app`).
4. In-memory singleton state (e.g. `FeedCompletionLocalStore`) is cleared — a Room wipe alone cannot reach process-lifetime overlays.
5. Every screen cache table (`ScreenCacheStore.clearAll()`) and the write outbox (`OutboxWiper.clearAll()`) are wiped.
6. WorkManager periodic/retry jobs are cancelled (`SyncJobsCanceller.cancelAll()`) so nothing tries to drain an outbox that was just wiped.
7. `SessionStore`/`DeviceStore` are cleared **last** — token, language, `app_install_id`, `device_id` — so the next principal starts from a fresh install identity.

Beyond the disk wipe, **`SessionRelauncher.relaunchToLogin()`** provides the root-cause guarantee for a truly clean slate: it starts a brand-new launcher task (`FLAG_ACTIVITY_NEW_TASK | FLAG_ACTIVITY_CLEAR_TASK`) and then calls `Runtime.getRuntime().exit(0)` to kill the current process outright. This is intentional overkill against a subtler bug class: disk-only cleanup left every `@Singleton` repository's in-memory `StateFlow` cache, retained Activity-scoped ViewModels, Coil's image cache, and process-static holders (e.g. `AppLocaleState`) still serving the departing principal's data from RAM after a Room wipe. Killing the process is the only mechanism that cannot silently miss a future in-memory cache the way manually resetting dozens of singletons would.

### 3.5 Force-update gate

`RemoteConfigUpdateGate` (`UpdateModule`) wraps Firebase Remote Config, reading two server-owned keys: `min_supported_version_code` (a hard floor) and `update_url` (the App Distribution install link, since the app is not Play Store-distributed). It is designed to **fail open**: any SDK/network exception resolves to `UpdateDecision.Allowed`, and Remote Config's own `fetchAndActivate` persistence means a relaunch with no network still reads the last-activated minimum, so a build already below the floor correctly stays blocked even offline. The `dev` flavor skips Remote Config entirely (`skipRemoteConfig = BuildConfig.DEBUG`-derived), and a debug-only local override file lets testers exercise the force-update UI deterministically.

`MainActivity` tracks the in-progress update attempt through `ForceUpdateAttemptState` (`Idle → Downloading → PermissionNeeded/InstallerOpened/Failed`), driving `ForceUpdateScreen`. A backend-triggered silent push (`type = "force_update_recheck"`) can also re-trigger the check mid-session via `ForceUpdateRecheckSignal`, which persists a "pending" flag in `SharedPreferences` so the recheck survives even if no Activity collector was alive when the push arrived.

---

## 4. RFID & Device Integration

The RFID subsystem is the operational entry point for animal identification and is deliberately architected as a **Bluetooth HID keyboard-wedge** integration rather than a vendor SDK: Android itself owns the Bluetooth HID connection lifecycle, and the app only *observes* readiness and *intercepts* the resulting synthetic keyboard events.

### 4.1 Port abstraction

```kotlin
interface RfidReaderPort {
    val status: StateFlow<RfidReaderStatus>
    val reads: SharedFlow<RfidRead>
    val readerName: StateFlow<String?>
    val devices: StateFlow<List<RfidReaderDevice>>
    fun refreshStatus()
    fun openSystemPairing()
    fun setCaptureEnabled(enabled: Boolean)
    fun setCompletionKeySwallowEnabled(enabled: Boolean)
    fun onKeyEvent(event: KeyEvent): Boolean
}
```

`RfidReaderStatus` is a five-state readiness enum (`READY`, `PAIRED_NOT_READY`, `NOT_PAIRED`, `PERMISSION_NEEDED`, `BLUETOOTH_OFF`) that drives a color-coded UI banner. Feature/UI modules never import `Bluetooth`/`InputManager` directly — they consume only this port, which keeps the door open for a future vendor BLE/SDK adapter without touching feature code.

### 4.2 Production implementation: `KeyboardWedgeRfidReader`

This class fuses two Android signal sources into one readiness computation:

- **`InputManager.InputDeviceListener`** — detects when an external keyboard-class input device (the reader itself) is added/removed/changed.
- **A `BroadcastReceiver`** for `ACTION_ACL_CONNECTED`/`ACTION_ACL_DISCONNECTED`/`ACTION_BOND_STATE_CHANGED` — tracks Bluetooth connection/bond state for devices whose name matches a configured hint list (`RfidReaderNameMatcher.DEFAULT_HINTS`).

`computeStatus()` prioritizes an actually-active input device (`signalLabel == "Ready"`) over a merely-bonded-but-inactive Bluetooth device, and correctly distinguishes a missing `BLUETOOTH_CONNECT` runtime permission (Android 12+) from a disabled adapter or a genuinely unpaired reader. All Bluetooth API access is wrapped behind an explicit `needsBluetoothConnectPermission()` guard with `@SuppressLint("MissingPermission")`, since lint cannot statically verify the custom predicate.

A noteworthy manifest-level implication of this design: because the reader is registered as a Bluetooth HID **keyboard**, every connect/disconnect changes `Configuration.keyboard`/`hardKeyboardHidden`/`navigation`. Without declaring `android:configChanges="keyboard|keyboardHidden|navigation"` on `MainActivity`, the platform would destroy and relaunch the Activity on every reader edge — a real, reproduced defect (documented with an on-device log trace) that tore down the Activity-scoped `BootstrapViewModel` and threw the operator out of the scan screen mid-drive on every reader reconnect. The manifest configuration and in-process handling (the reader observes connect/disconnect itself) together are the actual fix, not a workaround.

### 4.3 Key-event capture pipeline

`RfidKeyboardCapture` buffers raw `KeyEvent`s from the activity's key stream into a completed tag string, with several careful correctness properties:

- **`DROP_OLDEST` overflow policy** on its internal `MutableSharedFlow` (capacity 64) rather than the default `SUSPEND` — `onKeyEvent()` runs on the main/input thread and cannot suspend, so under a scanner burst that outruns a slow collector, the buffer must drop the *stalest* queued tag, never the one just scanned (the newest read is always what the operator is currently acting on).
- **Route gating** — `enabled` and `swallowCompletionKeys` are `@Volatile` flags toggled per-screen so hardware Enter/Tab from the reader never leaks into unrelated focused UI controls, while non-tag keys (Back, volume) always pass through untouched.
- **Completion detection** — `KEYCODE_ENTER`, `KEYCODE_NUMPAD_ENTER`, and `KEYCODE_TAB` all terminate a read; the accumulated buffer is trimmed and emitted as `RfidRead(tag, deviceName, capturedAtDeviceMs)`.
- **No `EditText` anywhere** in the capture path — reads come directly from the Activity's key-event stream, by design (documented in `docs/mobile/rfid-keyboard-reader.md`), avoiding IME/autocorrect/clipboard side effects a text field would introduce.

### 4.4 Scan source abstraction and input transform

`ScanSource` is a second-tier port specifically for the Submit recording form's `goat_scan` capture field, wrapping `RfidReaderPort` rather than re-implementing buffering logic:

```kotlin
interface ScanSource {
    val tags: Flow<String>
    fun start()
    fun stop()
}
```

`BtHidScanSource` is the production adapter (maps `RfidReaderPort.reads` to bare tag strings); `FakeScanSource` is a test double exposing `start`/`stop` call counters for assertion-based tests.

`RfidInputTransform` provides a seam for adjusting raw tag strings before feature-specific matching/sync — `PassthroughRfidInputTransform` is the identity default, with room for a `DefaultRfidInputTransform` dev-flavor variant (referenced in DI wiring) that applies the per-shed scan-scope prefixing described in §2.2 for QA convenience.

---

## 5. Local Data & Offline-First Sync Layer

This is the domain's most architecturally significant subsystem — a deliberate mirror, at the mobile edge, of the backend's own transactional outbox pattern (see the parent System Architecture report, §5 and §12.4), giving the platform end-to-end reliability across the connectivity boundary.

### 5.1 Two separate Room databases

The app runs **two independent Room databases**, split by design for isolation and independent migratability:

- **`GoatDatabase`** (`core-data`) — the large bootstrap/screen-cache database, holding dozens of feature-specific cache tables (`BootstrapCache`, `CalendarCacheEntity`, `ControlTowerCacheEntity`, `WeighingAlertsCacheEntity`, `FeedDirectionItemEntity`, `PcCareTaskItemEntity`, `CountsBreakdownItemEntity`, and many more) plus proof-capture entities (`ProofCaptureEntity`, `ScannedGoatEntity`, `RfidScanAttemptEntity`). It has undergone dozens of migrations (schema versions up to v63 per the workflow research), reflecting the constant addition of new feature-level read caches.
- **`OutboxDatabase`** (`core-database`) — a small, high-write-frequency schema holding only `OutboxEntity`, intentionally kept separate so the write-durability path can be independently tested/migrated without any risk to the much larger bootstrap-cache schema.

The separation reflects a clear architectural principle stated directly in the outbox database's KDoc: *the outbox holds not-yet-synced writes, so a silently-wrong migration here loses operator submissions* — every migration (`OUTBOX_MIGRATION_1_2` through `_4_5`) is therefore non-destructive, validated against golden schema JSON via `MigrationTestHelper`, and reviewed as a first-class schema change (`exportSchema = true`).

### 5.2 The Outbox pattern

`OutboxStore` is the persistence port `SyncEngine`/`SyncRepository` talk to — never Room directly — which keeps the drain/business logic unit-testable on the plain JVM with an in-memory fake, sidestepping the need for Robolectric or an instrumented emulator test path. `RoomOutboxStore` is the real implementation over `OutboxDao`.

Key correctness guarantees baked into the `OutboxEntity` lifecycle:

- **Atomic, status-guarded transitions** — `markInFlight`, `markSucceeded`, `markFailed`, `markRetryReady`, `reopenTerminalForRetry` all return whether the transition was actually applied (`true`) or was a no-op because the row had already moved on (`false`), so a manual retry and a concurrent drain pass can never clobber each other.
- **Idempotency keys + request fingerprints** — every write carries a stable idempotency key end-to-end, and (since migration v2→v3) a request fingerprint so a same-key-but-different-payload retry is correctly rejected as a conflict rather than mistaken for an exact idempotent replay.
- **Ordering groups** — rows are grouped by `groupKey` (e.g. a shed id) and drain strictly in `createdAt` order within a group, while different groups may drain concurrently (bounded by a permit-based semaphore, `maxConcurrentGroups = 3` by default) — never unbounded coroutine fan-out.
- **Crash/process-death recovery** — every drain pass first calls `reclaimInFlight()`, resetting rows stranded `IN_FLIGHT` by a prior crash back to `QUEUED`, guaranteeing an interrupted submit is always retried on the next pass.
- **Bounded observation** — `observeActiveCounts()` and `observeActiveWindow(limit)` exist specifically so UI badges/lists never materialize the entire outbox table into memory; `observeActive()` (full materialization) is explicitly marked deprecated for UI use.
- **Retention** — `pruneSucceeded(retentionMs, now)` removes only terminal `SUCCEEDED` rows past a retention window; in-flight work is never pruned.

### 5.3 `SyncEngine` — the framework-free drain body

`SyncEngine.drainOnce()` is the pure "do the work" body a `CoroutineWorker.doWork()` delegates to — deliberately framework-agnostic so it can be triggered from **three independent callers**, all sharing the same Room-backed outbox as the single source of truth:

1. **`SyncRepository`**, on every write enqueue — an optimistic immediate drain attempt.
2. **`ConnectivitySyncTrigger`**, on network reconnect — drains promptly *while the process is alive*.
3. **`SyncWorker`** (`@HiltWorker`, `CoroutineWorker`) — a periodic, `CONNECTED`-constrained WorkManager job that is the OS-scheduled backstop surviving **process death**. `doWork()` is intentionally a ~10-line shim with zero drain logic of its own.

`SyncEngine` composes per-feature DAOs (weighing observation, health diagnosis run, feed repository, feed transport repository, etc.) as **optional** post-success reconciliation targets — after an outbox row reaches `SUCCEEDED`, the engine writes the server's confirmed result directly into the relevant Room cache row so the UI reflects sync state ("Submitted — video in review") durably, offline-capable, without waiting for the next network fetch. Two hook interfaces formalize this:

```kotlin
fun interface PostSuccessRefreshHook { suspend fun onSuccess(payloadJson: String) }
fun interface PostTerminalFailureHook { suspend fun onTerminalFailure(payloadJson: String) }
```

`PostSuccessRefreshHook` covers page-blob caches (e.g. Milk Feeding's whole-page cache) that have no server-truth row to patch directly — the correct reconcile there is simply "refetch the page." `NonRetryableSyncException` distinguishes a *definitive* server rejection (e.g. failed validation) from a transient failure: `SyncEngine` terminalizes such a row immediately (`conflict = true`) rather than burning the backoff budget retrying a rejection that can never succeed unchanged.

### 5.4 Connectivity gating

`ConnectivityGate` fails **open** by design — if the platform's `ConnectivityManager` service cannot be read, `isOnline()` returns `true`, because a spuriously-blocked drain is worse than an attempted drain that transiently fails and backs off. A special `LocalBackendConnectivityGate` decorator additionally treats any `localhost`/`127.0.0.1`/`::1` API base as always-online — necessary because Android can mark a network as "no validated internet" even when a laptop-tunneled loopback backend (via `adb reverse`) is perfectly reachable. The `isLoopbackHttpBase()` extension is explicitly called out as the *single source of truth* shared between the drain-time gate and WorkManager's own enqueue-time `Constraints`, since disagreement between the two would silently veto work the runtime gate would otherwise allow.

### 5.5 Foreground upload service

`UploadForegroundService` provides Drive/Photos-style visible progress for upload-relevant outbox rows (proof-video registrations, shed submissions) — but is architected as a thin Android shim around `UploadSyncCoordinator` (framework-free, unit-tested in `core-data`), exactly mirroring `SyncWorker`'s own "no drain logic lives here" discipline. Because Android 14+ forbids starting a `dataSync`-typed foreground service from a `BOOT_COMPLETED` context, the service's `ForegroundServiceStartNotAllowedException` handler falls back to `SyncWorkScheduler.syncNow()` — a one-shot, connectivity-constrained WorkManager job — so a reboot with queued writes still drains promptly rather than waiting up to the periodic worker's next scheduled tick.

### 5.6 Execution cache versioning

`ExecutionCacheVersionGate` + `SharedPrefsCacheVersionStore` implement a narrowly-scoped cache-invalidation mechanism tied to `BuildConfig.VERSION_CODE`: an in-place app update preserves app data, so a cached execution row/shed blob written before a serving-shape change (e.g. a vaccination drive date move) could otherwise survive as a stale "ghost." The gate purges **only** read blobs on first run after a version bump — never the outbox — so unsynced operator writes are always preserved regardless of app-update timing.

---

## 6. Push Notifications (Firebase Cloud Messaging)

### 6.1 Reception and display

`GoatOsMessagingService` (`FirebaseMessagingService`, `@AndroidEntryPoint`) implements the two standard FCM callbacks, both defensively wrapped in `runCatching` so a build flavor lacking a confirmed Firebase project (e.g. `prod` at time of writing) degrades push to a silent no-op rather than crashing a process the OS just woke up:

- **`onNewToken(token)`** — forwards the new/rotated token to `NotificationsPort.registerToken()`.
- **`onMessageReceived(message)`** — builds and posts the notification via `PushNotifications.show()` for **both** data-only and notification+data payloads. This is a deliberate necessity, not redundancy: FCM auto-displays the `notification` block only while the app is backgrounded/killed, but while the app is in the **foreground**, `onMessageReceived` is the *only* code path that can put anything on screen.

A special silent-push type (`PushExtras.FORCE_UPDATE_RECHECK_TYPE`) short-circuits notification display entirely and instead emits `ForceUpdateRecheckSignal`, triggering an immediate force-update re-check without any visible notification.

### 6.2 Channels and notification construction

`PushNotifications` manages exactly two notification channels: `push_channel_vaccination_id` (`IMPORTANCE_HIGH`, time-sensitive obligation/escalation/reschedule alerts) and `push_channel_general_id` (`IMPORTANCE_DEFAULT`, everything else). The full data payload rides along on the tap `PendingIntent`'s intent extras (`PushExtras.ROUTE_KEYS`) rather than being pre-resolved into a route, because the same resolution logic must work identically whether *this class* built the launch intent (foreground tap) or the *system* built it (background/killed-app auto-display tap) — only reading the raw payload works for both paths.

Notification IDs are derived preferentially from a stable business key (`item_id`, `obligation_id`, or `shed_id` hash) rather than a random value, so repeat pushes about the *same* obligation update the same notification slot instead of stacking duplicates.

### 6.3 Route resolution

`resolvePushRoute()` maps an FCM data payload to an in-app route through a strict, most-specific-first precedence, explicitly documented as a contract because an earlier version got the ordering backwards and produced real routing defects:

1. An explicit `target`/`href` the backend precomputed for *this specific recipient* (e.g. a verifier's `/verification/items/{id}` review link) — checked first.
2. A named `screen`/`type` value, module-scoped (vaccination, weighing, feed, leadership tasks, pen visits, leave, counts, calendar, verification) — each module owns its own screen name; there is no shared default, and a module with no named screen sends the recipient to their *own* home screen rather than guessing another module's.
3. Otherwise, `null` — the caller falls through to the recipient's own landing route.

Notably, **recipient role is deliberately never consulted** in this resolution — who someone is comes only from their own authenticated sign-in session, never from a field on a message a sender could populate incorrectly.

### 6.4 Pending navigation bridge

Because a notification tap can arrive **before** the auth/bootstrap gates have resolved (cold start goes `MainActivity → login → bootstrap → GoatOsShell`, and `GoatOsShell` is the only place a real `NavHostController` exists), `PendingNavigation` is a process-scoped singleton bridging that timing gap:

```kotlin
class PendingNavigation @Inject constructor() {
    private val pendingRoute = AtomicReference<String?>(null)
    private val _route = MutableStateFlow<String?>(null)
    fun set(route: String) { ... }
    fun consume(): String? { ... }
}
```

The `consume()` design specifically closes a TOCTOU race: a naive "read `.value`, then set `.value = null`" on a bare `StateFlow` has a window where two concurrent callers could both observe the same non-null route before either's clearing write lands. `AtomicReference.getAndSet(null)` is a single atomic read-and-clear, guaranteeing **at most one** caller of `consume()` ever receives a given route — the `StateFlow` mirror exists purely so Compose's `collectAsStateWithLifecycle()` still gets reactive emission across a lifecycle STOP/START (e.g. configuration change). `PushNavigationViewModel` (Activity-scoped) is the thin adapter `GoatOsShell` observes via `hiltViewModel()`.

### 6.5 Token sync and heartbeat coupling

`PushTokenSync` (`AndroidPushTokenSync`) is called once after **every** successful bootstrap (`BootstrapViewModel.applyAnalyticsIdentity`), deliberately covering the case `onNewToken` alone would miss: `onNewToken` fires only once per token mint/rotation, not on every app open, so a cold start with an already-valid session (and an already-minted token) still needs its binding re-confirmed. `DevicePushStateViewModel` reuses the same seam to re-report push state on demand (e.g. when an operator flips notifications back on mid-session from the alerts gate or system settings) — deliberately avoiding a second, divergent reporting path.

`forceUpdateTopicEnabled(flavor)` gates FCM topic subscription (`goatos_force_update_prod`) to the `prod` flavor only, since the silent force-update-recheck push is a production-specific mechanism.

---

## 7. Telemetry, Analytics & Crash Reporting

### 7.1 Layered analytics fan-out

`AnalyticsModule` composes the `AnalyticsPort` seam as a stack of decorators selected by build configuration:

- **`FanOutAnalytics(FirebaseAnalyticsAdapter, BackendAnalyticsAdapter)`** — when `BuildConfig.TELEMETRY_ENABLED`, every event is sent to *both* Firebase/GA4 and a backend-mirrored analytics endpoint.
- **`NoopAnalytics()`** — otherwise (flavors without a confirmed Firebase project, and always in tests).
- **`LogcatAnalyticsAdapter`** wraps whichever sink above in debug builds — mirroring every event to logcat under tag `GoatOSAnalytics` as a durable proof path for real-device E2E testing when Firebase delivery lags or is unavailable.

The dual-channel design (Firebase *and* backend mirror) reflects an explicit trust decision: Firebase/GA4 is useful but "not trustworthy enough as the only receipt surface" when the console can lag or be misconfigured — the backend mirror is the forensic source of truth for debugging offline/failure incidents.

### 7.2 `BackendAnalyticsAdapter` and durable critical-event queue

`BackendAnalyticsAdapter.track()` is **best-effort/fire-and-forget** for most events — a network failure simply logs a `Log.w` breadcrumb and drops the event. However, a hand-picked `CRITICAL_EVENT_ALLOWLIST` (proof-capture/processing/upload failures, sync-write lifecycle events, PC Care stock-proof events) is routed through `DurableAnalyticsQueue` first — a minimal, capped (500 entries, drop-oldest), file-backed JSON queue under `Context.filesDir`, written with a temp-file-then-rename swap so a process death mid-write cannot leave a truncated queue. Every request (fresh or replayed) carries a `client_event_id` (a UUID) as both an analytics property and a first-class DTO field; the backend enforces `UNIQUE(tenant_id, client_event_id)` with `ON CONFLICT DO NOTHING`, so a queue-drain resend using the *same* original id can never double-count on the server even if a local "did the removal actually persist" race occurs.

Deliberately, this queue is **not** full parity with the Room-backed write outbox: no WorkManager-scheduled background drain, no exponential backoff — it drains only opportunistically, on the next `track()` call after any event (since any live network attempt is itself evidence connectivity may have returned). This scope limitation is explicitly documented as intentional, to avoid wiring a second background trigger mechanism for what is a narrow forensic-debugging need.

### 7.3 Crash reporting and performance tracing

`TelemetryModule` wires `CrashReporter` and `PerformanceTracer`, each gated independently on `BuildConfig.TELEMETRY_ENABLED` with zero-dependency `Noop*` fallbacks for flavors without a confirmed Firebase project. Network telemetry (`NetworkTelemetryReporter`) is unusual in that its **failure-reporting wrapper is always installed**, regardless of `TELEMETRY_ENABLED` — even a flavor with no real Firebase project still gets logcat visibility into every refused/failed API call, since silent failure during a retry storm is precisely the defect this wrapper exists to eliminate.

`MediaModule` extends the same telemetry discipline to proof-video playback: rather than hanging a separate `AnalyticsListener` off each ExoPlayer instance, media3 is routed through the *same* OkHttp client and the *same* `FailureReportingNetworkTelemetryReporter` every other API call uses — one policy, one implementation, applied uniformly.

---

## 8. Feature Workflow Support Utilities

Beyond the shared infrastructure above, several app-level utilities support specific feature capture flows:

- **`AttachmentImporter`** (`leadershiptasks/`) — copies a content-picker `Uri` result into app-private storage off the main thread, refusing files over a caller-specified size cap (checked both from declared `ContentResolver` metadata *and* incrementally during the copy, in case the declared size lied), and best-effort extracts media duration for audio/video attachments via `MediaMetadataRetriever`.
- **`VoiceNoteRecorder`** (`leadershiptasks/`) — wraps `MediaRecorder` for AAC/MPEG-4 (`.m4a`) voice notes, one recording at a time, writing into the same private draft tree the attachment importer uses. A recording that produced zero bytes is discarded rather than surfaced as a note.
- **`WeighingExportFileWriter`** — a port (mirroring the `RfidReaderPort` pattern) so ViewModels requesting a CSV export stay `Context`/`FileProvider`-free and unit-testable; writes into `cacheDir/exports` (disposable, re-downloadable on demand) and prunes stale prior exports before each write.
- **`DurableAnalyticsQueue`**, **`SharedPrefsCacheVersionStore`** — covered above.

These utilities consistently follow the port/adapter pattern established by `RfidReaderPort` and `ScanSource`: an interface consumed by ViewModels, with a production Android implementation and (where relevant) a fake for tests — extending the same architectural discipline visible in the RFID and sync subsystems throughout the rest of the app.

---

## 9. Cross-Cutting Architectural Patterns Observed

Several patterns recur consistently across this domain's implementation and are worth calling out as house conventions:

1. **Port/adapter isolation for every hardware or OS dependency.** `RfidReaderPort`, `ScanSource`, `WeighingExportFileWriter`, `AttachmentImporter`, `VoiceNoteRecorder`, `SessionRelauncher`, `ConnectivityGate`, `ConnectivitySource` — every Android-framework touchpoint is abstracted behind a small interface with a real implementation and typically a fake, keeping ViewModels and business logic unit-testable on the plain JVM without Robolectric or an emulator.
2. **Fail-open vs. fail-closed decisions are explicit and justified per-case.** `ConnectivityGate` fails open (an attempted drain that turns out offline just backs off safely); `RemoteConfigUpdateGate` fails open (never block on a Remote Config outage); but `DevSessionViewModel`'s catch-all after a dev-session wipe failure explicitly fails **closed** (never open the gate on a possibly-still-live previous principal's token) — each choice is tied to which failure mode is less harmful.
3. **Atomic, status-guarded state transitions wherever concurrent mutation is possible.** The outbox's `markInFlight`/`markSucceeded`/`markFailed` methods, and `PendingNavigation.consume()`'s `AtomicReference.getAndSet`, both return/behave based on whether the caller actually "won" the transition — preventing silent double-processing under concurrency.
4. **Root-cause fixes over narrow patches for identity-leak classes.** The C35-001 logout defect (a departing principal's data surviving in-memory across a device handoff) was fixed with an unconditional process kill + relaunch rather than an ever-growing list of manually-cleared singletons — explicitly reasoned as the only approach that cannot silently miss a *future* cache.
5. **Best-effort side effects never gate the primary flow.** Device heartbeat/registration during bootstrap, push-token sync, and analytics identity are all wrapped in `runCatching` and documented as "must never fail the load/logout/track it's attached to" — the operational core (nav load, offline capture, logout) is never allowed to depend on a non-critical side call succeeding.
6. **Build-flavor-gated behavior is compiled out, not runtime-guarded, wherever possible.** `SCAN_SCOPE_PREFIX`, `forceUpdateTopicEnabled`, and `skipRemoteConfig` are all `BuildConfig` fields decided at compile time, ensuring test-only or environment-specific behavior structurally cannot leak into a production build regardless of runtime state.