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

/**
 * A TREATMENT STEP'S VIDEO IS WATCHABLE, and keyed on something that does not rotate.
 *
 * Health was the one proof surface that showed a clip only as the words "Video recorded": the
 * operator could not play back what they had just filmed, could not see the step a colleague had
 * already covered, and could not share one on. A step's video IS the evidence the treatment
 * happened, so a record nobody can look at is a record nobody can check.
 *
 * The identity is what keeps the fix from costing money. The shared preview caches and keys its
 * player on `mediaIdentity`; a signed download URL rotates, so keying on one would re-fetch the
 * same video under a new identity on every refresh -- the paid-egress defect the proof-media rule
 * exists to prevent.
 */
class TreatmentStepProofPreviewTest {

    @Test
    fun `a clip filmed on this phone plays from the local file before it has uploaded`() {
        val proof = TreatmentStepProof(
            stepId = "s1",
            state = StepProofState.SENDING,
            uploadOutboxItemId = "upload-1",
            previewPath = "file:///data/proofs/step-1.mp4",
            previewIdentity = "upload-1",
        )
        assertTrue("an operator checks their own clip before submitting", proof.previewPath.isNotBlank())
        assertFalse("a local file is not a teammate's", proof.capturedByTeammate)
        // Not yet recorded: the register has not landed, and the preview must not imply it has.
        assertFalse(proof.recorded)
    }

    @Test
    fun `a teammate's clip plays from the server and is marked as theirs`() {
        val proof = TreatmentStepProof(
            stepId = "s2",
            state = StepProofState.RECORDED,
            previewPath = "https://api.example/app/proofs/proof-9/download",
            previewIdentity = "proof-9",
            capturedByTeammate = true,
        )
        assertTrue(proof.recorded)
        assertTrue("a step someone else filmed must still be viewable", proof.previewPath.isNotBlank())
        assertTrue(proof.capturedByTeammate)
    }

    @Test
    fun `the preview identity is never the download url`() {
        val url = "https://api.example/app/proofs/proof-9/download?sig=rotates"
        val proof = TreatmentStepProof(
            stepId = "s3",
            state = StepProofState.RECORDED,
            previewPath = url,
            previewIdentity = "proof-9",
        )
        assertEquals("proof-9", proof.previewIdentity)
        assertFalse(
            "keying on the signed URL re-fetches the same clip whenever the URL rotates",
            proof.previewIdentity.contains("http"),
        )
    }

    @Test
    fun `a step with nothing filmed offers no player`() {
        val proof = TreatmentStepProof(stepId = "s4")
        assertTrue("a blank path is what tells the screen there is nothing to show", proof.previewPath.isBlank())
    }
}

/**
 * THE PREVIEW SURVIVES EVERY LATER STATE CHANGE.
 *
 * Found on the phone, not in review: the clip was recorded, the row read "Video saving…", and
 * there was no player on it at all. The view model rebuilt `TreatmentStepProof` from scratch on
 * each outbox emission -- upload queued, register queued, register landed -- so the fields that
 * observer does not own, the local file the video plays from and its identity, were dropped the
 * instant the upload began.
 *
 * A treatment video that stops being watchable the moment it starts uploading is the whole
 * feature failing quietly, so the rule is COPY, never rebuild.
 */
class TreatmentStepProofPreviewSurvivesStateChangesTest {

    @Test
    fun `a state change keeps the local file and identity`() {
        val recorded = TreatmentStepProof(
            stepId = "s1",
            state = StepProofState.NONE,
            previewPath = "file:///data/proofs/step-1.mp4",
            previewIdentity = "upload-1",
        )
        var proofs = TreatmentStepProofs().with(recorded)

        // Upload queued, then register queued, then the register lands. The clip must stay
        // playable through all three.
        listOf(StepProofState.SENDING, StepProofState.SENDING, StepProofState.RECORDED)
            .forEach { next ->
                proofs = proofs.with(proofs.of("s1").copy(state = next))
                assertEquals(
                    "the local file must survive the $next transition",
                    "file:///data/proofs/step-1.mp4",
                    proofs.of("s1").previewPath,
                )
                assertEquals("upload-1", proofs.of("s1").previewIdentity)
            }
        assertTrue(proofs.of("s1").recorded)
    }

    @Test
    fun `a failure keeps the clip watchable so the operator can see what went out`() {
        val proofs = TreatmentStepProofs()
            .with(
                TreatmentStepProof(
                    stepId = "s1",
                    previewPath = "file:///data/proofs/step-1.mp4",
                    previewIdentity = "upload-1",
                ),
            )
        val failed = proofs.with(
            proofs.of("s1").copy(state = StepProofState.FAILED, message = "Could not reach the server."),
        )
        assertEquals("file:///data/proofs/step-1.mp4", failed.of("s1").previewPath)
        assertEquals(StepProofState.FAILED, failed.of("s1").state)
    }
}
