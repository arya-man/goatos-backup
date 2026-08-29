package sg.mesha.goatos.core.ui

import java.io.File
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ProofMediaPreviewContractTest {

    @Test
    fun `video preview keeps controls small and exposes time progress`() {
        val source = File("src/main/kotlin/sg/mesha/goatos/core/ui/ProofMediaPreview.kt").readText()

        assertTrue(source.contains("LinearProgressIndicator"))
        assertTrue(source.contains("readProofVideoDurationMs(context, path)"))
        assertTrue(source.contains("MediaMetadataRetriever.METADATA_KEY_DURATION"))
        assertTrue(source.contains("SystemClock.elapsedRealtime()"))
        assertTrue(source.contains("playStartedPositionMs + elapsed"))
        assertTrue(source.contains("playRequested by remember(path)"))
        assertTrue(source.contains("playWhenReady = playRequested"))
        assertTrue(source.contains("currentPlayer.play()"))
        assertTrue(source.contains("currentPlayer.pause()"))
        assertTrue(source.contains("currentPlayer.seekTo(0L)"))
        assertTrue(source.contains("formatProofPreviewTime(displayPositionMs)"))
        assertTrue(source.contains("formatProofPreviewTime(displayDurationMs)"))
        assertTrue(source.contains("MeshaIcons.Play"))
        assertTrue(source.contains("MeshaIcons.Pause"))
        assertTrue(source.contains("LocalProofPlayerFactory.current"))
        assertTrue(source.contains("withContext(Dispatchers.IO)"))
        assertTrue(source.contains("withTimeoutOrNull(PROOF_POSTER_LOAD_TIMEOUT_MS)"))
        assertTrue(source.contains("remoteProofPreviewLooksReadable(path)"))
        assertTrue(source.contains("PROOF_REMOTE_PROBE_TIMEOUT_MS"))
        assertTrue(source.contains("extractFrame()"))
        assertTrue(source.contains("ProofPreviewLoad.ReadableWithoutPoster"))
        assertTrue(source.contains("isRemote && remoteReadable -> ProofPreviewLoad.ReadableWithoutPoster"))
        assertTrue(source.contains("Video unavailable"))
        assertTrue(source.contains("Photo unavailable"))
        assertFalse(
            "Shared proof preview must use the instrumented proof player factory, not a direct Media3 player.",
            source.contains("ExoPlayer.Builder(context).build()"),
        )
        assertFalse(
            "Video poster extraction must never run synchronously from remember/composition.",
            source.contains("remember(path) { extractFrame() }"),
        )
        assertFalse(
            "The proof preview must not regress to a large text pill that covers the video frame.",
            source.contains("text = if (isPlaying) \"Pause\" else \"Play\""),
        )
    }

    @Test
    fun `live camera recorder does not own post capture preview`() {
        val recorder = File("../../app/src/main/kotlin/sg/mesha/goatos/capture/InAppVideoRecorder.kt").readText()

        assertFalse(
            "Preview belongs to feature proof screens after capture; the live camera surface must stay record-stop only.",
            recorder.contains("ProofMediaPreview("),
        )
        assertFalse(
            "Do not add ExoPlayer playback controls to the recording screen.",
            recorder.contains("ExoPlayer"),
        )
    }
}
