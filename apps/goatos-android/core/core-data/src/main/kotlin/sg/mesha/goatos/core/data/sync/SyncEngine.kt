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
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.FeedDirectionCompleteRequestDto
import sg.mesha.goatos.core.network.dto.ProofReferenceDto
import sg.mesha.goatos.core.network.dto.ProofUploadResponseDto
import sg.mesha.goatos.core.network.isTerminalAppApiError
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicLong

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

    /**
     * Drains every currently-eligible outbox row, in bounded [DRAIN_BATCH_SIZE] batches so a
     * long offline period never materializes the whole table into memory at once.
     *
     * Returns `true` when the drain PASS completed — every eligible row was attempted. A per-row
     * transport failure does NOT flip this to `false`: [recordFailure] has already recorded that
     * row's next attempt, and the pass schedules the earliest retry once through [retryScheduler].
     * So the WorkManager shim reports `Result.success()` and MUST NOT also `Result.retry()`, or the
     * two mechanisms would double-schedule the same backed-off row (retry-churn).
     *
     * Returns `false` only when the pass was ABORTED before attempting anything — today just
     * "offline at pass start", where nothing was dispatched and nothing was scheduled. The caller
     * (`SyncWorker`) turns that into `Result.retry()` so WorkManager re-runs it under its CONNECTED
     * constraint. Backed-off rows are owned by the explicit retry work, never by a worker retry.
     */
    suspend fun drainOnce(): Boolean {
        if (!connectivityGate.isOnline()) return false // capture continues offline; sync just waits.
        val earliestRetryAt = AtomicLong(NO_RETRY_DUE)
        fun rememberRetryDue(epochMillis: Long) {
            while (true) {
                val current = earliestRetryAt.get()
                if (epochMillis >= current) return
                if (earliestRetryAt.compareAndSet(current, epochMillis)) return
            }
        }
        drainMutex.withLock {
            withContext(dispatchers.io) {
                // Recover rows stranded IN_FLIGHT by a prior crash/process-death mid-dispatch.
                // Safe here: the drain mutex guarantees no other pass is dispatching, so any
                // IN_FLIGHT row is orphaned, not actively in-flight. Without this they would be
                // excluded from eligibility forever (never retried, never dead-lettered).
                store.reclaimInFlight(clock())
                // Groups whose FIFO head failed this pass. Once a group's oldest in-flight write
                // fails it backs off, so eligibleForDrain would still return that group's NEWER
                // queued rows on the next batch fetch — dispatching them would post newer writes
                // ahead of the older failed one (breaking "ordered per shed"). So a failed group is
                // frozen for the REST of this pass; its rows drain on a later pass, oldest-first,
                // once the head is eligible again. Written from concurrent group coroutines, read
                // only after each supervisorScope barrier -> a concurrent set.
                val blockedGroups = ConcurrentHashMap.newKeySet<String>()
                while (true) {
                    val due = store.eligibleForDrain(clock(), DRAIN_BATCH_SIZE)
                    if (due.isEmpty()) break
                    val actionable = due.filterNot { it.groupKey in blockedGroups }
                    // Every remaining eligible row belongs to a group already frozen this pass —
                    // nothing left to do now (and re-fetching would spin on the same rows).
                    if (actionable.isEmpty()) break
                    val semaphore = Semaphore(maxConcurrentGroups.coerceAtLeast(1))
                    supervisorScope {
                        actionable.groupBy { it.groupKey }.values.forEach { groupItems ->
                            launch {
                                semaphore.withPermit {
                                    for (item in groupItems.sortedBy { it.createdAt }) {
                                        if (!processItem(item, ::rememberRetryDue)) {
                                            blockedGroups += item.groupKey
                                            break
                                        }
                                    }
                                }
                            }
                        }
                    }
                    // A short final batch means the eligible set is exhausted — no further rows a
                    // full batch left untouched, so re-querying would only re-fetch backed-off rows.
                    if (due.size < DRAIN_BATCH_SIZE) break
                }
            }
        }
        val retryAt = earliestRetryAt.get()
        if (retryAt != NO_RETRY_DUE) {
            retryScheduler.scheduleAt(retryAt)
        }
        // The pass ran to completion: every row eligible at pass start was attempted, and any
        // failures booked the earliest retry above. Only the offline early-return reports false.
        return true
    }

    /**
     * Returns `true` when this group may continue to its next row. A dispatch failure returns
     * `false` so same-shed FIFO stops at the first broken write instead of posting newer writes
     * over an older failed proof/submission.
     */
    private suspend fun processItem(item: OutboxEntity, rememberRetryDue: (Long) -> Unit): Boolean {
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
            recordFailure(item, error)?.let(rememberRetryDue)
            false
        }
    }

    private suspend fun recordFailure(item: OutboxEntity, error: Throwable): Long? {
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
        return if (applied && !terminal) {
            nextAttemptAt
        } else {
            null
        }
    }

    /** Calls the app-api for [item], reusing its stored idempotency key verbatim (never a new
     *  key on retry). Returns the raw JSON response on success, echoed back via
     *  [OutboxEntity.resultJson] so a later observer can decode the original server result
     *  without a second network call. */
    private suspend fun dispatch(item: OutboxEntity): String = when (OutboxOpType.valueOf(item.opType)) {
        OutboxOpType.SHED_SUBMIT -> dispatchShedSubmit(item)
        OutboxOpType.SCAN_CAPTURE -> dispatchScanCapture(item)
        OutboxOpType.SCAN_ATTEMPT -> dispatchScanAttempt(item)
        OutboxOpType.RESCHEDULE -> dispatchReschedule(item)
        OutboxOpType.PROOF_UPLOAD -> dispatchProofUpload(item)
        OutboxOpType.VERIFY_TASK -> dispatchVerifyTask(item)
        OutboxOpType.REWORK_TASK -> dispatchReworkTask(item)
        OutboxOpType.VERIFICATION_VERDICT -> dispatchVerificationVerdict(item)
        OutboxOpType.VERIFICATION_CLOSE -> dispatchVerificationClose(item)
        OutboxOpType.VERIFICATION_CLOSE_SUBMISSION -> dispatchVerificationSubmissionClose(item)
        OutboxOpType.VERIFICATION_CLOSE_BATCH -> dispatchVerificationBatchClose(item)
        OutboxOpType.COUNTS_SHIFTING -> dispatchCountsShifting(item)
        OutboxOpType.COUNTS_BIRTH -> dispatchCountsBirth(item)
        OutboxOpType.COUNTS_DEATH -> dispatchCountsDeath(item)
        OutboxOpType.COUNTS_APPROVAL_APPROVE -> dispatchCountsApprovalApprove(item)
        OutboxOpType.COUNTS_APPROVAL_REJECT -> dispatchCountsApprovalReject(item)
        OutboxOpType.SHIFTING_COMPLETE -> dispatchShiftingComplete(item)
        OutboxOpType.SHIFTING_CANCEL -> dispatchShiftingCancel(item)
        OutboxOpType.COUNTS_PROMOTE_IDENTIFIER -> dispatchPromoteIdentifier(item)
        OutboxOpType.FEED_DIRECTION_COMPLETE -> dispatchFeedDirectionComplete(item)
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

    private suspend fun dispatchScanCapture(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<ScanCapturePayload>(item.payloadJson)
        val response = api.recordScanCapture(payload.taskId, item.idempotencyKey, payload.request)
        return syncJson.encodeToString(response)
    }

    private suspend fun dispatchScanAttempt(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<ScanAttemptPayload>(item.payloadJson)
        val response = api.recordScanAttempt(payload.taskId, item.idempotencyKey, payload.request)
        return syncJson.encodeToString(response)
    }

    // Turns backend validation failures into operator-facing lines, stored as
    // OutboxEntity.lastError for the UI to render verbatim (see SubmitViewModel). The backend
    // owns field-level validation copy, so preserve its messages instead of replacing them with
    // a generic client sentence.
    private fun rejectionReason(report: sg.mesha.goatos.core.network.dto.ValidationReportDto): String {
        return report.errors.orEmpty()
            .mapNotNull { issue ->
                val message = issue.message.trim().ifBlank { issue.code.trim() }
                if (message.isBlank()) null else message
            }
            .distinct()
            .joinToString("\n")
            .ifBlank { "Server rejected the submission." }
    }

    private suspend fun dispatchReschedule(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<ReschedulePayload>(item.payloadJson)
        val response = api.rescheduleObligation(payload.obligationId, item.idempotencyKey, payload.request)
        return syncJson.encodeToString(response)
    }

    /**
     * The full signed-upload flow as ONE outbox dispatch (docs/mobile/proof-capture-sync-and-e2e.md
     * §3): register the proof's metadata (mints a signed URL), stream the captured video's bytes
     * to that URL, then call the completion endpoint. A row only reaches SYNCED here — i.e. once
     * this whole function returns — so "SYNCED" now means the video is actually durable
     * server-side, not just that metadata was registered (closes the boundary
     * `CaptureRepository`'s kdoc used to call out).
     *
     * Resumable / idempotent by construction: [processItem] retries this ENTIRE function on any
     * failure (same stored [OutboxEntity.idempotencyKey] every time, never a new one — same
     * contract as every other dispatch* here). A retry therefore always calls [AppApi.registerProof]
     * again FIRST, which — now that proof creation is idempotent server-side on that key
     * (backend/internal/proof/adapters/postgres — CreateProof ON CONFLICT) — returns the SAME
     * proof_id with a FRESH signed URL rather than a stale/expired one. [AppApi.uploadProofBlob]
     * treats a GCS 412 (the object a prior attempt already fully wrote) as success, not failure,
     * so a crash between a successful PUT and the completion call self-heals on the next attempt
     * instead of re-uploading the bytes.
     */
    private suspend fun dispatchProofUpload(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<ProofUploadPayload>(item.payloadJson)
        val registered = api.registerProof(item.idempotencyKey, payload.request)
        val proofId = registered.proof.proofId
        if (proofId.isBlank()) {
            throw NonRetryableSyncException("Proof registration did not return a proof id.")
        }
        val completed = api.uploadProofBlob(
            proofId = proofId,
            uploadUrl = registered.uploadUrl,
            uploadMethod = registered.uploadMethod,
            uploadHeaders = registered.headers,
            uploadProtocol = registered.uploadProtocol,
            chunkSizeBytes = registered.chunkSizeBytes,
            mimeType = payload.request.mimeType,
            filePath = payload.localFilePath,
            durationMs = payload.durationMs,
        )
        // Re-shaped into the SAME ProofUploadResponseDto/ProofReferenceDto envelope the metadata
        // registration step used to echo, so CaptureRepository's decodeServerProofId keeps
        // working unchanged — it only ever reads `.proof.proofId`.
        return syncJson.encodeToString(
            ProofUploadResponseDto(
                proof = ProofReferenceDto(
                    proofId = completed.proof.proofId.ifBlank { proofId },
                    proofType = completed.proof.proofType,
                    subjectType = completed.proof.subjectType,
                    subjectId = completed.proof.subjectId,
                    uploadState = completed.proof.uploadState,
                ),
            ),
        )
    }

    private suspend fun dispatchVerifyTask(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<VerifyTaskPayload>(item.payloadJson)
        val response = api.verifyAppTask(payload.taskId, item.idempotencyKey, payload.request)
        return syncJson.encodeToString(response)
    }

    private suspend fun dispatchReworkTask(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<ReworkTaskPayload>(item.payloadJson)
        val response = api.reworkAppTask(payload.taskId, item.idempotencyKey, payload.request)
        return syncJson.encodeToString(response)
    }

    /** The standalone Verifier section's approve/reject + reason verdict
     *  (context/architecture/verifier-app-and-flow.md). Same idempotent-replay contract as
     *  every other dispatch* here: the row's stored key is reused verbatim on every retry. */
    private suspend fun dispatchVerificationVerdict(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<VerificationVerdictPayload>(item.payloadJson)
        val response = api.submitVerificationVerdict(payload.itemId, item.idempotencyKey, payload.request)
        return syncJson.encodeToString(response)
    }

    private suspend fun dispatchVerificationClose(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<VerificationClosePayload>(item.payloadJson)
        val response = api.closeVerificationItem(payload.itemId, item.idempotencyKey, payload.request)
        return syncJson.encodeToString(response)
    }

    private suspend fun dispatchVerificationSubmissionClose(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<VerificationCloseSubmissionPayload>(item.payloadJson)
        val response = api.closeVerificationSubmission(payload.submissionId, item.idempotencyKey)
        return syncJson.encodeToString(response)
    }

    private suspend fun dispatchVerificationBatchClose(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<VerificationCloseBatchPayload>(item.payloadJson)
        val response = api.closeVaccinationBatch(payload.batchId, item.idempotencyKey)
        return syncJson.encodeToString(response)
    }

    /**
     * The three Counts writes. Same idempotent-replay contract as every other `dispatch*` here:
     * the row's STORED key is passed through verbatim as the `Idempotency-Key` header on every
     * attempt, never regenerated. That is what makes a server-committed-but-client-unrecorded
     * retry return the original result instead of recording a second movement / animal / death.
     *
     * A definitive server rejection (a 4xx validation failure — e.g. `dob` after `entry_date`, a
     * non-`dead`/`died` pairing, a stale `row_version`) is classified terminal by
     * [recordFailure]'s `isTerminalAppApiError` check, so it is surfaced to the operator for
     * correction rather than silently retried against an unchanged payload.
     */
    private suspend fun dispatchCountsShifting(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<CountsShiftingPayload>(item.payloadJson)
        val response = api.recordCountsShiftingEvent(item.idempotencyKey, payload.request)
        return syncJson.encodeToString(response)
    }

    private suspend fun dispatchCountsBirth(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<CountsBirthPayload>(item.payloadJson)
        val response = api.recordCountsBirthEvent(item.idempotencyKey, payload.request)
        return syncJson.encodeToString(response)
    }

    private suspend fun dispatchCountsDeath(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<CountsDeathPayload>(item.payloadJson)
        val response = api.recordCountsDeathEvent(item.idempotencyKey, payload.request)
        return syncJson.encodeToString(response)
    }

    /**
     * The two Counts APPROVAL decisions. Same idempotent-replay contract as every other
     * `dispatch*` here — the row's STORED key is passed through verbatim on every attempt — and it
     * matters more here than anywhere else: these calls APPLY the effect (a birth creates the kid,
     * a death exits the animal, a shifting relocates the named animals), so a replay under a fresh
     * key would apply it a second time. Under the stored key the backend returns the original
     * decision with `idempotent_replay=true` and applies nothing.
     *
     * Deciding a request that was already decided the OTHER way is a 409 — terminal by
     * [recordFailure]'s `isTerminalAppApiError` check, so it surfaces to the approver ("someone
     * else already decided this") instead of being retried against a state that will never change.
     */
    private suspend fun dispatchCountsApprovalApprove(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<CountsApprovalDecisionPayload>(item.payloadJson)
        val response = api.approveCountsApproval(payload.requestId, item.idempotencyKey, payload.request)
        return syncJson.encodeToString(response)
    }

    private suspend fun dispatchCountsApprovalReject(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<CountsApprovalDecisionPayload>(item.payloadJson)
        val response = api.rejectCountsApproval(payload.requestId, item.idempotencyKey, payload.request)
        return syncJson.encodeToString(response)
    }

    /**
     * The Shifting EXECUTION "Mark done". Same idempotent-replay contract as every other `dispatch*`
     * — the row's STORED key is passed through verbatim — and it matters as much here as for the
     * approval decisions: this call RELOCATES the animals, so a replay under a fresh key would move
     * them a second time. Under the stored key the backend returns the original relocation with
     * `idempotent_replay=true` and moves nobody again. Completing a movement that is no longer
     * authorized (already applied elsewhere, or cancelled) is a 400/409 — terminal by
     * [recordFailure]'s `isTerminalAppApiError` check, so it surfaces to the operator instead of
     * being retried against a state that will never change.
     */
    private suspend fun dispatchShiftingComplete(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<ShiftingCompletePayload>(item.payloadJson)
        val response = api.completeCountsShiftingEvent(
            payload.shiftingEventId,
            item.idempotencyKey,
            payload.destinationTag,
            // The MANDATORY video's proof_id (maintainer decision, 2026-07-26): resolved from the
            // PROOF_UPLOAD item enqueued on the same group, which drains first.
            resolveShiftingProofRef(payload),
        )
        return syncJson.encodeToString(response)
    }

    /**
     * Resolves the uploaded proof_id for a shifting completion from its coupled PROOF_UPLOAD outbox
     * row. Same-group ordering means that row has already drained to SUCCEEDED before this completion
     * runs; if it has not (a rare concurrency edge, or a pre-upgrade row with no coupling), the
     * completion is retried (plain exception -> non-conflict retry) until the video is uploaded. A
     * missing coupling or a permanently-failed upload is terminal — a shed move without a verifiable
     * video must not reach the backend.
     */
    private suspend fun resolveShiftingProofRef(payload: ShiftingCompletePayload): String {
        val proofItemId = payload.proofOutboxItemId
            ?: throw NonRetryableSyncException("Shifting completion is missing its mandatory video reference.")
        val proofRow = store.findById(proofItemId)
            ?: throw NonRetryableSyncException("The shifting video upload could not be found.")
        if (proofRow.status != OutboxStatus.SUCCEEDED.name) {
            throw IllegalStateException("Waiting for the shifting video to finish uploading before completing.")
        }
        val resultJson = proofRow.resultJson
            ?: throw IllegalStateException("The shifting video upload result is not yet available.")
        val proofId = syncJson.decodeFromString<ProofUploadResponseDto>(resultJson).proof.proofId
        if (proofId.isBlank()) {
            throw NonRetryableSyncException("The shifting video upload did not return a proof id.")
        }
        return proofId
    }

    private suspend fun dispatchFeedDirectionComplete(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<FeedDirectionCompletePayload>(item.payloadJson)
        val response = api.completeFeedDirectionSession(
            item.idempotencyKey,
            FeedDirectionCompleteRequestDto(
                parkId = payload.parkId,
                shedId = payload.shedId,
                sessionNo = payload.sessionNo,
                targetDate = payload.targetDate,
                workflow = payload.workflow,
            ),
        )
        return syncJson.encodeToString(response)
    }

    /**
     * Assigns a permanent RFID to a temporary-tagged goat, atomically retiring the temp. Drained
     * under the stored stable idempotency key: a server-committed-but-client-unrecorded retry returns
     * the original promotion (`idempotent_replay=true`) instead of retagging twice. A stale
     * row_version or a goat that no longer carries a temp tag is a 409 — terminal by
     * [recordFailure]'s check, so it surfaces to the operator instead of being retried forever.
     */
    private suspend fun dispatchPromoteIdentifier(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<PromoteIdentifierPayload>(item.payloadJson)
        val response = api.promoteCountsIdentifier(
            payload.goatId,
            item.idempotencyKey,
            payload.permanentIdentifier,
            payload.rowVersion,
            payload.animalIdentifier2,
        )
        return syncJson.encodeToString(response)
    }

    private suspend fun dispatchShiftingCancel(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<ShiftingCancelPayload>(item.payloadJson)
        val response = api.cancelCountsShiftingEvent(
            payload.shiftingEventId,
            item.idempotencyKey,
            payload.reason,
        )
        return syncJson.encodeToString(response)
    }

    private companion object {
        // Max rows pulled into memory per drain iteration. A long offline backlog drains in
        // successive batches of this size rather than one unbounded SELECT * materialization.
        const val DRAIN_BATCH_SIZE = 200
        const val NO_RETRY_DUE = Long.MAX_VALUE
    }
}
