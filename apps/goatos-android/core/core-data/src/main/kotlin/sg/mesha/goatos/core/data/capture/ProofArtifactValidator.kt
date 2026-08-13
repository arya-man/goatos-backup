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
 */
class FileSystemProofArtifactValidator : ProofArtifactValidator {
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

            // Metadata must be readable (off-main thread; safe here)
            val retriever = MediaMetadataRetriever()
            val durationMs = try {
                retriever.setDataSource(file.absolutePath)
                retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_DURATION)
                    ?.toLongOrNull() ?: 0L
            } finally {
                runCatching { retriever.release() }
            }

            if (durationMs <= 0L) {
                return ProofArtifactValidator.ValidationResult(
                    isValid = false,
                    reason = "Recording has no valid duration.",
                )
            }

            // Metadata: width + height both readable
            val width = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_VIDEO_WIDTH)
            val height = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_VIDEO_HEIGHT)
            if (width.isNullOrBlank() || height.isNullOrBlank()) {
                return ProofArtifactValidator.ValidationResult(
                    isValid = false,
                    reason = "Recording has unreadable video dimensions.",
                )
            }

            // All checks passed
            ProofArtifactValidator.ValidationResult(isValid = true)
        }.getOrElse { error ->
            ProofArtifactValidator.ValidationResult(
                isValid = false,
                reason = "Could not validate recording: ${error.message}",
            )
        }
    }
}

/** Noop validator for testing; always returns valid. */
object NoopProofArtifactValidator : ProofArtifactValidator {
    override fun validateVideoFile(localUri: String): ProofArtifactValidator.ValidationResult =
        ProofArtifactValidator.ValidationResult(isValid = true)
}
