package sg.mesha.goatos.core.network

import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.network.dto.CountsBreakdownRowDto
import sg.mesha.goatos.core.network.dto.toFatteningDisplayStage

class FatteningStageDisplayTest {
    @Test
    fun `fattening stage codes render as names for mobile UI`() {
        assertEquals("Fattening", "F2".toFatteningDisplayStage())
        assertEquals("Fattening male", "F2-Male".toFatteningDisplayStage())
        assertEquals("Fattening female", "F2-Female".toFatteningDisplayStage())
        assertEquals("K2", "K2".toFatteningDisplayStage())
    }

    @Test
    fun `counts breakdown row exposes display management stage without changing raw value`() {
        val row = CountsBreakdownRowDto(managementStage = "F2-Male")

        assertEquals("F2-Male", row.managementStage)
        assertEquals("Fattening male", row.displayManagementStage)
    }
}
