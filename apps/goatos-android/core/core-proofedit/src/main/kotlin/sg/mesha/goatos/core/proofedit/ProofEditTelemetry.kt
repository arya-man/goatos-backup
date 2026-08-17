package sg.mesha.goatos.core.proofedit

/**
 * Telemetry contract for the capture→process→edit→save flow.
 *
 * PARAM BUDGET IS THE BINDING CONSTRAINT. `FIREBASE_PARAM_ALLOWLIST` in
 * `core-analytics/FirebaseAnalyticsAdapter.kt` holds exactly 25 entries, which is GA4's per-event
 * maximum, and `firebaseEventParams` drops anything past the limit IN LIST ORDER. Appending a new
 * key there would therefore silently stop `submit_status` — the last entry — from ever reaching
 * GA4 again, and nothing would fail loudly.
 *
 * So this flow adds NO new Firebase params. It reports new EVENT NAMES carrying keys already on
 * the allowlist, with new values:
 *
 *   feature_surface     which feature opened the camera (feed_packing, vaccination, ...)
 *   rfid_tag / rfid     the animal identity, when the surface has one
 *   proof_id / task_id  the proof row and its task
 *   proof_subject       what the proof is of
 *   processing_state    which STEP this event reports (see [Step])
 *   outcome / reason    how the step ended, and why when it failed
 *   duration_bucket     coarse output length
 *   original_size_bucket / processed_size_bucket
 *   capture_source / mime_type
 *
 * Richer editing detail (exact clip count, kept share, per-clip ranges) goes to the BACKEND proof
 * event, which has no 25-key ceiling — see `recordProofEvent` in `CaptureRepository`.
 */
object ProofEditTelemetry {

    /** Event names. Distinct events, shared params — this is what keeps the budget intact. */
    object Events {
        /** Camera opened for a capture. Carries the surface + rfid so a stuck flow is traceable. */
        const val CAPTURE_OPENED = "proof_capture_opened"

        /** Recording finalized and handed back a file. */
        const val CAPTURE_RECORDED = "proof_capture_recorded"

        /** Compress + overlay pass started / finished / failed. */
        const val PROCESS_STARTED = "proof_process_started"
        const val PROCESS_SUCCEEDED = "proof_process_succeeded"
        const val PROCESS_FAILED = "proof_process_failed"

        /** Trim editor shown (only when [ProofEditGate] allowed it). */
        const val EDIT_OPENED = "proof_edit_opened"

        /** Operator kept a range / removed one / cleared all. */
        const val EDIT_CLIP_KEPT = "proof_edit_clip_kept"
        const val EDIT_CLIP_REMOVED = "proof_edit_clip_removed"
        const val EDIT_CLEARED = "proof_edit_cleared"

        /** Operator finished the editor. `outcome` says whether anything was actually cut. */
        const val EDIT_DONE = "proof_edit_done"

        /** Stitch pass started / finished / failed. */
        const val STITCH_STARTED = "proof_stitch_started"
        const val STITCH_SUCCEEDED = "proof_stitch_succeeded"
        const val STITCH_FAILED = "proof_stitch_failed"

        /** Flow handed a final artifact back to the proofs screen. Closes the funnel. */
        const val CAPTURE_DELIVERED = "proof_capture_delivered"

        /** Operator abandoned the flow. `processing_state` says which step they were on. */
        const val CAPTURE_ABANDONED = "proof_capture_abandoned"
    }

    /** Values for the existing `processing_state` param — which step an event belongs to. */
    object Step {
        const val RECORDING = "recording"
        const val PROCESSING = "processing"
        const val EDITING = "editing"
        const val STITCHING = "stitching"
        const val DELIVERING = "delivering"
    }

    /** Values for the existing `outcome` param. */
    object Outcome {
        const val OK = "ok"
        const val FAILED = "failed"
        const val CANCELLED = "cancelled"

        /** Editor finished with cuts applied. */
        const val EDITED = "edited"

        /** Editor finished with nothing cut — the processed clip is used whole. */
        const val UNEDITED = "unedited"

        /** Editor was not offered, because the gate said no. */
        const val EDIT_NOT_OFFERED = "edit_not_offered"
    }

    /** Existing allowlisted param keys this flow writes. NEVER add a key that is not already
     *  on the Firebase allowlist; see the class comment for why. */
    object Params {
        const val FEATURE_SURFACE = "feature_surface"
        const val PROOF_SUBJECT = "proof_subject"
        const val RFID_TAG = "rfid_tag"
        const val PROOF_ID = "proof_id"
        const val TASK_ID = "task_id"
        const val PROCESSING_STATE = "processing_state"
        const val OUTCOME = "outcome"
        const val REASON = "reason"
        const val DURATION_BUCKET = "duration_bucket"
        const val CAPTURE_SOURCE = "capture_source"
        const val MIME_TYPE = "mime_type"
    }

    /** Coarse duration bucket, so an exact length never becomes a high-cardinality param. */
    fun durationBucket(durationMs: Long): String = when {
        durationMs <= 0L -> "unknown"
        durationMs < 10_000L -> "lt_10s"
        durationMs < 30_000L -> "10_30s"
        durationMs < 60_000L -> "30_60s"
        durationMs < 180_000L -> "1_3m"
        else -> "gt_3m"
    }
}
