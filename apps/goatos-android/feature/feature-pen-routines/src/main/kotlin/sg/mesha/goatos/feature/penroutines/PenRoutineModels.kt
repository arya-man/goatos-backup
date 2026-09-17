package sg.mesha.goatos.feature.penroutines

// telemetry:exempt pure UI model declarations; the @HiltViewModels in :app own the
// pen_routine_* AnalyticsEventsPenRoutines + CrashReporter wiring for every read refresh,
// check-in, capture, upload and submit.

import androidx.compose.runtime.Immutable

/**
 * UI models for the Routines module (maintainer instruction 2026-09-16,
 * docs/decisions/pen-routines.md): a routine is a rule the CEO writes once per park; the kernel
 * raises ONE task per pen per occurrence and the assignee works each card — check in to the
 * pen, answer the questions, capture the photo/video, submit.
 *
 * EVERY business sentence here is BACKEND-OWNED and carried through verbatim: the title, the pen
 * label, the reason line, the evidence line, the state chip, the instruction, the presence line,
 * the done line, every question title / hint / option label, the filter chips and their empty
 * copy, the page title. The renderer composes none of them. The only strings the client owns are
 * its own chrome (button labels, progress lines, the required marker).
 */

/** The chip's colour, mirrored one-to-one from the wire `state_tone`; the words stay backend copy. */
enum class PenRoutineTone {
    INFO,
    REVIEW,
    DANGER,
    SUCCESS,
    MUTED,
    ;

    companion object {
        fun from(raw: String): PenRoutineTone = when (raw) {
            "info" -> INFO
            "review" -> REVIEW
            "danger" -> DANGER
            "success" -> SUCCESS
            else -> MUTED
        }
    }
}

/** The authored question kinds (backend/internal/penroutines/domain: Question.Kind). */
enum class PenRoutineQuestionKind {
    YES_NO,
    CHOICE,
    MULTI_CHOICE,
    NUMBER,
    TEXT,
    ;

    companion object {
        fun from(raw: String): PenRoutineQuestionKind = when (raw) {
            "yes_no" -> YES_NO
            "choice" -> CHOICE
            "multi_choice" -> MULTI_CHOICE
            "number" -> NUMBER
            else -> TEXT
        }
    }
}

@Immutable
data class PenRoutineOptionUi(
    val value: String,
    /** Backend-composed option label, VERBATIM. */
    val label: String,
)

/** One authored question with the phone's current draft answer beside it. */
@Immutable
data class PenRoutineQuestionUi(
    val id: String,
    val kind: PenRoutineQuestionKind,
    /** Backend-composed, VERBATIM. */
    val title: String,
    /** Backend-composed, VERBATIM; blank renders nothing. */
    val hint: String = "",
    val required: Boolean = false,
    val options: List<PenRoutineOptionUi> = emptyList(),
    val min: Double? = null,
    val max: Double? = null,
    /** Backend-composed unit ("kg", "animals"), VERBATIM. */
    val unit: String = "",
    /** The chosen option value(s) for YES_NO / CHOICE / MULTI_CHOICE; empty when unanswered. */
    val selected: List<String> = emptyList(),
    /** The typed text for NUMBER / TEXT; blank when unanswered. */
    val text: String = "",
    /** A NUMBER answer that does not parse or sits outside min/max — the field is marked. */
    val invalid: Boolean = false,
)

enum class PenRoutineSlotKind { PHOTO, VIDEO }

/**
 * One capture slot of the form (photo slots up to `form.photo.max`, video slots up to
 * `form.video.max`). The state comes from :app's read of Room — the durable proof row and its
 * upload — so leaving and re-opening the screen, or a process death mid-upload, shows the capture
 * already taken instead of asking for it again.
 */
@Immutable
data class PenRoutineSlotUi(
    /** Stable list key: the capture identity's field key (`routine-photo-1`, ...). */
    val fieldKey: String,
    val kind: PenRoutineSlotKind,
    /** 1-based position among its kind. */
    val index: Int,
    /** Within the routine's `min` for this kind. */
    val required: Boolean,
    /** Local path of the capture for the preview; blank when the slot is empty. */
    val previewPath: String = "",
    /** True while the capture is compressing/uploading — the pipeline's own progress line. */
    val working: Boolean = false,
    val progressLabel: String = "",
    /** The pipeline's own sentence when the upload failed for good; blank otherwise. */
    val failureReason: String = "",
)

/** One stored answer as the backend renders it, for the read-only view. */
@Immutable
data class PenRoutineAnswerRowUi(
    val questionId: String,
    val title: String,
    val value: String,
)

/**
 * Where the task stands, derived in :app from Room (the server's task, the queued outbox rows)
 * — never from a remembered flag alone.
 */
enum class PenRoutinePhase {
    /** Work is owed and the caller may do it: the form is live. */
    OPEN,

    /** The check-in or the submit is on its way (queued / uploading / sending). */
    SENDING,

    /** The submit reached the server and the verifier has it. Read-only. */
    IN_REVIEW,

    /** The verifier sent it back: the form is live again beside the verifier's words. */
    REWORK,

    /** Completed (on submit for a `none` review, on approval otherwise). Read-only. */
    DONE,

    /** The caller may not work it (not an assignee, canceled). Read-only, no camera. */
    LOCKED,
}

@Immutable
data class PenRoutineDetailUiState(
    /** True until the first cached/fetched detail lands. */
    val loading: Boolean = true,
    val taskId: String = "",
    val title: String = "",
    /** The backend's pen label, VERBATIM; blank for a general park task (never rendered then). */
    val penLabel: String = "",
    val parkName: String = "",
    /** A general PARK task (`scope_kind = park`): no pen, and the check-in button reads the
     *  neutral "Check in" instead of naming a pen. */
    val parkTask: Boolean = false,
    val reasonLine: String = "",
    val evidenceLine: String = "",
    val stateChip: String = "",
    val tone: PenRoutineTone = PenRoutineTone.MUTED,
    val instruction: String = "",
    val phase: PenRoutinePhase = PenRoutinePhase.LOCKED,
    val presenceRequired: Boolean = false,
    /** Backend-composed ("Check in to the pen before you start" / "In pen since 07:12"), VERBATIM. */
    val presenceLine: String = "",
    val inPen: Boolean = false,
    /** The backend's `can_check_in`, AND no check-in is already on the wire. */
    val canCheckIn: Boolean = false,
    /** True while the location is being captured or the punch is queued but unanswered. */
    val checkingIn: Boolean = false,
    /** The backend's `can_submit` — the ONLY thing that makes the form live. */
    val canSubmit: Boolean = false,
    val questions: List<PenRoutineQuestionUi> = emptyList(),
    val photoSlots: List<PenRoutineSlotUi> = emptyList(),
    val photoMin: Int = 0,
    val videoSlots: List<PenRoutineSlotUi> = emptyList(),
    val videoMin: Int = 0,
    /** can_submit AND every required question answered AND counts at their minimum AND
     *  (presence off OR in pen) AND nothing already on the wire. */
    val submitEnabled: Boolean = false,
    /** The read-only rendering of the stored answers once the task left the form. */
    val answerRows: List<PenRoutineAnswerRowUi> = emptyList(),
    /** How many captures the server holds for the task (read-only view). */
    val proofCount: Int = 0,
    val doneLine: String = "",
    /** The verifier's words while REWORK (backend copy, verbatim); blank otherwise. */
    val reworkReason: String = "",
    /** The server's or the queue's own sentence when the last write was refused; blank otherwise. */
    val failureReason: String = "",
    val isRefreshing: Boolean = false,
    /** True while a camera is open or a capture is being written down. */
    val capturing: Boolean = false,
    /** Transient notice; the ViewModel owns the copy and clears it. */
    val message: String? = null,
)

sealed interface PenRoutineDetailEvent {
    data object Refresh : PenRoutineDetailEvent
    data object Back : PenRoutineDetailEvent

    /** Capture the device location and queue the `enter` punch. */
    data object CheckIn : PenRoutineDetailEvent

    /** YES_NO / CHOICE: pick one option value. */
    data class SetChoice(val questionId: String, val value: String) : PenRoutineDetailEvent

    /** MULTI_CHOICE: toggle one option value. */
    data class ToggleChoice(val questionId: String, val value: String) : PenRoutineDetailEvent

    /** NUMBER / TEXT: the typed text. */
    data class SetText(val questionId: String, val text: String) : PenRoutineDetailEvent

    /** Open the in-app camera for one slot; a real capture is written down and queued for upload. */
    data class CaptureSlot(val fieldKey: String) : PenRoutineDetailEvent

    /** Queue the submit with the draft answers and every captured slot. */
    data object Submit : PenRoutineDetailEvent

    /** A play/pause/fullscreen/share/failure action on a capture's preview. */
    data class ProofPreviewAction(val fieldKey: String, val action: String) : PenRoutineDetailEvent
    data object DismissMessage : PenRoutineDetailEvent
}

/** One task as the Routines list renders it. */
@Immutable
data class PenRoutineCardUi(
    /** Stable list key — the task id IS this list's grain. */
    val listKey: String,
    val taskId: String,
    /** Backend-composed ("Pen cleaning · Castro 2 · Coimbatore"), VERBATIM. */
    val title: String,
    /** The backend's pen label, VERBATIM; blank for a general park task. */
    val penLabel: String,
    val parkName: String = "",
    /** A general PARK task (`scope_kind = park`) — no pen line. */
    val parkTask: Boolean = false,
    /** Backend-composed ("Every day"), VERBATIM. */
    val reasonLine: String,
    /** Backend-composed ("2 questions · 1 photo · check in"), VERBATIM. */
    val evidenceLine: String = "",
    /** Backend-composed chip copy, VERBATIM; [tone] only colours it. */
    val stateChip: String,
    val tone: PenRoutineTone = PenRoutineTone.MUTED,
    /** True while this task's submit is still on the wire — drawn as a quiet "Sending" mark. */
    val sending: Boolean = false,
    val done: Boolean = false,
)

/** One filter chip. Label, count and empty copy are BACKEND-COMPOSED; the screen sends back [key]. */
@Immutable
data class PenRoutineFilterUi(
    val key: String,
    val label: String,
    val count: Int,
    val selected: Boolean,
    val emptyMessage: String = "",
)

@Immutable
data class PenRoutineListUiState(
    /** The backend's page title, VERBATIM. */
    val title: String = "",
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val emptyMessage: String? = null,
    val isErrorEmpty: Boolean = false,
    val filters: List<PenRoutineFilterUi> = emptyList(),
)

sealed interface PenRoutineListEvent {
    data object Refresh : PenRoutineListEvent
    data class SelectFilter(val key: String) : PenRoutineListEvent
    data class OpenTask(val taskId: String) : PenRoutineListEvent
}
