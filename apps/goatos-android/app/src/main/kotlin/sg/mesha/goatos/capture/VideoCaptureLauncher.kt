package sg.mesha.goatos.capture

import android.net.Uri
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.key
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.R
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.proofedit.ProofClip
import sg.mesha.goatos.core.proofedit.ProofEditGate
import sg.mesha.goatos.core.proofedit.ProofEditTelemetry
import sg.mesha.goatos.core.proofedit.ProofTrimEditor
import sg.mesha.goatos.core.proofedit.ProofVideoStitcher
import sg.mesha.goatos.core.proofedit.totalDurationMs
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.withContext
import sg.mesha.goatos.core.data.capture.ProofArtifactValidator
import sg.mesha.goatos.core.data.capture.FileSystemProofArtifactValidator
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
fun BindVideoCaptureSource(
    source: DelegatingProofCaptureSource,
    // MEDIUM: Accept validator as dependency instead of constructing inline
    artifactValidator: ProofArtifactValidator = remember { FileSystemProofArtifactValidator() },
    /**
     * Backend bootstrap `feature_flags`, verbatim. Empty (the default) means the trim editor is
     * never offered, so a host that has not opted in keeps today's capture flow exactly.
     */
    featureFlags: Map<String, Boolean> = emptyMap(),
    /** Step-by-step capture telemetry. No-op by default so existing hosts are unchanged. */
    onTelemetry: (event: String, props: Map<String, String>) -> Unit = { _, _ -> },
) {
    val context = LocalContext.current
    // The ONE capture request the recorder is currently open for, or null when no camera is up.
    // Identity (`token`) is what binds a recording to a subject: a scan of a different animal
    // cancels the in-flight request and starts a NEW token, so a clip finalized late by CameraX
    // still carries the token of the request it was shot for and can never be re-attributed.
    var activeRequest by remember { mutableStateOf<CaptureRequest?>(null) }
    val relay = remember { ProofCaptureRelay(onDiscardedResult = { discardOrphanCapture(context, it) }) }
    // Carry only the raw picked Uri back on the main thread; the (potentially large) copy into
    // app-private storage runs off-main inside the suspend `pick` delegate below, so importing a
    // long clip from the gallery never blocks the UI thread (ANR).
    val pickerChannel = remember { Channel<Uri?>(capacity = 1) }
    val pickerLauncher = rememberLauncherForActivityResult(ActivityResultContracts.GetContent()) { uri: Uri? ->
        pickerChannel.trySend(uri)
    }

    DisposableEffect(source) {
        val bindToken = source.bind(
            launch = { captureContext ->
                val token = relay.nextRequestToken()
                activeRequest = CaptureRequest(token, captureContext)
                try {
                    relay.awaitResult(token)
                } finally {
                    // Only close the camera if it is still OURS. A request cancelled by a scan of
                    // a different animal must not tear down the request that replaced it.
                    if (activeRequest?.token == token) {
                        activeRequest = null
                    }
                }
            },
            pick = {
                pickerLauncher.launch("video/*")
                pickerChannel.receive()?.let { selected ->
                    withContext(Dispatchers.IO) {
                        copyPickedVideoToPrivateCache(context, selected, System.currentTimeMillis(), artifactValidator)
                    }
                }
            },
        )
        onDispose { source.unbind(bindToken) }
    }

    val request = activeRequest
    if (request != null) {
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
            // Keyed by request token: retargeting the camera at a newly scanned animal DISPOSES
            // the previous recorder (releasing the camera and deleting its unfinished file) and
            // composes a fresh one. `request.token` is captured by this composition, so whatever
            // that recorder eventually reports is stamped with the request it was shot for.
            key(request.token) {
                ProofCaptureFlow(
                    captureContext = request.captureContext,
                    artifactValidator = artifactValidator,
                    featureFlags = featureFlags,
                    onTelemetry = onTelemetry,
                    onFinished = { result ->
                        // Dismiss the recorder immediately, but only if this request still owns
                        // the camera -- a superseded recorder must not close the live one.
                        if (activeRequest?.token == request.token) {
                            activeRequest = null
                        }
                        relay.deliverResult(request.token, result)
                    },
                )
            }
        }
    }
}

/** One in-flight capture request: its identity and the operator-facing copy it opened with. */
private data class CaptureRequest(
    val token: Long,
    val captureContext: ProofCaptureContext?,
)

/**
 * A recording that finalized for a request that had already been cancelled or superseded. It
 * belongs to nobody: attributing it to the next subject is the wrong-animal bug the relay exists
 * to prevent, so the orphan file is deleted. Restricted to this app's own capture directories so
 * a malformed uri can never delete anything else.
 */
private fun discardOrphanCapture(context: android.content.Context, video: CapturedVideo) {
    runCatching {
        val file = File(java.net.URI(video.localUri))
        val ownedRoots = listOf(File(context.filesDir, "captures"), File(context.cacheDir, "proof-videos"))
        if (ownedRoots.any { file.canonicalPath.startsWith(it.canonicalPath + File.separator) }) {
            file.delete()
        }
    }
}

private fun copyPickedVideoToPrivateCache(
    context: android.content.Context,
    sourceUri: Uri,
    nowMs: Long,
    // MEDIUM: Accept validator as dependency instead of constructing inline
    validator: ProofArtifactValidator = FileSystemProofArtifactValidator(),
): CapturedVideo? = runCatching { // exception:exempt best-effort local cache copy; null return already surfaces a retry-capable failure to the caller, nothing extra to record
    val dir = File(context.cacheDir, "proof-videos").apply { mkdirs() }
    val out = File(dir, "gallery-$nowMs.mp4")
    context.contentResolver.openInputStream(sourceUri)?.use { input ->
        out.outputStream().use { output -> input.copyTo(output) }
    } ?: return@runCatching null

    // Gate 4: Gallery copy validation — copied file non-zero + metadata readable
    val validation = validator.validateVideoFile(out.toURI().toString())
    if (!validation.isValid) {
        out.delete()
        return@runCatching null
    }

    CapturedVideo(
        localUri = out.toURI().toString(),
        mimeType = context.contentResolver.getType(sourceUri) ?: "video/mp4",
        startedAtMs = nowMs,
        endedAtMs = nowMs,
        captureSource = "gallery_picker",
    )
}.getOrNull()

/** Where a capture currently is. Only [Recording] exists when the trim editor is not offered. */
private sealed interface CaptureStage {
    data object Recording : CaptureStage
    data class Editing(val video: CapturedVideo) : CaptureStage
    data class Saving(val video: CapturedVideo, val clips: List<ProofClip>) : CaptureStage
}

/**
 * The whole capture flow the operator sees inside the camera window: record, then — ONLY when
 * [ProofEditGate] allows it for this surface — trim, then a progress step while the kept parts are
 * joined, then back to the screen that asked for the proof.
 *
 * This composable owns every step so the operator never returns to the proofs screen mid-work and
 * never sees a screen with no explanation of what the app is doing.
 *
 * IMPORTANT: whatever this hands to [onFinished] re-enters the EXISTING pipeline in the same shape
 * an untrimmed capture does. Compression and the audit overlay still happen downstream in
 * `ProofMediaProcessor`, so editing adds no branch there and cannot double-process a proof.
 *
 * Every exit path calls [onFinished] exactly once — including a failed join, which falls back to
 * the untrimmed recording rather than losing the operator's work.
 */
@Composable
private fun ProofCaptureFlow(
    captureContext: ProofCaptureContext?,
    artifactValidator: ProofArtifactValidator,
    featureFlags: Map<String, Boolean>,
    onTelemetry: (String, Map<String, String>) -> Unit,
    onFinished: (CapturedVideo?) -> Unit,
) {
    val context = LocalContext.current
    val surface = captureContext?.featureSurface
    val editingOffered = remember(featureFlags, surface) {
        ProofEditGate.isEditingOffered(featureFlags, surface)
    }
    var stage by remember { mutableStateOf<CaptureStage>(CaptureStage.Recording) }

    /** Params every step of this capture reports. All keys are already on the Firebase allowlist. */
    fun baseProps(step: String): Map<String, String> = buildMap {
        put(ProofEditTelemetry.Params.PROCESSING_STATE, step)
        surface?.takeIf { it.isNotBlank() }?.let { put(ProofEditTelemetry.Params.FEATURE_SURFACE, it) }
        // The scanned identity, when this surface has one — so a stuck capture is traceable to the
        // exact animal rather than to "some proof".
        captureContext?.primaryTag?.takeIf { it.isNotBlank() }
            ?.let { put(ProofEditTelemetry.Params.RFID_TAG, it) }
    }

    LaunchedEffect(Unit) {
        onTelemetry(
            ProofEditTelemetry.Events.CAPTURE_OPENED,
            baseProps(ProofEditTelemetry.Step.RECORDING) +
                mapOf(
                    ProofEditTelemetry.Params.OUTCOME to
                        if (editingOffered) {
                            ProofEditTelemetry.Outcome.OK
                        } else {
                            ProofEditTelemetry.Outcome.EDIT_NOT_OFFERED
                        },
                ),
        )
    }

    fun finish(result: CapturedVideo?, step: String, outcome: String, reason: String? = null) {
        onTelemetry(
            if (result == null) {
                ProofEditTelemetry.Events.CAPTURE_ABANDONED
            } else {
                ProofEditTelemetry.Events.CAPTURE_DELIVERED
            },
            baseProps(step) +
                buildMap {
                    put(ProofEditTelemetry.Params.OUTCOME, outcome)
                    reason?.let { put(ProofEditTelemetry.Params.REASON, it) }
                    // `source` is allowlisted; capture_source/mime_type are NOT and would be
                    // dropped before reaching GA4.
                    result?.let { put(ProofEditTelemetry.Params.SOURCE, it.captureSource) }
                },
        )
        onFinished(result)
    }

    when (val current = stage) {
        CaptureStage.Recording -> InAppVideoRecorderOverlay(
            captureContext = captureContext,
            onResult = { result ->
                when {
                    result == null -> finish(
                        null,
                        ProofEditTelemetry.Step.RECORDING,
                        ProofEditTelemetry.Outcome.CANCELLED,
                    )
                    editingOffered -> {
                        onTelemetry(
                            ProofEditTelemetry.Events.CAPTURE_RECORDED,
                            baseProps(ProofEditTelemetry.Step.RECORDING) +
                                mapOf(ProofEditTelemetry.Params.OUTCOME to ProofEditTelemetry.Outcome.OK),
                        )
                        stage = CaptureStage.Editing(result)
                    }
                    // Editor not offered: today's path, unchanged.
                    else -> finish(
                        result,
                        ProofEditTelemetry.Step.DELIVERING,
                        ProofEditTelemetry.Outcome.EDIT_NOT_OFFERED,
                    )
                }
            },
            artifactValidator = artifactValidator,  // MEDIUM: pass injected validator
        )

        is CaptureStage.Editing -> ProofTrimEditor(
            sourceUri = current.video.localUri,
            // Abandoning here cancels the whole capture, exactly as back during recording does.
            // The recorder's own cancel deletes its file; an abandoned edit leaves the recording
            // for the relay to discard as an orphan, which is the existing cleanup path.
            onCancel = {
                finish(null, ProofEditTelemetry.Step.EDITING, ProofEditTelemetry.Outcome.CANCELLED)
            },
            onTelemetry = { event, props -> onTelemetry(event, baseProps(ProofEditTelemetry.Step.EDITING) + props) },
            onDone = { clips ->
                if (clips.isEmpty()) {
                    // Nothing cut: deliver the recording as-is. Re-encoding an untouched clip
                    // would cost a generation of quality for no change.
                    finish(
                        current.video,
                        ProofEditTelemetry.Step.DELIVERING,
                        ProofEditTelemetry.Outcome.UNEDITED,
                    )
                } else {
                    stage = CaptureStage.Saving(current.video, clips)
                }
            },
        )

        is CaptureStage.Saving -> {
            LaunchedEffect(current.video.localUri, current.clips) {
                onTelemetry(
                    ProofEditTelemetry.Events.STITCH_STARTED,
                    baseProps(ProofEditTelemetry.Step.STITCHING) +
                        mapOf(
                            ProofEditTelemetry.Params.DURATION_BUCKET to
                                ProofEditTelemetry.durationBucket(current.clips.totalDurationMs()),
                        ),
                )
                val stitched = runCatching {
                    ProofVideoStitcher(context).stitch(current.video.localUri, current.clips)
                }
                stitched.fold(
                    onSuccess = { result ->
                        // Same gate a picked/imported artifact passes: a zero-byte or unreadable
                        // output must never reach the upload queue as this operator's evidence.
                        val validation = artifactValidator.validateVideoFile(result.outputUri)
                        if (!validation.isValid) {
                            onTelemetry(
                                ProofEditTelemetry.Events.STITCH_FAILED,
                                baseProps(ProofEditTelemetry.Step.STITCHING) +
                                    mapOf(
                                        ProofEditTelemetry.Params.OUTCOME to ProofEditTelemetry.Outcome.FAILED,
                                        ProofEditTelemetry.Params.REASON to "invalid_output",
                                    ),
                            )
                            finish(
                                current.video,
                                ProofEditTelemetry.Step.DELIVERING,
                                ProofEditTelemetry.Outcome.UNEDITED,
                                reason = "invalid_output",
                            )
                            return@fold
                        }
                        onTelemetry(
                            ProofEditTelemetry.Events.STITCH_SUCCEEDED,
                            baseProps(ProofEditTelemetry.Step.STITCHING) +
                                mapOf(
                                    ProofEditTelemetry.Params.OUTCOME to ProofEditTelemetry.Outcome.OK,
                                    ProofEditTelemetry.Params.DURATION_BUCKET to
                                        ProofEditTelemetry.durationBucket(result.outputDurationMs),
                                ),
                        )
                        finish(
                            // Capture start/stop are PRESERVED: they are the freshness metadata for
                            // when the work was filmed, not for when it was edited.
                            current.video.copy(
                                localUri = result.outputUri,
                                mimeType = result.outputMimeType,
                            ),
                            ProofEditTelemetry.Step.DELIVERING,
                            ProofEditTelemetry.Outcome.EDITED,
                        )
                    },
                    onFailure = { error ->
                        onTelemetry(
                            ProofEditTelemetry.Events.STITCH_FAILED,
                            baseProps(ProofEditTelemetry.Step.STITCHING) +
                                mapOf(
                                    ProofEditTelemetry.Params.OUTCOME to ProofEditTelemetry.Outcome.FAILED,
                                    ProofEditTelemetry.Params.REASON to (error::class.java.simpleName),
                                ),
                        )
                        // Never lose the operator's work to an export failure: the untrimmed
                        // recording is still valid evidence and still gets processed downstream.
                        finish(
                            current.video,
                            ProofEditTelemetry.Step.DELIVERING,
                            ProofEditTelemetry.Outcome.UNEDITED,
                            reason = "stitch_failed",
                        )
                    },
                )
            }
            // Back is deliberately swallowed WHILE the export runs: cancelling mid-encode would
            // leave a partial file and the step is seconds long. It is a considered block, not a
            // missing handler — the editor behind it is fully cancellable.
            BackHandler {}
            ProofSavingStep(clipCount = current.clips.size)
        }
    }
}

/** Progress while the kept parts are joined. Says what is happening in farm language only. */
@Composable
private fun ProofSavingStep(clipCount: Int) {
    Column(
        Modifier
            .fillMaxSize()
            .background(MeshaColors.PageBg),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        CircularProgressIndicator(color = MeshaColors.Brand)
        Spacer(Modifier.height(18.dp))
        Text(
            text = stringResource(R.string.proof_capture_saving_title),
            color = MeshaColors.Ink,
            style = MeshaType.cardTitle,
        )
        Spacer(Modifier.height(6.dp))
        Text(
            text = stringResource(R.string.proof_capture_saving_body, clipCount),
            color = MeshaColors.Muted,
            style = MeshaType.caption,
        )
    }
}
