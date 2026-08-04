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
enum class CalendarSegmentKind { Week, Month }

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
    val bucketKey: String = "",
    val bucketCount: Int = 0,
)

/**
 * Park-level vaccination-drive progress (v4) — a straight mapping of the backend
 * `drive_summary` object (see `DriveSummaryDto`). [EventCard] renders every field as-is;
 * it never re-derives shed/animal counts from the underlying target rows. Null when the
 * item is not drive-aggregated or the backend has not populated `drive_summary` yet.
 */
data class CalendarDriveSummary(
    val parkName: String = "",
    val driveName: String = "",
    val driveTotal: Int? = null,
    val dueDateLabel: String = "",
    val shedCount: Int = 0,
    val shedsCompleted: Int = 0,
    val vaccineLabels: List<String> = emptyList(),
    val totalCount: Int = 0,
    val completedCount: Int = 0,
    val submittedCount: Int = 0,
    // Distinct-animal coverage (grain differs from the obligation counts above): a goat due for
    // several vaccines the same day is one animal, completed only when all its drive obligations are.
    // Nullable: absent on legacy cache / mixed-version responses -> card falls back to doses (CDR-R1).
    val totalAnimals: Int? = null,
    val completedAnimals: Int? = null,
    val submittedAnimals: Int? = null,
    val remainingCount: Int = 0,
    val dueCount: Int = 0,
    val overdueCount: Int = 0,
    val deferredCount: Int = 0,
    // Backend-owned cross-surface progress (numerator + denominator + its grain + the rounded
    // percentage). Rendered VERBATIM; the client must not compute its own numerator. Null only on
    // legacy cache / older-backend responses, where the card falls back to the local derivation.
    val progressBasis: String? = null,
    val progressCompleted: Int? = null,
    val progressTotal: Int? = null,
    val progressPct: Int? = null,
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
    val shedLabels: List<String> = emptyList(),
    val dateLabel: String = "",
    val dateKey: String? = null,
    val parkLabel: String = "",
    val parkId: String? = null,
    val assigneeLabel: String? = null,
    /** Park-level drive progress card content (v4); see [CalendarDriveSummary]. */
    val driveSummary: CalendarDriveSummary? = null,
    val statusLabel: String,
    val statusTone: CalendarTone,
    val categoryLabel: String? = null,
    val ctaLabel: String? = null,
    val target: String? = null,
)

@Immutable
data class CalendarFilterOption(
    val value: String,
    val label: String,
    val parentValue: String? = null,
)

@Immutable
data class CalendarMonthFilters(
    val year: Int,
    val month: Int,
    val parkId: String? = null,
    val shedId: String? = null,
    val vaccine: String? = null,
    val status: String? = null,
) {
    val secondaryFilterCount: Int
        get() = listOf(parkId, shedId, vaccine, status).count { !it.isNullOrBlank() }
}

@Immutable
data class CalendarMonthFilterOptions(
    val parks: List<CalendarFilterOption> = emptyList(),
    val sheds: List<CalendarFilterOption> = emptyList(),
    val vaccines: List<CalendarFilterOption> = emptyList(),
    val statuses: List<CalendarFilterOption> = emptyList(),
    val months: List<CalendarFilterOption> = emptyList(),
    val years: List<CalendarFilterOption> = emptyList(),
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

/**
 * L1 day-detail screen: the drives/sheds due on a tapped month day, opened as its OWN
 * screen (a real navigation drill), NOT appended below the month grid. Offline-first sync
 * fields mirror the Calendar reference so the day screen shows syncing/stale over its own
 * Room-backed read and never a blank wall on re-entry.
 */
@Immutable
data class CalendarDayUiState(
    val title: String = "",
    val dateKey: String? = null,
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
    /** Set when a cold cache + failed refresh leaves nothing to render (R50-009).
     *  Non-null only when every segment resource has no cached data AND the
     *  background refresh failed — never gates already-rendered Room data. */
    val errorMessage: String? = null,
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
    val monthFallbackItems: List<CalendarItem> = emptyList(),
    val monthFilters: CalendarMonthFilters = CalendarMonthFilters(year = 2026, month = 1),
    val monthFilterOptions: CalendarMonthFilterOptions = CalendarMonthFilterOptions(),
    val monthEmptyLabel: String = "",
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

    data class TapItem(
        val itemId: String,
        val target: String? = null,
        val dateKey: String? = null,
        val parkId: String? = null,
    ) : CalendarEvent

    data class ApplyMonthFilters(val filters: CalendarMonthFilters) : CalendarEvent

    data object ClearMonthFilters : CalendarEvent

    data object LoadMoreWeek : CalendarEvent

    /** Header refresh — reloads the calendar (mock `.vhead` refresh affordance). */
    data object Refresh : CalendarEvent
}
