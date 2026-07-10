package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import kotlinx.coroutines.supervisorScope
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.Semaphore
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.sync.withPermit
import kotlinx.coroutines.withContext
import sg.mesha.goatos.core.common.DefaultDispatchers
import sg.mesha.goatos.core.common.DispatcherProvider
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.isTerminalAppApiError

fun interface SyncRetryScheduler {
    fun scheduleAt(epochMillis: Long)

    companion object {
        val Noop = SyncRetryScheduler { }
    }
}

/**
 * A definitive, non-retryable server rejection (e.g. a failed submission validation).
 * Retrying with the SAME payload would only reproduce the same rejection, so [SyncEngine]
 * terminalizes the row immediately (marks it `conflict`) instead of burning the backoff
 * budget on a rejection that will never change without a new/edited payload.
 */
class NonRetryableSyncException(message: String) : Exception(message)

/**
 * Drains the outbox oldest-first (per group) and performs the real app-api call per queued
 * item — the pure, framework-agnostic "do the work" body a `CoroutineWorker.doWork()` would
 * delegate to.
 *
 * ### Triggers (TRD `docs/mobile/trd-operator-mobile.md` §6)
 * [SyncEngine] is WorkManager-agnostic — it is just the framework-free "do the work" body.
 * Three things call [drainOnce], all on a Hilt application-scoped `CoroutineScope` (never
 * `GlobalScope` — see AppModule's `provideAppScope`):
 *  - [SyncRepository] on every enqueue (optimistic write, drain immediately);
 *  - [ConnectivitySyncTrigger] on reconnect (WHILE the process is alive);
 *  - `app`'s `SyncWorker : CoroutineWorker` (`@HiltWorker`), a periodic CONNECTED-constrained
 *    WorkManager job — the OS-scheduled backstop that survives PROCESS DEATH. `doWork()` is a
 *    ~10-line shim that just calls [drainOnce]; no drain logic lives there.
 *
 * Nothing is ever lost regardless of trigger: every write is durable in Room, and each drain
 * pass first [OutboxStore.reclaimInFlight]s rows stranded IN_FLIGHT by a crash/process-death
 * mid-dispatch, so an interrupted submit is always retried on the next pass (relaunch,
 * reconnect, or the WorkManager backstop).
 *
 * ### Ordering & concurrency
 * Items are grouped by [OutboxEntity.groupKey] (e.g. a shed id) and each group drains
 * strictly in `createdAt` order — never reordered (TRD: outbox is "ordered per shed").
 * Different groups may drain concurrently, bounded by [maxConcurrentGroups] permits (no
 * unbounded coroutines).
 */
class SyncEngine(
    private val store: OutboxStore,
    private val api: AppApi,
    private val connectivityGate: ConnectivityGate,
    private val dispatchers: DispatcherProvider = DefaultDispatchers,
    private val clock: () -> Long = System::currentTimeMillis,
    private val backoff: BackoffPolicy = BackoffPolicy.Default,
    private val maxConcurrentGroups: Int = 3,
    private val retryScheduler: SyncRetryScheduler = SyncRetryScheduler.Noop,
) {
    // The WorkManager-equivalent of "enqueue as unique work": never run two overlapping
    // drain passes. A trigger that arrives mid-drain simply waits its turn, then re-reads
    // eligibility fresh (so a just-enqueued item is never missed).
    private val drainMutex = Mutex()

    suspend fun drainOnce() {
        if (!connectivityGate.isOnline()) return // capture continues offline; sync just waits.
        drainMutex.withLock {
            withContext(dispatchers.io) {
                // Recover rows stranded IN_FLIGHT by a prior crash/process-death mid-dispatch.
                // Safe here: the drain mutex guarantees no other pass is dispatching, so any
                // IN_FLIGHT row is orphaned, not actively in-flight. Without this they would be
                // excluded from eligibility forever (never retried, never dead-lettered).
                store.reclaimInFlight(clock())
                val due = store.eligibleForDrain(clock())
                if (due.isEmpty()) return@withContext
                val semaphore = Semaphore(maxConcurrentGroups.coerceAtLeast(1))
                supervisorScope {
                    due.groupBy { it.groupKey }.values.forEach { groupItems ->
                        launch {
                            semaphore.withPermit {
                                for (item in groupItems.sortedBy { it.createdAt }) {
                                    if (!processItem(item)) break
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    /**
     * Returns `true` when this group may continue to its next row. A dispatch failure returns
     * `false` so same-shed FIFO stops at the first broken write instead of posting newer writes
     * over an older failed proof/submission.
     */
    private suspend fun processItem(item: OutboxEntity): Boolean {
        // Guard the transition: if the row is no longer QUEUED/FAILED (e.g. a manual retry or a
        // concurrent pass already claimed it) markInFlight is a no-op and we skip it — never
        // dispatch a row we didn't actually transition.
        if (!store.markInFlight(item.id, clock())) return true
        return try {
            val resultJson = dispatch(item)
            store.markSucceeded(item.id, resultJson, clock())
            true
        } catch (cancellation: CancellationException) {
            throw cancellation
        } catch (error: Throwable) {
            recordFailure(item, error)
            false
        }
    }

    private suspend fun recordFailure(item: OutboxEntity, error: Throwable) {
        val attempt = item.attemptCount + 1
        // Terminal = a definitive server rejection (validation) OR a non-retryable 4xx: neither
        // changes by re-sending the same payload, so don't burn the backoff budget on it.
        val conflict = error is NonRetryableSyncException || error.isTerminalAppApiError()
        val terminal = conflict || attempt >= item.maxAttempts
        val nextAttemptAt = if (terminal) Long.MAX_VALUE else clock() + backoff.delayMillis(attempt)
        val applied = store.markFailed(
            id = item.id,
            attemptCount = attempt,
            nextAttemptAt = nextAttemptAt,
            conflict = conflict,
            lastError = error.message ?: (error::class.simpleName ?: "sync_failed"),
            now = clock(),
        )
        if (applied && !terminal) {
            retryScheduler.scheduleAt(nextAttemptAt)
        }
    }

    /** Calls the app-api for [item], reusing its stored idempotency key verbatim (never a new
     *  key on retry). Returns the raw JSON response on success, echoed back via
     *  [OutboxEntity.resultJson] so a later observer can decode the original server result
     *  without a second network call. */
    private suspend fun dispatch(item: OutboxEntity): String = when (OutboxOpType.valueOf(item.opType)) {
        OutboxOpType.SHED_SUBMIT -> dispatchShedSubmit(item)
        OutboxOpType.RESCHEDULE -> dispatchReschedule(item)
        OutboxOpType.PROOF_UPLOAD -> dispatchProofUpload(item)
    }

    private suspend fun dispatchShedSubmit(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<ShedSubmitPayload>(item.payloadJson)
        // Reuse the row's stable key verbatim (never a new key on retry) so the backend dedupes a
        // server-committed-but-client-unrecorded replay instead of creating a duplicate submission.
        val response = api.submitAppTask(payload.taskId, item.idempotencyKey, payload.request)
        val report = response.submission.validationReport
        if (!report.valid) {
            throw NonRetryableSyncException(rejectionReason(report))
        }
        return syncJson.encodeToString(response)
    }

    // Turns a backend validation failure into an honest operator-facing line, stored as
    // OutboxEntity.lastError for the UI to render verbatim (see SubmitViewModel). When the
    // ONLY errors are missing required answers/proof, the real blocker is that this build has
    // no recording-form capture yet (form DSL runner + camera→proof upload land later) — say
    // that plainly instead of leaking a raw field-error code. Any other rejection is shown
    // verbatim. (Ported from the previous synchronous SubmitViewModel.rejectionReason, moved
    // here because the full ValidationReportDto is only available at the point of failure.)
    private fun rejectionReason(report: sg.mesha.goatos.core.network.dto.ValidationReportDto): String {
        val codes = report.errors.map { it.code }
        val onlyFormGaps = codes.isNotEmpty() && codes.all {
            it == "required" || it == "proof_required" || it == "proof_subject_required"
        }
        return if (onlyFormGaps) {
            "This drive needs the recording form before it can be submitted — form capture lands in a later build."
        } else {
            report.errors.firstOrNull()?.message ?: "Server rejected the submission."
        }
    }

    private suspend fun dispatchReschedule(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<ReschedulePayload>(item.payloadJson)
        val response = api.rescheduleObligation(payload.obligationId, item.idempotencyKey, payload.request)
        return syncJson.encodeToString(response)
    }

    private suspend fun dispatchProofUpload(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<ProofUploadPayload>(item.payloadJson)
        val response = api.registerProof(item.idempotencyKey, payload.request)
        return syncJson.encodeToString(response)
    }
}
