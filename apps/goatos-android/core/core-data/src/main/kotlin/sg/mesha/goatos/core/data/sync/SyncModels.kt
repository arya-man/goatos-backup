package sg.mesha.goatos.core.data.sync

/** Mirrors [sg.mesha.goatos.core.database.outbox.OutboxStatus] one-to-one. Kept as a
 *  separate core-data-level type so consumers (ViewModels, and eventually the sync-status
 *  overlay) never need a direct dependency on `core-database`'s Room types — only on this
 *  port's models (module boundary: `feature-*`/`:app` -> `core-*`, never straight to Room). */
enum class SyncItemStatus { QUEUED, IN_FLIGHT, SUCCEEDED, FAILED }

/**
 * One queued write, as the UI/ViewModel layer sees it — the per-item shape inside
 * [SyncStatus.items] that a sync-status sheet lists (TRD §6: "a sync status sheet listing
 * each queued shed record with its state, a per-item progress bar, and a retry affordance
 * on failure").
 */
data class SyncQueueItem(
    val id: String,
    val opType: String,
    val groupKey: String,
    val status: SyncItemStatus,
    val attemptCount: Int,
    val maxAttempts: Int,
    /** true = the last attempt was a definitive server rejection (e.g. failed submission
     *  validation) — retrying without changing the payload will not help. false + [status]
     *  == [SyncItemStatus.FAILED] means a transport/backoff failure that will auto-retry
     *  until [isDeadLetter]. */
    val conflict: Boolean,
    val createdAt: Long,
    val updatedAt: Long,
    val lastError: String?,
) {
    /** Terminal, non-retryable TRANSPORT failure — the TRD's "dead-letter after N attempts,
     *  visible + actionable" state. A [conflict] item is terminal for a different reason
     *  (business rejection, not exhausted retries) and is reported separately so the UI can
     *  tell the two apart (e.g. render CONFLICT vs DEAD_LETTER banners). */
    val isDeadLetter: Boolean get() = status == SyncItemStatus.FAILED && !conflict && attemptCount >= maxAttempts
}

/**
 * The public sync-status snapshot — **the UI integration point**. The sync-status overlay
 * (built by a separate agent against [SyncRepository.observeStatus]) renders this directly:
 * a connectivity/sync bar (`online` + pending/in-flight/failed counts) plus a sync-status
 * sheet (the per-item [items] list, each with a retry affordance when failed/dead-lettered).
 */
data class SyncStatus(
    val online: Boolean,
    val pendingCount: Int,
    val inFlightCount: Int,
    val failedCount: Int,
    val deadLetterCount: Int,
    /** Epoch millis of the most recent successful sync, or null if nothing has synced yet. */
    val lastSyncAt: Long?,
    val items: List<SyncQueueItem>,
) {
    companion object {
        fun empty(online: Boolean) = SyncStatus(
            online = online,
            pendingCount = 0,
            inFlightCount = 0,
            failedCount = 0,
            deadLetterCount = 0,
            lastSyncAt = null,
            items = emptyList(),
        )
    }
}
