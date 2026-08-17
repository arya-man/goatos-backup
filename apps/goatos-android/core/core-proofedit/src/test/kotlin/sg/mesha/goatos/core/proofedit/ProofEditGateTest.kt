package sg.mesha.goatos.core.proofedit

import org.junit.Test
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue

class ProofEditGateTest {

    private val flagOn = mapOf(ProofEditGate.FLAG_KEY to true)

    @Test
    fun `editor is offered for feed packing when the flag is on`() {
        assertTrue(ProofEditGate.isEditingOffered(flagOn, "feed_packing"))
    }

    @Test
    fun `absent flag fails closed`() {
        // A device that never reached bootstrap, or a backend that predates the flag, must get
        // today's unedited flow — not a new one.
        assertFalse(ProofEditGate.isEditingOffered(emptyMap(), "feed_packing"))
    }

    @Test
    fun `flag explicitly false fails closed`() {
        assertFalse(
            ProofEditGate.isEditingOffered(mapOf(ProofEditGate.FLAG_KEY to false), "feed_packing"),
        )
    }

    @Test
    fun `clinical proof surfaces are never editable even with the flag on`() {
        // Cutting a vaccination clip would let the injection itself be removed from the evidence
        // the verifier judges. This must stay false until a maintainer decision says otherwise.
        listOf(
            "vaccination",
            "vaccination_video",
            "weighing",
            "shifting",
            "feed_distribution",
            "water_distribution",
            "feed_transport",
            "birth",
            "death",
            "post_mortem",
        ).forEach { surface ->
            assertFalse("$surface must not be editable", ProofEditGate.isEditingOffered(flagOn, surface))
        }
    }

    @Test
    fun `unidentified surface fails closed`() {
        assertFalse(ProofEditGate.isEditingOffered(flagOn, null))
        assertFalse(ProofEditGate.isEditingOffered(flagOn, ""))
        assertFalse(ProofEditGate.isEditingOffered(flagOn, "   "))
    }

    @Test
    fun `surface match ignores case and surrounding space`() {
        assertTrue(ProofEditGate.isEditingOffered(flagOn, " Feed_Packing "))
    }

    @Test
    fun `allowlist is exactly feed packing today`() {
        // Guards against a silent widening: adding a surface here is a maintainer decision.
        assertEquals(setOf("feed_packing"), ProofEditGate.EDITABLE_SURFACES)
    }
}
