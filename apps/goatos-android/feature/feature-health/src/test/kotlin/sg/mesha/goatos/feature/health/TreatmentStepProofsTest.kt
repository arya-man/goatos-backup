package sg.mesha.goatos.feature.health

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The state the proof business-ack contract is about.
 *
 * `docs/decisions/proof-business-ack-contract.md` requires every proof-backed workflow to prove
 * this exact case: **proof upload succeeded, business write missing or failed → pending or
 * failed, never final green.** PC Care shipped the opposite in Sept 2026 -- `proof_artifacts`
 * held finished videos while the feature row's ref was empty and the phone read "Video sent".
 */
class TreatmentStepProofsTest {

    private val steps = listOf("s1", "s2", "s3")

    @Test
    fun `an uploaded video whose register has not landed is not recorded`() {
        val proofs = TreatmentStepProofs().with(
            TreatmentStepProof(
                stepId = "s1",
                state = StepProofState.SENDING,
                uploadOutboxItemId = "upload-1",
                registerOutboxItemId = "register-1",
            ),
        )

        assertFalse("an upload alone must never read as recorded", proofs.of("s1").recorded)
        assertTrue("the step still owes its video", "s1" in proofs.missing(steps))
        assertTrue("a queued register may still be submitted offline", proofs.of("s1").readyForSubmit)
    }

    @Test
    fun `an uploaded video whose register FAILED is owed, not done`() {
        // The blob is in storage. The server does not hold it against the step, so the submit
        // would be refused -- and telling the operator it is done is how a proof goes missing.
        val proofs = TreatmentStepProofs().with(
            TreatmentStepProof(
                stepId = "s1",
                state = StepProofState.FAILED,
                uploadOutboxItemId = "upload-1",
                registerOutboxItemId = "register-1",
                message = "Could not attach this video. Try again.",
            ),
        )

        assertFalse(proofs.of("s1").recorded)
        assertFalse(proofs.of("s1").readyForSubmit)
        assertEquals(listOf("s1", "s2", "s3"), proofs.missing(steps))
        assertEquals(listOf("s1", "s2", "s3"), proofs.missingForSubmit(steps))
        assertFalse(proofs.complete(steps))
        assertFalse(proofs.readyForSubmit(steps))
    }

    @Test
    fun `a step is recorded only once its register lands`() {
        val proofs = TreatmentStepProofs().with(
            TreatmentStepProof(stepId = "s1", state = StepProofState.RECORDED, registerOutboxItemId = "register-1"),
        )
        assertTrue(proofs.of("s1").recorded)
        assertEquals(listOf("s2", "s3"), proofs.missing(steps))
    }

    @Test
    fun `the card submits only when every step is recorded`() {
        var proofs = TreatmentStepProofs()
        assertFalse("an empty card must not submit", proofs.complete(steps))

        steps.dropLast(1).forEach {
            proofs = proofs.with(TreatmentStepProof(stepId = it, state = StepProofState.RECORDED))
        }
        assertFalse("two of three recorded is not a complete card", proofs.complete(steps))

        proofs = proofs.with(TreatmentStepProof(stepId = "s3", state = StepProofState.RECORDED))
        assertTrue(proofs.complete(steps))
    }

    @Test
    fun `the card can queue submit when every step register is queued or recorded`() {
        val proofs = TreatmentStepProofs()
            .with(TreatmentStepProof(stepId = "s1", state = StepProofState.RECORDED))
            .with(TreatmentStepProof(stepId = "s2", state = StepProofState.SENDING))
            .with(TreatmentStepProof(stepId = "s3", state = StepProofState.SENDING))

        assertFalse("queued registers are not final recorded evidence", proofs.complete(steps))
        assertTrue("same-lane queued registers may drain before completion", proofs.readyForSubmit(steps))
        assertEquals(emptyList<String>(), proofs.missingForSubmit(steps))
    }

    // A card with NO steps must not submit either. Otherwise a session whose steps failed to load
    // would sail through with no evidence at all -- vacuously "complete".
    @Test
    fun `a card with no steps is not complete`() {
        assertFalse(TreatmentStepProofs().complete(emptyList()))
        assertFalse(TreatmentStepProofs().readyForSubmit(emptyList()))
    }

    @Test
    fun `missing steps keep the order the operator works them`() {
        val proofs = TreatmentStepProofs().with(
            TreatmentStepProof(stepId = "s2", state = StepProofState.RECORDED),
        )
        assertEquals(listOf("s1", "s3"), proofs.missing(steps))
    }
}
