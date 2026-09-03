package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Test
import java.time.LocalDate
import java.time.ZoneId
import java.time.ZonedDateTime

/**
 * The 20:00 IST picker rule both plan wizards mirror (maintainer decision 2026-09-03). Every case
 * pins its OWN IST instant — never the device clock — because this rule's whole content is what
 * the clock reads (the exact defect class the vaccination hour-anchored fixtures shipped).
 */
class FeedWaterRemovalCutoffTest {

    private fun ist(date: String, hour: Int, minute: Int = 0): ZonedDateTime =
        ZonedDateTime.of(LocalDate.parse(date).atTime(hour, minute), ZoneId.of("Asia/Kolkata"))

    @Test
    fun `before 20 00 IST the earliest plannable date is tomorrow`() {
        assertEquals(LocalDate.parse("2026-09-04"), earliestPlannableDateWithFeedRemoval(ist("2026-09-03", 8)))
        assertEquals(LocalDate.parse("2026-09-04"), earliestPlannableDateWithFeedRemoval(ist("2026-09-03", 19, 59)))
    }

    @Test
    fun `at and after 20 00 IST the earliest plannable date is the day after tomorrow`() {
        assertEquals(LocalDate.parse("2026-09-05"), earliestPlannableDateWithFeedRemoval(ist("2026-09-03", 20)))
        assertEquals(LocalDate.parse("2026-09-05"), earliestPlannableDateWithFeedRemoval(ist("2026-09-03", 23, 59)))
    }

    @Test
    fun `today is never plannable`() {
        // Its removal evening was yesterday, whatever the hour.
        val morning = earliestPlannableDateWithFeedRemoval(ist("2026-09-03", 0))
        assertEquals(LocalDate.parse("2026-09-04"), morning)
    }

    @Test
    fun `the rule reads the farm clock never the device zone`() {
        // 15:00 UTC on 2026-09-03 is 20:30 IST — past the cutoff even though the UTC hour is not.
        val utcAfternoon = ZonedDateTime.of(
            LocalDate.parse("2026-09-03").atTime(15, 0),
            ZoneId.of("UTC"),
        )
        assertEquals(LocalDate.parse("2026-09-05"), earliestPlannableDateWithFeedRemoval(utcAfternoon))
    }
}
