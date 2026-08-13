package sg.mesha.goatos

import android.net.Uri
import android.content.ContentValues
import android.content.Context
import android.os.Build
import android.os.Bundle
import android.provider.MediaStore
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.media3.common.MediaItem
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.PlayerView
import dagger.hilt.android.AndroidEntryPoint
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import sg.mesha.goatos.capture.AppProofMediaProcessor
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.capture.InAppVideoRecorderOverlay
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCapturePrompt
import sg.mesha.goatos.core.data.capture.ProofMediaProcessingRequest
import sg.mesha.goatos.core.designsystem.locale.ProvideAppLocale
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import java.io.File
import java.net.URI
import javax.inject.Inject

@AndroidEntryPoint
class ProofMediaSampleActivity : ComponentActivity() {
    @Inject
    lateinit var mediaProcessor: AppProofMediaProcessor

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            GoatOsTheme {
                ProvideAppLocale {
                    ProofMediaSampleScreen(mediaProcessor)
                }
            }
        }
    }
}

@Composable
private fun ProofMediaSampleScreen(mediaProcessor: AppProofMediaProcessor) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var recording by remember { mutableStateOf(false) }
    var processing by remember { mutableStateOf(false) }
    var status by remember { mutableStateOf("Ready") }
    var original by remember { mutableStateOf<CapturedVideo?>(null) }
    var processedUri by remember { mutableStateOf<String?>(null) }
    var sizeSummary by remember { mutableStateOf("Original: --\nCompressed: --\nReduction: --") }
    var stats by remember { mutableStateOf("") }

    Box(Modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(18.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Text("Proof media sample", color = MeshaColors.Ink, style = MaterialTheme.typography.headlineMedium)
            Text(status, color = MeshaColors.Muted, style = MaterialTheme.typography.bodyMedium)
            Text(sizeSummary, color = MeshaColors.Ink, style = MaterialTheme.typography.titleMedium)
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Button(enabled = !recording && !processing, onClick = {
                    status = "Recording..."
                    recording = true
                }) {
                    Text("Record")
                }
                OutlinedButton(enabled = !recording && !processing, onClick = {
                    original = null
                    processedUri = null
                    sizeSummary = "Original: --\nCompressed: --\nReduction: --"
                    stats = ""
                    status = "Ready"
                }) {
                    Text("Reset")
                }
            }
            if (processing) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                    CircularProgressIndicator(modifier = Modifier.size(22.dp), strokeWidth = 2.dp)
                    Text("Compressing + burning overlay", color = MeshaColors.Ink)
                }
            }
            stats.takeIf { it.isNotBlank() }?.let {
                Text(it, color = MeshaColors.Ink, style = MaterialTheme.typography.bodySmall)
            }
            original?.localUri?.let {
                Text("Original", color = MeshaColors.Muted)
                VideoPreview(it)
            }
            processedUri?.let {
                Text("Processed: overlay must be bottom-right", color = MeshaColors.Muted)
                VideoPreview(it)
            }
        }

        if (recording) {
            InAppVideoRecorderOverlay(
                captureContext = ProofCaptureContext(
                    title = "Proof sample",
                    primaryTag = "Overlay burn test",
                    secondaryTag = "Hold phone at a bright object",
                    workLabel = "Sample",
                    prompt = ProofCapturePrompt.FEED_DISTRIBUTION,
                    headerTitle = "Sample proof video",
                ),
                onResult = { captured ->
                    recording = false
                    if (captured == null) {
                        status = "Capture cancelled"
                        return@InAppVideoRecorderOverlay
                    }
                    original = captured
                    processing = true
                    status = "Captured. Processing..."
                    scope.launch {
                        val started = System.currentTimeMillis()
                        runCatching {
                            withContext(Dispatchers.Default) {
                                mediaProcessor.process(
                                    ProofMediaProcessingRequest(
                                        proofId = "sample-${System.currentTimeMillis()}",
                                        taskId = "sample-proof-media",
                                        fieldKey = "sample_video",
                                        subjectType = "sample",
                                        subjectId = "sample-subject",
                                        rfidTag = "RFID-SAMPLE-123",
                                        originalUri = captured.localUri,
                                        mimeType = captured.mimeType,
                                        capturedStartMs = captured.startedAtMs,
                                        capturedEndMs = captured.endedAtMs,
                                        capturedByPrincipalId = "sample-operator",
                                        caption = "Sample overlay burn",
                                    ),
                                )
                            }
                        }.onSuccess { result ->
                            val originalBytes = fileSize(captured.localUri)
                            val processedBytes = fileSize(result.outputUri)
                            val reduction = if (originalBytes > 0L && processedBytes > 0L) {
                                100 - ((processedBytes * 100) / originalBytes)
                            } else {
                                0
                            }
                            val originalGalleryUri = withContext(Dispatchers.IO) {
                                saveVideoToGallery(
                                    context = context,
                                    sourceUriText = captured.localUri,
                                    displayName = "goatos_sample_original_${System.currentTimeMillis()}.mp4",
                                )
                            }
                            val processedGalleryUri = withContext(Dispatchers.IO) {
                                saveVideoToGallery(
                                    context = context,
                                    sourceUriText = result.outputUri,
                                    displayName = "goatos_sample_processed_${System.currentTimeMillis()}.mp4",
                                )
                            }
                            processedUri = result.outputUri
                            status = "Processed OK"
                            sizeSummary = buildString {
                                appendLine("Original: ${formatBytes(originalBytes)} ($originalBytes bytes)")
                                appendLine("Compressed: ${formatBytes(processedBytes)} ($processedBytes bytes)")
                                append("Reduction: $reduction%")
                            }
                            stats = buildString {
                                appendLine("Elapsed ms: ${System.currentTimeMillis() - started}")
                                appendLine("Original Gallery: $originalGalleryUri")
                                appendLine("Processed Gallery: $processedGalleryUri")
                                appendLine("Output: ${result.outputUri}")
                            }
                        }.onFailure { error ->
                            status = "Processing failed: ${error::class.java.simpleName} ${error.message.orEmpty()}"
                        }
                        processing = false
                    }
                },
            )
        }
    }
}

@Composable
@UnstableApi
private fun VideoPreview(uriText: String) {
    val context = LocalContext.current
    val player = remember(uriText) {
        ExoPlayer.Builder(context).build().apply {
            setMediaItem(MediaItem.fromUri(Uri.parse(uriText)))
            prepare()
            playWhenReady = false
        }
    }
    DisposableEffect(player) {
        onDispose { player.release() }
    }
    AndroidView(
        factory = { ctx ->
            PlayerView(ctx).apply {
                this.player = player
                useController = true
            }
        },
        modifier = Modifier
            .fillMaxWidth()
            .aspectRatio(9f / 16f)
            .clip(RoundedCornerShape(8.dp))
            .background(Color.Black),
    )
}

private fun fileSize(uriText: String): Long =
    // exception:exempt debug sample size probe falls back to 0 bytes for display only.
    runCatching {
        val uri = Uri.parse(uriText)
        when (uri.scheme) {
            "file" -> File(URI(uriText)).length()
            null, "" -> File(uriText).length()
            else -> 0L
        }
    }.getOrDefault(0L)

private fun formatBytes(bytes: Long): String =
    when {
        bytes >= 1_048_576L -> "%.2f MB".format(bytes / 1_048_576.0)
        bytes >= 1024L -> "%.1f KB".format(bytes / 1024.0)
        else -> "$bytes B"
    }

private fun saveVideoToGallery(context: Context, sourceUriText: String, displayName: String): Uri {
    val values = ContentValues().apply {
        put(MediaStore.Video.Media.DISPLAY_NAME, displayName)
        put(MediaStore.Video.Media.MIME_TYPE, "video/mp4")
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            put(MediaStore.Video.Media.RELATIVE_PATH, "Movies/GoatOS Proof Sample")
            put(MediaStore.Video.Media.IS_PENDING, 1)
        }
    }
    val resolver = context.contentResolver
    val destination = checkNotNull(resolver.insert(MediaStore.Video.Media.EXTERNAL_CONTENT_URI, values)) {
        "Gallery insert failed"
    }
    try {
        resolver.openOutputStream(destination)?.use { output ->
            openUriInput(context, sourceUriText).use { input -> input.copyTo(output) }
        } ?: error("Gallery output stream failed")
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            values.clear()
            values.put(MediaStore.Video.Media.IS_PENDING, 0)
            resolver.update(destination, values, null, null)
        }
        return destination
    } catch (error: Throwable) {
        resolver.delete(destination, null, null)
        throw error
    }
}

private fun openUriInput(context: Context, uriText: String) =
    when (val uri = Uri.parse(uriText)) {
        else -> when (uri.scheme) {
            "content" -> checkNotNull(context.contentResolver.openInputStream(uri)) { "Cannot open $uriText" }
            "file" -> File(URI(uriText)).inputStream()
            null, "" -> File(uriText).inputStream()
            else -> error("Unsupported uri scheme ${uri.scheme}")
        }
    }
