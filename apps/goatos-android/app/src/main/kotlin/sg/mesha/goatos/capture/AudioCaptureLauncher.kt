package sg.mesha.goatos.capture

import android.content.Context
import android.media.MediaRecorder
import android.os.Build
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
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
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import sg.mesha.goatos.R
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import java.io.File

/** A voice note is a short spoken remark; a note longer than this is a document, not a note. */
private const val MAX_VOICE_NOTE_MS = 3 * 60 * 1000L

/**
 * Binds [source] to a real, LIVE in-app microphone recording for as long as the calling composable
 * is composed, and unbinds on dispose — the audio sibling of [BindPhotoCaptureSource].
 * [AudioCaptureSource.captureAudio] then works from the ViewModel without it ever touching the
 * recorder or composition APIs directly.
 */
@Composable
fun BindAudioCaptureSource(source: DelegatingAudioCaptureSource) {
    var captureRequested by remember { mutableStateOf(false) }
    var captureContext by remember { mutableStateOf(AudioCaptureContext()) }
    var requestToken by remember { mutableLongStateOf(0L) }
    val resultChannel = remember { Channel<CapturedAudio?>(capacity = 1) }

    DisposableEffect(source) {
        val bindToken = source.bind(
            capture = { context ->
                requestToken += 1L
                captureContext = context
                captureRequested = true
                resultChannel.receive()
            },
        )
        onDispose { source.unbind(bindToken) }
    }

    if (captureRequested) {
        // A real modal window, like the camera overlays: the recorder owns the screen while it runs.
        Dialog(
            onDismissRequest = {},
            properties = DialogProperties(
                dismissOnBackPress = false,
                dismissOnClickOutside = false,
                usePlatformDefaultWidth = false,
                decorFitsSystemWindows = false,
            ),
        ) {
            InAppAudioRecorderOverlay(
                audioContext = captureContext,
                requestToken = requestToken,
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
 * LIVE in-app microphone recording (AAC in an MP4 container, `audio/mp4`). Writes straight to this
 * app's OWN private storage, never external/MediaStore. The recorder is released the moment this
 * composable leaves composition, so a note never keeps the microphone after the operator backs
 * out — and backing out mid-recording discards the file rather than delivering a fragment.
 */
@Composable
private fun InAppAudioRecorderOverlay(
    audioContext: AudioCaptureContext,
    requestToken: Long,
    onResult: (CapturedAudio?) -> Unit,
) {
    val context = LocalContext.current
    val unavailableMessage = stringResource(R.string.proof_audio_unavailable)
    var recorder by remember(requestToken) { mutableStateOf<MediaRecorder?>(null) }
    var file by remember(requestToken) { mutableStateOf<File?>(null) }
    var startedAtMs by remember(requestToken) { mutableLongStateOf(0L) }
    var elapsedMs by remember(requestToken) { mutableLongStateOf(0L) }
    var recording by remember(requestToken) { mutableStateOf(false) }
    var error by remember(requestToken) { mutableStateOf<String?>(null) }
    var resultDelivered by remember(requestToken) { mutableStateOf(false) }

    fun deliver(result: CapturedAudio?) {
        if (resultDelivered) return
        resultDelivered = true
        onResult(result)
    }

    fun releaseRecorder() {
        // exception:exempt a recorder that was never started throws on stop(); either way the
        // handle is released and the overlay closes.
        runCatching { recorder?.stop() }
        runCatching { recorder?.release() }
        recorder = null
    }

    fun start() {
        val target = newAudioCaptureFile(context)
        val mediaRecorder = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) MediaRecorder(context) else @Suppress("DEPRECATION") MediaRecorder()
        try {
            mediaRecorder.setAudioSource(MediaRecorder.AudioSource.MIC)
            mediaRecorder.setOutputFormat(MediaRecorder.OutputFormat.MPEG_4)
            mediaRecorder.setAudioEncoder(MediaRecorder.AudioEncoder.AAC)
            mediaRecorder.setAudioEncodingBitRate(64_000)
            mediaRecorder.setAudioSamplingRate(44_100)
            mediaRecorder.setMaxDuration(MAX_VOICE_NOTE_MS.toInt())
            mediaRecorder.setOutputFile(target.absolutePath)
            mediaRecorder.prepare()
            mediaRecorder.start()
            recorder = mediaRecorder
            file = target
            startedAtMs = System.currentTimeMillis()
            elapsedMs = 0L
            recording = true
            error = null
        } catch (e: Exception) {
            // exception:exempt the microphone being busy/denied is an operator-visible state, not a crash.
            runCatching { mediaRecorder.release() }
            target.delete()
            error = unavailableMessage
        }
    }

    fun stopAndDeliver() {
        if (!recording) return
        recording = false
        val endedAt = System.currentTimeMillis()
        releaseRecorder()
        val recorded = file
        if (recorded == null || !recorded.exists() || recorded.length() <= 0L) {
            recorded?.delete()
            error = unavailableMessage
            return
        }
        deliver(CapturedAudio(localUri = recorded.toURI().toString(), startedAtMs = startedAtMs, endedAtMs = endedAt))
    }

    fun cancel() {
        recording = false
        releaseRecorder()
        file?.delete()
        deliver(null)
    }

    BackHandler(onBack = ::cancel)

    DisposableEffect(requestToken) {
        onDispose {
            runCatching { recorder?.release() }
            recorder = null
        }
    }

    LaunchedEffect(recording) {
        while (recording) {
            elapsedMs = System.currentTimeMillis() - startedAtMs
            if (elapsedMs >= MAX_VOICE_NOTE_MS) {
                stopAndDeliver()
                break
            }
            delay(250L)
        }
    }

    Box(
        Modifier
            .fillMaxSize()
            .background(MeshaColors.PageBg)
            .windowInsetsPadding(WindowInsets.safeDrawing),
    ) {
        Column(
            modifier = Modifier
                .align(Alignment.TopCenter)
                .fillMaxWidth()
                .padding(horizontal = 20.dp, vertical = 16.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text(
                text = audioContext.title.ifBlank { stringResource(R.string.proof_audio_title) },
                color = MeshaColors.Ink,
                style = MeshaType.avatarInitials,
            )
            Spacer(Modifier.height(4.dp))
            Text(
                text = error ?: audioContext.instruction.ifBlank { stringResource(R.string.proof_audio_instruction) },
                color = if (error != null) MeshaColors.Danger else MeshaColors.Muted,
                style = MeshaType.rowLabel,
            )
        }
        Column(
            modifier = Modifier.align(Alignment.Center),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Box(
                modifier = Modifier
                    .size(96.dp)
                    .clip(CircleShape)
                    .background(if (recording) MeshaColors.DangerX else MeshaColors.Surf2),
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    MeshaIcons.Microphone,
                    contentDescription = null,
                    tint = if (recording) MeshaColors.Danger else MeshaColors.Ink,
                    modifier = Modifier.size(40.dp),
                )
            }
            Text(
                text = formatElapsed(elapsedMs),
                color = MeshaColors.Ink,
                style = MeshaType.screenTitle,
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
                Icon(MeshaIcons.Close, contentDescription = stringResource(R.string.proof_audio_cancel), tint = MeshaColors.Ink)
            }
            IconButton(
                onClick = { if (recording) stopAndDeliver() else start() },
                modifier = Modifier
                    .minimumInteractiveComponentSize()
                    .size(72.dp)
                    .clip(CircleShape)
                    .background(if (recording) MeshaColors.Danger else MeshaColors.Brand),
            ) {
                Icon(
                    if (recording) MeshaIcons.Pause else MeshaIcons.Microphone,
                    contentDescription = stringResource(if (recording) R.string.proof_audio_stop else R.string.proof_audio_start),
                    tint = MeshaColors.OnBrand,
                    modifier = Modifier.size(32.dp),
                )
            }
            Spacer(Modifier.size(48.dp))
        }
    }
}

private fun formatElapsed(ms: Long): String {
    val totalSeconds = (ms / 1000L).coerceAtLeast(0L)
    val minutes = totalSeconds / 60L
    val seconds = totalSeconds % 60L
    return "%d:%02d".format(minutes, seconds)
}

/** App-private destination for one voice note, like the photo/video capture files. */
private fun newAudioCaptureFile(context: Context): File {
    val dir = File(context.filesDir, "proof_audio").apply { mkdirs() }
    return File(dir, "voice-note-${System.currentTimeMillis()}.m4a")
}
