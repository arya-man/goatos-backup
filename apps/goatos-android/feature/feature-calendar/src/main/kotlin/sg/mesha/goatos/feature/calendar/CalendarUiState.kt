package sg.mesha.goatos.feature.calendar

import androidx.compose.runtime.Immutable
import sg.mesha.goatos.core.ui.CoverageBannerUiState

/**
 * Calendar screen state (TRD §14 dumb-renderer). Every visible label, status,
 * count, action, and drill target is a backend-provided FIELD — the screen never
 * derives which animals are due, never checks role, never counts modules. A later
 * ViewModel fills this from the mobile bootstrap + `GET calendar` reads.
 *
 * The Calendar is the universal landing for ALL roles (screens.md): which segments
 * are visible comes from bootstrap `presentationConfig` (here: [segments]); the
 * drill target for each item comes from the backend ([CalendarItem.target]), so the
 * same screen serves operator (execute) and leadership (follow-up) with no client
 * `role ==` branch.
 */

/** Colour intent for a pill/dot, decided by the backend — the screen only maps it to a token. */
enum class CalendarTone { Ok, Warn, Danger, Muted, Neutral }

/** Which layout a segment renders. Backend marks the kind; the screen never guesses from role. */
enum class CalendarSegmentKind { Week, Month, History }

/** One tab in the week/month/history segmented control. Availability is backend-driven. */
data class CalendarSegment(
    val id: String,
    val label: String,
    val kind: CalendarSegmentKind,
)

/** A cell in the week strip: its due-work count and a drill affordance (tap → [CalendarEvent.TapDay]). */
data class CalendarWeekDay(
    val dateKey: String,
    val dayName: String,
    val dayNumber: String,
    val dueCountLabel: String,
    val hasWork: Boolean,
    val isSelected: Boolean = false,
    val isToday: Boolean = false,
)

/**
 * Park-level vaccination-drive progress (v4) — a straight mapping of the backend
 * `drive_summary` object (see `DriveSummaryDto`). [EventCard] renders every field as-is;
 * it never re-derives shed/animal counts from the underlying target rows. Null when the
 * item is not drive-aggregated or the backend has not populated `drive_summary` yet.
 */
data class CalendarDriveSummary(
    val parkName: String = "",
    val dueDateLabel: String = "",
    val shedCount: Int = 0,
    val shedsCompleted: Int = 0,
    val vaccineLabels: List<String> = emptyList(),
    val totalCount: Int = 0,
    val completedCount: Int = 0,
    // Distinct-animal coverage (grain differs from the obligation counts above): a goat due for
    // several vaccines the same day is one animal, completed only when all its drive obligations are.
    val totalAnimals: Int = 0,
    val completedAnimals: Int = 0,
    val remainingCount: Int = 0,
    val dueCount: Int = 0,
    val overdueCount: Int = 0,
    val deferredCount: Int = 0,
    val ownerLabel: String = "",
)

/**
 * A drive/shed card shown under a week day or inside a day sheet. [ctaLabel] and
 * [target] are backend-provided per principal (operator "Open drive" / leadership
 * "View drive status"); a null [ctaLabel] renders a dimmed, non-drillable card.
 */
data class CalendarItem(
    val id: String,
    val title: String,
    val subtitle: String,
    val aggregated: Boolean = false,
    val allDay: Boolean = false,
    val timeLabel: String = "",
    val summaryPrimary: String = "",
    val summarySecondary: String = "",
    val shedCount: Int = 0,
    val vaccineCount: Int = 0,
    val targetCount: Int = 0,
    val vaccineLabels: List<String> = emptyList(),
    /** Park-level drive progress card content (v4); see [CalendarDriveSummary]. */
    val driveSummary: CalendarDriveSummary? = null,
    val statusLabel: String,
    val statusTone: CalendarTone,
    val categoryLabel: String? = null,
    val ctaLabel: String? = null,
    val target: String? = null,
)

/** A cell in the month grid. [dateKey]/[dayNumber] are null for leading blank cells. */
data class CalendarMonthDay(
    val dateKey: String?,
    val dayNumber: String?,
    val hasWork: Boolean = false,
    val hasCompletedHistory: Boolean = false,
    val dotTone: CalendarTone = CalendarTone.Neutral,
    val isSelected: Boolean = false,
)

/** A past shed/drive record row in the History segment. */
data class CalendarHistoryRow(
    val id: String,
    val title: String,
    val subtitle: String,
    val badgeLabel: String,
    val badgeTone: CalendarTone,
    val target: String? = null,
)

/**
 * L1 day-detail screen: the drives/sheds due on a tapped month day, opened as its OWN
 * screen (a real navigation drill), NOT appended below the month grid. Offline-first sync
 * fields mirror the Calendar reference so the day screen shows syncing/stale over its own
 * Room-backed read and never a blank wall on re-entry.
 */
@Immutable
data class CalendarDayUiState(
    val title: String = "",
    val items: List<CalendarItem> = emptyList(),
    val showCompletedHistory: Boolean = false,
    val emptyLabel: String = "",
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
    val hasMore: Boolean = false,
    val isLoadingMore: Boolean = false,
)

// @Immutable: every field is a val built once from a fixed List — the compiler otherwise
// treats the several List<T> fields below as potentially-mutable and marks the whole class
// (and every screen that takes it as a parameter) unstable, which disables recomposition
// skipping. See build/compose_reports/*-classes.txt (item 6, perf/stability pass).
@Immutable
data class CalendarUiState(
    val eyebrow: String = "",
    val title: String = "",
    val selectedDateLabel: String = "",
    val windowLabel: String? = null,
    /** HRMS coverage banner (docs/hr/roster-rbac-design.md S4.6/S4.8) — non-null only
     *  while the principal holds an active ad-hoc-leave/week-off coverage window for
     *  another position. Null hides the banner entirely (CoverageBanner renders nothing). */
    val coverageBanner: CoverageBannerUiState? = null,
    // Offline-first sync state (docs/decisions/android-offline-first.md), rendered by
    // sg.mesha.goatos.core.ui.SyncStatusIndicator. [isRefreshing]/[lastSyncedAt]/[isOffline]
    // describe the background network refresh over the ALREADY-RENDERED Room cache above —
    // they never gate whether the rest of this state renders.
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
    val segments: List<CalendarSegment> = emptyList(),
    val selectedSegmentId: String = "",
    // WEEK
    val weekDays: List<CalendarWeekDay> = emptyList(),
    val weekItems: List<CalendarItem> = emptyList(),
    val weekEmptyLabel: String = "",
    val weekHasMore: Boolean = false,
    val weekLoadingMore: Boolean = false,
    // MONTH
    val monthLabel: String = "",
    val monthWeekdayLabels: List<String> = emptyList(),
    val monthDays: List<CalendarMonthDay> = emptyList(),
    val monthHint: String = "",
    // HISTORY
    val historyLabel: String = "",
    val historyCount: Int = 0,
    val historyRows: List<CalendarHistoryRow> = emptyList(),
    val historyEmptyLabel: String = "",
    val historyHasMore: Boolean = false,
    val historyLoadingMore: Boolean = false,
)

/** User intents. The ViewModel maps each to a backend read/drill — the screen decides nothing. */
sealed interface CalendarEvent {
    data class SelectSegment(val segmentId: String) : CalendarEvent

    /** Week-strip day tap — re-scopes the week agenda list in place (stays on the calendar). */
    data class TapDay(val dateKey: String) : CalendarEvent

    /** Month-grid day tap — opens that day's drives as their OWN L1 screen (nav host routes it).
     *  History-only days request the completed-history branch explicitly so a visible muted marker
     *  never drills into an empty open-work query. */
    data class OpenDay(val dateKey: String, val showCompletedHistory: Boolean = false) : CalendarEvent

    data class TapItem(val itemId: String) : CalendarEvent

    data object LoadMoreWeek : CalendarEvent

    data object LoadMoreHistory : CalendarEvent

    /** Header refresh — reloads the calendar (mock `.vhead` refresh affordance). */
    data object Refresh : CalendarEvent
}
