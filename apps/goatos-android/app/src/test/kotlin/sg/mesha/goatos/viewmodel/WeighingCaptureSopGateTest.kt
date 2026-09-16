package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.feature.scan.ProofUploadStatus
import sg.mesha.goatos.feature.weighing.R
import sg.mesha.goatos.feature.weighing.WeighingCaptureSopUi
import sg.mesha.goatos.feature.weighing.WeighingProofUiRow
import sg.mesha.goatos.feature.weighing.WeighingRosterUiRow
import sg.mesha.goatos.feature.weighing.WeighingUiState

/**
 * THE WEIGH CAPTURES ARE AUTHORED (2026-09-16): Submit waits for the pinned SOP's captures and
 * answers in BOTH sections, and says so -- a synced primary video alone is no longer enough when
 * the SOP asks for more.
 */
class WeighingCaptureSopGateTest {
    private val animalRow = WeighingRosterUiRow(
        id = "r1", animalId = "TAG-1", displayAnimalId = "TAG-1", status = "Scanned",
        weightInput = "21", weightSaved = true, proofUploadStatus = ProofUploadStatus.SYNCED,
    )

    @Test
    fun `an animal still owing an authored capture blocks submit with its own reason`() {
        val owing = WeighingUiState(hasScope = true, category = "individual_animal", visibleRows = listOf(animalRow.copy(capturesMissing = listOf("Scale display"))))
        assertFalse(owing.individualSubmitReady)
        assertEquals(R.string.weighing_blocked_need_captures, owing.submitBlockedReason)
        val done = owing.copy(visibleRows = listOf(animalRow))
        assertTrue(done.individualSubmitReady)
        assertNull(done.submitBlockedReason)
    }

    @Test
    fun `a pen still owing an authored capture blocks submit even with a synced group video`() {
        val pen = WeighingUiState(
            hasScope = true, category = "per_shed_partition", weightInput = "120",
            shedProofs = listOf(WeighingProofUiRow(id = "v1", label = "synced", status = ProofUploadStatus.SYNCED)),
            captureSop = WeighingCaptureSopUi(penCapturesMissing = listOf("Gate photo")),
        )
        assertFalse(pen.canRecordShedPartition)
        assertEquals(R.string.weighing_blocked_need_captures, pen.submitBlockedReason)
        val done = pen.copy(captureSop = WeighingCaptureSopUi())
        assertTrue(done.canRecordShedPartition)
        assertNull(done.submitBlockedReason)
    }

    @Test
    fun `the pen's first-slot ceiling comes from the pinned SOP`() {
        assertEquals(5, WeighingCaptureSopUi().primaryPenSlotMax)
        assertEquals(3, WeighingCaptureSopUi(primaryPenSlotMax = 3).primaryPenSlotMax)
    }
}
