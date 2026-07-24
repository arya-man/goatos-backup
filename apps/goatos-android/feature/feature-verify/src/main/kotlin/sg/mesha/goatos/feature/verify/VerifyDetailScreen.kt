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
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.media3.common.MediaItem
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.PlayerView
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
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
    val categoryLabel: String = "",
    val media: List<VerifyMediaItem> = emptyList(),
    val context: List<VerifyContextRow> = emptyList(),
    val statusTone: VerifyTone = VerifyTone.PENDING,
    val rowVersion: Int = 1,
    /** False once a verdict has already been recorded (server or a just-submitted local
     *  optimistic state) — the buttons disable rather than allow a second conflicting verdict. */
    // Fail closed while the requested item is absent/loading. The ViewModel enables decisions
    // only after a real pending row with resolvable evidence arrives from Room.
    val isDecisionEnabled: Boolean = false,
    val isSubmitting: Boolean = false,
    // Offline-first sync state (docs/decisions/android-offline-first.md).
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
    val errorMessage: String? = null,
)

sealed interface VerifyDetailEvent {
    data object Close : VerifyDetailEvent
    data object Approve : VerifyDetailEvent
    /** [reason] is always non-blank — the reject dialog below refuses to emit this otherwise. */
    data class Reject(val reason: String) : VerifyDetailEvent
    data object Refresh : VerifyDetailEvent
}

@Composable
fun VerifyDetailScreen(
    state: VerifyDetailUiState,
    onEvent: (VerifyDetailEvent) -> Unit = {},
    modifier: Modifier = Modifier,
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
                    items(state.media, key = { it.signedUrl }) { media ->
                        VerifyVideoPlayer(
                            media = media,
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(horizontal = 16.dp, vertical = 8.dp),
                        )
                    }
                }
                item { ContextCard(state.context) }
                item {
                    Box(modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 12.dp)) {
                        StatusPill(tone = state.statusTone)
                    }
                }
                item {
                    DecisionRow(
                        enabled = state.isDecisionEnabled && !state.isSubmitting,
                        isSubmitting = state.isSubmitting,
                        onApprove = { onEvent(VerifyDetailEvent.Approve) },
                        onReject = { showRejectDialog = true },
                    )
                }
                state.errorMessage?.let { message ->
                    item {
                        Text(
                            text = message,
                            color = MeshaColors.Danger,
                            fontSize = 12.5.sp,
                            fontWeight = FontWeight.W600,
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
                fontSize = 16.sp,
                fontWeight = FontWeight.W700,
            )
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
private fun VerifyVideoPlayer(media: VerifyMediaItem, modifier: Modifier = Modifier) {
    val context = LocalContext.current
    val player = remember(media.signedUrl) {
        ExoPlayer.Builder(context).build().apply {
            setMediaItem(MediaItem.fromUri(Uri.parse(media.signedUrl)))
            prepare()
            playWhenReady = false
        }
    }
    DisposableEffect(player) {
        onDispose { player.release() }
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
                    useController = true
                }
            },
            modifier = Modifier.fillMaxSize(),
        )
    }
}

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
            fontSize = 10.5.sp,
            fontWeight = FontWeight.W700,
            letterSpacing = 0.6.sp,
            modifier = Modifier.padding(top = 12.dp, bottom = 4.dp),
        )
        rows.forEachIndexed { index, row ->
            Row(
                verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier.fillMaxWidth().padding(vertical = 11.dp),
            ) {
                Text(text = contextKindLabel(row.kind), color = MeshaColors.Muted, fontSize = 13.sp, modifier = Modifier.weight(1f))
                val displayValue = remember(row.value, row.kind, locale) {
                    if (row.kind == VerifyContextKind.CAPTURED_AT) {
                        formatCapturedAt(row.value, locale, ZoneId.systemDefault())
                    } else {
                        row.value
                    }
                }
                Text(text = displayValue, color = MeshaColors.Ink, fontSize = 13.sp, fontWeight = FontWeight.W700)
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
    isSubmitting: Boolean,
    onApprove: () -> Unit,
    onReject: () -> Unit,
) {
    if (!enabled && !isSubmitting) {
        Text(
            text = stringResource(R.string.verify_detail_already_decided),
            color = MeshaColors.Muted,
            fontSize = 12.5.sp,
            fontWeight = FontWeight.W600,
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
            Text(text = label, color = fg, fontSize = 13.5.sp, fontWeight = FontWeight.W700)
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
        title = { Text(stringResource(R.string.verify_reject_dialog_title), fontWeight = FontWeight.W700) },
        text = {
            Column {
                Text(
                    text = stringResource(R.string.verify_reject_dialog_subtitle),
                    color = MeshaColors.Muted,
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
