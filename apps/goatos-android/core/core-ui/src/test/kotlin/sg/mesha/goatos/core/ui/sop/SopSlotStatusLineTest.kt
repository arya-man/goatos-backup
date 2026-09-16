package sg.mesha.goatos.core.ui.sop

import org.junit.Assert.assertEquals
import org.junit.Test

/** Realme E2E 2026-09-17: a captured slot read just "Proof ready" and lost which capture it was. */
class SopSlotStatusLineTest {
    @Test
    fun `a captured slot keeps its name beside its status`() {
        assertEquals("Carcass photo · Proof ready", sopSlotStatusLine("Carcass photo", "Proof ready"))
    }

    @Test
    fun `an empty slot whose status is its title is not doubled`() {
        assertEquals("Carcass photo", sopSlotStatusLine("Carcass photo", "Carcass photo"))
        assertEquals("Proof ready", sopSlotStatusLine("", "Proof ready"))
    }
}
