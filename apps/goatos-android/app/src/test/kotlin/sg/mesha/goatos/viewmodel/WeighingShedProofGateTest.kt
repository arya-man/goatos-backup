package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.feature.weighing.R
import sg.mesha.goatos.feature.scan.ProofUploadStatus
import sg.mesha.goatos.feature.weighing.WeighingProofUiRow
import sg.mesha.goatos.feature.weighing.WeighingUiState

/**
 * Pins the lump-sum Submit gate and the DISABLED-WITH-REASON that must accompany it.
 *
 * Live blocker, phone-QA 2026-08-03: Mandela 2, a valid total weight of 120 over 10 animals, one
 * group video stuck at "uploading" because every `POST /app/proofs/uploads` 403'd for a
 * growth_director. Submit was greyed out with NO stated cause. The root cause is fixed in
 * `backend/internal/permissions/routes.go` (see weighing_proof_route_test.go); this pins the
 * screen half — the gate is correct to hold, but it must SAY why, and it must never claim a
 * video was uploaded when it is still in flight.
 */
class WeighingShedProofGateTest {

    private fun lumpSumState(vararg proofs: ProofUploadStatus): WeighingUiState = WeighingUiState(
        hasScope = true,
        category = "per_shed_partition",
        weightInput = "120",
        animalCountInput = "10",
        shedProofs = proofs.mapIndexed { index, status ->
            WeighingProofUiRow(id = "proof-$index", label = "video", status = status)
        },
    )

    @Test
    fun `submit stays blocked while the only group video is still uploading`() {
        val state = lumpSumState(ProofUploadStatus.UPLOADING)
        assertFalse(
            "an in-flight video is not evidence: Submit must stay disabled",
            state.canRecordShedPartition,
        )
    }

    @Test
    fun `a blocked submit states the reason in farm language`() {
        assertEquals(
            "a video still uploading must say so, not leave the operator staring at a grey button",
            R.string.weighing_submit_blocked_uploading,
            lumpSumState(ProofUploadStatus.UPLOADING).shedProofBlockReasonRes,
        )
        assertEquals(
            "a permanently rejected upload must tell the operator to record it again",
            R.string.weighing_submit_blocked_failed,
            lumpSumState(ProofUploadStatus.FAILED).shedProofBlockReasonRes,
        )
        assertEquals(
            "no video at all is its own reason",
            R.string.weighing_submit_blocked_no_video,
            lumpSumState().shedProofBlockReasonRes,
        )
    }

    @Test
    fun `a failed upload alongside an in-flight one reports the failure, not the wait`() {
        // The operator can act on a failure (re-record) but can only wait on an upload, so the
        // actionable reason wins when both are present.
        assertEquals(
            R.string.weighing_submit_blocked_failed,
            lumpSumState(ProofUploadStatus.UPLOADING, ProofUploadStatus.FAILED).shedProofBlockReasonRes,
        )
    }

    @Test
    fun `one saved video unblocks submit and clears the reason`() {
        val state = lumpSumState(ProofUploadStatus.UPLOADING, ProofUploadStatus.SYNCED)
        assertTrue(
            "one durable video is enough: free-flow weighing has no required video count",
            state.canRecordShedPartition,
        )
        assertNull("an enabled Submit must not carry a blocked reason", state.shedProofBlockReasonRes)
    }

    @Test
    fun `no reason is shown while the numbers are still missing`() {
        // The empty weight/count fields speak for themselves; naming proof there would be noise.
        assertNull(
            lumpSumState(ProofUploadStatus.UPLOADING).copy(weightInput = "").shedProofBlockReasonRes,
        )
        assertNull(
            lumpSumState(ProofUploadStatus.UPLOADING).copy(animalCountInput = "").shedProofBlockReasonRes,
        )
        // ... but once both are valid, proof is the only blocker left and must be named.
        assertNotNull(lumpSumState(ProofUploadStatus.UPLOADING).shedProofBlockReasonRes)
    }

    @Test
    fun `individual weighing never shows the lump-sum proof reason`() {
        assertNull(lumpSumState(ProofUploadStatus.UPLOADING).copy(category = "individual_animal").shedProofBlockReasonRes)
    }
}
