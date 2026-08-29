package sg.mesha.goatos.feature.weighing

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import sg.mesha.goatos.feature.scan.ProofUploadStatus

/**
 * A disabled Submit must always name its own reason. Seen live: a lump-sum proof sat at
 * "uploading" indefinitely with a Retry offered, Submit greyed out, and nothing on screen
 * telling the operator which of the two was the hold-up.
 *
 * telemetry:exempt pure UI-state unit test — asserts a derived `String?` on a data class and
 * renders no surface, so there is no user-facing event, funnel step, or error path to wire.
 * The surface it covers (WeighingScreen) carries its own telemetry.
 */
class WeighingSubmitBlockedReasonTest {

    private fun lumpSum(
        weight: String = "42.5",
        proofs: List<ProofUploadStatus> = listOf(ProofUploadStatus.SYNCED),
        actionInFlight: Boolean = false,
    ) = WeighingUiState(
        hasScope = true,
        category = "per_shed_partition",
        weightInput = weight,
        actionInFlight = actionInFlight,
        shedProofs = proofs.mapIndexed { i, status -> WeighingProofUiRow("p$i", "video ${i + 1}", status) },
    )

    @Test
    fun `a still-uploading video says so instead of leaving a dead button`() {
        val state = lumpSum(proofs = listOf(ProofUploadStatus.UPLOADING))

        assertEquals(false, state.canRecordShedPartition)
        assertEquals(R.string.weighing_blocked_video_uploading, state.submitBlockedReason)
    }

    @Test
    fun `a failed video points at Retry rather than staying silent`() {
        val state = lumpSum(proofs = listOf(ProofUploadStatus.FAILED))

        assertEquals(false, state.canRecordShedPartition)
        assertEquals(R.string.weighing_blocked_video_failed, state.submitBlockedReason)
    }

    @Test
    fun `no video at all asks for the video`() {
        assertEquals(R.string.weighing_blocked_need_video, lumpSum(proofs = emptyList()).submitBlockedReason)
    }

    @Test
    fun `missing weight names itself — and there is no count to ask for any more`() {
        // The head count stopped being an operator input on 2026-08-24: the backend
        // snapshots it from the herd register at submit, so the only typed field a
        // lump-sum submit can be blocked on is the total weight.
        assertEquals(R.string.weighing_blocked_need_weight, lumpSum(weight = "").submitBlockedReason)
    }

    @Test
    fun `nothing blocks a complete lump-sum submission`() {
        val state = lumpSum()

        assertEquals(true, state.canRecordShedPartition)
        assertNull("a pressable Submit must carry no reason line", state.submitBlockedReason)
    }

    @Test
    fun `with no shed open the button says to open one`() {
        assertEquals(
            R.string.weighing_blocked_no_shed_open,
            WeighingUiState(category = "per_shed_partition").submitBlockedReason,
        )
    }

    @Test
    fun `an in-flight save explains the wait`() {
        assertEquals(R.string.weighing_blocked_saving, lumpSum(actionInFlight = true).submitBlockedReason)
    }

    @Test
    fun `the individual flow explains an empty capture and an unfinished upload too`() {
        val empty = WeighingUiState(hasScope = true, category = "individual")
        assertEquals(R.string.weighing_blocked_nothing_captured, empty.submitBlockedReason)

        val uploading = WeighingUiState(
            hasScope = true,
            category = "individual",
            visibleRows = listOf(
                row(weightSaved = true, proof = ProofUploadStatus.SYNCED),
                row(weightSaved = true, proof = ProofUploadStatus.UPLOADING),
            ),
        )
        assertEquals(false, uploading.individualSubmitReady)
        assertEquals(R.string.weighing_blocked_video_uploading, uploading.submitBlockedReason)

        val ready = WeighingUiState(
            hasScope = true,
            category = "individual",
            visibleRows = listOf(row(weightSaved = true, proof = ProofUploadStatus.SYNCED)),
        )
        assertEquals(true, ready.individualSubmitReady)
        assertNull(ready.submitBlockedReason)
    }

    private fun row(weightSaved: Boolean, proof: ProofUploadStatus) = WeighingRosterUiRow(
        id = "r-$proof-$weightSaved",
        animalId = "a1",
        displayAnimalId = "A1",
        status = "captured",
        weightSaved = weightSaved,
        proofUploadStatus = proof,
    )
}
