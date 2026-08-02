package sg.mesha.goatos.feature.verify

import android.net.Uri
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.border
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.PlayerView
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncStatusIndicator
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle

// telemetry:exempt: pure stateless renderer — AnalyticsPort/funnel wiring lives in
// VerifyDetailViewModel (:app), which owns every side effect this screen triggers.
/**
 * The standalone Verifier section's detail screen (context/architecture/verifier-app-and-flow.md):
 * play the video(s) + context (shed/park/operator/timestamp from the capture metadata), then
 * Approve or Reject + MANDATORY reason. Communication/penalty/action stay with the authority
 * (Park Head/Director/CEO on admin-web) — this screen only records the verdict.
 */

/** One playable proof clip — [signedUrl] is streamed directly (never proxied/downloaded whole). */
data class VerifyMediaItem(
    val signedUrl: String,
    val mimeType: String,
    val proofSubject: String,
    val taskTitle: String? = null,
    val answer: String? = null,
)

/** The four fixed context dimensions the spec calls out (shed/park/operator/timestamp). The
 *  LABEL for each is client UI chrome, resolved from a string resource by [ContextCard] — only
 *  [VerifyContextRow.value] is backend data. */
enum class VerifyContextKind { SHED, PARK, OPERATOR, CAPTURED_AT }

/** One context line: [kind] picks the localized label, [value] is the backend-composed
 *  display string (shed/park/operator name, or a formatted capture timestamp). */
data class VerifyContextRow(val kind: VerifyContextKind, val value: String)

@Immutable
data class VerifyDetailUiState(
    val itemId: String = "",
    val category: String = "",
    val categoryLabel: String = "",
    // Backend-composed subject for this item (e.g. a feed-packing item's shed-session "Session 1").
    // Shown as a subtitle under the category; null hides it. Category-agnostic — whatever the producer
    // set as the subject renders verbatim.
    val subjectLabel: String? = null,
    val media: List<VerifyMediaItem> = emptyList(),
    val context: List<VerifyContextRow> = emptyList(),
    val statusTone: VerifyTone = VerifyTone.PENDING,
    val rowVersion: Int = 1,
    val isCloseMode: Boolean = false,
    val isCloseEnabled: Boolean = false,
    val verdictReason: String? = null,
    /** False once a verdict has already been recorded (server or a just-submitted local
     *  optimistic state) — the buttons disable rather than allow a second conflicting verdict. */
    // Fail closed while the requested item is absent/loading. The ViewModel enables decisions
    // only after a real pending row with resolvable evidence arrives from Room.
    val isDecisionEnabled: Boolean = false,
    val decisionUnavailableReason: VerifyDecisionUnavailableReason = VerifyDecisionUnavailableReason.NONE,
    val isSubmitting: Boolean = false,
    // Offline-first sync state (docs/decisions/android-offline-first.md).
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
    val errorMessage: String? = null,
    val autoCloseAfterDecision: Boolean = false,
)

enum class VerifyDecisionUnavailableReason { NONE, ALREADY_DECIDED, EVIDENCE_UNAVAILABLE }

enum class VideoPlaybackAction { PLAY_STARTED, WATCH_SUMMARY, PLAYBACK_ERROR, FULLSCREEN_OPENED }

sealed interface VerifyDetailEvent {
    data object Close : VerifyDetailEvent
    data object Approve : VerifyDetailEvent
    /** [reason] is always non-blank — the reject dialog below refuses to emit this otherwise. */
    data class Reject(val reason: String) : VerifyDetailEvent
    data object Refresh : VerifyDetailEvent
    data class VideoPlayback(
        val proofSubject: String,
        val mimeType: String,
        val action: VideoPlaybackAction,
        val reason: String? = null,
        val watchTimeMs: Long = 0,
        val durationMs: Long = 0,
        val positionMs: Long = 0,
        val percentWatched: Int = 0,
        val seekCount: Int = 0,
        val replayCount: Int = 0,
        val bufferingTimeMs: Long = 0,
    ) : VerifyDetailEvent
}

@Composable
fun VerifyDetailScreen(
    state: VerifyDetailUiState,
    onEvent: (VerifyDetailEvent) -> Unit = {},
    modifier: Modifier = Modifier,
    videoControlsEnabled: Boolean = false,
) {
    var showRejectDialog by remember { mutableStateOf(false) }
    RefreshOnResume { onEvent(VerifyDetailEvent.Refresh) }

    Column(modifier = modifier.fillMaxSize().background(MeshaColors.Bg)) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .background(MeshaColors.Surf, shape = RoundedCornerShape(topStart = 26.dp, topEnd = 26.dp)),
        ) {
            DetailHeader(state = state, onClose = { onEvent(VerifyDetailEvent.Close) })
            LazyColumn(modifier = Modifier.fillMaxWidth().weight(1f), contentPadding = PaddingValues(bottom = 20.dp)) {
                if (state.media.isEmpty()) {
                    item {
                        EmptyState(
                            title = stringResource(R.string.verify_detail_no_media),
                            icon = MeshaIcons.Video,
                            tone = EmptyTone.Warn,
                            modifier = Modifier.padding(horizontal = 16.dp),
                        )
                    }
                } else {
                    itemsIndexed(
                        items = state.media,
                        key = { _, media -> media.signedUrl },
                        contentType = { _, _ -> "verification_media" },
                    ) { _, media ->
                        Column(
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(horizontal = 16.dp, vertical = 8.dp),
                        ) {
                            val recordedAnswer = media.answer?.takeIf { it.isNotBlank() }
                            media.taskTitle?.takeIf { it.isNotBlank() }?.let { taskTitle ->
                                Text(
                                    text = taskTitle,
                                    color = MeshaColors.Ink,
                                    style = MeshaType.cardTitle,
                                    modifier = Modifier.padding(bottom = if (recordedAnswer == null) 8.dp else 3.dp),
                                )
                            }
                            recordedAnswer?.let { answer ->
                                Text(
                                    text = stringResource(R.string.verify_detail_recorded_answer, answer),
                                    color = MeshaColors.Muted,
                                    style = MeshaType.body,
                                    modifier = Modifier.padding(bottom = 8.dp),
                                )
                            }
                            VerifyVideoPlayer(
                                media = media,
                                onPlayback = { onEvent(it) },
                                controlsEnabled = videoControlsEnabled,
                                modifier = Modifier.fillMaxWidth(),
                            )
                        }
                    }
                }
                item { ContextCard(state.context) }
                item {
                    Box(modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 12.dp)) {
                        StatusPill(tone = state.statusTone)
                    }
                }
                state.verdictReason?.takeIf { it.isNotBlank() }?.let { reason ->
                    item { RejectionReasonCard(reason = reason) }
                }
                item {
                    if (!state.isCloseMode) {
                        DecisionRow(
                            enabled = state.isDecisionEnabled && !state.isSubmitting,
                            unavailableReason = state.decisionUnavailableReason,
                            isSubmitting = state.isSubmitting,
                            onApprove = { onEvent(VerifyDetailEvent.Approve) },
                            onReject = { showRejectDialog = true },
                        )
                    }
                }
                state.errorMessage?.let { message ->
                    item {
                        Text(
                            text = message,
                            color = MeshaColors.Danger,
                            style = MeshaType.cta,
                            modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 8.dp),
                        )
                    }
                }
            }
        }
    }

    if (showRejectDialog) {
        RejectReasonDialog(
            onConfirm = { reason ->
                showRejectDialog = false
                onEvent(VerifyDetailEvent.Reject(reason))
            },
            onDismiss = { showRejectDialog = false },
        )
    }
}

@Composable
private fun DetailHeader(state: VerifyDetailUiState, onClose: () -> Unit) {
    Row(
        verticalAlignment = Alignment.Top,
        modifier = Modifier.fillMaxWidth().padding(start = 20.dp, end = 12.dp, top = 16.dp, bottom = 10.dp),
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = state.categoryLabel,
                color = MeshaColors.Ink,
                style = MeshaType.headerTitle,
            )
            state.subjectLabel?.takeIf { it.isNotBlank() }?.let { subject ->
                Text(
                    text = subject,
                    color = MeshaColors.Muted,
                    style = MeshaType.listTitle,
                    modifier = Modifier.padding(top = 2.dp),
                )
            }
            SyncStatusIndicator(
                isRefreshing = state.isRefreshing,
                lastSyncedAt = state.lastSyncedAt,
                hasData = state.lastSyncedAt != null,
                isOffline = state.isOffline,
                modifier = Modifier.padding(top = 2.dp),
            )
        }
        Box(
            modifier = Modifier
                .size(48.dp)
                .background(MeshaColors.Surf2, shape = RoundedCornerShape(12.dp))
                .clickable { onClose() },
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = MeshaIcons.Close,
                contentDescription = stringResource(R.string.verify_detail_close),
                tint = MeshaColors.Muted,
                modifier = Modifier.size(16.dp),
            )
        }
    }
}

/**
 * Streamed signed-URL video playback (never a whole-file download). Off-Main decode is
 * ExoPlayer's own concern; this composable's job is lifecycle correctness — the player is
 * built once per [media] and RELEASED on dispose (mobile-anti-patterns: "release
 * camera/recorder/BT capture + observers on lifecycle stop"), so navigating away or the queue
 * recycling this row never leaks a player instance.
 */
@Composable
private fun VerifyVideoPlayer(
    media: VerifyMediaItem,
    onPlayback: (VerifyDetailEvent.VideoPlayback) -> Unit,
    modifier: Modifier = Modifier,
    controlsEnabled: Boolean = false,
) {
    val context = LocalContext.current
    val view = LocalView.current
    var isFullscreen by rememberSaveable(media.signedUrl) { mutableStateOf(false) }
    var isPlaying by remember { mutableStateOf(false) }
    val currentOnPlayback by rememberUpdatedState(onPlayback)
    val player = remember(media.signedUrl) {
        ExoPlayer.Builder(context).build().apply {
            setMediaItem(MediaItem.fromUri(Uri.parse(media.signedUrl)))
            prepare()
            playWhenReady = false
        }
    }
    DisposableEffect(view) {
        val previous = view.keepScreenOn
        view.keepScreenOn = true
        onDispose { view.keepScreenOn = previous }
    }
    DisposableEffect(player) {
        val tracker = VideoPlaybackTracker(media = media, onPlayback = currentOnPlayback)
        val listener = object : Player.Listener {
            override fun onIsPlayingChanged(isPlayingNow: Boolean) {
                isPlaying = isPlayingNow
                tracker.onPlayingChanged(isPlayingNow, player.duration, player.currentPosition)
            }

            override fun onPlaybackStateChanged(playbackState: Int) {
                tracker.onPlaybackStateChanged(playbackState, player.duration, player.currentPosition)
            }

            override fun onPositionDiscontinuity(
                oldPosition: Player.PositionInfo,
                newPosition: Player.PositionInfo,
                reason: Int,
            ) {
                tracker.onPositionDiscontinuity(reason)
            }

            override fun onPlayerError(error: PlaybackException) {
                tracker.onError(error.message ?: error.errorCodeName)
            }
        }
        player.addListener(listener)
        onDispose {
            tracker.flush(player.duration, player.currentPosition)
            player.removeListener(listener)
            player.release()
        }
    }
    Box(
        modifier = modifier
            .aspectRatio(16f / 9f)
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Bg),
    ) {
        AndroidView(
            factory = { ctx ->
                PlayerView(ctx).apply {
                    this.player = player
                    useController = controlsEnabled
                    keepScreenOn = true
                }
            },
            // The factory runs ONCE. controlsEnabled comes from the bootstrap flag, which resolves
            // asynchronously, so a player composed before the flag lands would keep whatever value
            // it was built with for the rest of the session -- leaving leadership without the seek
            // controls they are entitled to. Re-apply it on every recomposition.
            //
            // The verifier stays without a controller by design: they must WATCH the proof, not
            // scrub it. Everyone else who may see the video may seek within it.
            update = { view -> view.useController = controlsEnabled },
            modifier = Modifier.fillMaxSize(),
        )
        PlayPauseButton(
            isPlaying = isPlaying,
            onClick = {
                if (player.isPlaying) {
                    player.pause()
                } else {
                    player.play()
                }
            },
            modifier = Modifier.align(Alignment.Center),
        )
        // Maintainer decision 2026-08-02: a verifier gets PLAY/PAUSE ONLY. They must watch the
        // proof as recorded — no scrubbing (useController stays false for them) and no fullscreen
        // re-frame. Everyone else who may see the video keeps both. `controlsEnabled` is the same
        // bootstrap-driven flag that governs the seek controller, so the two can never disagree.
        if (controlsEnabled) {
            VideoFullscreenButton(
                onClick = {
                    currentOnPlayback(
                    VerifyDetailEvent.VideoPlayback(
                        proofSubject = media.proofSubject,
                        mimeType = media.mimeType,
                        action = VideoPlaybackAction.FULLSCREEN_OPENED,
                        durationMs = player.duration.coerceAtLeast(0L),
                        positionMs = player.currentPosition.coerceAtLeast(0L),
                    ),
                )
                    player.playWhenReady = false
                    isFullscreen = true
                },
                modifier = Modifier.align(Alignment.TopEnd).padding(8.dp),
            )
        }
    }

    if (isFullscreen) {
        FullscreenVideoDialog(
            media = media,
            onPlayback = onPlayback,
            controlsEnabled = controlsEnabled,
            onDismiss = { isFullscreen = false },
        )
    }
}

@Composable
private fun RejectionReasonCard(reason: String) {
    Column(
        modifier = Modifier
            .padding(horizontal = 16.dp, vertical = 8.dp)
            .fillMaxWidth()
            .background(MeshaColors.DangerX, shape = RoundedCornerShape(16.dp))
            .border(1.dp, MeshaColors.Danger.copy(alpha = 0.28f), shape = RoundedCornerShape(16.dp))
            .padding(14.dp),
    ) {
        Text(
            text = stringResource(R.string.verify_detail_rejection_reason_title),
            color = MeshaColors.Danger,
            // design-system:ignore: 12sp/W800 has no close token — `cardSubtitle` is 12sp but W500,
            // and the only W800 styles (`button` 15sp, `dayNumber` 15sp) are 3sp larger.
            fontSize = 12.sp,
            fontWeight = FontWeight.W800,
        )
        Text(
            text = reason,
            color = MeshaColors.Ink,
            style = MeshaType.listTitle,
            modifier = Modifier.padding(top = 4.dp),
        )
    }
}

@Composable
private fun VideoFullscreenButton(onClick: () -> Unit, modifier: Modifier = Modifier) {
    Box(
        modifier = modifier
            .size(44.dp)
            .background(MeshaColors.Ink.copy(alpha = 0.72f), shape = RoundedCornerShape(12.dp)),
        contentAlignment = Alignment.Center,
    ) {
        IconButton(onClick = onClick, modifier = Modifier.fillMaxSize()) {
            Icon(
                imageVector = MeshaIcons.Expand,
                contentDescription = stringResource(R.string.verify_detail_fullscreen),
                tint = MeshaColors.Surf,
                modifier = Modifier.size(20.dp),
            )
        }
    }
}

@Composable
private fun PlayPauseButton(isPlaying: Boolean, onClick: () -> Unit, modifier: Modifier = Modifier) {
    Box(
        modifier = modifier
            .size(58.dp)
            .background(MeshaColors.Ink.copy(alpha = 0.72f), shape = RoundedCornerShape(18.dp))
            .clickable(onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            imageVector = if (isPlaying) MeshaIcons.Pause else MeshaIcons.Play,
            contentDescription = null,
            tint = MeshaColors.Surf,
            modifier = Modifier.size(24.dp),
        )
    }
}

@Composable
private fun FullscreenVideoDialog(
    media: VerifyMediaItem,
    onPlayback: (VerifyDetailEvent.VideoPlayback) -> Unit,
    onDismiss: () -> Unit,
    controlsEnabled: Boolean = false,
) {
    val context = LocalContext.current
    var isPlaying by remember { mutableStateOf(true) }
    val currentOnPlayback by rememberUpdatedState(onPlayback)
    val player = remember(media.signedUrl) {
        ExoPlayer.Builder(context).build().apply {
            setMediaItem(MediaItem.fromUri(Uri.parse(media.signedUrl)))
            prepare()
            playWhenReady = true
        }
    }
    DisposableEffect(player) {
        val tracker = VideoPlaybackTracker(media = media, onPlayback = currentOnPlayback)
        val listener = object : Player.Listener {
            override fun onIsPlayingChanged(isPlayingNow: Boolean) {
                isPlaying = isPlayingNow
                tracker.onPlayingChanged(isPlayingNow, player.duration, player.currentPosition)
            }

            override fun onPlaybackStateChanged(playbackState: Int) {
                tracker.onPlaybackStateChanged(playbackState, player.duration, player.currentPosition)
            }

            override fun onPositionDiscontinuity(
                oldPosition: Player.PositionInfo,
                newPosition: Player.PositionInfo,
                reason: Int,
            ) {
                tracker.onPositionDiscontinuity(reason)
            }

            override fun onPlayerError(error: PlaybackException) {
                tracker.onError(error.message ?: error.errorCodeName)
            }
        }
        player.addListener(listener)
        onDispose {
            tracker.flush(player.duration, player.currentPosition)
            player.removeListener(listener)
            player.release()
        }
    }

    Dialog(
        onDismissRequest = onDismiss,
        properties = DialogProperties(
            usePlatformDefaultWidth = false,
            decorFitsSystemWindows = false,
        ),
    ) {
        Box(modifier = Modifier.fillMaxSize().background(MeshaColors.Ink)) {
            AndroidView(
                factory = { ctx ->
                    PlayerView(ctx).apply {
                        this.player = player
                        useController = controlsEnabled
                        keepScreenOn = true
                    }
                },
                // Same reason as the inline player: the factory runs once, the bootstrap flag lands later.
                update = { view -> view.useController = controlsEnabled },
                modifier = Modifier.fillMaxSize(),
            )
            PlayPauseButton(
                isPlaying = isPlaying,
                onClick = {
                    if (player.isPlaying) {
                        player.pause()
                    } else {
                        player.play()
                    }
                },
                modifier = Modifier.align(Alignment.Center),
            )
            Box(
                modifier = Modifier
                    .align(Alignment.TopEnd)
                    .padding(16.dp)
                    .size(48.dp)
                    .background(MeshaColors.Ink.copy(alpha = 0.72f), shape = RoundedCornerShape(12.dp))
                    .clickable { onDismiss() },
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    imageVector = MeshaIcons.Close,
                    contentDescription = stringResource(R.string.verify_detail_exit_fullscreen),
                    tint = MeshaColors.Surf,
                    modifier = Modifier.size(18.dp),
                )
            }
        }
    }
}

private class VideoPlaybackTracker(
    private val media: VerifyMediaItem,
    private val onPlayback: (VerifyDetailEvent.VideoPlayback) -> Unit,
    private val nowMs: () -> Long = { System.currentTimeMillis() },
) {
    private var playStarted = false
    private var playingSinceMs: Long? = null
    private var watchTimeMs: Long = 0
    private var bufferingSinceMs: Long? = null
    private var bufferingTimeMs: Long = 0
    private var seekCount: Int = 0
    private var replayCount: Int = 0
    private var ended = false
    private var flushed = false

    fun onPlayingChanged(isPlaying: Boolean, durationMs: Long, positionMs: Long) {
        if (isPlaying) {
            if (!playStarted) {
                playStarted = true
                onPlayback(
                    VerifyDetailEvent.VideoPlayback(
                        proofSubject = media.proofSubject,
                        mimeType = media.mimeType,
                        action = VideoPlaybackAction.PLAY_STARTED,
                        durationMs = durationMs.safeMediaMs(),
                        positionMs = positionMs.safeMediaMs(),
                    ),
                )
            } else if (ended) {
                replayCount += 1
                ended = false
            }
            if (playingSinceMs == null) playingSinceMs = nowMs()
        } else {
            accrueWatchTime()
        }
    }

    fun onPlaybackStateChanged(playbackState: Int, durationMs: Long, positionMs: Long) {
        when (playbackState) {
            Player.STATE_BUFFERING -> {
                if (bufferingSinceMs == null) bufferingSinceMs = nowMs()
            }
            Player.STATE_READY -> accrueBufferingTime()
            Player.STATE_ENDED -> {
                ended = true
                accrueWatchTime()
                flush(durationMs, positionMs)
            }
        }
    }

    fun onPositionDiscontinuity(reason: Int) {
        if (reason == Player.DISCONTINUITY_REASON_SEEK) seekCount += 1
    }

    fun onError(reason: String) {
        onPlayback(
            VerifyDetailEvent.VideoPlayback(
                proofSubject = media.proofSubject,
                mimeType = media.mimeType,
                action = VideoPlaybackAction.PLAYBACK_ERROR,
                reason = reason,
            ),
        )
    }

    fun flush(durationMs: Long, positionMs: Long) {
        if (flushed || !playStarted) return
        flushed = true
        accrueWatchTime()
        accrueBufferingTime()
        val safeDuration = durationMs.safeMediaMs()
        val safePosition = positionMs.safeMediaMs()
        val percentWatched = if (safeDuration > 0) {
            ((safePosition.coerceAtMost(safeDuration) * 100) / safeDuration).toInt()
        } else {
            0
        }
        onPlayback(
            VerifyDetailEvent.VideoPlayback(
                proofSubject = media.proofSubject,
                mimeType = media.mimeType,
                action = VideoPlaybackAction.WATCH_SUMMARY,
                watchTimeMs = watchTimeMs,
                durationMs = safeDuration,
                positionMs = safePosition,
                percentWatched = percentWatched,
                seekCount = seekCount,
                replayCount = replayCount,
                bufferingTimeMs = bufferingTimeMs,
            ),
        )
    }

    private fun accrueWatchTime() {
        val started = playingSinceMs ?: return
        watchTimeMs += (nowMs() - started).coerceAtLeast(0)
        playingSinceMs = null
    }

    private fun accrueBufferingTime() {
        val started = bufferingSinceMs ?: return
        bufferingTimeMs += (nowMs() - started).coerceAtLeast(0)
        bufferingSinceMs = null
    }
}

private fun Long.safeMediaMs(): Long = takeIf { it > 0 } ?: 0L

@Composable
private fun ContextCard(rows: List<VerifyContextRow>) {
    if (rows.isEmpty()) return
    val locale = LocalContext.current.resources.configuration.locales[0]
    Column(
        modifier = Modifier
            .padding(horizontal = 16.dp, vertical = 8.dp)
            .fillMaxWidth()
            .background(MeshaColors.Surf, shape = RoundedCornerShape(16.dp))
            .border(1.dp, MeshaColors.Hair, shape = RoundedCornerShape(16.dp))
            .padding(horizontal = 15.dp),
    ) {
        Text(
            text = stringResource(R.string.verify_detail_context_title),
            color = MeshaColors.Faint,
            style = MeshaType.overline,
            modifier = Modifier.padding(top = 12.dp, bottom = 4.dp),
        )
        rows.forEachIndexed { index, row ->
            Row(
                verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier.fillMaxWidth().padding(vertical = 11.dp),
            ) {
                // design-system:ignore: 13sp/W400 has no close token — the only W400 style is
                // `body` at 14.5sp, which would render this label larger than its 13.5sp value.
                Text(text = contextKindLabel(row.kind), color = MeshaColors.Muted, fontSize = 13.sp, modifier = Modifier.weight(1f))
                val displayValue = remember(row.value, row.kind, locale) {
                    if (row.kind == VerifyContextKind.CAPTURED_AT) {
                        formatCapturedAt(row.value, locale, ZoneId.of("Asia/Kolkata"))
                    } else {
                        row.value
                    }
                }
                Text(text = displayValue, color = MeshaColors.Ink, style = MeshaType.listTitle)
            }
            if (index != rows.lastIndex) {
                HorizontalDivider(thickness = 1.dp, color = MeshaColors.Surf2)
            }
        }
    }
}

internal fun formatCapturedAt(raw: String, locale: java.util.Locale, zoneId: ZoneId): String =
    runCatching {
        DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM, FormatStyle.SHORT)
            .withLocale(locale)
            .withZone(zoneId)
            .format(Instant.parse(raw))
    }.getOrDefault(raw)

@Composable
private fun contextKindLabel(kind: VerifyContextKind): String = when (kind) {
    VerifyContextKind.SHED -> stringResource(R.string.verify_detail_shed_label)
    VerifyContextKind.PARK -> stringResource(R.string.verify_detail_park_label)
    VerifyContextKind.OPERATOR -> stringResource(R.string.verify_detail_operator_label)
    VerifyContextKind.CAPTURED_AT -> stringResource(R.string.verify_detail_captured_label)
}

@Composable
private fun DecisionRow(
    enabled: Boolean,
    unavailableReason: VerifyDecisionUnavailableReason,
    isSubmitting: Boolean,
    onApprove: () -> Unit,
    onReject: () -> Unit,
) {
    if (isSubmitting) {
        Row(
            modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 14.dp),
            horizontalArrangement = Arrangement.Center,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            CircularProgressIndicator(modifier = Modifier.size(16.dp), color = MeshaColors.Brand, strokeWidth = 2.dp)
            Spacer(Modifier.size(8.dp))
            Text(
                text = stringResource(R.string.verify_detail_submitting),
                color = MeshaColors.Muted,
                style = MeshaType.listTitle,
            )
        }
        return
    }
    if (!enabled && !isSubmitting) {
        val message = when (unavailableReason) {
            VerifyDecisionUnavailableReason.EVIDENCE_UNAVAILABLE -> stringResource(R.string.verify_detail_media_unavailable)
            VerifyDecisionUnavailableReason.ALREADY_DECIDED -> stringResource(R.string.verify_detail_already_decided)
            VerifyDecisionUnavailableReason.NONE -> null
        }
        if (message == null) return
        Text(
            text = message,
            color = MeshaColors.Muted,
            style = MeshaType.cta,
            modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 8.dp),
        )
        return
    }
    Row(
        modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 14.dp),
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        DecisionButton(
            label = stringResource(R.string.verify_detail_reject),
            icon = MeshaIcons.Close,
            bg = MeshaColors.DangerX,
            fg = MeshaColors.Danger,
            enabled = enabled,
            loading = isSubmitting,
            onClick = onReject,
            modifier = Modifier.weight(1f),
        )
        DecisionButton(
            label = stringResource(R.string.verify_detail_approve),
            icon = MeshaIcons.Check,
            bg = MeshaColors.OkX,
            fg = MeshaColors.Ok,
            enabled = enabled,
            loading = isSubmitting,
            onClick = onApprove,
            modifier = Modifier.weight(1f),
        )
    }
}

@Composable
private fun DecisionButton(
    label: String,
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    bg: androidx.compose.ui.graphics.Color,
    fg: androidx.compose.ui.graphics.Color,
    enabled: Boolean,
    loading: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier
            .clip(RoundedCornerShape(14.dp))
            .background(bg)
            .clickable(enabled = enabled, onClick = onClick)
            .padding(vertical = 13.dp),
        horizontalArrangement = Arrangement.Center,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (loading) {
            CircularProgressIndicator(modifier = Modifier.size(16.dp), color = fg, strokeWidth = 2.dp)
        } else {
            Icon(imageVector = icon, contentDescription = null, tint = fg, modifier = Modifier.size(16.dp))
            Spacer(Modifier.size(6.dp))
            Text(text = label, color = fg, style = MeshaType.listTitle)
        }
    }
}

/** Reject requires a reason — [onConfirm] is only ever invoked with a non-blank [String]. */
@Composable
private fun RejectReasonDialog(
    onConfirm: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    var reason by remember { mutableStateOf("") }
    var showError by remember { mutableStateOf(false) }
    val latestReason by rememberUpdatedState(reason)

    AlertDialog(
        onDismissRequest = onDismiss,
        // design-system:ignore: weight-only override on the Material dialog title style — applying a
        // MeshaType style here would also replace the AlertDialog's own title size/line-height.
        title = { Text(stringResource(R.string.verify_reject_dialog_title), fontWeight = FontWeight.W700) },
        text = {
            Column {
                Text(
                    text = stringResource(R.string.verify_reject_dialog_subtitle),
                    color = MeshaColors.Muted,
                    // design-system:ignore: 12.5sp/W400 has no close token — `cta` matches the size
                    // but is W700, which would visibly bold this dialog subtitle.
                    fontSize = 12.5.sp,
                    modifier = Modifier.padding(bottom = 10.dp),
                )
                OutlinedTextField(
                    value = reason,
                    onValueChange = {
                        reason = it
                        if (it.isNotBlank()) showError = false
                    },
                    placeholder = { Text(stringResource(R.string.verify_reject_dialog_placeholder)) },
                    isError = showError,
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                    colors = OutlinedTextFieldDefaults.colors(
                        focusedBorderColor = MeshaColors.Brand,
                        unfocusedBorderColor = MeshaColors.Hair,
                    ),
                    modifier = Modifier.fillMaxWidth(),
                )
                if (showError) {
                    Text(
                        text = stringResource(R.string.verify_reject_dialog_error_required),
                        color = MeshaColors.Danger,
                        // design-system:ignore: 11.5sp/W400 has no close token — `caption` matches the
                        // size but is W600 and `eyebrow` is W700 with 1.6sp tracking.
                        fontSize = 11.5.sp,
                        modifier = Modifier.padding(top = 4.dp),
                    )
                }
            }
        },
        confirmButton = {
            TextButton(onClick = {
                val trimmed = latestReason.trim()
                if (trimmed.isBlank()) {
                    showError = true
                } else {
                    onConfirm(trimmed)
                }
            }) {
                // design-system:ignore: weight-only override on the Material TextButton label style —
                // a MeshaType style would also replace the button's own size/line-height.
                Text(stringResource(R.string.verify_reject_dialog_confirm), color = MeshaColors.Danger, fontWeight = FontWeight.W700)
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text(stringResource(R.string.verify_reject_dialog_cancel), color = MeshaColors.Muted)
            }
        },
    )
}
