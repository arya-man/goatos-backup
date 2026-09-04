package sg.mesha.goatos.viewmodel

import java.time.LocalDate
import java.time.ZoneId
import java.time.ZonedDateTime

// telemetry:exempt pure date arithmetic shared by the two plan wizards; the wizard ViewModels own
// the analytics wiring for every step and save.

/** Every business rule in Goat OS reads the farm's own calendar, never the device zone. */
internal val INDIA_BUSINESS_ZONE: ZoneId = ZoneId.of("Asia/Kolkata")

/** The removal evening opens at 20:00 IST — the SERVER's gate; the client only mirrors it in pickers. */
internal const val FEED_WATER_REMOVAL_CUTOFF_HOUR = 20

/**
 * The earliest date a task NEEDING feed & water removal may be planned for, mirroring the
 * backend's picker rule (maintainer decision 2026-09-03): the removal happens the evening BEFORE
 * the work, from 20:00 IST — so before 20:00 IST the earliest plannable date is TOMORROW (tonight's
 * removal evening is still ahead), and at/after 20:00 IST it is the DAY AFTER TOMORROW (tonight's
 * evening has already begun and cannot be assigned any more). Today is never plannable: its
 * removal evening was yesterday.
 *
 * A CLIENT MIRROR ONLY — the server still enforces the same rule (422 `fasting_window_closed`)
 * and its farm copy is rendered verbatim if the two ever disagree (a clock-skewed phone).
 */
internal fun earliestPlannableDateWithFeedRemoval(now: ZonedDateTime): LocalDate {
    val ist = now.withZoneSameInstant(INDIA_BUSINESS_ZONE)
    val offset = if (ist.hour >= FEED_WATER_REMOVAL_CUTOFF_HOUR) 2L else 1L
    return ist.toLocalDate().plusDays(offset)
}
