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
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
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
        val resized = resizePhotoForProof(bitmap)
        val outputBitmap = resized.copy(Bitmap.Config.ARGB_8888, true)
        if (resized !== bitmap) resized.recycle()
        bitmap.recycle()
        val outputWidth = outputBitmap.width
        val outputHeight = outputBitmap.height
        return try {
            drawAuditOverlayAtBottomRight(
                canvas = Canvas(outputBitmap),
                width = outputBitmap.width,
                height = outputBitmap.height,
                lines = overlayLines(request),
                textScale = photoOverlayScale(outputBitmap.width, outputBitmap.height),
            )
            val output = processedFile(request.proofId, "jpg")
            output.outputStream().use { stream ->
                check(outputBitmap.compress(Bitmap.CompressFormat.JPEG, 88, stream)) {
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
        val overlayBitmap = createVideoOverlayBitmap(overlayLines(request), metadata.width, metadata.height)
        val overlaySettings = StaticOverlaySettings.Builder()
            .setOverlayFrameAnchor(0f, 0f)
            .setBackgroundFrameAnchor(0f, 0f)
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
        val result = try {
            withContext(Dispatchers.Main.immediate) {
                Transformer.Builder(context)
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
                    .exportAwait(composition, output.absolutePath)
            }
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
        request.locationAddress?.takeIf { it.isNotBlank() }?.let { add("Loc: $it") }
            ?: request.latitude?.let { lat ->
                request.longitude?.let { lon ->
                    val accuracy = request.gpsAccuracyM?.let { " +/- ${it.toInt()}m" }.orEmpty()
                    add("Loc: %.5f, %.5f%s".format(Locale.US, lat, lon, accuracy))
                }
            }
            ?: profile?.primaryLocation?.takeIf { it.isNotBlank() }?.let { add("Loc: $it") }
    }

    private fun createOverlayBitmap(lines: List<String>, videoWidth: Int, videoHeight: Int): Bitmap {
        val layout = overlayLayout(lines, videoWidth, videoHeight)
        return Bitmap.createBitmap(layout.width, layout.height, Bitmap.Config.ARGB_8888).also {
            drawAuditOverlay(Canvas(it), layout)
        }
    }

    private fun createVideoOverlayBitmap(lines: List<String>, videoWidth: Int, videoHeight: Int): Bitmap =
        Bitmap.createBitmap(videoWidth.coerceAtLeast(1), videoHeight.coerceAtLeast(1), Bitmap.Config.ARGB_8888).also { bitmap ->
            drawAuditOverlayAtTopLeft(
                canvas = Canvas(bitmap),
                width = bitmap.width,
                height = bitmap.height,
                lines = lines,
                textScale = videoOverlayScale(bitmap.width, bitmap.height),
                maxWidthFraction = VIDEO_OVERLAY_MAX_WIDTH_FRACTION,
                maxHeightFraction = VIDEO_OVERLAY_MAX_HEIGHT_FRACTION,
            )
        }

    private fun drawAuditOverlayAtBottomRight(
        canvas: Canvas,
        width: Int,
        height: Int,
        lines: List<String>,
        textScale: Float,
        maxWidthFraction: Float = DEFAULT_OVERLAY_MAX_WIDTH_FRACTION,
        maxHeightFraction: Float = DEFAULT_OVERLAY_MAX_HEIGHT_FRACTION,
    ) {
        val layout = overlayLayout(lines, width, height, textScale, maxWidthFraction, maxHeightFraction)
        canvas.save()
        canvas.translate((width - layout.width).toFloat(), (height - layout.height).toFloat())
        drawAuditOverlay(canvas, layout)
        canvas.restore()
    }

    private fun drawAuditOverlayAtTopLeft(
        canvas: Canvas,
        width: Int,
        height: Int,
        lines: List<String>,
        textScale: Float,
        maxWidthFraction: Float = DEFAULT_OVERLAY_MAX_WIDTH_FRACTION,
        maxHeightFraction: Float = DEFAULT_OVERLAY_MAX_HEIGHT_FRACTION,
    ) {
        val layout = overlayLayout(lines, width, height, textScale, maxWidthFraction, maxHeightFraction)
        drawAuditOverlay(canvas, layout)
    }

    private fun overlayLayout(
        lines: List<String>,
        mediaWidth: Int,
        mediaHeight: Int,
        textScale: Float = 1f,
        maxWidthFraction: Float = DEFAULT_OVERLAY_MAX_WIDTH_FRACTION,
        maxHeightFraction: Float = DEFAULT_OVERLAY_MAX_HEIGHT_FRACTION,
    ): OverlayLayout {
        val density = context.resources.displayMetrics.density
        val widthBound = mediaWidth.coerceAtLeast(1)
        val heightBound = mediaHeight.coerceAtLeast(1)
        val minWidth = minOf((180 * textScale).toInt().coerceAtLeast(1), widthBound)
        val minHeight = minOf((72 * textScale).toInt().coerceAtLeast(1), heightBound)
        val maxWidth = (mediaWidth * maxWidthFraction).toInt().coerceIn(minWidth, widthBound)
        val maxHeight = (mediaHeight * maxHeightFraction).toInt().coerceIn(minHeight, heightBound)
        val baseTextSize = ((14f * density).coerceIn(18f, 30f) * textScale).coerceAtMost(72f)
        val basePadding = ((10f * density).coerceIn(12f, 22f) * textScale).coerceAtMost(54f)
        val baseGap = ((4f * density).coerceIn(4f, 8f) * textScale).coerceAtMost(18f)
        listOf(1f, 0.92f, 0.84f, 0.76f, 0.68f, 0.60f, 0.52f, 0.46f).forEach { shrink ->
            val textSize = baseTextSize * shrink
            val padding = basePadding * shrink
            val lineGap = baseGap * shrink
            val paint = overlayTextPaint(textSize)
            val textMaxWidth = (maxWidth - padding * 2f).coerceAtLeast(80f)
            measuredOverlayLayout(
                lines = lines,
                paint = paint,
                textMaxWidth = textMaxWidth,
                maxWidth = maxWidth,
                minWidth = minWidth,
                minHeight = minHeight,
                textSize = textSize,
                padding = padding,
                lineGap = lineGap,
            ).takeIf { it.height <= maxHeight }?.let { return it }
        }
        val textSize = baseTextSize * 0.46f
        val padding = basePadding * 0.46f
        val lineGap = baseGap * 0.46f
        val paint = overlayTextPaint(textSize)
        val textMaxWidth = (maxWidth - padding * 2f).coerceAtLeast(80f)
        return measuredOverlayLayout(
            lines = compactLocationLineForOverlay(lines, paint, textMaxWidth),
            paint = paint,
            textMaxWidth = textMaxWidth,
            maxWidth = maxWidth,
            minWidth = minWidth,
            minHeight = minHeight,
            textSize = textSize,
            padding = padding,
            lineGap = lineGap,
        )
    }

    private fun measuredOverlayLayout(
        lines: List<String>,
        paint: Paint,
        textMaxWidth: Float,
        maxWidth: Int,
        minWidth: Int,
        minHeight: Int,
        textSize: Float,
        padding: Float,
        lineGap: Float,
    ): OverlayLayout {
        val wrapped = wrapOverlayLinesByPaint(lines, paint, textMaxWidth)
        val contentWidth = wrapped.maxOfOrNull { paint.measureText(it) } ?: 0f
        val width = min(maxWidth, (contentWidth + padding * 2f).toInt().coerceAtLeast(minWidth))
        val height = (wrapped.size * (textSize + lineGap) - lineGap + padding * 2f).toInt()
            .coerceAtLeast(minHeight)
        return OverlayLayout(
            width = width,
            height = height,
            lines = wrapped,
            textSize = textSize,
            padding = padding,
            lineGap = lineGap,
        )
    }

    private fun compactLocationLineForOverlay(lines: List<String>, paint: Paint, maxWidth: Float): List<String> =
        lines.map { line ->
            if (!line.startsWith("Loc:", ignoreCase = true)) return@map line
            val normalized = line.replace(Regex("\\s+"), " ").trim()
            if (wrapOverlayLinesByPaint(listOf(normalized), paint, maxWidth).size <= 2) return@map normalized
            ellipsizeToWrappedLineCount(normalized, paint, maxWidth, maxLines = 2)
        }

    private fun ellipsizeToWrappedLineCount(line: String, paint: Paint, maxWidth: Float, maxLines: Int): String {
        val suffix = "..."
        var end = line.length
        while (end > "Loc: ".length) {
            val candidate = line.take(end).trimEnd() + suffix
            if (wrapOverlayLinesByPaint(listOf(candidate), paint, maxWidth).size <= maxLines) return candidate
            end--
        }
        return "Loc: $suffix"
    }

    private fun drawAuditOverlay(canvas: Canvas, layout: OverlayLayout) {
        val textPaint = overlayTextPaint(layout.textSize)
        val bgPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
            color = Color.argb(90, 0, 0, 0)
        }
        canvas.drawRoundRect(RectF(0f, 0f, layout.width.toFloat(), layout.height.toFloat()), 10f, 10f, bgPaint)
        var y = layout.padding - textPaint.fontMetrics.ascent
        layout.lines.forEach { line ->
            canvas.drawText(line, layout.padding, y, textPaint)
            y += layout.textSize + layout.lineGap
        }
    }

    private fun overlayTextPaint(textSize: Float): Paint =
        Paint(Paint.ANTI_ALIAS_FLAG).apply {
            color = Color.argb(255, 255, 255, 255)
            this.textSize = textSize
            setShadowLayer(2.5f, 0f, 1.5f, Color.argb(255, 0, 0, 0))
        }

    private fun wrapOverlayLinesByPaint(lines: List<String>, paint: Paint, maxWidth: Float): List<String> =
        lines.flatMap { line ->
            val words = line.split(' ').filter { it.isNotBlank() }.flatMap { splitWideToken(it, paint, maxWidth) }
            if (words.isEmpty()) {
                listOf(line)
            } else {
                buildList {
                    var current = ""
                    words.forEach { word ->
                        val candidate = if (current.isBlank()) word else "$current $word"
                        if (paint.measureText(candidate) <= maxWidth || current.isBlank()) {
                            current = candidate
                        } else {
                            add(current)
                            current = word
                        }
                    }
                    if (current.isNotBlank()) add(current)
                }
            }
        }

    private fun splitWideToken(token: String, paint: Paint, maxWidth: Float): List<String> {
        if (paint.measureText(token) <= maxWidth) return listOf(token)
        return buildList {
            var current = ""
            token.forEach { char ->
                val candidate = current + char
                if (paint.measureText(candidate) <= maxWidth || current.isBlank()) {
                    current = candidate
                } else {
                    add(current)
                    current = char.toString()
                }
            }
            if (current.isNotBlank()) add(current)
        }
    }

    private fun photoOverlayScale(width: Int, height: Int): Float {
        val longSide = max(width, height).coerceAtLeast(1)
        return (longSide / 1280f).coerceIn(1.25f, 2.4f)
    }

    private fun videoOverlayScale(width: Int, height: Int): Float {
        val longSide = max(width, height).coerceAtLeast(1)
        return (longSide / 1280f).coerceIn(0.62f, 1.0f)
    }

    private fun selectVideoBitrate(width: Int, height: Int, originalBitrate: Int?): Int {
        val longSide = max(width, height)
        val profile = when {
            longSide >= 1080 -> 1_600_000
            longSide >= 720 -> 1_000_000
            else -> 650_000
        }
        val capped = originalBitrate?.takeIf { it > 0 }?.let { (it * 0.35f).toInt() } ?: profile
        return min(profile, capped.coerceAtLeast(450_000))
    }

    private fun resizePhotoForProof(source: Bitmap): Bitmap {
        val longSide = max(source.width, source.height)
        if (longSide <= PHOTO_MAX_LONG_SIDE_PX) return source
        val scale = PHOTO_MAX_LONG_SIDE_PX.toFloat() / longSide.toFloat()
        val targetWidth = (source.width * scale).toInt().coerceAtLeast(1)
        val targetHeight = (source.height * scale).toInt().coerceAtLeast(1)
        return Bitmap.createScaledBitmap(source, targetWidth, targetHeight, true)
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

    private data class OverlayLayout(
        val width: Int,
        val height: Int,
        val lines: List<String>,
        val textSize: Float,
        val padding: Float,
        val lineGap: Float,
    )

    private companion object {
        private const val PHOTO_MAX_LONG_SIDE_PX = 1920
        private const val DEFAULT_OVERLAY_MAX_WIDTH_FRACTION = 0.86f
        private const val DEFAULT_OVERLAY_MAX_HEIGHT_FRACTION = 0.92f
        private const val VIDEO_OVERLAY_MAX_WIDTH_FRACTION = 0.56f
        private const val VIDEO_OVERLAY_MAX_HEIGHT_FRACTION = 0.34f
    }
}
