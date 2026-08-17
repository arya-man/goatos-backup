package sg.mesha.goatos.core.proofedit

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The capture flow's terminal rules. These are the ones that decide whether an operator keeps or
 * loses a video they actually filmed, so each failure mode is asserted explicitly rather than
 * being left to the host's branching.
 */
class ProofEditDecisionTest {

    private val clips = listOf(ProofClip(1_000, 10_000), ProofClip(20_000, 30_000))

    @Test
    fun `a good stitch is delivered as the edited artifact`() {
        val outcome = ProofEditDecision.edited(
            keptClips = clips,
            stitchOutputUri = "file:///data/edited.mp4",
            stitchOutputMimeType = "video/mp4",
            stitchFailed = false,
            outputValid = true,
        )
        assertEquals(ProofEditOutcome.UseEdited("file:///data/edited.mp4", "video/mp4"), outcome)
    }

    @Test
    fun `a failed export keeps the operator's recording instead of losing it`() {
        // The clip was really filmed. Discarding it would send the operator back to the shed.
        val outcome = ProofEditDecision.edited(
            keptClips = clips,
            stitchOutputUri = null,
            stitchOutputMimeType = null,
            stitchFailed = true,
            outputValid = false,
        )
        assertEquals(ProofEditOutcome.UseOriginal(ProofEditDecision.REASON_STITCH_FAILED), outcome)
    }

    @Test
    fun `an unusable export never reaches the upload queue as evidence`() {
        val outcome = ProofEditDecision.edited(
            keptClips = clips,
            stitchOutputUri = "file:///data/edited.mp4",
            stitchOutputMimeType = "video/mp4",
            stitchFailed = false,
            outputValid = false,
        )
        assertEquals(ProofEditOutcome.UseOriginal(ProofEditDecision.REASON_INVALID_OUTPUT), outcome)
    }

    @Test
    fun `a blank output uri is treated as unusable even when the export claimed success`() {
        val outcome = ProofEditDecision.edited(
            keptClips = clips,
            stitchOutputUri = "   ",
            stitchOutputMimeType = "video/mp4",
            stitchFailed = false,
            outputValid = true,
        )
        assertEquals(ProofEditOutcome.UseOriginal(ProofEditDecision.REASON_INVALID_OUTPUT), outcome)
    }

    @Test
    fun `nothing cut uses the recording whole rather than re-encoding it`() {
        val outcome = ProofEditDecision.edited(
            keptClips = emptyList(),
            stitchOutputUri = null,
            stitchOutputMimeType = null,
            stitchFailed = false,
            outputValid = true,
        )
        assertEquals(ProofEditOutcome.UseOriginal(ProofEditDecision.REASON_NOTHING_CUT), outcome)
    }

    @Test
    fun `gate refusal passes the recording straight through unchanged`() {
        assertEquals(
            ProofEditOutcome.UseOriginal(ProofEditDecision.REASON_NOT_OFFERED),
            ProofEditDecision.editingNotOffered(),
        )
    }

    @Test
    fun `explicit abandon is the only path that discards a recording`() {
        assertEquals(
            ProofEditOutcome.Cancel(ProofEditDecision.REASON_ABANDONED),
            ProofEditDecision.abandoned(),
        )
        assertEquals(
            ProofEditOutcome.Cancel(ProofEditDecision.REASON_NO_RECORDING),
            ProofEditDecision.noRecording(),
        )
    }

    @Test
    fun `only an abandon or a missing recording ever cancels`() {
        // Every other combination must resolve to a delivered artifact, so the relay always
        // receives exactly one result and the capture can never silently vanish.
        val combos = buildList {
            for (failed in listOf(true, false)) {
                for (valid in listOf(true, false)) {
                    for (uri in listOf(null, "", "file:///data/e.mp4")) {
                        for (kept in listOf(emptyList(), clips)) {
                            add(
                                ProofEditDecision.edited(
                                    keptClips = kept,
                                    stitchOutputUri = uri,
                                    stitchOutputMimeType = "video/mp4",
                                    stitchFailed = failed,
                                    outputValid = valid,
                                ),
                            )
                        }
                    }
                }
            }
        }
        assertTrue(
            "no editor completion may cancel a capture",
            combos.none { it is ProofEditOutcome.Cancel },
        )
        assertEquals("every combination must resolve", 24, combos.size)
    }

    @Test
    fun `mime type falls back to mp4 rather than delivering a blank type`() {
        val outcome = ProofEditDecision.edited(
            keptClips = clips,
            stitchOutputUri = "file:///data/edited.mp4",
            stitchOutputMimeType = "",
            stitchFailed = false,
            outputValid = true,
        )
        assertEquals(ProofEditOutcome.UseEdited("file:///data/edited.mp4", "video/mp4"), outcome)
    }
}
