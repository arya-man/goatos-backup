package sg.mesha.goatos.viewmodel

import java.time.LocalDate
import java.time.LocalTime
import java.time.ZoneId
import java.time.ZonedDateTime
import java.time.format.DateTimeParseException

// telemetry:exempt pure date arithmetic shared by the two plan wizards; the wizard ViewModels own
// the analytics wiring for every step and save.

/** Every business rule in Goat OS reads the farm's own calendar, never the device zone. */
internal val INDIA_BUSINESS_ZONE: ZoneId = ZoneId.of("Asia/Kolkata")

/**
 * Parses the removal cutoff hint the backend serves on the bootstrap
 * (`feed_water_removal_cutoff_time`, "HH:MM" Asia/Kolkata wall-clock; maintainer decision
 * 2026-09-07: the cutoff is CONFIG, not code). Null for a blank or malformed value — an older
 * cached bootstrap, or a farm with no configured cutoff — so the caller falls back to the rule's
 * invariant floor rather than to an hour compiled into the app. The backend remains the authority
 * for planning validity and card visibility.
 */
internal fun parseFeedWaterRemovalCutoff(raw: String?): LocalTime? {
    val text = raw?.trim().orEmpty()
    if (text.isBlank()) return null
    return try {
        // The wire form is HH:MM; a Postgres-shaped HH:MM:SS is tolerated by reading the minute.
        LocalTime.parse(text.take(5))
    } catch (e: DateTimeParseException) {
        // exception:exempt a malformed cutoff string is the server's defect to surface; the
        // picker degrades to the invariant floor and the server's own refusal still decides.
        null
    }
}

/**
 * The earliest date a task NEEDING feed & water removal may be planned for, mirroring the
 * backend's picker rule (maintainer decision 2026-09-03) under the farm's CONFIGURED cutoff: the
 * removal happens the evening BEFORE the work, from [cutoff] IST — so before the cutoff the
 * earliest plannable date is TOMORROW (tonight's removal evening is still ahead), and at/after it
 * the DAY AFTER TOMORROW (tonight's evening has already begun and cannot be assigned any more).
 * Today is never plannable: its removal evening was yesterday.
 *
 * A null [cutoff] means the phone does not know the farm's evening (no bootstrap value yet). The
 * only part of the rule that holds without it is the floor — today is never offerable — so the
 * picker offers from tomorrow and lets the server judge the rest. No hour is invented here.
 *
 * A CLIENT PICKER HINT ONLY — the server still enforces the same rule (422
 * `fasting_window_closed`) and its farm copy is rendered verbatim if the two ever disagree (a
 * clock-skewed phone, or a cutoff changed since the bootstrap was cached).
 */
internal fun earliestPlannableDateWithFeedRemoval(now: ZonedDateTime, cutoff: LocalTime?): LocalDate {
    val ist = now.withZoneSameInstant(INDIA_BUSINESS_ZONE)
    val eveningBegun = cutoff != null && !ist.toLocalTime().isBefore(cutoff)
    val offset = if (eveningBegun) 2L else 1L
    return ist.toLocalDate().plusDays(offset)
}
