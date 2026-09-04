package sg.mesha.goatos.feature.leadershiptasks

// telemetry:exempt pure stateless renderer; LeadershipTaskDetailViewModel (in :app) owns the
// leadership_task_* AnalyticsEventsLeadershipTasks + CrashReporter wiring for every refresh,
// fetch, and status write.

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaIconButton
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.ProofMediaPreview
import sg.mesha.goatos.core.ui.ProofMediaPreviewKind
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton

/**
 * ONE task (`/leadership-tasks/{taskId}`) — a hosted drill with Up/Back and NO L0 chrome
 * (Android navigation-stack invariant).
 *
 * Everything on it renders as the SERVER composed it: the number, the chip, the meta line, the
 * status options this caller may move to, and whether they may edit or cancel. The screen
 * decides nothing about who the caller is.
 */
@Composable
fun LeadershipTaskDetailScreen(
    state: LeadershipTaskDetailUiState,
    onEvent: (LeadershipTaskDetailEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(LeadershipTaskDetailEvent.Refresh) }

    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            // Backend-composed number ("#12"), verbatim — the one line that names this task.
            title = state.numberLabel,
            onBack = { onEvent(LeadershipTaskDetailEvent.Back) },
            actions = {
                if (state.canEdit) {
                    MeshaIconButton(
                        icon = MeshaIcons.Edit,
                        contentDescription = stringResource(R.string.leadership_tasks_action_edit),
                        onClick = { onEvent(LeadershipTaskDetailEvent.Edit) },
                    )
                    Spacer(Modifier.width(8.dp))
                }
                SyncIconButton(
                    isSyncing = state.isRefreshing,
                    onSync = { onEvent(LeadershipTaskDetailEvent.Refresh) },
                    contentDescription = stringResource(R.string.leadership_tasks_action_refresh),
                )
            },
        )
        if (state.loading) {
            Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                CircularProgressIndicator(color = MeshaColors.BrandD)
            }
            return@Column
        }
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(top = 4.dp, bottom = 28.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            item(key = "brief") {
                Column(
                    modifier = leadershipCardModifier(),
                    verticalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                        LeadershipStatusChip(label = state.statusChip)
                        Spacer(Modifier.weight(1f))
                    }
                    Text(text = state.title, color = MeshaColors.Ink, style = MeshaType.headerTitle)
                    // Backend-composed meta line, verbatim.
                    if (state.metaLine.isNotBlank()) {
                        Text(text = state.metaLine, color = MeshaColors.Muted, style = MeshaType.caption)
                    }
                    if (state.body.isNotBlank()) {
                        Text(text = state.body, color = MeshaColors.Ink, style = MeshaType.body)
                    }
                }
            }

            if (state.attachments.isNotEmpty()) {
                item(key = "attachments_label") {
                    Text(
                        text = stringResource(R.string.leadership_tasks_attachments_label),
                        color = MeshaColors.Faint,
                        style = MeshaType.sectionLabel,
                        modifier = Modifier.padding(horizontal = 32.dp, vertical = 2.dp),
                    )
                }
                items(count = state.attachments.size, key = { index -> state.attachments[index].listKey }) { index ->
                    LeadershipAttachmentRow(attachment = state.attachments[index], onEvent = onEvent)
                }
            }

            if (state.statusOptions.isNotEmpty() || state.canCancel) {
                item(key = "actions") {
                    Column(
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
                        verticalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        // One filled button per backend option, labelled with the backend's copy.
                        state.statusOptions.forEach { option ->
                            LeadershipPrimaryButton(
                                label = option.label,
                                enabled = !state.actionInFlight,
                                onClick = { onEvent(LeadershipTaskDetailEvent.ChangeStatus(option.key)) },
                                modifier = Modifier.fillMaxWidth(),
                            )
                        }
                        if (state.canCancel) {
                            LeadershipGhostButton(
                                label = stringResource(R.string.leadership_tasks_action_cancel_task),
                                enabled = !state.actionInFlight,
                                onClick = { onEvent(LeadershipTaskDetailEvent.RequestCancel) },
                                modifier = Modifier.fillMaxWidth(),
                                danger = true,
                            )
                        }
                    }
                }
            }

            if (state.message != null) {
                item(key = "message") {
                    Text(
                        text = state.message,
                        color = MeshaColors.Danger,
                        style = MeshaType.caption,
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(horizontal = 16.dp)
                            .clickable { onEvent(LeadershipTaskDetailEvent.DismissMessage) },
                    )
                }
            }
        }
    }

    if (state.showCancelConfirm) {
        AlertDialog(
            onDismissRequest = { onEvent(LeadershipTaskDetailEvent.DismissCancel) },
            containerColor = MeshaColors.Surf,
            title = {
                Text(
                    text = stringResource(R.string.leadership_tasks_cancel_confirm_title),
                    color = MeshaColors.Ink,
                    style = MeshaType.cardTitle,
                )
            },
            text = {
                Text(
                    text = stringResource(R.string.leadership_tasks_cancel_confirm_body),
                    color = MeshaColors.Muted,
                    style = MeshaType.cardSubtitle,
                )
            },
            confirmButton = {
                TextButton(onClick = { onEvent(LeadershipTaskDetailEvent.ConfirmCancel) }) {
                    Text(
                        text = stringResource(R.string.leadership_tasks_cancel_confirm_yes),
                        color = MeshaColors.Danger,
                        style = MeshaType.button,
                    )
                }
            },
            dismissButton = {
                TextButton(onClick = { onEvent(LeadershipTaskDetailEvent.DismissCancel) }) {
                    Text(
                        text = stringResource(R.string.leadership_tasks_cancel_confirm_no),
                        color = MeshaColors.Muted,
                        style = MeshaType.button,
                    )
                }
            },
        )
    }
}

/**
 * One attachment. A photo shows its thumbnail and a video its poster once the bytes are fetched;
 * a voice note plays in place; a file opens outside the app on tap. Until fetched, each shows its
 * name, size and kind so the row is legible offline too.
 */
@Composable
private fun LeadershipAttachmentRow(
    attachment: LeadershipAttachmentUi,
    onEvent: (LeadershipTaskDetailEvent) -> Unit,
) {
    val open = { onEvent(LeadershipTaskDetailEvent.OpenAttachment(attachment.listKey)) }
    Column(
        modifier = leadershipCardModifier(enabled = !attachment.loading, onClick = open),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            LeadershipAttachmentGlyph(kind = attachment.kind)
            Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(
                    text = attachment.fileName,
                    color = MeshaColors.Ink,
                    style = MeshaType.bodyStrong,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                val detail = listOf(attachment.durationLabel, attachment.sizeLabel)
                    .filter { it.isNotBlank() }
                    .joinToString(" · ")
                if (detail.isNotBlank()) {
                    Text(text = detail, color = MeshaColors.Muted, style = MeshaType.caption)
                }
            }
            when {
                attachment.loading -> LeadershipInlineSpinner()
                attachment.localPath.isBlank() -> Icon(
                    imageVector = MeshaIcons.Download,
                    contentDescription = stringResource(R.string.leadership_tasks_attachment_open),
                    tint = MeshaColors.Faint,
                    modifier = Modifier.size(18.dp),
                )
                attachment.kind == LeadershipAttachmentKind.FILE -> Icon(
                    imageVector = MeshaIcons.Expand,
                    contentDescription = stringResource(R.string.leadership_tasks_attachment_open),
                    tint = MeshaColors.Faint,
                    modifier = Modifier.size(18.dp),
                )
            }
        }
        if (attachment.localPath.isNotBlank()) {
            when (attachment.kind) {
                LeadershipAttachmentKind.PHOTO -> ProofMediaPreview(
                    path = attachment.localPath,
                    kind = ProofMediaPreviewKind.Photo,
                    modifier = Modifier.fillMaxWidth().height(220.dp).clip(RoundedCornerShape(12.dp)),
                    expandable = true,
                )
                LeadershipAttachmentKind.VIDEO -> ProofMediaPreview(
                    path = attachment.localPath,
                    kind = ProofMediaPreviewKind.Video,
                    modifier = Modifier.fillMaxWidth().height(220.dp).clip(RoundedCornerShape(12.dp)),
                    expandable = true,
                )
                LeadershipAttachmentKind.AUDIO -> LeadershipAudioPlayerRow(localPath = attachment.localPath)
                LeadershipAttachmentKind.FILE -> Unit
            }
        }
    }
}
