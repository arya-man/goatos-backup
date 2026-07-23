package sg.mesha.goatos.core.data

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
 * badge ahead of the next successful refresh. Keyed by the completion grain (shed-session-workflow),
 * which both the direction and packing rows can compute.
 */
class FeedCompletionLocalStore {
    private val _completedKeys = MutableStateFlow<Set<String>>(emptySet())

    /** The set of locally-completed shed-session keys. Emits on every [markCompleted]. */
    val completedKeys: StateFlow<Set<String>> = _completedKeys.asStateFlow()

    fun markCompleted(key: String) {
        _completedKeys.update { it + key }
    }

    fun isCompleted(key: String): Boolean = _completedKeys.value.contains(key)

    companion object {
        /** The completion grain a feed row maps to: one shed's one session on one workflow. */
        fun key(shedId: String, sessionNo: Int, workflow: String): String =
            listOf(shedId, sessionNo.toString(), workflow).joinToString("|")
    }
}
