package sg.mesha.goatos.core.ui

import android.graphics.BitmapFactory
import android.media.MediaMetadataRetriever
import android.net.Uri
import androidx.annotation.OptIn
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
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
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.media3.common.MediaItem
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.PlayerView
import java.io.IOException
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType

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
    val bitmap = remember(path) {
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
    if (bitmap != null) {
        Image(
            bitmap = bitmap.asImageBitmap(),
            contentDescription = null,
            contentScale = ContentScale.Fit,
            modifier = modifier.fillMaxWidth().aspectRatio(16f / 9f).clip(RoundedCornerShape(12.dp)),
        )
    }
}

@OptIn(UnstableApi::class)
@Composable
private fun ProofVideoPreview(path: String, modifier: Modifier = Modifier) {
    val context = LocalContext.current
    var isPlaying by remember(path) { mutableStateOf(false) }
    var armed by remember(path) { mutableStateOf(false) }
    var firstFrameRendered by remember(path) { mutableStateOf(false) }
    val player = remember(path, armed) {
        if (!armed) {
            null
        } else {
            ExoPlayer.Builder(context).build().apply {
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
        Text(
            text = if (isPlaying) "Pause" else "Play",
            color = MeshaColors.OnBrand,
            style = MeshaType.cta,
            modifier = Modifier
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
                }
                .padding(horizontal = 14.dp, vertical = 8.dp),
        )
    }
}

@Composable
private fun ProofVideoPoster(path: String) {
    val context = LocalContext.current
    val bitmap = remember(path) {
        val retriever = MediaMetadataRetriever()
        try {
            retriever.setDataSource(context, Uri.parse(path))
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
