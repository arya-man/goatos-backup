package sg.mesha.goatos.capture

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ProofTorchTest {
    @Test
    fun proofTorchModeCyclesAutoOnOff() {
        assertEquals(ProofTorchMode.ON, ProofTorchMode.AUTO.next())
        assertEquals(ProofTorchMode.OFF, ProofTorchMode.ON.next())
        assertEquals(ProofTorchMode.AUTO, ProofTorchMode.OFF.next())
    }

    @Test
    fun proofTorchEnabledRespectsModeAndHardware() {
        assertTrue(proofTorchEnabled(ProofTorchMode.AUTO, lowLight = true, hasFlashUnit = true))
        assertFalse(proofTorchEnabled(ProofTorchMode.AUTO, lowLight = false, hasFlashUnit = true))
        assertTrue(proofTorchEnabled(ProofTorchMode.ON, lowLight = false, hasFlashUnit = true))
        assertFalse(proofTorchEnabled(ProofTorchMode.OFF, lowLight = true, hasFlashUnit = true))
        assertFalse(proofTorchEnabled(ProofTorchMode.ON, lowLight = true, hasFlashUnit = false))
    }

    @Test
    fun lowLightAverageLumaUsesStrictThreshold() {
        assertTrue(isLowLightAverageLuma(totalLuma = 47, samples = 1))
        assertFalse(isLowLightAverageLuma(totalLuma = 48, samples = 1))
        assertFalse(isLowLightAverageLuma(totalLuma = 0, samples = 0))
    }
}
