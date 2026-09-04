package sg.mesha.goatos.capture

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Pins the pre-record briefing contract (maintainer request 2026-09-04): a weighing clip must
 * open on the scale reading 0 kg, so the shared recorder HOLDS at the live preview until the
 * operator acknowledges the briefing, and only that acknowledgement starts the clip.
 *
 * Two facts, each one a future edit is likely to break:
 *
 * 1. The auto-start rule is the pre-existing rule PLUS the acknowledgement. Every workflow without
 *    a briefing passes `briefingAcknowledged = true` from the start, so its behaviour is exactly
 *    what it was before the gate existed -- the test with every other input satisfied and only
 *    the acknowledgement missing is the mutation test: delete `&& briefingAcknowledged` from
 *    `recorderMayAutoStart` and it goes red.
 * 2. A briefing is OPT-IN per capture. The default context carries none, so adding this gate
 *    could not silently put a "show the scale" prompt in front of a birth or feed video.
 */
class PreRecordBriefingTest {

    @Test
    fun `a capture with a briefing does not auto-start until the operator acknowledges it`() {
        assertFalse(
            "preview streaming, nothing recording, nothing validating, nothing delivered -- the ONLY " +
                "thing holding the clip is the unacknowledged briefing, and it must hold.",
            recorderMayAutoStart(
                previewStreaming = true,
                isRecording = false,
                validationPending = false,
                resultDelivered = false,
                briefingAcknowledged = false,
            ),
        )
        assertTrue(
            "the same recorder state with the briefing acknowledged starts the clip.",
            recorderMayAutoStart(
                previewStreaming = true,
                isRecording = false,
                validationPending = false,
                resultDelivered = false,
                briefingAcknowledged = true,
            ),
        )
    }

    @Test
    fun `acknowledging the briefing never overrides the camera gates`() {
        // The acknowledgement is one more condition, not a bypass: a clip still cannot start before
        // the preview streams, while one is recording, while a file is validating, or after a
        // result was delivered.
        assertFalse(recorderMayAutoStart(previewStreaming = false, isRecording = false, validationPending = false, resultDelivered = false, briefingAcknowledged = true))
        assertFalse(recorderMayAutoStart(previewStreaming = true, isRecording = true, validationPending = false, resultDelivered = false, briefingAcknowledged = true))
        assertFalse(recorderMayAutoStart(previewStreaming = true, isRecording = false, validationPending = true, resultDelivered = false, briefingAcknowledged = true))
        assertFalse(recorderMayAutoStart(previewStreaming = true, isRecording = false, validationPending = false, resultDelivered = true, briefingAcknowledged = true))
    }

    @Test
    fun `a capture context carries no briefing unless the call site asks for one`() {
        val plain = ProofCaptureContext(title = "Record birth video", primaryTag = "workflow")
        assertNull("every non-weighing capture must keep opening straight into recording", plain.preRecordBriefing)

        val weighing = plain.copy(preRecordBriefing = ProofPreRecordBriefing.WEIGHING_SCALE_ZERO)
        assertEquals(ProofPreRecordBriefing.WEIGHING_SCALE_ZERO, weighing.preRecordBriefing)
    }

    @Test
    fun `the fake capture source records the context it was asked to record with`() {
        // The weighing ViewModel tests assert the briefing THROUGH this fake, so the fake must
        // faithfully keep what it was handed rather than only counting calls.
        val fake = FakeProofCaptureSource()
        val context = ProofCaptureContext(
            title = "Weighing",
            primaryTag = "TAG-1",
            preRecordBriefing = ProofPreRecordBriefing.WEIGHING_SCALE_ZERO,
        )
        kotlinx.coroutines.runBlocking { fake.captureVideo(context) }
        assertEquals(listOf<ProofCaptureContext?>(context), fake.captureContexts)
    }
}
