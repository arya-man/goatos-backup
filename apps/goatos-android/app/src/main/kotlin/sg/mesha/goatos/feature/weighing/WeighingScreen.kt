package sg.mesha.goatos.feature.weighing

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.AssistChip
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.component.MeshaIconButton
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.LoadingSkeletonList
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.feature.scan.ProofUploadStatus
import sg.mesha.goatos.feature.scan.RosterRow
import sg.mesha.goatos.feature.scan.ScanEvent
import sg.mesha.goatos.feature.scan.ScanFeedEntry
import sg.mesha.goatos.feature.scan.ScanFeedTone
import sg.mesha.goatos.feature.scan.ScanReaderConnection
import sg.mesha.goatos.feature.scan.ScanScreen
import sg.mesha.goatos.feature.scan.ScanStatus
import sg.mesha.goatos.feature.scan.ScanTileLabels
import sg.mesha.goatos.feature.scan.ScanUiState
import sg.mesha.goatos.feature.scan.VaccineGroup
import sg.mesha.goatos.R

data class WeighingUiState(
    val title: String = "Weighing",
    val scopeLabel: String = "",
    val hasScope: Boolean = false,
    val plannerMode: Boolean = false,
    val plannerWeekLabel: String = "",
    val plannerPeriodLabel: String = "",
    val plannerDayTabs: List<WeighingDayTabUiRow> = emptyList(),
    val plannerParks: List<WeighingPlannerParkUiRow> = emptyList(),
    val plannerOperators: List<WeighingPlannerOperatorUiRow> = emptyList(),
    val assignments: List<WeighingAssignmentUiRow> = emptyList(),
    val visibleRows: List<WeighingRosterUiRow> = emptyList(),
    val totalExpected: Int = 0,
    val individualDrafts: List<WeighingDraftUiRow> = emptyList(),
    val shedDrafts: List<WeighingDraftUiRow> = emptyList(),
    val selectedAnimalId: String? = null,
    val selectedAnimalLabel: String? = null,
    val scanInput: String = "",
    val weightInput: String = "",
    val animalCountInput: String = "",
    val message: String? = null,
    val actionInFlight: Boolean = false,
    val loading: Boolean = false,
    val category: String = "",
    val readerConnection: ScanReaderConnection? = null,
    val shedProofs: List<WeighingProofUiRow> = emptyList(),
) {
    val isShedPartition: Boolean get() = category.trim().equals("per_shed_partition", ignoreCase = true)
    val individualCompleted: Int get() = individualDrafts.count { it.readyToSubmit }
    val individualSubmitReady: Boolean get() =
        totalExpected > 0 &&
            individualDrafts.count { it.readyToSubmit && it.syncedToBackend } >= totalExpected
    val individualResolved: Int get() = maxOf(
        individualCompleted,
        visibleRows.count { it.isResolved },
    )
    val shedCompleted: Int get() = shedDrafts.count { it.readyToSubmit }
    val progress: Float get() = when {
        isShedPartition -> if (shedCompleted > 0) 1f else 0f
        totalExpected <= 0 -> 0f
        else -> individualResolved.toFloat() / totalExpected.toFloat()
    }
    val canRecordIndividual: Boolean get() =
        hasScope && !isShedPartition && !actionInFlight && !selectedAnimalId.isNullOrBlank() && weightInput.toDoubleOrNull()?.let { it > 0.0 } == true
    val canRecordShedPartition: Boolean get() =
        hasScope && isShedPartition && !actionInFlight &&
            weightInput.toDoubleOrNull()?.let { it > 0.0 } == true &&
            animalCountInput.toIntOrNull()?.let { it > 0 } == true &&
            shedProofs.any { it.status == ProofUploadStatus.SYNCED }
}

data class WeighingProofUiRow(
    val id: String,
    val label: String,
    val status: ProofUploadStatus,
)

data class WeighingPlannerParkUiRow(
    val parkId: String,
    val name: String,
    val kidCount: Int,
    val existingCampaignId: String?,
    val existingCampaignStatus: String?,
    val existingCampaignShedCount: Int,
    val sheds: List<WeighingPlannerShedUiRow>,
) {
    val hasExistingTask: Boolean get() = !existingCampaignStatus.isNullOrBlank()
    val individualKids: Int get() = sheds.filter { it.category == "individual_animal" }.sumOf { it.kidCount }
    val lumpsumSheds: Int get() = sheds.count { it.category != "individual_animal" }
}

data class WeighingDayTabUiRow(
    val dayLabel: String,
    val dateLabel: String,
    val selected: Boolean,
)

data class WeighingPlannerShedUiRow(
    val locationId: String,
    val name: String,
    val kidCount: Int,
    val category: String,
    val selected: Boolean = false,
)

data class WeighingPlannerOperatorUiRow(
    val userId: String,
    val displayName: String,
    val displayCode: String,
)

data class WeighingRosterUiRow(
    val id: String,
    val animalId: String,
    val displayAnimalId: String,
    val expectedLocationLabel: String,
    val actualLocationLabel: String?,
    val status: String,
    val availabilityStatus: String?,
    val wrongShed: Boolean,
    val scannedAtLabel: String? = null,
    val weightInput: String = "",
    val savedWeightLabel: String? = null,
    val canSaveWeight: Boolean = false,
    val weightSaved: Boolean = false,
    val proofCaptureId: String? = null,
    val proofUploadStatus: ProofUploadStatus = ProofUploadStatus.MISSING,
    val proofStatusLabel: String? = null,
    val backendSynced: Boolean = false,
    val weightUpdating: Boolean = false,
    val reuploadRequested: Boolean = false,
) {
    val isResolved: Boolean
        get() = status.equals("weighed", ignoreCase = true) ||
            status.equals("completed", ignoreCase = true) ||
            status.equals("accepted", ignoreCase = true) ||
            availabilityStatus.equals("unavailable", ignoreCase = true) ||
            !availabilityStatus.isNullOrBlank()
}

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
) {
    val isClosed: Boolean
        get() = status.equals("Completed", ignoreCase = true) ||
            status.equals("Accepted", ignoreCase = true) ||
            status.equals("Submitted", ignoreCase = true) ||
            status.equals("Done", ignoreCase = true)
}

data class WeighingDraftUiRow(
    val id: String,
    val animalId: String = "",
    val label: String,
    val proofReady: Boolean,
    val readyToSubmit: Boolean,
    val syncedToBackend: Boolean = false,
)

@Composable
fun WeighingScreen(
    state: WeighingUiState,
    onScanInputChange: (String) -> Unit = {},
    onScanSubmit: () -> Unit = {},
    onWeightChange: (String) -> Unit = {},
    onAnimalCountChange: (String) -> Unit = {},
    onWeightEntryActive: (Boolean) -> Unit = {},
    onAnimalWeightChange: (String, String) -> Unit = { _, _ -> },
    onRecordAnimalWeight: (String, String) -> Unit = { _, _ -> },
    onRetryVideo: (String) -> Unit = {},
    onReuploadVideo: (String) -> Unit = {},
    onSelectAnimal: (String) -> Unit = {},
    onRecordIndividual: () -> Unit = {},
    onSubmitIndividualScope: () -> Unit = {},
    onRecordShedPartition: () -> Unit = {},
    onCaptureShedVideo: () -> Unit = {},
    onRetryShedVideo: (String) -> Unit = {},
    onReplaceShedVideo: (String) -> Unit = {},
    onRemoveShedVideo: (String) -> Unit = {},
    onReconnectReader: () -> Unit = {},
    onOpenAssignment: (WeighingAssignmentUiRow) -> Unit = {},
    onCreateOrEditTask: () -> Unit = {},
    onTogglePlannerShed: (String) -> Unit = {},
    onPlannerShedCategory: (String, String) -> Unit = { _, _ -> },
    onRefresh: () -> Unit = {},
    onBack: () -> Unit = {},
    modifier: Modifier = Modifier,
) {
    var rosterSheetOpen by remember { mutableStateOf(false) }
    if (!state.hasScope) {
        RefreshOnResume(onRefresh = onRefresh)
    }
    if (rosterSheetOpen) {
        WeighingRosterSheet(
            title = state.title.ifBlank { "Weighing rows" },
            rows = state.visibleRows,
            totalExpected = state.totalExpected,
            onDismiss = { rosterSheetOpen = false },
        )
    }
    if (state.hasScope) {
        WeighingExecutionScanScreen(
            state = state,
            onScanInputChange = onScanInputChange,
                onScanSubmit = onScanSubmit,
                onWeightChange = onWeightChange,
                onAnimalCountChange = onAnimalCountChange,
                onWeightEntryActive = onWeightEntryActive,
                onAnimalWeightChange = onAnimalWeightChange,
                onRecordAnimalWeight = onRecordAnimalWeight,
                onRetryVideo = onRetryVideo,
                onReuploadVideo = onReuploadVideo,
                onSelectAnimal = onSelectAnimal,
            onRecordIndividual = onRecordIndividual,
            onSubmitIndividualScope = onSubmitIndividualScope,
            onRecordShedPartition = onRecordShedPartition,
            onCaptureShedVideo = onCaptureShedVideo,
            onRetryShedVideo = onRetryShedVideo,
            onReplaceShedVideo = onReplaceShedVideo,
            onRemoveShedVideo = onRemoveShedVideo,
            onReconnectReader = onReconnectReader,
            onRefresh = onRefresh,
            onBack = onBack,
            modifier = modifier,
        )
        return
    }
    Column(
        modifier = modifier.fillMaxSize(),
    ) {
        MeshaScreenHeader(
            title = if (state.plannerMode) "Plan" else "My work",
            eyebrow = "WEIGHING",
            eyebrowColor = MeshaColors.BrandD,
            onBack = onBack,
            actions = {
                SyncIconButton(
                    isSyncing = state.loading,
                    onSync = onRefresh,
                    contentDescription = stringResource(R.string.weighing_refresh),
                )
                MeshaIconButton(
                    icon = MeshaIcons.Bell,
                    contentDescription = stringResource(R.string.weighing_alerts),
                    onClick = {},
                )
            },
        )
        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .background(MeshaColors.PageBg)
                .padding(horizontal = 16.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            item {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    if (state.scopeLabel.isNotBlank()) {
                        Text(
                            text = state.scopeLabel,
                            style = MaterialTheme.typography.bodyMedium,
                            color = MeshaColors.Muted,
                            maxLines = 2,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                    if (state.plannerMode && !state.hasScope) {
                        PlannerRootContent(
                            state = state,
                            onCreateOrEditTask = onCreateOrEditTask,
                            onTogglePlannerShed = onTogglePlannerShed,
                            onPlannerShedCategory = onPlannerShedCategory,
                        )
                    } else if (state.hasScope) {
                        WeighingCapturePanel(
                            state = state,
                            onScanInputChange = onScanInputChange,
                            onScanSubmit = onScanSubmit,
                            onWeightChange = onWeightChange,
                            onAnimalWeightChange = onAnimalWeightChange,
                            onRecordAnimalWeight = onRecordAnimalWeight,
                            onSelectAnimal = onSelectAnimal,
                            onRecordIndividual = onRecordIndividual,
                            onRecordShedPartition = onRecordShedPartition,
                            onCaptureShedVideo = onCaptureShedVideo,
                            onOpenRoster = { rosterSheetOpen = true },
                        )
                    } else {
                        if (state.loading && state.assignments.isEmpty()) {
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
            if (!state.plannerMode && !state.hasScope && state.assignments.isNotEmpty()) {
                item {
                    WeekPlanStrip(
                        weekLabel = state.plannerWeekLabel,
                        periodLabel = state.assignments.firstOrNull()?.periodLabel ?: state.plannerPeriodLabel,
                        tabs = state.plannerDayTabs,
                    )
                }
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

            if (state.hasScope && !state.isShedPartition && state.visibleRows.isNotEmpty()) {
                item { SectionTitle("Recent row updates") }
                items(state.visibleRows.take(3), key = { it.id }) { row ->
                    RosterRow(row)
                }
            }
        }
    }
}

@Composable
private fun PlannerRootContent(
    state: WeighingUiState,
    onCreateOrEditTask: () -> Unit,
    onTogglePlannerShed: (String) -> Unit,
    onPlannerShedCategory: (String, String) -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        WeekPlanStrip(
            weekLabel = state.plannerWeekLabel,
            periodLabel = state.plannerPeriodLabel,
            tabs = state.plannerDayTabs,
        )
        if (state.loading) {
            LoadingWorkSkeleton()
            return
        }
        if (state.plannerParks.isEmpty()) {
            EmptyWorkCard(
                title = "No kid sheds to plan",
                body = "Weekly kid weighing opens here when park and shed roster truth is available.",
            )
            return
        }
        val primaryPark = state.plannerParks.first()
        PlannerParkCard(
            park = primaryPark,
            operator = state.plannerOperators.firstOrNull(),
            busy = state.actionInFlight,
            onCreateOrEditTask = onCreateOrEditTask,
        )
        PlannerLaneCard()
        SectionTitle("SHEDS & CATEGORY")
        primaryPark.sheds.take(6).forEach { shed ->
            PlannerShedCard(
                shed = shed,
                selected = shed.selected,
                onToggle = { onTogglePlannerShed(shed.locationId) },
                onCategory = { category -> onPlannerShedCategory(shed.locationId, category) },
            )
        }
        PlannerSummaryCard(
            park = primaryPark,
            operator = state.plannerOperators.firstOrNull(),
        )
    }
}

@Composable
private fun WeekPlanStrip(
    weekLabel: String,
    periodLabel: String,
    tabs: List<WeighingDayTabUiRow>,
) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            SectionTitle("WEEK PLAN")
            Text(
                text = periodLabel.ifBlank { weekLabel.ifBlank { "THIS WEEK" } },
                color = MeshaColors.Muted,
                style = MeshaType.bodyStrong,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(7.dp),
        ) {
            tabs.take(7).forEach { tab ->
                Column(
                    modifier = Modifier
                        .weight(1f)
                        .height(74.dp)
                        .clip(RoundedCornerShape(14.dp))
                        .background(if (tab.selected) MeshaColors.Brand else MeshaColors.Surf)
                        .border(1.dp, if (tab.selected) MeshaColors.Brand else MeshaColors.Hair, RoundedCornerShape(14.dp))
                        .padding(vertical = 10.dp),
                    horizontalAlignment = Alignment.CenterHorizontally,
                    verticalArrangement = Arrangement.SpaceBetween,
                ) {
                    Text(
                        text = tab.dayLabel,
                        color = if (tab.selected) MeshaColors.PageBg else MeshaColors.Muted,
                        fontSize = 10.sp,
                        fontWeight = FontWeight.W900,
                    )
                    Text(
                        text = tab.dateLabel,
                        color = if (tab.selected) MeshaColors.PageBg else MeshaColors.Ink,
                        fontSize = 20.sp,
                        fontWeight = FontWeight.W900,
                    )
                }
            }
        }
    }
}

@Composable
private fun PlannerLaneCard() {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Brand, RoundedCornerShape(16.dp))
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text("STEP 1 · LANE", color = MeshaColors.BrandD, style = MeshaType.sectionLabel)
        Text("Weekly · Kids", color = MeshaColors.Ink, style = MeshaType.cardTitle)
        Text(
            "Kids weighing runs weekly. Adults stay out of v1, and each selected shed gets its own category below.",
            color = MeshaColors.Muted,
            style = MeshaType.cardSubtitle,
        )
    }
}

@Composable
private fun PlannerParkCard(
    park: WeighingPlannerParkUiRow,
    operator: WeighingPlannerOperatorUiRow?,
    busy: Boolean,
    onCreateOrEditTask: () -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(
                1.dp,
                if (park.hasExistingTask) MeshaColors.Warn else MeshaColors.Hair,
                RoundedCornerShape(18.dp),
            )
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = if (park.hasExistingTask) "task already exists" else "weekly kids",
                color = if (park.hasExistingTask) MeshaColors.Warn else MeshaColors.BrandD,
                fontSize = 12.sp,
                fontWeight = FontWeight.W900,
                modifier = Modifier
                    .clip(RoundedCornerShape(9.dp))
                    .background(if (park.hasExistingTask) MeshaColors.WarnX else MeshaColors.BrandTint)
                    .padding(horizontal = 10.dp, vertical = 6.dp),
            )
            Box(Modifier.weight(1f))
            Text(
                text = "${park.kidCount}",
                color = MeshaColors.Ink,
                style = MeshaType.cardTitle,
            )
        }
        Text(
            text = park.name,
            color = MeshaColors.Ink,
            style = MeshaType.cardTitle,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        Text(
            text = if (park.hasExistingTask) {
                "${park.existingCampaignStatus} - ${park.existingCampaignShedCount} sheds. Creating another task for this park/week is blocked."
            } else {
                "${park.sheds.size} kid sheds - assign ${operator?.displayName ?: "operator"}."
            },
            color = MeshaColors.Muted,
            style = MeshaType.cardSubtitle,
        )
        if (park.hasExistingTask) {
            ExistingTaskSummary(park)
        }
        Text(
            text = if (park.hasExistingTask) "Edit existing task" else "New task",
            color = if (busy) MeshaColors.Faint else MeshaColors.BrandD,
            style = MeshaType.cta,
            modifier = Modifier
                .fillMaxWidth()
                .minimumInteractiveComponentSize()
                .clip(RoundedCornerShape(999.dp))
                .background(if (busy) MeshaColors.Surf3 else MeshaColors.BrandTint)
                .clickable(
                    enabled = !busy,
                    interactionSource = remember { MutableInteractionSource() },
                    indication = null,
                    onClick = onCreateOrEditTask,
                )
                .padding(horizontal = 14.dp, vertical = 12.dp),
        )
    }
}

@Composable
private fun ExistingTaskSummary(park: WeighingPlannerParkUiRow) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.WarnX)
            .border(1.dp, MeshaColors.Warn, RoundedCornerShape(12.dp)),
    ) {
        PlannerSummaryRow("Existing task", "${park.existingCampaignShedCount} sheds")
        PlannerSummaryRow("Status", park.existingCampaignStatus ?: "Open")
    }
}

@Composable
private fun PlannerShedCard(
    shed: WeighingPlannerShedUiRow,
    selected: Boolean,
    onToggle: () -> Unit,
    onCategory: (String) -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, if (selected) MeshaColors.Brand else MeshaColors.Hair, RoundedCornerShape(16.dp))
            .clickable(onClick = onToggle)
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(9.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(
                modifier = Modifier
                    .size(30.dp)
                    .clip(RoundedCornerShape(9.dp))
                    .background(if (selected) MeshaColors.BrandTint else MeshaColors.Surf3),
                contentAlignment = Alignment.Center,
            ) {
                if (selected) {
                    Icon(
                        imageVector = MeshaIcons.CheckCircle,
                        contentDescription = null,
                        tint = MeshaColors.BrandD,
                        modifier = Modifier.size(17.dp),
                    )
                }
            }
            Spacer(Modifier.width(10.dp))
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = shed.name,
                    color = MeshaColors.Ink,
                    style = MeshaType.bodyStrong,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    text = "kid shed",
                    color = MeshaColors.Muted,
                    style = MeshaType.cardSubtitle,
                )
            }
            Text(
                text = "${shed.kidCount}",
                color = MeshaColors.BrandD,
                style = MeshaType.bodyStrong,
            )
        }
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(12.dp))
                .background(MeshaColors.Bg)
                .border(1.dp, MeshaColors.Hair, RoundedCornerShape(12.dp))
                .padding(4.dp),
        ) {
            PlannerCategorySegment(
                label = "Individual",
                active = shed.category == "individual_animal",
                enabled = selected,
                onClick = { onCategory("individual_animal") },
                modifier = Modifier.weight(1f),
            )
            PlannerCategorySegment(
                label = "Lumpsum",
                active = shed.category != "individual_animal",
                enabled = selected,
                onClick = { onCategory("per_shed_partition") },
                modifier = Modifier.weight(1f),
            )
        }
    }
}

@Composable
private fun PlannerCategorySegment(
    label: String,
    active: Boolean,
    enabled: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Text(
        text = label,
        color = when {
            !enabled -> MeshaColors.Faint
            active && label == "Individual" -> MeshaColors.Info
            active -> MeshaColors.Purple
            else -> MeshaColors.Muted
        },
        fontSize = 12.sp,
        fontWeight = FontWeight.W900,
        modifier = modifier
            .minimumInteractiveComponentSize()
            .clip(RoundedCornerShape(9.dp))
            .background(if (active) MeshaColors.Surf3 else Color.Transparent)
            .clickable(enabled = enabled, onClick = onClick)
            .padding(horizontal = 8.dp, vertical = 8.dp),
    )
}

@Composable
private fun PlannerSummaryCard(
    park: WeighingPlannerParkUiRow,
    operator: WeighingPlannerOperatorUiRow?,
) {
    val selected = park.sheds.filter { it.selected }
    val individual = selected.filter { it.category == "individual_animal" }
    val lumpsum = selected.filter { it.category != "individual_animal" }
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp)),
    ) {
        PlannerSummaryRow("Operator", operator?.displayName ?: "Amit Kumar")
        PlannerSummaryRow("Selected", "${selected.size} sheds - ${selected.sumOf { it.kidCount }} kids")
        PlannerSummaryRow("Individual", "${individual.size} sheds - ${individual.sumOf { it.kidCount }} kids")
        PlannerSummaryRow("Lumpsum", "${lumpsum.size} sheds - ${lumpsum.sumOf { it.kidCount }} in scope")
    }
}

@Composable
private fun PlannerSummaryRow(label: String, value: String) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 14.dp, vertical = 13.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(label, color = MeshaColors.Muted, style = MeshaType.cardSubtitle)
        Text(
            text = value,
            color = MeshaColors.Ink,
            style = MeshaType.bodyStrong,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
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
            }
            Text(
                text = row.expectedLocationLabel.ifBlank { row.label },
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
            if (!row.isClosed) {
                Text(
                    text = assignmentAction(),
                    color = MeshaColors.BrandD,
                    style = MeshaType.cta,
                    modifier = Modifier
                        .clickable(onClick = onOpen)
                        .padding(top = 2.dp),
                )
            }
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
        text = status.ifBlank { "Not started" },
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
    "Free-flow RFID, weight, and video capture"

@Composable
private fun assignmentAction(): String =
    stringResource(R.string.weighing_action_scan_animals)

@Composable
private fun weighingCategoryLabel(category: String): String =
    if (category == "per_shed_partition") {
        stringResource(R.string.weighing_category_lumpsum)
    } else {
        stringResource(R.string.weighing_category_individual)
    }

@Composable
private fun EmptyWorkCard(title: String, body: String) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(18.dp))
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(
                modifier = Modifier
                    .size(38.dp)
                    .clip(RoundedCornerShape(12.dp))
                    .background(MeshaColors.BrandTint),
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    imageVector = MeshaIcons.Module,
                    contentDescription = null,
                    tint = MeshaColors.Brand,
                    modifier = Modifier.size(19.dp),
                )
            }
            Spacer(Modifier.width(12.dp))
            Text(title, color = MeshaColors.Ink, style = MeshaType.cardTitle)
        }
        Text(
            text = body,
            color = MeshaColors.Muted,
            style = MeshaType.cardSubtitle,
        )
        if (body.contains("published", ignoreCase = true)) {
            Text(
                text = "Pull to refresh after leadership publishes the weekly plan.",
                color = MeshaColors.BrandD,
                style = MeshaType.cta,
            )
        }
    }
}

@Composable
private fun LoadingWorkSkeleton() {
    LoadingSkeletonList(
        rows = 2,
        contentPadding = PaddingValues(0.dp),
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
    onAnimalWeightChange: (String, String) -> Unit,
    onRecordAnimalWeight: (String, String) -> Unit,
    onSelectAnimal: (String) -> Unit,
    onRecordIndividual: () -> Unit,
    onRecordShedPartition: () -> Unit,
    onCaptureShedVideo: () -> Unit,
    onOpenRoster: () -> Unit,
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
                    "Keep scanning RFID tags. Each captured animal stays on this page."
                } else {
                    "Add weight and video proof for this animal."
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
            actionLabel = if (state.isShedPartition) "Record shed" else "Add row",
            actionEnabled = if (state.isShedPartition) state.canRecordShedPartition else state.canRecordIndividual,
            onAction = if (state.isShedPartition) onRecordShedPartition else onRecordIndividual,
        )
    }
}

@Composable
private fun RosterPeekCard(
    total: Int,
    visible: Int,
    wrongShed: Int,
    onOpenRoster: () -> Unit,
) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(14.dp))
            .clickable(onClick = onOpenRoster)
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
                tint = MeshaColors.BrandD,
                modifier = Modifier.size(18.dp),
            )
        }
        Spacer(Modifier.width(11.dp))
        Column(modifier = Modifier.weight(1f)) {
            Text("View rows", color = MeshaColors.Ink, style = MeshaType.bodyStrong)
            Text(
                text = rosterPeekSummary(total = total, visible = visible, wrongShed = wrongShed),
                color = MeshaColors.Muted,
                style = MeshaType.cardSubtitle,
                modifier = Modifier.padding(top = 3.dp),
            )
        }
        Text(
            text = "Open",
            color = MeshaColors.BrandD,
            style = MeshaType.cta,
        )
    }
}

private fun rosterPeekSummary(total: Int, visible: Int, wrongShed: Int): String {
    val base = if (total > 0) "$visible visible of $total kids" else "$visible visible rows"
    return if (wrongShed > 0) "$base - $wrongShed wrong shed" else base
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun WeighingRosterSheet(
    title: String,
    rows: List<WeighingRosterUiRow>,
    totalExpected: Int,
    onDismiss: () -> Unit,
) {
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = sheetState,
        containerColor = MeshaColors.Surf,
        contentColor = MeshaColors.Ink,
        shape = RoundedCornerShape(topStart = 26.dp, topEnd = 26.dp),
        dragHandle = { SheetGrip() },
    ) {
        WeighingRosterSheetContent(
            title = title,
            rows = rows,
            totalExpected = totalExpected,
        )
    }
}

@Composable
private fun SheetGrip() {
    Box(
        modifier = Modifier
            .padding(top = 10.dp, bottom = 6.dp)
            .fillMaxWidth(),
        contentAlignment = Alignment.Center,
    ) {
        Box(
            modifier = Modifier
                .width(38.dp)
                .height(4.dp)
                .clip(RoundedCornerShape(999.dp))
                .background(MeshaColors.Surf3),
        )
    }
}

@Composable
private fun WeighingRosterSheetContent(
    title: String,
    rows: List<WeighingRosterUiRow>,
    totalExpected: Int,
) {
    var query by remember { mutableStateOf("") }
    val filtered = remember(query, rows) {
        if (query.isBlank()) {
            rows
        } else {
            rows.filter { row ->
                row.displayAnimalId.contains(query, ignoreCase = true) ||
                    row.expectedLocationLabel.contains(query, ignoreCase = true) ||
                    row.actualLocationLabel.orEmpty().contains(query, ignoreCase = true)
            }
        }
    }
    Surface(color = MeshaColors.Surf, modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(bottom = 22.dp)) {
            Column(Modifier.padding(horizontal = 20.dp)) {
                Text(title, color = MeshaColors.Ink, fontSize = 16.sp, fontWeight = FontWeight.W700)
                Text(
                    text = rosterSheetSubtitle(filtered.size, totalExpected),
                    color = MeshaColors.Muted,
                    fontSize = 12.sp,
                    modifier = Modifier.padding(top = 2.dp),
                )
            }
            Spacer(Modifier.height(10.dp))
            OutlinedTextField(
                value = query,
                onValueChange = { query = it },
                singleLine = true,
                placeholder = {
                    Text("Search tag or shed", color = MeshaColors.Faint, style = MeshaType.cardSubtitle)
                },
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 16.dp),
            )
            Spacer(Modifier.height(6.dp))
            if (filtered.isEmpty()) {
                EmptyWorkCard(
                    title = "No matching rows",
                    body = "Try another tag or shed name.",
                )
            } else {
                LazyColumn(
                    modifier = Modifier
                        .fillMaxWidth()
                        .heightIn(max = 520.dp),
                ) {
                    items(filtered, key = { it.id }, contentType = { "weighing_roster_row" }) { row ->
                        RosterRow(row)
                    }
                }
            }
        }
    }
}

private fun rosterSheetSubtitle(visibleCount: Int, totalExpected: Int): String =
    if (totalExpected > 0) "$visibleCount rows visible of $totalExpected kids" else "$visibleCount rows visible"

@Composable
private fun WeighingExecutionScanScreen(
    state: WeighingUiState,
    onScanInputChange: (String) -> Unit,
    onScanSubmit: () -> Unit,
    onWeightChange: (String) -> Unit,
    onAnimalCountChange: (String) -> Unit,
    onWeightEntryActive: (Boolean) -> Unit,
    onAnimalWeightChange: (String, String) -> Unit,
    onRecordAnimalWeight: (String, String) -> Unit,
    onRetryVideo: (String) -> Unit,
    onReuploadVideo: (String) -> Unit,
    onSelectAnimal: (String) -> Unit,
    onRecordIndividual: () -> Unit,
    onSubmitIndividualScope: () -> Unit,
    onRecordShedPartition: () -> Unit,
    onCaptureShedVideo: () -> Unit,
    onRetryShedVideo: (String) -> Unit,
    onReplaceShedVideo: (String) -> Unit,
    onRemoveShedVideo: (String) -> Unit,
    onReconnectReader: () -> Unit,
    onRefresh: () -> Unit,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Scaffold(
        modifier = modifier.fillMaxSize(),
        containerColor = MeshaColors.PageBg,
        topBar = {
            MeshaScreenHeader(
                title = state.title.ifBlank { "Weighing" },
                eyebrow = "WEIGHING",
                subtitle = state.scopeLabel,
                onBack = onBack,
                actions = {
                    MeshaIconButton(
                        icon = MeshaIcons.Refresh,
                        contentDescription = stringResource(R.string.weighing_refresh),
                        onClick = onRefresh,
                    )
                },
            )
        },
        bottomBar = {
            ActionButton(
                text = if (state.isShedPartition) "Submit lump-sum weighing" else "Submit",
                enabled = if (state.isShedPartition) state.canRecordShedPartition else state.individualSubmitReady,
                onClick = if (state.isShedPartition) onRecordShedPartition else onSubmitIndividualScope,
                modifier = Modifier
                    .fillMaxWidth()
                    .background(MeshaColors.PageBg)
                    .padding(horizontal = 16.dp, vertical = 12.dp),
                primary = true,
            )
        },
    ) { padding ->
        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding),
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            if (!state.isShedPartition) {
                item {
                    WeighingReaderBanner(
                        reader = state.readerConnection,
                        onReconnect = onReconnectReader,
                    )
                }
                state.message?.takeIf { it.startsWith("Already scanned") }?.let { message ->
                    item { WeighingDuplicateNotice(message) }
                }
                item {
                    Text(
                        text = "${state.visibleRows.size} ${if (state.visibleRows.size == 1) "animal" else "animals"} captured",
                        color = MeshaColors.Ink,
                        style = MeshaType.bodyStrong,
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
                if (state.visibleRows.isEmpty()) {
                    item {
                        Text(
                            text = "Scan an RFID tag to begin",
                            color = MeshaColors.Muted,
                            style = MeshaType.bodyStrong,
                            textAlign = TextAlign.Center,
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(vertical = 28.dp),
                        )
                    }
                } else {
                    items(
                        items = state.visibleRows,
                        key = { row -> row.animalId },
                    ) { row ->
                        WeighingFreeFlowFeedRow(
                            row = row,
                            updating = row.weightUpdating,
                            onWeightChange = { onAnimalWeightChange(row.animalId, it) },
                            onWeightEntryActive = onWeightEntryActive,
                            onSaveWeight = { weight ->
                                onRecordAnimalWeight(row.animalId, weight)
                            },
                            onRetryVideo = { onRetryVideo(row.animalId) },
                            onReuploadVideo = { onReuploadVideo(row.animalId) },
                        )
                    }
                }
            } else {
                item {
                    WeighingLumpSumCapture(
                        state = state,
                        onWeightChange = onWeightChange,
                        onAnimalCountChange = onAnimalCountChange,
                        onWeightEntryActive = onWeightEntryActive,
                        onCaptureShedVideo = onCaptureShedVideo,
                        onRetryShedVideo = onRetryShedVideo,
                        onReplaceShedVideo = onReplaceShedVideo,
                        onRemoveShedVideo = onRemoveShedVideo,
                    )
                }
            }
        }
    }
}

@Composable
private fun WeighingReaderBanner(
    reader: ScanReaderConnection?,
    onReconnect: () -> Unit,
) {
    val connected = reader?.connected == true
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(8.dp))
            .background(if (connected) MeshaColors.OkX else MeshaColors.DangerX)
            .border(
                1.dp,
                if (connected) MeshaColors.Ok else MeshaColors.Danger,
                RoundedCornerShape(8.dp),
            )
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(
            imageVector = MeshaIcons.Bluetooth,
            contentDescription = null,
            tint = if (connected) MeshaColors.Ok else MeshaColors.Danger,
            modifier = Modifier.size(20.dp),
        )
        Spacer(Modifier.width(10.dp))
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = reader?.readerName ?: "RFID reader",
                color = MeshaColors.Ink,
                style = MeshaType.bodyStrong,
            )
            Text(
                text = reader?.statusLabel ?: "Checking connection",
                color = MeshaColors.Muted,
                style = MeshaType.caption,
            )
        }
        if (!connected) {
            ActionButton(
                text = reader?.actionLabel ?: "Reconnect",
                enabled = true,
                onClick = onReconnect,
                primary = false,
            )
        }
    }
}

@Composable
private fun WeighingDuplicateNotice(message: String) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(8.dp))
            .background(MeshaColors.WarnX)
            .border(1.dp, MeshaColors.Warn, RoundedCornerShape(8.dp))
            .padding(horizontal = 12.dp, vertical = 10.dp),
    ) {
        Icon(
            imageVector = MeshaIcons.Warn,
            contentDescription = null,
            tint = MeshaColors.Warn,
            modifier = Modifier.size(20.dp),
        )
        Spacer(Modifier.width(10.dp))
        Text(
            text = message,
            color = MeshaColors.Warn,
            style = MeshaType.bodyStrong,
            modifier = Modifier.weight(1f),
            maxLines = 2,
        )
    }
}

@Composable
private fun WeighingFreeFlowFeedRow(
    row: WeighingRosterUiRow,
    updating: Boolean,
    onWeightChange: (String) -> Unit,
    onWeightEntryActive: (Boolean) -> Unit,
    onSaveWeight: (String) -> Unit,
    onRetryVideo: () -> Unit,
    onReuploadVideo: () -> Unit,
) {
    val focusManager = LocalFocusManager.current
    var editingWeight by remember(row.animalId, row.weightSaved) { mutableStateOf(!row.weightSaved) }
    var draftWeight by remember(row.animalId) { mutableStateOf(row.weightInput) }
    LaunchedEffect(row.animalId, row.weightInput, row.weightSaved) {
        if (!editingWeight || row.weightSaved) {
            draftWeight = row.weightInput
        }
    }
    LaunchedEffect(row.savedWeightLabel) {
        if (row.weightSaved) editingWeight = false
    }
    val canSaveDraftWeight = draftWeight.toDoubleOrNull()?.let { it > 0.0 } == true
    val complete = row.weightSaved &&
        row.proofUploadStatus == ProofUploadStatus.SYNCED &&
        row.backendSynced
    val tone = when {
        complete -> MeshaColors.Ok
        row.proofUploadStatus == ProofUploadStatus.FAILED -> MeshaColors.Danger
        else -> MeshaColors.Hair
    }
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(8.dp))
            .background(if (complete) MeshaColors.OkX else MeshaColors.Surf)
            .border(1.dp, tone, RoundedCornerShape(8.dp))
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = row.displayAnimalId,
                color = MeshaColors.Ink,
                style = MeshaType.bodyStrong,
                modifier = Modifier.weight(1f),
            )
            if (complete) {
                Icon(
                    imageVector = MeshaIcons.CheckCircle,
                    contentDescription = null,
                    tint = MeshaColors.Ok,
                    modifier = Modifier.size(20.dp),
                )
            }
        }
        Text(
            text = row.scannedAtLabel.orEmpty(),
            color = MeshaColors.Muted,
            style = MeshaType.caption,
        )
        Text(
            text = when {
                complete -> "Weight saved · ${row.proofStatusLabel ?: "video synced"}"
                row.proofUploadStatus == ProofUploadStatus.FAILED -> row.proofStatusLabel ?: "Video upload failed"
                row.proofUploadStatus == ProofUploadStatus.UPLOADING -> row.proofStatusLabel ?: "Video syncing"
                row.proofUploadStatus == ProofUploadStatus.SYNCED && !row.backendSynced ->
                    "${row.proofStatusLabel ?: "Video synced"} · weight waiting to sync"
                row.proofUploadStatus == ProofUploadStatus.SYNCED -> row.proofStatusLabel ?: "Video synced"
                else -> "Video captured"
            },
            color = when {
                complete -> MeshaColors.Ok
                row.proofUploadStatus == ProofUploadStatus.FAILED -> MeshaColors.Danger
                else -> MeshaColors.Muted
            },
            style = MeshaType.caption,
        )
        if (row.weightSaved && !editingWeight) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text(
                    text = "Weight ${row.savedWeightLabel ?: row.weightInput} kg",
                    color = MeshaColors.Ink,
                    style = MeshaType.bodyStrong,
                    modifier = Modifier.weight(1f),
                )
                ActionButton(
                    text = if (updating) "Updating..." else "Edit weight",
                    enabled = !updating,
                    onClick = {
                        editingWeight = true
                        draftWeight = row.weightInput
                        onWeightChange(draftWeight)
                    },
                    primary = false,
                )
            }
        } else {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                OutlinedTextField(
                    value = draftWeight,
                    onValueChange = {
                        draftWeight = it
                        onWeightChange(it)
                    },
                    label = { Text("Weight") },
                    suffix = { Text("kg") },
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                    singleLine = true,
                    enabled = !updating,
                    modifier = Modifier
                        .weight(1f)
                        .onFocusChanged { onWeightEntryActive(it.isFocused) },
                )
                ActionButton(
                    text = if (updating) "Updating..." else if (row.weightSaved) "Update" else "Save",
                    enabled = canSaveDraftWeight && !updating,
                    onClick = {
                        focusManager.clearFocus()
                        onSaveWeight(draftWeight)
                    },
                    primary = true,
                )
            }
        }
        if (row.reuploadRequested) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(8.dp))
                    .background(MeshaColors.InfoX)
                    .border(1.dp, MeshaColors.Info, RoundedCornerShape(8.dp))
                    .padding(horizontal = 12.dp, vertical = 10.dp),
            ) {
                Icon(
                    imageVector = MeshaIcons.Refresh,
                    contentDescription = null,
                    tint = MeshaColors.Info,
                    modifier = Modifier.size(18.dp),
                )
                Spacer(Modifier.width(8.dp))
                Text(
                    text = "Scan this RFID tag again to replace the video.",
                    color = MeshaColors.Info,
                    style = MeshaType.caption,
                    modifier = Modifier.weight(1f),
                )
            }
        }
        when (row.proofUploadStatus) {
            ProofUploadStatus.FAILED -> ActionButton(
                text = "Retry",
                enabled = true,
                onClick = onRetryVideo,
                modifier = Modifier.fillMaxWidth(),
                primary = false,
            )
            ProofUploadStatus.SYNCED -> ActionButton(
                text = if (row.reuploadRequested) "Waiting for RFID scan" else "Re-upload",
                enabled = !row.reuploadRequested,
                onClick = onReuploadVideo,
                modifier = Modifier.fillMaxWidth(),
                primary = false,
            )
            else -> Unit
        }
    }
}

@Composable
private fun WeighingLumpSumCapture(
    state: WeighingUiState,
    onWeightChange: (String) -> Unit,
    onAnimalCountChange: (String) -> Unit,
    onWeightEntryActive: (Boolean) -> Unit,
    onCaptureShedVideo: () -> Unit,
    onRetryShedVideo: (String) -> Unit,
    onReplaceShedVideo: (String) -> Unit,
    onRemoveShedVideo: (String) -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(8.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(8.dp))
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        OutlinedTextField(
            value = state.weightInput,
            onValueChange = onWeightChange,
            label = { Text("Total weight") },
            suffix = { Text("kg") },
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
            singleLine = true,
            modifier = Modifier
                .fillMaxWidth()
                .onFocusChanged { onWeightEntryActive(it.isFocused) },
        )
        OutlinedTextField(
            value = state.animalCountInput,
            onValueChange = onAnimalCountChange,
            label = { Text("Animal count") },
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
            singleLine = true,
            modifier = Modifier
                .fillMaxWidth()
                .onFocusChanged { onWeightEntryActive(it.isFocused) },
        )
        val totalWeight = state.weightInput.toDoubleOrNull()
        val animalCount = state.animalCountInput.toIntOrNull()
        if (totalWeight != null && totalWeight > 0 && animalCount != null && animalCount > 0) {
            Text(
                text = "Average ${"%.2f".format(totalWeight / animalCount)} kg per animal",
                color = MeshaColors.Ok,
                style = MeshaType.bodyStrong,
            )
        }
        Text(
            text = "${state.shedProofs.size} / 5 videos uploaded",
            color = if (state.shedProofs.size >= 5) MeshaColors.Warn else MeshaColors.Muted,
            style = MeshaType.bodyStrong,
            modifier = Modifier.fillMaxWidth(),
        )
        state.shedProofs.forEachIndexed { index, proof ->
            val proofColor = when (proof.status) {
                ProofUploadStatus.SYNCED -> MeshaColors.Ok
                ProofUploadStatus.FAILED -> MeshaColors.Danger
                else -> MeshaColors.Muted
            }
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(8.dp))
                    .background(MeshaColors.PageBg)
                    .padding(horizontal = 12.dp, vertical = 10.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                Icon(
                    imageVector = if (proof.status == ProofUploadStatus.SYNCED) {
                        MeshaIcons.CheckCircle
                    } else {
                        MeshaIcons.Video
                    },
                    contentDescription = null,
                    tint = proofColor,
                    modifier = Modifier.size(20.dp),
                )
                Text(
                    text = "Video ${index + 1}",
                    color = MeshaColors.Ink,
                    style = MeshaType.bodyStrong,
                    modifier = Modifier.weight(1f),
                )
                Text(
                    text = proof.label,
                    color = proofColor,
                    style = MeshaType.caption,
                )
            }
            when (proof.status) {
                ProofUploadStatus.SYNCED -> Row(
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                    modifier = Modifier.fillMaxWidth(),
                ) {
                    ShedVideoAction(
                        text = "Replace",
                        icon = MeshaIcons.Refresh,
                        enabled = !state.actionInFlight,
                        onClick = { onReplaceShedVideo(proof.id) },
                        modifier = Modifier.weight(1f),
                    )
                    ShedVideoAction(
                        text = "Remove",
                        icon = MeshaIcons.Close,
                        enabled = !state.actionInFlight,
                        onClick = { onRemoveShedVideo(proof.id) },
                        modifier = Modifier.weight(1f),
                        danger = true,
                    )
                }
                ProofUploadStatus.FAILED -> ShedVideoAction(
                    text = "Retry",
                    icon = MeshaIcons.Refresh,
                    enabled = !state.actionInFlight,
                    onClick = { onRetryShedVideo(proof.id) },
                    modifier = Modifier.fillMaxWidth(),
                    danger = true,
                )
                ProofUploadStatus.MISSING,
                ProofUploadStatus.UPLOADING -> Unit
            }
        }
        ActionButton(
            text = if (state.shedProofs.isEmpty()) "Capture group video" else "Add another video",
            enabled = !state.actionInFlight && state.shedProofs.size < 5,
            onClick = onCaptureShedVideo,
            modifier = Modifier.fillMaxWidth(),
            primary = false,
        )
    }
}

@Composable
private fun ShedVideoAction(
    text: String,
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    enabled: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    danger: Boolean = false,
) {
    val color = when {
        !enabled -> MeshaColors.Faint
        danger -> MeshaColors.Danger
        else -> MeshaColors.Info
    }
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.Center,
        modifier = modifier
            .minimumInteractiveComponentSize()
            .clip(RoundedCornerShape(12.dp))
            .background(if (danger) MeshaColors.DangerX else MeshaColors.InfoX)
            .border(1.dp, color, RoundedCornerShape(12.dp))
            .clickable(enabled = enabled, onClick = onClick)
            .padding(horizontal = 10.dp, vertical = 9.dp),
    ) {
        Icon(
            imageVector = icon,
            contentDescription = null,
            tint = color,
            modifier = Modifier.size(15.dp),
        )
        Spacer(Modifier.width(6.dp))
        Text(text = text, color = color, style = MeshaType.caption, fontWeight = FontWeight.Bold)
    }
}

private fun WeighingRosterUiRow.weighingScanLabel(draft: WeighingDraftUiRow? = null): String = buildString {
    when {
        availabilityStatus.equals("unavailable", ignoreCase = true) -> append("Unavailable")
        draft != null -> append(draft.weightLabel("Weight saved"))
        status.equals("Weighed", ignoreCase = true) -> append("Weighed")
        else -> append(status)
    }
    if (draft != null) {
        append(if (draft.proofReady) " · video ready" else " · video required")
    }
    actualLocationLabel?.takeIf { wrongShed && it.isNotBlank() && it != expectedLocationLabel }?.let {
        append(" · ").append(it)
    }
}

private fun WeighingDraftUiRow.weightLabel(fallback: String): String =
    label.substringAfter(" - ", missingDelimiterValue = "")
        .takeIf { it.isNotBlank() }
        ?: fallback

private fun WeighingRosterUiRow.weighingDetails(draft: WeighingDraftUiRow?): List<String> =
    buildList {
        add("Expected shed: $expectedLocationLabel")
        actualLocationLabel
            ?.takeIf { it.isNotBlank() && it != expectedLocationLabel }
            ?.let { add("Current shed: $it") }
        draft?.label
            ?.substringAfter(" - ", missingDelimiterValue = "")
            ?.takeIf { it.isNotBlank() }
            ?.let { add("Weight: $it") }
        if (draft != null) {
            add(if (draft.proofReady) "Video proof: ready" else "Video proof: required")
        } else if (!availabilityStatus.equals("unavailable", ignoreCase = true)) {
            add("Weight and animal video pending")
        }
    }

private fun String.compactWeighingTag(): String =
    replace("\n", "")
        .replace(" ", "")
        .takeIf { it.length > 14 }
        ?.let { "${it.take(6)}…${it.takeLast(4)}" }
        ?: this

@Composable
private fun WeighingScanCaptureCard(
    state: WeighingUiState,
    onWeightChange: (String) -> Unit,
    onRecordIndividual: () -> Unit,
    onRecordShedPartition: () -> Unit,
) {
    val selectedLabel = when {
        state.isShedPartition -> state.scopeLabel.ifBlank { state.title.ifBlank { "Selected shed" } }
        !state.selectedAnimalLabel.isNullOrBlank() -> state.selectedAnimalLabel
        else -> null
    }
    val canSave = if (state.isShedPartition) state.canRecordShedPartition else state.canRecordIndividual
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .padding(bottom = 8.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(
                modifier = Modifier
                    .size(36.dp)
                    .clip(RoundedCornerShape(11.dp))
                    .background(if (state.isShedPartition) MeshaColors.PurpleX else MeshaColors.BrandTint),
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    text = "KG",
                    color = if (state.isShedPartition) MeshaColors.Purple else MeshaColors.Brand,
                    fontSize = 10.sp,
                    fontWeight = FontWeight.Black,
                )
            }
            Spacer(Modifier.width(11.dp))
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = if (state.isShedPartition) "Shed weight + proof" else "Animal weight + proof",
                    color = MeshaColors.Ink,
                    fontSize = 14.sp,
                    lineHeight = 17.sp,
                    fontWeight = FontWeight.Bold,
                )
                Text(
                    text = selectedLabel ?: "Tap Pending, then scan or select an animal.",
                    color = if (selectedLabel == null) MeshaColors.Faint else MeshaColors.Muted,
                    fontSize = 11.sp,
                    lineHeight = 15.sp,
                    fontWeight = FontWeight.Medium,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.padding(top = 3.dp),
                )
            }
        }
        OutlinedTextField(
            value = state.weightInput,
            onValueChange = onWeightChange,
            label = { Text(if (state.isShedPartition) "Total weight" else "Weight") },
            suffix = { Text("kg") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        Text(
            text = if (state.isShedPartition) {
                "One shed video is mandatory before this result can be submitted."
            } else {
                "One animal video is mandatory for every saved weight."
            },
            color = MeshaColors.Muted,
            fontSize = 11.sp,
            lineHeight = 15.sp,
            fontWeight = FontWeight.Medium,
        )
        ActionButton(
            text = if (state.isShedPartition) "Add shed camera clip" else "Add camera clip",
            enabled = canSave,
            onClick = if (state.isShedPartition) onRecordShedPartition else onRecordIndividual,
            modifier = Modifier.fillMaxWidth(),
            primary = true,
        )
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun WeighingWeightSheet(
    state: WeighingUiState,
    onWeightChange: (String) -> Unit,
    onRecordIndividual: () -> Unit,
    onRecordShedPartition: () -> Unit,
    onDismiss: () -> Unit,
) {
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        containerColor = MeshaColors.PageBg,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 18.dp, vertical = 14.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            SectionTitle(if (state.isShedPartition) "SHED RESULT" else "ANIMAL WEIGHT")
            Text(
                text = if (state.isShedPartition) {
                    "Record total weight and capture one shed video."
                } else {
                    state.selectedAnimalLabel ?: "Scan or select a pending animal first."
                },
                color = MeshaColors.Ink,
                style = MeshaType.cardTitle,
            )
            OutlinedTextField(
                value = state.weightInput,
                onValueChange = onWeightChange,
                label = { Text(if (state.isShedPartition) "Total weight" else "Weight") },
                suffix = { Text("kg") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            Text(
                text = if (state.isShedPartition) {
                    "Video proof opens after saving."
                } else {
                    "Per-animal video proof opens after saving."
                },
                color = MeshaColors.Muted,
                style = MeshaType.cardSubtitle,
            )
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                ActionButton(
                    text = "Cancel",
                    enabled = true,
                    onClick = onDismiss,
                    modifier = Modifier.weight(1f),
                    primary = false,
                )
                ActionButton(
                    text = if (state.isShedPartition) "Save shed" else "Save kid",
                    enabled = if (state.isShedPartition) state.canRecordShedPartition else state.canRecordIndividual,
                    onClick = if (state.isShedPartition) onRecordShedPartition else onRecordIndividual,
                    modifier = Modifier.weight(1f),
                    primary = true,
                )
            }
        }
    }
}

@Composable
private fun ActionButton(
    text: String,
    enabled: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    primary: Boolean,
) {
    Text(
        text = text,
        color = when {
            !enabled -> MeshaColors.Faint
            primary -> MeshaColors.OnBrand
            else -> MeshaColors.Muted
        },
        style = MeshaType.cta,
        textAlign = TextAlign.Center,
        modifier = modifier
            .minimumInteractiveComponentSize()
            .clip(RoundedCornerShape(14.dp))
            .background(
                when {
                    !enabled -> MeshaColors.Hair
                    primary -> MeshaColors.Brand
                    else -> MeshaColors.Surf
                },
            )
            .border(
                1.dp,
                when {
                    !enabled -> MeshaColors.Faint
                    primary -> MeshaColors.Brand
                    else -> MeshaColors.Hair
                },
                RoundedCornerShape(14.dp),
            )
            .clickable(enabled = enabled, onClick = onClick)
            .padding(horizontal = 12.dp, vertical = 13.dp),
    )
}

@Composable
private fun CaptureProgressTiles(state: WeighingUiState) {
    val done = if (state.isShedPartition) state.shedCompleted else state.individualResolved
    val total = if (state.isShedPartition) 1 else state.totalExpected
    val pending = (total - done).coerceAtLeast(0)
    val proofReady = state.individualDrafts.count { it.proofReady } + state.shedDrafts.count { it.proofReady }
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        CaptureMetricTile(
            value = done.toString(),
            label = if (state.isShedPartition) "Result" else "Done",
            selected = done > 0,
            tone = if (state.isShedPartition) MeshaColors.Purple else MeshaColors.BrandD,
            modifier = Modifier.weight(1f),
        )
        CaptureMetricTile(
            value = pending.toString(),
            label = "Pending",
            selected = pending == 0 && total > 0,
            tone = MeshaColors.Ink,
            modifier = Modifier.weight(1f),
        )
        CaptureMetricTile(
            value = proofReady.toString(),
            label = "Proof",
            selected = proofReady > 0,
            tone = MeshaColors.BrandD,
            modifier = Modifier.weight(1f),
        )
    }
}

@Composable
private fun CaptureMetricTile(
    value: String,
    label: String,
    selected: Boolean,
    tone: Color,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .height(64.dp)
            .clip(RoundedCornerShape(11.dp))
            .background(if (selected) MeshaColors.BrandTint else MeshaColors.Surf)
            .border(1.dp, if (selected) MeshaColors.Brand else MeshaColors.Hair, RoundedCornerShape(11.dp))
            .padding(horizontal = 6.dp, vertical = 8.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Text(
            text = value,
            color = tone,
            style = MeshaType.bodyStrong,
            maxLines = 1,
        )
        Spacer(Modifier.height(6.dp))
        Text(
            text = label.uppercase(),
            color = MeshaColors.Muted,
            style = MeshaType.overline,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
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
