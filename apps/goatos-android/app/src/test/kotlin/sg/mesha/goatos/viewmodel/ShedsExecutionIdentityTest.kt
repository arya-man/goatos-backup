package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.network.dto.VaccinationExecutionRowDto
import sg.mesha.goatos.core.network.dto.currentScheduleDate
import java.time.LocalDate
import java.time.ZoneId
import java.time.ZonedDateTime

class ShedsExecutionIdentityTest {

    @Test
    fun `one park task spanning two sheds produces distinct stable card keys`() {
        val gandhiOne = executionCardId("shed-gandhi-1", "task-park", "batch-park", "drive-park")
        val gandhiTwo = executionCardId("shed-gandhi-2", "task-park", "batch-park", "drive-park")

        assertEquals("shed:shed-gandhi-1|task:task-park", gandhiOne)
        assertEquals(gandhiOne, executionCardId("shed-gandhi-1", "task-park", "batch-park", "drive-park"))
        assertNotEquals(gandhiOne, gandhiTwo)
    }

    @Test
    fun `shed key falls back through batch drive and shed identity`() {
        assertEquals("shed:shed-1|batch:batch-1", executionCardId("shed-1", null, "batch-1", "drive-1"))
        assertEquals("shed:shed-1|drive:drive-1", executionCardId("shed-1", null, null, "drive-1"))
        assertEquals("shed:shed-1", executionCardId("shed-1", null, null, null))
    }

    @Test
    fun `shed totals use backend animal counts instead of aggregated row count`() {
        val counts = executionCounts(
            listOf(
                VaccinationExecutionRowDto(targetCount = 2, openCount = 2, doneCount = 0),
                VaccinationExecutionRowDto(targetCount = 3, openCount = 1, doneCount = 2),
            ),
        )

        assertEquals(ExecutionCounts(target = 5, open = 3, done = 2), counts)
    }

    @Test
    fun `overview adherence uses execution queue totals not global adherence totals`() {
        val summary = protocolAdherenceSummary(ExecutionCounts(target = 324, open = 210, done = 114))

        assertEquals(324, summary?.expectedCount)
        assertEquals(114, summary?.submittedCount)
        assertEquals(0, summary?.acceptedCount)
        assertEquals(0, summary?.acceptedPercent)
    }

    @Test
    fun `overview adherence keeps selected drive rows and excludes unrelated completed history`() {
        val rows = listOf(
            VaccinationExecutionRowDto(
                batchId = "drive-current",
                dueDate = "2026-07-24",
                targetCount = 114,
                openCount = 0,
                doneCount = 114,
                sopStatus = "accepted",
            ),
            VaccinationExecutionRowDto(
                batchId = "drive-current",
                dueDate = "2026-07-25",
                targetCount = 210,
                openCount = 210,
                doneCount = 0,
            ),
            VaccinationExecutionRowDto(
                batchId = "drive-future",
                dueDate = "2026-07-26",
                targetCount = 324,
                openCount = 324,
                doneCount = 0,
            ),
            VaccinationExecutionRowDto(
                batchId = "drive-old",
                dueDate = "2026-07-24",
                targetCount = 438,
                openCount = 0,
                doneCount = 438,
                sopStatus = "accepted",
            ),
        )

        val selectedDayRows = rows.filter { it.currentScheduleDate == "2026-07-25" }
        val counts = executionCounts(adherenceWindowRows(rows, selectedDayRows, LocalDate.parse("2026-07-25")))
        val summary = protocolAdherenceSummary(counts)

        assertEquals(ExecutionCounts(target = 324, open = 210, done = 114), counts)
        assertEquals(324, summary?.expectedCount)
        assertEquals(114, summary?.submittedCount)
    }

    @Test
    fun `overview adherence separates submitted review overdue and accepted states`() {
        val rows = listOf(
            VaccinationExecutionRowDto(
                targetCount = 32,
                openCount = 0,
                doneCount = 32,
                sopStatus = "needs_review",
                workState = "verification_pending",
                dueDate = "2000-01-01",
            ),
            VaccinationExecutionRowDto(
                targetCount = 11,
                openCount = 0,
                doneCount = 11,
                sopStatus = "accepted",
                workState = "closed",
            ),
        )

        val summary = protocolAdherenceSummary(rows)

        assertEquals(43, summary?.expectedCount)
        assertEquals(43, summary?.submittedCount)
        assertEquals(11, summary?.acceptedCount)
        assertEquals(1, summary?.reviewItemCount)
        assertEquals(1, summary?.overdueItemCount)
        assertEquals(25, summary?.acceptedPercent)
    }

    @Test
    fun `all animals done but draft shed proof still opens execution`() {
        val godelOne = listOf(
            VaccinationExecutionRowDto(
                targetCount = 2,
                openCount = 0,
                doneCount = 2,
                workState = "in_progress",
                primaryActionKey = "submit",
                sopStatus = "draft",
            ),
        )

        assertFalse(godelOne.opensSubmittedRecordOnly())
    }

    @Test
    fun `submitted shed opens record only`() {
        val submitted = listOf(
            VaccinationExecutionRowDto(
                targetCount = 2,
                openCount = 0,
                doneCount = 2,
                primaryActionKey = "record",
                sopStatus = "submitted",
            ),
        )

        assertTrue(submitted.opensSubmittedRecordOnly())
    }

    @Test
    fun `operator shed queue window is yesterday through today plus five in India time`() {
        val window = OperatorWorkWindow.today(
            ZonedDateTime.of(2026, 7, 21, 9, 30, 0, 0, ZoneId.of("Asia/Kolkata")),
        )

        assertEquals(null, window.asOf)
        // Upper bound covers lastDay (today+5 = 26 Jul).
        assertEquals("2026-07-27T09:30:00+05:30", window.dueBefore)
        assertEquals("Today · Tue 21 Jul", window.todayLabel)
        // Strip: yesterday (Mon 20) → today+5 (Sun 26); landing stays on today.
        assertEquals(java.time.LocalDate.of(2026, 7, 20), window.firstDay)
        assertEquals(java.time.LocalDate.of(2026, 7, 26), window.lastDay)
        assertEquals("Mon 20 Jul → Sun 26 Jul", window.windowLabel)
    }

    @Test
    fun `execution due date parser uses backend dueDate not stage or drive labels`() {
        assertEquals("2026-07-24", parseExecutionDate("2026-07-24")?.toString())
        assertEquals("2026-07-24", parseExecutionDate("2026-07-24T23:00:00+05:30")?.toString())
        assertEquals(null, parseExecutionDate("Adult"))
    }

    @Test
    fun `execution schedule date uses OpenAPI dueDate field`() {
        val row = VaccinationExecutionRowDto(
            dueDate = "2026-08-03",
        )

        assertEquals("2026-08-03", row.currentScheduleDate)
    }
}
