package sg.mesha.goatos.capture

import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.withContext
import java.io.File

/**
 * Binds [source] to a real, LIVE in-app camera recording for as long as the composable calling
 * this is part of the composition, and unbinds on dispose — no leaked camera session once the
 * capture surface leaves screen (performance/memory rule: release capture resources on
 * lifecycle stop). Call this once near the top of the screen that renders the `video_proof`
 * control (e.g. inside `SubmitScreen`'s host in `:app`); [ProofCaptureSource.captureVideo] then
 * works from the ViewModel without it ever touching camera/composition APIs directly.
 *
 * SOP-controlled capture (docs/mobile/proof-capture-sync-and-e2e.md): camera capture still shows
 * [InAppVideoRecorderOverlay] — a full-screen live CameraX preview + record button. When the
 * backend SOP permits shed-level gallery proof, [ProofCaptureSource.pickVideo] uses Android's
 * picker and immediately copies the selected clip into app-private storage before Room/GCS sync.
 */
@Composable
fun BindVideoCaptureSource(source: DelegatingProofCaptureSource) {
    val context = LocalContext.current
    var recorderPrompt by remember { mutableStateOf<ProofCapturePrompt?>(null) }
    val resultChannel = remember { Channel<CapturedVideo?>(capacity = 1) }
    // Carry only the raw picked Uri back on the main thread; the (potentially large) copy into
    // app-private storage runs off-main inside the suspend `pick` delegate below, so importing a
    // long clip from the gallery never blocks the UI thread (ANR).
    val pickerChannel = remember { Channel<Uri?>(capacity = 1) }
    val pickerLauncher = rememberLauncherForActivityResult(ActivityResultContracts.GetContent()) { uri: Uri? ->
        pickerChannel.trySend(uri)
    }

    DisposableEffect(source) {
        val bindToken = source.bind(
            launch = { prompt ->
                recorderPrompt = prompt
                resultChannel.receive()
            },
            pick = {
                pickerLauncher.launch("video/*")
                pickerChannel.receive()?.let { selected ->
                    withContext(Dispatchers.IO) {
                        copyPickedVideoToPrivateCache(context, selected, System.currentTimeMillis())
                    }
                }
            },
        )
        onDispose { source.unbind(bindToken) }
    }

    recorderPrompt?.let { prompt ->
        // This must be a real modal window. Rendering the recorder as a sibling before the
        // Scan/Submit screen puts the live preview *behind* that screen in Compose draw order:
        // the camera runs, but the operator still sees (and can touch) the RFID UI. A full-screen
        // Dialog gives capture exclusive visibility/input and guarantees the preview + controls
        // sit above the originating surface.
        Dialog(
            onDismissRequest = {}, // Back is handled inside the recorder as an explicit cancel.
            properties = DialogProperties(
                dismissOnBackPress = false,
                dismissOnClickOutside = false,
                usePlatformDefaultWidth = false,
                decorFitsSystemWindows = false,
            ),
        ) {
            InAppVideoRecorderOverlay(
                prompt = prompt,
                onResult = { result ->
                    if (recorderPrompt != null) {
                        recorderPrompt = null
                        resultChannel.trySend(result)
                    }
                },
            )
        }
    }
}

private fun copyPickedVideoToPrivateCache(
    context: android.content.Context,
    sourceUri: Uri,
    nowMs: Long,
): CapturedVideo? = runCatching {
    val dir = File(context.cacheDir, "proof-videos").apply { mkdirs() }
    val out = File(dir, "gallery-$nowMs.mp4")
    context.contentResolver.openInputStream(sourceUri)?.use { input ->
        out.outputStream().use { output -> input.copyTo(output) }
    } ?: return@runCatching null
    CapturedVideo(
        localUri = out.toURI().toString(),
        mimeType = context.contentResolver.getType(sourceUri) ?: "video/mp4",
        startedAtMs = nowMs,
        endedAtMs = nowMs,
        captureSource = "gallery_picker",
    )
}.getOrNull()
