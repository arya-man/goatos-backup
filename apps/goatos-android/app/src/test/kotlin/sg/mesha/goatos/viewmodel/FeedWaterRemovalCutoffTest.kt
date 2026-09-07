package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.time.LocalDate
import java.time.LocalTime
import java.time.ZoneId
import java.time.ZonedDateTime

/**
 * The picker rule both plan wizards mirror (maintainer decision 2026-09-03) under the farm's
 * CONFIGURED cutoff (maintainer decision 2026-09-07: config, not code). Every case pins its OWN
 * IST instant — never the device clock — because this rule's whole content is what the clock
 * reads against the configured time (the exact defect class the vaccination hour-anchored
 * fixtures shipped).
 */
class FeedWaterRemovalCutoffTest {

    private val eightPm: LocalTime = LocalTime.of(20, 0)

    private fun ist(date: String, hour: Int, minute: Int = 0): ZonedDateTime =
        ZonedDateTime.of(LocalDate.parse(date).atTime(hour, minute), ZoneId.of("Asia/Kolkata"))

    @Test
    fun `before the cutoff the earliest plannable date is tomorrow`() {
        assertEquals(LocalDate.parse("2026-09-04"), earliestPlannableDateWithFeedRemoval(ist("2026-09-03", 8), eightPm))
        assertEquals(LocalDate.parse("2026-09-04"), earliestPlannableDateWithFeedRemoval(ist("2026-09-03", 19, 59), eightPm))
    }

    @Test
    fun `at and after the cutoff the earliest plannable date is the day after tomorrow`() {
        assertEquals(LocalDate.parse("2026-09-05"), earliestPlannableDateWithFeedRemoval(ist("2026-09-03", 20), eightPm))
        assertEquals(LocalDate.parse("2026-09-05"), earliestPlannableDateWithFeedRemoval(ist("2026-09-03", 23, 59), eightPm))
    }

    @Test
    fun `the hour is the farm's configured cutoff, not a compiled-in one`() {
        // The same 20:14 instant: under a 21:00 evening tomorrow is still plannable; under a
        // 19:30 evening it is already gone.
        val twentyFourteen = ist("2026-09-03", 20, 14)
        assertEquals(LocalDate.parse("2026-09-04"), earliestPlannableDateWithFeedRemoval(twentyFourteen, LocalTime.of(21, 0)))
        assertEquals(LocalDate.parse("2026-09-05"), earliestPlannableDateWithFeedRemoval(twentyFourteen, LocalTime.of(19, 30)))
        // ...and a minute-grained cutoff flips exactly on its minute.
        assertEquals(LocalDate.parse("2026-09-04"), earliestPlannableDateWithFeedRemoval(ist("2026-09-03", 19, 29), LocalTime.of(19, 30)))
        assertEquals(LocalDate.parse("2026-09-05"), earliestPlannableDateWithFeedRemoval(ist("2026-09-03", 19, 30), LocalTime.of(19, 30)))
    }

    @Test
    fun `an unknown cutoff keeps only the invariant floor and invents no hour`() {
        // No bootstrap value: today is still never plannable, but no evening is assumed, so even
        // 23:59 offers tomorrow and leaves the judgement to the server.
        assertEquals(LocalDate.parse("2026-09-04"), earliestPlannableDateWithFeedRemoval(ist("2026-09-03", 23, 59), null))
        assertEquals(LocalDate.parse("2026-09-04"), earliestPlannableDateWithFeedRemoval(ist("2026-09-03", 0), null))
    }

    @Test
    fun `today is never plannable`() {
        // Its removal evening was yesterday, whatever the hour.
        val morning = earliestPlannableDateWithFeedRemoval(ist("2026-09-03", 0), eightPm)
        assertEquals(LocalDate.parse("2026-09-04"), morning)
    }

    @Test
    fun `the rule reads the farm clock never the device zone`() {
        // 15:00 UTC on 2026-09-03 is 20:30 IST — past the cutoff even though the UTC hour is not.
        val utcAfternoon = ZonedDateTime.of(
            LocalDate.parse("2026-09-03").atTime(15, 0),
            ZoneId.of("UTC"),
        )
        assertEquals(LocalDate.parse("2026-09-05"), earliestPlannableDateWithFeedRemoval(utcAfternoon, eightPm))
    }

    @Test
    fun `the bootstrap value parses as HH MM and degrades to unknown when absent or malformed`() {
        assertEquals(LocalTime.of(20, 0), parseFeedWaterRemovalCutoff("20:00"))
        assertEquals(LocalTime.of(19, 30), parseFeedWaterRemovalCutoff(" 19:30 "))
        assertEquals(LocalTime.of(21, 15), parseFeedWaterRemovalCutoff("21:15:00"))
        assertNull(parseFeedWaterRemovalCutoff(null))
        assertNull(parseFeedWaterRemovalCutoff(""))
        assertNull(parseFeedWaterRemovalCutoff("8pm"))
        assertNull(parseFeedWaterRemovalCutoff("24:00"))
    }
}
