package sg.mesha.goatos.core.data.capture

import android.media.MediaMetadataRetriever
import java.io.File

/**
 * Pure, testable validator for proof artifacts (video files). Ensures:
 * - File exists and is non-zero
 * - Metadata readable (duration, width, height)
 * - Duration > 0 (non-empty recording)
 *
 * All validation runs off the main thread via the caller's dispatcher context.
 * Inject as a dependency for testability; NO MediaMetadataRetriever instantiation
 * in UI code.
 *
 * ITEM 6 distinction:
 * - Original camera files: probe-threw-but-plausible-size → accept (OEM quirks justified)
 * - Processed artifacts (we control encoder): probe-threw OR metadata-invalid → reject decisively
 */
interface ProofArtifactValidator {
    data class ValidationResult(
        val isValid: Boolean,
        val reason: String? = null,  // null if valid
    )

    /**
     * Validate a proof video file: exists, non-zero, metadata readable, duration > 0.
     * Returns [ValidationResult.isValid] = true only if ALL checks pass.
     */
    fun validateVideoFile(localUri: String): ValidationResult

    /**
     * Validate a PROCESSED/compressed artifact: stricter than original validation.
     * For processed files we control the encoder on, metadata-probe failure is DEFINITIVE
     * rejection, never plausible-accept (unlike original camera files with OEM quirks).
     * Returns [ValidationResult.isValid] = true only if probe succeeds AND metadata is valid.
     */
    fun validateProcessedArtifact(localUri: String): ValidationResult =
        validateVideoFile(localUri)  // Default: same as original (overridable per implementation)
}

/**
 * Production validator: reads file size + MediaMetadataRetriever metadata.
 * Must be called off the main thread — the retriever does disk/system I/O.
 *
 * B5: Tightens validation per "probe-succeeded vs probe-threw" distinction:
 * - probe-succeeded-with-invalid-metadata (duration<=0, unreadable dims) → reject, never plausible-accept
 * - probe-threw-transient-failure + file>=threshold → accept (deliver to server for validation)
 * - definitely-invalid (missing, zero-byte) → delete + re-record
 *
 * ITEM 6: For PROCESSED artifacts (encoder-controlled), metadata-probe failure is DEFINITIVE
 * rejection (no plausible-accept), while ORIGINAL camera files use the threshold.
 */
class FileSystemProofArtifactValidator : ProofArtifactValidator {
    // Threshold: if file is at least this many bytes AND probe threw (not succeeded-with-bad-data),
    // accept it (typical MP4 video header + keyframe is >100KB; a corrupt 0-byte file is unrecoverable)
    private val minAcceptableSizeBytes = 1024L  // 1 KB minimum

    override fun validateVideoFile(localUri: String): ProofArtifactValidator.ValidationResult {
        return validateVideoFileImpl(localUri, allowPlausibleAccept = true)
    }

    /**
     * ITEM 6: Validate PROCESSED artifact with stricter rules.
     * Probe-threw (transient failure) → reject decisively (no plausible-accept for processed files).
     */
    override fun validateProcessedArtifact(localUri: String): ProofArtifactValidator.ValidationResult {
        return validateVideoFileImpl(localUri, allowPlausibleAccept = false)
    }

    private fun validateVideoFileImpl(
        localUri: String,
        allowPlausibleAccept: Boolean,
    ): ProofArtifactValidator.ValidationResult {
        return runCatching {
            val file = File(java.net.URI(localUri))

            // File must exist
            if (!file.exists()) {
                return ProofArtifactValidator.ValidationResult(
                    isValid = false,
                    reason = "Recording file not found.",
                )
            }

            // File must be non-zero
            if (file.length() <= 0L) {
                return ProofArtifactValidator.ValidationResult(
                    isValid = false,
                    reason = "Recording was empty.",
                )
            }

            val fileSize = file.length()

            // Metadata must be readable (off-main thread; safe here)
            val retriever = MediaMetadataRetriever()
            val probeResult = try {
                retriever.setDataSource(file.absolutePath)
                val duration = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_DURATION)
                    ?.toLongOrNull() ?: 0L
                val w = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_VIDEO_WIDTH)
                val h = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_VIDEO_HEIGHT)
                ProbeSuccess(duration, w, h)
            } finally {
                runCatching { retriever.release() }
            }

            // B5: Probe succeeded; check validity of extracted metadata
            when (probeResult) {
                is ProbeSuccess -> {
                    // Reject if probe succeeded but duration is invalid (not plausible-accept)
                    if (probeResult.durationMs <= 0L) {
                        return ProofArtifactValidator.ValidationResult(
                            isValid = false,
                            reason = "Recording has no valid duration.",
                        )
                    }
                    // Reject if probe succeeded but dimensions unreadable (not plausible-accept)
                    if (probeResult.width.isNullOrBlank() || probeResult.height.isNullOrBlank()) {
                        return ProofArtifactValidator.ValidationResult(
                            isValid = false,
                            reason = "Recording has unreadable video dimensions.",
                        )
                    }
                    // All checks passed
                    return ProofArtifactValidator.ValidationResult(isValid = true)
                }
            }
        }.getOrElse { error ->
            // B5: Probe threw (transient failure).
            // ITEM 6: For processed files (allowPlausibleAccept=false), this is DEFINITIVE rejection.
            // For original files (allowPlausibleAccept=true), accept only if file has plausible size.
            if (!allowPlausibleAccept) {
                // Processed artifact: metadata-probe failure is definitive rejection
                return ProofArtifactValidator.ValidationResult(
                    isValid = false,
                    reason = "Could not validate processed recording: ${error.message}",
                )
            }
            // exception:exempt malformed/non-file URI just falls through to the size-check branch below, which already handles a null file safely
            val file = runCatching { File(java.net.URI(localUri)) }.getOrNull()
            return if (file != null && file.exists() && file.length() >= minAcceptableSizeBytes) {
                // Original file looks plausible despite probe exception → deliver, flag in logs, let server validate
                ProofArtifactValidator.ValidationResult(isValid = true)
            } else {
                ProofArtifactValidator.ValidationResult(
                    isValid = false,
                    reason = "Could not validate recording: ${error.message}",
                )
            }
        }
    }

    private data class ProbeSuccess(val durationMs: Long, val width: String?, val height: String?)
}

/** Noop validator for testing; always returns valid. */
object NoopProofArtifactValidator : ProofArtifactValidator {
    override fun validateVideoFile(localUri: String): ProofArtifactValidator.ValidationResult =
        ProofArtifactValidator.ValidationResult(isValid = true)
}
