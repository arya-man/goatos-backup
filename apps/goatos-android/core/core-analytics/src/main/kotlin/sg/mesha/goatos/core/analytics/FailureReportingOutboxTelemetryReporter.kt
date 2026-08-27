package sg.mesha.goatos.core.analytics

import android.util.Log
import sg.mesha.goatos.core.common.OutboxTelemetryEvent
import sg.mesha.goatos.core.common.OutboxTelemetryReporter
import sg.mesha.goatos.core.common.OutboxTerminalReason
import sg.mesha.goatos.core.common.OutboxWritePhase

/**
 * Turns the durable write queue's LIFECYCLE into signal, at the one seam every queued write
 * already passes through.
 *
 * The sibling of [FailureReportingNetworkTelemetryReporter], covering the half that one
 * explicitly cannot: a write that never reaches the network. The night an operator's proof
 * upload retried for minutes, the screen said "uploading" and 6000 lines of `adb logcat`
 * contained ZERO lines about the upload, the retries, or the failure — because the only
 * telemetry that existed sat in an OkHttp interceptor, and a write stuck behind a stalled queue
 * head never gets that far. Diagnosing it took a server log dump and a Postgres query, for a
 * problem the phone knew about the entire time.
 *
 * Outputs, by phase:
 *
 * | phase             | logcat | breadcrumb | analytics event               | non-fatal |
 * |-------------------|:------:|:----------:|-------------------------------|:---------:|
 * | `ENQUEUED`        |   yes  |    yes     | —                             |     —     |
 * | `ATTEMPT_STARTED` |   yes  |    yes     | —                             |     —     |
 * | `ATTEMPT_FAILED`  |   yes  |    yes     | `sync_write_attempt_failed`   |     —     |
 * | `RETRY_SCHEDULED` |   yes  |    yes     | —                             |     —     |
 * | `DEPENDENCY_WAIT` |   yes  |    yes     | `sync_write_dependency_wait`  |     —     |
 * | `TERMINAL`        |   yes  |    yes     | `sync_write_dead`             | scoped    |
 *
 * Non-terminal phases stay logcat + breadcrumb deliberately: they are what makes a stalled queue
 * READABLE live on a phone (`adb logcat -s GoatOsOutbox` shows enqueue → attempt → retry, over
 * and over, which IS the diagnosis), and they attach that same history to whatever Crashlytics
 * report lands next — but they are not worth an analytics event per attempt.
 *
 * `TERMINAL` also emits `sync_write_dead`, because it is the one phase describing data that will
 * never be sent. Expected transport failures stay out of Crashlytics; unexpected client defects
 * still get one throttled non-fatal so they remain triageable.
 *
 * ### What never crosses this seam
 * Operation type, row id, attempt counters, a failure CLASS name and a terminal reason. Never a
 * payload, never the server's error copy, never an Authorization header or FCM token. Nothing
 * here is user-visible copy — this is diagnostics, and the operator's screen keeps speaking farm
 * language.
 *
 * Stable public API grepped by the telemetry CI guardrail (see `docs/TELEMETRY.md`) — do not
 * rename without updating that doc and the guard.
 */
class FailureReportingOutboxTelemetryReporter(
    private val crashReporter: CrashReporter,
    private val analytics: AnalyticsPort,
    private val nowMs: () -> Long = { System.currentTimeMillis() },
    private val logLine: (String) -> Unit = { Log.w(TAG, it) },
) : OutboxTelemetryReporter {

    private val lastNonFatalMs = mutableMapOf<String, Long>() // mobile-guard:ignore: keyed by (op type, terminal reason, failure class) — bounded by enum cardinality, not traffic

    override fun onOutboxWrite(event: OutboxTelemetryEvent) {
        runCatching { report(event) }
    }

    private fun report(event: OutboxTelemetryEvent) {
        val summary = summarize(event)
        logLine(summary)
        crashReporter.log(summary)
        when (event.phase) {
            OutboxWritePhase.ATTEMPT_FAILED -> analytics.track(
                AnalyticsEvents.SYNC_WRITE_ATTEMPT_FAILED,
                baseParams(event),
            )
            OutboxWritePhase.TERMINAL -> {
                analytics.track(
                    AnalyticsEvents.SYNC_WRITE_DEAD,
                    baseParams(event) + mapOf(
                        AnalyticsEvents.Params.REASON to event.terminalReason.orEmpty(),
                    ),
                )
                if (shouldRecordNonFatal(event)) {
                    crashReporter.recordException(DeadQueuedWrite(summary), summary)
                }
            }
            OutboxWritePhase.DEPENDENCY_WAIT -> analytics.track(
                AnalyticsEvents.SYNC_WRITE_DEPENDENCY_WAIT,
                baseParams(event),
            )
            OutboxWritePhase.ENQUEUED,
            OutboxWritePhase.ATTEMPT_STARTED,
            OutboxWritePhase.RETRY_SCHEDULED,
            -> Unit
        }
    }

    private fun baseParams(event: OutboxTelemetryEvent): Map<String, String> = mapOf(
        AnalyticsEvents.Params.OP_TYPE to event.opType,
        AnalyticsEvents.Params.OUTBOX_ITEM_ID to event.itemId,
        AnalyticsEvents.Params.GROUP_KEY to event.groupKey,
        AnalyticsEvents.Params.IDEMPOTENCY_KEY to event.idempotencyKey,
        AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID to event.referencedProofOutboxItemId,
        AnalyticsEvents.Params.ATTEMPT to event.attempt.toString(),
        AnalyticsEvents.Params.MAX_ATTEMPTS to event.maxAttempts.toString(),
        AnalyticsEvents.Params.REASON to event.failureClass.orEmpty(),
    )

    private fun summarize(event: OutboxTelemetryEvent): String = buildString {
        append("outbox_write ")
        append(event.phase.name.lowercase())
        append(" op=").append(event.opType)
        append(" item=").append(event.itemId)
        event.groupKey.takeIf { it.isNotBlank() }?.let { append(" group=").append(it) }
        event.idempotencyKey.takeIf { it.isNotBlank() }?.let { append(" idempotency=").append(it) }
        event.referencedProofOutboxItemId.takeIf { it.isNotBlank() }?.let { append(" proof_outbox=").append(it) }
        append(" attempt=").append(event.attempt).append('/').append(event.maxAttempts)
        event.failureClass?.let { append(" failure=").append(it) }
        event.terminalReason?.let { append(" terminal_reason=").append(it) }
        event.retryInMs?.let { append(" retry_in_ms=").append(it) }
    }

    private fun shouldRecordNonFatal(event: OutboxTelemetryEvent): Boolean {
        if (event.failureClass in EXPECTED_TRANSPORT_FAILURES) return false
        val key = "${event.opType} ${event.terminalReason} ${event.failureClass}"
        val now = nowMs()
        val previous = lastNonFatalMs[key]
        if (previous != null && now - previous < NON_FATAL_THROTTLE_MS) return false
        lastNonFatalMs[key] = now
        return true
    }

    class DeadQueuedWrite(message: String) : RuntimeException(message)

    companion object {
        /** Filter with `adb logcat -s GoatOsOutbox` to watch the queue live on a device. */
        private const val TAG = "GoatOsOutbox"

        const val NON_FATAL_THROTTLE_MS: Long = 60_000

        private val EXPECTED_TRANSPORT_FAILURES = setOf(
            "ConnectException",
            "HttpException",
            "IOException",
            "NoRouteToHostException",
            "SocketException",
            "SocketTimeoutException",
            "UnknownHostException",
        )
    }
}
