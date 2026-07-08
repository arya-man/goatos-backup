# TRD — Goat OS Operator Mobile (Android · Kotlin + Jetpack Compose)

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

### Baseline versions (pin at first build, record actuals in app README)

```text
Kotlin              2.x (latest stable)
AGP / Gradle        latest stable, Gradle version catalog (libs.versions.toml)
Jetpack Compose     BOM (latest stable) + Material 3
compileSdk/target   latest stable (36+)
minSdk              24   (covers the low-end field fleet; revisit with device data)
JDK                 17 (toolchain)
```

Every dependency is pinned in a **version catalog**. New deps require a note in
the app README with the reason, matching the admin-web pinning discipline.

## 2. Libraries (all have a fake/mock for tests — non-negotiable)

| Concern | Choice | Notes |
|---|---|---|
| UI | Compose + Material 3 | custom theme from design-system tokens |
| Navigation | Navigation-Compose (type-safe routes) | module registry drives destinations |
| DI | Hilt | constructor injection; no service locators |
| Async | Coroutines + Flow | structured concurrency; no `GlobalScope` |
| Local DB | **Room** (SQLite) | tasks/sheds/roster/submission queue/outbox |
| Key-value | DataStore (Proto) | session flags, language, device/reader state |
| Network | Retrofit + OkHttp + kotlinx.serialization | generated client (below) |
| API client | **OpenAPI-generated Kotlin client** | from backend app-api contract; never hand-written DTOs |
| Images/video | CameraX (video capture) + Coil (thumbnails) | bounded bitmap sizes |
| RFID/BLE | Vendor `.aar` behind a `RfidReaderPort` | Chainway-class UHF; adapter isolates SDK |
| Haptics + sound | `Vibrator` + short tone (`ToneGenerator` / `SoundPool`) behind a `FeedbackPort` | scan feedback; distinct not-due alert tone; fake in tests |
| Media upload | WorkManager + signed-URL uploader | resumable, retryable |
| Background | WorkManager | sync, upload, retry, reminders |
| Firebase | Analytics, Performance, Crashlytics, Messaging (FCM) | `asia-south1` where selectable; GA4/Crashlytics/Perf/FCM are global — see firebase doc |
| i18n | Android resources per-locale + backend contract copy | en/hi/kn/te |
| Testing | JUnit5, Turbine, MockK, Compose UI test, Room in-memory, Robolectric, Maestro (E2E), Macrobenchmark | see §11 |

No vendor SDK may be called from feature/product code. SDKs live only inside
adapters behind ports (mirrors backend ports/adapters).

## 3. Module structure (Gradle multi-module)

`apps/operator-android/` — Gradle root. Multi-module keeps build times low on
low-end CI and enforces boundaries (the mobile equivalent of admin-web's
`check-boundaries`).

```text
apps/operator-android/
  app/                         # thin: Application, DI graph, nav host, theme, boot
  core/
    core-designsystem/         # theme, tokens, Compose components (design-system.md)
    core-ui/                   # shared stateless UI (rings, chips, sheets, list rows)
    core-model/                # pure Kotlin domain models (no Android/vendor deps)
    core-common/               # Result types, dispatchers, time (Asia/Kolkata), errors
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
                              Repository (core-data) ── local-first ──▶ Room (source of truth on device)
                                                 │                         ▲
                                                 └── remote ──▶ app-api ────┘ (sync engine reconciles)
```

- **UI state is immutable** (`data class UiState`, `copy()`), one `StateFlow`
  per screen; no mutable shared state. (Matches the repo-wide immutability rule.)
- ViewModels hold **no Android Context**; use `SavedStateHandle` + injected
  use cases. Scope tied to nav entry; cancelled with it (no leaks).
- **Local Room is the on-device read source of truth**; the UI never blocks on
  network. The sync engine (§6) reconciles with the backend.
- Client-side checks are UX-only; **server validation is authoritative** on every
  submit (revalidates form_version + permissions + current state).

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
- **Bootstrap**: reuse/extend `/app/bootstrap` for the mobile role lens —
  principal + role, park/shed scope, grants/capabilities, visible navigation +
  labels, disabled reasons, app-version gate, pinned SOP/form versions, scoped
  option caches. The app is a **renderer**; visible nav/labels/filters/disabled
  reasons/summary-vs-detail come from this contract, not hardcoded (golden
  frontend rule).
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

State lives in Room; a WorkManager-driven engine reconciles.

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
  **not-due = double buzz + a distinct audible alert tone**. Feedback fires
  locally on device (works offline), so the operator gets an unmistakable
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

### Server→client push for live screens (OPEN DECISION — needs ADR)

Today client reads are **pull** (bootstrap + on-demand GETs) and the only
server→client channel is **FCM** for kernel reminders/escalations (§8). For
near-real-time leadership screens (e.g. the drive-status follow-up updating as
operators submit), two options are on the table and must be decided in an ADR
before build:

- **A — FCM data-ping + pull (default lean path)**: backend sends a lightweight
  FCM *data* message ("drive X changed"); the app invalidates and re-pulls the
  affected read. Cheap, offline-tolerant, reuses existing infra, no new transport.
- **B — streaming transport (SSE / WebSocket / gRPC-streaming) behind the app-api
  boundary**: a live subscription for dashboards. More infra + connection
  management. Repo rule bars direct gRPC for browser/RN clients without an ADR;
  native Android *could* use gRPC, but it stays ADR-gated and behind the app-api
  boundary — never a vendor SDK in feature code.

Recommendation to discuss: start with **A** (data-ping + pull) for the follow-up
and reminders; adopt **B** only if a screen genuinely needs sub-second live
streaming. Capture the decision in `docs/decisions/` before implementing.

## 7. Security & auth

- Auth via `AuthProvider` semantics already in the backend (Firebase Auth token
  today; adapter-isolated). App holds a short-lived token; refresh through the
  auth interceptor. Tokens live in **EncryptedSharedPreferences / DataStore with
  Keystore**, never in plain prefs, logs, or analytics.
- **Server-authoritative RBAC**: the app renders role lens from bootstrap but
  every command is checked server-side. No client-only permission enforcement.
- **No secrets in the repo/APK**: Firebase config via `google-services.json`
  per build type (not a secret, but scoped per project); no API keys hardcoded.
- **Logging**: goat identifiers (RFID/old tag/breed/shed) are operational data,
  log freely for field diagnosis; **never** log tokens/credentials. Crashlytics
  custom keys may carry shed/goat ids, never auth material.
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
| Domain/use cases | JUnit5 + MockK | shed math, group progress, buffer logic, idempotency-key derivation |
| Repositories/sync | Room in-memory + fake api + Turbine | offline write → outbox → ack; retry; conflict; dedupe (first call, exact replay, same-key/different-payload, downstream dup) |
| ViewModels | Turbine + fakes | MVI state transitions, effects |
| Compose UI | Compose UI test + Robolectric | screen renders each state, role gating, disabled-with-reason |
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
3. `apps/operator-android` Gradle skeleton: modules, theme from design tokens,
   nav host + module registry, Hilt graph, Room + DataStore, sync/outbox engine
   with fakes, RFID/camera ports with fakes.
4. Auth + mobile bootstrap wiring; role-lens shell.
5. Vertical slice end-to-end: today's sheds → scan (fake reader) → submit
   (offline) → sync → leadership sees it. Then real RFID adapter, then remaining
   screens to mock fidelity.

## 14. Non-negotiables (mobile)

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
- `Asia/Kolkata` for all business-time meaning.
- Mock is the only UI source of truth; match its structure, not a plainer copy.
- Firebase: `asia-south1` for location-selectable resources under the correct org
  (telemetry/push are global services); nothing created before the
  org-verification checklist passes.
