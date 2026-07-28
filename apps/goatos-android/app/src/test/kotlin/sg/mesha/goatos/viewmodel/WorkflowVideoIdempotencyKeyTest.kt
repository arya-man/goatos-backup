package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

class WorkflowVideoIdempotencyKeyTest {
    @Test
    fun `exact retry is stable but a re-shoot gets a new proof round`() {
        val first = workflowProofUploadKey("death-action", 1000L)

        assertEquals(first, workflowProofUploadKey("death-action", 1000L))
        assertNotEquals(first, workflowProofUploadKey("death-action", 2000L))
        assertNotEquals(
            workflowVideoCompletionKey("death-action", "proof-item-1"),
            workflowVideoCompletionKey("death-action", "proof-item-2"),
        )
    }
}
