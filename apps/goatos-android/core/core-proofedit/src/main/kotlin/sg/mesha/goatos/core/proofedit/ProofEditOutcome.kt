package sg.mesha.goatos.core.proofedit

/**
 * What the capture flow hands back, and why.
 *
 * Extracted from the Compose host so the rules below are testable without a device: the flow's
 * correctness is not "does it render", it is "does every path deliver exactly one artifact, and is
 * that artifact the right one".
 */
sealed interface ProofEditOutcome {

    /** The operator's original recording, unmodified. */
    data class UseOriginal(val reason: String) : ProofEditOutcome

    /** The joined file. Only ever produced from a validated, non-empty stitch. */
    data class UseEdited(val outputUri: String, val outputMimeType: String) : ProofEditOutcome

    /** Nothing is delivered; the capture is abandoned. */
    data class Cancel(val reason: String) : ProofEditOutcome
}

/**
 * The single decision table for how a capture ends.
 *
 * Deliberately total: every combination of inputs resolves to exactly one outcome, so the host
 * cannot fall through a branch and deliver twice or not at all — the failure mode
 * `ProofCaptureRelay` exists to prevent, one layer up.
 *
 * The bias is ALWAYS toward keeping the operator's footage. An export failure, an unusable output,
 * or an empty keep-list all fall back to the untrimmed recording, because a clip that was really
 * filmed is valid evidence and losing it costs the operator a trip back to the shed. Only an
 * explicit abandon discards anything.
 */
object ProofEditDecision {

    /** Operator pressed back and confirmed discard. */
    fun abandoned(): ProofEditOutcome = ProofEditOutcome.Cancel(REASON_ABANDONED)

    /** Recorder returned nothing (operator cancelled, or capture failed). */
    fun noRecording(): ProofEditOutcome = ProofEditOutcome.Cancel(REASON_NO_RECORDING)

    /** The gate refused editing for this surface, so the recording passes straight through. */
    fun editingNotOffered(): ProofEditOutcome = ProofEditOutcome.UseOriginal(REASON_NOT_OFFERED)

    /** Operator finished the editor. */
    fun edited(
        keptClips: List<ProofClip>,
        stitchOutputUri: String?,
        stitchOutputMimeType: String?,
        stitchFailed: Boolean,
        outputValid: Boolean,
    ): ProofEditOutcome = when {
        // Nothing cut: re-encoding an untouched clip would cost a generation of quality for no
        // change, so the recording is used as-is.
        keptClips.normalized().isEmpty() -> ProofEditOutcome.UseOriginal(REASON_NOTHING_CUT)
        stitchFailed -> ProofEditOutcome.UseOriginal(REASON_STITCH_FAILED)
        // A zero-byte or unreadable export must never reach the upload queue as evidence.
        !outputValid -> ProofEditOutcome.UseOriginal(REASON_INVALID_OUTPUT)
        stitchOutputUri.isNullOrBlank() -> ProofEditOutcome.UseOriginal(REASON_INVALID_OUTPUT)
        else -> ProofEditOutcome.UseEdited(
            outputUri = stitchOutputUri,
            outputMimeType = stitchOutputMimeType?.takeIf { it.isNotBlank() } ?: "video/mp4",
        )
    }

    const val REASON_ABANDONED = "abandoned"
    const val REASON_NO_RECORDING = "no_recording"
    const val REASON_NOT_OFFERED = "edit_not_offered"
    const val REASON_NOTHING_CUT = "nothing_cut"
    const val REASON_STITCH_FAILED = "stitch_failed"
    const val REASON_INVALID_OUTPUT = "invalid_output"
}
