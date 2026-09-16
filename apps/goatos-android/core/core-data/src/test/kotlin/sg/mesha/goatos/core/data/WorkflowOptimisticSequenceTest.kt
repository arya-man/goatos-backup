package sg.mesha.goatos.core.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.network.dto.WorkflowActionDto
import sg.mesha.goatos.core.network.dto.WorkflowCardDto
import sg.mesha.goatos.core.network.dto.WorkflowDetailResponseDto

class WorkflowOptimisticSequenceTest {
    @Test
    fun `optimistic sequencing preserves backend temporal block`() {
        val detail = WorkflowDetailResponseDto(
            actionsDone = 5,
            actionsTotal = 6,
            actions = listOf(
                WorkflowActionDto(
                    actionId = "ors-2",
                    actionKey = "ors_water_2",
                    seq = 6,
                    actionType = "action",
                    status = "pending",
                    blocked = true,
                    blockedReason = "not_yet_due",
                ),
            ),
        )

        val optimistic = detail.withOptimisticOperatorSequence()

        assertTrue(optimistic.actions.single().blocked)
    }

    @Test
    fun `queued death video counts as done and unlocks post mortem`() {
        val detail = WorkflowDetailResponseDto(
            actionsDone = 0,
            actionsTotal = 2,
            actions = listOf(
                WorkflowActionDto(
                    actionId = "death",
                    actionKey = "death_video",
                    seq = 1,
                    actionType = "action",
                    status = "in_review",
                    blocked = false,
                ),
                WorkflowActionDto(
                    actionId = "postmortem",
                    actionKey = "post_mortem_video",
                    seq = 2,
                    actionType = "action",
                    status = "pending",
                    blocked = true,
                ),
            ),
        )

        val optimistic = detail.withOptimisticOperatorSequence()

        assertEquals(1, optimistic.actionsDone)
        assertFalse(optimistic.actions.single { it.actionKey == "post_mortem_video" }.blocked)
    }

    @Test
    fun `refresh preserves queued death video until its durable command finishes`() {
        val server = deathDetail(firstStatus = "pending", secondBlocked = true)
        val cached = deathDetail(firstStatus = "in_review", secondBlocked = false)

        val reconciled = server.withActiveWorkflowActionsPreserved(
            cached = cached,
            activeActionIds = setOf("death"),
        )

        assertEquals("in_review", reconciled.actions.single { it.actionId == "death" }.status)
        assertEquals(1, reconciled.actionsDone)
        assertFalse(reconciled.actions.single { it.actionId == "postmortem" }.blocked)
    }

    @Test
    fun `refresh accepts backend rollback when no death command remains active`() {
        val server = deathDetail(firstStatus = "rework", secondBlocked = true)
        val cached = deathDetail(firstStatus = "in_review", secondBlocked = false)

        val reconciled = server.withActiveWorkflowActionsPreserved(
            cached = cached,
            activeActionIds = emptySet(),
        )

        assertEquals("rework", reconciled.actions.single { it.actionId == "death" }.status)
        assertEquals(0, reconciled.actionsDone)
    }

    @Test
    fun `list refresh preserves two of two while death command is active`() {
        val staleServer = WorkflowCardDto(actionsDone = 0, actionsTotal = 2)
        val submittedCache = WorkflowCardDto(actionsDone = 2, actionsTotal = 2, nextAction = null)

        val reconciled = staleServer.withActiveCachedProgressPreserved(
            cached = submittedCache,
            hasActiveCommand = true,
        )

        assertEquals(2, reconciled.actionsDone)
        assertEquals(null, reconciled.nextAction)
    }

    @Test
    fun `list refresh accepts verifier rework after death command is terminal`() {
        val serverRework = WorkflowCardDto(actionsDone = 0, actionsTotal = 2)
        val submittedCache = WorkflowCardDto(actionsDone = 2, actionsTotal = 2, nextAction = null)

        val reconciled = serverRework.withActiveCachedProgressPreserved(
            cached = submittedCache,
            hasActiveCommand = false,
        )

        assertEquals(0, reconciled.actionsDone)
    }

    @Test
    fun `birth optimistic progress includes scheduled colostrum`() {
        val detail = WorkflowDetailResponseDto(
            module = "birth",
            actionsDone = 0,
            actionsTotal = 3,
            actions = listOf(
                WorkflowActionDto(
                    actionId = "first-colostrum",
                    actionKey = "first_colostrum",
                    seq = 1,
                    section = "main",
                    actionType = "action",
                    status = "completed",
                ),
                WorkflowActionDto(
                    actionId = "scheduled-colostrum",
                    actionKey = "colostrum_day_1_1500",
                    seq = 2,
                    section = "colostrum_session",
                    actionType = "action",
                    status = "in_review",
                ),
                WorkflowActionDto(
                    actionId = "tag",
                    actionKey = "tag_the_kid",
                    seq = 3,
                    section = "main",
                    actionType = "action",
                    status = "pending",
                ),
            ),
        )

        val optimistic = detail.withOptimisticOperatorSequence()
        val card = WorkflowCardDto().withOptimisticOperatorProgress(optimistic, nowMs = 0)

        assertEquals(2, optimistic.actionsDone)
        assertEquals(2, card.actionsDone)
        assertEquals(3, card.actionsTotal)
    }

    @Test
    fun `birth step sent back for rework never holds the step after it`() {
        // Birth clips are reviewed one recorded step at a time (2026-09-16): a rejected clip is
        // re-shot on its own while the operator carries on with the next step. The backend says
        // so (blocked = false); the phone's optimistic pass must agree instead of re-blocking it.
        val detail = WorkflowDetailResponseDto(
            module = "birth",
            templateKey = "birth_kid",
            actionsDone = 1,
            actionsTotal = 3,
            actions = listOf(
                WorkflowActionDto(actionId = "clean", actionKey = "kid_clean", seq = 1, actionType = "question", status = "rework", blocked = false),
                WorkflowActionDto(actionId = "iodine", actionKey = "iodine_dipping", seq = 2, actionType = "action", status = "in_review", blocked = false),
                WorkflowActionDto(actionId = "teeth", actionKey = "front_teeth_check", seq = 3, actionType = "question", status = "pending", blocked = false),
            ),
        )

        val optimistic = detail.withOptimisticOperatorSequence()

        assertFalse(optimistic.actions.single { it.actionId == "teeth" }.blocked)
        assertEquals(1, optimistic.actionsDone)
    }

    @Test
    fun `death video sent back for rework still holds the post mortem`() {
        val optimistic = deathDetail(firstStatus = "rework", secondBlocked = true).withOptimisticOperatorSequence()

        assertTrue(optimistic.actions.single { it.actionKey == "post_mortem_video" }.blocked)
    }

    private fun deathDetail(firstStatus: String, secondBlocked: Boolean) =
        WorkflowDetailResponseDto(
            module = "death",
            actionsDone = 0,
            actionsTotal = 2,
            actions = listOf(
                WorkflowActionDto(
                    actionId = "death",
                    actionKey = "death_video",
                    seq = 1,
                    actionType = "action",
                    status = firstStatus,
                    blocked = false,
                ),
                WorkflowActionDto(
                    actionId = "postmortem",
                    actionKey = "post_mortem_video",
                    seq = 2,
                    actionType = "action",
                    status = "pending",
                    blocked = secondBlocked,
                ),
            ),
        )
}
