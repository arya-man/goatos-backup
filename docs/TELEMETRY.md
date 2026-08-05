# Telemetry Standard (repo-wide)

Status: standing rule, machine-enforced by `make ci-local`. GitHub Actions is
billing-blocked in this project, so `make ci-local` is the only real CI gate —
see `docs/observability/GUARDRAIL_RATCHET.md` and `AGENTS.md` /
`docs/runbooks/local-ci.md` for that context.

This doc is the one-stop, copy-paste-able reference for instrumenting a NEW
screen/feature so it passes CI on the first try, without reading any diff or
session history. Companion docs (read these for the full backstory/design,
not required to follow the recipe below):

- `docs/observability/OBSERVABILITY_DESIGN.md` — infra/signal design (Firebase
  project layout, egress pipeline, funnel definitions).
- `docs/observability/TELEMETRY_GUARDRAILS.md` — the original repo-wide rule
  write-up and the `DeadControlWatchdog` intent/outcome pattern for controls
  whose async outcome can silently never arrive.
- `apps/goatos-android/docs/TELEMETRY.md` — the Android-specific stable
  symbol list (`AnalyticsPort`, `AnalyticsEvents`, `AnalyticsFunnels`,
  `CrashReporter`) and Firebase wiring/build-flavor details. That doc is the
  Android symbol-name source of truth; this doc does not duplicate it.
- `tools/telemetry-guard/README.md` — the guard's own CLI usage reference.

## 1. Event naming convention

- `snake_case`, always. Matches the analytics egress convention
  (`context/analytics/final-analytics-infra.md`) and GA4/BigQuery export
  column naming.
- Every event/param/user-property name is a named constant in
  `AnalyticsEvents` (or a feature-local `object Params`), never an inline
  string literal at the call site. This is what makes the reserved-name and
  screen-view guards (§4, §7) mechanically checkable at all — they look for
  constant declarations and direct call-site literals, not arbitrary strings.
- Funnel/journey step events go in `AnalyticsFunnels` (see
  `core/core-analytics/src/main/kotlin/sg/mesha/goatos/core/analytics/AnalyticsFunnels.kt`)
  as typed helper functions, one call per stage, never hand-rolled at each
  feature call site.

## 2. Required params for journey reconstruction

Every event must be reconstructable into a single user journey after the
fact, purely from BigQuery, without joining against app logs. At minimum:

| Param | Purpose |
|---|---|
| `journey_id` | This work-session's stable id (`DeviceStore.journeyId()`), stamped on every event by the adapter — not passed manually per call site. |
| `device_id` | This install's stable id (`DeviceStore.appInstallId()`) — distinguishes the same login on two phones. Also stamped automatically. |
| an entity id (`drive_id`, `shed_id`, `goat_id`/`tag_id`, `proof_id`, `verdict_id`, …) | Whatever the event is ABOUT. Without this, an event says "something happened" with no way to answer "to what". |
| `reason` (on every failure/blocked event) | A coarse, non-PII, enum-like cause — e.g. `permission_denied`, `not_assigned`, `network_timeout`, `validation_failed`. Free-text error strings are NOT acceptable reason codes (they fragment a dashboard into one row per unique message). |

`journey_id`/`device_id` are stamped centrally by the `AnalyticsPort`
implementation — a call site never sets them manually. A call site IS
responsible for the entity id(s) and, on any failure branch, the `reason`
code.

## 3. The LaunchedEffect-keyed screen-view rule

Compose recomposes. An unkeyed `LaunchedEffect(Unit) { analytics.track(...) }`
at the top of a screen fires again on every recomposition that happens to
re-enter that scope (config change, parent state churn, nav restore), not
just once per real "user arrived at this screen" event — which silently
inflates screen-view counts and corrupts funnel drop-off math (a funnel stage
that "fires twice" looks like 100% retention into itself).

**Rule:** key the `LaunchedEffect` on the identity of what's being viewed —
the route/id, not `Unit`:

```kotlin
@Composable
fun ShedDetailScreen(shedId: String, viewModel: ShedDetailViewModel = hiltViewModel()) {
    LaunchedEffect(shedId) {
        analytics.track(AnalyticsEvents.SHED_DETAIL_OPENED, mapOf(AnalyticsEvents.Params.SHED_ID to shedId))
    }
    // ... screen content
}
```

If the screen truly has no varying identity (a static settings screen, say),
key on the screen's own name as a literal string constant instead of `Unit`,
so intent is explicit either way — a bare `Unit` key reads as "nobody thought
about this" to the next reader.

## 4. The PII rule

- **Goat RFID / tag IDs ARE loggable.** Maintainer ruling (see repo memory
  `goat-data-not-pii`): goat identifiers are operational data, not personal
  data. Log them freely in analytics events and debug output.
- **Emails, phone numbers, auth tokens, and any human-identifying free text
  are NOT loggable.** Never pass a raw email/phone/token as an event param.
  If a human-identity signal is genuinely needed for analysis, use a stable
  opaque id (Firebase UID, internal user id) as a user-property, never the
  raw PII value itself.
- This distinction is why `Params.EMAIL`/user-email-adjacent params must be
  reviewed case-by-case — the guard cannot detect "this param is PII", only
  reserved Firebase names (§5). Use judgment; when genuinely unsure, ask
  before wiring it.

## 5. The reserved-Firebase-name trap

Firebase silently REJECTS events/params using its own reserved vocabulary —
no compile error, no runtime exception, the event is just dropped on the
floor and the dashboard has a silent gap. This bit this exact repo for real:
the very first event of every journey was meant to be `session_start`, but
Firebase reserves that name and rejected it outright ("Invalid public event
name. Event will not be logged (FE): session_start"). It became
`AnalyticsEvents.SESSION_START = "app_session_start"` — see the comment at
that constant in
`core/core-analytics/src/main/kotlin/sg/mesha/goatos/core/analytics/AnalyticsEvents.kt`.

Reserved and now hard-CI-checked (see §7 — `tools/telemetry-guard`'s
`reserved_names` rule, NOT exemptable, not a style warning):

- Reserved **event names**: `app_remove`, `app_update`, `app_clear_data`,
  `app_uninstall`, `error`, `first_open`, `first_visit`, `first_open_time`,
  `first_visit_time`, `in_app_purchase`, `notification_dismiss`,
  `notification_foreground`, `notification_open`, `notification_receive`,
  `os_update`, `session_start`, `session_start_with_rollout`, `session_end`,
  `screen_view`, `user_engagement`, `ad_activeview`, `ad_click`,
  `ad_exposure`, `ad_query`, `ad_reward`, `adunit_exposure`,
  `dynamic_link_first_open`, `dynamic_link_app_open`,
  `dynamic_link_app_update`.
- Reserved **param/user-property prefixes**: `firebase_`, `google_`, `ga_`.

If your event name collides, prefix it (`app_`, a feature prefix, whatever
reads naturally) exactly like `SESSION_START` did. There is no escape hatch
for this rule — Firebase itself rejects the literal name regardless of what
the code intends.

## 6. Failure/outcome events need a reason code, always

See §2's `reason` param requirement. Concretely: a catch block, an
`AppResult.Failure` branch, or a `Result.failure` shown to the user should
fire a `*_FAILED`/`*_FAILURE` analytics event carrying `reason`, in addition
to (not instead of) recording the exception via `CrashReporter` — see
`tools/exception-guard/` for the separate, stricter "never swallow an
exception" rule, which this overlaps with intentionally. exception-guard
enforces "record it somewhere technical (Crashlytics/logs)"; this rule wants
a user-facing outcome event a product dashboard can chart.

## 7. What the guards enforce, and what they deliberately cannot catch

Two Python guards, both stdlib-only, both diff-scoped by default with a
whole-tree shrink-only ratchet closing the "existing debt never re-checked"
gap (see `docs/observability/GUARDRAIL_RATCHET.md` for why the ratchet
pattern exists at all).

### `tools/telemetry-guard/telemetry-guard.py`

| Surface | Mode | What it checks | Known limitation |
|---|---|---|---|
| `android` / `admin_web` (original, unchanged) | block / warn | File-presence of an analytics/Crashlytics marker string, in the changed file or a sibling in the same directory. | Presence-only — does not verify the marker is on a reachable code path, or fires with correct params. |
| `screen_view` (new) | block | Same presence check, scoped to `*Screen.kt`/`*Route.kt` files, requiring a `AnalyticsEvents.`/`AnalyticsFunnels.`/`trackScreenView`-family marker. | Cannot verify the marker sits inside a properly `LaunchedEffect`-keyed block (§3) — that is a code-review responsibility this guard cannot statically prove. |
| `primary_action` (new) | **warn only** | Best-effort: flags `*Screen.kt` files with no analytics marker at all near an onClick-shaped write/nav action. | Cannot trace across function boundaries — `onClick = { viewModel.onSubmitClicked() }` with the actual `analytics.track(...)` call living inside `onSubmitClicked()` (the single most common real shape in this codebase) is invisible to a whole-file/sibling marker check and would false-positive constantly at `mode=block`. Kept WARN deliberately; treat findings as a worklist, not a gate. |
| `failure_outcome` (new) | **warn only** | Best-effort: flags `*ViewModel.kt` files with no `*_FAILURE`/`*_FAILED`/AnalyticsEvents/AnalyticsFunnels marker anywhere in the file. | File-level presence only — cannot verify the failure event fires on the actual failing branch, or that it carries a `reason` param (§2/§6). WARN for the same reason as `primary_action`. |
| `reserved_names` (new) | **block, not exemptable** | Regex scan for (a) `const val X = "..."` / `export const X = "..."` constant declarations and (b) literals passed directly to `.track(`/`logEvent(`/`trackEvent(`/`pushEvent(`/`*ScreenView(` calls, checked against the reserved list in §5. | Cannot see a name built at runtime via string concatenation/interpolation, or passed through an intermediate variable before the call. Scoped this narrowly on purpose — an earlier "flag every quoted string" draft produced overwhelming noise on ordinary UI strings (e.g. the word `"error"` used as a generic state label, or `"google_error"` as an unrelated object key) with nothing to do with analytics. |

Escape hatch for everything except `cancellation_swallowed` and
`reserved_names`: `// telemetry:exempt <reason>` anywhere in the file.

Ratchets (whole-tree, shrink-only, never blocking on pre-existing debt but
blocking on ANY new debt): `telemetry-guard-ratchet` (original 2 surfaces,
baseline `tools/telemetry-guard/baseline.json`) and
`telemetry-guard-ratchet-v2` (the new `screen_view`/`reserved_names` FAIL
rules only — `primary_action`/`failure_outcome` are WARN and never
ratcheted — baseline `tools/telemetry-guard/baseline-v2.json`). The v2
baseline is intentionally SEPARATE from the original so the original's
51-violation baseline (frozen before this guard existed) is never touched or
reinterpreted by the newer rules.

### `tools/exception-guard/exception-guard.py`

| Kind | Exemptable? | What it checks |
|---|---|---|
| `empty_catch` / `log_only_catch` / `swallowed_catch` / `runcatching_discard` (Kotlin), `swallowed_err` / `discarded_err` (Go) — original 6 | Yes, `// exception:exempt <reason>` | The pre-existing "never swallow an exception" rule. See the script's own docstring for the exact shapes matched/not matched. |
| `cancellation_swallowed` (new) | **No — hard rule** | `catch (e: CancellationException) { ... }` (Kotlin coroutines) that does not `throw e` before the block ends. Swallowing cancellation breaks structured concurrency — the parent scope never learns the child was cancelled. There is no legitimate reason to swallow it, so the exempt marker does not suppress this kind. |
| `bare_exempt_marker` (new) | N/A (it IS the exempt-marker rule) | `// exception:exempt` with no reason text after it on the same line. Previously the marker's presence alone was sufficient — a bare marker is indistinguishable from someone reaching for the escape hatch just to silence the guard. |

Escape hatch: none for `cancellation_swallowed`; `bare_exempt_marker` is
fixed by adding the reason the marker was always supposed to carry.

Ratchets: `exception-guard-ratchet` (original 6 kinds, baseline
`tools/exception-guard/baseline.json`, 124 pre-existing violations at the
time this doc was written) and `exception-guard-ratchet-v2` (the two new
kinds, baseline `tools/exception-guard/baseline-v2.json`) — same
separate-baseline reasoning as the telemetry ratchets above.

## 8. Copy-paste example: instrumenting a brand-new screen

Say you're adding `FeatureXScreen.kt` with a primary "Submit" action that can
fail.

```kotlin
package sg.mesha.goatos.feature.featurex

import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort

@Composable
fun FeatureXScreen(
    itemId: String,
    viewModel: FeatureXViewModel = hiltViewModel(),
    analytics: AnalyticsPort = LocalAnalytics.current, // however this repo's DI exposes it
) {
    // 1. SCREEN-VIEW — keyed on itemId (§3), not Unit.
    LaunchedEffect(itemId) {
        analytics.track(
            AnalyticsEvents.FEATURE_X_OPENED,
            mapOf(AnalyticsEvents.Params.ITEM_ID to itemId),
        )
    }

    val state by viewModel.uiState.collectAsState()

    Button(onClick = {
        // 2. PRIMARY ACTION — fire synchronously at the tap, before the write.
        analytics.track(
            AnalyticsEvents.FEATURE_X_SUBMIT_ATTEMPTED,
            mapOf(AnalyticsEvents.Params.ITEM_ID to itemId),
        )
        viewModel.onSubmitClicked(itemId)
    }) {
        Text("Submit")
    }
}
```

```kotlin
// FeatureXViewModel.kt
class FeatureXViewModel(
    private val repo: FeatureXRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    fun onSubmitClicked(itemId: String) {
        viewModelScope.launch {
            when (val result = repo.submit(itemId)) {
                is AppResult.Success -> {
                    analytics.track(
                        AnalyticsEvents.FEATURE_X_SUBMIT_SUCCEEDED,
                        mapOf(AnalyticsEvents.Params.ITEM_ID to itemId),
                    )
                }
                is AppResult.Failure -> {
                    // 3. FAILURE OUTCOME — reason code + non-fatal, never one without the other.
                    analytics.track(
                        AnalyticsEvents.FEATURE_X_SUBMIT_FAILED,
                        mapOf(
                            AnalyticsEvents.Params.ITEM_ID to itemId,
                            AnalyticsEvents.Params.REASON to result.reasonCode, // enum-like, not free text
                        ),
                    )
                    crashReporter.recordException(result.cause, "FeatureX submit failed: ${result.reasonCode}")
                }
            }
        }
    }
}
```

```kotlin
// AnalyticsEvents.kt additions — snake_case, non-reserved (§5), constants only:
const val FEATURE_X_OPENED = "feature_x_opened"
const val FEATURE_X_SUBMIT_ATTEMPTED = "feature_x_submit_attempted"
const val FEATURE_X_SUBMIT_SUCCEEDED = "feature_x_submit_succeeded"
const val FEATURE_X_SUBMIT_FAILED = "feature_x_submit_failed"
```

This satisfies: `screen_view` (block), `android` (block, marker present),
`primary_action` (warn, marker present in-block so it wouldn't even warn),
`failure_outcome` (warn, `FAILED` marker present in the ViewModel),
`reserved_names` (none of the four names collide with §5's list), and
exception-guard (the `AppResult.Failure` branch records via
`crashReporter.recordException`, not swallowed).

## 9. Running the guards locally

```bash
# Diff-scoped (what a PR/commit sees):
python3 tools/telemetry-guard/telemetry-guard.py
python3 tools/exception-guard/exception-guard.py

# Whole-tree, unratcheted (for exploring current debt):
python3 tools/telemetry-guard/telemetry-guard.py --all
python3 tools/exception-guard/exception-guard.py --all

# The actual CI gates (shrink-only ratchets against the baselines above):
make telemetry-guard-ratchet
make telemetry-guard-ratchet-v2
make exception-guard-ratchet
make exception-guard-ratchet-v2

# Everything (what make ci-local runs):
make ci-local
```
