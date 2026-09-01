package sg.mesha.goatos.core.data.capture

import android.media.MediaExtractor
import android.media.MediaFormat
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
        val failureKind: String? = null,
        val containerDurationMs: Long? = null,
        val videoTrackDurationMs: Long? = null,
        val videoFrameRate: Double? = null,
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

    /**
     * Validation for a PROCESSED artifact whose kind is known from its mime type. A processed
     * PHOTO must never be judged by video metadata (a duration probe on a JPEG always fails and
     * silently pushed every compressed+overlaid photo onto the raw-fallback path — field bug
     * 2026-08-15: overlay-free originals reached the server). Images validate by decodability;
     * videos keep the strict processed probe.
     */
    fun validateProcessedArtifact(localUri: String, mimeType: String?): ValidationResult =
        if (mimeType?.startsWith("image/") == true) validateImageFile(localUri)
        else validateProcessedArtifact(localUri)

    /** Image validation: the file must exist, be non-empty, and decode to positive bounds. */
    fun validateImageFile(localUri: String): ValidationResult = ValidationResult(true, null)
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
class FileSystemProofArtifactValidator internal constructor(
    private val metadataProbe: (File) -> ProbeSuccess,
    private val videoTrackDurationReader: (File) -> Long,
    private val processedFrameDecoder: (File, Long) -> Boolean,
    private val videoTrackStatsReader: (File) -> VideoTrackStats = { file ->
        VideoTrackStats(durationMs = videoTrackDurationReader(file))
    },
) : ProofArtifactValidator {
    constructor() : this(
        metadataProbe = ::readMetadataProbe,
        videoTrackDurationReader = ::readVideoTrackDurationMs,
        processedFrameDecoder = ::canDecodeProcessedVideoFrames,
        videoTrackStatsReader = ::readVideoTrackStats,
    )

    // Threshold: if file is at least this many bytes AND probe threw (not succeeded-with-bad-data),
    // accept it (typical MP4 video header + keyframe is >100KB; a corrupt 0-byte file is unrecoverable)
    private val minAcceptableSizeBytes = 1024L  // 1 KB minimum

    override fun validateVideoFile(localUri: String): ProofArtifactValidator.ValidationResult {
        return validateVideoFileImpl(localUri, allowPlausibleAccept = true)
    }

    override fun validateImageFile(localUri: String): ProofArtifactValidator.ValidationResult {
        return try {
            // Same URI parsing as the video path: processed URIs arrive as file:/single-slash
            // (File.toURI) — a naive "file://" strip left the scheme in the path and made every
            // processed photo read as missing.
            val file = if (localUri.startsWith("file:")) java.io.File(java.net.URI(localUri))
            else java.io.File(localUri)
            val path = file.absolutePath
            if (!file.exists() || file.length() == 0L) {
                return ProofArtifactValidator.ValidationResult(false, "Photo file is missing or empty.")
            }
            val opts = android.graphics.BitmapFactory.Options().apply { inJustDecodeBounds = true }
            android.graphics.BitmapFactory.decodeFile(path, opts)
            if (opts.outWidth <= 0 || opts.outHeight <= 0) {
                ProofArtifactValidator.ValidationResult(false, "Photo could not be decoded.")
            } else {
                ProofArtifactValidator.ValidationResult(true, null)
            }
        } catch (_: Exception) {
            ProofArtifactValidator.ValidationResult(false, "Photo could not be read.")
        }
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
                    failureKind = "file_missing",
                )
            }

            // File must be non-zero
            if (file.length() <= 0L) {
                return ProofArtifactValidator.ValidationResult(
                    isValid = false,
                    reason = "Recording was empty.",
                    failureKind = "file_empty",
                )
            }

            val fileSize = file.length()

            // Metadata must be readable (off-main thread; safe here)
            val probeResult = metadataProbe(file)

            // B5: Probe succeeded; check validity of extracted metadata.
            if (probeResult.durationMs <= 0L) {
                return ProofArtifactValidator.ValidationResult(
                    isValid = false,
                    reason = "Recording has no valid duration.",
                    failureKind = "invalid_duration",
                )
            }
            if (probeResult.width.isNullOrBlank() || probeResult.height.isNullOrBlank()) {
                return ProofArtifactValidator.ValidationResult(
                    isValid = false,
                    reason = "Recording has unreadable video dimensions.",
                    failureKind = "unreadable_dimensions",
                )
            }
            val videoTrackStats = videoTrackStatsReader(file)
            videoTrackStats.frameRate?.let { frameRate ->
                if (frameRate < MIN_ACCEPTABLE_AVERAGE_FRAME_RATE) {
                    return ProofArtifactValidator.ValidationResult(
                        isValid = false,
                        reason = "Recording frame rate is too low. Please re-record.",
                        failureKind = "video_frame_rate_too_low",
                        containerDurationMs = probeResult.durationMs,
                        videoTrackDurationMs = videoTrackStats.durationMs,
                        videoFrameRate = frameRate,
                    )
                }
            }
            if (!allowPlausibleAccept) {
                if (videoTrackStats.durationMs <= 0L) {
                    return ProofArtifactValidator.ValidationResult(
                        isValid = false,
                        reason = "Recording has no readable video track duration.",
                        failureKind = "video_track_duration_unreadable",
                        containerDurationMs = probeResult.durationMs,
                        videoTrackDurationMs = videoTrackStats.durationMs,
                    )
                }
                if (videoTrackStats.durationMs < (probeResult.durationMs * MIN_VIDEO_TRACK_DURATION_RATIO).toLong()) {
                    return ProofArtifactValidator.ValidationResult(
                        isValid = false,
                        reason = "Recording video track ended before audio.",
                        failureKind = "processed_video_track_truncated",
                        containerDurationMs = probeResult.durationMs,
                        videoTrackDurationMs = videoTrackStats.durationMs,
                        videoFrameRate = videoTrackStats.frameRate,
                    )
                }
                if (!processedFrameDecoder(file, probeResult.durationMs)) {
                    return ProofArtifactValidator.ValidationResult(
                        isValid = false,
                        reason = "Recording could not be decoded after processing.",
                        failureKind = "processed_video_decode_failed",
                        containerDurationMs = probeResult.durationMs,
                        videoTrackDurationMs = videoTrackStats.durationMs,
                    )
                }
            }
            return ProofArtifactValidator.ValidationResult(isValid = true)
        }.getOrElse { error ->
            // B5: Probe threw (transient failure).
            // ITEM 6: For processed files (allowPlausibleAccept=false), this is DEFINITIVE rejection.
            // For original files (allowPlausibleAccept=true), accept only if file has plausible size.
            if (!allowPlausibleAccept) {
                // Processed artifact: metadata-probe failure is definitive rejection
                return ProofArtifactValidator.ValidationResult(
                    isValid = false,
                    reason = "Could not validate processed recording: ${error.message}",
                    failureKind = "metadata_probe_failed",
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
                    failureKind = "metadata_probe_failed",
                )
            }
        }
    }

    internal data class ProbeSuccess(val durationMs: Long, val width: String?, val height: String?)
    data class VideoTrackStats(val durationMs: Long, val frameRate: Double? = null)

    private companion object {
        private const val MIN_VIDEO_TRACK_DURATION_RATIO = 0.80f
        private const val MIN_ACCEPTABLE_AVERAGE_FRAME_RATE = 15.0
        private const val MAX_FRAME_RATE_SAMPLE_COUNT = 900

        private fun readMetadataProbe(file: File): ProbeSuccess {
            val retriever = MediaMetadataRetriever()
            return try {
                retriever.setDataSource(file.absolutePath)
                val duration = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_DURATION)
                    ?.toLongOrNull() ?: 0L
                val w = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_VIDEO_WIDTH)
                val h = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_VIDEO_HEIGHT)
                ProbeSuccess(duration, w, h)
            } finally {
                runCatching { retriever.release() }
            }
        }

        private fun canDecodeProcessedVideoFrames(file: File, durationMs: Long): Boolean {
            val retriever = MediaMetadataRetriever()
            return try {
                retriever.setDataSource(file.absolutePath)
                frameProbeTimesUs(durationMs).all { atUs ->
                    val frame = retriever.getFrameAtTime(atUs, MediaMetadataRetriever.OPTION_CLOSEST_SYNC)
                    try {
                        frame != null && frame.width > 0 && frame.height > 0
                    } finally {
                        frame?.recycle()
                    }
                }
            } catch (_: Throwable) {
                false
            } finally {
                runCatching { retriever.release() }
            }
        }

        private fun frameProbeTimesUs(durationMs: Long): List<Long> {
            val safeDurationMs = durationMs.coerceAtLeast(1L)
            val midpointMs = (safeDurationMs / 2L).coerceAtLeast(1L)
            val nearEndMs = (safeDurationMs - 500L).coerceAtLeast(midpointMs)
            return listOf(500L.coerceAtMost(safeDurationMs), midpointMs, nearEndMs)
                .distinct()
                .map { it * 1_000L }
        }

        private fun readVideoTrackDurationMs(file: File): Long {
            return readVideoTrackStats(file).durationMs
        }

        private fun readVideoTrackStats(file: File): VideoTrackStats {
            val extractor = MediaExtractor()
            return try {
                extractor.setDataSource(file.absolutePath)
                for (index in 0 until extractor.trackCount) {
                    val format = extractor.getTrackFormat(index)
                    val mime = format.getString(MediaFormat.KEY_MIME).orEmpty()
                    if (!mime.startsWith("video/", ignoreCase = true)) continue
                    if (!format.containsKey(MediaFormat.KEY_DURATION)) return VideoTrackStats(durationMs = 0L)
                    val durationMs = (format.getLong(MediaFormat.KEY_DURATION) / 1_000L).coerceAtLeast(0L)
                    if (durationMs <= 0L) return VideoTrackStats(durationMs = 0L)
                    extractor.selectTrack(index)
                    var samples = 0
                    while (extractor.sampleTime >= 0L) {
                        samples += 1
                        if (samples >= MAX_FRAME_RATE_SAMPLE_COUNT) break
                        if (!extractor.advance()) break
                    }
                    val sampledDurationMs = if (samples >= MAX_FRAME_RATE_SAMPLE_COUNT) {
                        (extractor.sampleTime / 1_000L).coerceAtLeast(1L)
                    } else {
                        durationMs
                    }
                    val frameRate = samples * 1000.0 / sampledDurationMs.coerceAtLeast(1L)
                    return VideoTrackStats(durationMs = durationMs, frameRate = frameRate)
                }
                VideoTrackStats(durationMs = 0L)
            } finally {
                extractor.release()
            }
        }
    }
}

/** Noop validator for testing; always returns valid. */
object NoopProofArtifactValidator : ProofArtifactValidator {
    override fun validateVideoFile(localUri: String): ProofArtifactValidator.ValidationResult =
        ProofArtifactValidator.ValidationResult(isValid = true)
}
