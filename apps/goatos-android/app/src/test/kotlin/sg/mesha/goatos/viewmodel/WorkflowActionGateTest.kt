package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.network.dto.WorkflowActionDto
import java.time.Instant

class WorkflowActionGateTest {
    @Test
    fun `scheduled colostrum rounds remain in the actionable task list`() {
        val actions = listOf(
            WorkflowActionDto(actionId = "first", section = "main", actionType = "action"),
            WorkflowActionDto(actionId = "second", section = "colostrum_session", actionType = "action"),
            WorkflowActionDto(actionId = "verify", section = "internal", actionType = "approval"),
        )

        assertEquals(
            listOf("first", "second"),
            operatorVisibleWorkflowActions(actions).map { it.actionId },
        )
    }

    @Test
    fun `first scheduled colostrum waits for immediate first colostrum`() {
        val first = WorkflowActionDto(
            actionId = "first-colostrum",
            actionKey = "first_colostrum",
            seq = 5,
            section = "main",
            actionType = "action",
            status = "pending",
        )
        val colostrum = WorkflowActionDto(
            actionId = "second-colostrum",
            seq = 9,
            section = "colostrum_session",
            actionType = "action",
            status = "pending",
        )

        assertFalse(
            workflowPredecessorsReady(colostrum, listOf(first, colostrum)) { it.status == "completed" },
        )
    }

    @Test
    fun `tag is ordered last and waits for every other task`() {
        val tag = WorkflowActionDto(actionId = "tag", actionKey = "tag_the_kid", seq = 8, section = "main")
        val colostrum = WorkflowActionDto(
            actionId = "later-colostrum",
            actionKey = "colostrum_day_2_2200",
            seq = 14,
            section = "colostrum_session",
            status = "pending",
        )

        assertTrue(workflowDisplayOrder(tag) > workflowDisplayOrder(colostrum))
        assertFalse(
            workflowPredecessorsReady(tag, listOf(tag, colostrum)) { it.status == "completed" },
        )
    }

    @Test
    fun `future access label shows full scheduled date and time`() {
        assertEquals(
            "Available 29 Jul · 07:00",
            workflowAccessLabel(
                blockedReason = "not_yet_due",
                due = Instant.parse("2026-07-29T01:30:00Z"),
                now = Instant.parse("2026-07-28T12:00:00Z"),
            ),
        )
    }

    @Test
    fun `kid weight is a numeric kilogram answer`() {
        val weight = WorkflowActionDto(actionKey = "take_weight", actionType = "question")

        assertEquals("kg", workflowNumericAnswerUnit(weight))
        assertTrue(workflowNumericAnswerValid("2.35"))
        assertFalse(workflowNumericAnswerValid("2.0 – 2.5 kg"))
        assertFalse(workflowNumericAnswerValid("0"))
    }

    @Test
    fun `backend blocked video action cannot open camera`() {
        val action = WorkflowActionDto(
            actionType = "action",
            requiresVideo = true,
            status = "pending",
            blocked = true,
        )

        assertFalse(canRecordWorkflowVideo(action, draftsSubmitting = false, predecessorsReady = true))
    }

    @Test
    fun `ready unblocked video action can open camera`() {
        val action = WorkflowActionDto(
            actionType = "action",
            requiresVideo = true,
            status = "pending",
            blocked = false,
        )

        assertTrue(canRecordWorkflowVideo(action, draftsSubmitting = false, predecessorsReady = true))
    }
}
