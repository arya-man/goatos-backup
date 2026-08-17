package sg.mesha.goatos.capture

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.proofedit.ProofEditGate

/**
 * The trim editor is an ADD-ON. These tests pin the two properties that keep it one:
 *
 *  1. a capture that does not name its surface can never be offered the editor, so every existing
 *     caller keeps today's flow without being touched;
 *  2. feed packing is the only surface that is offered it.
 *
 * If either breaks, an unrelated proof surface silently gains an editing step.
 */
class ProofCaptureEditingOptInTest {

    private val flagOn = mapOf(ProofEditGate.FLAG_KEY to true)

    @Test
    fun `capture context declares no surface by default`() {
        // This default is what makes the change additive: every pre-existing construction site
        // omits featureSurface, so it arrives null and the gate refuses.
        val context = ProofCaptureContext(title = "t", primaryTag = "982000123456789")
        assertNull(context.featureSurface)
        assertFalse(ProofEditGate.isEditingOffered(flagOn, context.featureSurface))
    }

    @Test
    fun `every prompt except feed packing is refused even with the flag on`() {
        // Prompt and surface are different things, but a surface derived from any of these prompts
        // must not become editable by accident.
        ProofCapturePrompt.entries
            .filter { it != ProofCapturePrompt.FEED_PACKING }
            .forEach { prompt ->
                val surface = prompt.name.lowercase()
                assertFalse(
                    "$surface must not be editable",
                    ProofEditGate.isEditingOffered(flagOn, surface),
                )
            }
    }

    @Test
    fun `feed packing surface key matches the gate allowlist exactly`() {
        // Guards against drift between the key the packing capture sends and the key the gate
        // checks — a typo on either side would silently disable the feature with no error.
        val packingSurface = ProofCapturePrompt.FEED_PACKING.name.lowercase()
        assertEquals("feed_packing", packingSurface)
        assertTrue(packingSurface in ProofEditGate.EDITABLE_SURFACES)
        assertTrue(ProofEditGate.isEditingOffered(flagOn, packingSurface))
    }

    @Test
    fun `flag off keeps feed packing on the existing flow`() {
        assertFalse(ProofEditGate.isEditingOffered(emptyMap(), "feed_packing"))
    }
}
