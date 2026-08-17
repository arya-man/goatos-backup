package sg.mesha.goatos.core.proofedit

import android.graphics.Bitmap
import android.media.MediaMetadataRetriever
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.gestures.Orientation
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.gestures.draggable
import androidx.compose.foundation.gestures.rememberDraggableState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.ContentCut
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.media3.common.MediaItem
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.SeekParameters
import androidx.media3.ui.PlayerView
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.withContext
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import java.io.File
import java.net.URI
import kotlin.math.roundToInt
import kotlin.math.roundToLong

private const val THUMB_COUNT = 10
private const val MIN_CLIP_MS = 500L

/** Thin visible grab bar inside a much wider invisible touch target. */
private val HANDLE_BAR_W = 13.dp
private val HANDLE_TOUCH_W = 40.dp
private val STRIP_H = 92.dp

private enum class PreviewMode { Selection, Stitched }

/**
 * Operator trim surface for a freshly recorded proof clip.
 *
 * The operator marks one or more keep-ranges and taps Done; [onDone] receives them in source
 * order. An empty list means nothing was cut and the processed clip should be used whole — the
 * host must NOT stitch in that case, because re-encoding an untouched clip costs a second
 * generation of quality for no change.
 *
 * Only shown when [ProofEditGate] allows it, so this composable never decides policy itself.
 *
 * Compression and the audit overlay are deliberately absent from this screen's copy and
 * behaviour: the proof pipeline applies both AFTER this screen, they are not operator choices,
 * and naming them here would leak pipeline mechanics into field copy.
 */
@androidx.annotation.OptIn(UnstableApi::class)
@Composable
fun ProofTrimEditor(
    sourceUri: String,
    onDone: (List<ProofClip>) -> Unit,
    onTelemetry: (event: String, props: Map<String, String>) -> Unit = { _, _ -> },
) {
    val sourceFile = remember(sourceUri) { File(URI(sourceUri)) }
    val metadata = remember(sourceUri) { ProofVideoStitcher.readVideoMetadata(sourceFile) }
    val durationMs = metadata.durationMs.coerceAtLeast(1L)

    // Held sorted + non-overlapping at all times, so what the list shows is exactly what is
    // handed to the stitcher, and an index here is a stable handle for removal.
    val clips = remember { mutableStateListOf<ProofClip>() }
    var selStartMs by remember { mutableStateOf(0L) }
    var selEndMs by remember { mutableStateOf(minOf(durationMs, 5_000L)) }
    var playheadMs by remember { mutableStateOf(0L) }
    var isPlaying by remember { mutableStateOf(false) }
    var previewMode by remember { mutableStateOf(PreviewMode.Selection) }
    var stitchedIndex by remember { mutableStateOf(0) }
    var thumbs by remember { mutableStateOf<List<Bitmap>>(emptyList()) }
    var showClearConfirm by remember { mutableStateOf(false) }

    val context = LocalContext.current
    val player = remember {
        // CLOSEST_SYNC keeps drag-scrubbing responsive; an exact seek per drag delta decodes from
        // the previous keyframe every time and lags behind the operator's finger.
        ExoPlayer.Builder(context).build().apply {
            setSeekParameters(SeekParameters.CLOSEST_SYNC)
        }
    }
    DisposableEffect(Unit) { onDispose { player.release() } }

    // Load the source and park on the first frame. Without this the player holds no media item
    // until playback starts and the preview renders BLACK on entry.
    LaunchedEffect(sourceUri) {
        player.setMediaItem(MediaItem.fromUri(sourceUri))
        player.prepare()
        player.playWhenReady = false
        player.seekTo(selStartMs)
        onTelemetry(
            ProofEditTelemetry.Events.EDIT_OPENED,
            mapOf(
                ProofEditTelemetry.Params.PROCESSING_STATE to ProofEditTelemetry.Step.EDITING,
                ProofEditTelemetry.Params.DURATION_BUCKET to ProofEditTelemetry.durationBucket(durationMs),
            ),
        )
    }

    LaunchedEffect(sourceUri) {
        thumbs = withContext(Dispatchers.Default) {
            val retriever = MediaMetadataRetriever()
            try {
                retriever.setDataSource(sourceFile.absolutePath)
                (0 until THUMB_COUNT).mapNotNull { i ->
                    val atUs = (durationMs * 1000L * i) / THUMB_COUNT
                    runCatching { // exception:exempt one filmstrip thumbnail failing is cosmetic — the strip renders with fewer frames and trimming still works; nothing to report
                        retriever.getScaledFrameAtTime(atUs, MediaMetadataRetriever.OPTION_CLOSEST_SYNC, 120, 200)
                    }.getOrNull()
                }
            } finally {
                retriever.release()
            }
        }
    }

    fun overlapsKept(s: Long, e: Long): Boolean = clips.any { s < it.endMs && e > it.startMs }

    fun stopPlayback() {
        player.pause()
        isPlaying = false
    }

    /** Back to the whole source, which is what scrubbing and selection playback seek within. */
    fun ensureScrubSource() {
        if (previewMode == PreviewMode.Stitched) {
            previewMode = PreviewMode.Selection
            player.setMediaItem(MediaItem.fromUri(sourceUri))
            player.prepare()
            player.playWhenReady = false
        }
    }

    fun seekPreviewTo(atMs: Long) {
        ensureScrubSource()
        stopPlayback()
        playheadMs = atMs
        player.seekTo(atMs)
    }

    fun playSelection() {
        ensureScrubSource()
        player.seekTo(selStartMs)
        player.play()
        isPlaying = true
    }

    /** Real stitched preview: the same clip list the stitcher will use, played back to back. */
    fun playStitched() {
        if (clips.isEmpty()) return
        previewMode = PreviewMode.Stitched
        stitchedIndex = 0
        player.setMediaItems(
            clips.map { clip ->
                MediaItem.Builder()
                    .setUri(sourceUri)
                    .setClippingConfiguration(
                        MediaItem.ClippingConfiguration.Builder()
                            .setStartPositionMs(clip.startMs)
                            .setEndPositionMs(clip.endMs)
                            .build(),
                    )
                    .build()
            },
        )
        player.prepare()
        player.play()
        isPlaying = true
    }

    LaunchedEffect(isPlaying, previewMode) {
        while (isPlaying) {
            if (previewMode == PreviewMode.Selection) {
                // Playing the WHOLE source, so position is absolute; stop at the selection end.
                playheadMs = player.currentPosition
                if (playheadMs >= selEndMs) {
                    player.pause()
                    player.seekTo(selStartMs)
                    playheadMs = selStartMs
                    isPlaying = false
                }
            } else {
                stitchedIndex = player.currentMediaItemIndex
                clips.getOrNull(stitchedIndex)?.let { playheadMs = it.startMs + player.currentPosition }
            }
            if (!player.isPlaying && player.playbackState == Player.STATE_ENDED) isPlaying = false
            delay(60L)
        }
    }

    val selectionBlocked = overlapsKept(selStartMs, selEndMs)
    val selectionTooShort = (selEndMs - selStartMs) < MIN_CLIP_MS

    if (showClearConfirm) {
        AlertDialog(
            onDismissRequest = { showClearConfirm = false },
            containerColor = MeshaColors.Surf,
            title = {
                Text(
                    stringResource(R.string.proof_trim_clear_title),
                    color = MeshaColors.Ink,
                    style = MeshaType.cardTitle,
                )
            },
            text = {
                Text(
                    stringResource(R.string.proof_trim_clear_body, clips.size),
                    color = MeshaColors.Muted,
                    style = MeshaType.caption,
                )
            },
            confirmButton = {
                TextButton(onClick = {
                    onTelemetry(
                        ProofEditTelemetry.Events.EDIT_CLEARED,
                        mapOf(ProofEditTelemetry.Params.PROCESSING_STATE to ProofEditTelemetry.Step.EDITING),
                    )
                    stopPlayback()
                    clips.clear()
                    selStartMs = 0L
                    selEndMs = minOf(durationMs, 5_000L)
                    showClearConfirm = false
                }) {
                    Text(
                        stringResource(R.string.proof_trim_clear_confirm),
                        color = MeshaColors.Danger,
                        style = MeshaType.bodyStrong,
                    )
                }
            },
            dismissButton = {
                TextButton(onClick = { showClearConfirm = false }) {
                    Text(
                        stringResource(R.string.proof_trim_cancel),
                        color = MeshaColors.Muted,
                        style = MeshaType.bodyStrong,
                    )
                }
            },
        )
    }

    Column(
        Modifier
            .fillMaxSize()
            .background(MeshaColors.PageBg)
            .windowInsetsPadding(WindowInsets.safeDrawing),
    ) {
        // Everything above the action bar scrolls; Done stays pinned so it is always reachable
        // no matter how many ranges are kept.
        Column(
            Modifier
                .weight(1f)
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 16.dp)
                .padding(top = 12.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Box(
                    Modifier
                        .size(38.dp)
                        .clip(RoundedCornerShape(11.dp))
                        .background(MeshaColors.BrandTint),
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(Icons.Filled.ContentCut, contentDescription = null, tint = MeshaColors.BrandD)
                }
                Spacer(Modifier.width(12.dp))
                Column {
                    Text(
                        stringResource(R.string.proof_trim_title),
                        color = MeshaColors.Ink,
                        style = MeshaType.screenTitle,
                    )
                    Text(
                        stringResource(R.string.proof_trim_subtitle),
                        color = MeshaColors.BrandD,
                        style = MeshaType.caption,
                    )
                }
            }

            Spacer(Modifier.height(14.dp))

            Box(
                Modifier
                    .fillMaxWidth()
                    .aspectRatio(16f / 10f)
                    .clip(RoundedCornerShape(16.dp))
                    .background(Color.Black),
            ) {
                AndroidView(
                    factory = { ctx -> PlayerView(ctx).apply { useController = false; this.player = player } },
                    modifier = Modifier.fillMaxSize(),
                )
                Box(Modifier.align(Alignment.BottomStart).padding(10.dp)) {
                    TrimPill(
                        text = if (previewMode == PreviewMode.Stitched && isPlaying) {
                            stringResource(R.string.proof_trim_part_of, stitchedIndex + 1, clips.size)
                        } else {
                            playheadMs.timeLabel()
                        },
                        tone = if (previewMode == PreviewMode.Stitched) TrimTone.Work else TrimTone.Neutral,
                    )
                }
            }

            Spacer(Modifier.height(12.dp))

            Row(
                Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(10.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                IconButton(
                    onClick = { if (isPlaying) stopPlayback() else playSelection() },
                    modifier = Modifier
                        .size(44.dp)
                        .clip(RoundedCornerShape(12.dp))
                        .background(MeshaColors.Surf2),
                ) {
                    Icon(
                        if (isPlaying) Icons.Filled.Pause else Icons.Filled.PlayArrow,
                        contentDescription = stringResource(
                            if (isPlaying) R.string.proof_trim_pause else R.string.proof_trim_play,
                        ),
                        tint = MeshaColors.BrandD,
                    )
                }
                Text(
                    "${selStartMs.timeLabel()} – ${selEndMs.timeLabel()}  (${(selEndMs - selStartMs).timeLabel()})",
                    color = if (selectionBlocked) MeshaColors.Danger else MeshaColors.Ink,
                    style = MeshaType.bodyStrong,
                )
            }

            Spacer(Modifier.height(12.dp))

            Filmstrip(
                thumbs = thumbs,
                durationMs = durationMs,
                selStartMs = selStartMs,
                selEndMs = selEndMs,
                playheadMs = playheadMs,
                keeps = clips,
                blocked = selectionBlocked,
                onSelectionChange = { s, e ->
                    // Seek to whichever edge actually moved, so the frame tracks the handle.
                    val movedEdge = when {
                        s != selStartMs && e != selEndMs -> s
                        e != selEndMs -> e
                        else -> s
                    }
                    selStartMs = s
                    selEndMs = e
                    seekPreviewTo(movedEdge)
                },
            )

            Spacer(Modifier.height(10.dp))

            Button(
                onClick = {
                    val keptEnd = selEndMs
                    clips.add(ProofClip(selStartMs, selEndMs))
                    val merged = clips.normalized()
                    clips.clear()
                    clips.addAll(merged)
                    onTelemetry(
                        ProofEditTelemetry.Events.EDIT_CLIP_KEPT,
                        mapOf(
                            ProofEditTelemetry.Params.PROCESSING_STATE to ProofEditTelemetry.Step.EDITING,
                            ProofEditTelemetry.Params.DURATION_BUCKET to
                                ProofEditTelemetry.durationBucket(clips.totalDurationMs()),
                        ),
                    )
                    // Move the window FORWARD, past what was just kept.
                    val gap = nextFreeGap(clips, durationMs, keptEnd)
                    selStartMs = gap.first
                    selEndMs = gap.second
                    seekPreviewTo(gap.first)
                },
                enabled = !selectionBlocked && !selectionTooShort,
                colors = ButtonDefaults.buttonColors(
                    containerColor = MeshaColors.Brand,
                    contentColor = MeshaColors.OnBrand,
                    disabledContainerColor = MeshaColors.Surf3,
                    disabledContentColor = MeshaColors.Faint,
                ),
                shape = RoundedCornerShape(16.dp),
                modifier = Modifier
                    .fillMaxWidth()
                    .height(52.dp),
            ) {
                Icon(Icons.Filled.Add, contentDescription = null)
                Spacer(Modifier.width(8.dp))
                Text(stringResource(R.string.proof_trim_keep_range), style = MeshaType.button)
            }

            // A disabled control always says WHY, rather than looking broken.
            if (selectionBlocked || selectionTooShort) {
                Spacer(Modifier.height(6.dp))
                Text(
                    stringResource(
                        if (selectionBlocked) R.string.proof_trim_already_kept else R.string.proof_trim_too_short,
                    ),
                    color = MeshaColors.Warn,
                    style = MeshaType.caption,
                )
            }

            Spacer(Modifier.height(16.dp))

            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Text(
                    stringResource(R.string.proof_trim_kept_header, clips.size),
                    color = MeshaColors.Muted,
                    style = MeshaType.sectionLabel,
                )
                Spacer(Modifier.weight(1f))
                if (clips.isNotEmpty()) {
                    Box(
                        Modifier
                            .clip(RoundedCornerShape(10.dp))
                            .background(MeshaColors.Brand.copy(alpha = 0.22f))
                            .pointerInput(clips.size) { detectTapGestures { playStitched() } }
                            .padding(horizontal = 12.dp, vertical = 8.dp),
                    ) {
                        Text(
                            stringResource(R.string.proof_trim_preview_joined),
                            color = MeshaColors.BrandD,
                            style = MeshaType.pill,
                        )
                    }
                    Spacer(Modifier.width(8.dp))
                    Box(
                        Modifier
                            .clip(RoundedCornerShape(10.dp))
                            .background(MeshaColors.Danger.copy(alpha = 0.18f))
                            .pointerInput(Unit) { detectTapGestures { showClearConfirm = true } }
                            .padding(horizontal = 12.dp, vertical = 8.dp),
                    ) {
                        Text(
                            stringResource(R.string.proof_trim_clear_all),
                            color = MeshaColors.Danger,
                            style = MeshaType.pill,
                        )
                    }
                }
            }

            Spacer(Modifier.height(8.dp))

            if (clips.isEmpty()) {
                Box(
                    Modifier
                        .fillMaxWidth()
                        .clip(RoundedCornerShape(14.dp))
                        .background(MeshaColors.Surf)
                        .border(1.dp, MeshaColors.Hair, RoundedCornerShape(14.dp))
                        .padding(18.dp),
                ) {
                    Text(
                        stringResource(R.string.proof_trim_empty_help),
                        color = MeshaColors.Muted,
                        style = MeshaType.caption,
                    )
                }
            } else {
                clips.forEachIndexed { index, clip ->
                    Row(
                        Modifier
                            .fillMaxWidth()
                            .padding(bottom = 8.dp)
                            .clip(RoundedCornerShape(14.dp))
                            .background(MeshaColors.Surf)
                            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(14.dp))
                            .padding(start = 14.dp, top = 6.dp, bottom = 6.dp, end = 6.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Box(
                            Modifier
                                .size(26.dp)
                                .clip(RoundedCornerShape(8.dp))
                                .background(MeshaColors.BrandTint),
                            contentAlignment = Alignment.Center,
                        ) {
                            Text("${index + 1}", color = MeshaColors.BrandD, style = MeshaType.pill)
                        }
                        Spacer(Modifier.width(12.dp))
                        Column(Modifier.weight(1f)) {
                            Text(
                                "${clip.startMs.timeLabel()} – ${clip.endMs.timeLabel()}",
                                color = MeshaColors.Ink,
                                style = MeshaType.cardTitle,
                            )
                            Text(
                                stringResource(R.string.proof_trim_kept_row, clip.durationMs.timeLabel()),
                                color = MeshaColors.Muted,
                                style = MeshaType.cardSubtitle,
                            )
                        }
                        // Remove by INDEX. Matching on value fails once the list has been merged.
                        IconButton(
                            onClick = {
                                if (index in clips.indices) clips.removeAt(index)
                                onTelemetry(
                                    ProofEditTelemetry.Events.EDIT_CLIP_REMOVED,
                                    mapOf(
                                        ProofEditTelemetry.Params.PROCESSING_STATE to
                                            ProofEditTelemetry.Step.EDITING,
                                    ),
                                )
                                seekPreviewTo(selStartMs)
                            },
                            modifier = Modifier.size(44.dp),
                        ) {
                            Icon(
                                Icons.Filled.Close,
                                contentDescription = stringResource(
                                    R.string.proof_trim_remove_range,
                                    index + 1,
                                ),
                                tint = MeshaColors.Danger,
                            )
                        }
                    }
                }

                Text(
                    stringResource(
                        R.string.proof_trim_output_summary,
                        clips.totalDurationMs().timeLabel(),
                        durationMs.timeLabel(),
                    ),
                    color = MeshaColors.BrandD,
                    style = MeshaType.caption,
                )
            }

            Spacer(Modifier.height(16.dp))
        }

        Column(
            Modifier
                .fillMaxWidth()
                .background(MeshaColors.Bg)
                .padding(horizontal = 16.dp, vertical = 12.dp),
        ) {
            Button(
                onClick = {
                    val kept = clips.toList()
                    onTelemetry(
                        ProofEditTelemetry.Events.EDIT_DONE,
                        mapOf(
                            ProofEditTelemetry.Params.PROCESSING_STATE to ProofEditTelemetry.Step.EDITING,
                            ProofEditTelemetry.Params.OUTCOME to
                                if (kept.isEmpty()) {
                                    ProofEditTelemetry.Outcome.UNEDITED
                                } else {
                                    ProofEditTelemetry.Outcome.EDITED
                                },
                            ProofEditTelemetry.Params.DURATION_BUCKET to
                                ProofEditTelemetry.durationBucket(
                                    if (kept.isEmpty()) durationMs else kept.totalDurationMs(),
                                ),
                        ),
                    )
                    onDone(kept)
                },
                colors = ButtonDefaults.buttonColors(
                    containerColor = MeshaColors.Brand,
                    contentColor = MeshaColors.OnBrand,
                ),
                shape = RoundedCornerShape(18.dp),
                modifier = Modifier
                    .fillMaxWidth()
                    .height(58.dp),
            ) {
                Text(
                    if (clips.isEmpty()) {
                        stringResource(R.string.proof_trim_done_whole)
                    } else {
                        stringResource(R.string.proof_trim_done_length, clips.totalDurationMs().timeLabel())
                    },
                    style = MeshaType.button,
                )
            }
        }
    }
}

/**
 * Next stretch of untouched footage AT OR AFTER [fromMs] — the selection moves forward past what
 * was just kept. Scanning from zero sent the window back to the start of the clip whenever an
 * earlier hole existed.
 */
internal fun nextFreeGap(kept: List<ProofClip>, durationMs: Long, fromMs: Long): Pair<Long, Long> {
    val sorted = kept.sortedBy { it.startMs }

    fun scanFrom(startCursor: Long, limit: Long): Pair<Long, Long>? {
        var cursor = startCursor
        sorted.forEach { clip ->
            if (clip.endMs <= cursor) return@forEach
            if (clip.startMs - cursor >= MIN_CLIP_MS) {
                return cursor to minOf(clip.startMs, cursor + 5_000L)
            }
            cursor = maxOf(cursor, clip.endMs)
        }
        return if (limit - cursor >= MIN_CLIP_MS) cursor to minOf(limit, cursor + 5_000L) else null
    }

    return scanFrom(fromMs, durationMs)
        ?: scanFrom(0L, durationMs)
        ?: (fromMs to minOf(durationMs, fromMs + MIN_CLIP_MS))
}

@Composable
private fun Filmstrip(
    thumbs: List<Bitmap>,
    durationMs: Long,
    selStartMs: Long,
    selEndMs: Long,
    playheadMs: Long,
    keeps: List<ProofClip>,
    blocked: Boolean,
    onSelectionChange: (Long, Long) -> Unit,
) {
    val density = LocalDensity.current
    var widthPx by remember { mutableStateOf(1f) }

    fun msAt(x: Float): Long = ((x / widthPx).coerceIn(0f, 1f) * durationMs).roundToLong()
    fun xOf(ms: Long): Float = (ms.toFloat() / durationMs.toFloat()).coerceIn(0f, 1f) * widthPx
    fun deltaToMs(deltaPx: Float): Long = ((deltaPx / widthPx) * durationMs).roundToLong()

    // Walls are anchored on the OPPOSITE, stationary edge — not on the edge being dragged.
    // Anchoring on the moving edge lets a fast drag jump straight over a kept range, because the
    // wall is then recomputed from a value that has already passed it.
    val leftWall = keeps.filter { it.endMs <= selEndMs }.maxOfOrNull { it.endMs } ?: 0L
    val rightWall = keeps.filter { it.startMs >= selStartMs }.minOfOrNull { it.startMs } ?: durationMs

    Column {
        Box(
            Modifier
                .fillMaxWidth()
                .height(STRIP_H)
                .onSizeChanged { widthPx = it.width.toFloat() },
        ) {
            Box(
                Modifier
                    .fillMaxSize()
                    .clip(RoundedCornerShape(12.dp))
                    .background(MeshaColors.Surf2)
                    .pointerInput(durationMs, keeps.size) {
                        detectTapGestures { offset ->
                            val at = msAt(offset.x)
                            if (keeps.none { at >= it.startMs && at < it.endMs }) {
                                val wallR = keeps.filter { it.startMs >= at }.minOfOrNull { it.startMs }
                                    ?: durationMs
                                onSelectionChange(at, minOf(at + 5_000L, wallR))
                            }
                        }
                    },
            ) {
                Row(Modifier.fillMaxSize()) {
                    thumbs.forEach { bmp ->
                        Image(
                            bitmap = bmp.asImageBitmap(),
                            contentDescription = null,
                            contentScale = ContentScale.Crop,
                            modifier = Modifier
                                .weight(1f)
                                .fillMaxSize(),
                        )
                    }
                }

                Canvas(Modifier.fillMaxSize()) {
                    val startX = (selStartMs.toFloat() / durationMs) * size.width
                    val endX = (selEndMs.toFloat() / durationMs) * size.width

                    drawRect(Color.Black.copy(alpha = 0.62f), size = Size(startX, size.height))
                    drawRect(
                        Color.Black.copy(alpha = 0.62f),
                        topLeft = Offset(endX, 0f),
                        size = Size(size.width - endX, size.height),
                    )

                    // Kept footage is drawn locked — it cannot be selected again until removed.
                    keeps.forEach { clip ->
                        val sx = (clip.startMs.toFloat() / durationMs) * size.width
                        val ex = (clip.endMs.toFloat() / durationMs) * size.width
                        drawRect(
                            MeshaColors.Brand.copy(alpha = 0.32f),
                            topLeft = Offset(sx, 0f),
                            size = Size((ex - sx).coerceAtLeast(2f), size.height),
                        )
                        drawRect(
                            MeshaColors.Brand,
                            topLeft = Offset(sx, size.height - 8f),
                            size = Size((ex - sx).coerceAtLeast(2f), 8f),
                        )
                    }

                    val px = (playheadMs.toFloat() / durationMs) * size.width
                    drawRect(Color.White, topLeft = Offset(px - 1.5f, 0f), size = Size(3f, size.height))
                }
            }

            val selColor = if (blocked) MeshaColors.Danger else MeshaColors.Brand
            Canvas(Modifier.fillMaxSize()) {
                val startX = (selStartMs.toFloat() / durationMs) * size.width
                val endX = (selEndMs.toFloat() / durationMs) * size.width
                drawRect(selColor, topLeft = Offset(startX, 0f), size = Size((endX - startX).coerceAtLeast(2f), 5f))
                drawRect(
                    selColor,
                    topLeft = Offset(startX, size.height - 5f),
                    size = Size((endX - startX).coerceAtLeast(2f), 5f),
                )
            }

            // Hold BETWEEN the handles to slide the whole window as a rigid block.
            val touchHalfPx = with(density) { HANDLE_TOUCH_W.toPx() } / 2f
            val bodyStartPx = xOf(selStartMs) + touchHalfPx
            val bodyWidthPx = (xOf(selEndMs) - touchHalfPx) - bodyStartPx
            if (bodyWidthPx > 0f) {
                Box(
                    Modifier
                        .offset { IntOffset(bodyStartPx.roundToInt(), 0) }
                        .width(with(density) { bodyWidthPx.toDp() })
                        .height(STRIP_H)
                        .draggable(
                            orientation = Orientation.Horizontal,
                            state = rememberDraggableState { deltaPx ->
                                val shift = deltaToMs(deltaPx)
                                val length = selEndMs - selStartMs
                                val newStart = (selStartMs + shift).coerceIn(leftWall, rightWall - length)
                                onSelectionChange(newStart, newStart + length)
                            },
                        ),
                )
            }

            TrimHandle(
                edgeXPx = xOf(selStartMs),
                color = selColor,
                contentDescription = stringResource(R.string.proof_trim_handle_start),
                onDeltaMs = { shift ->
                    onSelectionChange(
                        (selStartMs + shift).coerceIn(leftWall, selEndMs - MIN_CLIP_MS),
                        selEndMs,
                    )
                },
                deltaToMs = ::deltaToMs,
            )
            TrimHandle(
                edgeXPx = xOf(selEndMs),
                color = selColor,
                contentDescription = stringResource(R.string.proof_trim_handle_end),
                onDeltaMs = { shift ->
                    onSelectionChange(
                        selStartMs,
                        (selEndMs + shift).coerceIn(selStartMs + MIN_CLIP_MS, rightWall),
                    )
                },
                deltaToMs = ::deltaToMs,
            )
        }

        Spacer(Modifier.height(6.dp))
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
            Text(0L.timeLabel(), color = MeshaColors.Faint, style = MeshaType.caption)
            Text(durationMs.timeLabel(), color = MeshaColors.Faint, style = MeshaType.caption)
        }
    }
}

@Composable
private fun TrimHandle(
    edgeXPx: Float,
    color: Color,
    contentDescription: String,
    onDeltaMs: (Long) -> Unit,
    deltaToMs: (Float) -> Long,
) {
    val density = LocalDensity.current
    val touchHalfPx = with(density) { HANDLE_TOUCH_W.toPx() } / 2f
    Box(
        Modifier
            .offset { IntOffset((edgeXPx - touchHalfPx).roundToInt(), 0) }
            .width(HANDLE_TOUCH_W)
            .height(STRIP_H)
            .semantics { this.contentDescription = contentDescription }
            .draggable(
                orientation = Orientation.Horizontal,
                state = rememberDraggableState { deltaPx -> onDeltaMs(deltaToMs(deltaPx)) },
            ),
        contentAlignment = Alignment.Center,
    ) {
        Box(
            Modifier
                .width(HANDLE_BAR_W)
                .height(STRIP_H)
                .clip(RoundedCornerShape(6.dp))
                .background(color),
            contentAlignment = Alignment.Center,
        ) {
            Box(
                Modifier
                    .width(2.dp)
                    .height(24.dp)
                    .clip(RoundedCornerShape(1.dp))
                    .background(MeshaColors.OnBrand.copy(alpha = 0.8f)),
            )
        }
    }
}

private enum class TrimTone { Work, Neutral }

@Composable
private fun TrimPill(text: String, tone: TrimTone) {
    val background = when (tone) {
        TrimTone.Work -> MeshaColors.Brand.copy(alpha = 0.22f)
        TrimTone.Neutral -> MeshaColors.Surf3
    }
    val foreground = when (tone) {
        TrimTone.Work -> MeshaColors.BrandD
        TrimTone.Neutral -> MeshaColors.Muted
    }
    Box(
        Modifier
            .clip(RoundedCornerShape(10.dp))
            .background(background)
            .padding(horizontal = 10.dp, vertical = 5.dp),
    ) {
        Text(text = text, color = foreground, style = MeshaType.pill)
    }
}

internal fun Long.timeLabel(): String {
    val totalSeconds = this / 1000L
    val tenths = (this % 1000L) / 100L
    return "%d:%02d.%d".format(totalSeconds / 60L, totalSeconds % 60L, tenths)
}
