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
            R.string.weighing_blocked_video_uploading,
            lumpSumState(ProofUploadStatus.UPLOADING).submitBlockedReason,
        )
        assertEquals(
            "a permanently rejected upload must tell the operator to record it again",
            R.string.weighing_blocked_video_failed,
            lumpSumState(ProofUploadStatus.FAILED).submitBlockedReason,
        )
        assertEquals(
            "no video at all is its own reason",
            R.string.weighing_blocked_need_video,
            lumpSumState().submitBlockedReason,
        )
    }

    @Test
    fun `a failed upload alongside an in-flight one still reports the wait`() {
        // Lump-sum needs exactly ONE durable video, so an upload still in flight can unblock
        // Submit on its own with no action from the operator. Saying "re-record" while that is
        // still possible sends someone back into the shed for work that may never be needed.
        // Failure is reported once nothing is in flight — see the all-FAILED case below.
        assertEquals(
            R.string.weighing_blocked_video_uploading,
            lumpSumState(ProofUploadStatus.UPLOADING, ProofUploadStatus.FAILED).submitBlockedReason,
        )
    }

    @Test
    fun `failure is reported once nothing is still uploading`() {
        assertEquals(
            R.string.weighing_blocked_video_failed,
            lumpSumState(ProofUploadStatus.FAILED, ProofUploadStatus.FAILED).submitBlockedReason,
        )
    }

    @Test
    fun `one saved video unblocks submit and clears the reason`() {
        val state = lumpSumState(ProofUploadStatus.UPLOADING, ProofUploadStatus.SYNCED)
        assertTrue(
            "one durable video is enough: free-flow weighing has no required video count",
            state.canRecordShedPartition,
        )
        assertNull("an enabled Submit must not carry a blocked reason", state.submitBlockedReason)
    }

    @Test
    fun `the most-blocking reason wins while the numbers are still missing`() {
        // A missing weight is what the operator must fix FIRST, so it outranks the proof that is
        // still uploading behind it. The rendered reason always names ONE next action.
        assertEquals(
            R.string.weighing_blocked_need_weight,
            lumpSumState(ProofUploadStatus.UPLOADING).copy(weightInput = "").submitBlockedReason,
        )
        assertEquals(
            R.string.weighing_blocked_need_count,
            lumpSumState(ProofUploadStatus.UPLOADING).copy(animalCountInput = "").submitBlockedReason,
        )
        // ... and once both are valid, proof is the only blocker left.
        assertEquals(
            R.string.weighing_blocked_video_uploading,
            lumpSumState(ProofUploadStatus.UPLOADING).submitBlockedReason,
        )
    }

    @Test
    fun `individual weighing reports its own blocker, not the lump-sum one`() {
        // Same rendered property serves both modes; on the individual path an empty row list is
        // the blocker, never the lump-sum group-video copy.
        assertEquals(
            R.string.weighing_blocked_nothing_captured,
            lumpSumState(ProofUploadStatus.UPLOADING).copy(category = "individual_animal").submitBlockedReason,
        )
    }
}
