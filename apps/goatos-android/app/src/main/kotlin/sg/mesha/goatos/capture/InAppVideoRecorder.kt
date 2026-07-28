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
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
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
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.LocalLifecycleOwner
import sg.mesha.goatos.R
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
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
    captureContext: ProofCaptureContext? = null,
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

    LaunchedEffect(cameraReady, isRecording, resultDelivered) {
        if (cameraReady && !isRecording && !resultDelivered) {
            startRecording()
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

    // Camera preview is edge-to-edge; only overlay content pads for system bars —
    // insets on the root would letterbox the preview inside black bars.
    Box(
        Modifier
            .fillMaxSize()
            .background(Color.Black),
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
            onRelease = {
                cameraReady = false
                videoCapture = null
                cameraSession.release()
            },
            modifier = Modifier.fillMaxSize(),
        )
        // Directional scrims keep the live preview clear while making overlay text legible.
        Box(
            Modifier
                .fillMaxWidth()
                .height(200.dp)
                .align(Alignment.TopCenter)
                .background(
                    Brush.verticalGradient(
                        listOf(Color.Black.copy(alpha = 0.62f), Color.Transparent),
                    ),
                ),
        )
        Box(
            Modifier
                .fillMaxWidth()
                .height(320.dp)
                .align(Alignment.BottomCenter)
                .background(
                    Brush.verticalGradient(
                        listOf(Color.Transparent, Color.Black.copy(alpha = 0.78f)),
                    ),
                ),
        )
        Column(
            modifier = Modifier
                .align(Alignment.TopCenter)
                .windowInsetsPadding(WindowInsets.safeDrawing)
                .fillMaxWidth()
                .padding(horizontal = 16.dp, vertical = 12.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            ProofCardHeader(
                cameraError = cameraError,
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(10.dp))
            RecordingPills(isRecording = isRecording)
        }
        Column(
            modifier = Modifier
                .align(Alignment.BottomCenter)
                .windowInsetsPadding(WindowInsets.safeDrawing)
                .fillMaxWidth()
                .padding(horizontal = 16.dp)
                .padding(bottom = 16.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            CaptureSubjectPanel(
                context = captureContext ?: proofCaptureContext(prompt, taskTitle),
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(12.dp))
            Text(
                text = stringResource(R.string.proof_camera_instruction),
                color = MeshaColors.Ink.copy(alpha = 0.82f),
                style = MeshaType.caption,
            )
            Spacer(Modifier.height(12.dp))
            StopRecordingButton(
                isRecording = isRecording,
                enabled = cameraReady,
                onClick = { if (isRecording) finishRecording() else startRecording() },
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}

data class ProofCaptureContext(
    val title: String,
    val primaryTag: String,
    val secondaryTag: String? = null,
    val workLabel: String = "",
)

@Composable
private fun ProofCardHeader(cameraError: String?, modifier: Modifier = Modifier) {
    Row(
        modifier = modifier
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf.copy(alpha = 0.88f))
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            Modifier
                .size(38.dp)
                .clip(RoundedCornerShape(11.dp))
                .background(MeshaColors.BrandTint),
            contentAlignment = Alignment.Center,
        ) {
            Icon(MeshaIcons.Video, contentDescription = null, tint = MeshaColors.BrandD)
        }
        Spacer(Modifier.width(12.dp))
        Column {
            Text(
                text = if (cameraError == null) stringResource(R.string.proof_camera_recording_title) else stringResource(R.string.proof_camera_title),
                color = MeshaColors.Ink,
                style = MeshaType.screenTitle,
            )
            Text(
                text = cameraError ?: stringResource(R.string.proof_camera_auto_opened),
                color = if (cameraError == null) MeshaColors.BrandD else MeshaColors.Danger,
                style = MeshaType.caption,
            )
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun CaptureSubjectPanel(
    context: ProofCaptureContext?,
    modifier: Modifier = Modifier,
) {
    val primary = context?.primaryTag?.takeIf { it.isNotBlank() } ?: return
    val secondary = context.secondaryTag?.takeIf { it.isNotBlank() }
    val workLabels = context.workLabel
        .split("·", ",")
        .map { it.trim() }
        .filter { it.isNotBlank() }
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf.copy(alpha = 0.92f))
            .border(1.dp, MeshaColors.Brand.copy(alpha = 0.8f), RoundedCornerShape(18.dp))
            .padding(horizontal = 18.dp, vertical = 16.dp),
    ) {
        Text(
            text = context.title.takeIf { it.isNotBlank() } ?: stringResource(R.string.proof_camera_title),
            color = MeshaColors.BrandD,
            style = MeshaType.caption,
        )
        Spacer(Modifier.height(8.dp))
        Text(
            text = primary,
            color = MeshaColors.Ink,
            style = MeshaType.headerTitle,
        )
        if (secondary != null) {
            Spacer(Modifier.height(8.dp))
            InfoChip(text = "2 tags", tone = ChipTone.Neutral)
            Spacer(Modifier.height(6.dp))
            Text(
                text = secondary,
                color = MeshaColors.Muted,
                style = MeshaType.bodyStrong,
            )
        }
        if (workLabels.isNotEmpty()) {
            Spacer(Modifier.height(12.dp))
            FlowRow(
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                workLabels.forEach { label ->
                    InfoChip(text = label, tone = ChipTone.Work)
                }
            }
        }
    }
}

internal data class RecorderCopyResources(
    val title: Int,
    val instruction: Int,
)

internal fun recorderHeaderTitle(taskTitle: String?, fallbackTitle: String): String =
    taskTitle?.trim()?.takeIf(String::isNotEmpty) ?: fallbackTitle

@Composable
private fun proofCaptureContext(prompt: ProofCapturePrompt, taskTitle: String?): ProofCaptureContext {
    val copy = recorderCopyResources(prompt)
    return ProofCaptureContext(
        title = recorderHeaderTitle(taskTitle, stringResource(copy.title)),
        primaryTag = stringResource(copy.instruction),
    )
}

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
    ProofCapturePrompt.SHIFTING_FEED_GIVEN -> RecorderCopyResources(
        R.string.proof_camera_shifting_feed_given_title,
        R.string.proof_camera_shifting_feed_given_instruction,
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
    ProofCapturePrompt.FEED_TRANSPORT -> RecorderCopyResources(
        R.string.proof_camera_feed_transport_title,
        R.string.proof_camera_feed_transport_instruction,
    )
}

@Composable
private fun RecordingPills(
    isRecording: Boolean,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        InfoChip(text = stringResource(R.string.proof_camera_live_proof), tone = ChipTone.Work)
        if (isRecording) InfoChip(text = stringResource(R.string.proof_camera_rec), tone = ChipTone.Danger)
    }
}

@Composable
private fun InfoChip(text: String, tone: ChipTone) {
    val background = when (tone) {
        ChipTone.Work -> MeshaColors.Brand.copy(alpha = 0.22f)
        ChipTone.Neutral -> MeshaColors.Surf3
        ChipTone.Danger -> MeshaColors.Danger.copy(alpha = 0.2f)
    }
    val foreground = when (tone) {
        ChipTone.Work -> MeshaColors.BrandD
        ChipTone.Neutral -> MeshaColors.Muted
        ChipTone.Danger -> MeshaColors.Danger
    }
    Box(
        Modifier
            .clip(RoundedCornerShape(10.dp))
            .background(background)
            .padding(horizontal = 10.dp, vertical = 5.dp),
    ) {
        Text(
            text = text,
            color = foreground,
            style = MeshaType.pill,
        )
    }
}

private enum class ChipTone {
    Work,
    Neutral,
    Danger,
}

@Composable
private fun StopRecordingButton(
    isRecording: Boolean,
    enabled: Boolean,
    modifier: Modifier = Modifier,
    onClick: () -> Unit,
) {
    val actionDescription = stringResource(
        if (isRecording) R.string.proof_camera_stop_recording else R.string.proof_camera_start_recording,
    )
    val recordingState = stringResource(
        if (isRecording) R.string.proof_camera_state_recording else R.string.proof_camera_state_ready,
    )
    Button(
        onClick = onClick,
        enabled = enabled,
        colors = ButtonDefaults.buttonColors(
            containerColor = MeshaColors.Brand,
            contentColor = MeshaColors.OnBrand,
            disabledContainerColor = MeshaColors.Surf3,
            disabledContentColor = MeshaColors.Faint,
        ),
        shape = RoundedCornerShape(20.dp),
        modifier = modifier
            .fillMaxWidth()
            .height(58.dp)
            .semantics {
                contentDescription = actionDescription
                stateDescription = recordingState
                role = Role.Button
            },
    ) {
        Icon(MeshaIcons.Video, contentDescription = null)
        Spacer(Modifier.width(10.dp))
        Text(
            text = if (isRecording) stringResource(R.string.proof_camera_stop_label) else stringResource(R.string.proof_camera_start_label),
            style = MeshaType.button,
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
