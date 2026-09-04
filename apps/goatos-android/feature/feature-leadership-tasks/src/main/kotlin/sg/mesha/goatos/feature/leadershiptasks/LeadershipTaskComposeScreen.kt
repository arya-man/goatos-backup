package sg.mesha.goatos.feature.leadershiptasks

// telemetry:exempt pure stateless renderer; LeadershipTaskComposeViewModel (in :app) owns the
// leadership_task_* AnalyticsEventsLeadershipTasks + CrashReporter wiring for every recording,
// attachment import, upload, and send.

import android.Manifest
import android.content.pm.PackageManager
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/** Bounds the system photo picker's multi-select to what a draft can still take. */
const val LEADERSHIP_TASK_ATTACHMENT_CAP = 12

/**
 * Raise a new task, or edit one (`/leadership-tasks/compose`) — a hosted drill with Up/Back and
 * NO L0 chrome. Deliberately NOT refresh-on-resume: this screen holds in-progress input.
 *
 * The screen hosts the three Activity-result contracts (photo picker, document picker, the
 * microphone permission) because they are UI plumbing; every result goes to :app as an event and
 * the ViewModel decides what to do with it. The recorder itself, the file import, the uploads and
 * the send all live in :app.
 */
@Composable
fun LeadershipTaskComposeScreen(
    state: LeadershipTaskComposeUiState,
    onEvent: (LeadershipTaskComposeEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    val context = LocalContext.current
    val remaining = (LEADERSHIP_TASK_ATTACHMENT_CAP - state.attachments.size).coerceAtLeast(0)
    val busy = state.sending || state.recordingElapsedMs != null

    val mediaPicker = rememberLauncherForActivityResult(
        ActivityResultContracts.PickMultipleVisualMedia(maxItems = remaining.coerceAtLeast(2)), // camera-only:ignore: a task attachment is not proof of work; gallery and file picks are the maintainer's explicit ask (2026-09-04)
    ) { uris ->
        if (uris.isNotEmpty()) onEvent(LeadershipTaskComposeEvent.MediaPicked(uris.map { it.toString() }))
    }
    val filePicker = rememberLauncherForActivityResult(ActivityResultContracts.OpenMultipleDocuments()) { uris -> // camera-only:ignore: a task attachment is not proof of work; gallery and file picks are the maintainer's explicit ask (2026-09-04)
        if (uris.isNotEmpty()) onEvent(LeadershipTaskComposeEvent.FilesPicked(uris.map { it.toString() }))
    }
    val micPermission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        if (granted) onEvent(LeadershipTaskComposeEvent.StartRecording) else onEvent(LeadershipTaskComposeEvent.RecordingPermissionDenied)
    }
    val startRecording = {
        val granted = context.checkSelfPermission(Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED
        if (granted) onEvent(LeadershipTaskComposeEvent.StartRecording) else micPermission.launch(Manifest.permission.RECORD_AUDIO)
    }

    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = stringResource(
                if (state.isEdit) R.string.leadership_tasks_compose_title_edit else R.string.leadership_tasks_compose_title_new,
            ),
            onBack = { onEvent(LeadershipTaskComposeEvent.Back) },
        )
        Column(
            modifier = Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(top = 4.dp, bottom = 28.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            // Who it is for. Locked once raised — a task is not handed from one CXO to another.
            Column(modifier = leadershipCardModifier(), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(
                    text = stringResource(R.string.leadership_tasks_field_for),
                    color = MeshaColors.Faint,
                    style = MeshaType.sectionLabel,
                )
                if (state.assigneesLoading && state.assignees.isEmpty()) {
                    LeadershipInlineSpinner()
                } else {
                    LeadershipAssigneeDropdown(
                        assignees = state.assignees,
                        selectedId = state.selectedAssigneeId,
                        enabled = !state.isEdit && !busy,
                        placeholder = stringResource(R.string.leadership_tasks_field_for_placeholder),
                        onSelect = { onEvent(LeadershipTaskComposeEvent.SelectAssignee(it)) },
                    )
                }
            }

            Column(modifier = leadershipCardModifier(), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                LeadershipTextField(
                    value = state.title,
                    onValueChange = { onEvent(LeadershipTaskComposeEvent.TitleChanged(it.take(LEADERSHIP_TASK_TITLE_CAP))) },
                    label = stringResource(R.string.leadership_tasks_field_title),
                    singleLine = true,
                    enabled = !busy,
                    counter = "${state.title.length} / $LEADERSHIP_TASK_TITLE_CAP",
                )
                LeadershipTextField(
                    value = state.body,
                    onValueChange = { onEvent(LeadershipTaskComposeEvent.BodyChanged(it)) },
                    label = stringResource(R.string.leadership_tasks_field_brief),
                    singleLine = false,
                    enabled = !busy,
                )
            }

            Column(modifier = leadershipCardModifier(), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(
                    text = stringResource(R.string.leadership_tasks_attachments_label),
                    color = MeshaColors.Faint,
                    style = MeshaType.sectionLabel,
                )
                state.attachments.forEach { attachment -> // compose-guard:ignore: a draft holds at most LEADERSHIP_TASK_ATTACHMENT_CAP (12) attachments, enforced by the ViewModel
                    LeadershipDraftAttachmentRow(
                        attachment = attachment,
                        enabled = !busy,
                        onRemove = { onEvent(LeadershipTaskComposeEvent.RemoveAttachment(attachment.listKey)) },
                    )
                }
                val recording = state.recordingElapsedMs
                if (recording != null) {
                    LeadershipRecordingRow(elapsedMs = recording, onStop = { onEvent(LeadershipTaskComposeEvent.StopRecording) })
                } else {
                    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        LeadershipGhostButton(
                            label = stringResource(R.string.leadership_tasks_add_voice_note),
                            enabled = !busy && remaining > 0,
                            onClick = startRecording,
                            icon = MeshaIcons.Mic,
                            modifier = Modifier.fillMaxWidth(),
                        )
                        LeadershipGhostButton(
                            label = stringResource(R.string.leadership_tasks_add_media),
                            enabled = !busy && remaining > 0,
                            onClick = {
                                mediaPicker.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageAndVideo)) // camera-only:ignore: a task attachment is not proof of work; gallery and file picks are the maintainer's explicit ask (2026-09-04)
                            },
                            icon = MeshaIcons.Photo,
                            modifier = Modifier.fillMaxWidth(),
                        )
                        LeadershipGhostButton(
                            label = stringResource(R.string.leadership_tasks_add_file),
                            enabled = !busy && remaining > 0,
                            onClick = { filePicker.launch(arrayOf("*/*")) },
                            icon = MeshaIcons.Document,
                            modifier = Modifier.fillMaxWidth(),
                        )
                    }
                }
            }

            if (state.message != null) {
                Text(
                    text = state.message,
                    color = MeshaColors.Danger,
                    style = MeshaType.caption,
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(horizontal = 16.dp)
                        .clickable { onEvent(LeadershipTaskComposeEvent.DismissMessage) },
                )
            }

            LeadershipPrimaryButton(
                label = stringResource(
                    when {
                        state.sending -> R.string.leadership_tasks_sending
                        state.isEdit -> R.string.leadership_tasks_save
                        else -> R.string.leadership_tasks_send
                    },
                ),
                enabled = state.canSend && !busy,
                onClick = { onEvent(LeadershipTaskComposeEvent.Send) },
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
            )
        }
    }
}

/** The CXO picker: one closed dropdown, the selected name on the field, the rest on tap. */
@Composable
private fun LeadershipAssigneeDropdown(
    assignees: List<LeadershipAssigneeUi>,
    selectedId: String?,
    enabled: Boolean,
    placeholder: String,
    onSelect: (String) -> Unit,
) {
    var open by remember { mutableStateOf(false) }
    val selectedName = assignees.firstOrNull { it.userId == selectedId }?.name
    Box(modifier = Modifier.fillMaxWidth()) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(min = 54.dp)
                .clip(RoundedCornerShape(14.dp))
                .background(MeshaColors.Surf)
                .border(1.dp, if (open) MeshaColors.BrandD else MeshaColors.Hair, RoundedCornerShape(14.dp))
                .clickable(enabled = enabled, role = Role.DropdownList) { open = true }
                .padding(horizontal = 16.dp, vertical = 14.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text(
                text = selectedName ?: placeholder,
                color = when {
                    selectedName == null -> MeshaColors.Muted
                    enabled -> MeshaColors.Ink
                    else -> MeshaColors.Faint
                },
                style = MeshaType.bodyStrong,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Icon(
                imageVector = MeshaIcons.ChevronDown,
                contentDescription = null,
                tint = if (enabled) MeshaColors.Muted else MeshaColors.Faint,
                modifier = Modifier.size(18.dp),
            )
        }
        DropdownMenu(
            expanded = open,
            onDismissRequest = { open = false },
            containerColor = MeshaColors.Surf,
            modifier = Modifier.fillMaxWidth(0.86f),
        ) {
            assignees.forEach { assignee -> // compose-guard:ignore: the handful of CXOs a task can go to (five today), never park-scale data
                DropdownMenuItem(
                    text = {
                        Text(
                            text = assignee.name,
                            color = if (assignee.userId == selectedId) MeshaColors.BrandD else MeshaColors.Ink,
                            style = MeshaType.bodyStrong,
                        )
                    },
                    onClick = {
                        open = false
                        onSelect(assignee.userId)
                    },
                )
            }
        }
    }
}

@Composable
private fun LeadershipTextField(
    value: String,
    onValueChange: (String) -> Unit,
    label: String,
    singleLine: Boolean,
    enabled: Boolean,
    counter: String? = null,
) {
    OutlinedTextField(
        value = value,
        onValueChange = onValueChange,
        modifier = Modifier.fillMaxWidth().heightIn(min = 54.dp),
        label = { Text(label) },
        supportingText = counter?.let { { Text(text = it, color = MeshaColors.Faint, style = MeshaType.caption) } },
        singleLine = singleLine,
        minLines = if (singleLine) 1 else 4,
        maxLines = if (singleLine) 1 else 10,
        enabled = enabled,
        shape = RoundedCornerShape(14.dp),
        keyboardOptions = KeyboardOptions(
            capitalization = KeyboardCapitalization.Sentences,
            imeAction = if (singleLine) ImeAction.Next else ImeAction.Default,
        ),
        colors = OutlinedTextFieldDefaults.colors(
            focusedTextColor = MeshaColors.Ink,
            unfocusedTextColor = MeshaColors.Ink,
            focusedBorderColor = MeshaColors.BrandD,
            unfocusedBorderColor = MeshaColors.Hair,
            focusedLabelColor = MeshaColors.BrandD,
            unfocusedLabelColor = MeshaColors.Muted,
            cursorColor = MeshaColors.BrandD,
        ),
    )
}

@Composable
private fun LeadershipDraftAttachmentRow(
    attachment: LeadershipDraftAttachmentUi,
    enabled: Boolean,
    onRemove: () -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
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
            when (attachment.uploadState) {
                LeadershipDraftUploadState.UPLOADING -> LeadershipInlineSpinner()
                LeadershipDraftUploadState.UPLOADED -> Icon(
                    imageVector = MeshaIcons.Check,
                    contentDescription = null,
                    tint = MeshaColors.Ok,
                    modifier = Modifier.size(18.dp),
                )
                LeadershipDraftUploadState.FAILED -> Icon(
                    imageVector = MeshaIcons.Warn,
                    contentDescription = null,
                    tint = MeshaColors.Danger,
                    modifier = Modifier.size(18.dp),
                )
                LeadershipDraftUploadState.PENDING -> Unit
            }
            Box(
                modifier = Modifier
                    .size(48.dp)
                    .clip(RoundedCornerShape(24.dp))
                    .clickable(enabled = enabled, role = Role.Button, onClick = onRemove),
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    imageVector = MeshaIcons.Close,
                    contentDescription = stringResource(R.string.leadership_tasks_remove_attachment),
                    tint = if (enabled) MeshaColors.Muted else MeshaColors.Faint,
                    modifier = Modifier.size(16.dp),
                )
            }
        }
        // A voice note just recorded plays back in place, so the sender hears what they are sending.
        if (attachment.kind == LeadershipAttachmentKind.AUDIO && attachment.localPath.isNotBlank()) {
            LeadershipAudioPlayerRow(localPath = attachment.localPath)
        }
    }
}

/** The live recording row: a pulsing-red dot, the elapsed clock, and one Stop. */
@Composable
private fun LeadershipRecordingRow(elapsedMs: Long, onStop: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf2)
            .padding(horizontal = 14.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Box(modifier = Modifier.size(10.dp).clip(RoundedCornerShape(5.dp)).background(MeshaColors.Danger))
        Text(
            text = formatClock(elapsedMs),
            color = MeshaColors.Ink,
            style = MeshaType.bodyStrong,
            modifier = Modifier.weight(1f),
        )
        LeadershipPrimaryButton(
            label = stringResource(R.string.leadership_tasks_stop_recording),
            enabled = true,
            onClick = onStop,
        )
    }
}

/** The title cap the phone enforces while typing; the backend refuses anything longer too. */
const val LEADERSHIP_TASK_TITLE_CAP = 80
