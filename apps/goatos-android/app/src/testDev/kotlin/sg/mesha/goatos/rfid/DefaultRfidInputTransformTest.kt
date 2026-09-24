package sg.mesha.goatos.rfid

import org.junit.Assert.assertEquals
import org.junit.Test

class DevDefaultRfidInputTransformTest {
    private val transform = DefaultRfidInputTransform()

    @Test
    fun `dev fixture prefixes only sample cards not real animal rfids`() {
        val gandhi2ShedId = "9c000000-0000-4000-8000-000000000302"

        assertEquals(
            "G2-TEMP-CPT-CASTRO1-001",
            transform.vaccination("TEMP-CPT-CASTRO1-001", gandhi2ShedId),
        )
        assertEquals(
            "901007000504418",
            transform.vaccination("901007000504418", gandhi2ShedId),
        )
    }
}
