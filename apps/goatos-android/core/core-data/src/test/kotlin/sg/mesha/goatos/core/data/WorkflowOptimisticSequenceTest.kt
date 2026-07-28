package sg.mesha.goatos.core.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test
import sg.mesha.goatos.core.network.dto.WorkflowActionDto
import sg.mesha.goatos.core.network.dto.WorkflowCardDto
import sg.mesha.goatos.core.network.dto.WorkflowDetailResponseDto

class WorkflowOptimisticSequenceTest {
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
