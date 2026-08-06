package sg.mesha.goatos.feature.weighing

import sg.mesha.goatos.feature.scan.ProofUploadStatus
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

/**
 * A captured-row label must never claim a video that does not exist.
 *
 * The row label used to fall through to "Video captured" for ANY status not explicitly matched.
 * FAILED, UPLOADING and SYNCED were all matched above it, so the only status that ever reached
 * that branch was MISSING -- the one case with no video at all. An operator saw "Video captured"
 * on the card while Submit stayed greyed out demanding "Record the video of this weighing before
 * you submit": two labels on one screen disagreeing, with the reassuring one wrong.
 */
class WeighingProofRowLabelTest {

    @Test
    fun `missing proof never reads as captured`() {
        assertEquals(R.string.weighing_proof_required, proofRowLabelRes(ProofUploadStatus.MISSING))
        assertNotEquals(
            "a row with no video must not claim one",
            R.string.weighing_proof_captured,
            proofRowLabelRes(ProofUploadStatus.MISSING),
        )
    }

    @Test
    fun `each upload status states its own truth`() {
        assertEquals(R.string.weighing_proof_upload_failed, proofRowLabelRes(ProofUploadStatus.FAILED))
        assertEquals(R.string.weighing_proof_syncing, proofRowLabelRes(ProofUploadStatus.UPLOADING))
        assertEquals(R.string.weighing_proof_synced, proofRowLabelRes(ProofUploadStatus.SYNCED))
    }

    @Test
    fun `no two statuses share a label`() {
        val labels = ProofUploadStatus.entries.map { proofRowLabelRes(it) }
        assertEquals("proof status labels must be distinct", labels.size, labels.toSet().size)
    }
}
