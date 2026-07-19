package sg.mesha.goatos.capture

import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import kotlinx.coroutines.channels.Channel

/**
 * Binds [source] to a real, LIVE in-app camera recording for as long as the composable calling
 * this is part of the composition, and unbinds on dispose — no leaked camera session once the
 * capture surface leaves screen (performance/memory rule: release capture resources on
 * lifecycle stop). Call this once near the top of the screen that renders the `video_proof`
 * control (e.g. inside `SubmitScreen`'s host in `:app`); [ProofCaptureSource.captureVideo] then
 * works from the ViewModel without it ever touching camera/composition APIs directly.
 *
 * Camera-only capture (anti-fraud, docs/mobile/proof-capture-sync-and-e2e.md): this shows
 * [InAppVideoRecorderOverlay] — a full-screen live CameraX preview + record button — and
 * NOTHING else. There is no gallery/file-picker path anywhere in this bridge; the only way a
 * `CapturedVideo` is ever produced is a live recording that just happened.
 */
@Composable
fun BindVideoCaptureSource(source: DelegatingProofCaptureSource) {
    var recorderRequested by remember { mutableStateOf(false) }
    val resultChannel = remember { Channel<CapturedVideo?>(capacity = 1) }

    DisposableEffect(source) {
        source.bind {
            recorderRequested = true
            resultChannel.receive()
        }
        onDispose { source.unbind() }
    }

    if (recorderRequested) {
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
                onResult = { result ->
                    if (recorderRequested) {
                        recorderRequested = false
                        resultChannel.trySend(result)
                    }
                },
            )
        }
    }
}
