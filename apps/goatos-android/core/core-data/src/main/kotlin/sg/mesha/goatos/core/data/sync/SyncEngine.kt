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
import sg.mesha.goatos.core.common.OutboxTelemetryEvent
import sg.mesha.goatos.core.common.OutboxTelemetryReporter
import sg.mesha.goatos.core.common.OutboxTerminalReason
import sg.mesha.goatos.core.common.OutboxWritePhase
import sg.mesha.goatos.core.database.capture.CaptureSyncStatus
import sg.mesha.goatos.core.database.capture.ScannedGoatDao
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.appApiStatusCode
import sg.mesha.goatos.core.network.dto.FeedDirectionCompleteRequestDto
import sg.mesha.goatos.core.network.dto.FeedDistributionCompleteRequestDto
import sg.mesha.goatos.core.network.dto.FeedTransportSubmitRequestDto
import sg.mesha.goatos.core.network.dto.FeedPackingCompleteRequestDto
import sg.mesha.goatos.core.network.dto.FeedWastageCompleteRequestDto
import sg.mesha.goatos.core.data.cache.HealthDiagnosisRunDao
import sg.mesha.goatos.core.data.cache.HealthDiagnosisRunEntity
import sg.mesha.goatos.core.network.dto.ConfirmHealthDiagnosisResponseDto
import sg.mesha.goatos.core.network.dto.HealthCompleteRequestDto
import sg.mesha.goatos.core.network.dto.HealthDiagnosisProposalResponseDto
import sg.mesha.goatos.core.network.dto.MilkPreparationProofsDto
import sg.mesha.goatos.core.network.dto.MilkPreparationAnswersDto
import sg.mesha.goatos.core.network.dto.MilkPreparationSubmissionRequestDto
import sg.mesha.goatos.core.network.dto.MilkFeedingProofsDto
import sg.mesha.goatos.core.network.dto.MilkFeedingSubmitRequestDto
import sg.mesha.goatos.core.network.dto.ProofReferenceDto
import sg.mesha.goatos.core.network.dto.ProofUploadResponseDto
import sg.mesha.goatos.core.network.dto.WorkflowActionAnswerRequestDto
import sg.mesha.goatos.core.network.dto.WorkflowActionCompleteRequestDto
import sg.mesha.goatos.core.network.isTerminalAppApiError
import sg.mesha.goatos.core.network.serverErrorText
import sg.mesha.goatos.core.data.weighing.WeighingObservationDao
import sg.mesha.goatos.core.data.weighing.WeighingShedObservationDao
import sg.mesha.goatos.core.data.weighing.WeighingTransitionEpochDao
import sg.mesha.goatos.core.data.weighing.WeighingTransitionEpochEntity
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicLong

fun interface SyncRetryScheduler {
    fun scheduleAt(epochMillis: Long)

    companion object {
        val Noop = SyncRetryScheduler { }
    }
}

/**
 * A generic, sync-engine-driven "the local cache is a whole-page KV blob, not a Room row keyed by
 * server id" reconcile hook: for opTypes whose observed screen state is a [Resource]-wrapped page
 * blob (e.g. Milk Feeding/Preparation's `CountsBreakdownMetaCacheDao` page cache — see
 * [sg.mesha.goatos.core.data.MilkFeedingRepository] / [sg.mesha.goatos.core.data.MilkPreparationRepository]),
 * there is no server-truth row to write directly into. The correct reconcile is instead "go fetch
 * the page again" — a plain `repo.refresh(...)`. Registered per-[OutboxOpType] at the repo/DI layer
 * ([sg.mesha.goatos.di.AppModule]), never in a ViewModel: the moment matching this reconcile's
 * timing (right after a row reaches SUCCEEDED, alongside every other [reconcileFeatureSuccess] arm)
 * belongs to [SyncEngine], not to whichever screen happens to be on-screen when it happens.
 *
 * Failures are swallowed the same way [reportCacheReconcileFailure] swallows every other
 * post-success cache-write failure here: the outbox row is already SUCCEEDED on the server, so a
 * refresh miss must never re-mark it failed or abort the rest of the drain pass. The next
 * successful list/detail fetch repairs the cache regardless (pull-to-refresh, next screen visit).
 */
fun interface PostSuccessRefreshHook {
    /** [payloadJson] is the SAME [OutboxEntity.payloadJson] this opType was dispatched with —
     *  the hook decodes whatever payload shape it needs to derive the page key(s) to refresh. */
    suspend fun onSuccess(payloadJson: String)
}

/** Repairs feature cache state after a definitive outbox failure. The hook is replayed from the
 * durable terminal row after process death, so an optimistic cache mutation cannot survive a
 * server rejection merely because the app died between terminalization and local rollback. */
fun interface PostTerminalFailureHook {
    suspend fun onTerminalFailure(payloadJson: String)
}

/**
 * A definitive, non-retryable server rejection (e.g. a failed submission validation).
 * Retrying with the SAME payload would only reproduce the same rejection, so [SyncEngine]
 * terminalizes the row immediately (marks it `conflict`) instead of burning the backoff
 * budget on a rejection that will never change without a new/edited payload.
 */
class NonRetryableSyncException(message: String) : Exception(message)

private class ProofDependencyPendingException(message: String) : Exception(message)

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
    private val scannedGoatDao: ScannedGoatDao? = null,
    private val weighingObservationDao: WeighingObservationDao? = null,
    private val weighingShedObservationDao: WeighingShedObservationDao? = null,
    private val healthDiagnosisRunDao: HealthDiagnosisRunDao? = null,
    // Advances the SAME transition-epoch mechanism `WeighingRepository.transitionIdempotencyKey`
    // reads, ONLY after a WEIGHING_SCOPE_SUBMIT row reaches SUCCEEDED here — mirroring the
    // repository's own "called only after the server confirmed" contract for reopen/close, which
    // stayed direct-HTTP and unmoved (see docs/decisions/weighing-rework-task-cards.md; the close
    // gate itself is unconditional and out of scope for this change).
    private val weighingTransitionEpochDao: WeighingTransitionEpochDao? = null,
    private val feedRepository: sg.mesha.goatos.core.data.FeedRepository? = null,
    private val feedTransportRepository: sg.mesha.goatos.core.data.FeedTransportRepository? = null,
    // PC Care (module pc_care): the durable scanned-animal rows this engine reconciles directly
    // (scan SYNCED/DUPLICATE/FAILED, and the server row id a slot registration dispatch resolves),
    // and the repository whose task caches a successful submit reconciles.
    private val pcCareAnimalRowDao: sg.mesha.goatos.core.data.cache.PcCareAnimalRowDao? = null,
    private val pcCareRepository: sg.mesha.goatos.core.data.PcCareRepository? = null,
    // Toxin (module toxin): every step-complete/submit dispatch RETURNS the task's fresh detail
    // (server-composed step states); this repository writes it through Room so the guided screen
    // re-renders server truth the moment the write drains — and refreshes it after a terminal
    // wait_not_elapsed / step_already_done refusal so the screen shows why.
    private val toxinRepository: sg.mesha.goatos.core.data.ToxinRepository? = null,
    private val idGenerator: () -> String = { java.util.UUID.randomUUID().toString() },
    /**
     * Lifecycle visibility for the queue itself. Defaults to
     * [OutboxTelemetryReporter.Noop] so every existing test/fake construction keeps compiling;
     * production wiring binds the reporting decorator in `:core:core-analytics`.
     *
     * Emitted HERE — the single place every queued write is claimed, attempted, backed off and
     * terminalized — rather than at the ~30 `dispatch*` bodies or the ~30 `enqueue*` overloads,
     * for the same reason the HTTP failure reporter lives in the interceptor: a per-call-site
     * emit can be forgotten by the next feature someone writes; a seam cannot.
     */
    private val telemetry: OutboxTelemetryReporter = OutboxTelemetryReporter.Noop,
    /**
     * Per-[OutboxOpType] whole-page-blob reconcile hooks — see [PostSuccessRefreshHook] kdoc.
     * Empty by default so every existing test/fake construction keeps compiling; production
     * wiring registers the Milk Feeding/Preparation refresh callbacks in `AppModule`.
     */
    private val postSuccessRefreshHooks: Map<OutboxOpType, PostSuccessRefreshHook> = emptyMap(),
    /** Cache handoffs that must finish before the active outbox overlay retracts. The same hook
     * remains registered in [postSuccessRefreshHooks] for durable restart replay. */
    private val preSuccessRefreshHooks: Map<OutboxOpType, PostSuccessRefreshHook> = emptyMap(),
    private val postTerminalFailureHooks: Map<OutboxOpType, PostTerminalFailureHook> = emptyMap(),
) {
    // The WorkManager-equivalent of "enqueue as unique work": never run two overlapping
    // drain passes. A trigger that arrives mid-drain simply waits its turn, then re-reads
    // eligibility fresh (so a just-enqueued item is never missed).
    private val drainMutex = Mutex()

    suspend fun deleteProof(proofId: String) = withContext(dispatchers.io) {
        api.deleteProof(proofId)
    }

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
        // Repair already-accepted feature state even while offline. A process can die after
        // markSucceeded and before Room reconciliation; replaying that durable response is local.
        withContext(dispatchers.io) {
            store.observeRecentTerminals(SUCCESS_RECONCILE_LIMIT).forEach { terminal ->
                runCatching {
                    when {
                        terminal.status == OutboxStatus.SUCCEEDED.name -> reconcileFeatureSuccess(terminal)
                        terminal.status == OutboxStatus.FAILED.name &&
                            (terminal.conflict || terminal.attemptCount >= terminal.maxAttempts) ->
                            reconcileFeatureTerminalFailure(terminal)
                    }
                }.onFailure { reportCacheReconcileFailure(terminal, it) }
                }
            }
        if (!connectivityGate.isOnline()) return false
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
                // Rebuild feature acceptance after a process dies between marking the outbox
                // success and updating the feature database. This projection is idempotent.
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
                                    // NOT re-sorted here. The store returns rows in drain
                                    // order (createdAt, then rowid), and re-sorting on
                                    // createdAt alone threw that away: two rows from the same
                                    // millisecond came back in an order the sort did not fix,
                                    // so a Submit could still be handed to the server before a
                                    // scan it must wait for. groupBy preserves encounter order.
                                    for (item in groupItems) {
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
        report(
            OutboxTelemetryEvent(
                phase = OutboxWritePhase.ATTEMPT_STARTED,
                opType = item.opType,
                itemId = item.id,
                groupKey = item.groupKey,
                idempotencyKey = item.idempotencyKey,
                referencedProofOutboxItemId = item.referencedProofOutboxItemId(),
                attempt = item.attemptCount + 1,
                maxAttempts = item.maxAttempts,
            ),
        )
        return try {
            val resultJson = dispatch(item)
            val preSuccessRefreshApplied = reconcileFeatureBeforeSuccess(item)
            if (store.markSucceeded(item.id, resultJson, clock())) {
                // Reconcile with the response from this successful dispatch. The original
                // in-memory item predates markSucceeded and therefore has resultJson=null.
                reconcileFeatureSuccess(
                    item.copy(resultJson = resultJson),
                    skipPostSuccessRefresh = preSuccessRefreshApplied,
                )
            }
            true
        } catch (cancellation: CancellationException) {
            throw cancellation
        } catch (pendingProof: ProofDependencyPendingException) {
            recordPendingProofDependency(item, pendingProof)?.let(rememberRetryDue)
            false
        } catch (error: Throwable) {
            recordFailure(item, error)?.let(rememberRetryDue)
            false
        }
    }

    /** The operator-facing reason a queued write did not go through — server copy where the
     *  server gave one, otherwise a plain sentence. Never a status line or exception name. */
    private fun Throwable.outboxLastError(): String =
        serverErrorText()?.display
            ?: (this as? NonRetryableSyncException)?.message?.trim()?.takeIf { it.isNotBlank() }
            ?: authAccessOutboxLastError()
            ?: "This did not go through yet. It will be tried again."

    private fun Throwable.authAccessOutboxLastError(): String? = when (appApiStatusCode()) {
        401 -> "Your session expired. Sign in again, then retry this write."
        403 -> "You do not have access for this write. Ask an admin to update your access, then retry."
        else -> null
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
            // SubmitScreen renders this verbatim to the operator when the row lands in
            // CONFLICT, so it must be the SERVER's own explanation of the refusal (message plus
            // any named field problems), never the transport's status line. A rejection the
            // server already explained arrives as NonRetryableSyncException carrying that copy.
            lastError = error.outboxLastError(),
            now = clock(),
        )
        // Report only what actually happened: a non-applied transition means another pass /
        // a manual retry already moved the row, so claiming a failure here would be a lie.
        if (applied) {
            val failureClass = error.javaClass.simpleName
            report(
                OutboxTelemetryEvent(
                    phase = OutboxWritePhase.ATTEMPT_FAILED,
                    opType = item.opType,
                    itemId = item.id,
                    groupKey = item.groupKey,
                    idempotencyKey = item.idempotencyKey,
                    referencedProofOutboxItemId = item.referencedProofOutboxItemId(),
                    attempt = attempt,
                    maxAttempts = item.maxAttempts,
                    failureClass = failureClass,
                ),
            )
            report(
                if (terminal) {
                    OutboxTelemetryEvent(
                        phase = OutboxWritePhase.TERMINAL,
                        opType = item.opType,
                        itemId = item.id,
                        groupKey = item.groupKey,
                        idempotencyKey = item.idempotencyKey,
                        referencedProofOutboxItemId = item.referencedProofOutboxItemId(),
                        attempt = attempt,
                        maxAttempts = item.maxAttempts,
                        failureClass = failureClass,
                        terminalReason = if (conflict) {
                            OutboxTerminalReason.CONFLICT
                        } else {
                            OutboxTerminalReason.ATTEMPTS_EXHAUSTED
                        },
                    )
                } else {
                    OutboxTelemetryEvent(
                        phase = OutboxWritePhase.RETRY_SCHEDULED,
                        opType = item.opType,
                        itemId = item.id,
                        groupKey = item.groupKey,
                        idempotencyKey = item.idempotencyKey,
                        referencedProofOutboxItemId = item.referencedProofOutboxItemId(),
                        attempt = attempt,
                        maxAttempts = item.maxAttempts,
                        failureClass = failureClass,
                        retryInMs = (nextAttemptAt - clock()).coerceAtLeast(0),
                    )
                },
            )
            if (terminal) reconcileFeatureTerminalFailure(item)
        }
        return if (applied && !terminal) {
            nextAttemptAt
        } else {
            null
        }
    }

    private suspend fun recordPendingProofDependency(item: OutboxEntity, error: ProofDependencyPendingException): Long? {
        val nextAttemptAt = clock() + PROOF_DEPENDENCY_WAIT_RETRY_MS
        val applied = store.markFailed(
            id = item.id,
            attemptCount = item.attemptCount,
            nextAttemptAt = nextAttemptAt,
            conflict = false,
            lastError = error.outboxLastError(),
            now = clock(),
        )
        if (applied) {
            report(
                OutboxTelemetryEvent(
                    phase = OutboxWritePhase.DEPENDENCY_WAIT,
                    opType = item.opType,
                    itemId = item.id,
                    groupKey = item.groupKey,
                    idempotencyKey = item.idempotencyKey,
                    referencedProofOutboxItemId = item.referencedProofOutboxItemId(),
                    attempt = item.attemptCount,
                    maxAttempts = item.maxAttempts,
                    failureClass = error.javaClass.simpleName,
                    retryInMs = (nextAttemptAt - clock()).coerceAtLeast(0),
                ),
            )
        }
        return if (applied) nextAttemptAt else null
    }

    /** Telemetry is diagnostics, never control flow: a broken reporter must not fail a write. */
    private fun report(event: OutboxTelemetryEvent) {
        runCatching { telemetry.onOutboxWrite(event) }
    }

    private fun OutboxEntity.referencedProofOutboxItemId(): String =
        PROOF_OUTBOX_ITEM_ID_REGEX.find(payloadJson)?.groupValues?.getOrNull(1).orEmpty()

    /** Logs a post-success local-cache reconcile failure (e.g. the Room mirror write in
     *  [reconcileFeatureSuccess] threw) without ever rethrowing: the golden rule is "never
     *  swallow an exception", but this one item is already SUCCEEDED on the server, so it must
     *  never be re-marked failed and must never abort the rest of a drain pass. Reused as
     *  [OutboxWritePhase.ATTEMPT_FAILED] telemetry (attempt/maxAttempts left at 0) — the closest
     *  existing signal that reaches the same Crashlytics/analytics sink as every other outbox
     *  failure, rather than adding a new wire-format phase for one call site. */
    private fun reportCacheReconcileFailure(item: OutboxEntity, error: Throwable) {
        report(
            OutboxTelemetryEvent(
                phase = OutboxWritePhase.ATTEMPT_FAILED,
                opType = item.opType,
                itemId = item.id,
                failureClass = error.javaClass.simpleName,
            ),
        )
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
        OutboxOpType.WEIGHING_WEIGHT_CORRECTION -> dispatchWeighingWeightCorrection(item)
        OutboxOpType.VERIFICATION_CLOSE -> dispatchVerificationClose(item)
        OutboxOpType.VERIFICATION_CLOSE_SUBMISSION -> dispatchVerificationSubmissionClose(item)
        OutboxOpType.VERIFICATION_CLOSE_BATCH -> dispatchVerificationBatchClose(item)
        OutboxOpType.VERIFICATION_REVIEW_EVENTS -> dispatchVerificationReviewEvents(item)
        OutboxOpType.COUNTS_SHIFTING -> dispatchCountsShifting(item)
        OutboxOpType.COUNTS_BIRTH -> dispatchCountsBirth(item)
        OutboxOpType.COUNTS_DEATH -> dispatchCountsDeath(item)
        OutboxOpType.COUNTS_APPROVAL_APPROVE -> dispatchCountsApprovalApprove(item)
        OutboxOpType.COUNTS_APPROVAL_REJECT -> dispatchCountsApprovalReject(item)
        OutboxOpType.SHIFTING_COMPLETE -> dispatchShiftingComplete(item)
        OutboxOpType.SHIFTING_CANCEL -> dispatchShiftingCancel(item)
        OutboxOpType.COUNTS_PROMOTE_IDENTIFIER -> dispatchPromoteIdentifier(item)
        OutboxOpType.FEED_DIRECTION_COMPLETE -> dispatchFeedDirectionComplete(item)
        OutboxOpType.FEED_DISTRIBUTION_COMPLETE -> dispatchFeedDistributionComplete(item)
        OutboxOpType.FEED_PACKING_COMPLETE -> dispatchFeedPackingComplete(item)
        OutboxOpType.FEED_WASTAGE_COMPLETE -> dispatchFeedWastageComplete(item)
        OutboxOpType.FEED_WASTAGE_MEASUREMENT -> dispatchFeedWastageMeasurement(item)
        OutboxOpType.MILK_PREPARATION_SUBMIT -> dispatchMilkPreparationSubmit(item)
        OutboxOpType.MILK_FEEDING_SUBMIT -> dispatchMilkFeedingSubmit(item)
        OutboxOpType.FEED_TRANSPORT_SUBMIT -> dispatchFeedTransportSubmit(item)
        OutboxOpType.WORKFLOW_ACTION_ANSWER -> dispatchWorkflowActionAnswer(item)
        OutboxOpType.WORKFLOW_ACTION_COMPLETE -> dispatchWorkflowActionComplete(item)
        OutboxOpType.HEALTH_CASE_OPEN -> dispatchHealthCaseOpen(item)
        OutboxOpType.HEALTH_OBSERVATION_SUBMIT -> dispatchHealthObservationSubmit(item)
        OutboxOpType.HEALTH_DIAGNOSIS_CONFIRM -> dispatchHealthDiagnosisConfirm(item)
        OutboxOpType.HEALTH_TREATMENT_COMPLETE -> dispatchHealthTreatmentComplete(item)
        OutboxOpType.WEIGHING_ANIMAL_OBSERVATION -> dispatchWeighingAnimalObservation(item)
        OutboxOpType.WEIGHING_SHED_OBSERVATION -> dispatchWeighingShedObservation(item)
        OutboxOpType.WEIGHING_SCOPE_SUBMIT -> dispatchWeighingScopeSubmit(item)
        OutboxOpType.PC_CARE_SCAN_ADD -> dispatchPcCareScanAdd(item)
        OutboxOpType.PC_CARE_SLOT_REGISTER -> dispatchPcCareSlotRegister(item)
        OutboxOpType.PC_CARE_TASK_PROOF_REGISTER -> dispatchPcCareTaskProofRegister(item)
        OutboxOpType.PC_CARE_TASK_SUBMIT -> dispatchPcCareTaskSubmit(item)
        OutboxOpType.TOXIN_STEP_COMPLETE -> dispatchToxinStepComplete(item)
        OutboxOpType.TOXIN_SUBMIT -> dispatchToxinSubmit(item)
        OutboxOpType.CLOCK_IN -> dispatchClockPunch(item, clockIn = true)
        OutboxOpType.CLOCK_OUT -> dispatchClockPunch(item, clockIn = false)
    }

    private suspend fun reconcileFeatureBeforeSuccess(item: OutboxEntity): Boolean {
        val opType = runCatching { OutboxOpType.valueOf(item.opType) }
            .onFailure { reportCacheReconcileFailure(item, it) }
            .getOrNull() ?: return false
        val hook = preSuccessRefreshHooks[opType] ?: return false
        return runCatching {
            hook.onSuccess(item.payloadJson)
            true
        }.getOrElse {
            reportCacheReconcileFailure(item, it)
            false
        }
    }

    private suspend fun reconcileFeatureSuccess(
        item: OutboxEntity,
        skipPostSuccessRefresh: Boolean = false,
    ) {
        when (OutboxOpType.valueOf(item.opType)) {
            OutboxOpType.SCAN_CAPTURE -> {
                val payload = syncJson.decodeFromString<ScanCapturePayload>(item.payloadJson)
                scannedGoatDao?.markFieldTagStatus(
                    taskId = payload.taskId,
                    partitionKey = payload.partitionKey,
                    fieldKey = payload.request.fieldKey,
                    tag = payload.request.tag,
                    obligationId = payload.request.obligationId?.takeIf { it.isNotBlank() },
                    status = CaptureSyncStatus.SYNCED.name,
                )
            }
            OutboxOpType.WEIGHING_ANIMAL_OBSERVATION ->
                weighingObservationDao?.markAcceptedByIdempotencyKey(item.idempotencyKey)
            OutboxOpType.WEIGHING_SHED_OBSERVATION ->
                weighingShedObservationDao?.markAcceptedByIdempotencyKey(item.idempotencyKey)
            OutboxOpType.HEALTH_OBSERVATION_SUBMIT -> projectDiagnosisProposal(item)
            OutboxOpType.HEALTH_DIAGNOSIS_CONFIRM -> projectDiagnosisDecision(item)
            OutboxOpType.WEIGHING_SCOPE_SUBMIT -> {
                val payload = syncJson.decodeFromString<WeighingScopeSubmitPayload>(item.payloadJson)
                val scopeId = "submit:${payload.campaignId}:${payload.campaignShedId}"
                // Only NOW -- the row is SUCCEEDED -- does the epoch rotate, exactly matching
                // WeighingRepository.advanceTransitionEpoch's "called only after the server
                // confirmed" contract it replaces. A failed/still-retrying row keeps sending the
                // SAME key.
                weighingTransitionEpochDao?.upsert(
                    WeighingTransitionEpochEntity(scopeId = scopeId, epoch = idGenerator(), updatedAt = clock()),
                )
                // Every OTHER epoch writer (WeighingRepository.advanceTransitionEpoch for
                // reopen/close-shed/close-campaign/update) prunes to the same bound right after
                // upserting; this one was skipped, leaving the transition-epoch table growing
                // unboundedly by one row per scope ever submitted. Same bound, same table.
                weighingTransitionEpochDao?.pruneOutsideNewest(WEIGHING_SCOPE_SUBMIT_CACHED_TRANSITION_SCOPES)
            }
            OutboxOpType.FEED_DISTRIBUTION_COMPLETE -> {
                val payload = syncJson.decodeFromString<FeedDistributionCompletePayload>(item.payloadJson)
                item.resultJson?.let { resultJson ->
                    val response = syncJson.decodeFromString<sg.mesha.goatos.core.network.dto.FeedDistributionCompleteResponseDto>(resultJson)
                    if (response.status.isNotBlank()) {
                        // The server already accepted this write (item is SUCCEEDED) — a failure
                        // HERE is only "local Room mirror didn't refresh", never a reason to mark
                        // the outbox row failed or abort the rest of this drain pass (this runs
                        // both per-item in processItem's try and in bulk in drainOnce's startup
                        // reconcile loop, which has no surrounding try/catch of its own). Caught
                        // locally and reported so it is never silently lost; the next successful
                        // preview/worklist fetch repairs the cache regardless.
                        runCatching {
                            feedRepository?.persistDirectionSessionStatuses(
                                targetDate = payload.targetDate,
                                shedId = payload.shedId,
                                partitionLabel = payload.partitionLabel ?: "",
                                workflow = payload.workflow,
                                sessionNo = payload.sessionNo,
                                lifecycleStatus = response.status,
                            )
                        }.onFailure { reportCacheReconcileFailure(item, it) }
                    }
                }
            }
            OutboxOpType.FEED_PACKING_COMPLETE -> {
                val payload = syncJson.decodeFromString<FeedPackingCompletePayload>(item.payloadJson)
                item.resultJson?.let { resultJson ->
                    val response = syncJson.decodeFromString<sg.mesha.goatos.core.network.dto.FeedPackingCompleteResponseDto>(resultJson)
                    if (response.status.isNotBlank()) {
                        // Same rationale as FEED_DISTRIBUTION_COMPLETE above: never let a local
                        // cache-write failure look like (or behave like) a dispatch failure.
                        runCatching {
                            feedRepository?.persistPackingRowStatuses(
                                targetDate = payload.targetDate,
                                shedId = payload.shedId,
                                partitionLabel = payload.partitionLabel ?: "",
                                workflow = payload.workflow,
                                sessionNo = payload.sessionNo,
                                lifecycleStatus = response.status,
                            )
                        }.onFailure { reportCacheReconcileFailure(item, it) }
                    }
                }
            }
            OutboxOpType.FEED_TRANSPORT_SUBMIT -> {
                val payload = syncJson.decodeFromString<FeedTransportSubmitPayload>(item.payloadJson)
                item.resultJson?.let { resultJson ->
                    val response = syncJson.decodeFromString<sg.mesha.goatos.core.network.dto.FeedTransportSubmitResponseDto>(resultJson)
                    runCatching {
                        feedTransportRepository?.persistTaskStatus(payload.taskId, response.status)
                    }.onFailure { reportCacheReconcileFailure(item, it) }
                }
            }
            OutboxOpType.FEED_WASTAGE_COMPLETE -> {
                val payload = syncJson.decodeFromString<FeedWastageCompletePayload>(item.payloadJson)
                item.resultJson?.let { resultJson ->
                    val response = syncJson.decodeFromString<sg.mesha.goatos.core.network.dto.FeedWastageCompleteResponseDto>(resultJson)
                    if (response.status.isNotBlank()) {
                        // Same rationale as FEED_PACKING_COMPLETE above: never let a local
                        // cache-write failure look like (or behave like) a dispatch failure.
                        runCatching {
                            feedRepository?.persistWastageRowStatus(
                                shedId = payload.shedId,
                                partitionLabel = payload.partitionLabel ?: "",
                                workflow = FEED_WASTAGE_WORKFLOW,
                                lifecycleStatus = response.status,
                            )
                        }.onFailure { reportCacheReconcileFailure(item, it) }
                    }
                }
            }
            OutboxOpType.PC_CARE_SCAN_ADD -> {
                val payload = syncJson.decodeFromString<PcCareScanAddPayload>(item.payloadJson)
                item.resultJson?.let { resultJson ->
                    // POST-dispatch result, never the pre-dispatch entity: the server row id only
                    // exists in the response this successful dispatch just returned.
                    val response = syncJson.decodeFromString<sg.mesha.goatos.core.network.dto.PcCareScanResponseDto>(resultJson)
                    // Same rationale as FEED_PACKING_COMPLETE above: never let a local cache-write
                    // failure look like (or behave like) a dispatch failure — reported, not thrown.
                    runCatching {
                        pcCareAnimalRowDao?.updateScanSynced(
                            taskId = payload.taskId,
                            normalizedTag = payload.normalizedTag,
                            animalRowId = response.animalRowId,
                            updatedAt = clock(),
                        )
                    }.onFailure { reportCacheReconcileFailure(item, it) }
                }
            }
            OutboxOpType.PC_CARE_TASK_SUBMIT -> {
                val payload = syncJson.decodeFromString<PcCareTaskSubmitPayload>(item.payloadJson)
                item.resultJson?.let { resultJson ->
                    val response = syncJson.decodeFromString<sg.mesha.goatos.core.network.dto.PcCareSubmitResponseDto>(resultJson)
                    if (response.status.isNotBlank()) {
                        // Same rationale as FEED_WASTAGE_COMPLETE above.
                        runCatching {
                            pcCareRepository?.persistTaskSubmitResult(
                                taskId = payload.taskId,
                                status = response.status,
                                rowVersion = response.rowVersion,
                                animalCount = response.animalCount,
                            )
                        }.onFailure { reportCacheReconcileFailure(item, it) }
                    }
                }
            }
            OutboxOpType.TOXIN_STEP_COMPLETE, OutboxOpType.TOXIN_SUBMIT -> {
                item.resultJson?.let { resultJson ->
                    // POST-dispatch result: the server returns the task's FRESH detail (its
                    // server-composed step states) from the very transaction this write landed
                    // in. Same rationale as FEED_PACKING_COMPLETE above: a local cache-write
                    // failure is reported, never allowed to look like a dispatch failure.
                    runCatching {
                        val detail = syncJson.decodeFromString<sg.mesha.goatos.core.network.dto.ToxinTaskDetailDto>(resultJson)
                        toxinRepository?.persistServerDetail(detail)
                    }.onFailure { reportCacheReconcileFailure(item, it) }
                }
            }
            else -> Unit
        }
        // Whole-page-blob opTypes (no server-truth row to write directly into a Room table) — run
        // AFTER the opType-specific arm above so any row-shaped reconcile still happens first.
        postSuccessRefreshHooks[OutboxOpType.valueOf(item.opType)]?.takeUnless { skipPostSuccessRefresh }?.let { hook ->
            runCatching { hook.onSuccess(item.payloadJson) }
                .onFailure { reportCacheReconcileFailure(item, it) }
        }
    }

    private suspend fun reconcileFeatureTerminalFailure(item: OutboxEntity) {
        val opType = runCatching { OutboxOpType.valueOf(item.opType) }
            .getOrElse {
                reportCacheReconcileFailure(item, it)
                return
            }
        if (opType == OutboxOpType.PC_CARE_SCAN_ADD) {
            // The durable animal row is the scan screen's model, so a terminally failed scan must
            // stop reading as still-queued work. PENDING-guarded: a DUPLICATE verdict (written by
            // the dispatch before the 409 terminalized this row) is never overwritten. Replayed
            // from the durable terminal row after process death like every reconcile here.
            runCatching {
                val payload = syncJson.decodeFromString<PcCareScanAddPayload>(item.payloadJson)
                pcCareAnimalRowDao?.markScanFailedIfPending(payload.taskId, payload.normalizedTag, clock())
            }.onFailure { reportCacheReconcileFailure(item, it) }
        }
        if (opType == OutboxOpType.TOXIN_STEP_COMPLETE || opType == OutboxOpType.TOXIN_SUBMIT) {
            // A terminal refusal here is the SERVER's clock/state disagreeing with the phone
            // (wait_not_elapsed, step_already_done, a cancelled task). The refusal's farm copy
            // rides the outbox row's lastError; re-reading the detail puts the server's own step
            // states back on screen — the network clearly works, the server just answered.
            runCatching {
                val taskId = when (opType) {
                    OutboxOpType.TOXIN_STEP_COMPLETE ->
                        syncJson.decodeFromString<ToxinStepCompletePayload>(item.payloadJson).taskId
                    else -> syncJson.decodeFromString<ToxinSubmitPayload>(item.payloadJson).taskId
                }
                toxinRepository?.refreshTaskDetail(taskId)
            }.onFailure { reportCacheReconcileFailure(item, it) }
        }
        postTerminalFailureHooks[opType]?.let { hook ->
            runCatching { hook.onTerminalFailure(item.payloadJson) }
                .onFailure { reportCacheReconcileFailure(item, it) }
        }
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

    /**
     * THE VERIFIER'S WEIGHT CORRECTION (maintainer decision 2026-08-17). Weighing owns the route --
     * the correction writes a weighing record -- while the verification item told the screen WHICH
     * record to address. The idempotency key is the outbox row's own stable key, so a redelivery
     * replays the original correction instead of writing a second one.
     */
    private suspend fun dispatchWeighingWeightCorrection(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<WeighingWeightCorrectionPayload>(item.payloadJson)
        val response = api.correctWeighingObservationWeight(payload.observationId, item.idempotencyKey, payload.request)
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

    private suspend fun dispatchVerificationReviewEvents(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<VerificationReviewEventsPayload>(item.payloadJson)
        val response = api.recordVerificationReviewEvents(payload.request)
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
    /**
     * The clock punch (module clock, maintainer decision 2026-08-27). Same idempotent-replay
     * contract as every other `dispatch*` here: the row's STORED day-scoped key
     * (`clock:<business_date>:<in|out>`) rides both the Idempotency-Key header and the body, so a
     * server-committed-but-client-unrecorded retry replays the original entry instead of punching
     * twice. A 409 (`already_clocked_in` / `not_clocked_in` / `already_clocked_out`) and a 422
     * `mock_location_detected` are definitive answers about an unchangeable day state — terminal
     * by [recordFailure]'s `isTerminalAppApiError` check, surfaced with the server's own message,
     * never retried against a payload that can never succeed.
     */
    private suspend fun dispatchClockPunch(item: OutboxEntity, clockIn: Boolean): String {
        val payload = syncJson.decodeFromString<ClockPunchPayload>(item.payloadJson)
        val response = if (clockIn) {
            api.recordClockIn(item.idempotencyKey, payload.request)
        } else {
            api.recordClockOut(item.idempotencyKey, payload.request)
        }
        return syncJson.encodeToString(response)
    }

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
            // The mandatory shifting video's proof_id; high-priority completions also resolve the
            // two embedded feed videos below before sending one atomic completion command.
            resolveShiftingProofRef(payload),
            resolveOptionalShiftingProofRef(payload.feedPackingProofOutboxItemId, "feed-packing"),
            resolveOptionalShiftingProofRef(payload.feedGivenProofOutboxItemId, "feeding"),
            payload.feedConfigFingerprint,
        )
        return syncJson.encodeToString(response)
    }

    /**
     * Resolves the uploaded proof_id for a shifting completion from its coupled PROOF_UPLOAD outbox
     * row. Same-group ordering means that row has already drained to SUCCEEDED before this completion
     * runs; if it has not (a rare concurrency edge, or a pre-upgrade row with no coupling), the
     * completion waits on the shared proof-dependency lane until the video is uploaded. A missing
     * coupling or a permanently-failed upload is terminal — a shed move without a verifiable video
     * must not reach the backend.
     */
    private suspend fun resolveShiftingProofRef(payload: ShiftingCompletePayload): String {
        val proofItemId = payload.proofOutboxItemId
            ?: throw NonRetryableSyncException("Shifting completion is missing its mandatory video reference.")
        return resolveUploadedProofRef(proofItemId)
    }

    private suspend fun resolveOptionalShiftingProofRef(proofItemId: String?, label: String): String? {
        if (proofItemId.isNullOrBlank()) return null
        return resolveUploadedProofRef(proofItemId)
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
     * The verifier-GATED feed-DISTRIBUTION completion (docs/decisions/feed-distribution-verification.md).
     * Same idempotent-replay contract as every other `dispatch*` — the row's STORED key is passed
     * verbatim as the `Idempotency-Key` header. ALL THREE mandatory proofs are resolved from their
     * referenced PROOF_UPLOAD outbox rows by id. The uploads do not have to share the completion's
     * group: a not-yet-succeeded proof row makes this completion retry, while a missing coupling or a
     * permanently-failed upload is terminal. A gated completion without every verifiable proof must not
     * reach the backend. The backend re-rejects a blank proof with `422 proof_required` (terminal by
     * [recordFailure]'s check).
     *
     * THE ROLLOUT CASE, stated because it costs an operator real work: a completion queued OFFLINE by
     * a build that predates the 2026-08-11 weight photo carries only two proofs. It cannot be healed
     * — the feed has been given out, so the weight photo no longer exists to take — and the backend
     * would reject it forever. It is failed TERMINALLY with a farm-language reason so the shed-session
     * returns to the operator's list as work still needing action, exactly as a verifier bounce does,
     * instead of retrying invisibly until someone notices the feeding never registered.
     */
    private suspend fun dispatchFeedDistributionComplete(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<FeedDistributionCompletePayload>(item.payloadJson)
        val response = api.completeFeedDistribution(
            item.idempotencyKey,
            FeedDistributionCompleteRequestDto(
                parkId = payload.parkId,
                shedId = payload.shedId,
                partitionLabel = payload.partitionLabel,
                sessionNo = payload.sessionNo,
                targetDate = payload.targetDate,
                workflow = payload.workflow,
                feedWeightProofRef = resolveFeedProofRef(
                    payload.feedWeightProofRef,
                    payload.feedWeightProofOutboxItemId,
                    "This feeding needs a feed weight photo. Please record this shed's feeding again.",
                ),
                distributionProofRef = resolveFeedProofRef(
                    payload.distributionProofRef,
                    payload.distributionProofOutboxItemId,
                    "This feeding needs a feed video. Please record this shed's feeding again.",
                ),
                waterProofRef = resolveFeedProofRef(
                    payload.waterProofRef,
                    payload.waterProofOutboxItemId,
                    "This feeding needs a water video. Please record this shed's feeding again.",
                ),
            ),
        )
        return syncJson.encodeToString(response)
    }

    /**
     * The verifier-GATED feed-PACKING completion. Simpler than [dispatchFeedDistributionComplete]:
     * a SINGLE mandatory packing video, resolved from its coupled PROOF_UPLOAD outbox row (same
     * group, drained first) exactly like [dispatchShiftingComplete]'s single video. A missing
     * coupling or a permanently-failed upload is terminal — a gated completion without a verifiable
     * proof must not reach the backend. The backend re-rejects a blank proof with `422
     * proof_required` (terminal by [recordFailure]'s check).
     */
    private suspend fun dispatchFeedPackingComplete(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<FeedPackingCompletePayload>(item.payloadJson)
        val response = api.completeFeedPacking(
            item.idempotencyKey,
            FeedPackingCompleteRequestDto(
                parkId = payload.parkId,
                shedId = payload.shedId,
                partitionLabel = payload.partitionLabel,
                // A row queued by the PEN-DAY build carries no session, so it decodes as 0. The route
                // rejects 0, which would strand an operator's already-recorded video on a 400 forever.
                // Map it to session 1 -- the same choice migration 000150 makes for the pen-day rows
                // already on the server, so the phone and the database agree on what an unlabelled
                // pen-day video proves: the morning bag.
                //
                // NOT a silent widening: a 0 can only come from a row written before this build, and
                // every row this build writes carries a real session. See FeedPackingCompletePayload.
                sessionNo = if (payload.sessionNo < 1) 1 else payload.sessionNo,
                targetDate = payload.targetDate,
                workflow = payload.workflow,
                packingProofRef = resolveUploadedProofRef(payload.packingProofOutboxItemId),
            ),
        )
        return syncJson.encodeToString(response)
    }

    /**
     * The verifier-GATED feed-WASTAGE completion (maintainer decision 2026-08-18). Shaped exactly
     * like [dispatchFeedPackingComplete]: a SINGLE mandatory video resolved from its coupled
     * PROOF_UPLOAD outbox row (same group, drained first). The grain is the PEN-DAY, so there is
     * no session and no workflow field. A `409` — this pen-day already holds a DIFFERENT video —
     * is terminal by [recordFailure]'s `isTerminalAppApiError` check, so it is surfaced to the
     * operator with the server's own sentence rather than retried against a state that will never
     * change.
     */
    private suspend fun dispatchFeedWastageComplete(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<FeedWastageCompletePayload>(item.payloadJson)
        val response = api.completeFeedWastage(
            item.idempotencyKey,
            FeedWastageCompleteRequestDto(
                parkId = payload.parkId,
                shedId = payload.shedId,
                partitionLabel = payload.partitionLabel,
                targetDate = payload.targetDate,
                wastageProofRef = resolveUploadedProofRef(payload.wastageProofOutboxItemId),
            ),
        )
        return syncJson.encodeToString(response)
    }

    /**
     * THE VERIFIER'S WASTAGE MEASUREMENT (maintainer decision 2026-08-18). Feed owns the route —
     * the measurement writes a feed-wastage record — while the verification item told the screen
     * WHICH record to address. The idempotency key is the outbox row's own stable, value-bearing
     * key, so a redelivery replays the original measurement instead of writing a second one.
     */
    private suspend fun dispatchFeedWastageMeasurement(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<FeedWastageMeasurementPayload>(item.payloadJson)
        val response = api.recordFeedWastageMeasurement(payload.completionId, item.idempotencyKey, payload.request)
        return syncJson.encodeToString(response)
    }

    /**
     * One PC Care RFID scan (module pc_care). Same idempotent-replay contract as every other
     * `dispatch*` — the row's STORED key is passed verbatim, never a new key on retry. A `409
     * duplicate_scan` (another phone already scanned this tag) is terminal by [recordFailure]'s
     * `isTerminalAppApiError` check; before rethrowing, the durable Room animal row is marked
     * DUPLICATE so the scan screen shows the server's verdict instead of a forever-pending row.
     * The server's own farm-language sentence rides the rethrown error into the outbox row's
     * lastError. A `409 task_locked` (submitted while this scan was queued) is likewise terminal,
     * surfaced with the server's copy through the generic terminal-failure reconcile.
     */
    private suspend fun dispatchPcCareScanAdd(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<PcCareScanAddPayload>(item.payloadJson)
        try {
            val response = api.scanPcCareAnimal(
                payload.taskId,
                item.idempotencyKey,
                sg.mesha.goatos.core.network.dto.PcCareScanRequestDto(scannedIdentifier = payload.tagVerbatim),
            )
            return syncJson.encodeToString(response)
        } catch (cancellation: CancellationException) {
            throw cancellation
        } catch (error: Throwable) {
            if (error.appApiStatusCode() == 409 && error.serverErrorText()?.code == "duplicate_scan") {
                // Room write failure here must not change the dispatch outcome; reported like
                // every other post-dispatch cache reconcile miss.
                runCatching {
                    pcCareAnimalRowDao?.markScanDuplicate(payload.taskId, payload.normalizedTag, clock())
                }.onFailure { reportCacheReconcileFailure(item, it) }
            }
            throw error
        }
    }

    /**
     * One PC Care slot proof registration. The animal's SERVER row id is re-resolved from the
     * durable Room animal row at dispatch time — it may have been blank at enqueue while the scan
     * was still syncing. Still blank is a plain retryable wait (the scan's own group drains
     * independently), never terminal. The video resolves through its coupled PROOF_UPLOAD row on
     * the same group exactly like [dispatchFeedPackingComplete]'s single video.
     */
    private suspend fun dispatchPcCareSlotRegister(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<PcCareSlotRegisterPayload>(item.payloadJson)
        val animalRowId = payload.animalRowId.ifBlank {
            val row = pcCareAnimalRowDao?.getByTag(payload.taskId, payload.normalizedTag)
            when {
                row == null ->
                    throw NonRetryableSyncException("This animal's scan is missing. Please scan it again.")
                row.scanSyncStatus == sg.mesha.goatos.core.data.cache.PcCareScanStatus.DUPLICATE ->
                    // The server refused the scan as already recorded by another phone; this
                    // phone's clip cannot attach to a row it does not own. Terminal with a
                    // farm-language reason instead of waiting on a sync that will never come.
                    throw NonRetryableSyncException("This animal was already scanned on another phone. Its video is recorded there.")
                row.animalRowId.isBlank() ->
                    throw ProofDependencyPendingException("Waiting for this animal's scan to finish syncing.")
                else -> row.animalRowId
            }
        }
        api.registerPcCareSlotProof(
            payload.taskId,
            animalRowId,
            payload.slotFieldKey,
            item.idempotencyKey,
            sg.mesha.goatos.core.network.dto.PcCareSlotProofRequestDto(
                proofRef = resolveUploadedProofRef(payload.proofOutboxItemId),
            ),
        )
        // The route returns no body; store an empty JSON object like other body-less successes.
        return "{}"
    }

    private suspend fun dispatchPcCareTaskProofRegister(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<PcCareTaskProofRegisterPayload>(item.payloadJson)
        api.registerPcCareTaskProof(
            payload.taskId,
            payload.slotFieldKey,
            item.idempotencyKey,
            sg.mesha.goatos.core.network.dto.PcCareSlotProofRequestDto(
                proofRef = resolveUploadedProofRef(payload.proofOutboxItemId),
            ),
        )
        return "{}"
    }

    /**
     * The WHOLE-task PC Care submit. Drains after every coupled PROOF_UPLOAD and
     * [dispatchPcCareSlotRegister] row on the same task group, so the server holds every slot
     * before the gate check runs. A `422 proof_incomplete` / `422 no_animals` is terminal by
     * [recordFailure]'s check and surfaces the server's own sentence to the operator.
     */
    private suspend fun dispatchPcCareTaskSubmit(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<PcCareTaskSubmitPayload>(item.payloadJson)
        val response = api.submitPcCareTask(payload.taskId, item.idempotencyKey)
        return syncJson.encodeToString(response)
    }

    /**
     * One Toxin step completion (module toxin). The proof resolves through its coupled
     * PROOF_UPLOAD row on the same task group exactly like [dispatchPcCareSlotRegister]'s video.
     * The row's STORED key is passed verbatim — never a new key on retry. A `422
     * wait_not_elapsed` (the SERVER clock gate — the phone never computes its own) or `409
     * step_already_done` (another authorized person got there first) is terminal by
     * [recordFailure]'s check, carries the server's own farm sentence into lastError, and the
     * terminal reconcile refreshes the task detail so the screen re-renders server state.
     */
    private suspend fun dispatchToxinStepComplete(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<ToxinStepCompletePayload>(item.payloadJson)
        val response = api.completeToxinStep(
            payload.taskId,
            payload.stepNo,
            item.idempotencyKey,
            sg.mesha.goatos.core.network.dto.ToxinStepCompleteRequestDto(
                proofRef = resolveUploadedProofRef(payload.proofOutboxItemId),
            ),
        )
        return syncJson.encodeToString(response)
    }

    /**
     * The Toxin reading submit — step 7's strip photo + outcome. Drains after every coupled
     * PROOF_UPLOAD and [dispatchToxinStepComplete] row on the same task group, so the server
     * holds every step before the reading lands. A `422` (steps incomplete / proof missing) is
     * terminal and surfaces the server's own sentence.
     */
    private suspend fun dispatchToxinSubmit(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<ToxinSubmitPayload>(item.payloadJson)
        val response = api.submitToxinReading(
            payload.taskId,
            item.idempotencyKey,
            sg.mesha.goatos.core.network.dto.ToxinSubmitRequestDto(
                outcome = payload.outcome,
                stripPhotoRef = resolveUploadedProofRef(payload.stripPhotoOutboxItemId),
            ),
        )
        return syncJson.encodeToString(response)
    }

    private suspend fun dispatchMilkPreparationSubmit(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<MilkPreparationSubmitPayload>(item.payloadJson)
        suspend fun proof(step: String): String? = payload.proofOutboxItemIds[step]?.let { resolveUploadedProofRef(it) }
        val response = api.submitMilkPreparation(
            item.idempotencyKey,
            MilkPreparationSubmissionRequestDto(
                parkId = payload.parkId,
                preparationDate = payload.preparationDate,
                goatMilkUsed = payload.goatMilkUsed,
                answers = MilkPreparationAnswersDto(
                    morningMilkCollectedLitres = payload.answers.morningMilkCollectedLitres,
                    eveningMilkCollectedLitres = payload.answers.eveningMilkCollectedLitres,
                    goatMilkQuantityLitres = payload.answers.goatMilkQuantityLitres,
                    boilingTemperatureC = payload.answers.boilingTemperatureC,
                    cooledTemperatureC = payload.answers.cooledTemperatureC,
                    uhtMilkQuantityLitres = payload.answers.uhtMilkQuantityLitres,
                    citricAcidGrams = payload.answers.citricAcidGrams,
                ),
                proofs = MilkPreparationProofsDto(
                    goatMilkQuantityProofRef = proof("goat_milk_quantity"),
                    boilingTemperatureProofRef = proof("boiling_temperature"),
                    cooledTemperatureProofRef = proof("cooled_temperature"),
                    uhtMilkQuantityProofRef = proof("uht_milk_quantity") ?: throw NonRetryableSyncException("UHT milk quantity video is missing."),
                    citricAcidMixingProofRef = proof("citric_acid_mixing") ?: throw NonRetryableSyncException("Citric acid mixing video is missing."),
                ),
            ),
        )
        return syncJson.encodeToString(response)
    }

    private suspend fun dispatchMilkFeedingSubmit(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<MilkFeedingSubmitPayload>(item.payloadJson)
        val response = api.submitMilkFeedingTask(
            payload.taskId,
            item.idempotencyKey,
            MilkFeedingSubmitRequestDto(
                parkId = payload.parkId,
                feedingDate = payload.feedingDate,
                sessionNo = payload.sessionNo,
                answers = payload.answers,
                proofs = MilkFeedingProofsDto(
                    cleanBottlesProofRef = resolveUploadedProofRef(payload.cleanBottlesProofOutboxItemId),
                    mixingAndFillingProofRef = resolveUploadedProofRef(payload.mixingAndFillingProofOutboxItemId),
                ),
            ),
        )
        return syncJson.encodeToString(response)
    }

    private suspend fun dispatchFeedTransportSubmit(item:OutboxEntity):String{val payload=syncJson.decodeFromString<FeedTransportSubmitPayload>(item.payloadJson);return syncJson.encodeToString(api.submitFeedTransport(payload.taskId,item.idempotencyKey,FeedTransportSubmitRequestDto(resolveUploadedProofRef(payload.proofOutboxItemId))))}

    /**
     * Resolves an uploaded proof_id from a referenced PROOF_UPLOAD outbox row. A not-yet-drained row
     * throws a plain exception -> a non-conflict retry until the upload finishes; a missing row or a
     * blank proof id is terminal.
     */
    /**
     * The server proof id for ONE feed slot, from whichever side holds it.
     *
     * [remoteRef] is already a SERVER proof id: the slot was shot on ANOTHER operator's phone, so
     * there is no local outbox row to resolve and it is sent verbatim. Otherwise the slot was shot
     * here and resolves through its own PROOF_UPLOAD row exactly as before.
     *
     * Neither present is terminal rather than retryable: a completion with a missing proof can never
     * succeed, so retrying forever would strand the row silently instead of telling the operator
     * what to re-record.
     */
    private suspend fun resolveFeedProofRef(
        remoteRef: String?,
        proofItemId: String?,
        missingMessage: String,
    ): String {
        remoteRef?.takeIf { it.isNotBlank() }?.let { return it }
        val localItemId = proofItemId?.takeIf { it.isNotBlank() }
            ?: throw NonRetryableSyncException(missingMessage)
        return resolveUploadedProofRef(localItemId)
    }

    private suspend fun resolveUploadedProofRef(proofItemId: String): String {
        val proofRow = store.findById(proofItemId)
            ?: throw NonRetryableSyncException("A required proof upload could not be found.")
        if (proofRow.status == OutboxStatus.FAILED.name && (proofRow.conflict || proofRow.attemptCount >= proofRow.maxAttempts)) {
            throw NonRetryableSyncException("A required proof upload failed permanently; record the proof again.")
        }
        if (proofRow.status != OutboxStatus.SUCCEEDED.name) {
            throw ProofDependencyPendingException("Waiting for a proof upload to finish before completing.")
        }
        val resultJson = proofRow.resultJson
            ?: throw IllegalStateException("A proof upload result is not yet available.")
        val proofId = syncJson.decodeFromString<ProofUploadResponseDto>(resultJson).proof.proofId
        if (proofId.isBlank()) {
            throw NonRetryableSyncException("A proof upload did not return a proof id.")
        }
        return proofId
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

    /**
     * The two Birth/Death workflow-action writes (docs/decisions/birth-death-workflows.md). Same
     * idempotent-replay contract as every other `dispatch*` — the row's STORED key is passed
     * verbatim, so an exact retry returns the original result (`idempotent_replay=true`). A NEW key
     * against an already-completed action is a 409, terminal by [recordFailure]'s check, so it
     * surfaces instead of retrying forever. A `requires_video` completion resolves its mandatory
     * proof from the coupled PROOF_UPLOAD outbox row (same group, drained first) exactly like
     * [dispatchShiftingComplete]'s video; the backend re-rejects a missing proof with `422
     * proof_required` (also terminal).
     */
    private suspend fun dispatchWorkflowActionAnswer(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<WorkflowActionAnswerPayload>(item.payloadJson)
        val response = api.answerWorkflowAction(
            payload.workflowId,
            payload.actionId,
            item.idempotencyKey,
            WorkflowActionAnswerRequestDto(
                answerValue = payload.answerValue,
                proofRef = payload.proofOutboxItemId?.let { resolveUploadedProofRef(it) },
            ),
        )
        return syncJson.encodeToString(response)
    }

    private suspend fun dispatchWorkflowActionComplete(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<WorkflowActionCompletePayload>(item.payloadJson)
        val response = api.completeWorkflowAction(
            payload.workflowId,
            payload.actionId,
            item.idempotencyKey,
            WorkflowActionCompleteRequestDto(
                proofRef = payload.proofOutboxItemId?.let { resolveUploadedProofRef(it) },
            ),
        )
        return syncJson.encodeToString(response)
    }

    private suspend fun dispatchHealthTreatmentComplete(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<HealthTreatmentCompletePayload>(item.payloadJson)
        val response = api.completeHealthWorkItem(
            healthSessionId = payload.healthSessionId,
            idempotencyKey = item.idempotencyKey,
            request = HealthCompleteRequestDto(proofRef = payload.proofRef),
        )
        return syncJson.encodeToString(response)
    }

    /**
     * Writes the register's assessment into Room so the proposal screen reads it like every
     * other screen-facing read model.
     *
     * Idempotent by construction (REPLACE upsert on the run id), which is what lets it run on
     * both the hot path and the crash-recovery sweep over recent terminals.
     *
     * A result that cannot be decoded is DROPPED, not thrown: the write itself already
     * succeeded on the server, and failing here would send a recorded observation back through
     * the queue to be submitted a second time. The manager loses the cached copy, not the work.
     */
    private suspend fun projectDiagnosisProposal(item: OutboxEntity) {
        val dao = healthDiagnosisRunDao ?: return
        val resultJson = item.resultJson ?: return
        // exception:exempt local cache projection; an undecodable payload leaves the row
        // unwritten and the screen's refresh-on-open re-fetches it from the server.
        val payload = runCatching {
            syncJson.decodeFromString<HealthObservationSubmitPayload>(item.payloadJson)
        }.getOrNull() ?: return
        // exception:exempt same cache projection; see above.
        val response = runCatching {
            syncJson.decodeFromString<HealthDiagnosisProposalResponseDto>(resultJson)
        }.getOrNull() ?: return
        if (response.diagnosisRunId.isBlank()) return

        dao.upsert(
            HealthDiagnosisRunEntity(
                diagnosisRunId = response.diagnosisRunId,
                goatId = payload.goatId,
                goatDisplayId = payload.goatDisplayId,
                status = response.status,
                // The observation instant, not the sync instant: a form filled in a shed with no
                // signal and synced hours later must still sort where the manager recorded it.
                observedAtMs = item.createdAt,
                dtoJson = resultJson,
                updatedAt = clock(),
            ),
        )
        dao.deleteOldestBeyond(DIAGNOSIS_RUN_CACHE_LIMIT)
    }

    /**
     * Moves a decided run out of the Director's queue.
     *
     * Only the status changes; the proposal blob is left exactly as it was, because what the
     * Director confirmed is the thing worth being able to re-read afterwards.
     */
    private suspend fun projectDiagnosisDecision(item: OutboxEntity) {
        val dao = healthDiagnosisRunDao ?: return
        // exception:exempt local cache projection; an undecodable payload leaves the cached
        // status stale and the queue's refresh-on-open corrects it from the server.
        val payload = runCatching {
            syncJson.decodeFromString<HealthDiagnosisConfirmPayload>(item.payloadJson)
        }.getOrNull() ?: return
        // exception:exempt same cache projection; see above.
        val response = runCatching {
            syncJson.decodeFromString<ConfirmHealthDiagnosisResponseDto>(item.resultJson ?: return)
        }.getOrNull() ?: return
        val cached = dao.get(payload.diagnosisRunId) ?: return
        dao.upsert(cached.copy(status = response.status, updatedAt = clock()))
    }

    private suspend fun dispatchHealthObservationSubmit(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<HealthObservationSubmitPayload>(item.payloadJson)
        val response = api.submitHealthObservation(
            idempotencyKey = item.idempotencyKey,
            request = sg.mesha.goatos.core.network.dto.SubmitHealthObservationRequestDto(
                goatId = payload.goatId,
                findings = payload.findings,
                context = payload.context,
            ),
        )
        return syncJson.encodeToString(response)
    }

    private suspend fun dispatchHealthDiagnosisConfirm(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<HealthDiagnosisConfirmPayload>(item.payloadJson)
        val response = api.confirmHealthDiagnosis(
            diagnosisRunId = payload.diagnosisRunId,
            idempotencyKey = item.idempotencyKey,
            request = sg.mesha.goatos.core.network.dto.ConfirmHealthDiagnosisRequestDto(
                confirmedProblems = payload.confirmedProblems,
            ),
        )
        return syncJson.encodeToString(response)
    }

    private suspend fun dispatchHealthCaseOpen(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<HealthCaseOpenPayload>(item.payloadJson)
        val response = api.openHealthCase(
            idempotencyKey = item.idempotencyKey,
            request = sg.mesha.goatos.core.network.dto.HealthOpenCaseRequestDto(
                goatId = payload.goatId,
                diseaseKey = payload.diseaseKey,
                ageBand = payload.ageBand,
                startDate = payload.startDate,
            ),
        )
        return syncJson.encodeToString(response)
    }

    private suspend fun dispatchWeighingAnimalObservation(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<WeighingAnimalObservationPayload>(item.payloadJson)
        val response = api.recordWeighingAnimalObservation(payload.campaignId, item.idempotencyKey, payload.request)
        return syncJson.encodeToString(response)
    }

    private suspend fun dispatchWeighingShedObservation(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<WeighingShedObservationPayload>(item.payloadJson)
        val response = api.recordWeighingShedObservation(payload.campaignId, item.idempotencyKey, payload.request)
        return syncJson.encodeToString(response)
    }

    // The close gate is UNCONDITIONAL server-side (verification pending -> the submit itself is
    // rejected, never bypassed by a client flag). Reusing the row's stable idempotencyKey verbatim
    // on every attempt is what lets a server-committed-but-client-unrecorded retry dedupe instead
    // of double-submitting; WeighingRepository advances its transition epoch only after this
    // returns successfully (via reconcileFeatureSuccess below), so a failed/retried attempt keeps
    // sending the SAME key until the server actually confirms it.
    private suspend fun dispatchWeighingScopeSubmit(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<WeighingScopeSubmitPayload>(item.payloadJson)
        api.submitWeighingScope(payload.campaignId, payload.campaignShedId, item.idempotencyKey, payload.request)
        return "{}"
    }

    private companion object {
        const val SUCCESS_RECONCILE_LIMIT = 20
        // Max rows pulled into memory per drain iteration. A long offline backlog drains in
        // successive batches of this size rather than one unbounded SELECT * materialization.
        const val DRAIN_BATCH_SIZE = 200
        const val NO_RETRY_DUE = Long.MAX_VALUE
        const val PROOF_DEPENDENCY_WAIT_RETRY_MS = 1_000L
        val PROOF_OUTBOX_ITEM_ID_REGEX = Regex(""""proof_outbox_item_id"\s*:\s*"([^"]+)"""")

        // The phone keeps the recent assessments a manager might re-open, not a history. The
        // server owns the record; without a cap this table only ever grows.
        // A RETENTION cap for deleteOldestBeyond, not a page fetch: nothing reads 50 rows;
        // this is the number KEPT before the oldest are pruned.
        const val DIAGNOSIS_RUN_CACHE_LIMIT = 50 // mobile-guard:ignore: retention cap, never fetched
        // Mirrors WeighingRepository's private WEIGHING_CACHED_TRANSITION_SCOPES bound for the
        // SAME weighing_transition_epoch table -- every writer of that table prunes to this bound.
        const val WEIGHING_SCOPE_SUBMIT_CACHED_TRANSITION_SCOPES = 50

        // Wastage exists only on experiment pens; the server stamps the workflow, and the Room
        // grain key mirrors it so the reconcile addresses the exact cached row the worklist wrote.
        const val FEED_WASTAGE_WORKFLOW = "experiment"
    }
}
