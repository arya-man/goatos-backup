package sg.mesha.goatos.feature.penvisits

// telemetry:exempt pure UI model declarations; the @HiltViewModels in :app own the
// pen_visit_* AnalyticsEventsPenVisits + CrashReporter wiring for every read refresh, capture,
// upload and submit.

import androidx.compose.runtime.Immutable

/**
 * UI models for the pen visit (maintainer decisions 2026-09-07 and 2026-09-14): the day after
 * vaccination or PC Care work in a pen, one of the park's configured visitors goes to that pen,
 * records ONE live in-app-camera video and submits it; the clip goes to the VERIFIER. The visit
 * is a task of its own, listed on the "For me" tab of the Tasks module -- the work the system
 * owes this person; pen visits are its first card type -- and never a step on a parent card.
 *
 * EVERY business sentence here is BACKEND-OWNED and carried through verbatim: the title, the pen
 * label, the reason line, the state chip, the instruction, the done line, the filter chips and
 * their empty copy, the page title. The renderer composes none of them. The only strings the
 * client owns are its own chrome (button labels, progress lines).
 */

/** The chip's colour, mirrored one-to-one from the wire `state_tone`; the words stay backend copy. */
enum class PenVisitTone {
    INFO,
    REVIEW,
    DANGER,
    SUCCESS,
    MUTED,
    ;

    companion object {
        fun from(raw: String): PenVisitTone = when (raw) {
            "info" -> INFO
            "review" -> REVIEW
            "danger" -> DANGER
            "success" -> SUCCESS
            else -> MUTED
        }
    }
}

/**
 * Where the visit's ONE video stands, derived in :app from Room (the durable proof row, the
 * queued submit and the server's own task) — never from a remembered flag alone.
 */
enum class PenVisitVideoState {
    /** Nothing recorded yet: offer "Record video". */
    EMPTY,

    /** Recorded and on its way: compressing, uploading, or the submit is queued/sending. */
    WORKING,

    /** The upload or the submit failed for good: offer "Record again" with the reason. */
    FAILED,

    /** The clip reached the server and is with the verifier. Nothing to do here. */
    IN_REVIEW,

    /** The verifier sent the clip back: offer "Record again" with the verifier's own words. */
    REWORK,

    /** The verifier approved the visit; the pen's work is closed. */
    DONE,
}

@Immutable
data class PenVisitDetailUiState(
    /** True until the first cached/fetched detail lands. */
    val loading: Boolean = true,
    val taskId: String = "",
    val title: String = "",
    val penLabel: String = "",
    /** The backend's park name, VERBATIM. */
    val parkName: String = "",
    val reasonLine: String = "",
    val stateChip: String = "",
    val tone: PenVisitTone = PenVisitTone.MUTED,
    val instruction: String = "",
    val doneLine: String = "",
    /** The backend's `can_submit` — the ONLY thing that opens the camera. */
    val canSubmit: Boolean = false,
    val videoState: PenVisitVideoState = PenVisitVideoState.EMPTY,
    /** Local path of the recorded clip for the preview; blank when there is nothing to show. */
    val previewPath: String = "",
    /** The upload/submit progress line while WORKING ("Uploading…"); blank otherwise. */
    val progressLabel: String = "",
    /** The server's or the queue's own sentence while FAILED; blank otherwise. */
    val failureReason: String = "",
    /** The verifier's words while REWORK (backend copy, verbatim); blank otherwise. */
    val reworkReason: String = "",
    val isRefreshing: Boolean = false,
    /** True while the camera is open or the clip is being written down. */
    val capturing: Boolean = false,
    /** Transient notice; the ViewModel owns the copy and clears it. */
    val message: String? = null,
)

sealed interface PenVisitDetailEvent {
    data object Refresh : PenVisitDetailEvent
    data object Back : PenVisitDetailEvent

    /** Open the in-app camera; a real recording is written down and its submit queued at once. */
    data object RecordVideo : PenVisitDetailEvent

    /** A play/pause/fullscreen/share/failure action on the recorded clip's preview. */
    data class ProofPreviewAction(val action: String) : PenVisitDetailEvent
    data object DismissMessage : PenVisitDetailEvent
}

/** One visit as the "For me" list renders it. */
@Immutable
data class PenVisitCardUi(
    /** Stable list key — the visit task id IS this list's grain. */
    val listKey: String,
    val taskId: String,
    /** Backend-composed ("Visit Castro 2 · Coimbatore"), VERBATIM. */
    val title: String,
    /** The backend's pen label, VERBATIM. */
    val penLabel: String,
    /** The backend's park name, VERBATIM. */
    val parkName: String = "",
    /** Backend-composed ("Vaccination yesterday"), VERBATIM. */
    val reasonLine: String,
    /** Backend-composed chip copy, VERBATIM; [tone] only colours it. */
    val stateChip: String,
    val tone: PenVisitTone = PenVisitTone.MUTED,
    /** True while this visit's video/submit is still on the wire — drawn as a quiet "Sending" mark. */
    val sending: Boolean = false,
    val done: Boolean = false,
)

/** One filter chip. Label, count and empty copy are BACKEND-COMPOSED; the screen sends back [key]. */
@Immutable
data class PenVisitFilterUi(
    val key: String,
    val label: String,
    val count: Int,
    val selected: Boolean,
    val emptyMessage: String = "",
)

@Immutable
data class PenVisitListUiState(
    /** The backend's page title, VERBATIM. */
    val title: String = "",
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val emptyMessage: String? = null,
    val isErrorEmpty: Boolean = false,
    val filters: List<PenVisitFilterUi> = emptyList(),
)

sealed interface PenVisitListEvent {
    data object Refresh : PenVisitListEvent
    data class SelectFilter(val key: String) : PenVisitListEvent
    data class OpenTask(val taskId: String) : PenVisitListEvent
}
