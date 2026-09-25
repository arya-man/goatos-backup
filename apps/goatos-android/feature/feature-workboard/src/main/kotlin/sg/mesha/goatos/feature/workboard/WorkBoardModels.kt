package sg.mesha.goatos.feature.workboard

// telemetry:exempt pure UI model declarations; the @HiltViewModels in :app own the
// work_board_* AnalyticsEventsWorkBoard + CrashReporter wiring for every refresh, filter change
// and row open.

import androidx.compose.runtime.Immutable

/**
 * UI models for the Work Board's My Work screen (maintainer decision 2026-09-10): every module's
 * work for one park and one business day, ONE row shape, served by the backend already scoped to
 * the caller (an operator sees only their own rows; a park head their park).
 *
 * EVERY business sentence here is BACKEND-OWNED and carried through verbatim: the title, the
 * subtitle, the pen label, the park name, the clock label, the owner's name. `lane`, `work_state`,
 * `severity` and `owner_state` are server vocabularies the screen only COLOURS or LABELS — it
 * never derives a row's lane or state from anything else, and it never filters rows by role.
 */

/** The row's clock tone, mirrored one-to-one from the wire `severity`; the words stay backend copy. */
enum class WorkBoardSeverity {
    OK,
    WATCH,
    AT_RISK,
    BROKEN,
    ;

    companion object {
        fun from(raw: String): WorkBoardSeverity = when (raw) {
            "watch" -> WATCH
            "at_risk" -> AT_RISK
            "broken" -> BROKEN
            else -> OK
        }
    }
}

/** How a row came to have (or lack) an owner — the wire `owner_state`. */
enum class WorkBoardOwnerState {
    /** The module's own row names the person: render [WorkBoardRowUi.ownerName]. */
    ASSIGNED,

    /** The module expects an owner and has none (never filled with a fallback). */
    MISSING,

    /** A claim pool by design. */
    POOL,
    ;

    companion object {
        fun from(raw: String): WorkBoardOwnerState = when (raw) {
            "missing" -> MISSING
            "pool" -> POOL
            else -> ASSIGNED
        }
    }
}

/** One board row as the list and the detail render it. */
@Immutable
data class WorkBoardRowUi(
    /** `module|source_type|source_id` — the stable list key AND the L1 route argument. */
    val rowKey: String,
    /** Backend module key (feed, health, ...); the screen maps it to a farm label. */
    val module: String,
    /** Backend-composed, VERBATIM. */
    val title: String,
    /** Backend-composed, VERBATIM; blank renders nothing. */
    val subtitle: String = "",
    /** The backend's `operational_location_display`, VERBATIM; blank renders nothing. */
    val penLabel: String = "",
    /** The backend's park name, VERBATIM. */
    val parkName: String = "",
    /** The IST business day the row belongs to (ISO). */
    val businessDate: String = "",
    /** Farm wording for the clock, VERBATIM; blank renders nothing. */
    val clockLabel: String = "",
    /** The server's process-integrity state key; the screen only labels it. */
    val workState: String = "",
    /** todo | in_progress | in_review | done — SERVER-derived; the screen only labels it. */
    val lane: String = "",
    val severity: WorkBoardSeverity = WorkBoardSeverity.OK,
    /** The owner's display name, VERBATIM; blank unless [ownerState] is ASSIGNED. */
    val ownerName: String = "",
    val ownerState: WorkBoardOwnerState = WorkBoardOwnerState.ASSIGNED,
    val done: Int = 0,
    val pending: Int = 0,
    val needsAttention: Int = 0,
    /** Of [pending], units handed in and waiting for a verdict; 0 when the source does not say. */
    val inReview: Int = 0,
    /** Of [pending], units nobody has started (feed pens not filmed); 0 when the source does not say. */
    val notStarted: Int = 0,
    /** Where the module's own screen opens this row; blank when it has none yet. */
    val href: String = "",
) {
    val total: Int get() = done + pending

    /** True when the source said where its pending work is (the feed cards). */
    val hasPendingSplit: Boolean get() = inReview > 0 || notStarted > 0

    /** Pending work started and not handed in: what is left after review, not started and attention. */
    val started: Int get() = (pending - inReview - notStarted - minOf(pending, needsAttention)).coerceAtLeast(0)
}

/** One filter chip over a BOUNDED vocabulary (the four lanes, or the caller's visible modules). */
@Immutable
data class WorkBoardChipUi(
    /** "" is the "all" chip. */
    val key: String,
    /** WHOLE-FILTER count from the summary, never a page-local sum. */
    val count: Int,
    val selected: Boolean,
)

/** Which client-owned sentence the empty list shows; the screen resolves it to a locale string. */
enum class WorkBoardEmptyMessage {
    /** Nothing cached yet and no failure: the first page is on its way. */
    LOADING,

    /** The server clamped the read to the caller's own rows and there are none. */
    EMPTY_OWN,

    /** The scope has no rows. */
    EMPTY_ALL,

    /** Nothing cached and the last read failed. */
    ERROR,
}

@Immutable
data class WorkBoardUiState(
    /** The backend nav label once bound; blank falls back to the screen's own title string. */
    val title: String = "",
    /** The board's business day (ISO, Asia/Kolkata). */
    val dateIso: String = "",
    /** The day as the reader sees it ("Wed, 10 Sep"); composed by the ViewModel. */
    val dateLabel: String = "",
    val isToday: Boolean = true,
    /** The four lanes plus "all", in display order. */
    val lanes: List<WorkBoardChipUi> = emptyList(),
    /** The modules the caller MAY see (from the summary) plus "all"; empty until a summary lands. */
    val modules: List<WorkBoardChipUi> = emptyList(),
    /** Summary tiles — WHOLE-FILTER counts from `/work-board/summary`, never from the page. */
    val doneCount: Int = 0,
    val pendingCount: Int = 0,
    val needsAttentionCount: Int = 0,
    val hasSummary: Boolean = false,
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
    val emptyMessage: WorkBoardEmptyMessage? = null,
    /** True when the server clamped the read to the caller's own rows. */
    val ownRowsOnly: Boolean = false,
)

sealed interface WorkBoardEvent {
    data object Refresh : WorkBoardEvent

    /** A lane key, or "" for every lane. */
    data class SelectLane(val lane: String) : WorkBoardEvent

    /** A module key, or "" for every module. */
    data class SelectModule(val module: String) : WorkBoardEvent
    data object PreviousDay : WorkBoardEvent
    data object NextDay : WorkBoardEvent
    data class OpenRow(val rowKey: String) : WorkBoardEvent
}

@Immutable
data class WorkBoardDetailUiState(
    /** True until the cached row is read; a row nothing cached renders the not-found copy. */
    val loading: Boolean = true,
    val row: WorkBoardRowUi? = null,
    /** True only when [WorkBoardRowUi.href] names a screen THIS build can open. */
    val canOpen: Boolean = false,
    /**
     * The row's units of work (its pens, animals, steps), worst first, as the backend drills them:
     * the pages loaded so far, in order. The admin-web drawer's list, on the phone.
     */
    val subtasks: List<WorkBoardSubtaskUi> = emptyList(),
    /** The WHOLE count for the row, never the pages loaded. */
    val subtaskTotal: Int = 0,
    /** True while nothing is cached yet and the first page is on its way. */
    val subtasksLoading: Boolean = true,
    /** True when the last read failed and nothing is cached to show. */
    val subtasksFailed: Boolean = false,
    /** True while a further page exists; the list asks for it as the reader nears the end. */
    val hasMoreSubtasks: Boolean = false,
)

/** One unit of a row's work, every string backend-composed and rendered verbatim. */
@Immutable
data class WorkBoardSubtaskUi(
    val key: String,
    val name: String,
    val subtitle: String = "",
    val workState: String = "",
    val lane: String = "",
    val ownerName: String = "",
    val needsAttention: Boolean = false,
    val steps: List<WorkBoardStepUi> = emptyList(),
)

/** One link of a subtask's chain; [state] is the closed seven-value step vocabulary. */
@Immutable
data class WorkBoardStepUi(val name: String, val state: String, val detail: String = "")

sealed interface WorkBoardDetailEvent {
    data object Back : WorkBoardDetailEvent

    /** Open the module's own screen through the row's backend `href`. */
    data object Open : WorkBoardDetailEvent

    /** Re-read the loaded subtask pages (the screen came back into view). */
    data object Refresh : WorkBoardDetailEvent

    /** The reader neared the end of the loaded subtasks; fetch the next page if there is one. */
    data object LoadMoreSubtasks : WorkBoardDetailEvent
}
