package sg.mesha.goatos.core.ui

import android.graphics.BitmapFactory
import android.media.MediaMetadataRetriever
import android.net.Uri
import android.os.SystemClock
import androidx.annotation.OptIn
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
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
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.media3.common.MediaItem
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.ui.PlayerView
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

private sealed interface ProofPreviewLoad {
    data object Loading : ProofPreviewLoad
    data class Ready(val bitmap: android.graphics.Bitmap) : ProofPreviewLoad
    data object ReadableWithoutPoster : ProofPreviewLoad
    data object Failed : ProofPreviewLoad
}

@Composable
fun ProofMediaPreview(path: String, kind: ProofMediaPreviewKind, modifier: Modifier = Modifier) {
    when (kind) {
        ProofMediaPreviewKind.Photo -> ProofPhotoPreview(path, modifier)
        ProofMediaPreviewKind.Video -> ProofVideoPreview(path, modifier)
    }
}

@Composable
private fun ProofPhotoPreview(path: String, modifier: Modifier = Modifier) {
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
    Box(
        modifier = modifier.fillMaxWidth().aspectRatio(16f / 9f).clip(RoundedCornerShape(12.dp)),
        contentAlignment = Alignment.Center,
    ) {
        if (bitmap != null) {
            Image(
                bitmap = bitmap.asImageBitmap(),
                contentDescription = null,
                contentScale = ContentScale.Fit,
                modifier = Modifier.fillMaxSize(),
            )
        } else if (isLoading) {
            CircularProgressIndicator(modifier = Modifier.size(32.dp), strokeWidth = 2.dp, color = MeshaColors.Brand)
        } else {
            ProofPreviewUnavailable(icon = MeshaIcons.EyeOff, label = "Photo unavailable")
        }
    }
}

@OptIn(UnstableApi::class)
@Composable
private fun ProofVideoPreview(path: String, modifier: Modifier = Modifier) {
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
        if (!isPlaying || !firstFrameRendered) {
            ProofVideoPoster(path)
        }
        IconButton(
            onClick = {
                val currentPlayer = player
                if (playRequested || currentPlayer?.isPlaying == true) {
                    playRequested = false
                } else {
                    if (!armed) armed = true
                    if (currentPlayer?.playbackState == Player.STATE_ENDED) {
                        currentPlayer.seekTo(0L)
                        positionMs = 0L
                    }
                    playStartedAtMs = SystemClock.elapsedRealtime()
                    playStartedPositionMs = positionMs
                    playRequested = true
                }
            },
            modifier = Modifier
                .align(Alignment.TopEnd)
                .padding(8.dp)
                .size(40.dp)
                .clip(RoundedCornerShape(999.dp))
                .background(MeshaColors.Brand),
        ) {
            Icon(
                imageVector = if (playRequested) MeshaIcons.Pause else MeshaIcons.Play,
                contentDescription = if (playRequested) "Pause proof video" else "Play proof video",
                tint = MeshaColors.OnBrand,
                modifier = Modifier.size(20.dp),
            )
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
