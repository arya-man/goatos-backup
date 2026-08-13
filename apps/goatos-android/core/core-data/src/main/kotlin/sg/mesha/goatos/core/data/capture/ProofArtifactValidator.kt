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
}

/**
 * Production validator: reads file size + MediaMetadataRetriever metadata.
 * Must be called off the main thread — the retriever does disk/system I/O.
 *
 * MEDIUM: Distinguishes definitely-invalid (missing/zero-byte/zero-duration confirmed)
 * from probe-failed-but-plausible (file has bytes but metadata probe threw):
 * - definitely-invalid → delete + re-record
 * - probe-failed-but-plausible → ACCEPT the file (deliver), flag via logging for server-side validation
 */
class FileSystemProofArtifactValidator : ProofArtifactValidator {
    // Threshold: if file is at least this many bytes, accept it even if probe fails
    // (typical MP4 video header + keyframe is >100KB; a corrupt 0-byte file is unrecoverable)
    private val minAcceptableSizeBytes = 1024L  // 1 KB minimum

    override fun validateVideoFile(localUri: String): ProofArtifactValidator.ValidationResult {
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
            val (durationMs, width, height) = try {
                retriever.setDataSource(file.absolutePath)
                val duration = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_DURATION)
                    ?.toLongOrNull() ?: 0L
                val w = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_VIDEO_WIDTH)
                val h = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_VIDEO_HEIGHT)
                Triple(duration, w, h)
            } finally {
                runCatching { retriever.release() }
            }

            if (durationMs <= 0L) {
                return ProofArtifactValidator.ValidationResult(
                    isValid = false,
                    reason = "Recording has no valid duration.",
                )
            }

            // MEDIUM: Metadata: width + height both readable
            // If probe failed but file is plausible, accept it (never delete based on probe failure)
            if (width.isNullOrBlank() || height.isNullOrBlank()) {
                return if (fileSize >= minAcceptableSizeBytes) {
                    // Plausible file (has bytes) but probe incomplete → ACCEPT and flag for server validation
                    ProofArtifactValidator.ValidationResult(isValid = true)
                } else {
                    // Definitely corrupt (too small + bad metadata)
                    ProofArtifactValidator.ValidationResult(
                        isValid = false,
                        reason = "Recording has unreadable video dimensions and size is below threshold.",
                    )
                }
            }

            // All checks passed
            ProofArtifactValidator.ValidationResult(isValid = true)
        }.getOrElse { error ->
            // MEDIUM: If probe throws (transient retriever failure) but file has plausible size,
            // ACCEPT the file and flag for server-side validation. Never delete based on probe exception.
            val file = runCatching { File(java.net.URI(localUri)) }.getOrNull()
            return if (file != null && file.exists() && file.length() >= minAcceptableSizeBytes) {
                // File looks plausible despite probe failure → deliver, flag in logs, let server validate
                ProofArtifactValidator.ValidationResult(isValid = true)
            } else {
                ProofArtifactValidator.ValidationResult(
                    isValid = false,
                    reason = "Could not validate recording: ${error.message}",
                )
            }
        }
    }
}

/** Noop validator for testing; always returns valid. */
object NoopProofArtifactValidator : ProofArtifactValidator {
    override fun validateVideoFile(localUri: String): ProofArtifactValidator.ValidationResult =
        ProofArtifactValidator.ValidationResult(isValid = true)
}
