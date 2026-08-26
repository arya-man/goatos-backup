package sg.mesha.goatos.core.common

/**
 * Lifecycle telemetry port for the durable write queue (the "outbox").
 *
 * ### Why this exists
 * The OkHttp telemetry seam only sees calls that REACHED the network. A queued write that is
 * never drained — the queue stalls, the head of a group keeps failing, a row exhausts its
 * attempts — never produces an HTTP call, so it produced no signal at all: an operator's proof
 * upload could retry for minutes while `adb logcat` stayed completely silent about it, and the
 * only way to learn what happened was to read the server log and query Postgres.
 *
 * This is that missing half. It is a PORT (framework-free, no Android, no vendor SDK) so the
 * drain engine can emit every transition without depending on the analytics module; the
 * reporting decorator lives in `:core:core-analytics`, mirroring exactly how
 * `NetworkTelemetryReporter` (in `:core:core-network`) is implemented by
 * `FailureReportingNetworkTelemetryReporter`.
 *
 * ### One seam, not N call sites
 * Emission happens at the two places every queued write already passes through — the enqueue
 * function in `SyncRepository` and the per-item dispatch in `SyncEngine`. No feature screen and
 * no `enqueue*` overload has to remember anything.
 *
 * ### What never crosses this seam
 * Operation TYPE, row id, attempt counters, and a failure CLASS name only. Never the payload,
 * never the server's error copy, never a token/Authorization header/FCM token. Goat RFIDs and
 * tags would be permissible (livestock data, not PII) but are simply not carried here — they
 * live in the payload, which never crosses.
 *
 * Stable public API grepped by the telemetry CI guardrail (see `docs/TELEMETRY.md`) — do not
 * rename without updating that doc and the guard.
 */
fun interface OutboxTelemetryReporter {
    fun onOutboxWrite(event: OutboxTelemetryEvent)

    companion object {
        /** Fallback for tests/local — never throws, never reports. */
        val Noop = OutboxTelemetryReporter { }
    }
}

/** Where in its life a queued write currently is. */
enum class OutboxWritePhase {
    /** Durably queued. From here on the write survives process death; it has NOT been sent. */
    ENQUEUED,

    /** Claimed by a drain pass and about to be dispatched. */
    ATTEMPT_STARTED,

    /** This attempt did not go through. Retryable unless followed by [TERMINAL]. */
    ATTEMPT_FAILED,

    /** Another attempt is booked; [OutboxTelemetryEvent.retryInMs] says how far out. */
    RETRY_SCHEDULED,

    /**
     * This row is ready, but one of its referenced proof-upload rows has not succeeded yet.
     * The row is rescheduled without consuming its retry budget.
     */
    DEPENDENCY_WAIT,

    /**
     * The write is DEAD: it will never be sent again without an operator/manual retry.
     * The single most important phase here — before this existed, permanently-undelivered
     * data was indistinguishable on-device from data still in flight.
     */
    TERMINAL,
}

/** Why a write reached [OutboxWritePhase.TERMINAL]. */
object OutboxTerminalReason {
    /** A definitive server refusal (validation / non-retryable 4xx) — resending is pointless. */
    const val CONFLICT = "conflict"

    /** Every allowed attempt was spent and the write still never went through. */
    const val ATTEMPTS_EXHAUSTED = "attempts_exhausted"
}

/**
 * One transition of one queued write.
 *
 * @param itemId the outbox row id (a locally generated UUID — not livestock or user data).
 * @param groupKey the durable sync lane key for diagnosing local ordering/dependency waits.
 * @param idempotencyKey the deterministic write key used for server-side replay safety.
 * @param referencedProofOutboxItemId the proof upload row this write is waiting on, when known.
 * @param failureClass the exception's SIMPLE CLASS NAME (`IOException`, `HttpException`, …) —
 *   deliberately not its message, which can carry arbitrary server copy.
 * @param terminalReason one of [OutboxTerminalReason], set only for [OutboxWritePhase.TERMINAL].
 * @param retryInMs how far out the next attempt is, set only for [OutboxWritePhase.RETRY_SCHEDULED].
 */
data class OutboxTelemetryEvent(
    val phase: OutboxWritePhase,
    val opType: String,
    val itemId: String,
    val groupKey: String = "",
    val idempotencyKey: String = "",
    val referencedProofOutboxItemId: String = "",
    val attempt: Int = 0,
    val maxAttempts: Int = 0,
    val failureClass: String? = null,
    val terminalReason: String? = null,
    val retryInMs: Long? = null,
)
