package sg.mesha.goatos.core.analytics

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/**
 * Intent/Outcome instrumentation for primary controls whose only success signal is an async
 * callback (a player listener, a repository result, a ViewModel side effect). Those controls are
 * structurally blind: if the callback never fires — a stuck ExoPlayer at STATE_ENDED, a coroutine
 * that silently loses its exception, a dead outbox write — NOTHING is ever recorded, and "the
 * operator tapped it and nothing happened" is indistinguishable from "the operator never tapped
 * it at all".
 *
 * [DeadControlWatchdog] closes that gap with two rules:
 *  1. [armIntent] fires an INTENT event synchronously, at the tap, before anything can no-op.
 *  2. If the matching [disarm] does not arrive within [timeoutMs], a distinct
 *     "dead control" event AND a non-fatal are emitted — the tap happened, the system did nothing.
 *
 * One [Job] is held at a time: a new [armIntent] cancels and replaces any pending watchdog for
 * this control, so a mashed dead button produces repeated INTENT events (a real signal — the
 * button IS being hammered) but never a pile of overlapping/duplicate dead-control reports for a
 * single tap.
 *
 * Reusable across features: build one per control (per screen, not shared globally, since each
 * carries its own event names + context props), call [armIntent] at the click site, call
 * [disarm] the moment the real outcome callback lands, and call [cancel] on dispose so an
 * in-flight watchdog never fires against a screen the user already left. See
 * `docs/observability/TELEMETRY_GUARDRAILS.md` for the intent/outcome rule this implements.
 *
 * Stable public API — [intentEvent] / [deadControlEvent] land in Firebase, so both MUST be
 * `snake_case`, non-empty, and never a Firebase-reserved name (`docs/TELEMETRY.md`).
 */
class DeadControlWatchdog(
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    private val scope: CoroutineScope,
    private val intentEvent: String,
    private val deadControlEvent: String,
) {
    private var pending: Job? = null

    /**
     * Call at the click site, BEFORE touching the control being instrumented. Emits
     * [intentEvent] immediately, then starts a [timeoutMs] watchdog for [deadControlEvent].
     * [contextProps] should include enough to act on the report without re-deriving it: an id for
     * the item/proof/task, plus whatever state existed at tap time (player state, armed/prepared,
     * pending count) — see call sites in `VerifyDetailScreen.kt` for the shape.
     */
    fun armIntent(contextProps: Map<String, String>, timeoutMs: Long) {
        pending?.cancel()
        runCatching { analytics.track(intentEvent, contextProps) }
        pending = scope.launch {
            delay(timeoutMs)
            runCatching { analytics.track(deadControlEvent, contextProps) }
            crashReporter.recordException(
                DeadControlException(deadControlEvent, contextProps, timeoutMs),
                "dead-control watchdog fired: $deadControlEvent",
            )
        }
    }

    /** Call the moment the real outcome callback lands — cancels the pending watchdog so a
     *  control that DID work never gets falsely reported as dead. */
    fun disarm() {
        pending?.cancel()
        pending = null
    }

    /** Call on dispose/screen-exit so a pending watchdog never fires against a torn-down screen
     *  (that would be a false positive, not a real dead control). */
    fun cancel() {
        pending?.cancel()
        pending = null
    }
}

/** Non-fatal payload for a fired [DeadControlWatchdog] — deliberately holds only the coarse,
 *  non-PII context props already sent as analytics params, never free-text user content. */
class DeadControlException(
    event: String,
    contextProps: Map<String, String>,
    timeoutMs: Long,
) : Exception(
    "dead control: $event did not resolve within ${timeoutMs}ms; context=$contextProps",
)
