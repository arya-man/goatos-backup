# TRD — Goat OS Mobile (Android · Kotlin + Jetpack Compose)

Status: draft for pre-implementation review. Pairs with [PRD](prd-operator-mobile.md),
[system-design](system-design.md), [design-system](design-system.md),
[screens](screens.md), [performance-and-memory](performance-and-memory.md),
[firebase-india-setup](firebase-india-setup.md),
[extensibility](extensibility-future-modules.md).

## 1. Technology decision

- **Native Android, Kotlin, Jetpack Compose** (Material 3, custom Goat OS theme).
- Rationale + rejected alternatives (RN, Flutter): [`docs/decisions/mobile-native-kotlin.md`](../decisions/mobile-native-kotlin.md).
  Short version: operators are Android-only on cheap phones; the app is
  BLE-RFID + camera heavy; the RFID SDK ships as a native `.aar`, so any
  cross-platform choice still requires Kotlin glue. Native wins on RAM, cold
  start, battery, and jank on the exact low-end devices that matter.

### Baseline versions (tested baseline — verify at scaffold against official sources)

Pin in the version catalog at first build; record actuals in the app README.
**Authority = official Android/Kotlin release notes + Maven metadata**, not any docs
mirror. These numbers drift fast — treat them as a **known-good baseline to verify,
not "the current latest"**; pin the exact current stable at scaffold from official
Maven metadata / release notes:

```text
Kotlin              2.4.0    (K2; Compose Compiler = Kotlin-bundled `org.jetbrains.kotlin.plugin.compose`, versioned with Kotlin — no separate compiler dep)
AGP / Gradle        AGP 9.2.x / Gradle 9.6.1, Gradle version catalog (libs.versions.toml)
Jetpack Compose     BOM 2026.06.01 (androidx.compose:compose-bom) + Material 3
compileSdk          36   (Android 16)
targetSdk           36
minSdk              31   (Android 12 — locked)
JDK                 17   (toolchain; 17+)
```

**minSdk = 31 is locked** (maintainer decision, 2026-07): Android 12+ only.
Procurement/support must ensure operator/leadership devices are Android 12+.
compileSdk/targetSdk = 36 is the latest stable (Android 16); bump only when a newer
stable ships and CI is green.

The **Compose BOM governs all `androidx.compose:*` versions** — never pin those
individually (override only via the BOM escape hatch when strictly needed).
Non-Compose libs are pinned explicitly (§2). Every dependency lives in the
**version catalog**; new deps require a README note with the reason, matching the
admin-web pinning discipline.

## 2. Libraries (all have a fake/mock for tests — non-negotiable)

Versions below are a **known-good baseline at authoring — verify each at scaffold
against official Maven metadata / release notes** (not a docs mirror); they are a
floor to confirm, not a claim of "current latest". Compose libs
are BOM-managed, so no per-lib Compose version.

| Concern | Choice | Version | Notes |
|---|---|---|---|
| UI | Compose + Material 3 | BOM 2026.06.01 | custom theme from design-system tokens; M3 `material3` + `material3-adaptive` |
| Compose↔lifecycle/activity | `lifecycle-*-compose`, `activity-compose` | lifecycle 2.10.0 · activity 1.13.0 | `collectAsStateWithLifecycle`, `viewModelScope` |
| Navigation | Navigation-Compose (type-safe routes) | 2.9.x | module registry drives destinations |
| DI | Hilt | 2.60.1 (+ hilt-navigation-compose 1.2.x) | constructor injection; no service locators |
| Async | Coroutines + Flow | kotlinx-coroutines 1.11.0 | structured concurrency; no `GlobalScope` |
| Stability | kotlinx-collections-immutable | 0.4.x | `ImmutableList`/`PersistentList` for skippable composables (§4a) |
| Local DB | **Room** (SQLite) | 2.8.x (+ KSP) | tasks/sheds/roster/scan/submission/outbox/config cache; expose `Flow` |
| Key-value | DataStore (Proto) | 1.1.x | session flags, language, device/reader state, bootstrap revision |
| Network | Retrofit + OkHttp + kotlinx.serialization | Retrofit 3.x · OkHttp 5.x · serialization-json 1.9.x | generated client (below); ETag/If-None-Match for config |
| API client | **OpenAPI-generated Kotlin client** | — | from backend app-api contract; never hand-written DTOs |
| Images/video | CameraX (video capture) + Coil 3 (thumbnails) | CameraX 1.6.1 · Coil 3.x | bounded bitmap sizes |
| RFID/BLE | Vendor `.aar` behind a `RfidReaderPort` | vendor-pinned | Chainway-class UHF; adapter isolates SDK |
| Haptics + sound | `Vibrator` + short tone (`ToneGenerator`/`SoundPool`) behind a `FeedbackPort` | platform | scan feedback; distinct not-due alert tone; fake in tests |
| Media upload / background | WorkManager | 2.11.2 | resumable signed-URL upload, sync, retry, dead-letter — **not** reminder timing (kernel-owned via `NotificationGateway`/FCM; the app only renders received pushes) |
| Firebase | Analytics, Performance, Crashlytics, Messaging (FCM), **Remote Config** | Firebase BOM 34.15.0 (verify at scaffold) | `asia-south1` where selectable; GA4/Crashlytics/Perf/FCM global — see firebase doc. Remote Config = kill-switch/flag fallback (bootstrap is primary — see backend-driven-config.md) |
| Logging | Timber + structured logger port | Timber 5.0.1 | `LoggerPort` → Timber(debug) + Crashlytics(release) breadcrumbs; see §7/§9 |
| i18n | Android resources per-locale + backend contract copy | — | en/hi/kn/te |
| Testing | JUnit5, Turbine, MockK, Compose UI test, Room in-memory, Robolectric, Maestro (E2E), Macrobenchmark 1.4.x + Baseline Profiles, LeakCanary | — | see §11 |

No vendor SDK may be called from feature/product code. SDKs live only inside
adapters behind ports (mirrors backend ports/adapters).

## 3. Module structure (Gradle multi-module)

`apps/goatos-android/` — Gradle root. Multi-module keeps build times low on
low-end CI and enforces boundaries (the mobile equivalent of admin-web's
`check-boundaries`).

```text
apps/goatos-android/
  app/                         # thin: Application, DI graph, nav host, theme, boot
  core/
    core-designsystem/         # theme, tokens, Compose components (design-system.md)
    core-ui/                   # shared stateless UI (rings, chips, sheets, list rows)
    core-model/                # pure Kotlin contract/presentation models (no Android/vendor deps, no business rules)
    core-common/               # Result types, dispatchers, time *formatting* (Asia/Kolkata display only), errors
    core-network/              # OkHttp/Retrofit setup, auth interceptor, error mapping
    core-data/                 # Room DB, DataStore, repositories base, sync/outbox engine
    core-datastore/            # Proto DataStore schemas
    core-analytics/            # AnalyticsPort + Firebase adapter + no-op fake
    core-notifications/        # push token registration, notification rendering
    core-testing/              # fakes, fixtures, test rules
  feature/
    feature-auth/              # login (email + OTP), session boot
    feature-calendar/          # week / month / history
    feature-sheds/             # calendar-card drill: operator execution list + leadership read-only drive-status follow-up (scope-filtered, red-on-delay)
    feature-scan/              # per-shed scan, RFID, groups, done/pending/skipped
    feature-submit/            # shed submit form (SOP/forms-runner render)
    feature-leadership/        # overview, coverage, backlog, data gaps, overdue, reschedule, assign
    feature-record/            # shed / drive record (read-only)
    feature-profile/           # you/settings, RFID reader pairing, alerts
  device/
    device-rfid/               # RfidReaderPort + Chainway adapter + fake reader
    device-camera/             # CameraCapturePort + CameraX adapter + fake
    device-feedback/           # FeedbackPort (haptics + alert tones) + fake
  :buildSrc / gradle/libs.versions.toml
```

Dependency rule (enforced in CI): `feature-* → core-*`, `feature-*` never imports
another `feature-*`; `core-model`/`core-common` depend on nothing Android; vendor
SDKs only in `device-*`/adapter modules. Navigation between features goes through
the app module's nav host + a route contract in `core-model` (module registry,
extensibility doc).

## 4. Architecture layers (Clean Architecture + MVI)

```text
Compose screen (stateless) ── observes ─▶ ViewModel (StateFlow<UiState>, MVI intents)
        ▲                                        │ calls
        └── one-shot effects (Channel/SharedFlow)│
                                                 ▼
                                          UseCase (core-model + repository ports)
                                                 │
                                                 ▼
                              Repository (core-data) ── local-first ──▶ Room (durable cache + outbox; backend is system of record)
                                                 │                         ▲
                                                 └── remote ──▶ app-api ────┘ (sync engine reconciles)
```

- **UI state is immutable** (`data class UiState`, `copy()`), one `StateFlow`
  per screen; no mutable shared state. (Matches the repo-wide immutability rule.)
- ViewModels hold **no Android Context**; use `SavedStateHandle` + injected
  use cases. Scope tied to nav entry; cancelled with it (no leaks).
- **Local Room is a durable read cache + outbox** (the on-device *render* source so
  the UI never blocks on network) — **not** product truth. The backend is the
  system of record; the sync engine (§6) reconciles Room with it.
- Client-side checks are UX-only; **server validation is authoritative** on every
  submit (revalidates form_version + permissions + current state).
- **"UseCase" / "domain" here = app orchestration + contract/presentation models,
  NOT business rules.** Lateness/eligibility/buffer/policy/aggregation live in the
  backend; a mobile use case only orchestrates ports (fetch → cache → render →
  queue), it never computes a business outcome.

## 4a. Recomposition safety & coroutines (no screen may lag)

Hard rule: **no screen janks at any point.** Follow the official Compose runtime +
performance guidance (developer.android.com). Full checklist +
budgets: [performance-and-memory.md](performance-and-memory.md).

**State & recomposition**

- One **immutable** `data class UiState` per screen, exposed as a single
  `StateFlow`; the Composable observes with `collectAsStateWithLifecycle()` (stops
  collecting in background). Hoist state; Composables are stateless renderers.
- **Everything the UI reads must be a stable/skippable type.** UI models are
  `@Immutable`/`@Stable` with `val` only (never `var`); lists use
  `kotlinx.collections.immutable.ImmutableList`/`PersistentList` or an `@Immutable`
  wrapper — a raw `List<T>` is treated as **unstable** and kills skipping. Add a
  **stability-configuration file** for domain packages the compiler can't infer.
  Kotlin 2.x **strong skipping** is on by default (auto-remembers lambdas), but do
  not rely on it to fix genuinely-unstable params.
- **Lazy lists**: `LazyColumn`/`LazyRow` with a stable unique `key = { it.id }` and
  `contentType` for mixed rows; `animateItem` requires keys. Never sort/filter/map
  inside `items {}` — the only ViewModel transform is **mapping to immutable UI
  models + precomputing stable keys**; render lists in the **backend-provided
  order**. Business sort/filter/order semantics come from the backend payload/query
  and are never re-derived on device.
- **Shrink recomposition scope**: defer fast-changing state reads to the lowest
  Composable via a lambda provider (e.g. `scrollProvider: () -> Int`), and use
  `derivedStateOf` for values derived from frequently-changing state (e.g. "show
  scroll-to-top" from `firstVisibleItemIndex`). Don't read scroll/animation/size
  state high in the tree.
- **No expensive work in composition**, no **backwards writes** (writing state you
  already read in the same pass), no **recomposition loops** (feeding layout size
  back into layout — use proper layout primitives / `Modifier.layout`).

**Coroutines (structured concurrency)**

- `viewModelScope` for UI-scoped work; **WorkManager** for background/sync/upload;
  **no `GlobalScope`**. Children cancel with their parent (`coroutineScope` /
  `supervisorScope`).
- Inject a `DispatcherProvider`: IO on `Dispatchers.IO`, CPU on `Default`, never
  block Main; Room/Retrofit are `suspend` and run off-main.
- Repositories expose cold `Flow`; UI state via
  `stateIn(scope, SharingStarted.WhileSubscribed(5_000), initial)` with
  `distinctUntilChanged`/`flowOn`. Cancellation is cooperative; `NonCancellable`
  only for cleanup; Room writes are transactional.

**Verify (CI + tooling)**

- Compose **compiler stability/metrics report** in CI — fail when a public
  `feature-*` Composable becomes non-skippable. Layout Inspector recomposition
  counts during review. **Macrobenchmark** `FrameTimingMetric` + startup gate;
  **Baseline Profiles** shipped via `ProfileInstaller`; **LeakCanary** in debug.

## 5. Backend contract & data access

- The app talks **only** to the Goat OS app-api through the **generated Kotlin
  client**. No Firestore/GCS/BigQuery/Postgres/Sheets access from the app (repo
  non-negotiable).
- **Extend the existing app-api; this is not a green-field contract set.**
  `contracts/openapi/app-api.yaml` already ships `/app/bootstrap` (Android
  bootstrap manifest), `/app/devices/register` + heartbeat, the `/app/proofs/*`
  signed-upload flow, `/app/tasks` + `/app/tasks/{task_id}/submissions`,
  `/vaccination/execution` + `/vaccination/execution/sheds/{shed_id}`,
  `/calendar/vaccination/events`, and leadership reads
  (`/vaccination/action-center`, `/vaccination/adherence`,
  `/control-tower/vaccination`, `/action-center/obligations`). Mobile backend work
  is an **additive pass** on these, not a rebuild.
- **Bootstrap = live backend-driven config** (full spec:
  [backend-driven-config.md](backend-driven-config.md)). Reuse/extend
  `/app/bootstrap` for the mobile role lens in **three blocks**: (1)
  **`presentationConfig`** — principal + role, park/shed scope, grants/capabilities
  (as data the app maps to routes/actions, not a client role predicate), visible
  navigation + labels + disabled reasons, module registry (kill-switch), feature
  flags, app-version gate, pinned SOP/form versions, scoped option caches, and a
  monotonic **`revision` + HTTP `ETag`**; (2) **`clientRuntimeConfig`** — bounded
  client operational knobs (page sizes, sync backoff/jitter, refresh cadence, cache
  TTLs, jank-sampling), backend-owned and server-clamped, not business policy; (3)
  **no policy block** — business/medical policy (buffer window, reminder lead,
  medical thresholds, lateness/`missed` math, Asia/Kolkata bucketing) is
  **computed server-side**; the app receives **outcomes** (a dose's `status`
  `in_buffer`/`missed`, allowed reschedule date options with per-option state,
  labels, disabled reasons) and only a read-only **`policy_revision` + source**
  echoed for traceability. The app **never** does buffer/lateness math and never
  exposes policy as an editable setting. The app is a **renderer**; visible
  nav/labels/filters/disabled reasons/summary-vs-detail come from this contract,
  not hardcoded (golden frontend rule). Config is **cache-first in Room/DataStore**,
  refreshed on cold start / resume / pull-to-refresh / an **FCM `config_changed`
  data-ping** — so the maintainer can push `presentationConfig` /
  `clientRuntimeConfig` changes on the fly with **no APK release**; policy outcomes
  change only when the backend recomputes them under governed policy updates.
  Permissions stay server-authoritative (config may hide UI, never widen access).
- **Gap = the mock-shaped shed-first mobile flow.** New/extended endpoints the
  current contracts don't cover in shed-first shape: **today's-sheds-for-a-drive**
  (scope-filtered — operator/parkmgr park, director/ceo all parks), **per-shed
  scan roster** (animal + its due vaccine group + tag[s] + skip reason),
  **shed-level submit** (one record across a shed's due vaccine groups),
  **reschedule**, **assign** primary/backup, and the **role-scoped leadership
  follow-up status** that backs the calendar-card drill (per-shed live status for
  a drive: done / in-progress / delayed).
- **Reads**: today's sheds (scope = operator/parkmgr own park, director/ceo all
  parks), per-shed roster + due vaccine groups, the **per-drive follow-up status**
  (done / in-progress / delayed for the leadership drill), coverage/backlog
  rollups, overdue list, records — all paginated/shaped by backend; no full-herd
  scans. Cursor pagination, no `COUNT(*)` on hot tables (million-animal rule).
- **Writes** (shed submit, reschedule, assign) go through app-api commands, each
  with an idempotency key; one server tx creates the record + typed event +
  verification + outbox. See §6, §7.
- Contract drift is a guardrail: regenerate the client from OpenAPI in CI and
  fail on drift.

## 6. Offline-first sync engine (the hard part)

Local cache / outbox / sync state lives in Room (the backend remains business
truth); a WorkManager-driven engine reconciles.

```text
Local tables (Room):
  shed_day            cached today's sheds for the caller's role scope
                      (operator/parkmgr = own park; director/ceo = all parks or picked park)
  shed_group          due vaccine groups per shed (vaccine, dose, batch, due, done)
  roster_animal       per-shed animals + their due vaccine + tag(s) + state
  scan_event          each tap: given / skipped(reason) + timestamp + operator
  submission          one per shed submit; carries idempotency_key + fingerprint
  media_pending       captured video/proof awaiting signed-URL upload
  outbox              ordered, at-least-once client outbox → app-api
  sync_meta           cursors, last-sync, app-version, bootstrap revision
```

Submission flow (matches the existing arch doc, restated for Kotlin):

```text
enter shed → pinned form_version renders (forms-runner) offline
  → tap-scan writes scan_event rows locally (idempotent per animal+shed+session)
  → proof captured via CameraCapturePort → media_pending
  → submit writes ONE submission row with a stable idempotency_key + semantic
    fingerprint (shed_id + form_version + animal set + operator + business_date)
  → outbox row enqueued
  → WorkManager sync worker: upload media via signed URLs, then POST submit
  → app-api revalidates version + permissions + current state, runs one tx
    (form_submission + typed event + verification + outbox), returns result
  → on 2xx: mark submission ACKED; on exact replay: treat as success (idempotent)
  → on same-key/different-payload: surface conflict, never silently overwrite
```

Rules:
- **Idempotency key is generated on device** and persisted with the side effects
  (repo idempotency contract). Retries and app restarts never create duplicates.
- **At-least-once outbox**, ordered per shed; backoff with jitter; dead-letter
  after N attempts with a visible, actionable error state (never a silent empty).
- **Conflict policy**: server is authoritative. If server state moved (e.g. shed
  reassigned, version bumped), the app shows a reconcile prompt; it does not
  clobber.
- **Connectivity**: `ConnectivityManager` gates sync workers; capture always
  works offline. No user action blocks on network.
- **Multi-sensory scan feedback (offline, ≤120 ms)**: each scan emits haptic +
  audio via `FeedbackPort` — eligible = single short buzz + soft confirm tone;
  **not-due = double buzz + a distinct audible alert tone**. The eligible/not-due
  choice is a **lookup of the cached backend eligibility flag** on the roster animal
  (+ local `scan_event` for already-done) — **never** a device-side eligibility or
  date/policy computation. Only the haptic/tone render is local, so it fires
  locally on device (works offline) and the operator gets an unmistakable
  by-feel-and-sound signal without watching the screen. Honor the OS ring/DND
  policy, but treat the not-due alert as safety-critical field feedback (prefer an
  in-app tone/stream that stays audible under silent mode where policy allows).
- Every adapter/port has a **fake** (in-memory queue, fake reader, fake camera)
  for tests and local dev.

### Sync status surface (what the operator sees)

The offline/online state and the outbox are **observable**, not silent. Expose a
`SyncStatus` cold `Flow` derived from the Room outbox + WorkManager work states
(online/offline; per-item state queued → uploading proof → syncing record →
synced/failed; overall progress). It drives:

- a slim **connectivity/sync bar** on every signed-in screen (Online/Offline · All
  synced / Syncing N… with a progress line / N queued), and
- a **sync status sheet** listing each queued shed record with its state, a
  per-item progress bar, and a retry affordance on failure.

This is a UI requirement, not just plumbing: the operator must always know whether
a record is saved on the phone vs synced to the server. Demonstrated in the mock
(connectivity toggle + sync sheet with per-record progress).

### Refresh (pull-to-refresh + manual)

Read screens (Calendar, Drive status / Today's sheds, Overview, Overdue) expose an
explicit refresh: **`PullToRefreshBox`** (Compose Material 3) as the primary
gesture plus a **header refresh action** for discoverability/accessibility. It
triggers a scoped re-pull (the same `scope=<park|all>` read that feeds the screen)
into Room, then recomposes from the cache. Offline → do NOT clear the cache; show
the last-synced data with a short "offline · showing last synced" message. The
ViewModel exposes an `isRefreshing` state; refresh is idempotent and cancels with
the nav entry. Execution screens (Scan / Submit) do not offer refresh — they are
local-first and reconcile via the sync engine. Demonstrated in the mock
(pull gesture + header refresh on the four read screens).

### Server→client push for live screens (DECIDED: A is the default; B is ADR-gated)

Today client reads are **pull** (bootstrap + on-demand GETs) and the only
server→client channel is **FCM** for kernel reminders/escalations (§8). For
near-real-time leadership screens (e.g. the drive-status follow-up updating as
operators submit), the **pre-build decision is A (FCM data-ping + pull)**. B
(streaming) is **not** built by default; a screen that genuinely needs sub-second
streaming must file an ADR first. Either way the app never times a notification —
push timing is kernel-owned. The two options:

- **A — FCM data-ping + pull (default lean path)**: backend sends a lightweight
  FCM *data* message ("drive X changed"); the app invalidates and re-pulls the
  affected read. Cheap, offline-tolerant, reuses existing infra, no new transport.
- **B — streaming transport (SSE / WebSocket / gRPC-streaming) behind the app-api
  boundary**: a live subscription for dashboards. More infra + connection
  management. Repo rule bars direct gRPC for browser/RN clients without an ADR;
  native Android *could* use gRPC, but it stays ADR-gated and behind the app-api
  boundary — never a vendor SDK in feature code.

Decision of record: ship **A** (data-ping + pull) for the follow-up and reminders.
**B** is adopted only if a screen genuinely needs sub-second live streaming, and
only via an ADR in `docs/decisions/` filed **before** that screen is built.

## 7. Security & auth

- Auth via `AuthProvider` semantics already in the backend (Firebase Auth token
  today; adapter-isolated). App holds a short-lived token; refresh through the
  auth interceptor. Tokens live in **EncryptedSharedPreferences / DataStore with
  Keystore**, never in plain prefs, logs, or analytics.
- **Server-authoritative RBAC**: the app renders role lens from bootstrap but
  every command is checked server-side. No client-only permission enforcement.
- **No secrets in the repo/APK**: Firebase config via `google-services.json`
  per build type (not a secret, but scoped per project); no API keys hardcoded.
- **Logging / observability (log generously — the field has no debugger)**: all
  logging goes through a `LoggerPort` → **Timber** (debug) + **Crashlytics**
  breadcrumbs/non-fatals + custom keys (release). Structured, tagged, with
  trace/request/tenant/import-run context where available. Instrument the field
  paths so any failure is reproducible from logs alone:
  - **RFID scan**: each tap (tag read, matched due vaccine, eligible/skip+reason),
    reader connect/disconnect, battery, dropped reads. Goat identifiers
    (RFID/old-tag/breed/shed) are operational data, **not** human PII — include the
    ids a failure needs to be traceable to the exact goat/row. But Crashlytics/
    Analytics is a **third-party processor** (India-residency scope): log them
    **purposefully** — the ids a repro needs, not blanket dumps — never in a way
    that turns telemetry into a bulk livestock export. (Secrets are still a hard
    never: no tokens/credentials/service-account JSON.)
  - **Sync engine**: outbox enqueue, per-item upload/submit start+result, retry
    count/backoff, conflict, dead-letter, applied server result.
  - **Camera/proof**: capture start/stop, duration/size, upload signed-URL result.
  - **Config**: applied bootstrap **revision**, fetch latency, 304 ratio, schema
    drops (see backend-driven-config.md).
  - **Firebase Performance custom traces**: `cold_start`, `scan_tap_feedback`,
    `shed_submit`, `sync_flush`, `ble_connect`, `config_fetch` — tied to the §9
    budgets. Analytics product events for funnel (login→sheds→scan→submit).
  - **Never** log tokens/credentials/service-account JSON. Crashlytics custom keys
    may carry shed/goat/config-revision ids, never auth material.
- Network: TLS only; certificate/domain pinning considered for prod (ADR if
  added). No cleartext traffic (`usesCleartextTraffic=false`).
- Media: uploaded via signed URLs, never through the API body.

## 8. Firebase (India data residency where selectable)

- Analytics (product events), **Performance Monitoring** (cold start, screen
  render, network traces, custom scan-tap trace), **Crashlytics** (crash-free
  rate), **FCM** (push for drive reminders/escalations).
- **Region — read this carefully.** Firebase has **no global project- or app-level
  location setting**; location is chosen **per product/resource**, and some
  products don't support location selection at all. Use `asia-south1` (Mumbai) for
  the **selectable** GCP/Firebase resources that back the app (default GCP resource
  location, and Firestore/Storage if/when introduced). **Analytics (GA4),
  Crashlytics, Performance Monitoring, and FCM are global services** and are not
  region-pinned. Data-residency scope, org/project boundary, and exact create
  steps: [firebase-india-setup.md](firebase-india-setup.md). **The Firebase app is
  not created yet** — gated on org verification + explicit go.
- FCM push must still pass through the backend `NotificationGateway` (FCM
  adapter). The app registers its token via the mobile bootstrap/register
  endpoint; it does not own notification policy. Reminder/escalation timing is
  kernel-owned (operational kernel), delivered via FCM as one channel.

## 9. Performance & memory (summary — full doc: performance-and-memory.md)

Hard budgets on target low-end device: cold start ≤ 2.5 s, scan-tap feedback
≤ 120 ms, no dropped frames on scan list scroll, APK ≤ ~15 MB base, steady-state
RAM within a low-end budget. Enforced by Macrobenchmark + Baseline Profiles +
Compose stability rules. Memory-leak prevention: lifecycle-scoped coroutines,
explicit release of CameraX/BLE handles, no Context in ViewModels/singletons,
LeakCanary in debug, bounded image/bitmap sizes, and a leak gate in CI.

## 10. Internationalization

- App copy: Android per-locale resources (`values-hi`, `values-kn`, `values-te`,
  default `en`). Language is a device setting (mock's language sheet) persisted
  in DataStore and applied app-wide.
- Backend-owned copy (nav/labels/disabled reasons) is localized server-side per
  the bootstrap contract locale; the app requests its locale.
- Numerals/dates rendered in `Asia/Kolkata` business calendar. Layouts must
  survive long strings (the mock already sizes for hi/kn/te); no clipping.

## 11. Testing strategy (target ≥ 80% on domain/data)

| Layer | Tooling | What |
|---|---|---|
| Domain/use cases | JUnit5 + MockK | UI progress math (ring fill from backend `done`/`total`), rendering backend-provided `status`/`in_buffer`/`date_options`, idempotency-key derivation — **not** buffer/lateness/scheduling math (backend-owned) |
| Repositories/sync | Room in-memory + fake api + Turbine | offline write → outbox → ack; retry; conflict; dedupe (first call, exact replay, same-key/different-payload, downstream dup) |
| ViewModels | Turbine + fakes | MVI state transitions, effects |
| Compose UI | Compose UI test + Robolectric | screen renders each state from **backend grant/action-target fixtures** (visible/hidden + disabled-with-reason) — no client role predicate |
| Device adapters | fakes + instrumented | fake RFID reader emits tags; fake camera |
| E2E | Maestro flows | login → today's sheds → scan → submit (offline) → sync |
| Performance | Macrobenchmark | cold start, scroll jank, baseline profile |
| Leaks | LeakCanary (debug) + CI leak assertion | no retained activities/VMs |

Idempotency tests are mandatory (repo contract).

## 12. CI/CD

- GitHub Actions (Mesha/VGoats repo authority; `git mesha-push` for pushes).
- Jobs: ktlint/detekt, unit + Robolectric tests, Compose tests, `assembleDebug`,
  generated-client drift check, boundary check (no cross-feature imports),
  Macrobenchmark on a hosted emulator (smoke), Maestro smoke on emulator.
- **App id, namespace & flavors** (one common app; roles are runtime, NOT build
  flavors — see [`docs/decisions/mobile-app-id-and-flavors.md`](../decisions/mobile-app-id-and-flavors.md)):

  ```kotlin
  android {
    namespace = "sg.mesha.goatos"                 // FIXED — R/BuildConfig package
    defaultConfig { applicationId = "sg.mesha.goatos" }
    flavorDimensions += "env"                      // ONLY env; never persona
    productFlavors {
      create("dev")  { dimension = "env"; applicationIdSuffix = ".dev"
                       resValue("string","app_name","Goat OS Dev") }
      create("stg")  { dimension = "env"; applicationIdSuffix = ".stg"
                       resValue("string","app_name","Goat OS Stg") }
      create("prod") { dimension = "env"
                       resValue("string","app_name","Goat OS") }
    }
  }
  ```

  appId `sg.mesha.goatos` (prod) · `.dev` · `.stg`. Each appId is a separate
  Firebase app registration → per-flavor `google-services.json`. There are **no**
  `operator/manager/director/ceo` flavors: role + scope + nav come from
  `/app/bootstrap` at runtime, so one AAB serves every role (one security model,
  one review/pen-test path, no variant drift). appId is permanent on Play — the
  Play Console account must be under the Mesha/VGoats org (org-boundary rule).
- Build variants: `dev` / `stg` / `prod` flavors × `debug`/`release`, each
  pointing at the matching Firebase project + app-api base URL. No prod secrets
  in CI logs; state active org/project before any Firebase/gcloud step
  (org-boundary rule).
- Release: signed AAB, staged rollout; Crashlytics + Performance gates before
  promotion.

## 13. Implementation order (first tasks, before feature code)

1. Backend: **extend the existing app-api** with the mock-shaped shed-first flow —
   today's-sheds-for-a-drive (scope-filtered), per-shed scan roster, shed-level
   submit, reschedule, assign, and the role-scoped leadership follow-up status —
   reusing `/app/bootstrap`, `/vaccination/execution/sheds/{shed_id}`,
   `/calendar/vaccination/events`, `/app/proofs/*`, `/app/tasks/*/submissions`,
   and the obligation read models. Regenerate the Kotlin client. (Additive pass,
   not a green-field contract set.)
2. Firebase India project/app create (gated runbook) → `google-services.json`
   per flavor.
3. `apps/goatos-android` Gradle skeleton: modules, theme from design tokens,
   nav host + module registry, Hilt graph, Room + DataStore, sync/outbox engine
   with fakes, RFID/camera ports with fakes.
4. Auth + mobile bootstrap wiring; role-lens shell.
5. Vertical slice end-to-end: today's sheds → scan (fake reader) → submit
   (offline) → sync → leadership sees it. Then real RFID adapter, then remaining
   screens to mock fidelity.

## 14. Non-negotiables (mobile)

- **The phone is a dumb renderer.** Backend owns truth, policy, permissions,
  scheduling, and all DB/Redis querying + aggregation. The app renders backend
  payloads and queues user input/media — it does **not** compute buffer/lateness/
  `missed` math, schedule reminders, pick/reserve FEFO stock, decide role/scope/
  action grants, or bucket business time. It receives computed outcomes (status,
  labels, date options, disabled reasons, action/route IDs) and renders them.
- App is a renderer; backend owns nav/labels/filters/disabled reasons/field sets.
- No direct datastore access; only generated client → app-api.
- No client-only RBAC; server authoritative on every command.
- Every write idempotent; sync is at-least-once with dedupe; no silent overwrite.
- No vendor SDK outside adapters; every adapter has a fake.
- Offline capture always works; failures surface as visible error states.
- Sync is observable, never silent: a `SyncStatus` Flow drives an on-screen
  connectivity/sync bar + outbox progress; the operator always knows saved-local
  vs synced-to-server.
- Scan feedback is multi-sensory via `FeedbackPort` (with a fake): a not-due (red)
  hit fires haptic + an audible alert tone; eligible fires haptic + a soft tone.
- Backend computes all business-time buckets (due / missed / reminder) in
  `Asia/Kolkata`; the app **formats and displays** backend date fields and uses
  `Asia/Kolkata` only for pure display formatting — it never buckets or decides
  business time itself.
- Mock is the only UI source of truth; match its structure, not a plainer copy.
- Firebase: `asia-south1` for location-selectable resources under the correct org
  (telemetry/push are global services); nothing created before the
  org-verification checklist passes.
