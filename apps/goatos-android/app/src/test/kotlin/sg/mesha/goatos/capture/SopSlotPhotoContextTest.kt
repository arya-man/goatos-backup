package sg.mesha.goatos.capture

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test

/** Realme E2E 2026-09-17: a slot with no hint showed "Carcass photo / Carcass photo" on the camera. */
class SopSlotPhotoContextTest {
    @Test
    fun `a slot without a hint shows its title and no instruction line`() {
        val context = sopSlotPhotoContext(title = "Carcass photo", hint = " ")
        assertEquals("Carcass photo", context.title)
        assertEquals("", context.instruction)
        assertFalse("no generic line stands in for the missing hint", context.defaultInstruction)
    }

    @Test
    fun `a slot with a hint shows the hint`() {
        assertEquals("Whole body, side on", sopSlotPhotoContext("Carcass photo", "Whole body, side on").instruction)
    }
}
