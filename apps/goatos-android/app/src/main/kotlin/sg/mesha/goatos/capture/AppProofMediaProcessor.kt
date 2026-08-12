package sg.mesha.goatos.capture

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.RectF
import android.media.MediaMetadataRetriever
import android.net.Uri
import androidx.annotation.OptIn
import androidx.media3.common.MediaItem
import androidx.media3.common.MimeTypes
import androidx.media3.common.util.UnstableApi
import androidx.media3.effect.BitmapOverlay
import androidx.media3.effect.OverlayEffect
import androidx.media3.effect.Presentation
import androidx.media3.effect.StaticOverlaySettings
import androidx.media3.transformer.AudioEncoderSettings
import androidx.media3.transformer.Composition
import androidx.media3.transformer.DefaultEncoderFactory
import androidx.media3.transformer.EditedMediaItem
import androidx.media3.transformer.EditedMediaItemSequence
import androidx.media3.transformer.Effects
import androidx.media3.transformer.ExportException
import androidx.media3.transformer.ExportResult
import androidx.media3.transformer.Transformer
import androidx.media3.transformer.VideoEncoderSettings
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.suspendCancellableCoroutine
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.capture.ProofMediaProcessingRequest
import sg.mesha.goatos.core.data.capture.ProofMediaProcessingResult
import sg.mesha.goatos.core.data.capture.ProofMediaProcessor
import java.io.File
import java.net.URI
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import javax.inject.Inject
import javax.inject.Singleton
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException
import kotlin.math.max
import kotlin.math.min

@Singleton
@OptIn(UnstableApi::class)
class AppProofMediaProcessor @Inject constructor(
    @ApplicationContext private val context: Context,
    private val bootstrapRepository: BootstrapRepository,
    private val crashReporter: CrashReporter,
) : ProofMediaProcessor {
    override suspend fun process(request: ProofMediaProcessingRequest): ProofMediaProcessingResult =
        if (request.mimeType.startsWith("image/", ignoreCase = true)) {
            processPhoto(request)
        } else {
            processVideo(request)
        }

    private suspend fun processPhoto(request: ProofMediaProcessingRequest): ProofMediaProcessingResult {
        val source = resolveLocalFile(request.originalUri)
        val originalBytes = source.length().takeIf { it > 0L }
        val bitmap = BitmapFactory.decodeFile(source.absolutePath)
            ?: error("proof photo decode failed")
        val outputBitmap = bitmap.copy(Bitmap.Config.ARGB_8888, true)
        bitmap.recycle()
        val outputWidth = outputBitmap.width
        val outputHeight = outputBitmap.height
        return try {
            drawAuditOverlay(Canvas(outputBitmap), outputBitmap.width, outputBitmap.height, overlayLines(request))
            val output = processedFile(request.proofId, "jpg")
            output.outputStream().use { stream ->
                check(outputBitmap.compress(Bitmap.CompressFormat.JPEG, 92, stream)) {
                    "proof photo encode failed"
                }
            }
            ProofMediaProcessingResult(
                outputUri = output.toURI().toString(),
                outputMimeType = "image/jpeg",
                originalBytes = originalBytes,
                processedBytes = output.length().takeIf { it > 0L },
                inputWidth = outputWidth,
                inputHeight = outputHeight,
            )
        } finally {
            outputBitmap.recycle()
        }
    }

    private suspend fun processVideo(request: ProofMediaProcessingRequest): ProofMediaProcessingResult {
        val source = resolveLocalFile(request.originalUri)
        val metadata = readVideoMetadata(source)
        val targetVideoBitrate = selectVideoBitrate(metadata.width, metadata.height, metadata.bitrate)
        val targetAudioBitrate = 48_000
        val overlayBitmap = createOverlayBitmap(overlayLines(request), metadata.width, metadata.height)
        val overlaySettings = StaticOverlaySettings.Builder()
            .setOverlayFrameAnchor(1f, 1f)
            .setBackgroundFrameAnchor(1f, -1f)
            .build()
        val output = processedFile(request.proofId, "mp4")
        val mediaItem = MediaItem.Builder()
            .setUri(Uri.fromFile(source))
            .setMimeType(request.mimeType.ifBlank { MimeTypes.VIDEO_MP4 })
            .build()
        val effects = Effects(
            emptyList(),
            listOf(
                Presentation.createForShortSide(min(metadata.width, metadata.height).coerceAtLeast(480)),
                OverlayEffect(
                    listOf(
                        BitmapOverlay.createStaticBitmapOverlay(overlayBitmap, overlaySettings),
                    ),
                ),
            ),
        )
        val edited = EditedMediaItem.Builder(mediaItem)
            .setEffects(effects)
            .build()
        val composition = Composition.Builder(EditedMediaItemSequence.Builder(edited).build())
            .setTransmuxAudio(false)
            .setTransmuxVideo(false)
            .build()
        val transformer = Transformer.Builder(context)
            .setVideoMimeType(MimeTypes.VIDEO_H264)
            .setAudioMimeType(MimeTypes.AUDIO_AAC)
            .setEncoderFactory(
                DefaultEncoderFactory.Builder(context)
                    .setRequestedVideoEncoderSettings(
                        VideoEncoderSettings.Builder()
                            .setBitrate(targetVideoBitrate)
                            .setiFrameIntervalSeconds(2f)
                            .build(),
                    )
                    .setRequestedAudioEncoderSettings(
                        AudioEncoderSettings.Builder()
                            .setBitrate(targetAudioBitrate)
                            .build(),
                    )
                    .setEnableFallback(true)
                    .build(),
            )
            .build()
        val result = try {
            transformer.exportAwait(composition, output.absolutePath)
        } finally {
            overlayBitmap.recycle()
        }
        return ProofMediaProcessingResult(
            outputUri = output.toURI().toString(),
            outputMimeType = "video/mp4",
            originalBytes = source.length().takeIf { it > 0L },
            processedBytes = output.length().takeIf { it > 0L } ?: result.fileSizeBytes.takeIf { it > 0L },
            inputWidth = metadata.width,
            inputHeight = metadata.height,
            targetVideoBitrate = targetVideoBitrate,
            targetAudioBitrate = targetAudioBitrate,
        )
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

    private suspend fun overlayLines(request: ProofMediaProcessingRequest): List<String> = buildList {
        val profile = runCatching { bootstrapRepository.operatorProfile() }
            .onFailure { crashReporter.recordException(it, "proof media overlay profile lookup failed") }
            .getOrNull()
        add(localTimestamp(request.capturedStartMs))
        val operator = profile?.displayName?.ifBlank { profile.displayCode }?.ifBlank { null }
            ?: request.capturedByPrincipalId?.takeIf { it.isNotBlank() }
        operator?.let { add("Operator: $it") }
        request.rfidTag?.takeIf { it.isNotBlank() }?.let { add("RFID: $it") }
        request.caption
            ?.takeIf { it.isNotBlank() && it != request.rfidTag }
            ?.let { add(it) }
            ?: add("${request.subjectType}: ${request.subjectId ?: request.taskId}")
        profile?.primaryLocation?.takeIf { it.isNotBlank() }?.let { add(it) }
    }

    private fun createOverlayBitmap(lines: List<String>, videoWidth: Int, videoHeight: Int): Bitmap {
        val longest = lines.maxOfOrNull { it.length } ?: 24
        val width = min(max(360, longest * 18 + 44), (videoWidth * 0.62f).toInt().coerceAtLeast(260))
        val height = min(lines.size * 34 + 32, (videoHeight * 0.34f).toInt().coerceAtLeast(116))
        return Bitmap.createBitmap(width, height, Bitmap.Config.ARGB_8888).also {
            drawAuditOverlay(Canvas(it), width, height, lines)
        }
    }

    private fun drawAuditOverlay(canvas: Canvas, width: Int, height: Int, lines: List<String>) {
        val density = context.resources.displayMetrics.density
        val textSize = (14f * density).coerceIn(18f, 30f)
        val padding = (10f * density).coerceIn(12f, 22f)
        val lineGap = (4f * density).coerceIn(4f, 8f)
        val textPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
            color = Color.argb(255, 255, 255, 255)
            this.textSize = textSize
            setShadowLayer(2.5f, 0f, 1.5f, Color.argb(255, 0, 0, 0))
        }
        val bgPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
            color = Color.argb(166, 0, 0, 0)
        }
        canvas.drawRoundRect(RectF(0f, 0f, width.toFloat(), height.toFloat()), 10f, 10f, bgPaint)
        var y = padding - textPaint.fontMetrics.ascent
        lines.forEach { line ->
            canvas.drawText(line.take(72), padding, y, textPaint)
            y += textSize + lineGap
        }
    }

    private fun selectVideoBitrate(width: Int, height: Int, originalBitrate: Int?): Int {
        val longSide = max(width, height)
        val profile = when {
            longSide >= 1080 -> 6_000_000
            longSide >= 720 -> 2_500_000
            else -> 800_000
        }
        val capped = originalBitrate?.takeIf { it > 0 }?.let { (it * 0.70f).toInt() } ?: profile
        val minimum = when {
            longSide >= 1080 -> 4_000_000
            longSide >= 720 -> 2_000_000
            else -> 700_000
        }
        return min(profile, max(capped, minimum))
    }

    private fun readVideoMetadata(file: File): VideoMetadata {
        val retriever = MediaMetadataRetriever()
        return try {
            retriever.setDataSource(file.absolutePath)
            val width = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_VIDEO_WIDTH)?.toIntOrNull() ?: 720
            val height = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_VIDEO_HEIGHT)?.toIntOrNull() ?: 1280
            val rotation = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_VIDEO_ROTATION)?.toIntOrNull() ?: 0
            val bitrate = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_BITRATE)?.toIntOrNull()
            if (rotation == 90 || rotation == 270) VideoMetadata(height, width, bitrate) else VideoMetadata(width, height, bitrate)
        } finally {
            retriever.release()
        }
    }

    private fun processedFile(proofId: String, extension: String): File {
        val dir = File(context.filesDir, "proofs/processed").apply { mkdirs() }
        return File(dir, "proof_${proofId}_${System.currentTimeMillis()}.$extension")
    }

    private fun resolveLocalFile(uriText: String): File {
        val uri = Uri.parse(uriText)
        return when (uri.scheme) {
            null -> File(uriText)
            "file" -> File(URI(uriText))
            else -> error("proof processor requires app-private file URI, got ${uri.scheme}")
        }.takeIf { it.exists() } ?: error("proof source file not found")
    }

    private fun localTimestamp(ms: Long): String =
        SimpleDateFormat("MMM d, yyyy h:mm:ss a", Locale.US).format(Date(ms.takeIf { it > 0L } ?: System.currentTimeMillis()))

    private data class VideoMetadata(val width: Int, val height: Int, val bitrate: Int?)
}
