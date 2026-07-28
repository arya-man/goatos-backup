package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.network.dto.WorkflowCardDto
import sg.mesha.goatos.core.network.dto.WorkflowNextActionDto
import sg.mesha.goatos.feature.counts.WorkflowCardBucket
import sg.mesha.goatos.feature.counts.WorkflowCardUi
import sg.mesha.goatos.feature.counts.WorkflowDeathSubmissionLabel
import sg.mesha.goatos.feature.counts.WorkflowDetailUiState

class WorkflowOperatorProgressTest {
    @Test
    fun `in review video is finished for operator progress`() {
        assertTrue(operatorFinishedWorkflowStatus("in_review"))
        assertTrue(operatorFinishedWorkflowStatus("completed"))
        assertFalse(operatorFinishedWorkflowStatus("pending"))
        assertFalse(operatorFinishedWorkflowStatus("rework"))
    }

    @Test
    fun `open two of two card is submitted and cannot reopen operator actions`() {
        assertTrue(
            WorkflowCardDto(
                state = "open",
                actionsDone = 2,
                actionsTotal = 2,
                nextAction = null,
            ).isOperatorSubmitted(),
        )
    }

    @Test
    fun `verifier rework reopens the first operator action`() {
        assertFalse(
            WorkflowCardDto(
                state = "open",
                actionsDone = 0,
                actionsTotal = 2,
                nextAction = WorkflowNextActionDto(key = "death_video"),
            ).isOperatorSubmitted(),
        )
    }

    @Test
    fun `completed history is not treated as pending submission`() {
        assertFalse(
            WorkflowCardDto(
                state = "completed",
                actionsDone = 2,
                actionsTotal = 2,
                nextAction = null,
            ).isOperatorSubmitted(),
        )
    }

    @Test
    fun `completed backend state remains in review until verifier gate closes`() {
        assertEquals(
            WorkflowCardBucket.IN_REVIEW,
            workflowCardBucket(
                state = "completed",
                awaitingVerification = true,
                operatorSubmitted = false,
                overdue = false,
            ),
        )
        assertEquals(
            WorkflowCardBucket.COMPLETED,
            workflowCardBucket(
                state = "completed",
                awaitingVerification = false,
                operatorSubmitted = false,
                overdue = false,
            ),
        )
    }

    @Test
    fun `submitted death card remains openable for video review`() {
        val card = WorkflowCardUi(
            workflowId = "death-workflow",
            displayId = "GOAT-1",
            roleLabel = "",
            metaLine = "",
            actionsDone = 2,
            actionsTotal = 2,
            nextKindLabel = "Done",
            nextTitle = "Submitted",
            dueLabel = "",
            overdue = false,
            bucket = WorkflowCardBucket.IN_REVIEW,
            operatorSubmitted = true,
        )

        assertTrue(card.canOpenDetail)
    }

    @Test
    fun `death footer becomes disabled submitted acknowledgement at two of two`() {
        val state = WorkflowDetailUiState(isDeath = true, actionsDone = 2, actionsTotal = 2)

        assertTrue(state.showDeathSubmissionButton)
        assertEquals(WorkflowDeathSubmissionLabel.SUBMITTED, state.deathSubmissionLabel)
        assertFalse(state.deathSubmissionEnabled)
    }

    @Test
    fun `death footer enables submit only after both editable drafts exist`() {
        val state = WorkflowDetailUiState(
            isDeath = true,
            actionsDone = 0,
            actionsTotal = 2,
            deathDraftCount = 2,
        )

        assertTrue(state.showDeathSubmissionButton)
        assertEquals(WorkflowDeathSubmissionLabel.SUBMIT, state.deathSubmissionLabel)
        assertTrue(state.deathSubmissionEnabled)
    }

    @Test
    fun `queued videos are uploading and are not falsely shown as submitted`() {
        val state = WorkflowDetailUiState(
            isDeath = true,
            actionsDone = 0,
            actionsTotal = 2,
            deathDraftCount = 2,
            deathDraftsSubmitting = true,
        )

        assertEquals(WorkflowDeathSubmissionLabel.UPLOADING, state.deathSubmissionLabel)
        assertFalse(state.deathSubmissionEnabled)
    }

    @Test
    fun `backend upload rejection remains visible and is not falsely shown as submitted`() {
        val state = WorkflowDetailUiState(
            isDeath = true,
            actionsDone = 0,
            actionsTotal = 2,
            deathDraftCount = 2,
            deathDraftsSubmitting = true,
            deathUploadFailed = true,
        )

        assertEquals(WorkflowDeathSubmissionLabel.UPLOAD_FAILED, state.deathSubmissionLabel)
        assertFalse(state.deathSubmissionEnabled)
    }
}
