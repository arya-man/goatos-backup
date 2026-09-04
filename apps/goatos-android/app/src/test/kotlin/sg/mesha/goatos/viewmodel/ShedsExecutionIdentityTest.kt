package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.network.dto.VaccinationExecutionRowDto
import sg.mesha.goatos.core.network.dto.currentScheduleDate
import sg.mesha.goatos.feature.sheds.ShedStatus
import java.time.LocalDate
import java.time.ZoneId
import java.time.ZonedDateTime

class ShedsExecutionIdentityTest {

    @Test
    fun `one park task spanning two sheds produces distinct stable card keys`() {
        val gandhiOne = executionCardId("shed-gandhi-1", "task-park", "batch-park", "drive-park")
        val gandhiTwo = executionCardId("shed-gandhi-2", "task-park", "batch-park", "drive-park")

        assertEquals("shed:shed-gandhi-1|partition:whole|task:task-park", gandhiOne)
        assertEquals(gandhiOne, executionCardId("shed-gandhi-1", "task-park", "batch-park", "drive-park"))
        assertNotEquals(gandhiOne, gandhiTwo)
    }

    @Test
    fun `shed key falls back through batch drive and shed identity`() {
        assertEquals("shed:shed-1|partition:whole|batch:batch-1", executionCardId("shed-1", null, "batch-1", "drive-1"))
        assertEquals("shed:shed-1|partition:whole|drive:drive-1", executionCardId("shed-1", null, null, "drive-1"))
        assertEquals("shed:shed-1|partition:whole", executionCardId("shed-1", null, null, null))
    }

    @Test
    fun `sibling partitions under one shed produce distinct card keys`() {
        val part1 = executionCardId("shed-castro", "task-a", "batch-a", null, "Part 1")
        val part2 = executionCardId("shed-castro", "task-a", "batch-a", null, "2")

        assertEquals("shed:shed-castro|partition:1|task:task-a", part1)
        assertEquals("shed:shed-castro|partition:2|task:task-a", part2)
        assertNotEquals(part1, part2)
    }

    @Test
    fun `execution card key ignores backend row metadata splits`() {
        val bluetongue = VaccinationExecutionRowDto(
            shedId = "shed-84",
            partitionLabel = "1",
            batchId = "batch-drive",
            driveId = "drive-aug",
            sopTaskId = "task-bt-sp",
            sopVersionId = "sop-v1",
            sopTaskRowVersion = 11,
            driveName = "Bluetongue",
        )
        val sheeppox = bluetongue.copy(
            sopVersionId = "sop-v2",
            sopTaskRowVersion = 13,
            driveName = "Sheeppox",
        )

        assertEquals("shed:shed-84|partition:1|task:task-bt-sp", bluetongue.executionCardId())
        assertEquals(bluetongue.executionCardId(), sheeppox.executionCardId())
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
    fun `overview adherence uses selected day rows and excludes prior completed history`() {
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
        val counts = executionCounts(selectedDayRows)
        val summary = protocolAdherenceSummary(counts)

        assertEquals(ExecutionCounts(target = 210, open = 210, done = 0), counts)
        assertEquals(210, summary?.expectedCount)
        assertEquals(0, summary?.submittedCount)
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
        // Submitted-and-awaiting-verification is NOT overdue, even with a long-past dueDate.
        // The row is already counted once as review; counting it again as overdue is the same
        // double-count that painted "In review" + "Overdue" together on the shed card.
        assertEquals(0, summary?.overdueItemCount)
        assertEquals(25, summary?.acceptedPercent)
    }

    @Test
    fun `overview adherence uses explicit accepted animals for mixed review shed rows`() {
        val rows = listOf(
            VaccinationExecutionRowDto(
                targetCount = 70,
                openCount = 0,
                doneCount = 70,
                acceptedCount = 50,
                reviewCount = 20,
                workState = "verification_pending",
                sopStatus = "submitted",
            ),
        )

        val summary = protocolAdherenceSummary(rows)

        assertEquals(70, summary?.expectedCount)
        assertEquals(70, summary?.submittedCount)
        assertEquals(50, summary?.acceptedCount)
        assertEquals(1, summary?.reviewItemCount)
        assertEquals(71, summary?.acceptedPercent)
    }

    @Test
    fun `overview adherence marks multi page data incomplete instead of presenting page totals as final`() {
        val firstPageRows = listOf(
            VaccinationExecutionRowDto(
                targetCount = 20,
                openCount = 10,
                doneCount = 10,
                acceptedCount = 8,
                reviewCount = 2,
                workState = "verification_pending",
                sopStatus = "submitted",
            ),
        )
        val summary = protocolAdherenceSummary(
            rows = firstPageRows,
            counts = executionCounts(firstPageRows),
            isComplete = false,
        )

        assertEquals(false, summary?.isComplete)
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
    fun `all animals scanned but unsubmitted shed is not green done`() {
        val scannedButDraft = listOf(
            VaccinationExecutionRowDto(
                targetCount = 3,
                openCount = 0,
                doneCount = 3,
                workState = "in_progress",
                primaryActionKey = "submit",
                sopStatus = "draft",
            ),
        )

        assertEquals(ShedStatus.PENDING, shedStatusForRows(scannedButDraft))
    }

    @Test
    fun `uploaded proof without final submit is not in review`() {
        val uploadedButNotSubmitted = listOf(
            VaccinationExecutionRowDto(
                targetCount = 30,
                openCount = 0,
                doneCount = 30,
                workState = "in_progress",
                proofStatus = "uploaded",
                verificationStatus = "",
                sopStatus = "draft",
                operatorCanContinue = true,
                primaryActionKey = "submit",
            ),
        )

        val summary = protocolAdherenceSummary(uploadedButNotSubmitted)

        assertEquals(ShedStatus.PENDING, shedStatusForRows(uploadedButNotSubmitted))
        assertEquals(0, summary?.reviewItemCount)
        assertFalse(uploadedButNotSubmitted.opensSubmittedRecordOnly())
    }

    @Test
    fun `review-like backend work state is not review while operator can still continue`() {
        val proofReadyButNotSubmitted = listOf(
            VaccinationExecutionRowDto(
                targetCount = 30,
                openCount = 0,
                doneCount = 30,
                workState = "verification_pending",
                proofStatus = "uploaded",
                verificationStatus = "pending",
                sopStatus = "needs_review",
                operatorCanContinue = true,
                primaryActionKey = "submit",
            ),
        )

        val summary = protocolAdherenceSummary(proofReadyButNotSubmitted)

        assertEquals(ShedStatus.PENDING, shedStatusForRows(proofReadyButNotSubmitted))
        assertEquals(0, summary?.reviewItemCount)
        assertFalse(proofReadyButNotSubmitted.opensSubmittedRecordOnly())
    }

    @Test
    fun `accepted shed is green done`() {
        val accepted = listOf(
            VaccinationExecutionRowDto(
                targetCount = 3,
                openCount = 0,
                doneCount = 3,
                workState = "closed",
                sopStatus = "accepted",
            ),
        )

        assertEquals(ShedStatus.DONE, shedStatusForRows(accepted))
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
    fun `verification pending shed is in review and opens record only`() {
        val inReview = listOf(
            VaccinationExecutionRowDto(
                targetCount = 3,
                openCount = 0,
                doneCount = 3,
                workState = "verification_pending",
                proofStatus = "uploaded",
                verificationStatus = "pending",
                sopStatus = "submitted", // Backend authority: TERMINAL sopStatus signals all work done
            ),
        )

        assertEquals(ShedStatus.PENDING, shedStatusForRows(inReview))
        assertTrue(inReview.opensSubmittedRecordOnly())
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

    @Test
    fun `card lock invariant - needs_review with open work (17-11-6) opens for scan, not record-only`() {
        // Field bug reproduction: target=17 done=11 open=6, needs_review, backend says can-continue.
        val needsReview = listOf(
            VaccinationExecutionRowDto(
                targetCount = 17,
                doneCount = 11,
                openCount = 6,
                sopStatus = "needs_review",
                verificationStatus = "pending",
                workState = "verification_pending",
                operatorCanContinue = true,
                operatorLockedReason = "none",
            ),
        )

        assertFalse(
            "17/11/6 needs_review with operatorCanContinue=true must open scan, never record-only",
            needsReview.opensSubmittedRecordOnly(),
        )
    }

    @Test
    fun `card lock invariant - final submit (17-17-0) locks to record-only`() {
        val finalSubmit = listOf(
            VaccinationExecutionRowDto(
                targetCount = 17,
                doneCount = 17,
                openCount = 0,
                sopStatus = "accepted",
                verificationStatus = "accepted",
                workState = "completed",
                operatorCanContinue = false,
                operatorLockedReason = "final_submitted",
            ),
        )

        assertTrue(
            "17/17/0 final submission must be record-only",
            finalSubmit.opensSubmittedRecordOnly(),
        )
    }

    @Test
    fun `card lock invariant - missing backend fields fall back to openCount guard`() {
        // Backward compat: API responses that predate operatorCanContinue must still never lock
        // while open work remains, and must lock once openCount hits zero.
        val olderApiOpenWork = listOf(
            VaccinationExecutionRowDto(
                targetCount = 17,
                doneCount = 11,
                openCount = 6,
                operatorCanContinue = null,
                operatorLockedReason = null,
            ),
        )
        assertFalse(
            "missing field + openCount > 0 must NOT lock",
            olderApiOpenWork.opensSubmittedRecordOnly(),
        )

        val olderApiDone = listOf(
            VaccinationExecutionRowDto(
                targetCount = 17,
                doneCount = 17,
                openCount = 0,
                sopStatus = "accepted", // fallback also requires a terminal sopStatus, not just openCount==0
                operatorCanContinue = null,
                operatorLockedReason = null,
            ),
        )
        assertTrue(
            "missing field + openCount == 0 + terminal sopStatus must lock (fallback)",
            olderApiDone.opensSubmittedRecordOnly(),
        )
    }
}
