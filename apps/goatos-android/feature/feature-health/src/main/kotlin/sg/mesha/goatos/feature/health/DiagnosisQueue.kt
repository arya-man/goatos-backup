package sg.mesha.goatos.feature.health

import androidx.compose.runtime.Immutable

/**
 * One row of the Health Director's queue.
 *
 * PURE, like the rest of this module: the wire-to-model mapping lives beside the
 * view model. Every string here is already in farm words — a raw register token
 * must never reach a screen.
 */
@Immutable
data class DiagnosisQueueRow(
    val diagnosisRunId: String,
    /** The animal as a person recognises it. Never a uuid. */
    val goatDisplayId: String,
    /** Backend-composed `shed - partition`. Blank when the shed does not resolve. */
    val location: String,
    /** When it was seen, in farm words: "Today", "Yesterday", or a date. */
    val seen: String,
    /** The ranked diagnoses, most serious first. The ORDER is the backend's. */
    val problems: List<String>,
    /** Things needing action NOW. They do not wait for the Director. */
    val emergencyCount: Int,
    /** Abnormal findings no diagnosis accounts for. */
    val unexplainedCount: Int,
) {
    /**
     * Whether the row needs to stand out.
     *
     * An emergency on a queue row is not decoration: it is work already owed that
     * the Director's decision does not gate. A queue that renders it the same as
     * everything else buries it.
     */
    val urgent: Boolean get() = emergencyCount > 0
}

@Immutable
data class DiagnosisQueueState(
    val refreshing: Boolean = false,
    /** Backend-owned. A manager may read the queue without being offered decisions. */
    val mayConfirm: Boolean = false,
    val message: String? = null,
)

sealed interface DiagnosisQueueEvent {
    data object Refresh : DiagnosisQueueEvent
    data class Open(val diagnosisRunId: String) : DiagnosisQueueEvent
    data object Back : DiagnosisQueueEvent
}

/**
 * The row's one-line headline.
 *
 * Takes only the first two diagnoses and counts the rest. The list is ranked
 * severity-first by the backend, so the front of it is what matters; printing all
 * of them turns a scannable queue into a wall of text.
 */
fun problemHeadline(problems: List<String>): String = when {
    problems.isEmpty() -> "Nothing found"
    problems.size <= 2 -> problems.joinToString(", ")
    else -> problems.take(2).joinToString(", ") + " +${problems.size - 2} more"
}
