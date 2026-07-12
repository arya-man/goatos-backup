package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.flow.first
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType

/**
 * Framework-free drain loop behind the background upload foreground service (MOB-002,
 * `docs/mobile/proof-capture-sync-and-e2e.md` §3). `sg.mesha.goatos.sync.UploadForegroundService`
 * (`:app`) is a thin Android `Service` shim around THIS class — exactly like
 * `sg.mesha.goatos.sync.SyncWorker` is a thin `CoroutineWorker` shim around [SyncEngine]. No
 * dispatch/drain logic is duplicated here: every pass delegates to the SAME
 * [SyncEngine.drainOnce] the in-process [ConnectivitySyncTrigger] and the WorkManager backstop
 * use, so a submission/proof-upload row is drained (and — via [SyncEngine.drainOnce]'s own
 * [OutboxStore.reclaimInFlight] — a crash/kill-mid-upload row is reclaimed) through ONE code
 * path regardless of which trigger woke it up.
 *
 * [RELEVANT_OP_TYPES] scopes the coordinator to the writes worth a visible "uploading" progress
 * notification — proof-video registrations and shed submissions (which carry the drive's proof
 * references). Leadership-only writes ([OutboxOpType.VERIFY_TASK]/[OutboxOpType.REWORK_TASK])
 * and reschedules never start the foreground service; they still drain via the connectivity
 * trigger / WorkManager backstop, just without a visible upload notification.
 */
class UploadSyncCoordinator(
    private val engine: SyncEngine,
    private val store: OutboxStore,
    /** Bounds the number of drain passes a single service invocation will run — this is a
     *  liveness guard (never spin the foreground service forever), not a correctness guard: if
     *  the cap is hit while work remains, the pass simply ends [Outcome.InProgress] and the next
     *  trigger (another enqueue's [ForegroundSyncController.ensureRunning], reconnect, or the
     *  WorkManager backstop) resumes exactly where the durable Room queue left off. */
    private val maxPasses: Int = 20,
) {
    /** Result of a full [run] invocation — tells the service what to show and whether to keep
     *  itself alive (never — the service always stops after [run] returns; only WHERE it left
     *  off differs). */
    sealed interface Outcome {
        /** Nothing upload-relevant was queued (or everything finished) — no notification needed
         *  beyond a transient "done" state; the service should stop immediately. */
        data object Idle : Outcome

        /** The pass hit [maxPasses] while rows were still actively draining (real progress was
         *  being made each pass) — a lot of backlog, not a stall. [remaining]/[total] describe
         *  the last-seen snapshot for the final notification before the service yields. */
        data class InProgress(val remaining: Int, val total: Int) : Outcome

        /** A pass made no further progress — either offline ([SyncEngine.drainOnce] returned
         *  `false`) or every remaining row is now backed off — so there is nothing more this
         *  invocation can do. [remaining] rows are still durably queued in Room; a reconnect,
         *  the next enqueue, or the WorkManager retry work resumes them later. */
        data class Waiting(val remaining: Int) : Outcome
    }

    /**
     * Repeatedly calls [SyncEngine.drainOnce] until the [RELEVANT_OP_TYPES] active-row count
     * reaches zero, stalls (no progress from one pass to the next), or [maxPasses] is hit.
     *
     * [onProgress] is invoked after EVERY pass — including the very first, before any network
     * call — with `(done, total)` where [Outcome] tallies against `total`'s HIGH-WATER MARK
     * across the run (never shrinks even if a NEW row is enqueued mid-run, e.g. the operator
     * captures another video while a drive is uploading), so the service's "Uploading X/Y…"
     * notification never regresses or shows an impossible fraction.
     */
    suspend fun run(onProgress: suspend (done: Int, total: Int) -> Unit = { _, _ -> }): Outcome {
        var totalSeen = relevantActiveRows().size
        if (totalSeen == 0) {
            onProgress(0, 0)
            return Outcome.Idle
        }
        var previousActive = Int.MAX_VALUE
        repeat(maxPasses) {
            val completedOnline = engine.drainOnce()
            val active = relevantActiveRows().size
            totalSeen = maxOf(totalSeen, active)
            onProgress((totalSeen - active).coerceAtLeast(0), totalSeen)
            if (active == 0) return Outcome.Idle
            // A pass either failed to even start (offline) or made no dent versus the prior
            // pass (every remaining row is backed off within its retry window) — no point
            // spinning the service; let a later trigger resume.
            if (!completedOnline || active >= previousActive) return Outcome.Waiting(active)
            previousActive = active
        }
        return Outcome.InProgress(relevantActiveRows().size, totalSeen)
    }

    // store.observeActive() is ALREADY scoped to QUEUED/IN_FLIGHT/non-conflict-FAILED rows (see
    // OutboxDao.observeActive) — this only narrows further by op type.
    private suspend fun relevantActiveRows(): List<OutboxEntity> =
        store.observeActive().first().filter { OutboxOpType.valueOf(it.opType) in RELEVANT_OP_TYPES }

    companion object {
        /** Op types that represent an "upload" worth a visible progress notification. */
        val RELEVANT_OP_TYPES: Set<OutboxOpType> = setOf(OutboxOpType.PROOF_UPLOAD, OutboxOpType.SHED_SUBMIT)
    }
}
