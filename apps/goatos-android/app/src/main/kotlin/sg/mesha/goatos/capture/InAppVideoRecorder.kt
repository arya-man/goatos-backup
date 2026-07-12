package sg.mesha.goatos.capture

import android.content.Context
import androidx.camera.core.CameraSelector
import androidx.camera.core.Preview
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.video.FileOutputOptions
import androidx.camera.video.Quality
import androidx.camera.video.QualitySelector
import androidx.camera.video.Recorder
import androidx.camera.video.Recording
import androidx.camera.video.VideoCapture
import androidx.camera.video.VideoRecordEvent
import androidx.camera.view.PreviewView
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.compose.ui.unit.dp
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.size
import androidx.compose.ui.unit.sp
import androidx.core.content.ContextCompat
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import java.io.File

/**
 * LIVE, in-app camera video recording — the ONLY way to produce a `video_proof` capture
 * (docs/mobile/proof-capture-sync-and-e2e.md "Camera-only capture", anti-fraud business rule).
 * No gallery import, no `ACTION_GET_CONTENT`/`ACTION_PICK`, no chooser: this composable owns
 * the camera preview + record button end to end and writes straight to this app's OWN private
 * storage ([Context.filesDir], never [Context.getExternalFilesDir]/MediaStore) so the resulting
 * file can never be swapped for an old/unrelated recording.
 *
 * The camera + its [ProcessCameraProvider] binding are released the moment this composable
 * leaves composition ([DisposableEffect]) — no leaked camera session once the operator backs
 * out or the capture completes (performance/memory rule).
 */
@Composable
fun InAppVideoRecorderOverlay(onResult: (CapturedVideo?) -> Unit) {
    val context = LocalContext.current
    val lifecycleOwner = LocalLifecycleOwner.current

    var videoCapture by remember { mutableStateOf<VideoCapture<Recorder>?>(null) }
    var activeRecording by remember { mutableStateOf<Recording?>(null) }
    var isRecording by remember { mutableStateOf(false) }
    var startedAtMs by remember { mutableStateOf(0L) }

    fun startRecording() {
        val capture = videoCapture ?: return
        if (isRecording) return
        val file = newCaptureFile(context)
        val output = FileOutputOptions.Builder(file).build()
        startedAtMs = System.currentTimeMillis()
        activeRecording = capture.output
            .prepareRecording(context, output)
            // No audio: RECORD_AUDIO is not part of the mandatory capture-permission set
            // (docs/mobile/proof-capture-sync-and-e2e.md §4 — camera/Bluetooth/location/
            // notifications/storage only). Silent proof video is sufficient for this pass.
            .start(ContextCompat.getMainExecutor(context)) { event ->
                if (event is VideoRecordEvent.Finalize) {
                    isRecording = false
                    val endedAtMs = System.currentTimeMillis()
                    if (!event.hasError()) {
                        onResult(
                            CapturedVideo(
                                localUri = file.toURI().toString(),
                                startedAtMs = startedAtMs,
                                endedAtMs = endedAtMs,
                            ),
                        )
                    } else {
                        onResult(null)
                    }
                }
            }
        isRecording = true
    }

    fun stopRecording() {
        activeRecording?.stop()
        activeRecording = null
    }

    DisposableEffect(Unit) {
        onDispose {
            // Release the recorder + unbind the camera the instant this leaves composition —
            // an operator backing out mid-record must not leave the camera session running.
            activeRecording?.stop()
            activeRecording = null
        }
    }

    Box(Modifier.fillMaxSize().background(Color.Black)) {
        androidx.compose.ui.viewinterop.AndroidView(
            factory = { ctx -> buildPreviewView(ctx, lifecycleOwner) { capture -> videoCapture = capture } },
            modifier = Modifier.fillMaxSize(),
        )
        Row(
            Modifier
                .fillMaxWidth()
                .align(Alignment.BottomCenter)
                .padding(24.dp),
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            RecorderControl(
                icon = MeshaIcons.Close,
                onClick = {
                    stopRecording()
                    if (!isRecording) onResult(null)
                },
            )
            RecordButton(isRecording = isRecording, onClick = { if (isRecording) stopRecording() else startRecording() })
            Box(Modifier.size(44.dp)) // spacer to balance the row
        }
        if (isRecording) {
            Text(
                "Recording…",
                color = MeshaColors.Danger,
                fontSize = 13.sp,
                modifier = Modifier.align(Alignment.TopCenter).padding(top = 32.dp),
            )
        }
    }
}

@Composable
private fun RecorderControl(icon: androidx.compose.ui.graphics.vector.ImageVector, onClick: () -> Unit) {
    Box(
        Modifier
            .size(44.dp)
            .clip(CircleShape)
            .background(MeshaColors.Surf3)
            .clickable(onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Icon(icon, contentDescription = "Cancel", tint = MeshaColors.Ink)
    }
}

@Composable
private fun RecordButton(isRecording: Boolean, onClick: () -> Unit) {
    Box(
        Modifier
            .size(64.dp)
            .clip(CircleShape)
            .background(if (isRecording) MeshaColors.Danger else MeshaColors.Brand)
            .clickable(onClick = onClick),
    )
}

/** Binds camera Preview + a [Recorder]-backed [VideoCapture] to [lifecycleOwner]; hands the
 *  bound [VideoCapture] back via [onBound] once the async [ProcessCameraProvider] resolves. */
private fun buildPreviewView(
    context: Context,
    lifecycleOwner: androidx.lifecycle.LifecycleOwner,
    onBound: (VideoCapture<Recorder>) -> Unit,
): PreviewView {
    val previewView = PreviewView(context)
    val providerFuture = ProcessCameraProvider.getInstance(context)
    providerFuture.addListener(
        {
            val provider = providerFuture.get()
            val preview = Preview.Builder().build().also { it.surfaceProvider = previewView.surfaceProvider }
            val recorder = Recorder.Builder().setQualitySelector(QualitySelector.from(Quality.HD)).build()
            val capture = VideoCapture.withOutput(recorder)
            provider.unbindAll()
            provider.bindToLifecycle(lifecycleOwner, CameraSelector.DEFAULT_BACK_CAMERA, preview, capture)
            onBound(capture)
        },
        ContextCompat.getMainExecutor(context),
    )
    return previewView
}

/** App-PRIVATE destination (`Context.filesDir`, never external/MediaStore) — invisible to the
 *  gallery and any other app, so a captured video can never be later swapped for a different
 *  file at the same path (anti-fraud). */
private fun newCaptureFile(context: Context): File {
    val dir = File(context.filesDir, "captures").apply { mkdirs() }
    return File(dir, "proof-${System.currentTimeMillis()}.mp4")
}
