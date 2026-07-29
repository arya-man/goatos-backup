package sg.mesha.goatos.capture

import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertEquals
import org.junit.Test

class ProofCapturePromptTest {
    @Test
    fun `workflow task title is carried by recorder context`() {
        val context = ProofCaptureContext(
            title = "Mother's Medicine",
            primaryTag = "workflow",
            workLabel = "Record birth video",
        )

        assertEquals(
            "Mother's Medicine",
            context.title,
        )
        assertEquals("Record birth video", context.workLabel)
    }

    @Test
    fun `workflow proof prompts remain distinct`() {
        assertNotEquals(ProofCapturePrompt.VACCINATION, ProofCapturePrompt.DEATH)
        assertNotEquals(ProofCapturePrompt.VACCINATION, ProofCapturePrompt.POST_MORTEM)
        assertNotEquals(ProofCapturePrompt.DEATH, ProofCapturePrompt.POST_MORTEM)
    }
}
