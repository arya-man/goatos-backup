package sg.mesha.goatos.feature.weighing

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.component.MeshaIconButton
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.R

data class WeighingUiState(
    val title: String = "Weighing",
    val scopeLabel: String = "",
    val hasScope: Boolean = false,
    val assignments: List<WeighingAssignmentUiRow> = emptyList(),
    val visibleRows: List<WeighingRosterUiRow> = emptyList(),
    val totalExpected: Int = 0,
    val individualDrafts: List<WeighingDraftUiRow> = emptyList(),
    val shedDrafts: List<WeighingDraftUiRow> = emptyList(),
    val selectedAnimalId: String? = null,
    val selectedAnimalLabel: String? = null,
    val scanInput: String = "",
    val weightInput: String = "",
    val message: String? = null,
    val actionInFlight: Boolean = false,
    val loading: Boolean = false,
    val category: String = "",
) {
    val isShedPartition: Boolean get() = category == "per_shed_partition"
    val individualCompleted: Int get() = individualDrafts.count { it.readyToSubmit }
    val shedCompleted: Int get() = shedDrafts.count { it.readyToSubmit }
    val progress: Float get() = when {
        isShedPartition -> if (shedCompleted > 0) 1f else 0f
        totalExpected <= 0 -> 0f
        else -> individualCompleted.toFloat() / totalExpected.toFloat()
    }
    val canRecordIndividual: Boolean get() =
        hasScope && !isShedPartition && !actionInFlight && !selectedAnimalId.isNullOrBlank() && weightInput.toDoubleOrNull()?.let { it > 0.0 } == true
    val canRecordShedPartition: Boolean get() =
        hasScope && isShedPartition && !actionInFlight && weightInput.toDoubleOrNull()?.let { it > 0.0 } == true
}

data class WeighingRosterUiRow(
    val id: String,
    val displayAnimalId: String,
    val expectedLocationLabel: String,
    val actualLocationLabel: String?,
    val status: String,
    val availabilityStatus: String?,
    val wrongShed: Boolean,
)

data class WeighingAssignmentUiRow(
    val campaignId: String,
    val tenantId: String,
    val workGroupId: String,
    val campaignShedId: String,
    val expectedLocationId: String,
    val expectedLocationLabel: String,
    val label: String,
    val category: String,
    val status: String,
    val expectedCount: Int,
    val periodLabel: String,
)

data class WeighingDraftUiRow(
    val id: String,
    val label: String,
    val proofReady: Boolean,
    val readyToSubmit: Boolean,
)

@Composable
fun WeighingScreen(
    state: WeighingUiState,
    onScanInputChange: (String) -> Unit = {},
    onScanSubmit: () -> Unit = {},
    onWeightChange: (String) -> Unit = {},
    onRecordIndividual: () -> Unit = {},
    onRecordShedPartition: () -> Unit = {},
    onOpenAssignment: (WeighingAssignmentUiRow) -> Unit = {},
    onRefresh: () -> Unit = {},
    modifier: Modifier = Modifier,
) {
    if (!state.hasScope) {
        RefreshOnResume(onRefresh = onRefresh)
    }
    Scaffold(
        modifier = modifier.fillMaxSize(),
        containerColor = MeshaColors.PageBg,
    ) { padding ->
        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .background(MeshaColors.PageBg)
                .padding(padding)
                .padding(horizontal = 16.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            item {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    MeshaScreenHeader(
                        title = if (state.hasScope) state.title else "My work",
                        eyebrow = "WEIGHING",
                        contentPadding = androidx.compose.foundation.layout.PaddingValues(0.dp),
                        actions = {
                            if (state.hasScope) {
                                MeshaIconButton(
                                    icon = MeshaIcons.Refresh,
                                    contentDescription = stringResource(R.string.weighing_refresh),
                                    onClick = onRefresh,
                                )
                            } else {
                                SyncIconButton(
                                    isSyncing = state.loading,
                                    onSync = onRefresh,
                                    contentDescription = stringResource(R.string.weighing_refresh),
                                )
                            }
                            MeshaIconButton(
                                icon = MeshaIcons.Bell,
                                contentDescription = stringResource(R.string.weighing_alerts),
                                onClick = {},
                            )
                        },
                    )
                    if (state.scopeLabel.isNotBlank()) {
                        Text(
                            text = state.scopeLabel,
                            style = MaterialTheme.typography.bodyMedium,
                            color = MeshaColors.Muted,
                            maxLines = 2,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                    if (state.hasScope) {
                        LinearProgressIndicator(
                            progress = { state.progress },
                            modifier = Modifier.fillMaxWidth(),
                        )
                        if (state.isShedPartition) {
                            Text(
                                text = if (state.shedCompleted > 0) "Shed / partition result ready" else "Shed / partition result pending",
                                style = MaterialTheme.typography.labelLarge,
                                color = MeshaColors.Muted,
                            )
                        } else {
                            Text(
                                text = "${state.individualCompleted}/${state.totalExpected} individual weights ready",
                                style = MaterialTheme.typography.labelLarge,
                                color = MeshaColors.Muted,
                            )
                        }
                        WeighingCapturePanel(
                            state = state,
                            onScanInputChange = onScanInputChange,
                            onScanSubmit = onScanSubmit,
                            onWeightChange = onWeightChange,
                            onRecordIndividual = onRecordIndividual,
                            onRecordShedPartition = onRecordShedPartition,
                        )
                    } else {
                        if (state.loading) {
                            LoadingWorkSkeleton()
                        } else if (state.assignments.isEmpty()) {
                            EmptyWorkCard(
                                title = "No weighing work",
                                body = "Assigned shed and partition tasks will appear here when the weekly plan is published.",
                            )
                        }
                    }
                    state.message?.takeIf { it.isNotBlank() }?.let {
                        MessageStrip(it)
                    }
                }
            }
            if (!state.hasScope && state.assignments.isNotEmpty()) {
                item { SectionTitle("TODAY · WED 29 JUL") }
                items(state.assignments, key = { it.campaignShedId }) { row ->
                    AssignmentRow(row = row, onOpen = { onOpenAssignment(row) })
                }
            }

            if (state.shedDrafts.isNotEmpty()) {
                item { SectionTitle("Shed / partition result") }
                items(state.shedDrafts, key = { it.id }) { row ->
                    DraftRow(row)
                }
            }

            if (state.individualDrafts.isNotEmpty()) {
                item { SectionTitle("Queued animal weights") }
                items(state.individualDrafts, key = { it.id }) { row ->
                    DraftRow(row)
                }
            }

            if (state.visibleRows.isNotEmpty()) {
                item { SectionTitle("Roster window") }
                items(state.visibleRows, key = { it.id }) { row ->
                    RosterRow(row)
                }
            }
        }
    }
}

@Composable
private fun AssignmentRow(row: WeighingAssignmentUiRow, onOpen: () -> Unit) {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(18.dp)),
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .clickable(onClick = onOpen)
                .padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(9.dp),
        ) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                StatusPill(row.status)
                CategoryPill(row.category)
                Box(Modifier.weight(1f))
                Text(
                    text = "today 10:12",
                    color = MeshaColors.Muted,
                    fontSize = 11.5.sp,
                    fontWeight = FontWeight.W800,
                    modifier = Modifier
                        .clip(RoundedCornerShape(9.dp))
                        .background(MeshaColors.Surf3)
                        .padding(horizontal = 10.dp, vertical = 6.dp),
                )
            }
            Text(
                text = row.label,
                color = MeshaColors.Ink,
                style = MeshaType.cardTitle,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                text = assignmentSummary(row),
                color = MeshaColors.Muted,
                style = MeshaType.cardSubtitle,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            WeighingProgressBar(complete = row.status.equals("Completed", ignoreCase = true), category = row.category)
            Text(
                text = assignmentAction(row),
                color = MeshaColors.BrandD,
                style = MeshaType.cta,
                modifier = Modifier.padding(top = 2.dp),
            )
        }
    }
}

@Composable
private fun StatusPill(status: String) {
    val normalized = status.lowercase()
    val color = when {
        normalized.contains("complete") || normalized.contains("accepted") -> MeshaColors.Ok
        normalized.contains("progress") || normalized.contains("due") || normalized.contains("pending") -> MeshaColors.Warn
        else -> MeshaColors.Muted
    }
    val bg = when {
        normalized.contains("complete") || normalized.contains("accepted") -> MeshaColors.OkX
        normalized.contains("progress") || normalized.contains("due") || normalized.contains("pending") -> MeshaColors.WarnX
        else -> MeshaColors.Surf3
    }
    Text(
        text = if (normalized.contains("progress") || normalized.contains("pending")) "due now" else status.lowercase(),
        color = color,
        fontSize = 12.sp,
        fontWeight = FontWeight.W800,
        modifier = Modifier
            .clip(RoundedCornerShape(9.dp))
            .background(bg)
            .padding(horizontal = 10.dp, vertical = 6.dp),
    )
}

@Composable
private fun CategoryPill(category: String) {
    Text(
        text = weighingCategoryLabel(category),
        color = MeshaColors.Purple,
        fontSize = 12.sp,
        fontWeight = FontWeight.W800,
        modifier = Modifier
            .clip(RoundedCornerShape(9.dp))
            .background(MeshaColors.PurpleX)
            .padding(horizontal = 10.dp, vertical = 6.dp),
    )
}

@Composable
private fun WeighingProgressBar(complete: Boolean, category: String) {
    Box(
        Modifier
            .fillMaxWidth()
            .height(7.dp)
            .clip(RoundedCornerShape(99.dp))
            .background(MeshaColors.Bg),
    ) {
        if (complete) {
            Box(
                Modifier
                    .fillMaxWidth()
                    .height(7.dp)
                    .clip(RoundedCornerShape(99.dp))
                    .background(if (category == "per_shed_partition") MeshaColors.Purple else MeshaColors.Brand),
            )
        }
    }
}

@Composable
private fun assignmentSummary(row: WeighingAssignmentUiRow): String =
    if (row.category == "per_shed_partition") {
        stringResource(R.string.weighing_assignment_summary_lumpsum, row.expectedCount)
    } else {
        stringResource(R.string.weighing_assignment_summary_individual, row.expectedCount)
    }

@Composable
private fun assignmentAction(row: WeighingAssignmentUiRow): String =
    when {
        row.status.equals("Completed", ignoreCase = true) -> stringResource(R.string.weighing_action_view_result)
        row.category == "per_shed_partition" -> stringResource(R.string.weighing_action_record_shed)
        else -> stringResource(R.string.weighing_action_scan_animals)
    }

@Composable
private fun weighingCategoryLabel(category: String): String =
    if (category == "per_shed_partition") {
        stringResource(R.string.weighing_category_lumpsum)
    } else {
        stringResource(R.string.weighing_category_individual)
    }

@Composable
private fun EmptyWorkCard(title: String, body: String) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(14.dp))
            .padding(horizontal = 13.dp, vertical = 12.dp),
    ) {
        Box(
            modifier = Modifier
                .size(34.dp)
                .clip(RoundedCornerShape(10.dp))
                .background(MeshaColors.BrandTint),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = MeshaIcons.Module,
                contentDescription = null,
                tint = MeshaColors.Brand,
                modifier = Modifier.size(17.dp),
            )
        }
        Spacer(Modifier.width(11.dp))
        Column(modifier = Modifier.weight(1f)) {
            Text(title, color = MeshaColors.Ink, style = MeshaType.bodyStrong)
            Text(
                text = body,
                color = MeshaColors.Muted,
                style = MeshaType.cardSubtitle,
                modifier = Modifier.padding(top = 3.dp),
            )
        }
    }
}

@Composable
private fun LoadingWorkSkeleton() {
    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        repeat(2) {
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(18.dp))
                    .background(MeshaColors.Surf)
                    .border(1.dp, MeshaColors.Hair, RoundedCornerShape(18.dp))
                    .padding(14.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    SkeletonBlock(width = 78.dp, height = 28.dp)
                    SkeletonBlock(width = 72.dp, height = 28.dp)
                }
                SkeletonBlock(width = 142.dp, height = 18.dp)
                SkeletonBlock(width = 220.dp, height = 14.dp)
                SkeletonBlock(width = null, height = 7.dp)
            }
        }
    }
}

@Composable
private fun SkeletonBlock(width: androidx.compose.ui.unit.Dp?, height: androidx.compose.ui.unit.Dp) {
    Box(
        modifier = Modifier
            .then(if (width == null) Modifier.fillMaxWidth() else Modifier.width(width))
            .height(height)
            .clip(RoundedCornerShape(99.dp))
            .background(MeshaColors.Surf3),
    )
}

@Composable
private fun MessageStrip(message: String) {
    val isError = message.contains("failed", ignoreCase = true) ||
        message.contains("couldn't", ignoreCase = true) ||
        message.contains("error", ignoreCase = true)
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(if (isError) MeshaColors.DangerX else MeshaColors.Surf)
            .border(1.dp, if (isError) MeshaColors.Danger else MeshaColors.Hair, RoundedCornerShape(12.dp))
            .padding(horizontal = 12.dp, vertical = 10.dp),
    ) {
        Icon(
            imageVector = if (isError) MeshaIcons.Warn else MeshaIcons.CheckCircle,
            contentDescription = null,
            tint = if (isError) MeshaColors.Danger else MeshaColors.BrandD,
            modifier = Modifier.size(16.dp),
        )
        Spacer(Modifier.width(9.dp))
        Text(
            text = message,
            color = if (isError) MeshaColors.Danger else MeshaColors.Muted,
            style = MeshaType.cardSubtitle,
            modifier = Modifier.weight(1f),
        )
    }
}

@Composable
private fun WeighingCapturePanel(
    state: WeighingUiState,
    onScanInputChange: (String) -> Unit,
    onScanSubmit: () -> Unit,
    onWeightChange: (String) -> Unit,
    onRecordIndividual: () -> Unit,
    onRecordShedPartition: () -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        if (state.isShedPartition) {
            WeighingActionCard(
                iconLabel = "KG",
                title = "Record shed result",
                body = "One live video is required for this shed / partition result.",
                tone = MeshaColors.Purple,
                background = MeshaColors.PurpleX,
            )
        } else {
            WeighingActionCard(
                iconLabel = "RFID",
                title = state.selectedAnimalLabel ?: "Scan RFID tag now",
                body = if (state.selectedAnimalLabel == null) {
                    "Keyboard-wedge scans are captured on this screen only."
                } else {
                    "Animal matched. Add weight and capture video proof."
                },
                tone = MeshaColors.Brand,
                background = MeshaColors.BrandTint,
            )
            InlineEntryCard(
                label = "RFID / animal tag",
                value = state.scanInput,
                placeholder = "Type tag only for manual retry",
                onValueChange = onScanInputChange,
                actionLabel = "Match",
                actionEnabled = !state.actionInFlight && state.scanInput.isNotBlank(),
                onAction = onScanSubmit,
            )
        }
        InlineEntryCard(
            label = "Weight",
            value = state.weightInput,
            placeholder = "0.0",
            suffix = "kg",
            onValueChange = onWeightChange,
            actionLabel = if (state.isShedPartition) "Record shed" else "Save weight",
            actionEnabled = if (state.isShedPartition) state.canRecordShedPartition else state.canRecordIndividual,
            onAction = if (state.isShedPartition) onRecordShedPartition else onRecordIndividual,
        )
    }
}

@Composable
private fun WeighingActionCard(
    iconLabel: String,
    title: String,
    body: String,
    tone: Color,
    background: Color,
) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(14.dp))
            .padding(horizontal = 13.dp, vertical = 12.dp),
    ) {
        Box(
            modifier = Modifier
                .size(34.dp)
                .clip(RoundedCornerShape(10.dp))
                .background(background),
            contentAlignment = Alignment.Center,
        ) {
            Text(
                text = iconLabel,
                color = tone,
                fontSize = 9.sp,
                lineHeight = 10.sp,
                fontWeight = FontWeight.W900,
            )
        }
        Spacer(Modifier.width(11.dp))
        Column(modifier = Modifier.weight(1f)) {
            Text(title, color = MeshaColors.Ink, style = MeshaType.bodyStrong)
            Text(
                text = body,
                color = MeshaColors.Muted,
                style = MeshaType.cardSubtitle,
                modifier = Modifier.padding(top = 3.dp),
            )
        }
        Icon(
            imageVector = MeshaIcons.Video,
            contentDescription = null,
            tint = tone,
            modifier = Modifier.size(18.dp),
        )
    }
}

@Composable
private fun InlineEntryCard(
    label: String,
    value: String,
    placeholder: String,
    onValueChange: (String) -> Unit,
    actionLabel: String,
    actionEnabled: Boolean,
    onAction: () -> Unit,
    suffix: String? = null,
) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(14.dp))
            .padding(horizontal = 13.dp, vertical = 11.dp),
    ) {
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(label.uppercase(), color = MeshaColors.Muted, style = MeshaType.fieldLabel)
            Row(verticalAlignment = Alignment.CenterVertically) {
                BasicTextField(
                    value = value,
                    onValueChange = onValueChange,
                    singleLine = true,
                    textStyle = MeshaType.bodyStrong.copy(color = MeshaColors.Ink),
                    cursorBrush = SolidColor(MeshaColors.Brand),
                    modifier = Modifier.weight(1f),
                    decorationBox = { innerTextField ->
                        Box {
                            if (value.isBlank()) {
                                Text(
                                    text = placeholder,
                                    color = MeshaColors.Faint,
                                    fontSize = 17.sp,
                                    fontWeight = FontWeight.W700,
                                )
                            }
                            innerTextField()
                        }
                    },
                )
                suffix?.let {
                    Text(
                        text = it,
                        color = MeshaColors.Muted,
                        style = MeshaType.caption,
                        modifier = Modifier.padding(start = 6.dp),
                    )
                }
            }
        }
        Spacer(Modifier.width(10.dp))
        Text(
            text = actionLabel,
            color = if (actionEnabled) MeshaColors.BrandD else MeshaColors.Faint,
            style = MeshaType.cta,
            modifier = Modifier
                .minimumInteractiveComponentSize()
                .clip(RoundedCornerShape(999.dp))
                .background(if (actionEnabled) MeshaColors.BrandTint else MeshaColors.Surf3)
                .clickable(
                    enabled = actionEnabled,
                    interactionSource = remember { MutableInteractionSource() },
                    indication = null,
                    onClick = onAction,
                )
                .padding(horizontal = 12.dp, vertical = 8.dp),
        )
    }
}

@Composable
private fun SectionTitle(text: String) {
    Text(
        text = text,
        style = MeshaType.sectionLabel,
        color = MeshaColors.Muted,
        modifier = Modifier.padding(top = 8.dp),
    )
}

@Composable
private fun RosterRow(row: WeighingRosterUiRow) {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(14.dp)),
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(12.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
            ) {
                Text(
                    text = row.displayAnimalId,
                    style = MeshaType.cardTitle,
                    color = MeshaColors.Ink,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    text = row.status,
                    style = MeshaType.caption,
                    color = MeshaColors.Muted,
                )
            }
            Text(
                text = "Expected: ${row.expectedLocationLabel}",
                style = MeshaType.cardSubtitle,
                color = MeshaColors.Muted,
            )
            row.actualLocationLabel?.takeIf { it.isNotBlank() && it != row.expectedLocationLabel }?.let {
                Text(
                    text = "Current: $it",
                    style = MeshaType.cardSubtitle,
                    color = MeshaColors.Danger,
                )
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                if (row.wrongShed) {
                    AssistChip(onClick = {}, label = { Text("Wrong shed") })
                }
                row.availabilityStatus?.takeIf { it.isNotBlank() }?.let {
                    AssistChip(onClick = {}, label = { Text(it) })
                }
            }
        }
    }
}

@Composable
private fun DraftRow(row: WeighingDraftUiRow) {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(14.dp)),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(12.dp),
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = row.label,
                    style = MeshaType.bodyStrong,
                    color = MeshaColors.Ink,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    text = if (row.proofReady) "Proof ready" else "Proof required",
                    style = MeshaType.cardSubtitle,
                    color = MeshaColors.Muted,
                )
            }
            Text(
                text = if (row.readyToSubmit) "Ready" else "Draft",
                style = MeshaType.caption,
                color = if (row.readyToSubmit) MeshaColors.BrandD else MeshaColors.Muted,
            )
        }
    }
}
