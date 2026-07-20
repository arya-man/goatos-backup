package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.feature.counts.CountsWriteResultUi
import sg.mesha.goatos.feature.counts.CountsWriteStatus
import java.util.UUID

/**
 * Shared write-side plumbing for the two Counts write ViewModels.
 *
 * ## The idempotency-key contract (the reason this file exists)
 *
 * Birth, death, and shifting are the canonical must-not-double-submit writes: a duplicate invents
 * an animal, exits one twice, or double-counts a movement. So the key must satisfy TWO opposing
 * requirements, and both are handled here rather than re-derived in each ViewModel:
 *
 *  1. **Stable across process death.** The key is minted ONCE per draft and persisted in
 *     `SavedStateHandle`, so a ViewModel recreated after the OS kills the app resends under the
 *     SAME key — the backend then returns the original result instead of writing a second event.
 *     This is why the timestamp-suffixed style used by verify/verdict is deliberately NOT used
 *     here: a fresh key on every resend is precisely the double-submit bug.
 *
 *  2. **Fresh after a TERMINAL rejection.** If the backend definitively rejected the write (a
 *     validation conflict — bad date ordering, a stale row version) and the operator CORRECTS the
 *     form, that is a genuinely different write. Reusing the old key would be rejected locally by
 *     the outbox's same-key/different-payload guard and wedge the operator. [invalidate] drops the
 *     persisted key so the corrected submission mints a new one. A rejected write never committed
 *     anything server-side, so this cannot duplicate.
 */
internal class DraftIdempotencyKey(
    private val savedStateHandle: SavedStateHandle,
    private val stateKey: String,
    private val prefix: String,
) {
    /** Returns the draft's key, minting and persisting one on first use. */
    fun current(): String {
        savedStateHandle.get<String>(stateKey)?.let { return it }
        val minted = "$prefix:${UUID.randomUUID()}"
        savedStateHandle[stateKey] = minted
        return minted
    }

    /** Drops the persisted key so the next [current] mints a fresh one. See the class kdoc. */
    fun invalidate() {
        savedStateHandle.remove<String>(stateKey)
    }
}

/** Persists the enqueued outbox row id so a recreated ViewModel keeps following the same write. */
internal class DraftOutboxItemId(
    private val savedStateHandle: SavedStateHandle,
    private val stateKey: String,
) {
    var value: String?
        get() = savedStateHandle[stateKey]
        set(next) {
            if (next == null) savedStateHandle.remove<String>(stateKey) else savedStateHandle[stateKey] = next
        }
}

/**
 * Maps a live outbox row to the operator-facing banner.
 *
 * QUEUED and IN_FLIGHT both read as "saved" rather than "pending": once the row exists the write
 * is durable on device, and telling an operator standing in a shed with no signal that their entry
 * is merely in-flight invites them to type it a second time.
 *
 * A [SyncQueueItem.conflict] row is TERMINAL — a definitive server rejection that will never
 * succeed on retry — so its backend message is surfaced verbatim for correction. A dead-lettered
 * row (retries exhausted) is also terminal but for transport reasons.
 */
internal fun SyncQueueItem.toWriteResult(
    queuedMessage: String,
    syncedMessage: String,
): CountsWriteResultUi = when {
    status == SyncItemStatus.SUCCEEDED ->
        CountsWriteResultUi(CountsWriteStatus.SYNCED, syncedMessage)
    conflict ->
        // Verbatim backend copy: the server owns rejection wording (AGENTS.md golden rule).
        CountsWriteResultUi(CountsWriteStatus.FAILED, lastError ?: "The server rejected this entry.")
    isDeadLetter ->
        CountsWriteResultUi(
            CountsWriteStatus.FAILED,
            lastError ?: "Couldn't sync after $maxAttempts attempts.",
        )
    else -> CountsWriteResultUi(CountsWriteStatus.QUEUED, queuedMessage)
}

/** True once the write is terminal-failed and the operator may edit and resubmit. */
internal val CountsWriteResultUi.isCorrectable: Boolean
    get() = status == CountsWriteStatus.FAILED

/** True while a write is queued or already synced — the form is done and must not be re-edited. */
internal val CountsWriteResultUi.isCommitted: Boolean
    get() = status == CountsWriteStatus.QUEUED || status == CountsWriteStatus.SYNCED
