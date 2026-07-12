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
`CrashReporter`/`AnalyticsFunnels` seams). That is expected — the guard is
**diff-scoped by default** so it only blocks *new or changed* surfaces going
forward; it does not retroactively fail the whole repo. Closing the backlog
(wiring per-route Faro events, wiring `AnalyticsFunnels`/`CrashReporter` into
the remaining feature call sites) is tracked via §3.2's TODO list, not by
making the guard stricter before the underlying SDK wiring exists.
