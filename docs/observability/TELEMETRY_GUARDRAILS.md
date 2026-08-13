# Telemetry Guardrails

> Status: **enforced** (local CI) · Owner: platform/observability
> Companion to `docs/observability/OBSERVABILITY_DESIGN.md` (infra/signal design).
> This doc is the **product-code** rule: what every feature must wire, and how
> that gets checked automatically.

## 1. The standing rule

**Every new or changed user-facing feature MUST wire telemetry:**

1. **Firebase Analytics event(s)** — the user action is recorded as a named,
   constant-defined analytics event (never an inline string).
2. **Fatal + non-fatal crash/error logging (Crashlytics)** — failure paths on
   that surface report a non-fatal, and unhandled exceptions still reach the
   fatal crash reporter.
3. **Funnel / user-journey step tracking**, where the surface is part of a
   tracked journey (see `docs/observability/OBSERVABILITY_DESIGN.md` §2.5 for
   the current funnel list: `login → bootstrap → drive-open → scan →
   vaccination-capture → submit`).

This applies to:

- **All future development** — every new screen, viewmodel, route, or
  product-facing event emission.
- **Existing surfaces being modified** — touching a screen/viewmodel/route
  for an unrelated reason still requires bringing its telemetry up to this
  bar (or exempting it with a stated reason — see §4).
- **New surfaces** — a brand-new feature-package, screen, or route ships with
  telemetry from day one, not as a follow-up ticket.

### Why

Goat OS is an operational kernel; every stage from `event → txn → audit/outbox
→ trigger → obligation → sweeper → notify → proof → read model` is measured on
the backend (`context/architecture/operational-kernel.md`,
`docs/observability/OBSERVABILITY_DESIGN.md`). The client side (Android app,
admin-web) is the part of the system that actually proves a human completed
the intended action. Without analytics + crash + funnel wiring at the client:

- A silently-broken screen (crash, dead button, blocked funnel step) is
  invisible until a field operator complains days later.
- Funnel drop-off (e.g. `login → bootstrap` succeeding but `drive-open → scan`
  never firing) can't be measured, so the mobile rollout can't be trusted.
- Product decisions (which surfaces get used, which fail silently) have no
  evidence trail.

A single guard doc + a single CI check keeps this rule from silently rotting
as new screens/routes get added by different agents/sessions.

## 2. What each surface must wire

### 2.1 Android screens / viewmodels

Every `*Screen.kt` (Composable screen) and `*ViewModel.kt`, and every new file
added under an `apps/goatos-android/**/feature-*/` or `.../features/*`
package, must reference:

- An **analytics event** on the primary user action(s) — via `AnalyticsPort`
  (`.track(event, props)`), using a constant from `AnalyticsEvents`, never an
  inline string (see §3).
- A **Crashlytics non-fatal** report on every caught failure path (a
  `catch`/`Result.failure`/error `UiState` branch that the user can hit) — via
  `CrashReporter.recordException(throwable, message)` (see §3.1; wired through
  DI in `di/TelemetryModule.kt`, gated on `BuildConfig.TELEMETRY_ENABLED`).
- A **funnel step**, if the screen sits on a tracked journey (`login →
  bootstrap → drive-open → scan → vaccination-capture → submit`, or a future
  documented funnel) — via the `AnalyticsFunnels` helper object (see §3.1),
  which already covers `drive-open`/`scan`/`vaccination-capture`/`submit`;
  `login`/`bootstrap` are covered by existing `AnalyticsEvents` calls.
  Feature call sites (feature-calendar, feature-scan, feature-record,
  feature-submit) still need to invoke these helpers — see §3.2.

### 2.2 admin-web routes

Every `app/**/page.tsx` route and `*Screen.tsx` component must reference:

- A **Faro event or trace** (`@grafana/faro-web-sdk`, initialized via
  `FaroProvider` in `apps/admin-web/app/layout.tsx` per
  `OBSERVABILITY_DESIGN.md` §2.4) for the primary user action, OR
- **Error boundary coverage** — every route under the `(admin)` route group is
  already covered GLOBALLY by `ObservabilityErrorBoundary`, wrapped once in
  `apps/admin-web/app/(admin)/layout.tsx` (a plain React class component that
  calls `faro.api?.pushError(...)` in `componentDidCatch`, not a per-route
  Next.js `error.tsx`) — see §3.1.

Because crash/error reporting is already structural (one boundary wrapping
every admin route), the remaining gap this surface's `warn` mode is tracking
is **per-route custom analytics events** (`faro.api.pushEvent`/`setView`-style
calls for the primary user action on that specific route), which individual
`page.tsx` files mostly do not have yet. This surface stays **`warn`
(non-blocking)** in the CI guard config (`tools/telemetry-guard/config.json`)
until that per-route event backlog is closed — see §3.2. Flip
`surfaces.admin_web.mode` to `"block"` once it is.

### 2.3 Backend commands / endpoints

Backend request paths already get RED metrics + traces + logs automatically
(`otelhttp` middleware, `otelpgx` tracer, `slog` → Cloud Logging — see
`OBSERVABILITY_DESIGN.md` §2.1–2.3 and `docs/decisions/observability.md`).
**No per-endpoint guard is needed for that baseline.** The rule that does
apply to backend code: when a backend change introduces a **new
product-meaningful event** that a funnel/journey depends on (for example, a
new obligation-completion event that a mobile funnel step should reflect),
the change must also update the relevant funnel documentation
(`OBSERVABILITY_DESIGN.md` §2.5/§2.6) so the client-side wiring stays in sync
with what the backend now emits. This is a documentation-sync expectation,
not a separate CI guard (backend RED/trace/log coverage is structural, not
per-PR-checkable the way a screen's analytics call is).

## 3. Required symbols (grep-able)

These are the exact, real symbol names in this repo today. Android names are
pulled from `apps/goatos-android/core/core-analytics/src/main/kotlin/sg/mesha/goatos/core/analytics/`
(`AnalyticsPort.kt`, `AnalyticsEvents.kt`, `AnalyticsFunnels.kt`,
`CrashReporter.kt`, `FirebaseAnalyticsAdapter.kt`, `FirebaseCrashReporter.kt`)
and `di/AnalyticsModule.kt` / `di/TelemetryModule.kt`. Several of those files'
own docstrings say "Stable public API grepped by the telemetry CI
guardrail (see `docs/TELEMETRY.md`) — do not rename without updating that doc
and the guard" — **`apps/goatos-android/docs/TELEMETRY.md` does not exist in
this repo yet** (as of this doc's last update); it is owned by the mobile
analytics lane, not this guardrail lane. When it lands, treat it as the
call-site-level source of truth for Android telemetry and link to it from
here instead of restating call-site detail; this doc and
`tools/telemetry-guard/config.json` remain the source of truth for what the
CI guard itself checks.

### 3.1 Wired today

**Android — analytics:**

| Symbol | Where | What it is |
|---|---|---|
| `AnalyticsPort` | `AnalyticsPort.kt` | The seam interface: `track(event, props)`, `setUserProperty(name, value)`. |
| `NoopAnalytics` | `AnalyticsPort.kt` | DI fallback for flavors without a confirmed Firebase project (today: `dev`/`prod`) and in tests — does nothing. Its presence alone does **not** satisfy the guard; the call site still needs a real `.track(...)` invocation using an `AnalyticsEvents` constant. |
| `FirebaseAnalyticsAdapter` | `FirebaseAnalyticsAdapter.kt` | Real `AnalyticsPort` bound when `BuildConfig.TELEMETRY_ENABLED` (today: `stg`); composes `CrashReporter` so every `setUserProperty` call also sets a Crashlytics custom key. |
| `AnalyticsEvents` | `AnalyticsEvents.kt` | Canonical event name constants: `APP_OPEN`, `SESSION_START`, `BOOTSTRAP_LOADED`, `LOGIN_ATTEMPT`, `LOGIN_SUCCESS`, `LOGIN_FAILURE`, `PASSWORD_RESET_REQUESTED`, `PASSWORD_RESET_SENT`, `SIGN_OUT`. |
| `AnalyticsEvents.Params` / `.UserProps` | `AnalyticsEvents.kt` | Event parameter keys (`METHOD`, `REASON`, `CHROME`) and durable user-property keys (`ROLE`, `PRIMARY_PARK`, `FLAVOR`, `TENANT`). |
| `analytics.track(...)` | any call site (`BootstrapViewModel.kt`, `SessionViewModel.kt`, `GoatOsApplication.kt`) | The actual invocation the guard looks for. |

**Android — crash/error (`CrashReporter` seam):**

| Symbol | Where | What it is |
|---|---|---|
| `CrashReporter` | `CrashReporter.kt` | The seam interface: `recordException(throwable, message?)`, `log(message)` (breadcrumb), `setCustomKey(key, value)` — non-PII only (role/park/flavor/tenant; never name/email/free text). |
| `NoopCrashReporter` | `CrashReporter.kt` | DI fallback, same gating as `NoopAnalytics`. |
| `FirebaseCrashReporter` | `FirebaseCrashReporter.kt` | Real `CrashReporter` backed by `com.google.firebase.crashlytics.FirebaseCrashlytics`, bound via `di/TelemetryModule.kt`. |
| `recordException` | any call site | The actual non-fatal-report invocation the guard looks for. |
| `FailureReportingNetworkTelemetryReporter` | `FailureReportingNetworkTelemetryReporter.kt`, bound in `di/TelemetryModule.kt` | **Structural** API-failure coverage. Decorates the `NetworkTelemetryReporter` seam that `TelemetryInterceptor` already invokes for every OkHttp call, and on `status >= 400` (or `-1`, meaning the call threw before any response) emits: a logcat WARN, a Crashlytics breadcrumb, an `AnalyticsEvents.API_CALL_FAILURE` event, and a Crashlytics non-fatal throttled to one per `(method, route, status)` per minute. Because it sits at the interceptor, a new screen that forgets its own `recordException` still produces API-failure signal. Only the Firebase **Performance** delegate is gated on `TELEMETRY_ENABLED` — the failure half runs on every flavor, so logcat is never silent during a retry storm. |
| `FailureReportingOutboxTelemetryReporter` | `FailureReportingOutboxTelemetryReporter.kt`, bound in `di/AppModule.kt` | **Structural** coverage for writes that never reach the network at all — the half the row above explicitly cannot see. Implements the `OutboxTelemetryReporter` port (`:core:core-common`), which `SyncRepository.enqueue` and `SyncEngine.processItem`/`recordFailure` emit from, so every queued write announces `ENQUEUED`, `ATTEMPT_STARTED`, `ATTEMPT_FAILED`, `RETRY_SCHEDULED` and `TERMINAL`. Every phase produces a logcat WARN (tag `GoatOsOutbox`) and a Crashlytics breadcrumb; `ATTEMPT_FAILED` also emits `AnalyticsEvents.SYNC_WRITE_ATTEMPT_FAILED`; `TERMINAL` — data that will never be sent — emits `AnalyticsEvents.SYNC_WRITE_DEAD` plus a Crashlytics non-fatal throttled to one per `(opType, terminalReason, failureClass)` per minute. Runs on every flavor: before this, a proof upload could retry for minutes with zero device-side evidence and could only be diagnosed from the server log and Postgres. Never carries a payload, the server's error copy, a token, or an Authorization header. |

**Android — funnel steps (`AnalyticsFunnels` helper):**

| Symbol | Where | What it is |
|---|---|---|
| `AnalyticsFunnels` | `AnalyticsFunnels.kt` | Typed helpers for the `drive-open`/`scan`/`vaccination-capture`/`submit` funnel stages: `trackDriveOpened`, `trackScanStarted`, `trackScanCompleted`, `trackVaccinationCaptureStarted`, `trackVaccinationCaptureCompleted`, `trackSubmitAttempted`, `trackSubmitSucceeded`, `trackSubmitFailed`. `login`/`bootstrap` stages are already covered by existing `AnalyticsEvents` calls (`AnalyticsFunnels.Events.LOGIN_*`/`BOOTSTRAP_LOADED` alias them for a single dashboard source). |

**admin-web:**

| Symbol | Where | What it is |
|---|---|---|
| `FaroProvider` | `apps/admin-web/components/observability/faro-provider.tsx`, mounted in `app/layout.tsx` | Initializes `@grafana/faro-web-sdk`, browser-only, no-op if `NEXT_PUBLIC_FARO_COLLECTOR_URL` is unset. Also calls `faro.api.setView({ name: pathname })` on route change. |
| `ObservabilityErrorBoundary` | `apps/admin-web/components/observability/error-boundary.tsx`, mounted once in `app/(admin)/layout.tsx` | Wraps **every** `(admin)` route globally; `componentDidCatch` calls `faro.api?.pushError(error, ...)`. This is why crash/error coverage is already structural for admin-web (§2.2) — it is not a per-route `error.tsx`. |
| `faro.api.pushError(...)` | `error-boundary.tsx` | The actual crash-report call the guard's `faro`/`pushError` markers match. |

### 3.2 TODO — still gaps (tracked here so the guard doesn't silently assume more coverage than exists)

| Gap | Status | Notes |
|---|---|---|
| Feature call sites for `AnalyticsFunnels` | **TODO** | `AnalyticsFunnels.kt`'s own docstring is explicit that feature-calendar (drive-open), feature-scan (`ScanViewModel`), feature-record (`RecordViewModel`), and feature-submit call sites are **not** wired by that file — they are owned by the parallel mobile-feature session. Until each feature calls the matching `AnalyticsFunnels.track*` helper, that funnel stage has no signal even though the helper exists. |
| Per-route Faro custom events on admin-web | **TODO** | Crash/error coverage is global (§2.2, §3.1), but individual `page.tsx` routes mostly do not yet call `faro.api.pushEvent`/`trackEvent` for their primary user action. This is why `admin_web` stays `mode: "warn"` in the guard config — see §2.2. |
| Writes that never reach the network produce no signal | **CLOSED** | Previously: the OkHttp seam only sees calls that were actually made, so a write stuck in the durable outbox — queue stalled, group head failing, attempts exhausted — emitted nothing at all. Closed by `FailureReportingOutboxTelemetryReporter` (§3.1) on the client and by the `outbox_message_dead_lettered` / `outbox_message_permanently_failed` / `outbox_messages_dead_lettered_on_claim` ERROR logs in `backend/internal/outbox/app/service.go` on the server, where abandoned domain events had previously been recorded only as metric counters. |
| ExoPlayer media requests bypass every network seam | **TODO** | `VerifyDetailScreen.kt` and `WeighingLeadershipVideosScreen.kt` build players with `ExoPlayer.Builder(context).build()`, which uses media3's `DefaultHttpDataSource` — **not** the app's OkHttp client. Proof-video fetches therefore never reach `TelemetryInterceptor`, so neither Firebase Perf's OkHttp instrumentation nor `FailureReportingNetworkTelemetryReporter` sees them. A burst of HTTP 500s on a proof object surfaces on the client only if media3 gives up entirely and raises `onPlayerError` (→ `VERIFY_VIDEO_PLAYBACK_ERROR`); retries below that threshold are invisible on-device, and the server log is the only evidence. Fix requires adding the `androidx.media3:media3-datasource-okhttp` dependency and passing an `OkHttpDataSource.Factory` built from the injected client — a dependency addition, deliberately not bundled into the guardrail change that documented it. |
| `apps/goatos-android/docs/TELEMETRY.md` | **TODO (other lane)** | Referenced by several Android source docstrings as the call-site-level reference doc; does not exist in this repo yet. Owned by the mobile analytics lane, not this guardrail lane — this doc intentionally does not create it (see the top of §3). |

### 3.3 Marker list used by the guard

The exact strings the CI guard searches for are configured (not
hardcoded in Python) in `tools/telemetry-guard/config.json`:

```json
"android":   ["AnalyticsPort", "analytics.track", "AnalyticsFunnels", "AnalyticsEvents", "CrashReporter", "Crashlytics", "recordException", "logNonFatal"],
"admin_web": ["faro", "trackEvent", "pushEvent", "pushError", "ErrorBoundary"]
```

Update that file (not this doc, not the Python script) when a new marker name
is adopted.

## 4. Escape hatch: `// telemetry:exempt <reason>`

If a surface genuinely has no user-facing telemetry to wire — an internal
debug-only screen, a pure layout/presentational component with no user
action, a route that is 100% covered by a parent layout's error boundary —
add a comment containing the exempt marker anywhere in the file:

```kotlin
// telemetry:exempt internal debug-only screen, never shipped to users
```

```tsx
// telemetry:exempt purely presentational wrapper; parent route owns telemetry
```

The reason text is not validated for content, only presence — this is a
**reviewer-facing** escape hatch. A vague or missing reason should be pushed
back on in code review; do not use it to silence the guard without a real
justification. The marker string itself (`telemetry:exempt`) is configurable
in `tools/telemetry-guard/config.json`'s top-level `exempt_marker` key.

## 5. Compliant vs non-compliant examples

### 5.1 Android — non-compliant (guard FAILs)

```kotlin
// apps/goatos-android/feature-drives/.../DriveOpenScreen.kt
@Composable
fun DriveOpenScreen(viewModel: DriveOpenViewModel) {
    val state by viewModel.state.collectAsState()
    Button(onClick = { viewModel.onOpenDrive() }) { Text("Open drive") }
}
```

No `AnalyticsPort`/`AnalyticsEvents`/`AnalyticsFunnels`/`CrashReporter`
reference anywhere in the file or a sibling in the same directory, and no
`// telemetry:exempt` — the guard reports
`[FAIL] android: .../DriveOpenScreen.kt`.

### 5.2 Android — compliant

```kotlin
// apps/goatos-android/feature-drives/.../DriveOpenViewModel.kt
class DriveOpenViewModel(
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    private val openDrive: OpenDriveUseCase,
) : ViewModel() {
    fun onOpenDrive(driveId: String) {
        AnalyticsFunnels.trackDriveOpened(analytics, driveId) // funnel step: drive-open
        viewModelScope.launch {
            openDrive(driveId)
                .onFailure { e ->
                    crashReporter.recordException(e, "DriveOpenViewModel.onOpenDrive")
                    analytics.track(
                        AnalyticsEvents.DRIVE_OPEN_FAILURE,
                        mapOf(AnalyticsEvents.Params.REASON to (e.message ?: "unknown")),
                    )
                }
        }
    }
}
```

The guard also accepts this wiring living in `DriveOpenViewModel.kt` alone
(sibling-file lookup) even if `DriveOpenScreen.kt` itself has no direct
analytics call — the screen and its viewmodel are treated as one unit.

### 5.3 admin-web — non-compliant (guard WARNs today, will FAIL once `block`)

```tsx
// apps/admin-web/app/(admin)/vaccination/page.tsx
export default function VaccinationPage() {
  return <VaccinationDashboard />;
}
```

### 5.4 admin-web — compliant

```tsx
// apps/admin-web/app/(admin)/vaccination/page.tsx
"use client";
import { useEffect } from "react";
import { faro } from "@grafana/faro-web-sdk";

export default function VaccinationPage() {
  useEffect(() => { faro.api?.pushEvent("vaccination_page_view"); }, []);
  return <VaccinationDashboard />;
}
```

Note that `ObservabilityErrorBoundary` already wraps every `(admin)` route
globally (`apps/admin-web/app/(admin)/layout.tsx`, §3.1) — a route does not
need its own `error.tsx` to get crash reporting. The gap this guard tracks
for admin-web today (§2.2, §3.2) is the missing per-route `pushEvent` call
above, not missing crash coverage.

## 6. How the CI guard works

### 6.0 What the guard CANNOT see (read this before trusting a PASS)

The guard is a **substring-presence check over a git diff**. It proves a
marker string appears; it can never prove telemetry works. Specifically it
cannot see:

- **Whether the call is on the path that actually runs.** A file containing
  `analytics.track(...)` in a branch nothing reaches passes.
- **Sibling contamination.** `sibling_lookup: true` means a marker in ANY
  `.kt` in the same directory satisfies every `Screen.kt`/`ViewModel.kt` in
  that directory. One instrumented file can carry a whole package.
- **Whether the port is a real vendor impl or a `Noop`.** `NoopAnalytics` /
  `NoopCrashReporter` are bound for any flavor without `TELEMETRY_ENABLED`,
  and they are silent — no logcat, no local echo. A PASS says nothing about
  whether an event leaves the device.
- **Anything outside the diff.** Untouched screens are never re-checked, so
  the guard reports no backlog; use `make telemetry-guard-audit` (`--all`)
  for that.
- **Non-OkHttp network paths.** See §3.2's ExoPlayer row.
- **The backend entirely.** There is no guard on backend log coverage; the
  `http_4xx`/`http_5xx` lines in `platform/httpresponse` are covered by unit
  tests, not by this script.

Treat a PASS as "nobody removed the markers", not as "this failure will be
visible".

### 6.1 Mechanics

Local CI script: `tools/telemetry-guard/telemetry-guard.py` (Python 3,
stdlib only). Config: `tools/telemetry-guard/config.json`. Tests:
`tools/telemetry-guard/test_telemetry_guard.py`. Full usage in
`tools/telemetry-guard/README.md`.

Summary:

- **Scope**: diff-scoped by default — `merge-base(origin/main, HEAD)...HEAD`,
  falling back to `HEAD~1...HEAD`, falling back to a full-repo scan with a
  printed warning if neither resolves. `--base <ref>`, `--staged`, and `--all`
  override the scope. This mirrors `tools/agent-hooks/check-mobile-list-fetch.mjs`'s
  diff-scoping convention so a commit touching neither Android nor admin-web
  passes instantly.
- **Android surface** (`mode: block`): ADDED/MODIFIED `*Screen.kt` /
  `*ViewModel.kt`, plus ADDED files under a `feature-*`/`features/` package,
  must reference a marker (§3.3) in the file or a sibling `.kt` file in the
  same directory, or carry `// telemetry:exempt <reason>`. Missing → `FAIL`
  (blocks the guard, non-zero exit).
- **admin-web surface** (`mode: warn`): ADDED `app/**/page.tsx` /
  `*Screen.tsx` must reference a marker or carry the exempt comment. Missing →
  `WARN` (reported, does not block). See §3.2/§2.2 for why this starts as
  `warn`.
- **Exit code**: non-zero only if at least one `FAIL` finding exists.

### Run it locally

```bash
# Same check CI runs (diff vs origin/main, or HEAD~1 fallback)
make telemetry-guard

# Full-repo audit (backlog view — this repo currently has real findings,
# since Analytics/Crashlytics/Faro rollout across every surface is in progress)
make telemetry-guard-audit

# Direct invocation with more control
python3 tools/telemetry-guard/telemetry-guard.py --base origin/main
python3 tools/telemetry-guard/telemetry-guard.py --staged
python3 tools/telemetry-guard/telemetry-guard.py --all --json
```

`make telemetry-guard` is part of the `guardrails` Makefile aggregate and runs
inside `make ci-local JOB=guardrails`, which is what `.github/workflows/ci.yml`'s
`guardrails` job invokes. Per `AGENTS.md`, a green `make ci-local` on the
pushed SHA is the authoritative gate regardless of GitHub Actions billing/
platform availability.

## 7. Rollout note

Running `make telemetry-guard-audit` against the current tree surfaces real,
pre-existing gaps (most admin-web routes have no per-route Faro event call
yet; several Android screens predate this rule and predate the
`CrashReporter`/`AnalyticsFunnels` seams). That is expected — `telemetry-guard`
itself is **diff-scoped by default** so it only blocks *new or changed*
surfaces going forward; it does not retroactively fail the whole repo on its
own. The pre-existing backlog is **not**, however, left unenforced: a
separate whole-tree shrink-only ratchet (`make telemetry-guard-ratchet`, IS
wired into `make ci-local` via `guardrails`) fails if that backlog grows or if
the checked-in baseline goes stale. See §9 and
`docs/observability/GUARDRAIL_RATCHET.md` for the mechanism. Closing the
backlog (wiring per-route Faro events, wiring
`AnalyticsFunnels`/`CrashReporter` into the remaining feature call sites) is
tracked via §3.2's TODO list; each fix should also shrink the ratchet
baseline (`make telemetry-guard-ratchet-regenerate`).

## 8. Companion rule: never swallow an exception (`exception-guard`)

> Status: **enforced** (local CI, `make ci-local`) · Owner: platform/observability
> Tool: `tools/exception-guard/exception_guard.py` (+ `exception-guard.py` CLI
> entrypoint, `config.json`). Same diff-scoping machinery as §6, a distinct
> escape-hatch marker, and a separate CI target — kept as a sibling guard
> rather than folded into `telemetry-guard` because it checks control-flow
> shapes (catch bodies, `if err != nil` blocks) instead of file-level marker
> presence.

**The maintainer's golden rule:** *"Never swallow any exception. Always
either dump it into Firebase non-fatal errors (mobile) or backend logs
(server)."*

This is narrower than §1–§7 above (which govern user-facing *screens/routes*
having analytics+crash+funnel wiring) and applies to **every** catch block or
Go error check touched by a diff, screen or not: if code catches an
exception or checks an `err`, it must either do something with it (record it,
log it, wrap and return it) or explicitly mark why not.

- **Mobile (Kotlin):** every caught exception must reach
  `CrashReporter.recordException(throwable, message)` (or an equivalent
  crash/analytics recording call — see the marker list in
  `tools/exception-guard/config.json`), not just a `Log.x(...)`/`println`
  call or a bare `return`/`emit(...)`.
- **Backend (Go):** every checked `err != nil` must be logged
  (`slog.Error`/`logger.Error`/…) or returned wrapped (`fmt.Errorf("...: %w",
  err)`), not silently discarded (`_ = err`) or converted to an explicit
  `return nil` success without a trace.

### 8.1 Compliant vs non-compliant examples

**Kotlin — non-compliant (swallowed):**

```kotlin
try {
    syncRepository.push(item)
} catch (e: IOException) {
    // nothing — or just a log line
    Log.e(TAG, "sync failed", e)
}
```

**Kotlin — compliant:**

```kotlin
try {
    syncRepository.push(item)
} catch (e: IOException) {
    CrashReporter.recordException(e, "sync push failed for ${item.id}")
}
```

**Go — non-compliant (swallowed):**

```go
err := writeAuditRow(ctx, tx, evt)
if err != nil {
    return nil // error silently discarded, caller sees success
}
```

**Go — compliant:**

```go
if err := writeAuditRow(ctx, tx, evt); err != nil {
    return fmt.Errorf("write audit row: %w", err)
}
```

### 8.2 Escape hatch

A `// exception:exempt <reason>` comment (deliberately a **different marker**
from `// telemetry:exempt <reason>` in §4, so the two guards' findings never
get confused, but the same convention family — one comment style, one
required freeform reason) inside or immediately preceding the catch/error
block exempts it.

### 8.3 Scope: diff-added lines only, never whole-repo

Like `telemetry-guard`, this guard is **diff-scoped by default**
(`origin/main...HEAD`, falling back to `HEAD~1...HEAD`) — but it goes one
step further and only looks at **lines the diff actually added**, not whole
changed files, because a changed file's *other*, untouched catch blocks are
legacy debt this guard does not (and should not) retroactively fail. A commit
that doesn't add or touch a swallowing catch/err-check passes instantly, even
if the file it's in has other, older violations. `exception-guard-audit`
(`--all`, full-tree scan) exists for a raw, unratcheted whole-tree listing.

That legacy backlog is not left unenforced, though — see §9: a whole-tree
shrink-only ratchet (`make exception-guard-ratchet`) IS wired into `make
ci-local` and fails if the backlog grows past a checked-in baseline, or if
the baseline goes stale relative to fixes.

### 8.4 Deliberately NOT detected

Tuned hard against false positives — when in doubt, this guard does **not**
flag:

- Multi-line catch bodies or `if err != nil` blocks with control flow beyond
  a simple empty/log-only/bare-return/explicit-nil-return shape (best-effort
  brace matching, not a real parser).
- An error wrapped in a custom type and returned up the stack without a local
  log call — that's a legitimate "log once, at the top layer" pattern; this
  guard only flags a block that references `err` **nowhere at all**.
- `if err != nil { return }` with a **bare** `return` (no value) — a common
  Go guard-clause idiom ("bail out of this optional/logging helper, the
  error was already handled by the caller or elsewhere"). Only an
  **explicit** `return nil` (or `return ..., nil`) is flagged, because that
  shape means the code affirmatively turned a real error into a reported
  success. This narrowing exists because the guard's first run against this
  repo's real (uncommitted) working-tree diff flagged two legitimate
  success-only-logging guard clauses —
  `backend/internal/sop/adapters/http/handler.go`'s `logSubmitOutcome` and
  `backend/internal/weighing/adapters/http/handler.go`'s `logScanOutcome` —
  as false positives; both bail out on error because the error was already
  logged with full context by the caller's `respond`/`WriteError` path.
- `_ = err` (or `_ = resp.Close()`-style discards) immediately after or on
  the same line as a `.Close(`/`.Rollback(` call — conventionally safe to
  discard in Go and not worth flagging.
- Generic `Result<T>`/`Either`-style monadic error handling.
- Test files: `*Test.kt`, `*_test.go`, and test directories are excluded
  entirely (see `config.json`'s `exclude_globs`).

### 8.5 Running it

```bash
python3 tools/exception-guard/exception-guard.py --self-test   # fixture suite
python3 tools/exception-guard/exception-guard.py                # diff vs origin/main
python3 tools/exception-guard/exception-guard.py --staged        # diff vs the index
python3 tools/exception-guard/exception-guard.py --all           # whole-repo audit (visibility only)
```

`make exception-guard` runs the self-test then the diff-scoped check, and is
chained into `make ci-local` the same way `make telemetry-guard` is. `make
exception-guard-audit` runs the `--all` full-tree scan.

## 9. Whole-tree enforcement: the shrink-only ratchet

> Full mechanism, key format, and rationale: `docs/observability/GUARDRAIL_RATCHET.md`.
> Driver: `tools/ci/ratchet-guard.py`. Wired in: `make ci-local` (via the
> `guardrails` Makefile target) → `make exception-guard-ratchet` +
> `make telemetry-guard-ratchet`.

**The lesson this section exists to prevent recurring:** a diff-scoped guard
(§6, §8.3) silently permits **unlimited pre-existing debt**. `telemetry-guard`
and `exception-guard` only ever inspect lines a diff touches — that is
correct and deliberate (day-one adoption without failing on legacy code) —
but it means the `--all` whole-tree variants (`telemetry-guard-audit`,
`exception-guard-audit`) were, for a long stretch of this repo's history,
**not wired into anything**. An adversarial whole-tree audit found on the order of 150 `exception-guard`
FAILs (Go + Kotlin combined) and 51 `telemetry-guard` FAILs (+24 WARNs)
sitting in the tree with a permanently green `make ci-local`; the exact count
moves as debt gets fixed (see the baseline files for the current number). A
guard
that cannot fail is documentation, not enforcement — and a diff-scoped-only
guard, by construction, never fails on anything that was already there
before the diff.

The fix is **not** `--all` wired in directly (that would fail CI immediately
and get someone to switch it off under time pressure). It is a **shrink-only
ratchet**:

1. Today's known violations are frozen into a committed baseline
   (`tools/exception-guard/baseline.json`, `tools/telemetry-guard/baseline.json`),
   keyed by **file + rule-kind, never line number** (line numbers churn on
   unrelated edits and would desync the baseline).
2. `make exception-guard-ratchet` / `make telemetry-guard-ratchet` run the
   `--all --json` whole-tree scan and fail if any FAIL finding is **not** in
   the baseline (new debt) — this is what actually blocks new swallowed
   exceptions / missing telemetry anywhere in the tree, not just on touched
   lines.
3. They also fail if the baseline is **stale-high** — contains an entry that
   no longer reproduces, meaning debt was fixed but the baseline was never
   shrunk down. Without this, fixed debt would silently free up "budget" to
   reintroduce an equivalent violation elsewhere without ever tripping the
   ratchet.
4. Every run prints the outstanding debt count, so it stays visible instead
   of forgotten.

**Regenerating the baseline is for shrinking it after a real fix** (`make
exception-guard-ratchet-regenerate` / `make telemetry-guard-ratchet-regenerate`).
**Adding a new entry to a baseline file to land code that trips the ratchet
is not an accepted way to land new code** — if the ratchet fails on your
change, fix the violation (add the recording/telemetry call, or a genuine
`// exception:exempt <reason>` / `// telemetry:exempt <reason>`), don't widen
the baseline.

## Instrument the INTENT, not only the OUTCOME

**A control that does nothing emits nothing.** Every event on a primary
control in this app — video play, verdict submit, scan submit, shed close —
used to hang off an async OUTCOME callback: a player listener, a repository
result, a ViewModel side effect. That is a structural blind spot: if the
callback never fires (a stuck ExoPlayer at `STATE_ENDED`/`STATE_IDLE` where
`play()` is a silent no-op, a coroutine that swallows its own failure, a dead
outbox write), there is no code path left to record anything. "The operator
tapped it and nothing happened" becomes indistinguishable from "the operator
never tapped it at all" — exactly the failure a verifier hit on real hardware
tapping play on a finished proof video (2026-08-04).

**The rule going forward:** a tap on a primary control must produce a
telemetry record synchronously, at the click site, before the control can
possibly no-op. The matching outcome — if there is one worth waiting for —
gets a bounded timeout: if it doesn't land, that is itself a reportable event
plus a non-fatal, not silence.

### The pattern: `DeadControlWatchdog`

`core/core-analytics/src/main/kotlin/sg/mesha/goatos/core/analytics/
TelemetryWatchdog.kt` — one reusable class, not copy-pasted per feature:

```kotlin
val watchdog = DeadControlWatchdog(analytics, crashReporter, scope, intentEvent, deadControlEvent)

// at the click site, BEFORE calling the control's action:
watchdog.armIntent(contextProps, timeoutMs)

// the moment the real outcome callback lands:
watchdog.disarm()

// on screen/ViewModel teardown, so leaving is never mistaken for a dead control:
watchdog.cancel()
```

- `armIntent` fires the INTENT event immediately and starts one timer. A
  second tap before the first resolves cancels-and-replaces the timer, so a
  user mashing a genuinely dead button shows up as **repeated INTENT
  events** (real signal) rather than a pile of overlapping dead-control
  reports for one tap (noise).
- If `disarm()` never arrives within `timeoutMs`, the watchdog fires BOTH a
  distinct dead-control analytics event and a `CrashReporter.recordException`
  non-fatal — never one without the other, and never swallowed.
- `contextProps` must carry enough to act on the report without re-deriving
  it: the relevant item/proof/task id, plus whatever state existed at tap
  time (player state, armed/prepared, pending count).

Event names for both halves follow the existing `snake_case` convention and
are checked against Firebase's reserved-name list by inspection before
landing (`session_start` was rejected outright and had to become
`app_session_start` — see above); `AnalyticsFunnels` constants are the single
source, never an inline string at a call site.

### Applied so far

| Control | Intent event | Dead-control event | Timeout | Why that window |
|---|---|---|---|---|
| Verify proof-video play/pause (inline + fullscreen) | `verify_video_play_intent` | `verify_video_play_dead` | 1500ms | Proof clips are seconds long and already buffered/streamed; a healthy tap responds in well under a second even from a cold decoder spin-up (`player.prepare()` on first tap). 1500ms absorbs that spin-up and a brief network stall without false-positiving, while staying short enough that a report is still useful — see `AnalyticsFunnels.VERIFY_VIDEO_PLAY_WATCHDOG_TIMEOUT_MS`. |

Owned by `VerifyDetailViewModel` (`app/src/main/kotlin/sg/mesha/goatos/
viewmodel/VerifyDetailViewModel.kt`), one watchdog per proof id
(`playWatchdogFor`), cancelled in `onCleared()`. The Composable
(`feature/feature-verify/.../VerifyDetailScreen.kt`) stays a pure renderer —
it only forwards `VerifyDetailEvent.VideoPlayback(action = PLAY_INTENT)`
synchronously at the tap (before `player.play()`/`pause()`) and
`PLAY_OUTCOME` synchronously from the player listener's
`onIsPlayingChanged` (either direction) — the ViewModel decides what to do
with those two signals.

**Reviewed and deliberately NOT wired with a watchdog in this pass:**

- **Approve/Reject verdict submit** (`VerifyDetailViewModel.submitVerdict`):
  already emits `VERIFY_VERDICT_ATTEMPTED` synchronously at the tap, before
  the outbox enqueue call — the INTENT half already exists. The OUTCOME half
  (`VERIFY_VERDICT_SUCCEEDED`/`VERIFY_VERDICT_FAILED`) is driven by
  `syncRepo.enqueueVerificationVerdict`'s direct `AppResult` return, not an
  async callback that can be silently dropped — the coroutine that calls it
  cannot "return nothing" the way a player listener can "never fire". Lower
  risk; left as a follow-up rather than blocking this pass.
- **Scan submit** (`feature-scan`) and **shed close/ack**
  (`feature-verify`'s drive-close path): both are owned by parallel
  workstreams per `AnalyticsFunnels.kt`'s existing "not wired here, see
  `docs/TELEMETRY.md`" notes for `feature-scan`/`feature-record`, and were
  out of scope for this pass to avoid colliding with in-flight changes to
  those files. They are real candidates for the same pattern — the
  dead-control class is intentionally reusable and takes no
  verify-specific dependency — and should adopt `DeadControlWatchdog` the
  next time those screens are touched, rather than reimplementing a
  bespoke intent/timeout scheme.

### Can the guard catch a missing INTENT event automatically?

**Not reliably, and this doc says so rather than pretending otherwise.**
`telemetry-guard.py` is a coarse, file-level check: "does this changed file
contain at least one recognized tracking/funnel call". It has no notion of
Compose click-handler bodies, no AST-level pairing of an `onClick =` lambda
with the analytics call inside it, and no cross-file tracing from a
Composable's `onClick` through an event/callback into the ViewModel that
actually calls `AnalyticsFunnels.*` (exactly the shape used here — the intent
event is one hop away from the click, in `VerifyDetailViewModel`, not inlined
in the Composable). Building that would need a real Kotlin/Compose AST parser
that understands lambda bodies and cross-function event flow — a
meaningfully bigger tool than the current line/regex-based guard, and prone
to false positives on any indirection (which is most of this codebase's
architecture, by design: `feature-*` modules stay analytics-free per the
"telemetry:exempt: pure stateless renderer" convention used throughout
`feature-verify`).

What the guard **can** and should keep doing: flag a changed
user-facing-surface file with **zero** telemetry calls of any kind (today's
check), and — as a possible future addition — flag a new `DeadControlWatchdog(`
construction whose paired `armIntent(`/`disarm(` calls don't both appear
in the same diff, since that pairing IS a simple textual co-occurrence check
unlike inferring intent-at-click-site from scratch. That extension is not
implemented in this pass; this paragraph exists so it isn't silently
forgotten either.
