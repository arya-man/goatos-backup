package sg.mesha.goatos.feature.calendar

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
 * A drive/shed card shown under a week day or inside a day sheet. [ctaLabel] and
 * [target] are backend-provided per principal (operator "Open drive" / leadership
 * "View drive status"); a null [ctaLabel] renders a dimmed, non-drillable card.
 */
data class CalendarItem(
    val id: String,
    val title: String,
    val subtitle: String,
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

/** The `ovl-day` sheet: the sheds for a tapped month day (surfaced as a bottom section). */
data class DaySheetUiState(
    val title: String,
    val items: List<CalendarItem>,
    val emptyLabel: String,
)

data class CalendarUiState(
    val eyebrow: String = "",
    val title: String = "",
    val selectedDateLabel: String = "",
    val windowLabel: String? = null,
    val segments: List<CalendarSegment> = emptyList(),
    val selectedSegmentId: String = "",
    // WEEK
    val weekDays: List<CalendarWeekDay> = emptyList(),
    val weekItems: List<CalendarItem> = emptyList(),
    val weekEmptyLabel: String = "",
    // MONTH
    val monthLabel: String = "",
    val monthWeekdayLabels: List<String> = emptyList(),
    val monthDays: List<CalendarMonthDay> = emptyList(),
    val monthHint: String = "",
    val daySheet: DaySheetUiState? = null,
    // HISTORY
    val historyLabel: String = "",
    val historyRows: List<CalendarHistoryRow> = emptyList(),
    val historyEmptyLabel: String = "",
)

/** User intents. The ViewModel maps each to a backend read/drill — the screen decides nothing. */
sealed interface CalendarEvent {
    data class SelectSegment(val segmentId: String) : CalendarEvent

    data class TapDay(val dateKey: String) : CalendarEvent

    data class TapItem(val itemId: String) : CalendarEvent

    /** Header refresh — reloads the calendar (mock `.vhead` refresh affordance). */
    data object Refresh : CalendarEvent
}
