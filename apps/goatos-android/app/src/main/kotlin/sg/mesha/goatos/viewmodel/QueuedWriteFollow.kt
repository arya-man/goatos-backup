package sg.mesha.goatos.viewmodel

import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.mapNotNull
import kotlinx.coroutines.flow.merge
import kotlinx.coroutines.flow.transformWhile
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncRepository

/**
 * What a form should tell the operator about a write it just queued.
 *
 * The outbox makes every write durable on the phone first, so the ONLY honest message the
 * instant after submit is "saving". Telling an operator on a good connection that the record
 * "will reach the ledger when the phone is online" reads as a failure to them (reported
 * 2026-09-04 on the Procurement module while the vendor had already landed, 201, within a
 * second). So the form follows the row: [Saved] once the server accepted it, [StillQueued]
 * only if the row is still unsent after [DEFAULT_OFFLINE_AFTER_MS] -- at which point the
 * offline wording is true -- and [Rejected] when the engine will never retry it.
 */
sealed interface QueuedWriteOutcome {
    data object Saved : QueuedWriteOutcome
    data object StillQueued : QueuedWriteOutcome
    data class Rejected(val reason: String?) : QueuedWriteOutcome
}

const val DEFAULT_OFFLINE_AFTER_MS = 5_000L

/**
 * Follows the outbox row [itemId]. Emits [QueuedWriteOutcome.StillQueued] at most once, after
 * [offlineAfterMs] without a terminal state, then keeps following so a late success still
 * upgrades the banner. Completes after [QueuedWriteOutcome.Saved] or [QueuedWriteOutcome.Rejected].
 */
fun SyncRepository.followQueuedWrite(
    itemId: String,
    offlineAfterMs: Long = DEFAULT_OFFLINE_AFTER_MS,
): Flow<QueuedWriteOutcome> {
    val timer = flow<QueuedWriteOutcome> {
        delay(offlineAfterMs)
        emit(QueuedWriteOutcome.StillQueued)
    }
    val row = observeItem(itemId).mapNotNull { item ->
        when {
            item == null -> null
            item.status == SyncItemStatus.SUCCEEDED -> QueuedWriteOutcome.Saved
            item.conflict || item.isDeadLetter -> QueuedWriteOutcome.Rejected(item.lastError)
            else -> null
        }
    }
    return merge(timer, row).transformWhile { outcome ->
        emit(outcome)
        outcome is QueuedWriteOutcome.StillQueued
    }
}
