package sg.mesha.goatos.core.data.sync

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
 * ### On WorkManager (read before assuming this should use it)
 * The TRD (`docs/mobile/trd-operator-mobile.md` §6) specifies WorkManager for the sync
 * worker. This build does NOT wire WorkManager: `androidx.work:work-runtime-ktx` (and
 * `androidx.hilt:hilt-work` for a `@HiltWorker`) have **no version alias** in
 * `gradle/libs.versions.toml`, and this task was explicitly instructed to STOP and report a
 * missing dependency alias rather than edit that file. [SyncEngine] is therefore built
 * WorkManager-agnostic on purpose: the day the alias is added, a
 * `SyncWorker : CoroutineWorker` can be a ~10-line shim whose `doWork()` calls [drainOnce] —
 * no logic moves. Until then, [drainOnce] is triggered by [SyncRepository] on every enqueue
 * and by [ConnectivitySyncTrigger] on reconnect, run on a Hilt-provided application-scoped
 * `CoroutineScope` (never `GlobalScope` — see AppModule's `provideAppScope`).
 *
 * The one real behavioral gap versus WorkManager: if the process is fully killed, drain does
 * not resume until the app is next opened (no persistent OS-level job survives process
 * death). No data is lost — everything durable is already in Room — sync is just not
 * *actively* retried while the process is dead. `drainOnce` is also called from
 * `Application.onCreate` indirectly (via the connectivity trigger's initial state) so a
 * relaunch drains promptly.
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
) {
    // The WorkManager-equivalent of "enqueue as unique work": never run two overlapping
    // drain passes. A trigger that arrives mid-drain simply waits its turn, then re-reads
    // eligibility fresh (so a just-enqueued item is never missed).
    private val drainMutex = Mutex()

    suspend fun drainOnce() {
        if (!connectivityGate.isOnline()) return // capture continues offline; sync just waits.
        drainMutex.withLock {
            withContext(dispatchers.io) {
                val due = store.eligibleForDrain(clock())
                if (due.isEmpty()) return@withContext
                val semaphore = Semaphore(maxConcurrentGroups.coerceAtLeast(1))
                supervisorScope {
                    due.groupBy { it.groupKey }.values.forEach { groupItems ->
                        launch {
                            semaphore.withPermit {
                                groupItems.sortedBy { it.createdAt }.forEach { processItem(it) }
                            }
                        }
                    }
                }
            }
        }
    }

    private suspend fun processItem(item: OutboxEntity) {
        store.markInFlight(item.id, clock())
        runCatching { dispatch(item) }
            .onSuccess { resultJson -> store.markSucceeded(item.id, resultJson, clock()) }
            .onFailure { error -> recordFailure(item, error) }
    }

    private suspend fun recordFailure(item: OutboxEntity, error: Throwable) {
        val attempt = item.attemptCount + 1
        val conflict = error is NonRetryableSyncException
        val terminal = conflict || attempt >= item.maxAttempts
        val nextAttemptAt = if (terminal) Long.MAX_VALUE else clock() + backoff.delayMillis(attempt)
        store.markFailed(
            id = item.id,
            attemptCount = attempt,
            nextAttemptAt = nextAttemptAt,
            conflict = conflict,
            lastError = error.message ?: (error::class.simpleName ?: "sync_failed"),
            now = clock(),
        )
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
        val response = api.submitAppTask(payload.taskId, payload.request)
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
