package sg.mesha.goatos.capture

import android.content.Context
import androidx.activity.compose.BackHandler
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
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.compose.foundation.layout.Row
import androidx.compose.ui.unit.sp
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.LocalLifecycleOwner
import kotlinx.coroutines.delay
import sg.mesha.goatos.R
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import java.io.File

/**
 * LIVE, in-app camera video recording. Per-goat proof must use this path; shed-level proof may
 * also use the gallery picker when backend SOP explicitly allows it. This composable owns the
 * camera preview + record button end to end and writes straight to this app's OWN private storage
 * ([Context.filesDir], never [Context.getExternalFilesDir]/MediaStore).
 *
 * The camera + its [ProcessCameraProvider] binding are released the moment this composable
 * leaves composition ([DisposableEffect]) — no leaked camera session once the operator backs
 * out or the capture completes (performance/memory rule).
 */
@Composable
fun InAppVideoRecorderOverlay(
    prompt: ProofCapturePrompt = ProofCapturePrompt.VACCINATION,
    taskTitle: String? = null,
    onResult: (CapturedVideo?) -> Unit,
) {
    val context = LocalContext.current
    val lifecycleOwner = LocalLifecycleOwner.current
    val cameraSession = remember { ProofCameraSession() }
    val cameraUnavailableMessage = stringResource(R.string.proof_camera_unavailable)

    var videoCapture by remember { mutableStateOf<VideoCapture<Recorder>?>(null) }
    var activeRecording by remember { mutableStateOf<Recording?>(null) }
    var isRecording by remember { mutableStateOf(false) }
    var startedAtMs by remember { mutableStateOf(0L) }
    var cancelled by remember { mutableStateOf(false) }
    var resultDelivered by remember { mutableStateOf(false) }
    var cameraReady by remember { mutableStateOf(false) }
    var cameraError by remember { mutableStateOf<String?>(null) }
    var elapsedSeconds by remember { mutableIntStateOf(0) }

    fun deliver(result: CapturedVideo?) {
        if (resultDelivered) return
        resultDelivered = true
        onResult(result)
    }

    fun startRecording() {
        val capture = videoCapture ?: return
        if (isRecording) return
        val file = newCaptureFile(context)
        val output = FileOutputOptions.Builder(file).build()
        cancelled = false
        startedAtMs = System.currentTimeMillis()
        elapsedSeconds = 0
        activeRecording = capture.output
            .prepareRecording(context, output)
            // No audio: RECORD_AUDIO is not part of the mandatory capture-permission set.
            // The camera writes directly to app-private storage, so no storage permission
            // or gallery/file-picker surface is needed.
            .start(ContextCompat.getMainExecutor(context)) { event ->
                if (event is VideoRecordEvent.Finalize) {
                    isRecording = false
                    activeRecording = null
                    val endedAtMs = System.currentTimeMillis()
                    if (cancelled) {
                        file.delete()
                        deliver(null)
                    } else if (!event.hasError()) {
                        deliver(
                            CapturedVideo(
                                localUri = file.toURI().toString(),
                                startedAtMs = startedAtMs,
                                endedAtMs = endedAtMs,
                            ),
                        )
                    } else {
                        file.delete()
                        deliver(null)
                    }
                }
            }
        isRecording = true
    }

    fun finishRecording() {
        activeRecording?.stop()
    }

    fun cancelRecording() {
        cancelled = true
        val recording = activeRecording
        if (recording == null) {
            deliver(null)
        } else {
            recording.stop()
        }
    }

    BackHandler(onBack = ::cancelRecording)

    LaunchedEffect(isRecording, startedAtMs) {
        while (isRecording) {
            elapsedSeconds = ((System.currentTimeMillis() - startedAtMs) / 1_000L).toInt().coerceAtLeast(0)
            delay(250)
        }
    }

    DisposableEffect(Unit) {
        onDispose {
            // Release the recorder + unbind the camera the instant this leaves composition —
            // an operator backing out mid-record must not leave the camera session running.
            cancelled = true
            activeRecording?.stop()
            activeRecording = null
            cameraSession.release()
        }
    }

    Box(
        Modifier
            .fillMaxSize()
            .background(Color.Black)
            .windowInsetsPadding(WindowInsets.safeDrawing),
    ) {
        androidx.compose.ui.viewinterop.AndroidView(
            factory = { ctx ->
                PreviewView(ctx).also { view ->
                    view.scaleType = PreviewView.ScaleType.FILL_CENTER
                    cameraSession.bind(
                        context = ctx,
                        previewView = view,
                        lifecycleOwner = lifecycleOwner,
                        onBound = { capture ->
                            videoCapture = capture
                            cameraReady = true
                            cameraError = null
                        },
                        onError = {
                            cameraReady = false
                            cameraError = cameraUnavailableMessage
                        },
                    )
                }
            },
            onRelease = { view ->
                cameraReady = false
                videoCapture = null
                cameraSession.release()
            },
            modifier = Modifier.fillMaxSize(),
        )
        RecorderHeader(
            prompt = prompt,
            taskTitle = taskTitle,
            isRecording = isRecording,
            elapsedSeconds = elapsedSeconds,
            cameraError = cameraError,
            modifier = Modifier.align(Alignment.TopCenter),
        )
        Row(
            Modifier
                .fillMaxWidth()
                .align(Alignment.BottomCenter)
                .padding(24.dp),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            RecorderControl(
                icon = MeshaIcons.Close,
                contentDescription = stringResource(R.string.proof_camera_cancel),
                onClick = ::cancelRecording,
            )
            RecordButton(
                isRecording = isRecording,
                enabled = cameraReady,
                onClick = { if (isRecording) finishRecording() else startRecording() },
            )
            Spacer(Modifier.size(48.dp)) // balances the cancel control without adding an action.
        }
    }
}

@Composable
private fun RecorderHeader(
    prompt: ProofCapturePrompt,
    taskTitle: String?,
    isRecording: Boolean,
    elapsedSeconds: Int,
    cameraError: String?,
    modifier: Modifier = Modifier,
) {
    val copy = recorderCopyResources(prompt)
    Column(
        modifier = modifier
            .fillMaxWidth()
            .background(Color.Black.copy(alpha = 0.68f))
            .padding(horizontal = 20.dp, vertical = 16.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            text = recorderHeaderTitle(taskTitle, stringResource(copy.title)),
            color = MeshaColors.Ink,
            fontSize = 18.sp,
            fontWeight = androidx.compose.ui.text.font.FontWeight.Bold,
        )
        Spacer(Modifier.height(4.dp))
        Text(
            text = when {
                cameraError != null -> cameraError
                isRecording -> stringResource(R.string.proof_camera_recording_time, elapsedSeconds)
                else -> stringResource(copy.instruction)
            },
            color = if (cameraError != null) MeshaColors.Danger else MeshaColors.Ink,
            fontSize = 13.sp,
        )
    }
}

internal data class RecorderCopyResources(
    val title: Int,
    val instruction: Int,
)

internal fun recorderHeaderTitle(taskTitle: String?, fallbackTitle: String): String =
    taskTitle?.trim()?.takeIf(String::isNotEmpty) ?: fallbackTitle

internal fun recorderCopyResources(prompt: ProofCapturePrompt): RecorderCopyResources = when (prompt) {
    ProofCapturePrompt.VACCINATION -> RecorderCopyResources(
        R.string.proof_camera_title,
        R.string.proof_camera_instruction,
    )
    ProofCapturePrompt.BIRTH -> RecorderCopyResources(
        R.string.proof_camera_birth_title,
        R.string.proof_camera_birth_instruction,
    )
    ProofCapturePrompt.DEATH -> RecorderCopyResources(
        R.string.proof_camera_death_title,
        R.string.proof_camera_death_instruction,
    )
    ProofCapturePrompt.POST_MORTEM -> RecorderCopyResources(
        R.string.proof_camera_post_mortem_title,
        R.string.proof_camera_post_mortem_instruction,
    )
    ProofCapturePrompt.SHIFTING -> RecorderCopyResources(
        R.string.proof_camera_shifting_title,
        R.string.proof_camera_shifting_instruction,
    )
    ProofCapturePrompt.FEED_DISTRIBUTION -> RecorderCopyResources(
        R.string.proof_camera_feed_distribution_title,
        R.string.proof_camera_feed_distribution_instruction,
    )
    ProofCapturePrompt.WATER_DISTRIBUTION -> RecorderCopyResources(
        R.string.proof_camera_water_distribution_title,
        R.string.proof_camera_water_distribution_instruction,
    )
    ProofCapturePrompt.FEED_PACKING -> RecorderCopyResources(
        R.string.proof_camera_feed_packing_title,
        R.string.proof_camera_feed_packing_instruction,
    )
}

@Composable
private fun RecorderControl(
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    contentDescription: String,
    onClick: () -> Unit,
) {
    IconButton(
        onClick = onClick,
        modifier = Modifier
            .minimumInteractiveComponentSize()
            .size(48.dp)
            .clip(CircleShape)
            .background(MeshaColors.Surf3),
    ) {
        Icon(icon, contentDescription = contentDescription, tint = MeshaColors.Ink)
    }
}

@Composable
private fun RecordButton(isRecording: Boolean, enabled: Boolean, onClick: () -> Unit) {
    val actionDescription = stringResource(
        if (isRecording) R.string.proof_camera_stop_recording else R.string.proof_camera_start_recording,
    )
    val recordingState = stringResource(
        if (isRecording) R.string.proof_camera_state_recording else R.string.proof_camera_state_ready,
    )
    Box(
        Modifier
            .minimumInteractiveComponentSize()
            .size(72.dp)
            .clip(CircleShape)
            .background(Color.Black.copy(alpha = 0.56f))
            .border(3.dp, if (enabled) MeshaColors.Ink else MeshaColors.Muted, CircleShape)
            .semantics {
                contentDescription = actionDescription
                stateDescription = recordingState
                role = Role.Button
            }
            .clickable(enabled = enabled, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Box(
            Modifier
                .size(if (isRecording) 26.dp else 56.dp)
                .clip(if (isRecording) androidx.compose.foundation.shape.RoundedCornerShape(7.dp) else CircleShape)
                .background(if (enabled) MeshaColors.Danger else MeshaColors.Muted),
        )
    }
}

/** Owns exactly the CameraX use cases bound to the Compose-hosted [PreviewView].
 * bindToLifecycle stops capture while the Activity is stopped; [release] handles the more common
 * in-Activity route/dialog dismissal and invalidates any still-pending provider callback. */
private class ProofCameraSession {
    private var generation = 0
    private var provider: ProcessCameraProvider? = null
    private var preview: Preview? = null
    private var capture: VideoCapture<Recorder>? = null

    fun bind(
        context: Context,
        previewView: PreviewView,
        lifecycleOwner: androidx.lifecycle.LifecycleOwner,
        onBound: (VideoCapture<Recorder>) -> Unit,
        onError: (Throwable) -> Unit,
    ) {
        val bindGeneration = ++generation
        val providerFuture = ProcessCameraProvider.getInstance(context)
        providerFuture.addListener(
            {
                if (bindGeneration != generation) return@addListener
                runCatching {
                    val cameraProvider = providerFuture.get()
                    val cameraPreview = Preview.Builder().build().also {
                        it.surfaceProvider = previewView.surfaceProvider
                    }
                    val recorder = Recorder.Builder().setQualitySelector(QualitySelector.from(Quality.HD)).build()
                    val videoCapture = VideoCapture.withOutput(recorder)
                    cameraProvider.unbindAll()
                    cameraProvider.bindToLifecycle(
                        lifecycleOwner,
                        CameraSelector.DEFAULT_BACK_CAMERA,
                        cameraPreview,
                        videoCapture,
                    )
                    provider = cameraProvider
                    preview = cameraPreview
                    capture = videoCapture
                    onBound(videoCapture)
                }.onFailure(onError)
            },
            ContextCompat.getMainExecutor(context),
        )
    }

    fun release() {
        generation += 1
        val currentProvider = provider
        preview?.let { useCase -> runCatching { currentProvider?.unbind(useCase) } }
        capture?.let { useCase -> runCatching { currentProvider?.unbind(useCase) } }
        preview = null
        capture = null
        provider = null
    }
}

/** App-PRIVATE destination (`Context.filesDir`, never external/MediaStore) — invisible to the
 *  gallery and any other app, so a captured video can never be later swapped for a different
 *  file at the same path (anti-fraud). */
private fun newCaptureFile(context: Context): File {
    val dir = File(context.filesDir, "captures").apply { mkdirs() }
    return File(dir, "proof-${System.currentTimeMillis()}.mp4")
}
