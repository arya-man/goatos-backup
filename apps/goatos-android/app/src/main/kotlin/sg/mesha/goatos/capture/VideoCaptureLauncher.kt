package sg.mesha.goatos.capture

import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
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
        InAppVideoRecorderOverlay(
            onResult = { result ->
                recorderRequested = false
                resultChannel.trySend(result)
            },
        )
    }
}
