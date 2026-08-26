package sg.mesha.goatos.core.ui

import android.graphics.BitmapFactory
import android.media.MediaMetadataRetriever
import android.net.Uri
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
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.produceState
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.withContext
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.media3.common.MediaItem
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.ui.PlayerView
import java.io.IOException
import java.net.URL
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.media.LocalProofPlayerFactory

enum class ProofMediaPreviewKind { Photo, Video }

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
    val remoteBitmap by produceState<android.graphics.Bitmap?>(initialValue = null, key1 = path) {
        if (isRemote) {
            value = withContext(Dispatchers.IO) {
                try {
                    URL(path).openStream().use(BitmapFactory::decodeStream)
                } catch (_: Exception) {
                    null
                }
            }
        }
    }
    val isLoading = isRemote && remoteBitmap == null
    val bitmap = if (isRemote) remoteBitmap else remember(path) {
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
        }
    }
}

@OptIn(UnstableApi::class)
@Composable
private fun ProofVideoPreview(path: String, modifier: Modifier = Modifier) {
    val context = LocalContext.current
    val playerFactory = LocalProofPlayerFactory.current
    var isPlaying by remember(path) { mutableStateOf(false) }
    var armed by remember(path) { mutableStateOf(false) }
    var firstFrameRendered by remember(path) { mutableStateOf(false) }
    var durationMs by remember(path) { mutableStateOf(0L) }
    var positionMs by remember(path) { mutableStateOf(0L) }
    val player = remember(path, armed, playerFactory) {
        if (!armed) {
            null
        } else {
            playerFactory.create(context).apply {
                setMediaItem(MediaItem.fromUri(Uri.parse(path)))
                playWhenReady = false
                prepare()
            }
        }
    }
    DisposableEffect(player) {
        val listener = object : Player.Listener {
            override fun onRenderedFirstFrame() {
                firstFrameRendered = true
            }
            override fun onIsPlayingChanged(isPlayingNow: Boolean) {
                isPlaying = isPlayingNow
            }
            override fun onPlaybackStateChanged(playbackState: Int) {
                val currentDuration = player?.duration?.takeIf { it > 0L } ?: 0L
                durationMs = currentDuration
                if (playbackState == Player.STATE_ENDED) {
                    isPlaying = false
                    positionMs = currentDuration
                }
            }
        }
        player?.addListener(listener)
        onDispose {
            player?.removeListener(listener)
            player?.release()
        }
    }
    LaunchedEffect(player, isPlaying) {
        if (isPlaying) player?.play() else player?.pause()
    }
    LaunchedEffect(player, isPlaying) {
        while (player != null && isPlaying) {
            positionMs = player.currentPosition.coerceAtLeast(0L)
            durationMs = player.duration.takeIf { it > 0L } ?: durationMs
            delay(500L)
        }
        player?.let {
            positionMs = it.currentPosition.coerceAtLeast(0L)
            durationMs = it.duration.takeIf { value -> value > 0L } ?: durationMs
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
        ProofVideoProgressBar(
            positionMs = positionMs,
            durationMs = durationMs,
            modifier = Modifier.align(Alignment.BottomCenter),
        )
        Box(
            modifier = Modifier
                .align(Alignment.BottomEnd)
                .padding(end = 10.dp, bottom = 34.dp)
                .size(32.dp)
                .clip(RoundedCornerShape(999.dp))
                .background(MeshaColors.Brand)
                .clickable {
                    val currentPlayer = player
                    if (currentPlayer?.isPlaying == true) {
                        isPlaying = false
                    } else {
                        if (!armed) armed = true
                        isPlaying = true
                    }
                },
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = if (isPlaying) MeshaIcons.Pause else MeshaIcons.Play,
                contentDescription = if (isPlaying) "Pause video preview" else "Play video preview",
                tint = MeshaColors.OnBrand,
                modifier = Modifier.size(14.dp),
            )
        }
    }
}

@Composable
private fun ProofVideoProgressBar(positionMs: Long, durationMs: Long, modifier: Modifier = Modifier) {
    val safeDuration = durationMs.coerceAtLeast(0L)
    val safePosition = positionMs.coerceIn(0L, safeDuration.takeIf { it > 0L } ?: Long.MAX_VALUE)
    val progress = if (safeDuration > 0L) safePosition.toFloat() / safeDuration.toFloat() else 0f
    Column(
        modifier = modifier
            .fillMaxWidth()
            .background(MeshaColors.Bg.copy(alpha = 0.82f))
            .padding(horizontal = 10.dp, vertical = 6.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        LinearProgressIndicator(
            progress = { progress.coerceIn(0f, 1f) },
            modifier = Modifier.fillMaxWidth().height(3.dp),
            color = MeshaColors.Brand,
            trackColor = MeshaColors.Hair,
        )
        Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
            Text(
                text = formatPreviewTime(safePosition),
                color = MeshaColors.Ink,
                fontSize = 11.sp,
                fontWeight = FontWeight.SemiBold,
            )
            Text(
                text = if (safeDuration > 0L) formatPreviewTime(safeDuration) else "--:--",
                color = MeshaColors.Muted,
                fontSize = 11.sp,
                fontWeight = FontWeight.SemiBold,
            )
        }
    }
}

private fun formatPreviewTime(ms: Long): String {
    val totalSeconds = (ms / 1_000L).coerceAtLeast(0L)
    val minutes = totalSeconds / 60L
    val seconds = totalSeconds % 60L
    return "%d:%02d".format(minutes, seconds)
}

@Composable
private fun ProofVideoPoster(path: String) {
    val context = LocalContext.current
    val isRemote = path.startsWith("http://") || path.startsWith("https://")
    // Remote posters pull the frame over the network — never on the composing thread.
    fun extractFrame(): android.graphics.Bitmap? {
        val retriever = MediaMetadataRetriever()
        return try {
            if (isRemote) retriever.setDataSource(path, emptyMap()) else retriever.setDataSource(context, Uri.parse(path))
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
    val remotePoster by produceState<android.graphics.Bitmap?>(initialValue = null, key1 = path) {
        if (isRemote) value = withContext(Dispatchers.IO) { extractFrame() }
    }
    val bitmap = if (isRemote) remotePoster else remember(path) { extractFrame() }
    if (bitmap != null) {
        Image(
            bitmap = bitmap.asImageBitmap(),
            contentDescription = null,
            contentScale = ContentScale.Fit,
            modifier = Modifier.fillMaxSize(),
        )
    } else {
        Icon(MeshaIcons.Video, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(32.dp))
    }
}
