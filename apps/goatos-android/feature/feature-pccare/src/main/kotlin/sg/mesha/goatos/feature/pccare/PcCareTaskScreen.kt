package sg.mesha.goatos.feature.pccare

// telemetry:exempt pure stateless renderer; PcCareTaskViewModel (in :app) owns the pc_care_*
// AnalyticsEvents + CrashReporter wiring for scans, captures, and submits.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.ProofMediaPreview
import sg.mesha.goatos.core.ui.ProofMediaPreviewKind
import sg.mesha.goatos.core.ui.SyncIconButton

/**
 * ONE PC Care task: free-flow tag scanning, per-animal parallel proof slots, whole-task submit.
 *
 * The animal list renders one row per scanned animal with N slot chips iterated from the task's
 * BACKEND-OWNED `expectedSlots`. Slots are PARALLEL — a chip's enabled state depends only on its
 * own state plus the task lifecycle lock, never on a sibling slot.
 */
@Composable
fun PcCareTaskScreen(
    state: PcCareTaskUiState,
    onEvent: (PcCareTaskEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    val taskProofMode = state.taskProofSlot != null
    // No RefreshOnResume: this is a scan-capture flow — a resume-triggered refresh is deliberately
    // skipped so it never disrupts mid-entry scanning (docs/decisions/android-offline-first.md);
    // the ViewModel's own status poll keeps peer work and the submit lock fresh instead.
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = state.title,
            subtitle = listOf(state.locationDisplay, state.parkLabel, state.dateLabel)
                .filter { it.isNotBlank() }
                .joinToString(" · ")
                .takeIf { it.isNotBlank() },
            onBack = { onEvent(PcCareTaskEvent.Back) },
            actions = {
                SyncIconButton(
                    isSyncing = state.isRefreshing,
                    onSync = { onEvent(PcCareTaskEvent.Refresh) },
                )
            },
        )
        LazyColumn(
            modifier = Modifier.weight(1f),
            contentPadding = if (taskProofMode) {
                PaddingValues(horizontal = 16.dp, vertical = 12.dp)
            } else {
                PaddingValues(bottom = 16.dp)
            },
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            // Bluetooth RFID reader banner — the weighing capture screen's shape: green when the
            // reader is live (scans flow straight in), otherwise the farm-worded status with a
            // tap-to-reconnect action. Roster mode records by tapping a listed RFID, so the
            // reader banner and scan row stay off that face.
            if (state.readerStatusLabel.isNotBlank() && !state.isLocked && !state.rosterMode && !taskProofMode) {
                item(key = "reader_banner") {
                    PcCareReaderBanner(
                        name = state.readerName,
                        statusLabel = state.readerStatusLabel,
                        connected = state.readerConnected,
                        onReconnect = { onEvent(PcCareTaskEvent.ReconnectReader) },
                    )
                }
            }
            if (state.assigneeLine.isNotBlank()) {
                item(key = "assignees") {
                    Text(
                        text = state.assigneeLine,
                        color = MeshaColors.Muted,
                        style = MeshaType.caption,
                        modifier = Modifier.padding(horizontal = 16.dp),
                    )
                }
            }

            // The verifier sent this task back — their sentence, verbatim.
            if (state.reworkReason.isNotBlank()) {
                item(key = "rework_banner") { PcCareBanner(text = state.reworkReason, danger = true) }
            }

            // In review / approved: the task is read-only.
            if (state.isLocked && state.lockNotice.isNotBlank()) {
                item(key = "lock_banner") { PcCareBanner(text = state.lockNotice, danger = false) }
            }

            // Scan entry drives the scan-and-record flow; in roster mode it appears only as the
            // fallback when the pen lists no animals (so the operator is never stuck).
            if (!state.isLocked && !taskProofMode && (!state.rosterMode || state.rosterRows.isEmpty())) {
                if (state.rosterEmptyNotice.isNotBlank()) {
                    item(key = "roster_empty") { PcCareBanner(text = state.rosterEmptyNotice, danger = false) }
                }
                item(key = "scan_row") {
                    PcCareScanRow(
                        input = state.scanInput,
                        onInputChange = { onEvent(PcCareTaskEvent.ScanInputChanged(it)) },
                        onSubmit = { onEvent(PcCareTaskEvent.SubmitTypedScan) },
                    )
                }
            }

            if (state.scanNotice.isNotBlank()) {
                item(key = "scan_notice") { PcCareBanner(text = state.scanNotice, danger = false) }
            }

            if (state.inventoryRequirements.isNotEmpty()) {
                item(key = "inventory_requirements") {
                    PcCareInventoryRequirementsCard(state.inventoryRequirements)
                }
            }

            if (taskProofMode && (state.taskProofPhotoSlot != null || state.taskProofVideoSlot != null)) {
                state.taskProofPhotoSlot?.let { proofSlot ->
                    item(key = "task_proof_photo") {
                        PcCareTaskProofAction(
                            title = "Take photo",
                            subtitle = "Photo evidence",
                            icon = MeshaIcons.Plus,
                            slot = proofSlot,
                            locked = state.isLocked,
                            loadingLabel = "Saving photo",
                            retryLabel = "Retry photo",
                            replaceLabel = "Replace photo",
                            capturedLabel = proofSlot.statusLabel.ifBlank { "Photo captured" },
                            onRecord = { onEvent(PcCareTaskEvent.RecordTaskProof(proofSlot.fieldKey, "photo")) },
                        )
                    }
                }
                state.taskProofVideoSlot?.let { proofSlot ->
                    item(key = "task_proof_video") {
                        PcCareTaskProofAction(
                            title = "Record video",
                            subtitle = "Video evidence",
                            icon = MeshaIcons.Video,
                            slot = proofSlot,
                            locked = state.isLocked,
                            loadingLabel = "Saving video",
                            retryLabel = "Retry video",
                            replaceLabel = "Replace video",
                            capturedLabel = proofSlot.statusLabel.ifBlank { "Video captured" },
                            onRecord = { onEvent(PcCareTaskEvent.RecordTaskProof(proofSlot.fieldKey, "video")) },
                        )
                    }
                }
            }

            if (!taskProofMode && state.animalCountLabel.isNotBlank()) {
                item(key = "animal_count") {
                    Text(
                        text = state.animalCountLabel,
                        color = MeshaColors.Muted,
                        style = MeshaType.pillStrong,
                        modifier = Modifier.padding(horizontal = 16.dp),
                    )
                }
            }

            if (taskProofMode) {
                // Task-level proof mode has no animal rows.
            } else if (state.rosterMode) {
                // Roster mode: one tappable row per RFID in the pen — tap to record that animal.
                items(
                    count = state.rosterRows.size,
                    key = { index -> "roster_${state.rosterRows[index].key}" },
                ) { index ->
                    PcCareRosterRow(
                        row = state.rosterRows[index],
                        locked = state.isLocked,
                        onTap = { onEvent(PcCareTaskEvent.RosterTapped(state.rosterRows[index].key)) },
                    )
                }
            } else {
                items(
                    count = state.animals.size,
                    // Stable per-animal key: the normalized tag, unique in this task by the
                    // duplicate rule (bounded list — the repository caps the observed window).
                    key = { index -> state.animals[index].key },
                ) { index ->
                    PcCareAnimalRow(
                        animal = state.animals[index],
                        locked = state.isLocked,
                        onOpen = { onEvent(PcCareTaskEvent.RosterTapped(state.animals[index].key)) },
                        onRecordSlot = { fieldKey ->
                            onEvent(PcCareTaskEvent.RecordSlot(state.animals[index].key, fieldKey))
                        },
                    )
                }
            }
        }

        state.message?.let { message ->
            Text(
                text = message,
                color = MeshaColors.Warn,
                style = MeshaType.caption,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
            )
        }

        if (!state.isLocked) {
            PcCareSubmitBar(state = state, onEvent = onEvent)
        }
    }

    if (state.showSubmitConfirmation) {
        PcCareSubmitConfirmationDialog(
            animalCountLabel = state.animalCountLabel,
            onConfirm = { onEvent(PcCareTaskEvent.ConfirmSubmit) },
            onDismiss = { onEvent(PcCareTaskEvent.DismissSubmitConfirmation) },
        )
    }
}

@Composable
private fun PcCareInventoryRequirementsCard(requirements: List<PcCareInventoryRequirementUi>) {
    Column(
        modifier = pcCareCardModifier(enabled = false, onClick = null),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        requirements.forEach { row ->
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(10.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = row.vaccineLabel,
                    color = MeshaColors.Ink,
                    style = MeshaType.cardSubtitle,
                    modifier = Modifier.weight(1f),
                )
                Text(text = row.requiredDosesLabel, color = MeshaColors.BrandD, style = MeshaType.pillStrong)
            }
        }
    }
}

@Composable
private fun PcCareBanner(text: String, danger: Boolean) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(if (danger) MeshaColors.DangerX else MeshaColors.Surf2)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(12.dp))
            .padding(12.dp),
    ) {
        Text(
            text = text,
            color = if (danger) MeshaColors.Danger else MeshaColors.Ink,
            style = MeshaType.cardSubtitle,
        )
    }
}

@Composable
private fun PcCareScanRow(
    input: String,
    onInputChange: (String) -> Unit,
    onSubmit: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp),
        horizontalArrangement = Arrangement.spacedBy(10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        OutlinedTextField(
            value = input,
            onValueChange = onInputChange,
            modifier = Modifier.weight(1f),
            placeholder = { Text(text = "Scan or type a tag", color = MeshaColors.Faint) },
            singleLine = true,
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
            keyboardActions = KeyboardActions(onDone = { onSubmit() }),
            colors = OutlinedTextFieldDefaults.colors(
                focusedTextColor = MeshaColors.Ink,
                unfocusedTextColor = MeshaColors.Ink,
                focusedBorderColor = MeshaColors.BrandD,
                unfocusedBorderColor = MeshaColors.Hair,
                cursorColor = MeshaColors.BrandD,
            ),
        )
        OutlinedButton(onClick = onSubmit) {
            Text(text = "Add", color = MeshaColors.BrandD, style = MeshaType.pillStrong)
        }
    }
}

@Composable
private fun PcCareAnimalRow(
    animal: PcCareAnimalUi,
    locked: Boolean,
    onOpen: () -> Unit,
    onRecordSlot: (String) -> Unit,
) {
    Column(
        // The whole card opens the animal's own capture screen (the roster drill) — the
        // per-slot Record buttons below stay as inline shortcuts.
        modifier = pcCareCardModifier(enabled = !locked, onClick = onOpen.takeIf { !locked }),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = animal.tagLabel,
                color = MeshaColors.Ink,
                style = MeshaType.cardTitle,
                modifier = Modifier.weight(1f),
            )
        }
        if (animal.scannedByLine.isNotBlank()) {
            Text(text = animal.scannedByLine, color = MeshaColors.Muted, style = MeshaType.caption)
        }
        animal.slots.forEach { slot ->
            PcCareSlotChipRow(
                slot = slot,
                locked = locked,
                onRecord = { onRecordSlot(slot.fieldKey) },
            )
        }
    }
}

/**
 * One slot's chip line: label, live status, and its own Record / Record again button. The button
 * is gated ONLY by this slot's [PcCareSlotChipUi.canRecord] and the task [locked] state — never
 * by a sibling slot.
 */
@Composable
private fun PcCareSlotChipRow(
    slot: PcCareSlotChipUi,
    locked: Boolean,
    onRecord: () -> Unit,
    onRecordPhoto: (() -> Unit)? = null,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(10.dp))
            .background(MeshaColors.Surf2)
            .padding(horizontal = 10.dp, vertical = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            // Backend-owned slot label, verbatim.
            Text(text = slot.label, color = MeshaColors.Ink, style = MeshaType.pillStrong)
            val (statusColor) = when (slot.state) {
                PcCareSlotState.SYNCED -> arrayOf(MeshaColors.Ok)
                PcCareSlotState.PEER -> arrayOf(MeshaColors.BrandD)
                PcCareSlotState.FAILED -> arrayOf(MeshaColors.Danger)
                PcCareSlotState.WORKING -> arrayOf(MeshaColors.Warn)
                PcCareSlotState.EMPTY -> arrayOf(MeshaColors.Muted)
            }
            if (slot.statusLabel.isNotBlank()) {
                Text(text = slot.statusLabel, color = statusColor, style = MeshaType.caption)
            }
            if (slot.hintLabel.isNotBlank()) {
                Text(text = slot.hintLabel, color = MeshaColors.Faint, style = MeshaType.caption)
            }
            if (slot.previewPath.isNotBlank()) {
                ProofMediaPreview(
                    path = slot.previewPath,
                    kind = when (slot.previewKind) {
                        PcCareProofPreviewKind.PHOTO -> ProofMediaPreviewKind.Photo
                        PcCareProofPreviewKind.VIDEO -> ProofMediaPreviewKind.Video
                    },
                    modifier = Modifier.padding(top = 6.dp),
                )
            }
        }
        if (!locked && slot.canRecord) {
            if (onRecordPhoto == null) {
                PcCarePrimaryButton(
                    label = when (slot.state) {
                        PcCareSlotState.EMPTY -> "Record"
                        else -> "Record again"
                    },
                    enabled = true,
                    onClick = onRecord,
                )
            } else {
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    PcCarePrimaryButton(label = "Photo", enabled = true, onClick = onRecordPhoto)
                    PcCarePrimaryButton(label = "Video", enabled = true, onClick = onRecord)
                }
            }
        }
    }
}

@Composable
private fun PcCareTaskProofAction(
    title: String,
    subtitle: String,
    icon: ImageVector,
    slot: PcCareSlotChipUi,
    locked: Boolean,
    loadingLabel: String,
    retryLabel: String,
    replaceLabel: String,
    capturedLabel: String,
    onRecord: () -> Unit,
) {
    val failed = slot.state == PcCareSlotState.FAILED
    val synced = slot.state == PcCareSlotState.SYNCED
    val working = slot.state == PcCareSlotState.WORKING
    val captured = slot.state != PcCareSlotState.EMPTY
    val enabled = !locked && slot.canRecord && !working
    val border = when {
        failed -> MeshaColors.Danger
        synced -> MeshaColors.Ok
        captured -> MeshaColors.Brand.copy(alpha = 0.5f)
        else -> MeshaColors.Hair
    }
    val iconBg = when {
        failed -> MeshaColors.Danger.copy(alpha = 0.14f)
        synced -> MeshaColors.Ok.copy(alpha = 0.14f)
        captured -> MeshaColors.Brand.copy(alpha = 0.14f)
        else -> MeshaColors.Surf2
    }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(min = 82.dp)
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, border, RoundedCornerShape(18.dp))
            .clickable(enabled = enabled && !captured, onClick = onRecord)
            .padding(14.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        androidx.compose.foundation.layout.Box(
            modifier = Modifier.size(42.dp).clip(RoundedCornerShape(14.dp)).background(iconBg),
            contentAlignment = Alignment.Center,
        ) {
            when {
                working -> CircularProgressIndicator(modifier = Modifier.size(18.dp), strokeWidth = 2.dp, color = MeshaColors.Brand)
                failed -> Icon(MeshaIcons.Warn, contentDescription = null, tint = MeshaColors.Danger, modifier = Modifier.size(22.dp))
                synced -> Icon(MeshaIcons.Check, contentDescription = null, tint = MeshaColors.Ok, modifier = Modifier.size(22.dp))
                captured -> Icon(icon, contentDescription = null, tint = MeshaColors.BrandD, modifier = Modifier.size(22.dp))
                else -> Icon(icon, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(22.dp))
            }
        }
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Text(
                text = when {
                    working -> loadingLabel
                    failed -> retryLabel
                    captured -> capturedLabel
                    else -> title
                },
                color = proofActionTitleColor(failed = failed, enabled = enabled, captured = captured, working = working),
                style = MeshaType.cardTitle,
            )
            Text(text = subtitle, color = MeshaColors.Muted, style = MeshaType.cardSubtitle)
            if (slot.previewPath.isNotBlank()) {
                ProofMediaPreview(
                    path = slot.previewPath,
                    kind = when (slot.previewKind) {
                        PcCareProofPreviewKind.PHOTO -> ProofMediaPreviewKind.Photo
                        PcCareProofPreviewKind.VIDEO -> ProofMediaPreviewKind.Video
                    },
                )
                if (enabled) {
                    PcCareProofRetryButton(label = if (failed) retryLabel else replaceLabel, onClick = onRecord)
                }
            } else if (!working && enabled) {
                PcCareProofRetryButton(label = if (captured || failed) replaceLabel else title, onClick = onRecord)
            }
            if (slot.hintLabel.isNotBlank()) {
                Text(text = slot.hintLabel, color = MeshaColors.Faint, style = MeshaType.caption)
            }
        }
    }
}

@Composable
private fun proofActionTitleColor(failed: Boolean, enabled: Boolean, captured: Boolean, working: Boolean): Color =
    when {
        failed -> MeshaColors.Danger
        enabled || captured || working -> MeshaColors.Ink
        else -> MeshaColors.Faint
    }

@Composable
private fun PcCareProofRetryButton(label: String, onClick: () -> Unit) {
    Text(
        text = label,
        color = MeshaColors.BrandD,
        style = MeshaType.cta,
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .clickable(onClick = onClick)
            .padding(vertical = 8.dp),
    )
}

@Composable
private fun PcCareSubmitBar(
    state: PcCareTaskUiState,
    onEvent: (PcCareTaskEvent) -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(MeshaColors.Surf)
            .padding(horizontal = 16.dp, vertical = 12.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        if (!state.submitEnabled && state.submitBlockedReason.isNotBlank()) {
            Text(text = state.submitBlockedReason, color = MeshaColors.Muted, style = MeshaType.caption)
        }
        // The weighing surfaces' primary-button chrome (Brand fill, dark label).
        PcCarePrimaryButton(
            label = when {
                state.submitQueued -> "Submitted"
                state.submitInFlight -> "Sending…"
                else -> "Submit task"
            },
            enabled = state.submitEnabled && !state.submitInFlight && !state.submitQueued,
            onClick = { onEvent(PcCareTaskEvent.Submit) },
            modifier = Modifier.fillMaxWidth(),
        )
    }
}

@Composable
private fun PcCareSubmitConfirmationDialog(
    animalCountLabel: String,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
) {
    androidx.compose.material3.AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(text = "Submit this task?", color = MeshaColors.Ink, style = MeshaType.cardTitle) },
        text = {
            Text(
                text = buildString {
                    if (animalCountLabel.isNotBlank()) append("$animalCountLabel recorded. ")
                    append("After submitting, this task is locked until review is complete.")
                },
                color = MeshaColors.Muted,
                style = MeshaType.cardSubtitle,
            )
        },
        dismissButton = {
            androidx.compose.material3.TextButton(onClick = onDismiss) {
                Text(text = "Not yet", color = MeshaColors.Muted, style = MeshaType.cardSubtitle)
            }
        },
        confirmButton = {
            androidx.compose.material3.TextButton(onClick = onConfirm) {
                Text(text = "Submit", color = MeshaColors.Danger, style = MeshaType.cardSubtitle)
            }
        },
        containerColor = MeshaColors.Surf,
    )
}

/**
 * One roster-tap row: the animal's RFID, its live video state, and the tap-to-record affordance.
 * The whole row is tappable while the task is open; a recorded row stays tappable so the
 * operator can re-record (the durable replacement rule).
 */
@Composable
private fun PcCareRosterRow(
    row: PcCareRosterRowUi,
    locked: Boolean,
    onTap: () -> Unit,
) {
    Row(
        modifier = pcCareCardModifier(enabled = !locked && !row.working, onClick = onTap.takeIf { !locked && !row.working }),
        horizontalArrangement = Arrangement.spacedBy(10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(
                text = row.tagLabel,
                color = MeshaColors.Ink,
                style = MeshaType.cardTitle,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (row.statusLabel.isNotBlank()) {
                Text(
                    text = row.statusLabel,
                    color = when {
                        row.done -> MeshaColors.Ok
                        row.working -> MeshaColors.Warn
                        else -> MeshaColors.Muted
                    },
                    style = MeshaType.caption,
                )
            }
        }
        if (!locked) {
            Text(
                text = when {
                    row.working -> "Recording…"
                    row.done -> "Record again"
                    else -> "Record"
                },
                color = if (row.done) MeshaColors.Muted else MeshaColors.BrandD,
                style = MeshaType.pillStrong,
            )
        }
    }
}

/** Bluetooth reader status banner; tapping it (or its action) opens the reader pairing screen. */
@Composable
private fun PcCareReaderBanner(
    name: String,
    statusLabel: String,
    connected: Boolean,
    onReconnect: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(10.dp))
            .background(if (connected) MeshaColors.OkX else MeshaColors.DangerX)
            .border(1.dp, if (connected) MeshaColors.Ok else MeshaColors.Danger, RoundedCornerShape(10.dp))
            .clickable(onClick = onReconnect)
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = "$statusLabel · $name",
            color = if (connected) MeshaColors.Ok else MeshaColors.Danger,
            style = MeshaType.cardSubtitle,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f),
        )
        Text(
            text = if (connected) "Scanning" else "Connect",
            color = if (connected) MeshaColors.Ok else MeshaColors.Danger,
            style = MeshaType.pillStrong,
        )
    }
}
