package sg.mesha.goatos.core.proofedit

import android.content.Context
import android.media.MediaMetadataRetriever
import android.net.Uri
import androidx.annotation.OptIn
import androidx.media3.common.MediaItem
import androidx.media3.common.MimeTypes
import androidx.media3.common.util.UnstableApi
import androidx.media3.transformer.Composition
import androidx.media3.transformer.EditedMediaItem
import androidx.media3.transformer.EditedMediaItemSequence
import androidx.media3.transformer.ExportException
import androidx.media3.transformer.ExportResult
import androidx.media3.transformer.Transformer
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
import java.io.File
import java.net.URI
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

/**
 * Joins the operator's kept ranges into ONE clip.
 *
 * Runs on the RAW capture, BEFORE the proof pipeline processes it. It deliberately does NOT
 * compress and does NOT draw an overlay, because `ProofMediaProcessor` still does both to this
 * file afterwards exactly as it does for an unedited capture. Compressing here would hand that
 * processor an already-reduced clip and stack two reductions; drawing an overlay here would be
 * stamped over by the real one.
 *
 * That ordering is what keeps this an ADD-ON: the trimmed file re-enters the existing pipeline in
 * the same shape an untrimmed capture does, so nothing downstream needs to know editing happened
 * and no suppression flag is required anywhere.
 *
 * It re-encodes rather than remuxing because a keep-range boundary generally falls mid-GOP; a
 * sample-copy export can only cut on sync frames and would silently hand back extra footage
 * outside the range the operator selected.
 */
@OptIn(UnstableApi::class)
class ProofVideoStitcher(private val context: Context) {

    data class Result(
        val outputUri: String,
        val outputMimeType: String,
        val clipCount: Int,
        val outputDurationMs: Long,
        val outputBytes: Long,
    )

    suspend fun stitch(sourceUri: String, clips: List<ProofClip>): Result {
        val source = resolveLocalFile(sourceUri)
        val kept = clips.normalized()
        require(kept.isNotEmpty()) { "stitch requires at least one kept clip" }

        val metadata = readVideoMetadata(source)
        val bitrate = metadata.bitrate

        val edited = kept.map { clip ->
            val item = MediaItem.Builder()
                .setUri(Uri.fromFile(source))
                .setMimeType(MimeTypes.VIDEO_MP4)
                .setClippingConfiguration(
                    MediaItem.ClippingConfiguration.Builder()
                        .setStartPositionMs(clip.startMs)
                        .setEndPositionMs(clip.endMs)
                        .build(),
                )
                .build()
            // No Effects on purpose: ProofMediaProcessor applies the overlay and the
            // presentation scaling to this file afterwards, exactly as for an untrimmed capture.
            EditedMediaItem.Builder(item).build()
        }

        val sequence = EditedMediaItemSequence.Builder().apply { edited.forEach { addItem(it) } }.build()
        val composition = Composition.Builder(sequence)
            .setTransmuxAudio(false)
            .setTransmuxVideo(false)
            .build()

        val output = File(File(context.filesDir, "proofs/edited").apply { mkdirs() }, "edited-${source.name}")

        val export = withContext(Dispatchers.Main.immediate) {
            Transformer.Builder(context)
                .setVideoMimeType(MimeTypes.VIDEO_H264)
                .setAudioMimeType(MimeTypes.AUDIO_AAC)
                .build()
                .exportAwait(composition, output.absolutePath)
        }

        return Result(
            outputUri = output.toURI().toString(),
            outputMimeType = "video/mp4",
            clipCount = kept.size,
            outputDurationMs = kept.totalDurationMs(),
            outputBytes = output.length().takeIf { it > 0L } ?: export.fileSizeBytes.coerceAtLeast(0L),
        ).also { require(bitrate == null || it.outputBytes > 0L) { "stitched proof is empty" } }
    }

    private suspend fun Transformer.exportAwait(composition: Composition, outputPath: String): ExportResult =
        suspendCancellableCoroutine { continuation ->
            val listener = object : Transformer.Listener {
                override fun onCompleted(composition: Composition, exportResult: ExportResult) {
                    removeListener(this)
                    if (continuation.isActive) continuation.resume(exportResult)
                }

                override fun onError(
                    composition: Composition,
                    exportResult: ExportResult,
                    exportException: ExportException,
                ) {
                    removeListener(this)
                    if (continuation.isActive) continuation.resumeWithException(exportException)
                }
            }
            addListener(listener)
            continuation.invokeOnCancellation {
                removeListener(listener)
                cancel()
            }
            start(composition, outputPath)
        }

    /**
     * Resolves the source and REFUSES anything outside this app's own capture/proof directories.
     *
     * The caller today always passes a file the recorder produced, but this is a shared module and
     * its `stitch(sourceUri, ...)` signature invites a future caller to hand it an externally
     * sourced uri. Without this check such a caller would re-encode any app-readable file into
     * `filesDir/proofs/edited`, and from there it would be uploaded as audit evidence — laundering
     * arbitrary content into a proof. Mirrors the owned-roots check `discardOrphanCapture` already
     * applies on the delete side.
     */
    private fun resolveLocalFile(uriText: String): File {
        val uri = Uri.parse(uriText)
        val file = when (uri.scheme) {
            null -> File(uriText)
            "file" -> File(URI(uriText))
            else -> error("proof editor requires an app-private file uri")
        }
        val ownedRoots = listOf(
            File(context.filesDir, "captures"),
            File(context.filesDir, "proofs"),
            File(context.cacheDir, "proof-videos"),
        )
        val canonical = file.canonicalPath
        require(ownedRoots.any { canonical.startsWith(it.canonicalPath + File.separator) }) {
            "proof editor refuses a source outside this app's own capture storage"
        }
        return file.takeIf { it.exists() } ?: error("proof source file not found")
    }

    companion object {
        data class VideoMetadata(val width: Int, val height: Int, val bitrate: Int?, val durationMs: Long)

        fun readVideoMetadata(file: File): VideoMetadata {
            val retriever = MediaMetadataRetriever()
            return try {
                retriever.setDataSource(file.absolutePath)
                val w = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_VIDEO_WIDTH)?.toIntOrNull() ?: 720
                val h = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_VIDEO_HEIGHT)?.toIntOrNull() ?: 1280
                val rotation = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_VIDEO_ROTATION)?.toIntOrNull() ?: 0
                val bitrate = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_BITRATE)?.toIntOrNull()
                val duration = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_DURATION)?.toLongOrNull() ?: 0L
                if (rotation == 90 || rotation == 270) {
                    VideoMetadata(h, w, bitrate, duration)
                } else {
                    VideoMetadata(w, h, bitrate, duration)
                }
            } finally {
                retriever.release()
            }
        }
    }
}

private fun ProofVideoStitcher.readVideoMetadata(file: File) = ProofVideoStitcher.readVideoMetadata(file)
