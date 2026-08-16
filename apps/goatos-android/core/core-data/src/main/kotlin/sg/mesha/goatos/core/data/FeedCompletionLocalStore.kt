package sg.mesha.goatos.core.data

import java.time.LocalDate
import java.time.ZoneId
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update

/**
 * Optimistic offline overlay for feed-direction completions.
 *
 * The completion WRITE is offline-first (it goes through the outbox), but the backend `completed`
 * flag on a feed row only flips after that write syncs AND the list refreshes. To keep the app
 * offline-first on the READ side too, the completion detail screen records the just-completed
 * shed-session here the moment it enqueues, and the feed list ViewModels OR this set into each row's
 * completed state. So the operator sees the row as completed immediately, with no signal, and the
 * backend flag takes over once the write has synced.
 *
 * App-scoped (a process singleton) rather than persisted: it is a short-lived optimistic hint, not a
 * source of truth. The outbox is the durable record of the pending command; this only advances the
 * badge ahead of the next successful refresh.
 *
 * TWO authority boundaries this overlay MUST respect, because it is a process singleton that
 * outlives both the principal and the working day:
 *
 *  1. PRINCIPAL — [clear] is called from [LogoutCoordinator.logout], the one logout path. Without
 *     it the departing operator's optimistic completions survived logout inside the process and
 *     the NEXT operator signed in on the same device saw someone else's sheds pre-badged as done
 *     (BLOCKER 6). Every other authority-sensitive store is wiped on logout; this one now is too.
 *
 *  2. BUSINESS DAY — the completion grain is one shed's one session on one workflow *on a given
 *     day*. The [key] therefore embeds the Asia/Kolkata business date, so an entry marked
 *     yesterday can never match a row keyed today: the same shed/session/workflow legitimately
 *     comes due again every day, and a day-blind key resurrected yesterday's "done" badge on
 *     today's due row. Writer and reader both go through [key], so both see the same date and no
 *     call site has to thread it. [markCompleted] also drops entries from earlier business days so
 *     the set cannot grow unboundedly in a long-lived process.
 */
class FeedCompletionLocalStore {
    private val _completedKeys = MutableStateFlow<Set<String>>(emptySet())
    private val _submittedForReviewKeys = MutableStateFlow<Set<String>>(emptySet())

    /** The set of locally-completed shed-session keys for the CURRENT business day. */
    val completedKeys: StateFlow<Set<String>> = _completedKeys.asStateFlow()

    /**
     * The set of locally-submitted-for-review shed-session keys for the CURRENT business day.
     * Used by the Feed Packing lifecycle overlay to show "pending_verification" before the server
     * processes the queued submit. Semantically distinct from [completedKeys] because a row
     * enqueued but not yet confirmed stays "pending_verification" (not "completed"), and the
     * backend always owns the completion gate: if the server rejects the submit, this key clears
     * on logout but the backend row remains "pending", giving the overlay a chance to self-correct.
     */
    val submittedForReviewKeys: StateFlow<Set<String>> = _submittedForReviewKeys.asStateFlow()

    fun markCompleted(key: String) {
        val prefix = businessDatePrefix()
        _completedKeys.update { current -> current.filterTo(mutableSetOf()) { it.startsWith(prefix) } + key }
    }

    fun markSubmittedForReview(key: String) {
        val prefix = businessDatePrefix()
        _submittedForReviewKeys.update { current -> current.filterTo(mutableSetOf()) { it.startsWith(prefix) } + key }
    }

    fun isCompleted(key: String): Boolean = _completedKeys.value.contains(key)

    /**
     * Drops every optimistic completion and submission. Called by [LogoutCoordinator.logout] so no
     * part of the departing operator's work is visible to the next principal on the device.
     */
    fun clear() {
        _completedKeys.value = emptySet()
        _submittedForReviewKeys.value = emptySet()
    }

    companion object {
        /** Asia/Kolkata — the farm's business day, the same zone the feed list ViewModels use. */
        private val BUSINESS_ZONE: ZoneId = ZoneId.of("Asia/Kolkata")

        /**
         * Injection seam for tests only; production always reads the real clock. Kept internal so
         * no production call site can smuggle a different business day into the key.
         */
        internal var businessDate: () -> String = { LocalDate.now(BUSINESS_ZONE).toString() }

        private fun businessDatePrefix(): String = businessDate() + "|"

        /**
         * The completion grain a feed row maps to: one shed's one session on one workflow, ON THE
         * CURRENT BUSINESS DAY. The date is part of the key (not an argument) so every caller —
         * writer and reader — is day-scoped without change.
         */
        /** The optimistic key must carry the PEN for the same reason the backend's natural key
         *  does: Castro 1 and Castro 2 share a shed_id, so a shed-only key made one pen's submit
         *  grey out every pen of that shed on the spot (STG 2026-08-08). */
        fun key(shedId: String, partitionLabel: String?, sessionNo: Int, workflow: String): String =
            listOf(
                businessDate(),
                shedId,
                partitionLabel?.trim().orEmpty().lowercase().ifBlank { "whole" },
                sessionNo.toString(),
                workflow,
            ).joinToString("|")
    }
}
