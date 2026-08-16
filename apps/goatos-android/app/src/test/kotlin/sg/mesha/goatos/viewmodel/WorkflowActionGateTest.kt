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

        assertFalse(
            canRecordWorkflowVideo(
                action,
                blocked = workflowBlockedForOperator(action, isDeath = false, predecessorsReady = true),
                draftsSubmitting = false,
            ),
        )
    }

    /**
     * Death holds BOTH videos as local drafts until Submit, so the backend has seen no completion
     * and correctly reports the second video blocked behind the first. Honouring that verbatim left
     * the operator with a recorded death video and no post-mortem control (maintainer, 2026-08-10).
     */
    @Test
    fun `death post-mortem opens once the first video exists as a local draft`() {
        val postMortem = WorkflowActionDto(
            actionKey = "post_mortem_video",
            actionType = "action",
            requiresVideo = true,
            status = "pending",
            blocked = true,
            blockedReason = "previous_action",
        )

        assertFalse(workflowBlockedForOperator(postMortem, isDeath = true, predecessorsReady = true))
        assertTrue(
            canRecordWorkflowVideo(
                postMortem,
                blocked = workflowBlockedForOperator(postMortem, isDeath = true, predecessorsReady = true),
                draftsSubmitting = false,
            ),
        )
    }

    /** The override is scoped: no draft recorded, no local answer — the backend block stands. */
    @Test
    fun `death post-mortem stays blocked with no first video`() {
        val postMortem = WorkflowActionDto(
            actionKey = "post_mortem_video",
            actionType = "action",
            requiresVideo = true,
            status = "pending",
            blocked = true,
            blockedReason = "previous_action",
        )

        assertTrue(workflowBlockedForOperator(postMortem, isDeath = true, predecessorsReady = false))
    }

    /** Only `previous_action` is stale pre-submit; every other reason is real. */
    @Test
    fun `a death row blocked for any other reason is still blocked`() {
        val gated = WorkflowActionDto(
            actionType = "action",
            requiresVideo = true,
            status = "pending",
            blocked = true,
            blockedReason = "signoff",
        )

        assertTrue(workflowBlockedForOperator(gated, isDeath = true, predecessorsReady = true))
    }

    @Test
    fun `ready unblocked video action can open camera`() {
        val action = WorkflowActionDto(
            actionType = "action",
            requiresVideo = true,
            status = "pending",
            blocked = false,
        )

        assertTrue(
            canRecordWorkflowVideo(
                action,
                blocked = workflowBlockedForOperator(action, isDeath = false, predecessorsReady = true),
                draftsSubmitting = false,
            ),
        )
    }

    /**
     * Field defect, kid G-005335 (born 13 Aug 22:58 IST, workflow 81cc525d): its five colostrum
     * feeds are due 14 Aug while `first_colostrum` is a `main`-section row due 13 Aug. The Colostrum
     * lens renders ONE business date, so the 14 Aug list does not contain `first_colostrum` at all —
     * and the camera gate used to AND in [workflowPredecessorsReady], which returns false the moment
     * that row is absent. Result: `GET .../workflows/{id}?lens=colostrum&date=2026-08-14` reported
     * `colostrum_day_2_1500` as `pending, blocked=false, requires_video=true`, and the row still
     * rendered with no camera. The same feed recorded fine from Birth, whose unlensed read returns
     * all 13 rows.
     *
     * The camera follows the BACKEND's `blocked` — which is computed against the kid's complete
     * action set on both lenses — so a truncated render can no longer suppress it.
     */
    @Test
    fun `colostrum feed opens camera on a day whose list omits first colostrum`() {
        // Exactly what the colostrum lens returns for 14 Aug: the day's feeds, no `first_colostrum`.
        val fourthFeed = WorkflowActionDto(
            actionId = "colostrum-1500",
            actionKey = "colostrum_day_2_1500",
            seq = 10,
            section = "colostrum_session",
            actionType = "action",
            requiresVideo = true,
            status = "pending",
            blocked = false,
        )
        val renderedDay = listOf(
            WorkflowActionDto(
                actionId = "colostrum-0700",
                actionKey = "colostrum_day_2_0700",
                seq = 8,
                section = "colostrum_session",
                actionType = "action",
                requiresVideo = true,
                status = "completed",
            ),
            WorkflowActionDto(
                actionId = "colostrum-1100",
                actionKey = "colostrum_day_2_1100",
                seq = 9,
                section = "colostrum_session",
                actionType = "action",
                requiresVideo = true,
                status = "completed",
            ),
            fourthFeed,
        )

        // The prerequisite genuinely is not on screen, so the local answer is (and stays) false.
        assertFalse(
            workflowPredecessorsReady(fourthFeed, renderedDay) { it.status == "completed" },
        )

        // It must no longer decide the camera: the backend said this feed is ready.
        assertTrue(
            canRecordWorkflowVideo(
                fourthFeed,
                blocked = workflowBlockedForOperator(
                    fourthFeed,
                    isDeath = false,
                    predecessorsReady = false,
                ),
                draftsSubmitting = false,
            ),
        )
    }

    /**
     * The other half of the same contract: a feed the backend DOES gate stays gated. Seq 11/12 of
     * G-005335 came back `blocked=previous_action` because the 15:00 feed is unfinished, and they
     * must keep their camera shut until it lands.
     */
    @Test
    fun `a later colostrum feed the backend blocks keeps its camera shut`() {
        val fifthFeed = WorkflowActionDto(
            actionId = "colostrum-1830",
            actionKey = "colostrum_day_2_1830",
            seq = 11,
            section = "colostrum_session",
            actionType = "action",
            requiresVideo = true,
            status = "pending",
            blocked = true,
            blockedReason = "previous_action",
        )

        assertFalse(
            canRecordWorkflowVideo(
                fifthFeed,
                blocked = workflowBlockedForOperator(
                    fifthFeed,
                    isDeath = false,
                    predecessorsReady = true,
                ),
                draftsSubmitting = false,
            ),
        )
    }

    @Test
    fun `tag action stops reopening RFID assignment after identifier is recorded`() {
        assertTrue(workflowTagNeedsPermanentIdentifier(answerValue = null))
        assertTrue(workflowTagNeedsPermanentIdentifier(answerValue = ""))
        assertFalse(workflowTagNeedsPermanentIdentifier(answerValue = "982000123456789"))
    }
}
