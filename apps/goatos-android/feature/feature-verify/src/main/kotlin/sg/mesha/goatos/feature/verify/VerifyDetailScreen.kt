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
import androidx.compose.foundation.lazy.items
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
import androidx.media3.exoplayer.ExoPlayer
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.layout.boundsInWindow
import androidx.compose.ui.layout.onGloballyPositioned
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
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import sg.mesha.goatos.core.media.LocalProofPlayerFactory
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

// telemetry:exempt: pure stateless renderer — AnalyticsPort/funnel wiring (including the
// PLAY_INTENT/PLAY_OUTCOME dead-control watchdog) lives in VerifyDetailViewModel (:app), which
// owns every side effect this screen triggers. This screen only forwards synchronous events
// (VerifyDetailEvent.VideoPlayback) at the exact tap/callback moments the ViewModel needs.
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

/** The context dimensions a reviewer needs: the four fixed ones the spec calls out
 *  (shed/park/operator/timestamp), plus the raiser's own note when the producer supplied one. The
 *  LABEL for each is client UI chrome, resolved from a string resource by [ContextCard] — only
 *  [VerifyContextRow.value] is backend data. */
enum class VerifyContextKind { SHED, PARK, OPERATOR, CAPTURED_AT, RAISED_NOTE }

/** One context line: [kind] picks the localized label, [value] is the backend-composed
 *  display string (shed/park/operator name, or a formatted capture timestamp). */
data class VerifyContextRow(val kind: VerifyContextKind, val value: String)

/**
 * ONE animal's proof clip and ITS OWN verdict, inside a shed-level detail screen.
 *
 * The backend now emits one verification_item PER GOAT (`source_ref_type=vaccination_goat`),
 * grouped by a shared `source_submission_id` per shed. A legacy bundled item
 * (`ref_type=sop_submission`, several clips under one verdict) still renders here as a
 * single-entry group — see [VerifyDetailViewModel] grouping.
 */
@Immutable
data class VerifyDetailEntryUiState(
    val itemId: String,
    val subjectLabel: String? = null,
    val media: List<VerifyMediaItem> = emptyList(),
    val statusTone: VerifyTone = VerifyTone.PENDING,
    val rowVersion: Int = 1,
    val verdictReason: String? = null,
    val isApproveEnabled: Boolean = false,
    val isRejectEnabled: Boolean = false,
    val decisionUnavailableReason: VerifyDecisionUnavailableReason = VerifyDecisionUnavailableReason.NONE,
    val isSubmitting: Boolean = false,
)

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
    // Approve and Reject are enabled SEPARATELY and deliberately.
    //
    // Approve is the only irreversible action in this app (there is no un-approve), so it needs
    // evidence she can actually see. Reject/rework is the safe direction: when the video will not
    // load, sending the work back so the team records it again is the ONLY correct move left, so
    // gating Reject on the same signal would strand her with an item she can neither approve nor
    // return. Both fail closed while the item is absent/loading or already decided.
    val isApproveEnabled: Boolean = false,
    val isRejectEnabled: Boolean = false,
    val decisionUnavailableReason: VerifyDecisionUnavailableReason = VerifyDecisionUnavailableReason.NONE,
    val isSubmitting: Boolean = false,
    // Offline-first sync state (docs/decisions/android-offline-first.md).
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
    val errorMessage: String? = null,
    val autoCloseAfterDecision: Boolean = false,
    /**
     * ONE entry per animal video sharing this shed's `source_submission_id` — always non-empty
     * once the item(s) are loaded, and length 1 for a legacy bundled item or a lone item with no
     * siblings. This is what the screen renders; the flat fields above mirror `entries.first()`
     * for a single-entry group so existing single-item call sites keep working unchanged.
     */
    val entries: List<VerifyDetailEntryUiState> = emptyList(),
    /** True once every entry in this group carries a terminal (non-pending) status AND that is
     *  server-confirmed (the group was reloaded from the queue/outbox, never guessed locally).
     *  Drives the "shed accepted" banner — no separate manual close action exists for this. */
    val isGroupFullyDecided: Boolean = false,
)

enum class VerifyDecisionUnavailableReason { NONE, ALREADY_DECIDED, EVIDENCE_UNAVAILABLE }

/** Approve is irreversible, so the screen asks once before sending it. Reject already has its own
 *  mandatory-reason dialog, so this keeps the two decisions symmetric. */
private const val APPROVE_NEEDS_CONFIRMATION = true

enum class VideoPlaybackAction {
    /** Fired synchronously at the play/pause TAP, before player.play()/pause() is even called —
     *  see [VerifyDetailScreen] play/pause click handlers. This is the INTENT half of the
     *  intent/outcome pair; [PLAY_OUTCOME] is the matching outcome the ViewModel's dead-control
     *  watchdog waits for. */
    PLAY_INTENT,
    /** Fired synchronously from the player listener's onIsPlayingChanged — proves the tap above
     *  actually changed player state (either direction), disarming the watchdog armed by the
     *  matching [PLAY_INTENT]. */
    PLAY_OUTCOME,
    PLAY_STARTED,
    WATCH_SUMMARY,
    PLAYBACK_ERROR,
    FULLSCREEN_OPENED,
    FULLSCREEN_EXITED,
}

sealed interface VerifyDetailEvent {
    data object Close : VerifyDetailEvent
    /** [itemId] null targets the legacy single-entry group (`entries.first()`); a grouped shed
     *  screen always passes the tapped entry's own item id, so one animal's verdict never
     *  touches its shed-mates. */
    data class Approve(val itemId: String? = null) : VerifyDetailEvent
    /** [reason] is always non-blank — the reject dialog below refuses to emit this otherwise.
     *  [itemId] follows the same null-means-legacy-single-entry contract as [Approve]. */
    data class Reject(val reason: String, val itemId: String? = null) : VerifyDetailEvent
    data object Refresh : VerifyDetailEvent
    /** Dialog-lifecycle telemetry: the Compose dialogs below own their own open/dismiss state
     *  (a screen-recomposition concern), but every open/cancel is still a real verifier action
     *  the analytics funnel needs to see. */
    data class RejectDialogOpened(val itemId: String) : VerifyDetailEvent
    data class RejectDialogCancelled(val itemId: String) : VerifyDetailEvent
    /** The reject dialog's own mandatory-reason gate refused a blank submit. */
    data class RejectBlockedEmptyReason(val itemId: String) : VerifyDetailEvent
    data class ApproveDialogOpened(val itemId: String) : VerifyDetailEvent
    data class ApproveDialogCancelled(val itemId: String) : VerifyDetailEvent
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
        /** [PLAY_INTENT]/[PLAY_OUTCOME] context: the ExoPlayer playback-state name at tap time
         *  (`STATE_IDLE`/`STATE_ENDED`/...) — the exact condition that used to make play() a
         *  silent no-op. Null for every other action. */
        val playerState: String? = null,
        /** [PLAY_INTENT] only: whether the decoder was already armed/prepared at tap time. */
        val armed: Boolean? = null,
        /** [PLAY_INTENT] only: `"play"` or `"pause"`, whichever the tap requested. */
        val targetAction: String? = null,
    ) : VerifyDetailEvent
}

@Composable
fun VerifyDetailScreen(
    state: VerifyDetailUiState,
    onEvent: (VerifyDetailEvent) -> Unit = {},
    modifier: Modifier = Modifier,
    videoControlsEnabled: Boolean = false,
) {
    // Scoped per animal: which entry's dialog is open, keyed by that entry's OWN item id — never
    // a single screen-wide flag, or one animal's tap would surface a dialog whose confirm posts
    // the wrong verdict once entries re-sort after a refresh.
    var rejectDialogForItemId by remember { mutableStateOf<String?>(null) }
    var approveDialogForItemId by remember { mutableStateOf<String?>(null) }
    RefreshOnResume { onEvent(VerifyDetailEvent.Refresh) }

    // The scrollable viewport's own bounds, in window coordinates. Each row's [VerifyVideoPlayer]
    // compares its own bounds against this to decide whether it is still visible — see
    // [isRowVisibleInViewport] below for the threshold and rationale.
    var viewportBounds by remember { mutableStateOf<Rect?>(null) }

    // entries is always populated once the group has loaded; a single legacy/bundled item is an
    // entries list of length 1, so the "one card, one verdict" layout below is also the correct
    // (and only) rendering for that case — no separate legacy code path needed.
    val entries = state.entries

    // Auto-close once the last item in this group is decided: the item leaves the queue,
    // entries becomes empty, and we have nothing left to show. This happens AFTER the
    // backend confirms the decision (waitForBackendDecision succeeds), at which point the
    // ViewModel sets autoCloseAfterDecision = true. A single legacy/bundled item (entries
    // of length 1) closes immediately; a multi-animal shed waits until all are decided.
    // Distinction: an entry with NO media (genuinely missing evidence) is still an entry —
    // it renders the "No video attached" warning inside VerifyEntryCard. An item with NO
    // entries means the queue no longer knows about this group at all — that is the signal
    // to close.
    // Keyed on the FLAG ALONE. It was `autoCloseAfterDecision && entries.isEmpty()`, which
    // never fires: the ViewModel does not drain `entries` on a verdict -- decided animals stay
    // rendered -- it sets autoCloseAfterDecision = !stillPending once every animal in the group
    // holds a terminal verdict (see its own comment). Requiring an empty list on top of that
    // meant the screen sat on a decided item showing "No video attached to this item" instead
    // of returning to the queue. An item with genuinely no media never sets the flag (no verdict
    // was submitted), so its EmptyState is untouched by this.
    LaunchedEffect(state.autoCloseAfterDecision) {
        if (state.autoCloseAfterDecision) {
            onEvent(VerifyDetailEvent.Close)
        }
    }

    Column(modifier = modifier.fillMaxSize().background(MeshaColors.Bg)) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .background(MeshaColors.Surf, shape = RoundedCornerShape(topStart = 26.dp, topEnd = 26.dp)),
        ) {
            DetailHeader(state = state, onClose = { onEvent(VerifyDetailEvent.Close) })
            LazyColumn(
                modifier = Modifier
                    .fillMaxWidth()
                    .weight(1f)
                    .onGloballyPositioned { viewportBounds = it.boundsInWindow() },
                contentPadding = PaddingValues(bottom = 20.dp),
            ) {
                if (entries.isEmpty()) {
                    item {
                        EmptyState(
                            title = stringResource(R.string.verify_detail_no_media),
                            icon = MeshaIcons.Video,
                            tone = EmptyTone.Warn,
                            modifier = Modifier.padding(horizontal = 16.dp),
                        )
                    }
                } else {
                    items(
                        items = entries,
                        key = { entry -> entry.itemId },
                        contentType = { "verification_entry" },
                    ) { entry ->
                        VerifyEntryCard(
                            entry = entry,
                            isCloseMode = state.isCloseMode,
                            videoControlsEnabled = videoControlsEnabled,
                            viewportBounds = viewportBounds,
                            onPlayback = { onEvent(it) },
                            onApprove = {
                                if (APPROVE_NEEDS_CONFIRMATION) {
                                    approveDialogForItemId = entry.itemId
                                    onEvent(VerifyDetailEvent.ApproveDialogOpened(entry.itemId))
                                } else {
                                    onEvent(VerifyDetailEvent.Approve(entry.itemId))
                                }
                            },
                            onReject = {
                                rejectDialogForItemId = entry.itemId
                                onEvent(VerifyDetailEvent.RejectDialogOpened(entry.itemId))
                            },
                        )
                    }
                }
                item { ContextCard(state.context) }
                if (entries.size > 1 && state.isGroupFullyDecided) {
                    item { GroupAcceptedBanner() }
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

    rejectDialogForItemId?.let { targetItemId ->
        RejectReasonDialog(
            onConfirm = { reason ->
                rejectDialogForItemId = null
                onEvent(VerifyDetailEvent.Reject(reason = reason, itemId = targetItemId))
            },
            onDismiss = {
                rejectDialogForItemId = null
                onEvent(VerifyDetailEvent.RejectDialogCancelled(targetItemId))
            },
            onBlockedEmptyReason = { onEvent(VerifyDetailEvent.RejectBlockedEmptyReason(targetItemId)) },
        )
    }

    approveDialogForItemId?.let { targetItemId ->
        ApproveConfirmDialog(
            onConfirm = {
                approveDialogForItemId = null
                onEvent(VerifyDetailEvent.Approve(targetItemId))
            },
            onDismiss = {
                approveDialogForItemId = null
                onEvent(VerifyDetailEvent.ApproveDialogCancelled(targetItemId))
            },
        )
    }
}

/** One animal's clip(s) + its OWN status pill, rejection reason, and Approve/Reject pair. */
@Composable
private fun VerifyEntryCard(
    entry: VerifyDetailEntryUiState,
    isCloseMode: Boolean,
    videoControlsEnabled: Boolean,
    viewportBounds: Rect?,
    onPlayback: (VerifyDetailEvent) -> Unit,
    onApprove: () -> Unit,
    onReject: () -> Unit,
) {
    Column(modifier = Modifier.fillMaxWidth()) {
        entry.subjectLabel?.takeIf { it.isNotBlank() }?.let { subject ->
            Text(
                text = subject,
                color = MeshaColors.Ink,
                style = MeshaType.cardTitle,
                modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 10.dp),
            )
        }
        if (entry.media.isEmpty()) {
            EmptyState(
                title = stringResource(R.string.verify_detail_no_media),
                icon = MeshaIcons.Video,
                tone = EmptyTone.Warn,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            )
        }
        entry.media.forEach { media ->
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
                    onPlayback = onPlayback,
                    controlsEnabled = videoControlsEnabled,
                    viewportBounds = viewportBounds,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        }
        Box(modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 4.dp)) {
            StatusPill(tone = entry.statusTone)
        }
        entry.verdictReason?.takeIf { it.isNotBlank() }?.let { reason ->
            RejectionReasonCard(reason = reason)
        }
        if (!isCloseMode) {
            DecisionRow(
                approveEnabled = entry.isApproveEnabled && !entry.isSubmitting,
                rejectEnabled = entry.isRejectEnabled && !entry.isSubmitting,
                unavailableReason = entry.decisionUnavailableReason,
                isSubmitting = entry.isSubmitting,
                onApprove = onApprove,
                onReject = onReject,
            )
        }
        HorizontalDivider(
            modifier = Modifier.padding(top = 14.dp),
            thickness = 1.dp,
            color = MeshaColors.Surf2,
        )
    }
}

@Composable
private fun GroupAcceptedBanner() {
    Column(
        modifier = Modifier
            .padding(horizontal = 16.dp, vertical = 8.dp)
            .fillMaxWidth()
            .background(MeshaColors.OkX, shape = RoundedCornerShape(16.dp))
            .padding(14.dp),
    ) {
        Text(
            text = stringResource(R.string.verify_detail_group_accepted),
            color = MeshaColors.Ok,
            style = MeshaType.listTitle,
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
    viewportBounds: Rect? = null,
) {
    val context = LocalContext.current
    // Telemetry-instrumented player (W-22): media3 must fetch over the app's OkHttp client, or a
    // 403 on an expired signed URL is invisible everywhere except the server log.
    val playerFactory = LocalProofPlayerFactory.current
    val view = LocalView.current
    var isFullscreen by rememberSaveable(media.signedUrl) { mutableStateOf(false) }
    var isPlaying by remember { mutableStateOf(false) }
    val currentOnPlayback by rememberUpdatedState(onPlayback)
    // ONE video is prepared at a time, and only after the reader asks for it.
    //
    // Every row used to build AND prepare() its own ExoPlayer as soon as it composed. A shed with
    // five animals therefore held five hardware video decoders at once; devices cap concurrent
    // MediaCodec instances, so the first clip played and every later one failed SILENTLY — the
    // play button did nothing and no error surfaced. That is exactly the "I tapped it and nothing
    // happened" class of bug this app is supposed to eliminate.
    //
    // `armed` flips on the first play tap. Until then the row shows its poster and costs no
    // decoder. Reader-facing behaviour is unchanged: tap play, it plays.
    var armed by remember(media.signedUrl) { mutableStateOf(false) }
    // This row's own bounds in window coordinates, refreshed on every layout pass (scroll included)
    // — compared against [viewportBounds] to decide whether this player should keep running.
    var rowBounds by remember { mutableStateOf<Rect?>(null) }
    val player = remember(media.signedUrl) {
        playerFactory.create(context).apply {
            setMediaItem(MediaItem.fromUri(Uri.parse(media.signedUrl)))
            playWhenReady = false
        }
    }
    // prepare() (the call that allocates the hardware decoder) is fired synchronously from the
    // play/pause click handler's STATE_IDLE branch below, in the same tap that flips `armed` to
    // true — there is no other path that sets `armed`, so a LaunchedEffect(armed, ...) mirroring
    // that call would only ever re-run one composition AFTER the click handler already prepared
    // the player; it was pure dead weight and has been removed. Do not reintroduce it: the whole
    // point of the click-handler prepare() is to avoid depending on effect ordering (see the
    // click handler's own comment for why a LaunchedEffect-only path silently drops the 2nd/3rd
    // video on some devices).
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
                // Outcome half of the intent/outcome pair below: the player told us it actually
                // changed state, so the watchdog armed by the matching PLAY_INTENT tap can stand
                // down. Fires on every transition (not just start), so a tap that only pauses
                // (which never produces a tracker PLAY_STARTED) is still provably not dead.
                currentOnPlayback(
                    VerifyDetailEvent.VideoPlayback(
                        proofSubject = media.proofSubject,
                        mimeType = media.mimeType,
                        action = VideoPlaybackAction.PLAY_OUTCOME,
                    ),
                )
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
    // Pause + free the decoder once this row is no longer (mostly) visible in the shed list.
    //
    // Every row used to keep playing forever once tapped: composition never tears the player down
    // until the row leaves COMPOSITION (LazyColumn recycling), which is later than leaving the
    // VIEWPORT, and never happens at all for a row merely scrolled half off-screen. With several
    // animals per shed and several sheds per park, that meant several hardware decoders running
    // (and audio playing) off-screen at once — the exact condition that silently starved the
    // 2nd/3rd decoder and made play() a no-op elsewhere in this screen.
    //
    // This is PAUSE, never auto-resume: scrolling a paused/playing row back into view must not
    // restart it on its own (a verifier must not have clips playing at her as she scrolls) — she
    // taps play again, which re-enters the STATE_IDLE/STATE_ENDED branches below exactly as if she
    // had never scrolled.
    //
    // Deliberately NOT routed through the play/pause click handler: this must never emit
    // PLAY_INTENT (that event means "the verifier tapped"), or a scroll would be misrecorded as a
    // deliberate pause and could spuriously arm/disarm the dead-control watchdog. The player's own
    // listener still fires PLAY_OUTCOME (proof the state changed) and stops accruing watch time —
    // both correct, since the clip truly did stop playing. Because this never reaches
    // STATE_ENDED, [VideoPlaybackTracker.flush] never fires here, so no premature WATCH_SUMMARY is
    // sent and the accrued totals are untouched; they keep accumulating from where they left off
    // if she scrolls back and taps play again.
    LaunchedEffect(rowBounds, viewportBounds) {
        val row = rowBounds ?: return@LaunchedEffect
        val viewport = viewportBounds ?: return@LaunchedEffect
        if (armed && player.isPlaying && !isRowVisibleInViewport(row, viewport)) {
            player.playWhenReady = false
            player.stop()
        }
    }
    // Backgrounding the app (lock screen, home button, task switch) is its own case: nothing
    // above fires because the row's bounds never change. ON_STOP (not ON_PAUSE, which also fires
    // for transient overlays like a permission dialog) matches the ExoPlayer-recommended pause
    // point and keeps a proof clip from playing audio behind a locked screen.
    LifecycleEventEffect(Lifecycle.Event.ON_STOP) {
        if (armed && player.isPlaying) {
            player.playWhenReady = false
            player.stop()
        }
    }
    Box(
        modifier = modifier
            .aspectRatio(16f / 9f)
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Bg)
            .onGloballyPositioned { rowBounds = it.boundsInWindow() },
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
        // READ-ONLY time readout. Deliberately NOT the media3 controller: turning that on would
        // hand the verifier a seek bar. This is a label, so she can see how long the clip is and
        // how far in she is, and still cannot skip through it.
        VideoTimeReadout(player = player, modifier = Modifier.align(Alignment.BottomStart))
        PlayPauseButton(
            isPlaying = isPlaying,
            onClick = {
                // INTENT half of the intent/outcome pair (docs/observability/
                // TELEMETRY_GUARDRAILS.md): recorded BEFORE pause()/play() is even called, so a
                // tap is proven to have happened whether or not the player responds. The matching
                // PLAY_OUTCOME above disarms the ViewModel's watchdog; if it never arrives within
                // AnalyticsFunnels.VERIFY_VIDEO_PLAY_WATCHDOG_TIMEOUT_MS the tap is reported as a
                // dead control instead of vanishing silently.
                currentOnPlayback(
                    VerifyDetailEvent.VideoPlayback(
                        proofSubject = media.proofSubject,
                        mimeType = media.mimeType,
                        action = VideoPlaybackAction.PLAY_INTENT,
                        playerState = player.playbackState.toPlayerStateLabel(),
                        armed = armed,
                        targetAction = if (player.isPlaying) "pause" else "play",
                    ),
                )
                if (player.isPlaying) {
                    player.pause()
                } else {
                    // Arm the decoder (see `armed` above) AND drive the player straight from
                    // this click. Both steps have to happen here, synchronously:
                    //
                    //  - prepare() used to be left to a LaunchedEffect keyed on `armed`. That
                    //    effect runs on the next composition, so play() could fire against a
                    //    still-IDLE player, where it is a silent no-op.
                    //  - a clip that ran to the end leaves the playhead AT the end, where play()
                    //    is likewise a no-op.
                    //
                    // Either one on its own makes the button look dead: the verifier taps, and
                    // nothing whatsoever happens. Re-watching a 3-second proof is the core of the
                    // job, so this path must never depend on effect ordering.
                    armed = true
                    when (player.playbackState) {
                        Player.STATE_IDLE -> player.prepare()
                        Player.STATE_ENDED -> player.seekTo(0)
                        else -> Unit
                    }
                    player.play()
                }
            },
            modifier = Modifier.align(Alignment.Center),
        )
        // Maintainer decision 2026-08-04 SUPERSEDES 2026-08-02: the verifier keeps NO SCRUBBING
        // (useController stays false, so there is no seek bar) but now gets FULLSCREEN and a
        // read-only elapsed/total readout.
        //
        // Why the change: judging a proof means judging what is in it, and "the clip is too short"
        // is a real verdict a verifier must be able to justify. Without a duration on screen she
        // was rejecting blind — a 1.9s and a 20.4s recording looked identical. Fullscreen is
        // needed for the same reason: an ear tag is unreadable in a 16:9 thumbnail.
        // Scrubbing stays disabled: she must still WATCH the proof rather than skim it.
        run {
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
                    // stop(), not just pause. The fullscreen dialog builds its OWN ExoPlayer and
                    // prepares it immediately, so a merely-paused inline player would still be
                    // holding its hardware decoder and the two would be allocated at once -- on a
                    // device with a tight decoder budget the fullscreen video then fails to open
                    // with no error at all. stop() releases the codec and drops this player to
                    // STATE_IDLE; the play handler above re-prepares from IDLE on the next tap, so
                    // closing fullscreen and pressing play still works.
                    player.playWhenReady = false
                    player.stop()
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
            onDismiss = {
                isFullscreen = false
                onPlayback(
                    VerifyDetailEvent.VideoPlayback(
                        proofSubject = media.proofSubject,
                        mimeType = media.mimeType,
                        action = VideoPlaybackAction.FULLSCREEN_EXITED,
                    ),
                )
            },
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

/**
 * Elapsed / total for a proof video, updated once a second while it plays.
 *
 * Exists because the verifier has no media3 controller (that would allow scrubbing) and was
 * therefore judging a clip with no idea of its length — yet "too short" is one of the verdicts
 * she is expected to give.
 */
@Composable
private fun VideoTimeReadout(player: ExoPlayer, modifier: Modifier = Modifier) {
    var positionMs by remember { mutableLongStateOf(0L) }
    var durationMs by remember { mutableLongStateOf(0L) }
    LaunchedEffect(player) {
        while (true) {
            positionMs = player.currentPosition.coerceAtLeast(0L)
            // duration is C.TIME_UNSET until the media is prepared; show 0 rather than a negative.
            durationMs = player.duration.coerceAtLeast(0L)
            kotlinx.coroutines.delay(500)
        }
    }
    Text(
        text = formatClock(positionMs) + " / " + formatClock(durationMs),
        style = MeshaType.caption,
        color = MeshaColors.Ink,
        modifier = modifier
            .padding(8.dp)
            .background(MeshaColors.Bg.copy(alpha = 0.72f), RoundedCornerShape(6.dp))
            .padding(horizontal = 8.dp, vertical = 4.dp),
    )
}

/** m:ss. Proof clips are seconds long, so hours would be noise. */
private fun formatClock(ms: Long): String {
    val total = ms / 1000
    return "%d:%02d".format(total / 60, total % 60)
}

/**
 * Whether a video row is still "visible enough" in the list's scrollable viewport to keep
 * playing, given both rects in the SAME coordinate space (window coordinates in production —
 * see the `onGloballyPositioned` call sites above).
 *
 * Threshold: at least [minVisibleFraction] (50%) of the row's height must overlap the viewport.
 * Chosen because the maintainer-confirmed gap was explicitly a row "merely scrolled half-off" —
 * a 0% ("fully off-screen only") threshold would leave that exact case still playing (and its
 * decoder still held) while the row is mostly unreadable and its audio is already disorienting.
 * A stricter threshold (e.g. 90%) would instead pause during ordinary small scroll jitter, which
 * is needlessly aggressive for a hardware-decoder / battery concern. 50% is the smallest
 * threshold that actually closes the reported gap.
 */
internal fun isRowVisibleInViewport(
    row: Rect,
    viewport: Rect,
    minVisibleFraction: Float = 0.5f,
): Boolean {
    val rowHeight = (row.bottom - row.top).coerceAtLeast(1f)
    val overlapTop = maxOf(row.top, viewport.top)
    val overlapBottom = minOf(row.bottom, viewport.bottom)
    val overlapHeight = (overlapBottom - overlapTop).coerceAtLeast(0f)
    return (overlapHeight / rowHeight) >= minVisibleFraction
}

/** Human-readable ExoPlayer state name for [VerifyDetailEvent.VideoPlayback.playerState] — the
 *  exact condition (STATE_IDLE/STATE_ENDED) that used to make play() a silent no-op, so a dead
 *  control report is actionable without re-deriving it from a raw int. */
private fun Int.toPlayerStateLabel(): String = when (this) {
    Player.STATE_IDLE -> "STATE_IDLE"
    Player.STATE_BUFFERING -> "STATE_BUFFERING"
    Player.STATE_READY -> "STATE_READY"
    Player.STATE_ENDED -> "STATE_ENDED"
    else -> "STATE_UNKNOWN($this)"
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
    val playerFactory = LocalProofPlayerFactory.current
    var isPlaying by remember { mutableStateOf(true) }
    val currentOnPlayback by rememberUpdatedState(onPlayback)
    val player = remember(media.signedUrl) {
        playerFactory.create(context).apply {
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
                // Outcome half of the intent/outcome pair — see the inline player's identical
                // listener above for the full rationale.
                currentOnPlayback(
                    VerifyDetailEvent.VideoPlayback(
                        proofSubject = media.proofSubject,
                        mimeType = media.mimeType,
                        action = VideoPlaybackAction.PLAY_OUTCOME,
                    ),
                )
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
    // Same background-pause rule as the inline player above — a fullscreen proof clip must not
    // keep playing (with audio) behind a locked screen or after a task switch.
    LifecycleEventEffect(Lifecycle.Event.ON_STOP) {
        if (player.isPlaying) {
            player.playWhenReady = false
            player.pause()
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
                    // INTENT half — see the inline player's identical click handler above for the
                    // full rationale. The fullscreen player is always prepared eagerly on
                    // creation, so `armed` is always true here.
                    currentOnPlayback(
                        VerifyDetailEvent.VideoPlayback(
                            proofSubject = media.proofSubject,
                            mimeType = media.mimeType,
                            action = VideoPlaybackAction.PLAY_INTENT,
                            playerState = player.playbackState.toPlayerStateLabel(),
                            armed = true,
                            targetAction = if (player.isPlaying) "pause" else "play",
                        ),
                    )
                    if (player.isPlaying) {
                        player.pause()
                    } else {
                        // Same rule as the inline player above: an IDLE player needs prepare()
                        // and a finished one needs rewinding, or play() does nothing at all and
                        // the control looks broken.
                        when (player.playbackState) {
                            Player.STATE_IDLE -> player.prepare()
                            Player.STATE_ENDED -> player.seekTo(0)
                            else -> Unit
                        }
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
        // exception:exempt timestamp display; unparseable instant shows raw ISO string
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
    VerifyContextKind.RAISED_NOTE -> stringResource(R.string.verify_detail_raised_note_label)
}

@Composable
private fun DecisionRow(
    approveEnabled: Boolean,
    rejectEnabled: Boolean,
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
    // Nothing at all is decidable (already decided, or the item is still loading): explain, no buttons.
    if (!approveEnabled && !rejectEnabled) {
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
    // The video will not load: say so and leave the rework route open, rather than leaving her on a
    // dead screen with an Approve she must not be able to press.
    if (!approveEnabled && unavailableReason == VerifyDecisionUnavailableReason.EVIDENCE_UNAVAILABLE) {
        Text(
            text = stringResource(R.string.verify_detail_media_unavailable_send_back),
            color = MeshaColors.Muted,
            style = MeshaType.cta,
            modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 8.dp),
        )
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
            enabled = rejectEnabled,
            loading = isSubmitting,
            onClick = onReject,
            modifier = Modifier.weight(1f),
        )
        DecisionButton(
            label = stringResource(R.string.verify_detail_approve),
            icon = MeshaIcons.Check,
            bg = MeshaColors.OkX,
            fg = MeshaColors.Ok,
            enabled = approveEnabled,
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
    onBlockedEmptyReason: () -> Unit = {},
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
                    onBlockedEmptyReason()
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

/**
 * Approve is the one decision nobody can take back — there is no un-approve anywhere in the app or
 * the backend. Reject already costs a dialog plus a typed reason, so an unguarded single tap made
 * the irreversible action the CHEAPEST one on the screen. This restores one deliberate tap; it adds
 * no typing and no reading, so a long queue costs one extra tap per item, not a new workflow.
 */
@Composable
private fun ApproveConfirmDialog(
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        // design-system:ignore: weight-only override on the Material dialog title style — applying a
        // MeshaType style here would also replace the AlertDialog's own title size/line-height.
        title = { Text(stringResource(R.string.verify_approve_dialog_title), fontWeight = FontWeight.W700) },
        text = {
            Text(
                text = stringResource(R.string.verify_approve_dialog_subtitle),
                color = MeshaColors.Muted,
                // design-system:ignore: 12.5sp/W400 has no close token — `cta` matches the size
                // but is W700, which would visibly bold this dialog subtitle.
                fontSize = 12.5.sp,
            )
        },
        confirmButton = {
            TextButton(onClick = onConfirm) {
                // design-system:ignore: weight-only override on the Material TextButton label style —
                // a MeshaType style would also replace the button's own size/line-height.
                Text(stringResource(R.string.verify_approve_dialog_confirm), color = MeshaColors.Ok, fontWeight = FontWeight.W700)
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text(stringResource(R.string.verify_approve_dialog_cancel), color = MeshaColors.Muted)
            }
        },
    )
}
