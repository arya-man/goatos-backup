package sg.mesha.goatos.capture

import android.content.Context
import androidx.activity.compose.BackHandler
import androidx.camera.core.Camera
import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageCapture
import androidx.camera.core.ImageCaptureException
import androidx.camera.core.Preview
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
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
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.LocalLifecycleOwner
import kotlinx.coroutines.channels.Channel
import sg.mesha.goatos.R
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import java.io.File

/**
 * Binds [source] to a real, LIVE in-app camera PHOTO capture for as long as the calling composable
 * is part of the composition, and unbinds on dispose — the photo sibling of [BindVideoCaptureSource].
 * [PhotoCaptureSource.capturePhoto] then works from the ViewModel without it ever touching
 * camera/composition APIs directly. Used by feed proof captures that need a still image.
 */
@Composable
fun BindPhotoCaptureSource(source: DelegatingPhotoCaptureSource) {
    val context = LocalContext.current
    val analytics = remember {
        dagger.hilt.android.EntryPointAccessors.fromApplication(context.applicationContext, ProofCaptureAnalyticsEntryPoint::class.java)
            .analyticsPort()
    }
    var captureRequested by remember { mutableStateOf(false) }
    var captureContext by remember { mutableStateOf(PhotoCaptureContext()) }
    var requestToken by remember { mutableStateOf(0L) }
    var nextRequestToken by remember { mutableStateOf(0L) }
    val resultChannel = remember { Channel<CapturedPhoto?>(capacity = 1) }

    DisposableEffect(source) {
        val bindToken = source.bind(
            capture = { context ->
                nextRequestToken += 1L
                requestToken = nextRequestToken
                captureContext = context
                captureRequested = true
                trackPhotoCameraEvent(analytics, AnalyticsEvents.PROOF_CAMERA_REQUESTED, requestToken, context)
                resultChannel.receive()
            },
        )
        onDispose { source.unbind(bindToken) }
    }

    if (captureRequested) {
        // A real modal window so the live preview sits above the originating surface with exclusive
        // visibility/input (same reason [BindVideoCaptureSource] uses a full-screen Dialog).
        Dialog(
            onDismissRequest = {},
            properties = DialogProperties(
                dismissOnBackPress = false,
                dismissOnClickOutside = false,
                usePlatformDefaultWidth = false,
                decorFitsSystemWindows = false,
            ),
        ) {
            LaunchedEffect(requestToken) {
                trackPhotoCameraEvent(analytics, AnalyticsEvents.PROOF_CAMERA_VISIBLE, requestToken, captureContext)
            }
            InAppPhotoCaptureOverlay(
                photoContext = captureContext,
                requestToken = requestToken,
                onCameraEvent = { stage ->
                    val event = when (stage) {
                        "bound" -> AnalyticsEvents.PROOF_CAMERA_BOUND
                        "streaming" -> AnalyticsEvents.PROOF_CAMERA_STREAMING
                        "cancelled" -> AnalyticsEvents.PROOF_CAMERA_CANCELLED
                        "finalized" -> AnalyticsEvents.PROOF_CAMERA_FINALIZED
                        "torch_on" -> AnalyticsEvents.PROOF_CAMERA_TORCH_ON
                        "torch_off" -> AnalyticsEvents.PROOF_CAMERA_TORCH_OFF
                        "torch_failed" -> AnalyticsEvents.PROOF_CAMERA_TORCH_FAILED
                        else -> AnalyticsEvents.PROOF_CAMERA_FAILED
                    }
                    trackPhotoCameraEvent(analytics, event, requestToken, captureContext, reason = stage)
                },
                onResult = { result ->
                    if (captureRequested) {
                        captureRequested = false
                        resultChannel.trySend(result)
                    }
                },
            )
        }
    }
}

/**
 * LIVE, in-app camera photo capture (CameraX `ImageCapture`). Owns the camera preview + shutter end
 * to end and writes straight to this app's OWN private storage ([Context.filesDir], never external/
 * MediaStore). The camera binding is released the moment this composable leaves composition
 * ([DisposableEffect]) — no leaked camera session once the operator backs out or the shot completes.
 */
@Composable
private fun InAppPhotoCaptureOverlay(
    photoContext: PhotoCaptureContext,
    requestToken: Long,
    onCameraEvent: (String) -> Unit,
    onResult: (CapturedPhoto?) -> Unit,
) {
    val context = LocalContext.current
    val lifecycleOwner = LocalLifecycleOwner.current
    val cameraSession = remember { PhotoCameraSession() }
    val cameraUnavailableMessage = stringResource(R.string.proof_camera_unavailable)

    var imageCapture by remember { mutableStateOf<ImageCapture?>(null) }
    var camera by remember { mutableStateOf<Camera?>(null) }
    var cameraReady by remember { mutableStateOf(false) }
    var cameraError by remember { mutableStateOf<String?>(null) }
    var isCapturing by remember { mutableStateOf(false) }
    var resultDelivered by remember { mutableStateOf(false) }
    var torchEnabled by remember { mutableStateOf(false) }
    var hasFlash by remember { mutableStateOf(false) }

    fun deliver(result: CapturedPhoto?) {
        if (resultDelivered) return
        resultDelivered = true
        onResult(result)
    }

    fun takePhoto() {
        val capture = imageCapture ?: return
        if (isCapturing) return
        isCapturing = true
        val file = newPhotoCaptureFile(context)
        val output = ImageCapture.OutputFileOptions.Builder(file).build()
        capture.takePicture(
            output,
            ContextCompat.getMainExecutor(context),
            object : ImageCapture.OnImageSavedCallback {
                override fun onImageSaved(results: ImageCapture.OutputFileResults) {
                    isCapturing = false
                    onCameraEvent("finalized")
                    deliver(
                        CapturedPhoto(
                            localUri = file.toURI().toString(),
                            capturedAtMs = System.currentTimeMillis(),
                        ),
                    )
                }

                override fun onError(exception: ImageCaptureException) {
                    isCapturing = false
                    file.delete()
                    cameraError = cameraUnavailableMessage
                    onCameraEvent("failed")
                }
            },
        )
    }

    fun cancel() {
        onCameraEvent("cancelled")
        deliver(null)
    }

    fun toggleTorch() {
        val boundCamera = camera ?: return
        val target = !torchEnabled
        boundCamera.cameraControl.enableTorch(target).addListener(
            {
                torchEnabled = target
                onCameraEvent(if (target) "torch_on" else "torch_off")
            },
            ContextCompat.getMainExecutor(context),
        )
    }

    BackHandler(onBack = ::cancel)

    DisposableEffect(Unit) {
        onDispose {
            imageCapture = null
            camera = null
            cameraSession.release()
        }
    }

    Box(
        Modifier
            .fillMaxSize()
            .background(MeshaColors.ViewfinderBackdrop)
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
                        onBound = { capture, boundCamera ->
                            imageCapture = capture
                            camera = boundCamera
                            cameraReady = true
                            hasFlash = boundCamera.cameraInfo.hasFlashUnit()
                            cameraError = null
                            onCameraEvent("bound")
                            onCameraEvent("streaming")
                        },
                        onError = {
                            cameraReady = false
                            cameraError = cameraUnavailableMessage
                            onCameraEvent("failed")
                        },
                    )
                }
            },
            onRelease = {
                cameraReady = false
                imageCapture = null
                camera = null
                cameraSession.release()
            },
            modifier = Modifier.fillMaxSize(),
        )
        Column(
            modifier = Modifier
                .align(Alignment.TopCenter)
                .fillMaxWidth()
                .background(MeshaColors.ViewfinderBackdrop.copy(alpha = 0.68f))
                .padding(horizontal = 20.dp, vertical = 16.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text(
                text = photoContext.title.ifBlank { stringResource(R.string.proof_photo_title) },
                color = MeshaColors.Ink,
                style = MeshaType.avatarInitials,
            )
            Spacer(Modifier.height(4.dp))
            Text(
                text = cameraError ?: photoContext.instruction.ifBlank { stringResource(R.string.proof_photo_instruction) },
                color = if (cameraError != null) MeshaColors.Danger else MeshaColors.Ink,
                style = MeshaType.rowLabel,
            )
        }
        Row(
            Modifier
                .fillMaxWidth()
                .align(Alignment.BottomCenter)
                .padding(24.dp),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(
                onClick = ::cancel,
                modifier = Modifier
                    .minimumInteractiveComponentSize()
                    .size(48.dp)
                    .clip(CircleShape)
                    .background(MeshaColors.Surf3),
            ) {
                Icon(MeshaIcons.Close, contentDescription = stringResource(R.string.proof_photo_cancel), tint = MeshaColors.Ink)
            }
            ShutterButton(
                enabled = cameraReady && !isCapturing,
                onClick = ::takePhoto,
            )
            IconButton(
                onClick = {
                    runCatching { toggleTorch() }
                        .onFailure { onCameraEvent("torch_failed") }
                },
                enabled = cameraReady && hasFlash,
                modifier = Modifier
                    .minimumInteractiveComponentSize()
                    .size(48.dp)
                    .clip(CircleShape)
                    .background(if (torchEnabled) MeshaColors.Brand else MeshaColors.Surf3),
            ) {
                Icon(
                    MeshaIcons.Flash,
                    contentDescription = if (torchEnabled) "Turn flash off" else "Turn flash on",
                    tint = if (torchEnabled) MeshaColors.OnBrand else MeshaColors.Ink,
                )
            }
        }
    }
}

@Composable
private fun ShutterButton(enabled: Boolean, onClick: () -> Unit) {
    val actionDescription = stringResource(R.string.proof_photo_take)
    Box(
        Modifier
            .minimumInteractiveComponentSize()
            .size(72.dp)
            .clip(CircleShape)
            .background(MeshaColors.ViewfinderBackdrop.copy(alpha = 0.56f))
            .border(3.dp, if (enabled) MeshaColors.Ink else MeshaColors.Muted, CircleShape)
            .semantics {
                contentDescription = actionDescription
                role = Role.Button
            }
            .clickable(enabled = enabled, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Box(
            Modifier
                .size(56.dp)
                .clip(CircleShape)
                .background(if (enabled) MeshaColors.Ink else MeshaColors.Muted),
        )
    }
}

/** Owns the CameraX Preview + ImageCapture use cases bound to the Compose-hosted [PreviewView].
 *  [release] unbinds them on route/dialog dismissal (the common in-Activity teardown). */
private class PhotoCameraSession {
    private var generation = 0
    private var provider: ProcessCameraProvider? = null
    private var preview: Preview? = null
    private var capture: ImageCapture? = null

    fun bind(
        context: Context,
        previewView: PreviewView,
        lifecycleOwner: androidx.lifecycle.LifecycleOwner,
        onBound: (ImageCapture, Camera) -> Unit,
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
                    val imageCapture = ImageCapture.Builder()
                        .setCaptureMode(ImageCapture.CAPTURE_MODE_MINIMIZE_LATENCY)
                        .build()
                    cameraProvider.unbindAll()
                    val camera = cameraProvider.bindToLifecycle(
                        lifecycleOwner,
                        CameraSelector.DEFAULT_BACK_CAMERA,
                        cameraPreview,
                        imageCapture,
                    )
                    provider = cameraProvider
                    preview = cameraPreview
                    capture = imageCapture
                    onBound(imageCapture, camera)
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
 *  gallery and any other app, so a captured photo can never be swapped for a different file at the
 *  same path (anti-fraud), mirroring the video capture path. */
private fun newPhotoCaptureFile(context: Context): File {
    val dir = File(context.filesDir, "captures").apply { mkdirs() }
    return File(dir, "proof-${System.currentTimeMillis()}.jpg")
}

private fun trackPhotoCameraEvent(
    analytics: AnalyticsPort,
    event: String,
    token: Long,
    captureContext: PhotoCaptureContext,
    reason: String? = null,
) {
    trackProofCameraEvent(
        analytics = analytics,
        event = event,
        token = token,
        captureContext = ProofCaptureContext(
            title = captureContext.title,
            primaryTag = "",
            workLabel = captureContext.title,
            prompt = captureContext.prompt,
            headerTitle = captureContext.title,
        ),
        source = "in_app_photo_camera",
        reason = reason,
    )
}
