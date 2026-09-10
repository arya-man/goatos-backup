package sg.mesha.goatos.core.common.datetime

import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.ZonedDateTime
import java.time.format.DateTimeFormatter
import java.time.temporal.TemporalAccessor
import java.util.Locale

/**
 * The ONE place the phone turns a date into something an operator reads.
 *
 * DATE DISPLAY RULE (maintainer decision 2026-09-10): every VISIBLE date on every
 * Goat OS surface -- admin-web, Android, and any date string the backend composes
 * for a screen -- renders `DD/MM/YYYY`, with slashes, in full. There is deliberately
 * NO compact variant: a list chip, a card subtitle and a chart axis all render the
 * same shape as a table cell, so a reader never has to learn a second date format
 * to compare two screens.
 *
 * This SUPERSEDES the assorted `d MMM` / `EEE d MMM` / `MMM d, yyyy` patterns that
 * were hand-written per screen. Those disagreed with each other and with the web:
 * the same drive read `14 Aug` on the phone and `14-08-2026` on the console, which
 * is the cross-surface disagreement about a business fact this repo bans.
 *
 * WHAT IS NOT A DATE, and is deliberately still allowed its own pattern:
 *  - a TIME on its own (`HH:mm`, `h:mm a`) -- there is no day to render;
 *  - a MONTH heading (`MMM yyyy`) -- it has no day component, so `DD/MM/YYYY` is
 *    undefined for it;
 *  - a WEEKDAY on its own (`EEE`) -- a name, not a date;
 *  - a PARSING pattern for a wire/EXIF string (`yyyyMMdd'T'HHmmssX`) -- that is a
 *    machine format and must not follow display.
 *
 * WIRE FORMATS STAY ISO. A business date sent to the backend, used as a Room key,
 * an idempotency key, or a file name is `yyyy-MM-dd` and must never be switched to
 * slashes -- see [WIRE_DATE].
 *
 * Everything is rendered in the BUSINESS timezone ([IST]), never the device's: a
 * business day is an Asia/Kolkata day, and a phone whose clock is set to another
 * zone must not shift a drive onto a different date than the console shows.
 *
 * Machine gate: `make date-format-guard`.
 */
object GoatOsDates {

    /** Goat OS' business timezone. A business day is an Asia/Kolkata day. */
    val IST: ZoneId = ZoneId.of("Asia/Kolkata")

    /** `14/08/2026` -- the ONE visible date shape. */
    const val DATE_PATTERN: String = "dd/MM/yyyy"

    /** `14/08/2026 09:30` -- a visible timestamp, 24-hour. */
    const val DATE_TIME_PATTERN: String = "dd/MM/yyyy HH:mm"

    /** `14/08/2026, 9:30 AM` -- a visible timestamp where the screen already uses 12-hour clocks. */
    const val DATE_TIME_12H_PATTERN: String = "dd/MM/yyyy, h:mm a"

    /** `Fri 14/08/2026` -- for the few screens where the working day matters as well as the date. */
    const val WEEKDAY_DATE_PATTERN: String = "EEE dd/MM/yyyy"

    /** `2026-08-14` -- WIRE ONLY. Never render this to an operator. */
    const val WIRE_DATE_PATTERN: String = "yyyy-MM-dd"

    val DATE: DateTimeFormatter = ofIst(DATE_PATTERN)
    val DATE_TIME: DateTimeFormatter = ofIst(DATE_TIME_PATTERN)
    val DATE_TIME_12H: DateTimeFormatter = ofIst(DATE_TIME_12H_PATTERN)
    val WEEKDAY_DATE: DateTimeFormatter = ofIst(WEEKDAY_DATE_PATTERN)
    val WIRE_DATE: DateTimeFormatter = ofIst(WIRE_DATE_PATTERN)

    private fun ofIst(pattern: String): DateTimeFormatter =
        DateTimeFormatter.ofPattern(pattern, Locale.ENGLISH).withZone(IST)

    /** `14/08/2026` for an already-resolved business day. */
    fun date(day: LocalDate): String = DATE.format(day)

    /** `14/08/2026` for an instant, resolved to its Asia/Kolkata business day. */
    fun date(instant: Instant): String = DATE.format(instant)

    /** `14/08/2026 09:30` for an instant, in IST. */
    fun dateTime(instant: Instant): String = DATE_TIME.format(instant)

    /** `14/08/2026, 9:30 AM` for an instant, in IST. */
    fun dateTime12h(instant: Instant): String = DATE_TIME_12H.format(instant)

    /** `Fri 14/08/2026` for an already-resolved business day. */
    fun weekdayDate(day: LocalDate): String = WEEKDAY_DATE.format(day)

    /** `Fri 14/08/2026` for an instant, in IST. */
    fun weekdayDate(instant: Instant): String = WEEKDAY_DATE.format(instant)

    /** `14/08/2026` for anything already carrying a date, e.g. a [ZonedDateTime]. */
    fun date(temporal: TemporalAccessor): String = DATE.format(temporal)

    /**
     * `14/08/2026` from a wire `yyyy-MM-dd` business date.
     *
     * An unparseable value is returned UNCHANGED rather than swallowed or replaced with a
     * placeholder: showing the raw string makes a bad row visible to whoever can fix it,
     * whereas a silent "--" reads to an operator as "there is no date", which is a different
     * and worse claim.
     */
    fun fromWireDate(value: String?): String {
        if (value.isNullOrBlank()) return ""
        // exception:exempt a malformed date is RETURNED VERBATIM on purpose (see the doc above):
        // the raw string makes a bad row visible to whoever can fix it, and there is no failure to
        // record -- this is a formatting fallback on a display path, not a swallowed operation.
        return runCatching { date(LocalDate.parse(value)) }.getOrDefault(value)
    }

    /** `14/08/2026 09:30` from a wire ISO-8601 instant; unparseable values return unchanged. */
    fun fromWireInstant(value: String?): String {
        if (value.isNullOrBlank()) return ""
        // exception:exempt same as fromWireDate above -- an unparseable instant is shown raw
        // rather than hidden, and there is no operation that failed to report.
        return runCatching { dateTime(Instant.parse(value)) }.getOrDefault(value)
    }
}
