package sg.mesha.goatos.capture

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import androidx.activity.compose.BackHandler
import androidx.camera.core.Camera
import androidx.camera.core.CameraSelector
import androidx.camera.core.Preview
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.video.FileOutputOptions
import androidx.camera.video.FallbackStrategy
import androidx.camera.video.Quality
import androidx.camera.video.QualitySelector
import androidx.camera.video.Recorder
import androidx.camera.video.Recording
import androidx.camera.video.VideoCapture
import androidx.camera.video.VideoRecordEvent
import androidx.camera.view.PreviewView
import androidx.camera.view.PreviewView.StreamState
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
import androidx.compose.material3.IconButton
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
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
import kotlinx.coroutines.delay
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import sg.mesha.goatos.BuildConfig
import sg.mesha.goatos.R
import sg.mesha.goatos.core.data.capture.ProofArtifactValidator
import sg.mesha.goatos.core.data.capture.FileSystemProofArtifactValidator
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
    captureContext: ProofCaptureContext? = null,
    onResult: (CapturedVideo?) -> Unit,
    onCameraEvent: (String) -> Unit = {},
    // MEDIUM: Accept validator as dependency instead of constructing inline
    artifactValidator: ProofArtifactValidator = remember { FileSystemProofArtifactValidator() },
    onValidationFailure: (ProofArtifactValidator.ValidationResult) -> Unit = {},
) {
    val context = LocalContext.current
    val lifecycleOwner = LocalLifecycleOwner.current
    val cameraSession = remember { ProofCameraSession() }
    val cameraUnavailableMessage = stringResource(R.string.proof_camera_unavailable)
    val previewTimeoutMessage = stringResource(R.string.proof_camera_preview_timeout)
    val invalidVideoMessage = stringResource(R.string.proof_capture_invalid_video)
    val retryLabel = stringResource(R.string.proof_camera_retry)

    var videoCapture by remember { mutableStateOf<VideoCapture<Recorder>?>(null) }
    var boundCamera by remember { mutableStateOf<Camera?>(null) }
    var torchEnabled by remember { mutableStateOf(false) }
    var activeRecording by remember { mutableStateOf<Recording?>(null) }
    var isRecording by remember { mutableStateOf(false) }
    var startedAtMs by remember { mutableStateOf(0L) }
    var elapsedRecordingSeconds by remember { mutableStateOf(0L) }
    var cancelled by remember { mutableStateOf(false) }
    var resultDelivered by remember { mutableStateOf(false) }
    var cameraReady by remember { mutableStateOf(false) }
    var previewStreaming by remember { mutableStateOf(false) }
    var cameraError by remember { mutableStateOf<String?>(null) }
    var previewTimeoutTriggered by remember { mutableStateOf(false) }
    var previewStreamingReported by remember { mutableStateOf(false) }
    var retryGeneration by remember { mutableStateOf(0) }
    var pendingValidation by remember { mutableStateOf<File?>(null) }  // HIGH-2: validation off-main

    fun deliver(result: CapturedVideo?) {
        if (resultDelivered) return
        resultDelivered = true
        onResult(result)
    }

    // The torch is a camera CONTROL, not a rebind: switching it on or off never touches the
    // recording in flight. One helper for both the auto-on at record start and the operator's
    // toggle, so the light, the chip and the analytics event can never disagree.
    fun applyTorch(next: Boolean) {
        val camera = boundCamera ?: return
        if (!camera.cameraInfo.hasFlashUnit()) return
        runCatching { camera.cameraControl.enableTorch(next) }
            .onSuccess {
                torchEnabled = next
                onCameraEvent(if (next) "torch_on" else "torch_off")
            }
            .onFailure { onCameraEvent("torch_failed") }
    }

    fun startRecording() {
        val capture = videoCapture ?: return
        if (isRecording) return
        if (pendingValidation != null) return
        val file = newCaptureFile(context)
        val output = FileOutputOptions.Builder(file).build()
        cancelled = false
        startedAtMs = System.currentTimeMillis()
        if (context.checkSelfPermission(Manifest.permission.RECORD_AUDIO) != PackageManager.PERMISSION_GRANTED) {
            file.delete()
            onCameraEvent("audio_permission_missing")
            deliver(null)
            return
        }
        activeRecording = capture.output
            .prepareRecording(context, output)
            // Audio is MANDATORY on every in-app proof clip: a verifier reviewing the video needs
            // what the operator says over it. Without this call CameraX writes a container with no
            // audio track at all (not muted — absent), which is how 344 silent STG proofs shipped.
            // RECORD_AUDIO is therefore part of the blocking capture-permission set
            // (CaptureAccessGate.kt), so the mic is already granted by the time this runs.
            // The camera writes directly to app-private storage, so no storage permission
            // or gallery/file-picker surface is needed.
            .withAudioEnabled()
            .start(ContextCompat.getMainExecutor(context)) { event ->
                if (event is VideoRecordEvent.Finalize) {
                    isRecording = false
                    activeRecording = null
                    if (torchEnabled) applyTorch(false)
                    if (cancelled) {
                        file.delete()
                        onCameraEvent("cancelled")
                        deliver(null)
                    } else if (!event.hasError()) {
                        onCameraEvent("finalized")
                        // HIGH-2: Defer validation to LaunchedEffect (off-main) to avoid jank at Stop-tap
                        pendingValidation = file
                    } else {
                        file.delete()
                        onCameraEvent("finalize_failed")
                        deliver(null)
                    }
                }
            }
        isRecording = true
        onCameraEvent("recording_started")
        // Sheds are dark and operators film at dusk, so the light comes on WITH the recording and
        // the operator only ever has to turn it off. A phone with no flash unit simply stays dark.
        if (!torchEnabled) applyTorch(true)
    }

    fun finishRecording() {
        onCameraEvent("stop_tapped")
        activeRecording?.stop()
    }

    fun cancelRecording() {
        cancelled = true
        val recording = activeRecording
        if (recording == null) {
            onCameraEvent("cancelled")
            deliver(null)
        } else {
            recording.stop()
        }
    }

    BackHandler(onBack = ::cancelRecording)

    // Gate 1: Preview readiness gate — record only when preview stream is STREAMING
    LaunchedEffect(previewStreaming, isRecording, pendingValidation, resultDelivered) {
        if (previewStreaming && !isRecording && pendingValidation == null && !resultDelivered) {
            if (!previewStreamingReported) {
                previewStreamingReported = true
                onCameraEvent("streaming")
            }
            startRecording()
        }
    }

    // Gate 1: Preview timeout (~4s) — if preview never reaches STREAMING, show error + retry
    LaunchedEffect(cameraReady, previewStreaming, previewTimeoutTriggered) {
        if (cameraReady && !previewStreaming && !previewTimeoutTriggered) {
            delay(4000L)
            if (!previewStreaming) {
                previewTimeoutTriggered = true
                cameraError = previewTimeoutMessage
                onCameraEvent("validation_failed")
            }
        }
    }

    LaunchedEffect(isRecording, startedAtMs) {
        elapsedRecordingSeconds = 0L
        while (isRecording && startedAtMs > 0L) {
            elapsedRecordingSeconds = ((System.currentTimeMillis() - startedAtMs) / 1000L).coerceAtLeast(0L)
            delay(250L)
        }
    }

    // HIGH-2: Validate off-main to avoid jank at Stop-tap
    LaunchedEffect(pendingValidation) {
        val fileToValidate = pendingValidation
                if (fileToValidate != null) {
            withContext(Dispatchers.IO) {
                val validation = artifactValidator.validateVideoFile(fileToValidate.toURI().toString())
                if (validation.isValid) {
                    deliver(
                        CapturedVideo(
                            localUri = fileToValidate.toURI().toString(),
                            startedAtMs = startedAtMs,
                            endedAtMs = System.currentTimeMillis(),
                        ),
                    )
                } else {
                    // CRITICAL-2: Invalid video — DO NOT deliver(null); show error UI with retry/cancel options
                    onValidationFailure(validation)
                    fileToValidate.delete()
                    onCameraEvent("validation_failed")
                    cameraError = invalidVideoMessage
                }
            }
            pendingValidation = null
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
            .background(MeshaColors.ViewfinderBackdrop),
    ) {
        // CRITICAL-1: key() forces AndroidView factory re-run + camera rebind on retry
        key(retryGeneration) {
            androidx.compose.ui.viewinterop.AndroidView(
            factory = { ctx ->
                PreviewView(ctx).also { view ->
                    view.scaleType = PreviewView.ScaleType.FILL_CENTER
                    // Gate 1: Monitor preview stream state
                    view.previewStreamState.observe(lifecycleOwner) { streamState ->
                        previewStreaming = streamState == StreamState.STREAMING
                        if (previewStreaming) {
                            previewTimeoutTriggered = false  // Reset timeout on successful streaming
                        }
                    }
                    cameraSession.bind(
                        context = ctx,
                        previewView = view,
                        lifecycleOwner = lifecycleOwner,
                        onBound = { capture ->
                            videoCapture = capture
                            boundCamera = cameraSession.camera
                            cameraReady = true
                            cameraError = null
                            onCameraEvent("bound")
                        },
                        onError = {
                            cameraReady = false
                            previewStreaming = false
                            cameraError = cameraUnavailableMessage
                            onCameraEvent("bind_failed")
                        },
                    )
                }
            },
            onRelease = {
                cameraReady = false
                previewStreaming = false
                videoCapture = null
                boundCamera = null
                torchEnabled = false
                cameraSession.release()
            },
            modifier = Modifier.fillMaxSize(),
        )
        }  // end key(retryGeneration)
        // Directional scrims keep the live preview clear while making overlay text legible.
        Box(
            Modifier
                .fillMaxWidth()
                .height(200.dp)
                .align(Alignment.TopCenter)
                .background(
                    Brush.verticalGradient(
                        listOf(MeshaColors.ViewfinderBackdrop.copy(alpha = 0.38f), Color.Transparent),
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
                        listOf(Color.Transparent, MeshaColors.ViewfinderBackdrop.copy(alpha = 0.46f)),
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
                captureContext = captureContext,
                torchEnabled = torchEnabled,
                torchAvailable = boundCamera?.cameraInfo?.hasFlashUnit() == true,
                onToggleTorch = { applyTorch(!torchEnabled) },
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(10.dp))
            RecordingPills(
                isRecording = isRecording,
                elapsedSeconds = elapsedRecordingSeconds,
            )
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
            Text(
                text = captureContext?.prompt?.let { stringResource(recorderCopyResources(it).instruction) }
                    ?: stringResource(R.string.proof_camera_instruction),
                color = MeshaColors.Ink.copy(alpha = 0.82f),
                style = MeshaType.caption,
            )
            Spacer(Modifier.height(12.dp))
            StopRecordingButton(
                isRecording = isRecording,
                enabled = previewStreaming && !previewTimeoutTriggered,
                cameraError = cameraError,
                retryLabel = retryLabel,
                onRetry = {
                    onCameraEvent("retry_tapped")
                    cameraError = null
                    previewTimeoutTriggered = false
                    cameraSession.release()
                    cameraReady = false
                    videoCapture = null
                    previewStreaming = false
                    previewStreamingReported = false
                    retryGeneration++ // CRITICAL-1: increment to force AndroidView factory re-run + rebind
                },
                onCancel = ::cancelRecording,
                onClick = { if (isRecording) finishRecording() else startRecording() },
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}

@Composable
private fun ProofCardHeader(
    cameraError: String?,
    captureContext: ProofCaptureContext?,
    torchEnabled: Boolean,
    torchAvailable: Boolean,
    onToggleTorch: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val promptCopy = captureContext?.prompt?.let(::recorderCopyResources)
    val fallbackTitle = promptCopy?.let { stringResource(it.title) }
        ?: stringResource(R.string.proof_camera_recording_title)
    Row(
        modifier = modifier
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf.copy(alpha = 0.62f))
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
        Column(Modifier.weight(1f)) {
            Text(
                text = if (cameraError == null) {
                    recorderHeaderTitle(captureContext?.headerTitle, fallbackTitle)
                } else {
                    stringResource(R.string.proof_camera_title)
                },
                color = MeshaColors.Ink,
                style = MeshaType.screenTitle,
            )
            Text(
                text = cameraError ?: stringResource(R.string.proof_camera_auto_opened),
                color = if (cameraError == null) MeshaColors.BrandD else MeshaColors.Danger,
                style = MeshaType.caption,
            )
        }
        IconButton(
            onClick = onToggleTorch,
            enabled = torchAvailable && cameraError == null,
            modifier = Modifier.semantics {
                contentDescription = if (torchEnabled) "Turn flash off" else "Turn flash on"
                stateDescription = if (torchEnabled) "Flash on" else "Flash off"
                role = Role.Button
            },
        ) {
            Icon(
                imageVector = MeshaIcons.Flash,
                contentDescription = null,
                tint = if (torchEnabled) MeshaColors.BrandD else MeshaColors.Muted,
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
            .background(MeshaColors.Surf.copy(alpha = 0.58f))
            .border(1.dp, MeshaColors.Brand.copy(alpha = 0.68f), RoundedCornerShape(18.dp))
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

@Composable
private fun RecordingPills(
    isRecording: Boolean,
    elapsedSeconds: Long,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        InfoChip(
            text = if (isRecording) {
                stringResource(R.string.proof_camera_recording_time, elapsedSeconds.asRecordingDuration())
            } else {
                stringResource(R.string.proof_camera_live_proof)
            },
            tone = ChipTone.Work,
        )
        if (isRecording) InfoChip(text = stringResource(R.string.proof_camera_rec), tone = ChipTone.Danger)
    }
}

private fun Long.asRecordingDuration(): String {
    val minutes = this / 60L
    val seconds = this % 60L
    return "%02d:%02d".format(minutes, seconds)
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
    ProofCapturePrompt.INVENTORY_VACCINE_STOCK -> RecorderCopyResources(
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
    ProofCapturePrompt.FEED_WASTAGE -> RecorderCopyResources(
        R.string.proof_camera_feed_wastage_title,
        R.string.proof_camera_feed_wastage_instruction,
    )
    ProofCapturePrompt.FEED_TRANSPORT -> RecorderCopyResources(
        R.string.proof_camera_feed_transport_title,
        R.string.proof_camera_feed_transport_instruction,
    )
    ProofCapturePrompt.MILK_PREPARATION -> RecorderCopyResources(
        R.string.proof_camera_milk_preparation_title,
        R.string.proof_camera_milk_preparation_instruction,
    )
    ProofCapturePrompt.MILK_FEEDING -> RecorderCopyResources(
        R.string.proof_camera_milk_preparation_title,
        R.string.proof_camera_milk_preparation_instruction,
    )
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
    cameraError: String? = null,
    retryLabel: String = "Retry",
    onRetry: () -> Unit = {},
    onCancel: () -> Unit = {},
    onClick: () -> Unit,
) {
    val actionDescription = stringResource(
        if (isRecording) R.string.proof_camera_stop_recording else R.string.proof_camera_start_recording,
    )
    val recordingState = stringResource(
        if (isRecording) R.string.proof_camera_state_recording else R.string.proof_camera_state_ready,
    )

    if (cameraError != null && !isRecording) {
        // Show retry UI when preview timeout or invalid video occurs
        Row(
            modifier = modifier
                .fillMaxWidth()
                .height(58.dp),
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Button(
                onClick = onRetry,
                colors = ButtonDefaults.buttonColors(
                    containerColor = MeshaColors.Brand,
                    contentColor = MeshaColors.OnBrand,
                ),
                shape = RoundedCornerShape(20.dp),
                modifier = Modifier
                    .weight(1f)
                    .height(58.dp)
                    .semantics {
                        contentDescription = retryLabel
                        role = Role.Button
                    },
            ) {
                Text(text = retryLabel, style = MeshaType.button)
            }
            Button(
                onClick = onCancel,
                colors = ButtonDefaults.buttonColors(
                    containerColor = MeshaColors.Surf3,
                    contentColor = MeshaColors.Muted,
                ),
                shape = RoundedCornerShape(20.dp),
                modifier = Modifier
                    .weight(1f)
                    .height(58.dp),
            ) {
                Text(text = stringResource(R.string.proof_camera_cancel), style = MeshaType.button)
            }
        }
    } else {
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
                .minimumInteractiveComponentSize()
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
}

/** Owns exactly the CameraX use cases bound to the Compose-hosted [PreviewView].
 * bindToLifecycle stops capture while the Activity is stopped; [release] handles the more common
 * in-Activity route/dialog dismissal and invalidates any still-pending provider callback. */
private class ProofCameraSession {
    private var generation = 0
    private var provider: ProcessCameraProvider? = null
    private var preview: Preview? = null
    private var capture: VideoCapture<Recorder>? = null
    var camera: Camera? = null
        private set

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
                    val recorder = Recorder.Builder()
                        .setQualitySelector(proofVideoQualitySelector())
                        .build()
                    val videoCapture = VideoCapture.withOutput(recorder)
                    cameraProvider.unbindAll()
                    val boundCamera = cameraProvider.bindToLifecycle(
                        lifecycleOwner,
                        CameraSelector.DEFAULT_BACK_CAMERA,
                        cameraPreview,
                        videoCapture,
                    )
                    provider = cameraProvider
                    preview = cameraPreview
                    capture = videoCapture
                    camera = boundCamera
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
        camera = null
        provider = null
    }
}

private fun proofVideoQualitySelector(): QualitySelector {
    val forcedQuality = when (BuildConfig.PROOF_VIDEO_QUALITY_OVERRIDE.uppercase()) {
        "SD" -> Quality.SD
        "HD" -> Quality.HD
        "FHD" -> Quality.FHD
        "UHD" -> Quality.UHD
        else -> null
    }
    return forcedQuality?.let(QualitySelector::from) ?: QualitySelector.fromOrderedList(
        listOf(Quality.HD, Quality.FHD, Quality.SD),
        FallbackStrategy.lowerQualityOrHigherThan(Quality.HD),
    )
}

/** App-PRIVATE destination (`Context.filesDir`, never external/MediaStore) — invisible to the
 *  gallery and any other app, so a captured video can never be later swapped for a different
 *  file at the same path (anti-fraud). */
private fun newCaptureFile(context: Context): File {
    val dir = File(context.filesDir, "captures").apply { mkdirs() }
    return File(dir, "proof-${System.currentTimeMillis()}.mp4")
}
