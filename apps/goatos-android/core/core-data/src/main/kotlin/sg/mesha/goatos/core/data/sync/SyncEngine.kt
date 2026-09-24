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
import sg.mesha.goatos.core.network.dto.WorkflowActionWriteResponseDto
import sg.mesha.goatos.core.network.dto.CountsSopCaptureDto
import sg.mesha.goatos.core.network.dto.FeedDirectionCompleteRequestDto
import sg.mesha.goatos.core.network.dto.FeedDistributionCompleteRequestDto
import sg.mesha.goatos.core.network.dto.FeedTransportSubmitRequestDto
import sg.mesha.goatos.core.network.dto.FeedPackingCompleteRequestDto
import sg.mesha.goatos.core.network.dto.FeedWastageCompleteRequestDto
import sg.mesha.goatos.core.data.cache.HealthDiagnosisRunDao
import sg.mesha.goatos.core.data.cache.HealthDiagnosisRunEntity
import sg.mesha.goatos.core.network.dto.ConfirmHealthDiagnosisResponseDto
import sg.mesha.goatos.core.network.dto.HealthCloseCaseRequestDto
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
import sg.mesha.goatos.core.network.dto.WorkflowProofItemDto
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
    // Feed & water removal (maintainer decision 2026-09-03): the Room card row a successful
    // fasting submit reconciles — the server returns the fresh card in the SAME response, so the
    // list/detail flip to "Submitted — video in review" offline-durably, not on the next fetch.
    private val weighingFastingCardDao: sg.mesha.goatos.core.data.weighing.WeighingFastingCardDao? = null,
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
    // Pen visits (maintainer decision 2026-09-07): a successful submit RETURNS the visit's fresh
    // task (its `Done` chip, done line, row_version); this repository writes it through Room so
    // the card and detail flip to done the moment the write drains. Same defect class as
    // toxinRepository above: null here silently no-ops the reconcile in production.
    private val penVisitsRepository: sg.mesha.goatos.core.data.PenVisitsRepository? = null,
    // Pen routines (maintainer instruction 2026-09-16): a successful presence punch or submit
    // RETURNS the task's fresh Step (its chip, in_pen, can_submit, row_version); this repository
    // writes it through Room so the detail re-renders the server's state the moment the write
    // drains. Same defect class: null here silently no-ops the reconcile in production.
    private val penRoutinesRepository: sg.mesha.goatos.core.data.PenRoutinesRepository? = null,
    // Vendors module (maintainer decision 2026-09-03): without this the VENDOR_CREATE /
    // FEED_PURCHASE_CREATE reconciliation silently no-ops and the phone keeps showing the
    // register without the vendor it just recorded until the next refresh. Same defect class as
    // feedRepository/toxinRepository above.
    private val vendorsRepository: sg.mesha.goatos.core.data.VendorsRepository? = null,
    // Sales (maintainer instruction 2026-09-04): the recorded deal reconciles into Room the same way.
    private val salesRepository: sg.mesha.goatos.core.data.SalesRepository? = null,
    // Animal purchases (maintainer decision 2026-09-13): the recorded load / animal reconciles into
    // Room the same way. Same defect class as feedRepository/toxinRepository above: null here
    // silently no-ops the reconcile in production and the phone shows the row only after refresh.
    private val animalPurchaseRepository: sg.mesha.goatos.core.data.AnimalPurchaseRepository? = null,
    // Market survey (maintainer decision 2026-09-14): the recorded card reconciles into the cached
    // day the same way. Null silently no-ops the reconcile -- the same defect class as above.
    private val marketRepository: sg.mesha.goatos.core.data.MarketRepository? = null,
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

    /**
     * Terminal rows (id:status) whose stored response has already been applied to Room in THIS
     * process. Bounded to a small multiple of [SUCCESS_RECONCILE_LIMIT]: the replay only ever
     * looks at the most recent terminals, so older keys can be forgotten without ever replaying
     * again (a forgotten row is also outside the window). Process-scoped by design -- a restart
     * replays each recent terminal exactly once, which is the process-death repair this exists for.
     */
    private val replayedTerminals = BoundedKeySet(capacity = SUCCESS_RECONCILE_LIMIT * 8)
    /** Conflicted step writes already re-read against the server in this process. */
    private val settleAttemptedRows = BoundedKeySet(capacity = SUCCESS_RECONCILE_LIMIT * 8)

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
        //
        // ONCE PER PROCESS, never once per drain (defect found on the phone 2026-09-07): a
        // terminal's stored response is the server's truth AT THE TIME IT LANDED. Replaying it on
        // every pass re-wrote a pen visit's detail cache with an OLD completed payload while a newer
        // attempt for the same task was still uploading, so the screen read "Submitted" over a video
        // that had not gone through. The repair a process death needs is the FIRST replay after the
        // restart; every later pass in the same process is a regression of fresher state.
        withContext(dispatchers.io) {
            store.observeRecentTerminals(SUCCESS_RECONCILE_LIMIT).forEach { terminal ->
                val replayKey = terminal.id + ":" + terminal.status
                if (!replayedTerminals.add(replayKey)) return@forEach
                runCatching {
                    when {
                        terminal.status == OutboxStatus.SUCCEEDED.name -> reconcileFeatureSuccess(terminal)
                        terminal.status == OutboxStatus.FAILED.name &&
                            (terminal.conflict || terminal.attemptCount >= terminal.maxAttempts) ->
                            reconcileFeatureTerminalFailure(terminal)
                        // Not terminal after all (a FAILED row still due a retry): let a later pass
                        // decide once it truly settles.
                        else -> replayedTerminals.remove(replayKey)
                    }
                }.onFailure {
                    // A failed local write may be retried by the next pass; the bounded set forgets it.
                    replayedTerminals.remove(replayKey)
                    reportCacheReconcileFailure(terminal, it)
                }
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
        withContext(dispatchers.io) { settleConflictedWorkflowWritesTheServerHolds(::rememberRetryDue) }
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
        val claimedItem = store.findById(item.id) ?: item
        report(
            OutboxTelemetryEvent(
                phase = OutboxWritePhase.ATTEMPT_STARTED,
                opType = claimedItem.opType,
                itemId = claimedItem.id,
                groupKey = claimedItem.groupKey,
                idempotencyKey = claimedItem.idempotencyKey,
                referencedProofOutboxItemId = claimedItem.referencedProofOutboxItemId(),
                attempt = claimedItem.attemptCount + 1,
                maxAttempts = claimedItem.maxAttempts,
            ),
        )
        return try {
            val resultJson = dispatch(claimedItem)
            val preSuccessRefreshApplied = reconcileFeatureBeforeSuccess(claimedItem)
            if (store.markSucceeded(claimedItem.id, resultJson, clock())) {
                report(
                    OutboxTelemetryEvent(
                        phase = OutboxWritePhase.SUCCEEDED,
                        opType = claimedItem.opType,
                        itemId = claimedItem.id,
                        groupKey = claimedItem.groupKey,
                        idempotencyKey = claimedItem.idempotencyKey,
                        referencedProofOutboxItemId = claimedItem.referencedProofOutboxItemId(),
                        attempt = claimedItem.attemptCount + 1,
                        maxAttempts = claimedItem.maxAttempts,
                    ),
                )
                // Reconcile with the response from this successful dispatch. The original
                // in-memory item predates markSucceeded and therefore has resultJson=null.
                reconcileFeatureSuccess(
                    claimedItem.copy(resultJson = resultJson),
                    skipPostSuccessRefresh = preSuccessRefreshApplied,
                )
                // Applied now, from this dispatch's own response; the next drain pass must not
                // replay the stored copy over whatever the screen has learned since.
                replayedTerminals.add(claimedItem.id + ":" + OutboxStatus.SUCCEEDED.name)
            }
            true
        } catch (cancellation: CancellationException) {
            throw cancellation
        } catch (pendingProof: ProofDependencyPendingException) {
            recordPendingProofDependency(claimedItem, pendingProof)?.let(rememberRetryDue)
            false
        } catch (error: Throwable) {
            recordFailure(claimedItem, error)?.let(rememberRetryDue)
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
            // The server's own code beside its sentence, so a screen can key on WHAT was refused
            // (the verifier's confirm guard) instead of parsing the wording. Null when it did not say.
            lastErrorCode = error.serverErrorText()?.code?.takeIf { it.isNotBlank() },
            // ...and the ONE input it named as refused, so a questionnaire form can mark that
            // question rather than parse the sentence. Null when it named none.
            lastErrorField = error.serverErrorText()?.field?.takeIf { it.isNotBlank() },
            // The HTTP status, so writes refused for ACCESS (403) can be re-queued once a parked
            // account gets its access back, without touching any other failure.
            lastHttpStatus = error.appApiStatusCode(),
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
            // A proof still uploading is not a server refusal; there is no code to keep.
            lastErrorCode = null,
            lastErrorField = null,
            lastHttpStatus = null,
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
        OutboxOpType.PEN_RECONCILIATION_COMPLETE -> dispatchPenReconciliationComplete(item)
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
        OutboxOpType.HEALTH_STEP_PROOF_REGISTER -> dispatchHealthStepProofRegister(item)
        OutboxOpType.HEALTH_TREATMENT_COMPLETE -> dispatchHealthTreatmentComplete(item)
        OutboxOpType.HEALTH_CASE_CLOSE -> dispatchHealthCaseClose(item)
        OutboxOpType.WEIGHING_ANIMAL_OBSERVATION -> dispatchWeighingAnimalObservation(item)
        OutboxOpType.WEIGHING_SHED_OBSERVATION -> dispatchWeighingShedObservation(item)
        OutboxOpType.WEIGHING_SCOPE_SUBMIT -> dispatchWeighingScopeSubmit(item)
        OutboxOpType.WEIGHING_FASTING_SUBMIT -> dispatchWeighingFastingSubmit(item)
        OutboxOpType.PC_CARE_SCAN_ADD -> dispatchPcCareScanAdd(item)
        OutboxOpType.PC_CARE_SLOT_REGISTER -> dispatchPcCareSlotRegister(item)
        OutboxOpType.PC_CARE_TASK_PROOF_REGISTER -> dispatchPcCareTaskProofRegister(item)
        OutboxOpType.PC_CARE_TASK_SUBMIT -> dispatchPcCareTaskSubmit(item)
        OutboxOpType.TOXIN_STEP_COMPLETE -> dispatchToxinStepComplete(item)
        OutboxOpType.TOXIN_SUBMIT -> dispatchToxinSubmit(item)
        OutboxOpType.PEN_VISIT_SUBMIT -> dispatchPenVisitSubmit(item)
        OutboxOpType.PEN_ROUTINE_PRESENCE -> dispatchPenRoutinePresence(item)
        OutboxOpType.PEN_ROUTINE_SUBMIT -> dispatchPenRoutineSubmit(item)
        OutboxOpType.CLOCK_IN -> dispatchClockPunch(item, clockIn = true)
        OutboxOpType.CLOCK_OUT -> dispatchClockPunch(item, clockIn = false)
        OutboxOpType.LEAVE_REQUEST_CREATE -> dispatchLeaveRequestCreate(item)
        OutboxOpType.LEAVE_REQUEST_WITHDRAW -> dispatchLeaveRequestWithdraw(item)
        OutboxOpType.LEAVE_APPROVE -> dispatchLeaveDecision(item, approve = true)
        OutboxOpType.LEAVE_REJECT -> dispatchLeaveDecision(item, approve = false)
        OutboxOpType.VENDOR_CREATE -> dispatchVendorCreate(item)
        OutboxOpType.VENDOR_UPDATE -> dispatchVendorUpdate(item)
        OutboxOpType.FEED_PURCHASE_CREATE -> dispatchFeedPurchaseCreate(item)
        OutboxOpType.SALES_DEAL_CREATE -> dispatchSalesDealCreate(item)
        OutboxOpType.MARKET_SURVEY_RECORD -> dispatchMarketSurveyRecord(item)
        OutboxOpType.SALES_DEAL_PAYMENT_WRITE -> dispatchSalesDealPaymentWrite(item)
        OutboxOpType.SALES_DEAL_STATUS_SET -> dispatchSalesDealStatusSet(item)
        OutboxOpType.SALES_PIPELINE_WRITE -> dispatchSalesPipelineWrite(item)
        OutboxOpType.FEED_PURCHASE_EDIT_WRITE -> dispatchFeedPurchaseEdit(item)
        OutboxOpType.ANIMAL_PURCHASE_LOAD_CREATE -> dispatchAnimalPurchaseLoadCreate(item)
        OutboxOpType.ANIMAL_PURCHASE_ANIMAL_CREATE -> dispatchAnimalPurchaseAnimalCreate(item)
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

    /** Test seam: drives the success-reconcile arm for one already-SUCCEEDED row. */
    internal suspend fun reconcileSucceededForTest(item: OutboxEntity) = reconcileFeatureSuccess(item)

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
            OutboxOpType.WEIGHING_FASTING_SUBMIT -> {
                item.resultJson?.let { resultJson ->
                    // Same rationale as FEED_DISTRIBUTION_COMPLETE below: the server already
                    // accepted this write, so a local Room mirror failure is reported, never
                    // allowed to look like (or behave like) a dispatch failure.
                    runCatching {
                        val response = syncJson.decodeFromString<sg.mesha.goatos.core.network.dto.WeighingFastingShedCardResponseDto>(resultJson)
                        val fresh = response.fastingShedCard
                        if (fresh.fastingTaskId.isNotBlank() && fresh.campaignShedId.isNotBlank()) {
                            val existing = weighingFastingCardDao?.getCard(fresh.fastingTaskId, fresh.campaignShedId)
                            // A RETRIED submit replays the server's ORIGINAL snapshot, which can
                            // be OLDER than what a list refresh has since written (the verifier
                            // may already have sent this shed back). Mirroring an older snapshot
                            // over newer state regressed a sent-back card to "Submitted" on a
                            // real phone (2026-09-03); the row_version fence keeps the newest.
                            val cachedVersion = existing?.let { row ->
                                runCatching {
                                    syncJson.decodeFromString(
                                        sg.mesha.goatos.core.network.dto.WeighingFastingShedCardDto.serializer(),
                                        row.dtoJson,
                                    ).rowVersion
                                }.onFailure { reportCacheReconcileFailure(item, it) }
                                    .getOrNull()
                            } ?: -1
                            if (fresh.rowVersion < cachedVersion) return@runCatching
                            weighingFastingCardDao?.upsert(
                                sg.mesha.goatos.core.data.weighing.WeighingFastingCardEntity(
                                    fastingTaskId = fresh.fastingTaskId,
                                    campaignShedId = fresh.campaignShedId,
                                    sortIndex = existing?.sortIndex ?: 0L,
                                    status = fresh.status,
                                    removalBusinessDate = fresh.removalBusinessDate,
                                    dtoJson = syncJson.encodeToString(
                                        sg.mesha.goatos.core.network.dto.WeighingFastingShedCardDto.serializer(),
                                        fresh,
                                    ),
                                    updatedAt = clock(),
                                ),
                            )
                        }
                    }.onFailure { reportCacheReconcileFailure(item, it) }
                }
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
            OutboxOpType.VENDOR_CREATE,
            OutboxOpType.VENDOR_UPDATE,
            -> {
                item.resultJson?.takeIf { it.trim() != "{}" }?.let { resultJson ->
                    runCatching {
                        vendorsRepository?.persistServerVendor(
                            syncJson.decodeFromString<sg.mesha.goatos.core.network.dto.VendorDto>(resultJson),
                        )
                    }.onFailure { reportCacheReconcileFailure(item, it) }
                }
            }
            // A create and every later change return the SAME shape -- the whole load, with the
            // server's recomputed balance, per-kg cost and stock figure -- so they reconcile alike.
            OutboxOpType.FEED_PURCHASE_CREATE,
            OutboxOpType.FEED_PURCHASE_EDIT_WRITE,
            -> {
                item.resultJson?.let { resultJson ->
                    runCatching {
                        vendorsRepository?.persistServerFeedPurchase(
                            syncJson.decodeFromString<sg.mesha.goatos.core.network.dto.FeedPurchaseDto>(resultJson),
                        )
                    }.onFailure { reportCacheReconcileFailure(item, it) }
                }
            }
            // A create, a receipt and a status change all return the SAME shape -- the whole
            // deal, with the server's recomputed balance -- so they reconcile identically.
            OutboxOpType.SALES_DEAL_CREATE,
            OutboxOpType.SALES_DEAL_PAYMENT_WRITE,
            OutboxOpType.SALES_DEAL_STATUS_SET,
            -> {
                item.resultJson?.let { resultJson ->
                    runCatching {
                        salesRepository?.persistServerDeal(
                            syncJson.decodeFromString<sg.mesha.goatos.core.network.dto.SalesDealDto>(resultJson),
                        )
                    }.onFailure { reportCacheReconcileFailure(item, it) }
                }
            }
            OutboxOpType.PEN_VISIT_SUBMIT -> {
                item.resultJson?.let { resultJson ->
                    // POST-dispatch result: the server returns the visit's FRESH task from the
                    // very transaction this submit landed in (or, after a 409 already_submitted,
                    // the re-fetched one). A local cache-write failure is reported, never allowed
                    // to look like a dispatch failure — the FEED_PACKING_COMPLETE rationale.
                    runCatching {
                        val detail = syncJson.decodeFromString<sg.mesha.goatos.core.network.dto.PenVisitDetailDto>(resultJson)
                        penVisitsRepository?.persistServerDetail(detail)
                    }.onFailure { reportCacheReconcileFailure(item, it) }
                }
            }
            OutboxOpType.PEN_ROUTINE_PRESENCE, OutboxOpType.PEN_ROUTINE_SUBMIT -> {
                item.resultJson?.let { resultJson ->
                    // POST-dispatch result: the server returns the task's FRESH Step from the very
                    // transaction this write landed in (or, after a 409 already_done / in_review,
                    // the re-fetched one). Same rationale as PEN_VISIT_SUBMIT above.
                    runCatching {
                        val detail = syncJson.decodeFromString<sg.mesha.goatos.core.network.dto.PenRoutineDetailDto>(resultJson)
                        penRoutinesRepository?.persistServerDetail(detail)
                    }.onFailure { reportCacheReconcileFailure(item, it) }
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
            // Animal purchases (2026-09-13): the create returns the WHOLE recorded load / animal
            // (backend title, summary, counts, decision chip), written straight into Room so the
            // list and the load screen show the server's row the moment the write drains.
            OutboxOpType.ANIMAL_PURCHASE_LOAD_CREATE -> {
                item.resultJson?.let { resultJson ->
                    runCatching {
                        animalPurchaseRepository?.persistServerLoad(
                            syncJson.decodeFromString<sg.mesha.goatos.core.network.dto.AnimalPurchaseLoadDto>(resultJson),
                        )
                    }.onFailure { reportCacheReconcileFailure(item, it) }
                }
            }
            // Market survey (2026-09-14): the write returns the city's card as the server now holds
            // it (its status, every question's price), written straight into the cached day.
            OutboxOpType.MARKET_SURVEY_RECORD -> {
                item.resultJson?.let { resultJson ->
                    runCatching {
                        val payload = syncJson.decodeFromString<MarketSurveyRecordPayload>(item.payloadJson)
                        marketRepository?.persistServerCard(
                            payload.businessDate,
                            syncJson.decodeFromString<sg.mesha.goatos.core.network.dto.MarketSurveyCardDto>(resultJson),
                        )
                    }.onFailure { reportCacheReconcileFailure(item, it) }
                }
            }
            OutboxOpType.ANIMAL_PURCHASE_ANIMAL_CREATE -> {
                item.resultJson?.let { resultJson ->
                    runCatching {
                        animalPurchaseRepository?.persistServerAnimal(
                            syncJson.decodeFromString<sg.mesha.goatos.core.network.dto.AnimalPurchaseAnimalDto>(resultJson),
                        )
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
        val completed = if (registered.proof.uploadState.equals("completed", ignoreCase = true)) {
            api.completeRegisteredProofUpload(
                proofId = proofId,
                mimeType = payload.request.mimeType,
                durationMs = payload.durationMs,
            )
        } else {
            api.uploadProofBlob(
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
        }
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
    /**
     * A vendor recorded on the phone (module vendors, maintainer decision 2026-09-03). The voice
     * note, when there is one, rides ahead as a PROOF_UPLOAD on the same group; its server proof id
     * is resolved here exactly like a toxin step's clip. The route carries no idempotency header:
     * the register's natural key (business, record type, state, phone) refuses a second identical
     * vendor with `409 vendor_duplicate`, so a replay after a lost response is read as "already
     * recorded" and succeeds with an empty result — the list refresh-on-open then shows the row.
     */
    private suspend fun dispatchVendorCreate(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<VendorCreatePayload>(item.payloadJson)
        val voiceNoteRef = if (payload.voiceNoteOutboxItemId.isBlank()) "" else resolveUploadedProofRef(payload.voiceNoteOutboxItemId)
        return try {
            val created = api.createProcurementVendor(payload.request.copy(voiceNoteProofRef = voiceNoteRef))
            syncJson.encodeToString(created)
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            if (error.appApiStatusCode() == 409 && error.serverErrorText()?.code == "vendor_duplicate" && item.attemptCount > 0) {
                // A retry of a create whose first attempt landed but whose response was lost.
                "{}"
            } else {
                throw error
            }
        }
    }

    /** A feed purchase recorded on the phone; the stored key rides as the backend's Idempotency-Key. */
    /**
     * A vendor edited on the phone (maintainer decision 2026-09-08). The request is the WHOLE row
     * fenced on the row_version the form opened with: a `409 vendor_stale_write` (someone saved
     * first) or a `404` is terminal by [recordFailure]'s `isTerminalAppApiError` check, so it
     * surfaces to the operator rather than retrying against a fence that can never match. A newly
     * recorded voice note resolves to its server proof id here, the create shape; a blank upload id
     * keeps the note the request already references.
     */
    private suspend fun dispatchVendorUpdate(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<VendorUpdatePayload>(item.payloadJson)
        val request = if (payload.voiceNoteOutboxItemId.isBlank()) {
            payload.request
        } else {
            payload.request.copy(voiceNoteProofRef = resolveUploadedProofRef(payload.voiceNoteOutboxItemId))
        }
        val updated = api.updateProcurementVendor(payload.vendorId, item.idempotencyKey, request)
        return syncJson.encodeToString(updated)
    }

    private suspend fun dispatchFeedPurchaseCreate(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<FeedPurchaseCreatePayload>(item.payloadJson)
        val created = api.createFeedPurchase(item.idempotencyKey, payload.request)
        return syncJson.encodeToString(created)
    }

    /**
     * A purchase load recorded on the phone (maintainer decision 2026-09-13); the stored key rides
     * as the backend's Idempotency-Key. `409 load_ref_taken` / `422 validation_failed` are terminal
     * by [recordFailure]'s check and carry the server's own sentence into lastError.
     */
    private suspend fun dispatchAnimalPurchaseLoadCreate(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<AnimalPurchaseLoadCreatePayload>(item.payloadJson)
        val created = api.createAnimalPurchaseLoad(item.idempotencyKey, payload.request)
        return syncJson.encodeToString(created)
    }

    /**
     * One animal recorded inside a load. Every capture resolves through its own coupled
     * PROOF_UPLOAD row on the same draft lane exactly like [dispatchToxinStepComplete]'s clip: the
     * uploads drain first, and each uploaded server proof id lands in `media[slot]` here, never
     * earlier. A capture whose upload is still pending holds the create (retry); one whose upload
     * died holds it as non-retryable, so the form can offer the per-capture retry. The row's
     * STORED key is passed verbatim on a retry. A `422 validation_failed` (the server refusing an
     * answer, naming its question in `field`) is terminal and surfaces the server's own sentence.
     */
    private suspend fun dispatchAnimalPurchaseAnimalCreate(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<AnimalPurchaseAnimalCreatePayload>(item.payloadJson)
        val media = payload.proofOutboxItemIds.mapValues { (_, outboxIds) -> outboxIds.map { resolveUploadedProofRef(it) } }
        val created = api.addAnimalPurchaseAnimal(
            payload.loadId,
            item.idempotencyKey,
            payload.request.copy(media = media),
        )
        return syncJson.encodeToString(created)
    }

    /** A city's morning prices; the stored key rides as the backend's Idempotency-Key. */
    private suspend fun dispatchMarketSurveyRecord(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<MarketSurveyRecordPayload>(item.payloadJson)
        val card = api.recordMarketSurveyCity(payload.cityId, item.idempotencyKey, payload.request)
        return syncJson.encodeToString(card)
    }

    /** A sale recorded on the phone; the stored key rides as the backend's Idempotency-Key. */
    private suspend fun dispatchSalesDealCreate(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<SalesDealCreatePayload>(item.payloadJson)
        val created = api.createSalesDeal(item.idempotencyKey, payload.request)
        return syncJson.encodeToString(created)
    }

    /** A receipt added, changed or removed. All three return the WHOLE updated deal. */
    private suspend fun dispatchSalesDealPaymentWrite(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<SalesDealPaymentPayload>(item.payloadJson)
        val deal = when (payload.op) {
            SalesPaymentOp.CREATE -> api.createSalesDealPayment(
                payload.dealId, item.idempotencyKey, requireNotNull(payload.request) { "payment create carries no body" },
            )
            SalesPaymentOp.UPDATE -> api.updateSalesDealPayment(
                payload.dealId, payload.paymentId, item.idempotencyKey,
                requireNotNull(payload.request) { "payment update carries no body" },
            )
            SalesPaymentOp.DELETE -> api.deleteSalesDealPayment(payload.dealId, payload.paymentId, item.idempotencyKey)
            else -> error("unknown sales payment op ${payload.op}")
        }
        return syncJson.encodeToString(deal)
    }

    /** An instalment, the payment word, corrected values, or the truck reaching. All return the load. */
    private suspend fun dispatchFeedPurchaseEdit(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<FeedPurchaseEditPayload>(item.payloadJson)
        val purchase = when (payload.kind) {
            FeedPurchaseEditKind.PAYMENT -> api.createFeedPurchasePayment(
                payload.purchaseId, item.idempotencyKey,
                requireNotNull(payload.payment) { "feed purchase payment carries no body" },
            )
            FeedPurchaseEditKind.PAYMENT_STATUS -> api.setFeedPurchasePaymentStatus(
                payload.purchaseId, requireNotNull(payload.paymentStatus) { "payment status carries no body" },
            )
            FeedPurchaseEditKind.EDIT -> api.editFeedPurchase(
                payload.purchaseId, requireNotNull(payload.edit) { "feed purchase edit carries no body" },
            )
            FeedPurchaseEditKind.DELIVERY -> api.recordFeedPurchaseDelivery(
                payload.purchaseId, requireNotNull(payload.delivery) { "feed purchase delivery carries no body" },
            )
            else -> error("unknown feed purchase edit kind ${payload.kind}")
        }
        return syncJson.encodeToString(purchase)
    }

    private suspend fun dispatchSalesDealStatusSet(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<SalesDealStatusPayload>(item.payloadJson)
        val deal = api.setSalesDealStatus(payload.dealId, item.idempotencyKey, payload.request)
        return syncJson.encodeToString(deal)
    }

    /**
     * One pipeline or evidence record. The results differ in shape by panel (a lead row, a count,
     * a bare acknowledgement), and no caller decodes them -- each panel re-reads its own bounded
     * list on success -- so this returns an empty result rather than a union nothing consumes.
     */
    private suspend fun dispatchSalesPipelineWrite(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<SalesPipelinePayload>(item.payloadJson)
        val key = item.idempotencyKey
        when (payload.kind) {
            SalesPipelineKind.BUYER_LEAD ->
                api.createSalesBuyerLead(key, requireNotNull(payload.buyerLead) { "buyer lead carries no body" })
            SalesPipelineKind.BUYER_LEAD_STATUS ->
                api.setSalesBuyerLeadStatus(payload.leadId, key, requireNotNull(payload.leadStatus) { "lead status carries no body" })
            SalesPipelineKind.BUYER_LEAD_EDIT ->
                api.updateSalesBuyerLead(payload.leadId, key, requireNotNull(payload.buyerLead) { "buyer lead carries no body" })
            SalesPipelineKind.FPO_LEAD ->
                api.createSalesFpoLead(key, requireNotNull(payload.fpoLead) { "farmer group carries no body" })
            SalesPipelineKind.FPO_LEAD_STATUS ->
                api.setSalesFpoLeadStatus(payload.leadId, key, requireNotNull(payload.leadStatus) { "lead status carries no body" })
            SalesPipelineKind.FPO_LEAD_EDIT ->
                api.updateSalesFpoLead(payload.leadId, key, requireNotNull(payload.fpoLead) { "farmer group carries no body" })
            SalesPipelineKind.BENCHMARK ->
                api.createSalesMarketBenchmark(key, requireNotNull(payload.benchmark) { "market quote carries no body" })
            SalesPipelineKind.SOLD_TAGS ->
                api.createSalesSoldTags(key, requireNotNull(payload.soldTags) { "sold tag list carries no body" })
            SalesPipelineKind.WEIGHT_CHECK ->
                api.createSalesWeightCheck(key, requireNotNull(payload.weightCheck) { "weight check carries no body" })
            else -> error("unknown sales pipeline kind ${payload.kind}")
        }
        return "{}"
    }

    /**
     * Leave requests (docs/features/leave-requests/plan.md). Same stored-key replay contract as
     * every other dispatch here: the raise replays the original request, a withdraw or decision on
     * a request that already moved on is a 409 -- terminal, surfaced once, never retried.
     */
    private suspend fun dispatchLeaveRequestCreate(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<LeaveRequestCreatePayload>(item.payloadJson)
        val response = api.createLeaveRequest(item.idempotencyKey, payload.request)
        return syncJson.encodeToString(response)
    }

    private suspend fun dispatchLeaveRequestWithdraw(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<LeaveRequestWithdrawPayload>(item.payloadJson)
        val response = api.withdrawLeaveRequest(payload.leaveRequestId, item.idempotencyKey)
        return syncJson.encodeToString(response)
    }

    private suspend fun dispatchLeaveDecision(item: OutboxEntity, approve: Boolean): String {
        val payload = syncJson.decodeFromString<LeaveDecisionPayload>(item.payloadJson)
        val response = if (approve) {
            api.approveLeaveRequest(payload.leaveRequestId, item.idempotencyKey, payload.request)
        } else {
            api.rejectLeaveRequest(payload.leaveRequestId, item.idempotencyKey, payload.request)
        }
        return syncJson.encodeToString(response)
    }

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
        // SHIFTING SOP (2026-09-16): the raise card's captures resolve to server proof ids here (a
        // not-yet-uploaded capture suspends the raise on the shared proof lane). A raise with none
        // sends the request exactly as it was queued.
        val request = if (payload.slotProofs.isEmpty()) payload.request
        else payload.request.copy(proofs = resolveSlotProofs(payload.slotProofs))
        val response = api.recordCountsShiftingEvent(item.idempotencyKey, request)
        return syncJson.encodeToString(response)
    }

    private suspend fun dispatchCountsBirth(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<CountsBirthPayload>(item.payloadJson)
        val request = payload.capture?.let { payload.request.copy(sopCapture = resolveCountsCapture(it)) } ?: payload.request
        val response = api.recordCountsBirthEvent(item.idempotencyKey, request)
        return syncJson.encodeToString(response)
    }

    private suspend fun dispatchCountsDeath(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<CountsDeathPayload>(item.payloadJson)
        val request = payload.capture?.let { payload.request.copy(sopCapture = resolveCountsCapture(it)) } ?: payload.request
        val response = api.recordCountsDeathEvent(item.idempotencyKey, request)
        return syncJson.encodeToString(response)
    }

    /** The capture card extras with every slot resolved to its server proof id (a pending upload
     *  holds the report back, exactly as a feed card's completion waits). */
    private suspend fun resolveCountsCapture(capture: CountsCapturePayload): CountsSopCaptureDto =
        CountsSopCaptureDto(
            sopVersionId = capture.sopVersionId?.takeIf { it.isNotBlank() },
            proofs = resolveSlotProofs(capture.slotProofs),
            answers = capture.answers,
        )

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
        if (payload.slotProofs.isNotEmpty()) {
            // SHIFTING SOP (2026-09-16): every capture of the pinned card resolves to its server proof
            // id; `proofs` + `answers` are the submission, and the legacy columns ride along as the
            // seeded slots' mirrors (proof_ref falls back to the first capture, as the server does).
            val proofs = resolveSlotProofs(payload.slotProofs)
            val answers = payload.answers?.takeIf { it.isNotEmpty() }
            // ROLLOUT ORDER (maintainer rule): a SEEDED card with no answers is exactly the pre-SOP
            // submission, so it goes on the wire as the legacy triple ALONE. A backend that predates
            // the shifting SOP decodes strictly and refuses unknown `proofs`/`answers` keys; the SOP
            // backend folds the seeded slots back onto the same triple, so both hash one completion.
            val seededOnly = answers == null &&
                SEEDED_SHIFTING_VIDEO_SLOT in proofs &&
                proofs.keys.all { it in SEEDED_SHIFTING_SLOTS }
            val response = api.completeCountsShiftingEvent(
                payload.shiftingEventId,
                item.idempotencyKey,
                payload.destinationTag,
                proofs[SEEDED_SHIFTING_VIDEO_SLOT] ?: proofs.values.firstOrNull()
                    ?: throw NonRetryableSyncException("Shifting completion is missing its captures."),
                proofs[SEEDED_SHIFTING_PACKING_SLOT],
                proofs[SEEDED_SHIFTING_FEEDING_SLOT],
                payload.feedConfigFingerprint,
                proofs.takeUnless { seededOnly },
                answers,
            )
            return syncJson.encodeToString(response)
        }
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

    /**
     * The Pen Reconciliation "Mark done" (docs/decisions/pen-reconciliation.md). Same
     * idempotent-replay contract as every other `dispatch*` — the row's STORED key is passed
     * through verbatim, so a server-committed-but-client-unrecorded retry returns the original
     * result (idempotent_replay=true) instead of queueing a second verification. The mandatory
     * video's proof_id is resolved from the coupled PROOF_UPLOAD row (same group, drained first);
     * a missing coupling is terminal — a pen return without a verifiable video must not reach the
     * backend. Completing a card that is no longer actionable (already submitted or completed) is
     * a 400 — terminal by [recordFailure]'s `isTerminalAppApiError` check, so it surfaces to the
     * operator instead of being retried against a state that will never change.
     */
    private suspend fun dispatchPenReconciliationComplete(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<PenReconciliationCompletePayload>(item.payloadJson)
        val proofItemId = payload.proofOutboxItemId
            ?: throw NonRetryableSyncException("This pen return is missing its required video. Please record it again.")
        val response = api.completeCountsPenReconciliationCard(
            payload.cardId,
            item.idempotencyKey,
            resolveUploadedProofRef(proofItemId),
        )
        return syncJson.encodeToString(response)
    }

    /**
     * ONE shed's feed & water removal submit (maintainer correction #2, 2026-09-03: the submit
     * is PER SHED — `POST /app/weighing/fasting/{fasting_task_id}/sheds/{campaign_shed_id}/submit`).
     * Same idempotent-replay contract as every other `dispatch*` — the row's STORED key rides the
     * Idempotency-Key header verbatim on every attempt. Each clip resolves from its referenced
     * PROOF_UPLOAD row by id: a not-yet-uploaded clip suspends the WHOLE submit on the shared
     * proof-dependency lane WITHOUT burning retry budget, while a missing coupling or a
     * permanently-failed upload is terminal — a gated submit without this shed's two verifiable
     * videos must not reach the backend, which would only re-refuse it with the proof-shaped 422s
     * or a `weighing_rejected_proof_reuse` (terminal by [recordFailure]'s check, surfaced with the
     * server's own sentence). A `409` already-submitted refusal is likewise terminal server truth.
     */
    private suspend fun dispatchWeighingFastingSubmit(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<WeighingFastingSubmitPayload>(item.payloadJson)
        // The card's captures: the SOP-keyed map (WEIGHING SOP) or, on a row queued before slots
        // existed, the legacy pair mapped onto the seeded keys. A row with no capture at all can
        // never satisfy the submit — terminal with a farm-worded reason.
        val proofItems: Map<String, String> = payload.proofOutboxItems.ifEmpty {
            buildMap {
                payload.feedProofOutboxItemId?.takeIf { it.isNotBlank() }?.let { put("feed_video", it) }
                payload.waterProofOutboxItemId?.takeIf { it.isNotBlank() }?.let { put("water_video", it) }
            }
        }
        if (payload.campaignShedId.isBlank() || proofItems.isEmpty()) {
            throw NonRetryableSyncException("This pen's removal needs its captures. Please record them again.")
        }
        // Each fresh capture resolves through its own PROOF_UPLOAD row — pending suspends the
        // WHOLE submit, permanently-failed terminalizes it.
        val proofs = proofItems.mapValues { (_, outboxId) -> resolveUploadedProofRef(outboxId) }
        val response = api.submitWeighingFastingShed(
            payload.fastingTaskId,
            payload.campaignShedId,
            item.idempotencyKey,
            sg.mesha.goatos.core.network.dto.SubmitWeighingFastingShedRequestDto(
                // The seeded slots also ride the legacy fields for an older server.
                feedProofRef = proofs["feed_video"],
                waterProofRef = proofs["water_video"],
                answers = payload.answers,
                proofs = proofs,
            ),
        )
        return syncJson.encodeToString(response)
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
        // FEED SOP (2026-09-16): a row queued against the CARD carries {slot key: source}; every
        // source resolves to a server proof id (a teammate's ref verbatim, this phone's upload once
        // it has finished). The three fixed fields mirror the seeded slots for an older backend and
        // are blank when the card no longer has that slot. A row queued by an older build carries
        // no slot map and resolves the fixed trio exactly as before.
        val proofs = resolveSlotProofs(payload.slotProofs)
        val cardShaped = payload.slotProofs.isNotEmpty()
        val response = api.completeFeedDistribution(
            item.idempotencyKey,
            FeedDistributionCompleteRequestDto(
                parkId = payload.parkId,
                shedId = payload.shedId,
                partitionLabel = payload.partitionLabel,
                sessionNo = payload.sessionNo,
                targetDate = payload.targetDate,
                workflow = payload.workflow,
                feedWeightProofRef = if (cardShaped) proofs[FEED_SLOT_WEIGHT_PHOTO].orEmpty() else resolveFeedProofRef(
                    payload.feedWeightProofRef,
                    payload.feedWeightProofOutboxItemId,
                    "This feeding needs a feed weight photo. Please record this shed's feeding again.",
                ),
                distributionProofRef = if (cardShaped) proofs[FEED_SLOT_FEED_VIDEO].orEmpty() else resolveFeedProofRef(
                    payload.distributionProofRef,
                    payload.distributionProofOutboxItemId,
                    "This feeding needs a feed video. Please record this shed's feeding again.",
                ),
                waterProofRef = if (cardShaped) proofs[FEED_SLOT_WATER_VIDEO].orEmpty() else resolveFeedProofRef(
                    payload.waterProofRef,
                    payload.waterProofOutboxItemId,
                    "This feeding needs a water video. Please record this shed's feeding again.",
                ),
                proofs = proofs,
                answers = payload.answers,
            ),
        )
        return syncJson.encodeToString(response)
    }

    /**
     * Resolves a SOP card's {slot key: source} (feed cards, herd capture cards) to
     * {slot key: server proof id}. A slot whose upload
     * is still pending raises [ProofDependencyPendingException] (the completion waits, exactly as
     * the fixed trio did); a slot whose upload failed permanently is terminal.
     */
    private suspend fun resolveSlotProofs(sources: Map<String, FeedSlotProofSourcePayload>): Map<String, String> {
        if (sources.isEmpty()) return emptyMap()
        val out = LinkedHashMap<String, String>(sources.size) // mobile-guard:ignore: bounded by the card's slot count (<= 12 per SOP); local to one dispatch
        for ((slotKey, source) in sources) {
            val ref = source.proofRef?.takeIf { it.isNotBlank() }
                ?: source.outboxItemId?.takeIf { it.isNotBlank() }?.let { resolveUploadedProofRef(it) }
                ?: continue
            out[slotKey] = ref
        }
        return out
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
                packingProofRef = legacyFeedSlotRef(payload.slotProofs, FEED_SLOT_PACKING_VIDEO, payload.packingProofOutboxItemId),
                proofs = resolveSlotProofs(payload.slotProofs),
                answers = payload.answers,
            ),
        )
        return syncJson.encodeToString(response)
    }

    /**
     * FEED SOP: the legacy single-proof field of a packing/wastage/transport request. A row queued
     * against the CARD mirrors its seeded slot there (blank when the card dropped that slot); a row
     * queued by an older build resolves its one coupled upload exactly as before.
     */
    private suspend fun legacyFeedSlotRef(slotProofs: Map<String, FeedSlotProofSourcePayload>, seededKey: String, legacyOutboxItemId: String): String =
        if (slotProofs.isNotEmpty()) resolveSlotProofs(slotProofs)[seededKey].orEmpty()
        else resolveUploadedProofRef(legacyOutboxItemId)

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
                wastageProofRef = legacyFeedSlotRef(payload.slotProofs, FEED_SLOT_WASTAGE_VIDEO, payload.wastageProofOutboxItemId),
                proofs = resolveSlotProofs(payload.slotProofs),
                answers = payload.answers,
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
        // A ROUND-grain feed & water removal stores its evidence PER PEN, so it dispatches to
        // the pen route with the pen named by the work task it gates. Anything else is the
        // ordinary task-level slot and takes the path it always took.
        if (payload.gatedTaskId.isNotBlank()) {
            api.putPcCareRemovalPenProof(
                payload.taskId,
                payload.slotFieldKey,
                item.idempotencyKey,
                sg.mesha.goatos.core.network.dto.PcCareRemovalPenProofRequestDto(
                    gatedTaskId = payload.gatedTaskId,
                    proofRef = resolveUploadedProofRef(payload.proofOutboxItemId),
                ),
            )
            return "{}"
        }
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
        val response = api.submitPcCareTask(payload.taskId, item.idempotencyKey, payload.answers)
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

    /**
     * The pen-visit submit (maintainer decision 2026-09-07): the visit's one video, resolved
     * through its coupled PROOF_UPLOAD row on the same task group exactly like
     * [dispatchToxinStepComplete]'s clip. The row's STORED key is passed verbatim on a retry.
     *
     * Three server answers are handled here rather than left to [recordFailure]:
     *  - `409 already_submitted` — the visit is already done (an earlier attempt landed but the
     *    phone never heard back, or a second phone submitted). That is SUCCESS: the fresh task is
     *    re-fetched and returned as this row's result so the reconcile flips the card to done.
     *  - `409 stale_task` — the task moved under the row version the screen rendered. The task
     *    is re-read; a visit already completed is success as above, otherwise the submit is
     *    retried ONCE under the FRESH row version and its own (task, row_version) key from
     *    PenVisitPayloads.kt — a genuinely new act, never the stored key with a different body.
     *    A second refusal propagates and is terminal.
     *  - `422 invalid_proof` / `proof_required` — terminal by [recordFailure]'s
     *    `isTerminalAppApiError` check, carrying the server's own sentence; the card reads
     *    "Record again" and the terminal hook re-reads the task.
     */
    private suspend fun dispatchPenVisitSubmit(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<PenVisitSubmitPayload>(item.payloadJson)
        val proofRef = resolveUploadedProofRef(payload.proofOutboxItemId)
        val response = try {
            api.submitPenVisit(
                item.idempotencyKey,
                payload.taskId,
                sg.mesha.goatos.core.network.dto.PenVisitSubmitRequestDto(proofRef = proofRef, rowVersion = payload.rowVersion),
            )
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            when {
                error.appApiStatusCode() == 409 && error.serverErrorText()?.code == PEN_VISIT_ALREADY_SUBMITTED ->
                    api.getPenVisit(payload.taskId)
                error.appApiStatusCode() == 409 && error.serverErrorText()?.code == PEN_VISIT_STALE_TASK -> {
                    val fresh = api.getPenVisit(payload.taskId)
                    if (fresh.task.workState == PEN_VISIT_STATE_COMPLETED || !fresh.task.canSubmit) {
                        fresh
                    } else {
                        api.submitPenVisit(
                            penVisitSubmitIdempotencyKey(payload.taskId, fresh.task.rowVersion),
                            payload.taskId,
                            sg.mesha.goatos.core.network.dto.PenVisitSubmitRequestDto(
                                proofRef = proofRef,
                                rowVersion = fresh.task.rowVersion,
                            ),
                        )
                    }
                }
                else -> throw error
            }
        }
        return syncJson.encodeToString(response)
    }

    /**
     * The pen-routine presence punch (maintainer instruction 2026-09-16): the `enter` check-in
     * with the location/integrity block captured at tap time. The row's STORED key is passed
     * verbatim on a retry. Three server answers are handled here rather than left to
     * [recordFailure], the pen-visit shape:
     *  - `409 already_done` / `409 in_review` — the task moved on without this punch (another
     *    assignee finished it, or an earlier attempt landed and the phone never heard back). That
     *    is SUCCESS: the fresh task is re-fetched and returned so the reconcile shows its state.
     *  - `409 version_conflict` — the task moved under the row version the screen rendered. The
     *    task is re-read; one that no longer offers a check-in is success as above, otherwise the
     *    punch is retried ONCE under the FRESH row version and its own key from
     *    PenRoutinePayloads.kt — a genuinely new act, never the stored key with a different body.
     *    A second refusal propagates and is terminal.
     *  - every other 4xx (`403 not_assignee`, a 422) — terminal by [recordFailure]'s
     *    `isTerminalAppApiError` check, carrying the server's own sentence; the terminal hook
     *    re-reads the task.
     */
    private suspend fun dispatchPenRoutinePresence(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<PenRoutinePresencePayload>(item.payloadJson)
        fun request(rowVersion: Int) = sg.mesha.goatos.core.network.dto.PenRoutinePresenceRequestDto(
            eventType = payload.eventType,
            capturedAt = payload.capturedAt,
            rowVersion = rowVersion,
            location = payload.location,
            integrity = payload.integrity,
        )
        val response = try {
            api.recordPenRoutinePresence(item.idempotencyKey, payload.taskId, request(payload.rowVersion))
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            val code = error.serverErrorText()?.code
            when {
                error.appApiStatusCode() == 409 && code in PEN_ROUTINE_MOVED_ON_CODES ->
                    api.getPenRoutine(payload.taskId)
                error.appApiStatusCode() == 409 && code == PEN_ROUTINE_VERSION_CONFLICT -> {
                    val fresh = api.getPenRoutine(payload.taskId)
                    if (!fresh.task.canCheckIn || fresh.task.inPen) {
                        fresh
                    } else {
                        api.recordPenRoutinePresence(
                            penRoutinePresenceIdempotencyKey(payload.taskId, payload.eventType, fresh.task.rowVersion),
                            payload.taskId,
                            request(fresh.task.rowVersion),
                        )
                    }
                }
                else -> throw error
            }
        }
        return syncJson.encodeToString(response)
    }

    /**
     * The pen-routine submit (maintainer instruction 2026-09-16): the answers plus every capture,
     * each resolved through its coupled PROOF_UPLOAD row on the same task group exactly like
     * [dispatchPenVisitSubmit]'s clip. Same 409 handling as [dispatchPenRoutinePresence]; a
     * `422 presence_missing` / `proof_count` / `answer_invalid` / `invalid_proof` is terminal by
     * [recordFailure], carries the server's sentence, and the terminal hook re-reads the task so
     * the form re-opens beside it.
     */
    private suspend fun dispatchPenRoutineSubmit(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<PenRoutineSubmitPayload>(item.payloadJson)
        val proofRefs = payload.proofs.map { proof ->
            sg.mesha.goatos.core.network.dto.PenRoutineProofDto(
                ref = resolveUploadedProofRef(proof.proofOutboxItemId),
                kind = proof.kind,
                questionId = proof.questionId,
            )
        }
        fun request(rowVersion: Int) = sg.mesha.goatos.core.network.dto.PenRoutineSubmitRequestDto(
            answers = payload.answers,
            proofRefs = proofRefs,
            rowVersion = rowVersion,
            capturedAt = payload.capturedAt,
            location = payload.location,
            integrity = payload.integrity,
        )
        val response = try {
            api.submitPenRoutine(item.idempotencyKey, payload.taskId, request(payload.rowVersion))
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            val code = error.serverErrorText()?.code
            when {
                error.appApiStatusCode() == 409 && code in PEN_ROUTINE_MOVED_ON_CODES ->
                    api.getPenRoutine(payload.taskId)
                error.appApiStatusCode() == 409 && code == PEN_ROUTINE_VERSION_CONFLICT -> {
                    val fresh = api.getPenRoutine(payload.taskId)
                    if (fresh.task.workState == PEN_VISIT_STATE_COMPLETED || !fresh.task.canSubmit) {
                        fresh
                    } else {
                        api.submitPenRoutine(
                            penRoutineSubmitIdempotencyKey(payload.taskId, fresh.task.rowVersion),
                            payload.taskId,
                            request(fresh.task.rowVersion),
                        )
                    }
                }
                else -> throw error
            }
        }
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

    private suspend fun dispatchFeedTransportSubmit(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<FeedTransportSubmitPayload>(item.payloadJson)
        val response = api.submitFeedTransport(
            payload.taskId,
            item.idempotencyKey,
            FeedTransportSubmitRequestDto(
                proofRef = legacyFeedSlotRef(payload.slotProofs, FEED_SLOT_TRANSPORT_VIDEO, payload.proofOutboxItemId),
                proofs = resolveSlotProofs(payload.slotProofs),
                answers = payload.answers,
            ),
        )
        return syncJson.encodeToString(response)
    }

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
        val response = settleWorkflowWriteTheServerHolds(payload.workflowId, payload.actionId) {
            api.answerWorkflowAction(
                payload.workflowId,
                payload.actionId,
                item.idempotencyKey,
                WorkflowActionAnswerRequestDto(
                    answerValue = payload.answerValue,
                    proofRef = payload.proofOutboxItemId?.let { resolveUploadedProofRef(it) },
                    proofs = resolveWorkflowProofs(payload.proofOutboxItems),
                ),
            )
        }
        return syncJson.encodeToString(response)
    }

    /**
     * A step write refused `409` (`action_already_completed` / `action_in_review`, or a key the
     * server already holds under another payload) for a step the SERVER SHOWS RECORDED is done, not
     * a conflict. Parking it as a dead conflict row held the workflow's whole outbox lane: the next
     * step's proof upload and answers sat queued and the card stayed stuck (Realme E2E 2026-09-17).
     * The step is re-read, never assumed: a 409 for a step still open stays a visible conflict.
     */
    private suspend fun settleWorkflowWriteTheServerHolds(
        workflowId: String,
        actionId: String,
        write: suspend () -> WorkflowActionWriteResponseDto,
    ): WorkflowActionWriteResponseDto = try {
        write()
    } catch (error: Exception) {
        if (error is CancellationException) throw error
        if (error.appApiStatusCode() != 409) throw error
        val refusedCode = error.serverErrorText()?.code
        val duplicate = refusedCode == WORKFLOW_REFUSED_ALREADY_COMPLETED || refusedCode == WORKFLOW_REFUSED_IN_REVIEW
        workflowStepTheServerHolds(workflowId, actionId, supersededDuplicate = duplicate) ?: throw error
    }

    /**
     * The server's own record of a step when it is already recorded (completed / in review), or --
     * for a [supersededDuplicate] -- when that recording has since been sent back for rework.
     */
    private suspend fun workflowStepTheServerHolds(
        workflowId: String,
        actionId: String,
        supersededDuplicate: Boolean = false,
    ): WorkflowActionWriteResponseDto? {
        val detail = api.getWorkflow(workflowId)
        val step = detail.actions.firstOrNull { it.actionId == actionId } ?: return null
        val recorded = step.status == WORKFLOW_STEP_COMPLETED || step.status == WORKFLOW_STEP_IN_REVIEW
        if (!recorded && !(supersededDuplicate && step.status == WORKFLOW_STEP_REWORK)) return null
        return WorkflowActionWriteResponseDto(
            workflowId = workflowId,
            actionId = actionId,
            status = step.status,
            actionsDone = detail.actionsDone,
            actionsTotal = detail.actionsTotal,
            awaitingVerification = step.status == WORKFLOW_STEP_IN_REVIEW,
            completedAt = step.completedAt,
            idempotentReplay = true,
        )
    }

    /**
     * Recovery for phones that already hold a dead-letter step write from an older build: a
     * conflict WORKFLOW_ACTION_COMPLETE / ANSWER row whose step the server shows recorded is settled
     * as succeeded so it stops holding its lane. Bounded per pass and tried once per process per row.
     */
    private suspend fun settleConflictedWorkflowWritesTheServerHolds(rememberRetryDue: (Long) -> Unit) {
        store.findConflictedWorkflowActionWrites(SUCCESS_RECONCILE_LIMIT).forEach { row ->
            if (!settleAttemptedRows.add(row.id)) return@forEach
            try {
                val (workflowId, actionId) = when (row.opType) {
                    OutboxOpType.WORKFLOW_ACTION_COMPLETE.name ->
                        syncJson.decodeFromString<WorkflowActionCompletePayload>(row.payloadJson).let { it.workflowId to it.actionId }
                    else ->
                        syncJson.decodeFromString<WorkflowActionAnswerPayload>(row.payloadJson).let { it.workflowId to it.actionId }
                }
                val held = workflowStepTheServerHolds(workflowId, actionId, supersededDuplicate = row.refusedAsAlreadyRecorded())
                    ?: return@forEach
                store.settleConflictAsSucceeded(row.id, syncJson.encodeToString(held), clock())
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                // Offline / server error: forget the attempt so a later pass can try again, and book
                // that pass. Nothing else would: the rows this conflict holds are not eligible, so the
                // drain schedules no retry and the lane waited for the next app launch (Realme
                // 2026-09-17).
                settleAttemptedRows.remove(row.id)
                reportCacheReconcileFailure(row, error)
                rememberRetryDue(clock() + backoff.delayMillis(1))
            }
        }
    }

    /**
     * The server refused this step write because it ALREADY HELD a recording of the step: the write
     * was a duplicate of an accepted one. Such a write can never matter again -- once that recording
     * is reviewed and sent back for rework, re-sending the duplicate would re-submit the rejected
     * proofs -- so it must not hold the lane in front of the re-shoot.
     */
    private fun OutboxEntity.refusedAsAlreadyRecorded(): Boolean =
        lastErrorCode == WORKFLOW_REFUSED_ALREADY_COMPLETED ||
            lastErrorCode == WORKFLOW_REFUSED_IN_REVIEW ||
            (lastErrorCode.isNullOrBlank() && lastError?.trim() == WORKFLOW_REFUSED_ALREADY_COMPLETED_TEXT)

    /** Resolves every queued proof of a multi-proof step to its uploaded server id, in capture
     *  order; null when the step carried none (a one-video step keeps using proof_ref). */
    private suspend fun resolveWorkflowProofs(items: List<WorkflowProofOutboxRef>): List<WorkflowProofItemDto>? {
        if (items.isEmpty()) return null
        // A step carries each proof once: a row queued by a build that appended one capture twice
        // must not send the same proof twice.
        return items.map {
            WorkflowProofItemDto(
                ref = it.proofRef.takeIf { ref -> ref.isNotBlank() } ?: resolveUploadedProofRef(it.outboxItemId),
                kind = it.kind,
            )
        }.distinctBy { it.ref }
    }

    private suspend fun dispatchWorkflowActionComplete(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<WorkflowActionCompletePayload>(item.payloadJson)
        val response = settleWorkflowWriteTheServerHolds(payload.workflowId, payload.actionId) {
            api.completeWorkflowAction(
                payload.workflowId,
                payload.actionId,
                item.idempotencyKey,
                WorkflowActionCompleteRequestDto(
                    proofRef = payload.proofOutboxItemId?.let { resolveUploadedProofRef(it) },
                    proofs = resolveWorkflowProofs(payload.proofOutboxItems),
                ),
            )
        }
        return syncJson.encodeToString(response)
    }

    /**
     * ONE STEP'S VIDEO, registered on its own.
     *
     * Separate from the session submit because the blob reaching storage is not the business
     * fact. If THIS fails after the upload succeeded, only this row retries -- with the proof id
     * the upload already produced, never the video again.
     */
    private suspend fun dispatchHealthStepProofRegister(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<HealthStepProofRegisterPayload>(item.payloadJson)
        if (store.hasNewerHealthStepProofRegister(item.id, item.groupKey, payload.healthSessionStepId, item.createdAt)) {
            return "{}"
        }
        api.registerHealthStepProof(
            payload.healthSessionId,
            payload.healthSessionStepId,
            item.idempotencyKey,
            sg.mesha.goatos.core.network.dto.HealthStepProofRequestDto(
                proofRef = resolveUploadedProofRef(payload.proofOutboxItemId),
            ),
        )
        return "{}"
    }

    private suspend fun dispatchHealthTreatmentComplete(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<HealthTreatmentCompletePayload>(item.payloadJson)
        // New rows carry the PROOF_UPLOAD outbox reference (same group, drains first) and the
        // uploaded proof id is resolved here; a legacy already-queued row falls back to its
        // literal proofRef ("" for the pre-video builds), which the backend still accepts.
        val proofRef = payload.proofOutboxItemId
            .takeIf { it.isNotBlank() }
            ?.let { resolveUploadedProofRef(it) }
            ?: payload.proofRef
        val response = api.completeHealthWorkItem(
            healthSessionId = payload.healthSessionId,
            idempotencyKey = item.idempotencyKey,
            request = HealthCompleteRequestDto(proofRef = proofRef),
        )
        return syncJson.encodeToString(response)
    }

    private suspend fun dispatchHealthCaseClose(item: OutboxEntity): String {
        val payload = syncJson.decodeFromString<HealthCaseClosePayload>(item.payloadJson)
        val response = api.closeHealthCase(
            healthCaseId = payload.healthCaseId,
            idempotencyKey = item.idempotencyKey,
            request = HealthCloseCaseRequestDto(
                outcome = payload.outcome,
                note = payload.note.takeIf { it.isNotBlank() },
            ),
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
                answers = payload.answers,
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
        private const val WORKFLOW_STEP_COMPLETED = "completed"
        private const val WORKFLOW_STEP_IN_REVIEW = "in_review"
        private const val WORKFLOW_STEP_REWORK = "rework"
        /** backend tasks/adapters/http handler: a step write refused because the step is recorded. */
        private const val WORKFLOW_REFUSED_ALREADY_COMPLETED = "action_already_completed"
        private const val WORKFLOW_REFUSED_IN_REVIEW = "action_in_review"
        private const val WORKFLOW_REFUSED_ALREADY_COMPLETED_TEXT = "this action is already completed"
        // Max rows pulled into memory per drain iteration. A long offline backlog drains in
        // successive batches of this size rather than one unbounded SELECT * materialization.
        const val DRAIN_BATCH_SIZE = 200
        const val NO_RETRY_DUE = Long.MAX_VALUE
        const val PROOF_DEPENDENCY_WAIT_RETRY_MS = 1_000L
        /** Pen-visit wire codes this engine reads (backend/internal/penvisits/app/errors.go). */
        private const val PEN_VISIT_ALREADY_SUBMITTED = "already_submitted"
        private const val PEN_VISIT_STALE_TASK = "stale_task"
        private const val PEN_VISIT_STATE_COMPLETED = "completed"
        /** Pen-routine wire codes this engine reads (backend/internal/penroutines, the Step contract). */
        // The backend's codes (penroutines/app/errors.go): a stale row version is stale_task; a
        // check that moved on without us is already_submitted / task_in_review / task_cancelled.
        private const val PEN_ROUTINE_VERSION_CONFLICT = "stale_task"
        private val PEN_ROUTINE_MOVED_ON_CODES = setOf("already_submitted", "task_in_review", "task_cancelled")
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

/**
 * A tiny insertion-ordered set with a hard capacity: the oldest key is evicted when a new one
 * would exceed [capacity]. Synchronized because the drain and the per-item success path can run
 * on different IO threads.
 */
internal class BoundedKeySet(private val capacity: Int) {
    private val keys = LinkedHashSet<String>()

    /** Adds [key]; returns false when it was already present. */
    @Synchronized
    fun add(key: String): Boolean {
        if (!keys.add(key)) return false
        if (keys.size > capacity) {
            val oldest = keys.iterator()
            oldest.next()
            oldest.remove()
        }
        return true
    }

    @Synchronized
    fun remove(key: String) {
        keys.remove(key)
    }

    @Synchronized
    fun contains(key: String): Boolean = key in keys
}

/** The seeded distribution slot keys (the proof register field keys the phones have always stamped). */
internal const val FEED_SLOT_WEIGHT_PHOTO = "feed_distribution_feed_weight_photo"
internal const val FEED_SLOT_FEED_VIDEO = "feed_distribution_video"
internal const val FEED_SLOT_WATER_VIDEO = "feed_distribution_water_video"
internal const val FEED_SLOT_PACKING_VIDEO = "feed_packing_video"
internal const val FEED_SLOT_WASTAGE_VIDEO = "feed_wastage_video"
internal const val FEED_SLOT_TRANSPORT_VIDEO = "feed_transport_video"

// SHIFTING SOP (2026-09-16): the seeded slot keys of the shifting cards -- the proof register field
// keys the phones have always stamped -- which the legacy completion columns mirror.
private const val SEEDED_SHIFTING_VIDEO_SLOT = "shifting_shifting_video"
private const val SEEDED_SHIFTING_PACKING_SLOT = "shifting_packing_video"
private const val SEEDED_SHIFTING_FEEDING_SLOT = "shifting_feeding_video"
private val SEEDED_SHIFTING_SLOTS = setOf(SEEDED_SHIFTING_VIDEO_SLOT, SEEDED_SHIFTING_PACKING_SLOT, SEEDED_SHIFTING_FEEDING_SLOT)
