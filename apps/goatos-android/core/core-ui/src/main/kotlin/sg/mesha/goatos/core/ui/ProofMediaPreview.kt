package sg.mesha.goatos.core.ui

import android.content.ActivityNotFoundException
import android.content.Intent
import android.graphics.BitmapFactory
import android.media.MediaMetadataRetriever
import android.net.Uri
import android.os.SystemClock
import androidx.annotation.OptIn
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.content.FileProvider
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.ui.PlayerView
import java.io.File
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import kotlin.math.max
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.media.LocalProofPlayerFactory

enum class ProofMediaPreviewKind { Photo, Video }

object ProofMediaPreviewActions {
    const val PLAY = "play"
    const val PAUSE = "pause"
    const val FULLSCREEN_OPEN = "fullscreen_open"
    const val FULLSCREEN_CLOSE = "fullscreen_close"
    const val SHARE = "share"
    const val PLAYBACK_FAILED = "playback_failed"
}

private sealed interface ProofPreviewLoad {
    data object Loading : ProofPreviewLoad
    data class Ready(val bitmap: android.graphics.Bitmap) : ProofPreviewLoad
    data object ReadableWithoutPoster : ProofPreviewLoad
    data object Failed : ProofPreviewLoad
}

private fun android.content.Context.startProofShare(path: String, kind: ProofMediaPreviewKind): String? {
    try {
        val mimeType = when (kind) {
            ProofMediaPreviewKind.Photo -> "image/*"
            ProofMediaPreviewKind.Video -> "video/mp4"
        }
        val parsed = Uri.parse(path)
        val shareUri = when (parsed.scheme) {
            "content" -> parsed
            "file" -> parsed.path?.let { filePath ->
                FileProvider.getUriForFile(this, "$packageName.fileprovider", File(filePath))
            }
            null, "" -> FileProvider.getUriForFile(this, "$packageName.fileprovider", File(path))
            else -> null
        }
        val shareIntent = if (shareUri != null) {
            Intent(Intent.ACTION_SEND).apply {
                type = mimeType
                putExtra(Intent.EXTRA_STREAM, shareUri)
                addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
            }
        } else {
            Intent(Intent.ACTION_SEND).apply {
                type = "text/plain"
                putExtra(Intent.EXTRA_TEXT, path)
            }
        }
        startActivity(Intent.createChooser(shareIntent, null))
        return null
    } catch (_: ActivityNotFoundException) {
        // exception:exempt no installed share target; preview playback remains available
        return "no_share_target"
    } catch (_: IllegalArgumentException) {
        // exception:exempt FileProvider rejected this path; preview playback remains available
        return "fileprovider_rejected"
    }
}

private fun proofShareAction(failureReason: String?): String =
    if (failureReason == null) {
        "${ProofMediaPreviewActions.SHARE}:success"
    } else {
        "${ProofMediaPreviewActions.SHARE}:failure:$failureReason"
    }

private fun proofPlaybackFailureAction(error: PlaybackException): String =
    "${ProofMediaPreviewActions.PLAYBACK_FAILED}:failure:${playbackFailureReason(error)}"

private fun playbackFailureReason(error: PlaybackException): String =
    when (error.errorCode) {
        PlaybackException.ERROR_CODE_IO_NETWORK_CONNECTION_FAILED,
        PlaybackException.ERROR_CODE_IO_NETWORK_CONNECTION_TIMEOUT,
        -> "network"
        PlaybackException.ERROR_CODE_IO_BAD_HTTP_STATUS,
        PlaybackException.ERROR_CODE_IO_FILE_NOT_FOUND,
        -> "unavailable"
        PlaybackException.ERROR_CODE_PARSING_CONTAINER_MALFORMED,
        PlaybackException.ERROR_CODE_DECODING_FAILED,
        -> "decode"
        else -> "player_error"
    }

@Composable
private fun ProofPreviewActionButton(
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    contentDescription: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    IconButton(
        onClick = onClick,
        modifier = modifier.size(36.dp),
    ) {
        Box(
            modifier = Modifier
                .size(28.dp)
                .clip(RoundedCornerShape(999.dp))
                .background(MeshaColors.Brand),
            contentAlignment = Alignment.Center,
        ) {
            Icon(icon, contentDescription = contentDescription, tint = MeshaColors.OnBrand, modifier = Modifier.size(16.dp))
        }
    }
}

@Composable
private fun ProofPreviewActions(
    path: String,
    kind: ProofMediaPreviewKind,
    onExpand: (() -> Unit)?,
    onAction: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    val context = LocalContext.current
    Row(
        modifier = modifier.padding(8.dp),
        horizontalArrangement = Arrangement.spacedBy(6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (onExpand != null) {
            ProofPreviewActionButton(
                icon = MeshaIcons.Expand,
                contentDescription = "Open proof full screen",
                onClick = {
                    onAction(ProofMediaPreviewActions.FULLSCREEN_OPEN)
                    onExpand()
                },
            )
        }
        ProofPreviewActionButton(
            icon = MeshaIcons.Share,
            contentDescription = "Share proof",
            onClick = {
                onAction(proofShareAction(context.startProofShare(path, kind)))
            },
        )
    }
}

@Composable
fun ProofMediaPreview(
    path: String,
    kind: ProofMediaPreviewKind,
    modifier: Modifier = Modifier,
    onPlaybackFailure: () -> Unit = {},
    onPreviewAction: (String) -> Unit = {},
    // When true, tapping the tile (photo) or the expand button (video) opens the proof
    // full-screen. Proof surfaces opt in by default; pass false only for deliberately static media.
    expandable: Boolean = true,
) {
    var showFullscreen by remember(path) { mutableStateOf(false) }
    // Where the inline video was when it was enlarged, and where fullscreen was when it closed.
    // Enlarging used to build a fresh player at 0:00 -- ten seconds in, expand, and the clip
    // restarted (maintainer report 2026-09-08). The position now travels both ways.
    var fullscreenStartPositionMs by remember(path) { mutableStateOf(0L) }
    var inlineResume by remember(path) { mutableStateOf<ProofVideoResume?>(null) }
    val onExpand: ((positionMs: Long) -> Unit)? = if (expandable) {
        { positionMs ->
            fullscreenStartPositionMs = positionMs
            showFullscreen = true
        }
    } else {
        null
    }
    when (kind) {
        ProofMediaPreviewKind.Photo -> ProofPhotoPreview(path, modifier, onExpand?.let { expand -> { expand(0L) } }, onPreviewAction)
        ProofMediaPreviewKind.Video -> ProofVideoPreview(path, modifier, onPlaybackFailure, onExpand, showFullscreen, onPreviewAction, inlineResume)
    }
    if (showFullscreen) {
        ProofMediaFullscreenDialog(
            path = path,
            kind = kind,
            onPreviewAction = onPreviewAction,
            startPositionMs = fullscreenStartPositionMs,
            onDismiss = { resumePositionMs ->
                onPreviewAction(ProofMediaPreviewActions.FULLSCREEN_CLOSE)
                showFullscreen = false
                if (kind == ProofMediaPreviewKind.Video) {
                    inlineResume = ProofVideoResume(positionMs = resumePositionMs, ticket = (inlineResume?.ticket ?: 0) + 1)
                }
            },
        )
    }
}

/** A position handed back from the fullscreen viewer. [ticket] makes each hand-back distinct so the
 *  inline preview applies it once even when the same position comes back twice. */
private data class ProofVideoResume(val positionMs: Long, val ticket: Int)

@Composable
private fun ProofPhotoPreview(
    path: String,
    modifier: Modifier = Modifier,
    onExpand: (() -> Unit)? = null,
    onPreviewAction: (String) -> Unit = {},
) {
    val context = LocalContext.current
    val isRemote = path.startsWith("http://") || path.startsWith("https://")
    // Local decode stays synchronous (small local files, unchanged behavior); a REMOTE preview must
    // never touch the network on the composing thread (NetworkOnMainThreadException), so it loads
    // via produceState on Dispatchers.IO and falls back to the icon-only state on any failure.
    val remoteLoad by produceState<ProofPreviewLoad>(initialValue = ProofPreviewLoad.Loading, key1 = path) {
        if (isRemote) {
            value = withContext(Dispatchers.IO) {
                try {
                    URL(path).openStream().use(BitmapFactory::decodeStream)?.let(ProofPreviewLoad::Ready)
                        ?: ProofPreviewLoad.Failed
                } catch (_: Exception) {
                    ProofPreviewLoad.Failed
                }
            }
        } else {
            value = ProofPreviewLoad.Failed
        }
    }
    val bitmap = if (isRemote) (remoteLoad as? ProofPreviewLoad.Ready)?.bitmap else remember(path) {
        try {
            val uri = Uri.parse(path)
            when (uri.scheme) {
                "content" -> context.contentResolver.openInputStream(uri)?.use(BitmapFactory::decodeStream)
                "file" -> BitmapFactory.decodeFile(uri.path)
                null, "" -> BitmapFactory.decodeFile(path)
                else -> BitmapFactory.decodeFile(path.removePrefix("file://"))
            }
        } catch (_: IOException) {
            null
        } catch (_: SecurityException) {
            null
        } catch (_: IllegalArgumentException) {
            null
        }
    }
    val isLoading = isRemote && remoteLoad is ProofPreviewLoad.Loading
    val tapToExpand = if (onExpand != null && bitmap != null) {
        Modifier.clickable(
            onClickLabel = "Open proof photo full screen",
            role = Role.Button,
            onClick = {
                onPreviewAction(ProofMediaPreviewActions.FULLSCREEN_OPEN)
                onExpand()
            },
        )
    } else {
        Modifier
    }
    Box(
        modifier = modifier.fillMaxWidth().aspectRatio(16f / 9f).clip(RoundedCornerShape(12.dp)).then(tapToExpand),
        contentAlignment = Alignment.Center,
    ) {
        if (bitmap != null) {
            Image(
                bitmap = bitmap.asImageBitmap(),
                contentDescription = null,
                contentScale = ContentScale.Fit,
                modifier = Modifier.fillMaxSize(),
            )
            if (onExpand != null) {
                ProofPreviewActions(
                    path = path,
                    kind = ProofMediaPreviewKind.Photo,
                    onExpand = onExpand,
                    onAction = onPreviewAction,
                    modifier = Modifier.align(Alignment.TopStart),
                )
            }
        } else if (isLoading) {
            CircularProgressIndicator(modifier = Modifier.size(32.dp), strokeWidth = 2.dp, color = MeshaColors.Brand)
        } else {
            ProofPreviewUnavailable(icon = MeshaIcons.EyeOff, label = "Photo unavailable")
        }
    }
}

@OptIn(UnstableApi::class)
@Composable
private fun ProofVideoPreview(
    path: String,
    modifier: Modifier = Modifier,
    onPlaybackFailure: () -> Unit = {},
    onExpand: ((positionMs: Long) -> Unit)? = null,
    fullscreenShowing: Boolean = false,
    onPreviewAction: (String) -> Unit = {},
    resume: ProofVideoResume? = null,
) {
    val context = LocalContext.current
    val playerFactory = LocalProofPlayerFactory.current
    var playRequested by remember(path) { mutableStateOf(false) }
    var isPlaying by remember(path) { mutableStateOf(false) }
    var armed by remember(path) { mutableStateOf(false) }
    var firstFrameRendered by remember(path) { mutableStateOf(false) }
    var durationMs by remember(path) { mutableStateOf(0L) }
    var positionMs by remember(path) { mutableStateOf(0L) }
    var playStartedAtMs by remember(path) { mutableStateOf(0L) }
    var playStartedPositionMs by remember(path) { mutableStateOf(0L) }
    val metadataDurationMs by produceState(initialValue = 0L, key1 = path) {
        value = withContext(Dispatchers.IO) {
            readProofVideoDurationMs(context, path)
        }
    }
    val displayDurationMs = max(durationMs, metadataDurationMs)
    val displayPositionMs = positionMs.coerceAtMost(displayDurationMs.takeIf { it > 0L } ?: positionMs)
    val player = remember(path, armed) {
        if (!armed) {
            null
        } else {
            playerFactory.create(context).apply {
                setMediaItem(MediaItem.fromUri(Uri.parse(path)))
                playWhenReady = playRequested
                prepare()
            }
        }
    }
    DisposableEffect(player) {
        val listener = object : Player.Listener {
            override fun onRenderedFirstFrame() {
                firstFrameRendered = true
            }

            override fun onPlaybackStateChanged(playbackState: Int) {
                val playerDuration = player?.duration ?: 0L
                if (playerDuration > 0L) durationMs = playerDuration
                if (playbackState == Player.STATE_ENDED) {
                    playRequested = false
                    isPlaying = false
                    positionMs = displayDurationMs
                }
            }

            override fun onIsPlayingChanged(playing: Boolean) {
                isPlaying = playing
                if (playing) {
                    playStartedAtMs = SystemClock.elapsedRealtime()
                    playStartedPositionMs = positionMs
                }
            }

            override fun onPlayerError(error: PlaybackException) {
                playRequested = false
                isPlaying = false
                armed = false
                firstFrameRendered = false
                onPreviewAction(proofPlaybackFailureAction(error))
                onPlaybackFailure()
            }
        }
        player?.addListener(listener)
        onDispose {
            player?.removeListener(listener)
            player?.release()
        }
    }
    LaunchedEffect(player, playRequested) {
        player?.let { currentPlayer ->
            if (playRequested) {
                if (currentPlayer.playbackState == Player.STATE_ENDED) {
                    currentPlayer.seekTo(0L)
                    positionMs = 0L
                }
                currentPlayer.playWhenReady = true
                currentPlayer.play()
            } else {
                currentPlayer.playWhenReady = false
                currentPlayer.pause()
            }
        }
    }
    LaunchedEffect(fullscreenShowing, player) {
        if (fullscreenShowing) {
            playRequested = false
            player?.pause()
        }
    }
    // Fullscreen closed at [resume]: continue from there rather than from the start. A never-armed
    // preview (she enlarged without ever pressing play inline) is armed first so a player exists to
    // seek; the effect re-runs once it does. Applied once per ticket.
    var appliedResumeTicket by remember(path) { mutableStateOf(0) }
    LaunchedEffect(player, resume) {
        val pending = resume ?: return@LaunchedEffect
        if (pending.ticket == appliedResumeTicket) return@LaunchedEffect
        if (player == null) {
            if (pending.positionMs > 0L && !armed) armed = true else appliedResumeTicket = pending.ticket
            return@LaunchedEffect
        }
        player.seekTo(pending.positionMs)
        positionMs = pending.positionMs
        playStartedPositionMs = pending.positionMs
        appliedResumeTicket = pending.ticket
    }
    // The frame she is on, handed to fullscreen so it opens there. A finished clip hands over 0 so
    // the fullscreen player replays it instead of opening on its last frame.
    val expandAtCurrentPosition: (() -> Unit)? = onExpand?.let { expand ->
        { expand(if (player?.playbackState == Player.STATE_ENDED) 0L else displayPositionMs) }
    }
    LaunchedEffect(player) {
        while (player != null) {
            val playerDuration = player.duration
            if (playerDuration > 0L) durationMs = playerDuration
            val playerPosition = player.currentPosition.coerceAtLeast(0L)
            positionMs = if (player.isPlaying && playerPosition <= playStartedPositionMs && displayDurationMs > 0L) {
                val elapsed = SystemClock.elapsedRealtime() - playStartedAtMs
                (playStartedPositionMs + elapsed).coerceIn(0L, displayDurationMs)
            } else {
                playerPosition
            }
            delay(250L)
        }
    }
    Box(
        modifier = modifier.fillMaxWidth().aspectRatio(16f / 9f).clip(RoundedCornerShape(12.dp)).background(MeshaColors.Bg),
        contentAlignment = Alignment.Center,
    ) {
        if (player != null) {
            AndroidView(
                factory = { ctx ->
                    PlayerView(ctx).apply {
                        this.player = player
                        useController = false
                    }
                },
                update = { view ->
                    if (view.player !== player) view.player = player
                },
                modifier = Modifier.fillMaxSize(),
            )
        }
        if ((!isPlaying || !firstFrameRendered) && displayPositionMs == 0L) {
            ProofVideoPoster(path)
        }
        if (onExpand != null) {
            // Tapping anywhere on the video body opens it full screen (the play/pause button,
            // drawn on top, still plays inline). Matches the photo's tap-to-expand.
            Box(
                modifier = Modifier
                    .matchParentSize()
                    .clickable(
                        onClickLabel = "Open proof video full screen",
                        role = Role.Button,
                        onClick = {
                            onPreviewAction(ProofMediaPreviewActions.FULLSCREEN_OPEN)
                            expandAtCurrentPosition?.invoke()
                        },
                    ),
            )
        }
        ProofPreviewActions(
            path = path,
            kind = ProofMediaPreviewKind.Video,
            onExpand = expandAtCurrentPosition,
            onAction = onPreviewAction,
            modifier = Modifier.align(Alignment.TopStart),
        )
        Box(
            modifier = Modifier
                .align(Alignment.TopEnd)
                .padding(6.dp)
                .size(ProofInlinePlayTouchSize)
                .clickable(
                    role = Role.Button,
                    onClick = {
                        val currentPlayer = player
                        if (playRequested || currentPlayer?.isPlaying == true) {
                            onPreviewAction(ProofMediaPreviewActions.PAUSE)
                            playRequested = false
                        } else {
                            onPreviewAction(ProofMediaPreviewActions.PLAY)
                            if (!armed) armed = true
                            if (currentPlayer?.playbackState == Player.STATE_ENDED) {
                                currentPlayer.seekTo(0L)
                                positionMs = 0L
                            }
                            playStartedAtMs = SystemClock.elapsedRealtime()
                            playStartedPositionMs = positionMs
                            playRequested = true
                        }
                    }
                ),
            contentAlignment = Alignment.Center,
        ) {
            Box(
                modifier = Modifier
                    .size(ProofInlinePlayButtonSize)
                    .clip(RoundedCornerShape(999.dp))
                    .background(MeshaColors.Brand),
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    imageVector = if (playRequested) MeshaIcons.Pause else MeshaIcons.Play,
                    contentDescription = if (playRequested) "Pause proof video" else "Play proof video",
                    tint = MeshaColors.OnBrand,
                    modifier = Modifier.size(ProofInlinePlayIconSize),
                )
            }
        }
        Column(
            modifier = Modifier
                .align(Alignment.BottomCenter)
                .fillMaxWidth()
                .background(MeshaColors.ViewfinderBackdrop.copy(alpha = 0.62f))
                .padding(horizontal = 10.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            LinearProgressIndicator(
                progress = {
                    if (displayDurationMs > 0L) {
                        (displayPositionMs.toFloat() / displayDurationMs.toFloat()).coerceIn(0f, 1f)
                    } else {
                        0f
                    }
                },
                modifier = Modifier.fillMaxWidth(),
                color = MeshaColors.Brand,
                trackColor = MeshaColors.Ink.copy(alpha = 0.18f),
            )
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = formatProofPreviewTime(displayPositionMs),
                    color = MeshaColors.OnBrand,
                    style = MeshaType.caption,
                )
                Text(
                    text = formatProofPreviewTime(displayDurationMs),
                    color = MeshaColors.OnBrand,
                    style = MeshaType.caption,
                )
            }
        }
    }
}

private fun readProofVideoDurationMs(context: android.content.Context, path: String): Long {
    val retriever = MediaMetadataRetriever()
    return try {
        if (path.startsWith("http://") || path.startsWith("https://")) {
            retriever.setDataSource(path, emptyMap())
        } else {
            retriever.setDataSource(context, Uri.parse(path))
        }
        retriever
            .extractMetadata(MediaMetadataRetriever.METADATA_KEY_DURATION)
            ?.toLongOrNull()
            ?.coerceAtLeast(0L)
            ?: 0L
    } catch (_: RuntimeException) {
        0L
    } catch (_: SecurityException) {
        0L
    } finally {
        retriever.release()
    }
}

private fun formatProofPreviewTime(ms: Long): String {
    val totalSeconds = (ms / 1000L).coerceAtLeast(0L)
    val minutes = totalSeconds / 60L
    val seconds = totalSeconds % 60L
    return "$minutes:${seconds.toString().padStart(2, '0')}"
}

@Composable
private fun ProofVideoPoster(path: String) {
    val context = LocalContext.current
    val isRemote = path.startsWith("http://") || path.startsWith("https://")
    var remoteReadable = false
    fun extractFrame(): android.graphics.Bitmap? {
        val retriever = MediaMetadataRetriever()
        return try {
            if (isRemote) {
                remoteReadable = remoteProofPreviewLooksReadable(path)
                if (!remoteReadable) return null
                retriever.setDataSource(path, emptyMap())
            } else {
                retriever.setDataSource(context, Uri.parse(path))
            }
            retriever.getFrameAtTime(500_000L, MediaMetadataRetriever.OPTION_CLOSEST_SYNC)
                ?: retriever.frameAtTime
        } catch (_: RuntimeException) {
            null
        } catch (_: SecurityException) {
            null
        } finally {
            retriever.release()
        }
    }
    val posterLoad by produceState<ProofPreviewLoad>(initialValue = ProofPreviewLoad.Loading, key1 = path) {
        value = withContext(Dispatchers.IO) {
            val frame = withTimeoutOrNull(PROOF_POSTER_LOAD_TIMEOUT_MS) {
                extractFrame()
            }
            when {
                frame != null -> ProofPreviewLoad.Ready(frame)
                isRemote && remoteReadable -> ProofPreviewLoad.ReadableWithoutPoster
                else -> ProofPreviewLoad.Failed
            }
        }
    }
    val poster = (posterLoad as? ProofPreviewLoad.Ready)?.bitmap
    if (poster != null) {
        Image(
            bitmap = poster.asImageBitmap(),
            contentDescription = null,
            contentScale = ContentScale.Fit,
            modifier = Modifier.fillMaxSize(),
        )
    } else if (posterLoad is ProofPreviewLoad.Loading) {
        Column(
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            CircularProgressIndicator(modifier = Modifier.size(24.dp), strokeWidth = 2.dp, color = MeshaColors.Brand)
            Icon(MeshaIcons.Video, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(28.dp))
        }
    } else if (posterLoad is ProofPreviewLoad.ReadableWithoutPoster) {
        Icon(MeshaIcons.Video, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(32.dp))
    } else {
        ProofPreviewUnavailable(icon = MeshaIcons.Video, label = "Video unavailable")
    }
}

private const val PROOF_POSTER_LOAD_TIMEOUT_MS = 4_000L
private val ProofInlinePlayTouchSize = 48.dp
private val ProofInlinePlayButtonSize = 30.dp
private val ProofInlinePlayIconSize = 16.dp

private fun remoteProofPreviewLooksReadable(path: String): Boolean {
    return try {
        val connection = URL(path).openConnection() as? HttpURLConnection ?: return false
        connection.instanceFollowRedirects = true
        connection.connectTimeout = PROOF_REMOTE_PROBE_TIMEOUT_MS
        connection.readTimeout = PROOF_REMOTE_PROBE_TIMEOUT_MS
        connection.requestMethod = "GET"
        connection.setRequestProperty("Range", "bytes=0-0")
        val code = connection.responseCode
        connection.disconnect()
        code in 200..399
    } catch (_: Exception) {
        false
    }
}

private const val PROOF_REMOTE_PROBE_TIMEOUT_MS = 1_500

/**
 * Full-screen proof viewer: a photo filling the screen, or a video with full playback controls.
 * Opened by tapping an [expandable] [ProofMediaPreview]; dismissed by the close button or Back.
 */
@OptIn(UnstableApi::class)
@Composable
private fun ProofMediaFullscreenDialog(
    path: String,
    kind: ProofMediaPreviewKind,
    onPreviewAction: (String) -> Unit,
    /** Called with the position the video was at when the viewer closed (0 for a photo, and 0 for
     *  a clip that ran to the end so the inline preview replays it). */
    onDismiss: (resumePositionMs: Long) -> Unit,
    /** Where the inline preview was when it was enlarged -- a video starts here, not at 0. */
    startPositionMs: Long = 0L,
) {
    val context = LocalContext.current
    val playerFactory = LocalProofPlayerFactory.current
    // Built here, outside the Dialog's own composition, so the dismiss request (Back, outside tap,
    // the close button) can read the playhead before the player is released.
    val player = remember(path, kind) {
        if (kind != ProofMediaPreviewKind.Video) {
            null
        } else {
            playerFactory.create(context).apply {
                setMediaItem(MediaItem.fromUri(Uri.parse(path)), startPositionMs.coerceAtLeast(0L))
                playWhenReady = true
                prepare()
            }
        }
    }
    val dismissAtCurrentPosition = {
        val current = player
        onDismiss(
            when {
                current == null -> 0L
                current.playbackState == Player.STATE_ENDED -> 0L
                else -> current.currentPosition.coerceAtLeast(0L)
            },
        )
    }
    androidx.compose.ui.window.Dialog(
        onDismissRequest = dismissAtCurrentPosition,
        properties = androidx.compose.ui.window.DialogProperties(
            usePlatformDefaultWidth = false,
            decorFitsSystemWindows = false,
        ),
    ) {
        Column(modifier = Modifier.fillMaxSize().background(MeshaColors.Bg)) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .statusBarsPadding()
                    .padding(horizontal = 12.dp, vertical = 8.dp),
                horizontalArrangement = Arrangement.End,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                IconButton(
                    onClick = {
                        onPreviewAction(proofShareAction(context.startProofShare(path, kind)))
                    },
                    modifier = Modifier.size(40.dp),
                ) {
                    Box(
                        modifier = Modifier
                            .size(32.dp)
                            .clip(RoundedCornerShape(999.dp))
                            .background(MeshaColors.Brand),
                        contentAlignment = Alignment.Center,
                    ) {
                        Icon(MeshaIcons.Share, contentDescription = "Share proof", tint = MeshaColors.OnBrand, modifier = Modifier.size(18.dp))
                    }
                }
                IconButton(
                    onClick = dismissAtCurrentPosition,
                    modifier = Modifier.size(40.dp),
                ) {
                    Box(
                        modifier = Modifier
                            .size(32.dp)
                            .clip(RoundedCornerShape(999.dp))
                            .background(MeshaColors.Brand),
                        contentAlignment = Alignment.Center,
                    ) {
                        Icon(MeshaIcons.Close, contentDescription = "Close", tint = MeshaColors.OnBrand, modifier = Modifier.size(18.dp))
                    }
                }
            }
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .weight(1f),
            ) {
                when (kind) {
                    ProofMediaPreviewKind.Photo -> {
                        val isRemote = path.startsWith("http://") || path.startsWith("https://")
                        val bitmap by produceState<android.graphics.Bitmap?>(initialValue = null, key1 = path) {
                            value = withContext(Dispatchers.IO) {
                                try {
                                    if (isRemote) {
                                        URL(path).openStream().use(BitmapFactory::decodeStream)
                                    } else {
                                        val uri = Uri.parse(path)
                                        when (uri.scheme) {
                                            "content" -> context.contentResolver.openInputStream(uri)?.use(BitmapFactory::decodeStream)
                                            "file" -> BitmapFactory.decodeFile(uri.path)
                                            null, "" -> BitmapFactory.decodeFile(path)
                                            else -> BitmapFactory.decodeFile(path.removePrefix("file://"))
                                        }
                                    }
                                } catch (_: Exception) {
                                    null
                                }
                            }
                        }
                        val current = bitmap
                        if (current != null) {
                            Image(
                                bitmap = current.asImageBitmap(),
                                contentDescription = null,
                                contentScale = ContentScale.Fit,
                                modifier = Modifier.fillMaxSize(),
                            )
                        } else {
                            CircularProgressIndicator(modifier = Modifier.align(Alignment.Center), color = MeshaColors.Brand)
                        }
                    }
                    ProofMediaPreviewKind.Video -> if (player != null) {
                        val lifecycleOwner = LocalLifecycleOwner.current
                        DisposableEffect(player) {
                            var lastIsPlaying: Boolean? = null
                            val listener = object : Player.Listener {
                                override fun onIsPlayingChanged(isPlaying: Boolean) {
                                    if (lastIsPlaying != isPlaying) {
                                        onPreviewAction(
                                            if (isPlaying) ProofMediaPreviewActions.PLAY else ProofMediaPreviewActions.PAUSE,
                                        )
                                        lastIsPlaying = isPlaying
                                    }
                                }

                                override fun onPlayerError(error: PlaybackException) {
                                    onPreviewAction(proofPlaybackFailureAction(error))
                                    onDismiss(0L)
                                }
                            }
                            player.addListener(listener)
                            val observer = LifecycleEventObserver { _, event ->
                                if (event == Lifecycle.Event.ON_STOP) {
                                    player.pause()
                                }
                            }
                            lifecycleOwner.lifecycle.addObserver(observer)
                            onDispose {
                                lifecycleOwner.lifecycle.removeObserver(observer)
                                player.removeListener(listener)
                                player.release()
                            }
                        }
                        AndroidView(
                            factory = { ctx ->
                                PlayerView(ctx).apply {
                                    this.player = player
                                    useController = true
                                }
                            },
                            modifier = Modifier.fillMaxSize(),
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun ProofPreviewUnavailable(
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    label: String,
) {
    Column(
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Icon(icon, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(28.dp))
        Text(text = label, color = MeshaColors.Muted, style = MeshaType.caption)
    }
}
