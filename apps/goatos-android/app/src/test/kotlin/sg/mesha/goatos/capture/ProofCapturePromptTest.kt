package sg.mesha.goatos.capture

import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.R

class ProofCapturePromptTest {
    @Test
    fun `workflow task title replaces generic birth recorder title`() {
        assertEquals(
            "Mother's Medicine",
            recorderHeaderTitle(
                taskTitle = "Mother's Medicine",
                fallbackTitle = "Record birth video",
            ),
        )
    }

    @Test
    fun `death recorders never reuse vaccination header or instruction`() {
        val vaccination = recorderCopyResources(ProofCapturePrompt.VACCINATION)
        val death = recorderCopyResources(ProofCapturePrompt.DEATH)
        val postMortem = recorderCopyResources(ProofCapturePrompt.POST_MORTEM)

        assertEquals(R.string.proof_camera_title, vaccination.title)
        assertEquals(R.string.proof_camera_instruction, vaccination.instruction)
        assertNotEquals(vaccination.title, death.title)
        assertNotEquals(vaccination.instruction, death.instruction)
        assertNotEquals(vaccination.title, postMortem.title)
        assertNotEquals(vaccination.instruction, postMortem.instruction)
        assertNotEquals(death.title, postMortem.title)
    }
}
