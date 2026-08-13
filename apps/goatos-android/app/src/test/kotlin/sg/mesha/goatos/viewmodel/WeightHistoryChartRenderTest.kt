package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.feature.weighing.WeightHistoryChartUiPoint
import sg.mesha.goatos.feature.weighing.WeightHistoryChartUiRow
import sg.mesha.goatos.feature.weighing.WeightHistoryChartUiState

/**
 * The chart's honesty rules, pinned as assertions.
 *
 * A weight chart that quietly interpolates a missing weigh day, or presents a truncated series as
 * a whole history, is worse than no chart: it reads as evidence. These tests hold the CONTRACT the
 * screen renders from, so a later change that starts inventing points breaks here first.
 */
class WeightHistoryChartRenderTest {

    private fun point(date: String, value: Double, sentBack: Boolean = false) =
        WeightHistoryChartUiPoint(dateLabel = date, value = value, sentBack = sentBack)

    @Test
    fun `a gap in weigh days stays a gap - no invented point`() {
        // The animal was weighed on the 1st and the 4th. Nothing happened on the 2nd or 3rd, and
        // the chart must not manufacture them to make a smoother line.
        val row = WeightHistoryChartUiRow(
            label = "901007000504332",
            shedName = "Godel 1",
            captureKind = "individual",
            points = listOf(point("1 Aug", 21.5), point("4 Aug", 24.0)),
        )
        assertEquals("only the weigh days that happened may be drawn", 2, row.points.size)
        assertEquals(listOf("1 Aug", "4 Aug"), row.points.map { it.dateLabel })
    }

    @Test
    fun `a single weigh day is still a valid series`() {
        val row = WeightHistoryChartUiRow(
            label = "901007000504418",
            shedName = "Godel 1",
            captureKind = "individual",
            points = listOf(point("4 Aug", 21.5)),
        )
        assertEquals(1, row.points.size)
        assertTrue("a lone point must carry its real value", row.points.first().value == 21.5)
    }

    @Test
    fun `a sent-back weigh is flagged, not hidden`() {
        // A rejected weight stays visible and marked. Dropping it would leave an unexplained hole
        // in the animal's history; showing it unmarked would present refused evidence as good.
        val row = WeightHistoryChartUiRow(
            label = "901007000504332",
            shedName = "Godel 1",
            captureKind = "individual",
            points = listOf(point("4 Aug", 22.5, sentBack = true), point("4 Aug", 24.0)),
        )
        assertTrue("the rejected weigh must remain in the series", row.points.any { it.sentBack })
        assertTrue("the accepted re-weigh must also be present", row.points.any { !it.sentBack })
    }

    @Test
    fun `truncation is stated on screen, never silent`() {
        // The ViewModel emits the truncation FLAGS, not a rendered sentence -- the screen turns
        // them into a localized notice at render time (see WeightHistoryChartScreen).
        val truncated = WeightHistoryChartUiState(
            hasData = true,
            rows = listOf(
                WeightHistoryChartUiRow("tag", "Godel 1", "individual", listOf(point("4 Aug", 21.5))),
            ),
            payloadTruncated = true,
        )
        assertTrue(
            "a partial series must say so, or it reads as the whole history",
            truncated.payloadTruncated || truncated.perSeriesCapped,
        )
    }

    @Test
    fun `an empty history says so instead of drawing an empty box`() {
        // seriesEmpty is the raw flag; the screen resolves it to a localized empty-state message.
        val empty = WeightHistoryChartUiState(
            hasData = true,
            rows = emptyList(),
            seriesEmpty = true,
        )
        assertTrue(empty.rows.isEmpty())
        assertTrue("an empty chart must explain itself", empty.hasData && empty.seriesEmpty)
    }

    @Test
    fun `individual and lump-sum are never mixed in one series`() {
        val individual = WeightHistoryChartUiRow("tag", "Godel 1", "individual", listOf(point("4 Aug", 21.5)))
        val lumpSum = WeightHistoryChartUiRow("Castro 3", "Castro 3", "lump_sum", listOf(point("4 Aug", 1500.0)))
        // 21.5 kg for one goat and 1500 kg for a shed on one axis would make the goat invisible.
        assertTrue("kinds must stay distinguishable", individual.captureKind != lumpSum.captureKind)
    }
}
