package sg.mesha.goatos.feature.toxin

// telemetry:exempt pure UI model declarations; the @HiltViewModels in :app own the toxin_*
// AnalyticsEventsToxin + CrashReporter wiring for every read refresh, capture, and submit.

import androidx.compose.runtime.Immutable

/**
 * UI models for the Toxin module (aflatoxin strip test, maintainer decision 2026-08-25 —
 * `docs/decisions/toxin-testing-module.md`).
 *
 * EVERY business sentence here is BACKEND-OWNED and carried through verbatim: the context line,
 * the status chip, the origin line, each step's title and instruction, the strip reading guide,
 * and the outcome labels. The renderer composes none of them.
 *
 * Step STATE is likewise server-composed. [ToxinStepState.WAITING] is a SERVER verdict, and
 * [ToxinStepUi.availableAtEpochMs] exists ONLY to render a display countdown beside the backend's
 * own instruction — the phone never promotes a waiting step to actionable on its own clock. The
 * server re-checks and refuses every write, so a device whose clock runs fast gains nothing.
 */

/** Server-composed step state, mirrored one-to-one from the wire (`done|available|waiting|locked`). */
enum class ToxinStepState {
    /** Recorded — the row shows who completed it and when. */
    DONE,

    /** The next open step: its capture action is live. */
    AVAILABLE,

    /** Blocked on the SERVER clock (the ~1h settle, and the two short develop/read windows). */
    WAITING,

    /** A later step whose predecessors are not done — dimmed and not actionable. */
    LOCKED,
    ;

    companion object {
        fun from(raw: String): ToxinStepState = when (raw) {
            "done" -> DONE
            "available" -> AVAILABLE
            "waiting" -> WAITING
            else -> LOCKED
        }
    }
}

/** What a step asks of the operator, mirrored from the wire (`video|wait|photo_reading`). */
enum class ToxinStepKind {
    VIDEO,
    WAIT,
    PHOTO_READING,
    ;

    companion object {
        fun from(raw: String): ToxinStepKind = when (raw) {
            "video" -> VIDEO
            "photo_reading" -> PHOTO_READING
            else -> WAIT
        }
    }
}

/** One of the seven guided steps of one test round. */
@Immutable
data class ToxinStepUi(
    /** Stable list key: the step number within this round. */
    val stepNo: Int,
    val kind: ToxinStepKind,
    val state: ToxinStepState,
    /** Backend-owned step title, rendered VERBATIM. */
    val title: String,
    /** Backend-owned step instruction, rendered VERBATIM. */
    val instruction: String,
    /**
     * Backend attribution for a done step ("Amit · 21 Aug, 4:10 pm"), composed by the :app
     * ViewModel from the payload's own `completed_by` / `completed_at`; blank while not done.
     * Steps are person-independent, so this is the only way the screen can say who did what.
     */
    val completedLine: String = "",
    /**
     * Server instant this step unlocks, as epoch millis; 0 unless [state] is
     * [ToxinStepState.WAITING]. DISPLAY-ONLY countdown input — never a gate.
     */
    val availableAtEpochMs: Long = 0L,
    /** True while this step's camera is open or its capture is being written durably. */
    val working: Boolean = false,
)

/** One backend-owned strip-reading option ("Negative"), never a client-invented vocabulary. */
@Immutable
data class ToxinOutcomeOptionUi(
    val value: String,
    /** Backend-owned label, rendered VERBATIM. */
    val label: String,
)

/** One test round as the task list renders it. */
@Immutable
data class ToxinTaskCardUi(
    /** Stable list key — the task id IS this list's grain (one live round per feed load). */
    val listKey: String,
    val taskId: String,
    /** Backend-composed card subtitle (feed, vendor, load, date in one farm line), VERBATIM. */
    val contextLine: String,
    /** Backend-composed chip copy, VERBATIM. */
    val statusChip: String,
    /** Backend-composed retest sentence; blank on a first round. */
    val originLine: String = "",
    val stepsDone: Int = 0,
    val stepsTotal: Int = 0,
    /** True while the round is still workable — a submitted/accepted/cancelled round does not open. */
    val openable: Boolean = true,
)

/**
 * One filter chip on the task list. Both the [label] and the [count] are BACKEND-COMPOSED and
 * rendered verbatim; the screen sends back only [key], so what "Pending" includes stays one
 * backend definition rather than a status list each surface re-derives.
 */
@Immutable
data class ToxinFilterUi(
    val key: String,
    /** Backend-owned chip copy, rendered VERBATIM. */
    val label: String,
    /** Whole-tenant count for this slice, never a page-local sum. */
    val count: Int,
    val selected: Boolean,
    /** Backend-owned "nothing here" copy for THIS slice, rendered VERBATIM. */
    val emptyMessage: String = "",
)

@Immutable
data class ToxinTaskListUiState(
    /** The tab's title — the backend nav label passed through, so the screen never invents one. */
    val title: String = "",
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val emptyMessage: String? = null,
    val isErrorEmpty: Boolean = false,
    /** Backend-composed filter chips in display order; empty until the first list refresh lands. */
    val filters: List<ToxinFilterUi> = emptyList(),
)

sealed interface ToxinTaskListEvent {
    data object Refresh : ToxinTaskListEvent

    /** Re-scope the list to a backend filter key (`all` | `pending` | `completed`). */
    data class SelectFilter(val key: String) : ToxinTaskListEvent
    data class OpenTask(val taskId: String) : ToxinTaskListEvent
}

@Immutable
data class ToxinTaskDetailUiState(
    /** Backend-composed context line for this round, VERBATIM. */
    val contextLine: String = "",
    /** Backend-composed chip copy, VERBATIM. */
    val statusChip: String = "",
    /** Backend-composed retest sentence; blank on a first round. */
    val originLine: String = "",
    /** The CEO/CXO rejection sentence, backend-owned, VERBATIM; blank unless the round was sent back. */
    val reviewReason: String = "",
    /** Backend-owned cancellation sentence (invalid strip / rejected round), VERBATIM. */
    val cancelReason: String = "",
    /** Backend-owned outcome label once a reading was accepted; blank before that. */
    val outcomeLabel: String = "",
    val steps: List<ToxinStepUi> = emptyList(),
    val stepsDone: Int = 0,
    val stepsTotal: Int = 0,
    /** Backend-owned strip reading guide lines, rendered VERBATIM, in order. */
    val readingGuide: List<String> = emptyList(),
    /** Backend-owned reading vocabulary, rendered VERBATIM. */
    val outcomeOptions: List<ToxinOutcomeOptionUi> = emptyList(),
    /** The reading the operator picked (a backend `outcome_options` VALUE); blank until picked. */
    val selectedOutcome: String = "",
    /** True once step 7's strip photo is durably captured on this phone. */
    val stripPhotoCaptured: Boolean = false,
    val stripPhotoWorking: Boolean = false,
    /** Both halves of step 7 are present, so the reading can be sent. */
    val submitEnabled: Boolean = false,
    val submitInFlight: Boolean = false,
    /** True once the reading was durably queued — the screen shows the sent state. */
    val submitQueued: Boolean = false,
    val isRefreshing: Boolean = false,
    /** Transient operator notice; the ViewModel owns the copy and clears it. */
    val message: String? = null,
)

sealed interface ToxinTaskDetailEvent {
    data object Refresh : ToxinTaskDetailEvent
    data object Back : ToxinTaskDetailEvent

    /** Record the video for an AVAILABLE video step. */
    data class RecordStepVideo(val stepNo: Int) : ToxinTaskDetailEvent

    /** Take (or re-take) step 7's strip photo. */
    data object CaptureStripPhoto : ToxinTaskDetailEvent

    /** Pick one of the backend-owned reading options. */
    data class SelectOutcome(val value: String) : ToxinTaskDetailEvent

    /** Send step 7's photo + reading. */
    data object SubmitReading : ToxinTaskDetailEvent

    data object DismissMessage : ToxinTaskDetailEvent
}
