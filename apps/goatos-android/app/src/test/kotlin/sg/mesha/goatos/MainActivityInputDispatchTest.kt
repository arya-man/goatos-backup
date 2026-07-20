package sg.mesha.goatos

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class MainActivityInputDispatchTest {

    @Test
    fun `RFID-consumed terminator never reaches focused Compose control`() {
        val order = mutableListOf<String>()

        val consumed = dispatchRfidFirst(
            rfidConsumes = {
                order += "rfid"
                true
            },
            dispatchNormally = {
                order += "compose"
                false
            },
        )

        assertTrue(consumed)
        assertEquals(listOf("rfid"), order)
    }

    @Test
    fun `ordinary key falls through to normal Activity view dispatch`() {
        val order = mutableListOf<String>()

        val consumed = dispatchRfidFirst(
            rfidConsumes = {
                order += "rfid"
                false
            },
            dispatchNormally = {
                order += "compose"
                false
            },
        )

        assertFalse(consumed)
        assertEquals(listOf("rfid", "compose"), order)
    }
}
