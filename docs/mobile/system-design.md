# System Design — Goat OS Operator Mobile

Runtime architecture for the Android app. Pairs with the [TRD](trd-operator-mobile.md).

## 1. High-level runtime

```text
┌──────────────────────────── Android device (low-end) ────────────────────────────┐
│  Compose UI  ─▶ ViewModel (StateFlow/MVI) ─▶ UseCase ─▶ Repository                 │
│                                                        │                           │
│                                          ┌─────────────┴──────────────┐            │
│                                          ▼                            ▼            │
│                                    Room (SQLite)                 core-network      │
│                                  on-device truth              Retrofit+OkHttp      │
│                                          │  outbox / media_pending   │ (gen client)│
│                                          ▼                            │            │
│                                   WorkManager sync ──────────────────┘            │
│   device-rfid (BLE adapter) ─▶ RfidReaderPort      device-camera ─▶ CameraPort     │
│   Firebase: Analytics · Performance · Crashlytics · FCM (all via ports/adapters)   │
└───────────────────────────────────────────┬───────────────────────────────────────┘
                                             │ HTTPS (TLS)
                                             ▼
                        Goat OS app-api (Go modular monolith, ports/adapters)
              bootstrap · shed reads · submit · reschedule · assign · signed media
                 │ one tx: form_submission + typed event + verification + outbox
                 ▼
     Postgres (operational truth) · GCS (media) · Pub/Sub (analytics/notify relay)
                 │
                 ├─▶ NotificationGateway → FCM → device push
                 └─▶ analytics/export → coverage & backlog read models
```

The device never reaches Postgres/GCS/BigQuery/Firestore directly. Every read is
a shaped/paginated backend response; every write is an idempotent command.

## 2. App boot / bootstrap sequence

```text
launch → splash
  → auth: valid session? ─no─▶ Login (email + OTP) ─▶ token
  → GET <mobile bootstrap>  (principal, role lens, park/shed scope, grants,
                             visible nav + labels, disabled reasons,
                             app-version gate, pinned SOP/form versions,
                             scoped option caches, bootstrap revision)
  → version gate: incompatible? ─▶ blocking "update app" screen
  → persist bootstrap to DataStore (revision-checked); build nav from module
    registry filtered by grants
  → route to role home:  operator → Calendar/Today's sheds ;
                         parkmgr/director/ceo → Overview
```

The app must **block business UI until bootstrap succeeds** (no local-default
flash), matching the admin-web contract rule. Cached bootstrap is used offline
with its revision; a newer revision refreshes nav/labels on reconnect.

## 3. Threading model

- **Main/UI**: Compose recomposition only. No IO, no parsing, no bitmap decode.
- **`Dispatchers.Default`**: shed math, group-progress computation, diffing.
- **`Dispatchers.IO`**: Room, network, file/media, DataStore.
- **WorkManager**: sync, media upload, reminder scheduling — survives process death.
- **BLE/RFID**: vendor SDK callbacks marshalled off the main thread into a Flow
  (`callbackFlow`), debounced, then to `Default` for dedupe.
- Structured concurrency only; scopes tied to `viewModelScope` / `WorkManager` /
  a lifecycle-bound reader scope. No `GlobalScope`. Cancellation releases BLE/
  camera handles (see performance doc).

## 4. Sync state machine (per submission)

```text
DRAFT ──submit──▶ QUEUED ──worker picks──▶ UPLOADING_MEDIA ──ok──▶ POSTING
  ▲                                                 │ fail(retryable)  │
  │                                                 ▼                  ▼
  └───────────────── edit while draft            BACKOFF◀───────── (retry w/ jitter)
                                                    │ N attempts        │ 2xx
                                                    ▼                    ▼
                                                 DEAD_LETTER          ACKED
                                                (visible error,     (idempotent;
                                                 actionable)         exact replay = ACKED)
                       server state moved ─▶ CONFLICT (reconcile prompt; never clobber)
```

Idempotency key + semantic fingerprint are written with the DRAFT row and reused
for every retry. Exact replay returns the original result with no new side
effects; same-key/different-payload is a conflict, not an overwrite.

## 5. Read data flow (shed-first)

```text
Today's sheds  = GET sheds?scope=<park>  → cache shed_day/shed_group/roster
Scan roster    = from roster_animal (each animal → its due vaccine group)
Coverage/hero  = GET rollup?scope=<park> (given/scheduled/pending, per-vaccine)
Backlog        = GET backlog?scope=<park> (pending doses per vaccine)
Data gaps      = GET gaps?scope=<park> (animals excluded from coverage + reason)
Overdue        = GET overdue?scope=<park> (missed vs in-buffer)
Records        = GET shed-record/{shed_id} (per-vaccine breakdown + animals)
```

All scope-filtered server-side; cursor pagination for lists; no `COUNT(*)`/full
scans. `Asia/Kolkata` day boundaries drive "today"/"overdue"/"missed".

## 6. Push / notification flow

```text
kernel obligation/reminder/escalation (backend, Asia/Kolkata timing)
  → NotificationGateway.NotifyUser/Role  → FCM adapter  → device
  → app renders notification (drive in 2 days / submit today / overdue escalation)
  → tap deep-links into the relevant screen (shed, overdue, reschedule)
Device registers FCM token via mobile bootstrap/register-device on boot + refresh.
```

The app never decides *when* to notify; timing/policy is kernel-owned. FCM is one
delivery channel alongside call/Slack/email (mock's 4-channel model).

## 7. Analytics event taxonomy (Firebase Analytics)

Product events (no PII; goat/shed ids allowed as params, tokens never):

```text
app_open, login_success, bootstrap_loaded{revision, role}
shed_opened{shed_id, park, groups}
scan_tap{shed_id, result: given|skipped, reason?, vaccine}
shed_submitted{shed_id, animals, vaccines, offline_duration_ms}
sync_result{submission_id, outcome: acked|conflict|dead_letter, attempts}
leadership_view{screen, scope}
scope_changed{from, to}          data_gaps_opened{scope, count}
reschedule_confirmed{shed_id, in_buffer}
assign_confirmed{shed_id, primary, backup}
```

Performance traces: `cold_start`, `scan_tap_feedback`, `shed_list_scroll`,
`submit_roundtrip`, per-network-call auto traces.

## 8. Error handling

- API/RBAC/route errors surface as **visible error states** (alert band / error
  card), never swallowed into an empty list that reads as "no data" (repo rule).
- Empty-but-OK keeps sections visible with zero-count badges + empty copy.
- Offline is a first-class state, not an error: capture continues; a subtle
  "will sync" indicator; dead-letter is the only hard failure and it is
  actionable (retry / view details).

## 9. Observability

Client: Crashlytics (crash-free rate, non-fatals for dead-letters/conflicts),
Performance (traces above), structured breadcrumbs with trace/request ids that
correlate to backend logs (goat/shed ids ok, no secrets). Backend already emits
latency/queue-lag/DLQ/media-failure metrics; mobile submit ids correlate through
the outbox trace id.
