package sg.mesha.goatos.core.ui.filters

import java.time.LocalDate
import java.time.format.DateTimeFormatter
import java.util.Locale

/**
 * The shared FILTER BAR model over a phone worklist (maintainer request 2026-09-10): the
 * Weighing task list and the Preventive Care round list carry the same three controls above
 * their cards -- a Pending / Completed status pill pair, an inclusive business-date window and
 * one pen -- and nothing below the bar changes.
 *
 * The window is a BUSINESS-DATE range in Asia/Kolkata, inclusive on both ends; a single day is
 * a window whose two ends are equal. The default is today through the next seven days.
 */
enum class WorklistStatus { PENDING, COMPLETED }

data class WorklistDateWindow(val from: LocalDate, val to: LocalDate) {
    val isSingleDay: Boolean get() = from == to
    val fromIso: String get() = from.toString()
    val toIso: String get() = to.toString()

    companion object {
        /** The default window: today and the next seven days (maintainer decision 2026-09-10). */
        const val DEFAULT_DAYS_AHEAD: Long = 7L
        fun default(today: LocalDate): WorklistDateWindow = WorklistDateWindow(today, today.plusDays(DEFAULT_DAYS_AHEAD))
    }
}

/** ONE pen as the backend names it: the shed's id, its partition label, and its display. */
data class WorklistPen(
    val shedId: String,
    val partitionLabel: String,
    val label: String,
) {
    val key: String get() = "$shedId|$partitionLabel"
}

/** One pen the picker can offer, with the backend's own count of work it holds in the window. */
data class WorklistPenOption(
    val shedId: String,
    val partitionLabel: String,
    val label: String,
    val parkName: String,
    val count: Int,
) {
    val pen: WorklistPen get() = WorklistPen(shedId = shedId, partitionLabel = partitionLabel, label = label)
}

private val dayMonth: DateTimeFormatter = DateTimeFormatter.ofPattern("d MMM", Locale.ENGLISH)
private val weekdayDayMonth: DateTimeFormatter = DateTimeFormatter.ofPattern("EEE d MMM", Locale.ENGLISH)

/** "Today · 10 Sep", "Mon 14 Sep", or "10 Sep – 17 Sep". */
fun WorklistDateWindow.displayLabel(today: LocalDate, todayWord: String): String = when {
    isSingleDay && from == today -> "$todayWord · ${from.format(dayMonth)}"
    isSingleDay -> from.format(weekdayDayMonth)
    else -> "${from.format(dayMonth)} – ${to.format(dayMonth)}"
}

/** The date header a list draws between cards when the window spans more than one day. */
fun worklistDateHeader(dateIso: String, today: LocalDate, todayWord: String): String {
    // exception:exempt date parsing for display; a value that is not a date renders verbatim
    val date = runCatching { LocalDate.parse(dateIso) }.getOrNull() ?: return dateIso
    return if (date == today) "$todayWord · ${date.format(weekdayDayMonth)}" else date.format(weekdayDayMonth)
}

/** Formats a picked day for the From / To fields of the calendar sheet. */
fun worklistFieldDate(date: LocalDate?): String = date?.format(weekdayDayMonth) ?: "—"
