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
        assertTrue(source.contains("mediaIdentity: String,"))
        assertTrue(source.contains("stableProofMediaIdentity(mediaIdentity)"))
        assertTrue(source.contains("const val PHOTO_LOAD = \"photo_load\""))
        assertTrue(source.contains("inlineRemotePhoto: Boolean = false"))
        assertTrue(source.contains("\"${'$'}{ProofMediaPreviewActions.PHOTO_LOAD}:${'$'}{if (bitmap != null) \"success\" else \"failure\"}\""))
        assertTrue(source.contains("loadCachedRemoteProofPhoto(context, path, mediaKey, remoteImageLoader)"))
        assertTrue(source.contains("proofPhotoMemoryCache.get(mediaKey)"))
        assertTrue(source.contains("allocationByteCount / 1024"))
        assertTrue(source.contains("proofPhotoCacheFile(context, mediaKey)"))
        assertTrue(source.contains("proofPhotoCacheName(mediaKey)"))
        assertTrue(source.contains("not the rotating signed URL"))
        assertTrue(source.contains("\"${'$'}{file.name}.tmp\""))
        assertTrue(source.contains("pruneProofPhotoCache(file.parentFile)"))
        assertTrue(source.contains("playRequested by remember(mediaKey)"))
        assertTrue(source.contains("LaunchedEffect(path, player)"))
        assertTrue(source.contains("currentPlayer.setMediaItem(MediaItem.fromUri(Uri.parse(path)))"))
        assertTrue(source.contains("playWhenReady = playRequested"))
        assertTrue(source.contains("currentPlayer.play()"))
        assertTrue(source.contains("currentPlayer.pause()"))
        assertTrue(source.contains("currentPlayer.seekTo(0L)"))
        assertTrue(source.contains("formatProofPreviewTime(displayPositionMs)"))
        assertTrue(source.contains("formatProofPreviewTime(displayDurationMs)"))
        assertTrue(source.contains("MeshaIcons.Play"))
        assertTrue(source.contains("MeshaIcons.Pause"))
        assertTrue(source.contains("expandable: Boolean = true"))
        assertTrue(source.contains("fullscreenShowing: Boolean = false"))
        assertTrue(source.contains("LaunchedEffect(fullscreenShowing, player)"))
        assertTrue(source.contains("Open proof photo full screen"))
        assertTrue(source.contains("Open proof video full screen"))
        assertTrue(source.contains("decorFitsSystemWindows = false"))
        assertTrue(source.contains("Lifecycle.Event.ON_STOP"))
        assertTrue(source.contains("ProofInlinePlayTouchSize = 48.dp"))
        assertTrue(source.contains("ProofInlinePlayButtonSize = 30.dp"))
        assertTrue(source.contains("ProofInlinePlayIconSize = 16.dp"))
        assertTrue(source.contains("LocalProofPlayerFactory.current"))
        assertTrue(source.contains("onGloballyPositioned"))
        assertTrue(source.contains("boundsInWindow()"))
        assertTrue(source.contains("LaunchedEffect(isInWindow, player)"))
        assertTrue(source.contains("withContext(Dispatchers.IO)"))
        assertTrue(source.contains("withTimeoutOrNull(PROOF_POSTER_LOAD_TIMEOUT_MS)"))
        assertTrue(source.contains("extractFrame()"))
        assertTrue(source.contains("ProofPreviewLoad.ReadableWithoutPoster"))
        assertTrue(source.contains("isRemote && remoteReadable -> ProofPreviewLoad.ReadableWithoutPoster"))
        assertTrue(source.contains("Video unavailable"))
        assertTrue(source.contains("Photo unavailable"))
        assertTrue(source.contains("override fun onIsPlayingChanged(isPlaying: Boolean)"))
        assertTrue(source.contains("onPreviewAction(proofPlaybackFailureAction(error))"))
        assertTrue(source.contains("\"${'$'}{ProofMediaPreviewActions.PLAYBACK_FAILED}:failure:${'$'}{playbackFailureReason(error)}\""))
        assertTrue(source.contains("PlaybackException.ERROR_CODE_IO_BAD_HTTP_STATUS"))
        assertTrue(source.contains("\"player_error\""))
        assertFalse(
            "Shared proof preview must use the instrumented proof player factory, not a direct Media3 player.",
            source.contains("ExoPlayer.Builder(context).build()"),
        )
        assertFalse(
            "Remote proof previews must not make raw paid object reads from composition.",
            source.contains("URL(path).openStream()"),
        )
        assertFalse(
            "Remote video metadata/poster probing must not use a signed URL as a MediaMetadataRetriever source.",
            source.contains("setDataSource(path, emptyMap())") || source.contains("setDataSource(signedUrl, emptyMap())"),
        )
        assertFalse(
            "Proof preview readability must not probe signed GCS URLs with byte-range requests.",
            source.contains("setRequestProperty(\"Range\""),
        )
        assertFalse(
            "Video poster extraction must never run synchronously from remember/composition.",
            source.contains("remember(path) { extractFrame() }"),
        )
        assertFalse(
            "Signed URLs must not be used as the player state key.",
            source.contains("remember(media.url)") || source.contains("remember(signedUrl)") || source.contains("remember(downloadUrl)"),
        )
        assertFalse(
            "Proof preview callers must pass a stable proof identity instead of falling back to the rotating signed URL.",
            source.contains("mediaIdentity: String = path"),
        )
        assertFalse(
            "The proof preview must not regress to a large text pill that covers the video frame.",
            source.contains("text = if (isPlaying) \"Pause\" else \"Play\""),
        )
        assertFalse(
            "Photo proofs must not regress to a blank click-to-reveal tile.",
            source.contains("Tap to open photo"),
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
