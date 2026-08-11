# Android Telemetry (Firebase Analytics / Performance / Crashlytics + network tracing)

> Status: **wired and gradle-compile-verified** (see "Verification" below — dev/stg/prod all
> compile; the reconstructed stg `google-services.json` passed the plugin's variant processing).
> Not yet verified: a real device/emulator run with `TELEMETRY_ENABLED=true` reaching a live
> Firebase project (only `stg`'s project is confirmed to exist). Companion to
> `docs/observability/OBSERVABILITY_DESIGN.md` §2.5/§2.6
> (infra/signal design) and `docs/observability/TELEMETRY_GUARDRAILS.md` (the repo-wide
> product-code rule + CI guard). **This doc supersedes
> `TELEMETRY_GUARDRAILS.md` §3's symbol list** per that doc's own note ("when
> `apps/goatos-android/docs/TELEMETRY.md` is added, it should supersede this list") — the guard
> doc and `tools/telemetry-guard/config.json` live outside `apps/goatos-android/` and are owned by
> a different lane; flag them for a follow-up sync if this doc and that one drift.

## 1. What shipped in this pass

All four Firebase deliverables are wired behind the pre-existing `AnalyticsPort` seam
(`core/core-analytics/.../AnalyticsPort.kt`), plus a new OkHttp telemetry interceptor. Nothing
here required editing `RecordViewModel.kt`, `ScanViewModel.kt`, or `ShedsViewModel.kt`
(the files owned by a parallel session).

| Deliverable | Real impl | Fallback (tests / `TELEMETRY_ENABLED=false`) |
|---|---|---|
| Analytics (GA4) | `FirebaseAnalyticsAdapter` | `NoopAnalytics` (pre-existing) |
| Funnels | `AnalyticsFunnels` (helper object, not a port — calls through whichever `AnalyticsPort` is bound) | n/a |
| Crash + non-fatal | `FirebaseCrashReporter` | `NoopCrashReporter` |
| Performance (custom trace) | `FirebasePerformanceTracer` | `NoopPerformanceTracer` |
| Network telemetry (traceparent + custom metric) | `TelemetryInterceptor` + `FirebasePerfNetworkTelemetryReporter` | `NoopNetworkTelemetryReporter` |

Automatic Firebase Performance traces (app-start-to-first-frame heuristic, screen render,
HttpURLConnection/OkHttp network calls) need **no code** — they come from applying the
`com.google.firebase.firebase-perf` Gradle plugin, which bytecode-instruments the assembled APK.
The custom pieces above exist for what automatic instrumentation can't express (a route
TEMPLATE instead of a raw URL; an app-cold-start span keyed to this app's own auth/bootstrap
lifecycle instead of a generic "first frame" heuristic).

## 2. Stable public API names (telemetry CI guardrail)

Exact symbol names + import paths a reviewer or `tools/telemetry-guard/telemetry-guard.py` can
grep for. Do not rename any of these without updating this doc (and, if the guard's
`config.json` marker list references the old name, that config too — it currently matches on
substrings like `"Crashlytics"` and `"AnalyticsFunnels"`, both still present below).

| Symbol | Import path | Kind |
|---|---|---|
| `AnalyticsPort` | `sg.mesha.goatos.core.analytics.AnalyticsPort` | seam interface (pre-existing) — `track(event, props)`, `setUserProperty(name, value)` |
| `FirebaseAnalyticsAdapter` | `sg.mesha.goatos.core.analytics.FirebaseAnalyticsAdapter` | real `AnalyticsPort` impl (GA4) |
| `NoopAnalytics` | `sg.mesha.goatos.core.analytics.NoopAnalytics` | fallback (pre-existing) |
| `AnalyticsEvents` | `sg.mesha.goatos.core.analytics.AnalyticsEvents` | event/param/user-prop name constants (pre-existing) |
| `AnalyticsFunnels` | `sg.mesha.goatos.core.analytics.AnalyticsFunnels` | funnel/journey helper — `Events`, `Params`, `trackDriveOpened`, `trackScanStarted`, `trackScanCompleted`, `trackVaccinationCaptureStarted`, `trackVaccinationCaptureCompleted`, `trackSubmitAttempted`, `trackSubmitSucceeded`, `trackSubmitFailed` |
| `CrashReporter` | `sg.mesha.goatos.core.analytics.CrashReporter` | crash/non-fatal seam — `recordException(throwable, message?)`, `log(message)`, `setCustomKey(key, value)` |
| `FirebaseCrashReporter` | `sg.mesha.goatos.core.analytics.FirebaseCrashReporter` | real `CrashReporter` impl (wraps `com.google.firebase.crashlytics.FirebaseCrashlytics`) |
| `NoopCrashReporter` | `sg.mesha.goatos.core.analytics.NoopCrashReporter` | fallback |
| `PerformanceTracer` / `TraceHandle` | `sg.mesha.goatos.core.analytics.PerformanceTracer` | custom-trace seam — `startTrace(name): TraceHandle`, `putMetric`, `putAttribute`, `stop` |
| `FirebasePerformanceTracer` | `sg.mesha.goatos.core.analytics.FirebasePerformanceTracer` | real impl (wraps `com.google.firebase.perf.FirebasePerformance`) |
| `PerformanceTraceNames.APP_COLD_START` | `sg.mesha.goatos.core.analytics.PerformanceTraceNames` | canonical trace name constant |
| `TelemetryInterceptor` | `sg.mesha.goatos.core.network.TelemetryInterceptor` | OkHttp `Interceptor` — traceparent + timing (`core-network`) |
| `NetworkTelemetryReporter` / `NetworkTelemetryEvent` | `sg.mesha.goatos.core.network.NetworkTelemetryReporter` | egress port for `TelemetryInterceptor` |
| `NoopNetworkTelemetryReporter` | `sg.mesha.goatos.core.network.NoopNetworkTelemetryReporter` | fallback |
| `FirebasePerfNetworkTelemetryReporter` | `sg.mesha.goatos.core.analytics.FirebasePerfNetworkTelemetryReporter` | real `NetworkTelemetryReporter` impl (custom Firebase Perf trace per call) |
| `TelemetryModule` | `sg.mesha.goatos.di.TelemetryModule` | Hilt module binding `CrashReporter`/`PerformanceTracer`/`NetworkTelemetryReporter` |
| `BuildConfig.TELEMETRY_ENABLED` | generated, per flavor | gates every real-vs-Noop binding above |
| `BuildConfig.OTLP_ENDPOINT` | generated, per flavor | reserved, currently `""` for every flavor (see §6) |

### Escape hatch

`// telemetry:exempt <reason>` — same marker string as
`docs/observability/TELEMETRY_GUARDRAILS.md` §4 (the repo-wide guard doc). Use it in a Kotlin
file when a screen/viewmodel genuinely has no user action to instrument (an internal debug-only
screen, a pure layout/presentational composable, or a screen whose only actions are already
covered by a sibling file's analytics calls per the guard's sibling-lookup rule). The reason text
is not validated for content, only presence — do not use it to silence review without a real
justification.

## 3. Funnel event constants (`AnalyticsFunnels`)

Journey: **login → bootstrap → drive-open → scan → vaccination-capture → submit**
(`OBSERVABILITY_DESIGN.md` §2.5).

| Stage | Status | Event constant(s) | Call site |
|---|---|---|---|
| login | **Wired** (pre-existing, unchanged) | `AnalyticsEvents.LOGIN_ATTEMPT` / `LOGIN_SUCCESS` / `LOGIN_FAILURE` | `app/src/main/kotlin/sg/mesha/goatos/boot/SessionViewModel.kt` |
| bootstrap | **Wired** (pre-existing, unchanged) | `AnalyticsEvents.BOOTSTRAP_LOADED` | `app/src/main/kotlin/sg/mesha/goatos/boot/BootstrapViewModel.kt` |
| drive-open | **Helper ready, call site NOT added** | `AnalyticsFunnels.Events.DRIVE_OPEN` via `AnalyticsFunnels.trackDriveOpened(analytics, driveId, parkId)` | TODO: the calendar/drive-list row-tap handler in `feature/feature-calendar` (not edited here — out of the safe-file list for this pass) |
| scan started/completed | **Helper ready, call site NOT added** | `AnalyticsFunnels.Events.SCAN_STARTED` / `SCAN_COMPLETED` via `trackScanStarted` / `trackScanCompleted` | TODO: `feature/feature-scan/.../viewmodel/ScanViewModel.kt` — **excluded from this pass** (owned by a parallel session) |
| vaccination-capture started/completed | **Helper ready, call site NOT added** | `AnalyticsFunnels.Events.VACCINATION_CAPTURE_STARTED` / `_COMPLETED` via `trackVaccinationCaptureStarted` / `trackVaccinationCaptureCompleted` | TODO: `feature/feature-record/.../viewmodel/RecordViewModel.kt` — **excluded from this pass** (owned by a parallel session) |
| submit attempted/succeeded/failed | **Helper ready, call site NOT added** | `AnalyticsFunnels.Events.SUBMIT_ATTEMPTED` / `_SUCCEEDED` / `_FAILED` via `trackSubmitAttempted` / `trackSubmitSucceeded` / `trackSubmitFailed` | TODO: the submit view model in `feature/feature-submit` (not edited here) |

**Why the last four stages have no call site yet**: this task's constraint list explicitly
scoped safe edit locations to `Application`, `MainActivity`, the login flow, and navigation —
and separately named viewmodels (`RecordViewModel`, `ScanViewModel`,
`ShedsViewModel`) as owned by a parallel session. `RecordViewModel`/`ScanViewModel` are exactly
where `vaccination-capture`/`scan` belong, and `feature-submit`'s view model was left untouched
for the same reason (staying out of concurrent feature-viewmodel edits). **Next step for
whoever owns those files**: add one `AnalyticsFunnels.trackXxx(analytics, ...)` call at the
start/end of the relevant action — each viewmodel already receives `AnalyticsPort` the same way
`BootstrapViewModel`/`SessionViewModel` do (constructor injection), so no new DI wiring is
needed, just the call.

## 4. Firebase project per flavor

| Flavor | Firebase project | Evidence | `TELEMETRY_ENABLED` |
|---|---|---|---|
| `stg` | `goatos-stg` | **Confirmed** — committed in `app/src/stg/google-services.json`, `app/src/stg/res/values/firebase.xml`, and `app/build.gradle.kts`'s `firebaseAppDistribution { appId = "1:514832198871:android:0cb898377ba4f7f7f19492" }` | `true` |
| `dev` | **not confirmed** | No Firebase config of any kind existed in this repo for `dev` before this change | `false` |
| `prod` | **not confirmed** | No Firebase config of any kind existed in this repo for `prod` before this change | `false` |

`app/src/stg/google-services.json` is the real staging Firebase Android client config for
`sg.mesha.goatos.stg`. `app/src/dev/google-services.json` is a schema-valid placeholder with
obviously-fake ids; prod remains unwired until the real prod Firebase project/app exists. Full
detail + setup steps:
`app/src/google-services-README.md`.

The `OBSERVABILITY_DESIGN.md` §2.5 convention ("Firebase project is per env flavor... each
flavor gets its own `google-services.json`") implies `goatos-dev` / `goatos-prod` would follow
the same naming as `goatos-stg`, but that is NOT verified in this pass — do not treat it as
confirmed. Confirm/create the project in the `vgoats.com` GCP organization before flipping
`TELEMETRY_ENABLED` for `dev` or `prod` (see `app/src/google-services-README.md` for the exact
steps).

## 5. India region (asia-south1)

- `stg`'s mobile API host is `https://stg-api.dashboard.mesha.sg/`, backed by
  `goatos-api-stg` in `asia-south1` (Mumbai). Do not point the stg release APK at
  `https://stg.dashboard.mesha.sg/` because that is the admin web/dashboard host. Do not use the
  raw Cloud Run URL for distribution builds once the API load-balancer host is live.
  Per `OBSERVABILITY_DESIGN.md` (header: "First target env: **stg** (`goatos-stg`,
  `asia-south1`)"), the OTel Collector Cloud Run service MUST also be provisioned in
  `asia-south1` — when it is deployed, `BuildConfig.OTLP_ENDPOINT` for `stg` should point at an
  `asia-south1` endpoint, not any other region.
- **GA4 → BigQuery export**: Firebase Analytics' native BigQuery export
  (`OBSERVABILITY_DESIGN.md` §2.6 — "GA4 (per Firebase project) → BigQuery daily export") must be
  linked, in the Firebase console / GCP BigQuery settings, to a dataset in the **`asia-south1`
  (Mumbai)** BigQuery location — not the default `US`/`EU` multi-region. This is a Firebase
  console / GCP configuration action for whoever owns the `goatos-stg` (and later `goatos-dev`/
  `goatos-prod`) Firebase project — it is NOT an Android code change and is out of scope for
  `apps/goatos-android/`, but is recorded here so the infra/backend lane doesn't default to a
  US/EU BQ dataset for GA4 export.

## 6. OTLP export — TODO, not wired

`TelemetryInterceptor` (`core/core-network/.../TelemetryInterceptor.kt`) always stamps a W3C
`traceparent` header (so the backend's OTel span can eventually link to the mobile call once the
collector exists), and reports method/route-template/status/duration to
`NetworkTelemetryReporter` — bound to `FirebasePerfNetworkTelemetryReporter` when
`TELEMETRY_ENABLED`. **Real OTLP export (`opentelemetry-android`) is NOT wired** — the OTel
Collector Cloud Run service described in `OBSERVABILITY_DESIGN.md` is not deployed yet (its own
rollout doc lists Android wiring as the LAST step, after the collector/Grafana/admin-web RUM).
Both `TelemetryInterceptor` and `FirebasePerfNetworkTelemetryReporter` carry a
`TODO(otel-otlp)` comment marking exactly where the OTLP exporter hooks in once the collector is
live and its `asia-south1` URL is known; `BuildConfig.OTLP_ENDPOINT` is reserved (currently `""`
for every flavor) for that URL.

## 6.1. API client identity headers

Every authenticated API request also carries client identity headers from
`BearerAuthInterceptor`, populated in `AppModule` from `BuildConfig`, Android `Build`, and
`DeviceStore.appInstallIdSync()`:

```
X-GoatOS-App-Version: <versionName, e.g. 0.1.17>
X-GoatOS-App-Version-Code: <versionCode, e.g. 18>
X-GoatOS-Build-Type: <flavor + Debug/Release, e.g. stgRelease>
X-GoatOS-Device-Id: <stable app install id>
X-Device-Id: <same stable app install id, legacy backend fallback>
X-GoatOS-Platform: android
X-GoatOS-OS-Version: Android <release>
X-GoatOS-SDK-Version: <SDK_INT>
X-GoatOS-Device-Model: <manufacturer model>
```

The backend request middleware records these into request logs and the audit recorder stores them
under `audit_log.metadata->'client'`. For a bad weighing/vaccination proof, audio/video capture, or
RFID scan, query the domain row's audit record and inspect that `client` block to identify the exact
APK version, build, install id, Android OS, SDK, and device model that submitted it.

## 7. Verification

Run with `JAVA_HOME=$(brew --prefix openjdk@21)` and `ANDROID_HOME=$HOME/Library/Android/sdk`
(resolved via `make android-doctor` / `tools/dev/android-doctor.sh`), network access required on
first resolution of the newly-added Firebase/google-services/perf/crashlytics coordinates
(`--offline` fails on a cold cache for those, even though every version here was picked to be
mutually compatible with AGP 9.2 — see the compatibility note on `firebasePerfPlugin` in
`gradle/libs.versions.toml`, and `core-analytics`/`app`'s `build.gradle.kts`):

```
./gradlew :core:core-network:compileDebugKotlin :core:core-analytics:compileDebugKotlin \
  :core:core-analytics:testDebugUnitTest \
  :app:compileStgDebugKotlin :app:compileDevDebugKotlin :app:compileProdDebugKotlin
```

**Result: BUILD SUCCESSFUL for every task above**, including `:app:compileStgDebugKotlin`'s
`processStgDebugGoogleServices` step (confirms the reconstructed `app/src/stg/google-services.json`
is well-formed and its `package_name` matches the `stg` flavor's applicationId), and
`:app:compileDevDebugKotlin` / `:app:compileProdDebugKotlin` (confirms the placeholder
`google-services.json` files for those flavors don't break their builds either). One real
compile error was caught and fixed during this pass: `:app` needed a direct `implementation(libs.okhttp)`
dependency (added to `app/build.gradle.kts`) because `core-network`'s own OkHttp dependency is
`implementation`-scoped and not exposed transitively, so `AppModule.kt` couldn't otherwise resolve
`okhttp3.Interceptor` when constructing `TelemetryInterceptor`. All warnings in the build output
are pre-existing (unrelated files this task never touched — `AppNavHost.kt`, `SyncWorker.kt`,
`Overlays.kt`).

**Not run** (out of scope / needs a device): `assembleStgRelease` / Firebase App Distribution
end-to-end, and any real on-device run with `TELEMETRY_ENABLED=true` hitting a live Firebase
project.

## 8. Companion rule: never swallow an exception

The `CrashReporter`/`recordException` seam documented in §2 exists to be used on *every* caught
exception, not just the ones a screen's own analytics wiring happens to hit. The maintainer's
golden rule — "never swallow any exception, always dump it into Firebase non-fatal errors
(mobile) or backend logs (server)" — is enforced separately from this doc's per-screen telemetry
rule, by a diff-scoped guard: `tools/exception-guard/` (CI target `make exception-guard`, chained
into `make ci-local`). It flags an empty `catch {}`, a catch body that only logs
(`Log.x`/`println`) without a `CrashReporter.recordException(...)` call, a catch body that only
`return`s/`emit`s without recording anything, and `runCatching { ... }.getOrNull()` /
`.getOrDefault(...)` chains that discard the failure instead of chaining `.onFailure { ... }`.
Escape hatch: `// exception:exempt <reason>` (distinct from this doc's own `// telemetry:exempt
<reason>` marker family in `docs/observability/TELEMETRY_GUARDRAILS.md` §4, so the two guards'
findings don't get confused). Full write-up, the Go-side equivalent, and the deliberately-NOT-
detected list: `docs/observability/TELEMETRY_GUARDRAILS.md` §8.
