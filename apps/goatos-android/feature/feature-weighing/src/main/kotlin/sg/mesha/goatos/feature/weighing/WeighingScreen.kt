package sg.mesha.goatos.feature.weighing

import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import android.content.Context
import android.view.inputmethod.InputMethodManager
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.relocation.BringIntoViewRequester
import androidx.compose.foundation.relocation.bringIntoViewRequester
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.AssistChip
import androidx.compose.material3.CircularProgressIndicator
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
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.remember
import androidx.compose.runtime.snapshotFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.first
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.LocalWindowInfo
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
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
import java.time.LocalDate
import java.time.format.DateTimeFormatter
import java.util.Locale
import sg.mesha.goatos.feature.scan.ScanUiState

/**
 * The MOST group videos a shed / partition result may carry. A ceiling, never a target.
 *
 * Weighing is free-flow: no number of videos is required, so this must never be rendered as the
 * denominator of a "N / 5" figure. It only gates the "Add another video" action and colours the
 * summary once the shed can hold no more.
 */
private const val SHED_PROOF_VIDEO_LIMIT = 5

// telemetry:exempt Weighing execution V1 has repository/viewmodel sync events; screen-level click telemetry is deferred until workflow names settle.

/**
 * What this shed's group videos ACTUALLY are, by upload state.
 *
 * Every part is a plain count of something that exists — uploaded, still uploading, failed. There
 * is no denominator: weighing is free-flow, no number of videos is required, and
 * [SHED_PROOF_VIDEO_LIMIT] is a ceiling rather than a target. "Uploaded" means the upload actually
 * landed, so a video still in flight is never counted as done.
 */
@Composable
private fun shedVideoSummary(proofs: List<WeighingProofUiRow>): String {
    if (proofs.isEmpty()) return stringResource(R.string.weighing_videos_none)
    val uploaded = proofs.count { it.status == ProofUploadStatus.SYNCED }
    val uploading = proofs.count { it.status == ProofUploadStatus.UPLOADING || it.status == ProofUploadStatus.MISSING }
    val failed = proofs.count { it.status == ProofUploadStatus.FAILED }
    val parts = buildList {
        if (uploaded > 0) add(stringResource(R.string.weighing_videos_uploaded_fmt, uploaded))
        if (uploading > 0) add(stringResource(R.string.weighing_videos_uploading_fmt, uploading))
        if (failed > 0) add(stringResource(R.string.weighing_videos_failed_fmt, failed))
        if (proofs.size >= SHED_PROOF_VIDEO_LIMIT) add(stringResource(R.string.weighing_videos_limit_reached))
    }
    return parts.joinToString(" · ")
}

data class WeighingUiState(
    val title: String = "Weighing",
    val scopeLabel: String = "",
    val hasScope: Boolean = false,
    // plannerMode still selects this screen's HEADER TITLE ("Plan" vs "My work") and suppresses
    // the operator work list on the planner surface. It is the only planner field this screen
    // reads: the week strip, the park card, the shed/category picker and the operator vocabulary
    // moved to the planner surface proper (WeighingTasksScreen + the create wizard), so their
    // state fields are gone rather than sitting here populated and unrendered.
    val plannerMode: Boolean = false,
    val parkFilters: List<WeighingParkFilterUiRow> = emptyList(),
    // Oversight authority as the BACKEND states it on the same read the rows came from. A screen
    // renders close/reopen only when the write behind them would be accepted -- these are
    // never inferred from which surface is on screen or from the viewer's role.
    val canEndWeighing: Boolean = false,
    val canReopenWeighing: Boolean = false,
    val assignments: List<WeighingAssignmentUiRow> = emptyList(),
    /**
     * The oversight surface's OPERATOR-grain rows, exactly as the backend counted them.
     *
     * Never derived from [assignments]: that is one keyset page, so a per-person total taken from
     * it would describe the scroll position rather than the person.
     */
    val operatorSummaries: List<WeighingOperatorUiRow> = emptyList(),
    val assignmentsLoadingMore: Boolean = false,
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
    val showSubmitConfirmation: Boolean = false,
    val hasLoadedOnce: Boolean = false,
) {
    val isShedPartition: Boolean get() = category.trim().equals("per_shed_partition", ignoreCase = true)
    val individualCompleted: Int get() = individualDrafts.count { it.readyToSubmit }
    // Weighing V1 treats the selected shed as an empty evidence bucket, not a roster.
    val individualSubmitReady: Boolean get() =
        visibleRows.isNotEmpty() &&
            visibleRows.all { row ->
                row.weightSaved &&
                    row.proofUploadStatus == ProofUploadStatus.SYNCED
        }
    val individualResolved: Int get() = maxOf(
        individualCompleted,
        visibleRows.count { it.isResolved },
    )
    val shedCompleted: Int get() = shedDrafts.count { it.readyToSubmit }
    // NO EXPECTED-ANIMAL DENOMINATOR (maintainer decision 2026-07-31). Weighing is
    // free-flow: the operator scans whatever is in front of them, so there is no known
    // total and "X of N animals" is a number we cannot honestly produce. The old
    // `individualResolved / totalExpected` ratio implied a completeness we never had.
    //
    // Progress is now capture-relative only: of the scans captured in THIS bucket, how
    // many are ready. That is a real fraction of a real set.
    val progress: Float get() = when {
        isShedPartition -> if (shedCompleted > 0) 1f else 0f
        visibleRows.isNotEmpty() -> individualCompleted.toFloat() / visibleRows.size.toFloat()
        else -> 0f
    }
    val canRecordIndividual: Boolean get() =
        hasScope && !isShedPartition && !actionInFlight && !selectedAnimalId.isNullOrBlank() && weightInput.toDoubleOrNull()?.let { it > 0.0 } == true
    val canRecordShedPartition: Boolean get() =
        hasScope && isShedPartition && !actionInFlight &&
            weightInput.toDoubleOrNull()?.let { it > 0.0 } == true &&
            animalCountInput.toIntOrNull()?.let { it > 0 } == true &&
            shedProofs.any { it.status == ProofUploadStatus.SYNCED }

    /**
     * Why Submit cannot be pressed yet, in farm language — or null when it can.
     *
     * A blocked action must always state its REASON. A greyed-out Submit next to a video that
     * is still uploading (or that failed) left an operator standing in the shed with a retry
     * button, a dead button, and nothing on screen saying which one was the hold-up.
     *
     * Ordered most-blocking first, so the operator is told the ONE thing to do next.
     */
    @get:StringRes
    val submitBlockedReason: Int? get() = when {
        !hasScope -> R.string.weighing_blocked_no_shed_open
        !isShedPartition -> individualSubmitBlockedReason
        actionInFlight -> R.string.weighing_blocked_saving
        weightInput.toDoubleOrNull()?.let { it > 0.0 } != true -> R.string.weighing_blocked_need_weight
        animalCountInput.toIntOrNull()?.let { it > 0 } != true -> R.string.weighing_blocked_need_count
        shedProofs.isEmpty() -> R.string.weighing_blocked_need_video
        shedProofs.any { it.status == ProofUploadStatus.SYNCED } -> null
        shedProofs.any { it.status == ProofUploadStatus.UPLOADING } -> R.string.weighing_blocked_video_uploading
        shedProofs.all { it.status == ProofUploadStatus.FAILED } -> R.string.weighing_blocked_video_failed
        else -> R.string.weighing_blocked_need_video
    }

    private val individualSubmitBlockedReason: Int? get() = when {
        visibleRows.isEmpty() -> R.string.weighing_blocked_nothing_captured
        visibleRows.any { !it.weightSaved } -> R.string.weighing_blocked_need_weight
        visibleRows.any { it.proofUploadStatus == ProofUploadStatus.UPLOADING } ->
            R.string.weighing_blocked_video_uploading
        visibleRows.any { it.proofUploadStatus == ProofUploadStatus.FAILED } ->
            R.string.weighing_blocked_video_failed
        visibleRows.all { it.proofUploadStatus == ProofUploadStatus.SYNCED } -> null
        else -> R.string.weighing_blocked_need_video
    }
}

data class WeighingProofUiRow(
    val id: String,
    val label: String,
    val status: ProofUploadStatus,
)

// Weighing is free-flow: this row is identified ONLY by the scanned identifier. It carries no
// herd/clinical/vaccination context (no expected/actual shed, no availability status, no
// "wrong shed" flag) -- those are herd-module concepts that do not belong in weighing (maintainer
// mandate: weighing never resolves a scan to a goat or reads herd/clinical state).
data class WeighingRosterUiRow(
    val id: String,
    val animalId: String,
    val displayAnimalId: String,
    val status: String,
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
    /**
     * Set when a verifier sent THIS animal back. The row renders green and "Video synced" on the
     * strength of upload state alone, so without this a rejected animal is indistinguishable from
     * an accepted one and the operator only finds out when Submit refuses the shed.
     */
    val sentBack: Boolean = false,
    val sentBackReason: String? = null,
    /**
     * Set when the server rejected the weight write with a conflict (409). The weight was
     * silently discarded and the operator must re-capture this animal to retry.
     */
    val weightSyncConflict: Boolean = false,
) {
    val isResolved: Boolean
        get() = status.equals("weighed", ignoreCase = true) ||
            status.equals("completed", ignoreCase = true) ||
            status.equals("accepted", ignoreCase = true)
}

/**
 * ONE person's weighing work, as the oversight surface reads it.
 *
 * Everything here is a backend-owned plain count. [notStarted] + [capturing] + [submitted] +
 * [accepted] == [shedCount] exactly, which is what lets the card draw a DISCRETE state ladder;
 * free-flow weighing has no expected-animal total, so nothing here is ever divided by anything.
 *
 * [name] is the backend-resolved display name. A blank name beside a non-blank [operatorUserId] is
 * a roster gap and is NAMED as one — this screen never falls back to rendering a user id.
 */
data class WeighingOperatorUiRow(
    val operatorUserId: String,
    val name: String,
    val shedCount: Int,
    /** FACT 1 of 2: animals this person has a recorded weight for, submitted or not. */
    val animalsWeighed: Int,
    /** FACT 2 of 2: the subset of [animalsWeighed] submitted for verification. */
    val animalsSubmitted: Int,
    val notStarted: Int,
    val capturing: Int,
    val submitted: Int,
    val accepted: Int,
    /** Buckets a verifier sent back. Overlaps the four state counts; never added to them. */
    val rework: Int,
) {
    /** Nobody holds these buckets yet. Distinct from a roster gap, which has an id. */
    val isUnassigned: Boolean get() = operatorUserId.isBlank()

    /** An assigned bucket whose owner has no active workforce record. */
    val hasRosterGap: Boolean get() = operatorUserId.isNotBlank() && name.isBlank()
}

data class WeighingAssignmentUiRow(
    val campaignId: String,
    val tenantId: String,
    val parkId: String,
    val parkLabel: String,
    val workGroupId: String,
    val campaignShedId: String,
    val expectedLocationId: String,
    val expectedLocationLabel: String,
    val label: String,
    val category: String,
    /**
     * The assignee's backend-resolved display NAME. Blank means the screen says so rather than
     * falling back to a user id; the oversight surface exists to answer "who did this".
     */
    val operatorName: String = "",
    val backendStatus: String = "",
    val status: String,
    // No expectedCount / denominator here: weighing is free-flow, so there is no expected-animal
    // list to count against.
    val periodLabel: String,
    val readyToClose: Boolean = false,
    val pendingVerificationCount: Int = 0,
    val reworkCount: Int = 0,
    val latestReworkReason: String = "",
    val plannedBusinessDate: String = "",
    val dueBusinessDate: String = "",
) {
    val uiKey: String
        get() = listOf(campaignId, workGroupId, campaignShedId, category, periodLabel)
            .joinToString(":")

    // After operator submits, bucket is non-clickable until reopened or verifier sends rework
    val isSubmittedAndWaitingVerification: Boolean
        get() = rawStatus.equals("completed", ignoreCase = true)

    val isClosed: Boolean
        get() = rawStatus.equals("closed", ignoreCase = true)

    val isRework: Boolean
        get() = reworkCount > 0

    val isDelayedBacklog: Boolean
        get() = plannedBusinessDate.isBeforeIsoDate(dueBusinessDate)

    val rawStatus: String
        get() = if (isDelayedBacklog) "delayed" else backendStatus.ifBlank { status }

    val isClickable: Boolean
        get() = !isSubmittedAndWaitingVerification && !isClosed

    val canClose: Boolean
        get() = readyToClose && !isClosed

    val isShedPartition: Boolean
        get() = category.trim().equals("per_shed_partition", ignoreCase = true)
}

/** One filter chip: a stable id, its label, and whether it is the active filter. */
data class WeighingFilterChipUiRow(
    val id: String,
    val label: String,
    val selected: Boolean,
)

data class WeighingParkFilterUiRow(
    val parkId: String,
    val label: String,
    val selected: Boolean,
)

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
    onConfirmSubmitIndividualScope: () -> Unit = {},
    onDismissSubmitConfirmation: () -> Unit = {},
    onRecordShedPartition: () -> Unit = {},
    onCaptureShedVideo: () -> Unit = {},
    onRetryShedVideo: (String) -> Unit = {},
    onReplaceShedVideo: (String) -> Unit = {},
    onRemoveShedVideo: (String) -> Unit = {},
    onReconnectReader: () -> Unit = {},
    onOpenAssignment: (WeighingAssignmentUiRow) -> Unit = {},
    onReopenAssignment: (WeighingAssignmentUiRow, String) -> Unit = { _, _ -> },
    onAssignmentRowVisible: (Int) -> Unit = {},
    onSelectPark: (String?) -> Unit = {},
    onRefresh: () -> Unit = {},
    onBack: () -> Unit = {},
    modifier: Modifier = Modifier,
) {
    var rosterSheetOpen by remember { mutableStateOf(false) }
    // Reopen is reason-bearing: the sentence is kept on the audit trail forever, so a person
    // writes it. Held by assignment UI key, and the typed reason saved, so neither is lost on rotation.
    var reopenAssignmentKey by rememberSaveable { mutableStateOf<String?>(null) }
    var reopenReason by rememberSaveable { mutableStateOf("") }
    reopenAssignmentKey?.let { key -> state.assignments.firstOrNull { it.uiKey == key } }?.let { row ->
        WeighingReasonDialog(
            title = stringResource(R.string.weighing_reopen_dialog_title, row.label),
            subtitle = stringResource(R.string.weighing_reopen_dialog_subtitle),
            placeholder = stringResource(R.string.weighing_reopen_dialog_placeholder),
            confirmLabel = stringResource(R.string.weighing_reopen_dialog_confirm),
            confirmColor = MeshaColors.BrandD,
            reason = reopenReason,
            onReasonChange = { reopenReason = it },
            onConfirm = { reason ->
                reopenAssignmentKey = null
                reopenReason = ""
                onReopenAssignment(row, reason)
            },
            onDismiss = {
                reopenAssignmentKey = null
                reopenReason = ""
            },
        )
    }
    if (!state.hasScope) {
        RefreshOnResume(onRefresh = onRefresh)
    }
    if (rosterSheetOpen) {
        WeighingRosterSheet(
            title = state.title.ifBlank { stringResource(R.string.weighing_rows_title) },
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
            onConfirmSubmitIndividualScope = onConfirmSubmitIndividualScope,
            onDismissSubmitConfirmation = onDismissSubmitConfirmation,
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
            title = if (state.plannerMode) {
                stringResource(R.string.weighing_home_title_plan)
            } else {
                stringResource(R.string.weighing_home_title_my_work)
            },
            eyebrow = stringResource(R.string.weighing_eyebrow),
            eyebrowColor = MeshaColors.BrandD,
            onBack = onBack,
            actions = {
                SyncIconButton(
                    isSyncing = state.loading,
                    onSync = onRefresh,
                    contentDescription = stringResource(R.string.weighing_refresh),
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
                    if (state.hasScope) {
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
                        // NOTHING READ YET is not the same as NOTHING TO DO. A freshly navigated screen
                        // starts with an empty state flow and its refresh has not necessarily begun, so
                        // showing a skeleton before a single page load would be a loading wall that then
                        // flips to content or an empty state. That flip is the flicker being removed.
                        // Loading is already told by the screen's own refresh/action choreography, never
                        // by a shimmer that flashes in and out on every navigation.
                        // Renders nothing while the first read is in flight -- no shimmer, and no
                        // empty state either, which would be a wrong answer that then flips.
                        // Gated on the marker alone: WeighingViewModel now publishes hasLoadedOnce
                        // (set in refreshAssignments' finally, keyed by park filter), so it is safe
                        // to drop `loading` -- keeping it would have re-blanked already-drawn rows
                        // the instant a background refresh started.
                        if (state.assignments.isEmpty() && !state.hasLoadedOnce) {
                            // NOTHING is drawn here. The absence of content is not known until the first
                            // read completes, so rendering a skeleton before that read is a confident
                            // wrong answer that then flips to a correct one.
                        } else if (state.assignments.isEmpty()) {
                            EmptyWorkCard(
                                title = stringResource(R.string.weighing_empty_title),
                                body = stringResource(R.string.weighing_empty_body),
                                showRefreshHint = true,
                            )
                        }
                    }
                    state.message?.takeIf { it.isNotBlank() }?.let {
                        MessageStrip(it)
                    }
                }
            }
            if (!state.plannerMode && !state.hasScope && state.assignments.isNotEmpty()) {
                if (state.parkFilters.size > 1) {
                    item {
                        WeighingParkFilters(
                            filters = state.parkFilters,
                            onSelect = onSelectPark,
                        )
                    }
                }
                itemsIndexed(state.assignments, key = { _, row -> row.uiKey }) { index, row ->
                    // The list itself pulls the next page as the operator scrolls near the end.
                    LaunchedEffect(row.uiKey, index, state.assignments.size) {
                        onAssignmentRowVisible(index)
                    }
                    AssignmentRow(
                        row = row,
                        onOpen = { onOpenAssignment(row) },
                        onReopen = {
                            reopenAssignmentKey = row.uiKey
                            reopenReason = ""
                        },
                    )
                }
                if (state.assignmentsLoadingMore) {
                    item(key = "assignments-loading-more") { ListLoadingFooter() }
                }
            }

            if (state.shedDrafts.isNotEmpty()) {
                item { SectionTitle(stringResource(R.string.weighing_section_shed_result)) }
                items(state.shedDrafts, key = { it.id }) { row ->
                    DraftRow(row)
                }
            }

            if (state.individualDrafts.isNotEmpty()) {
                item { SectionTitle(stringResource(R.string.weighing_section_queued_weights)) }
                items(state.individualDrafts, key = { it.id }) { row ->
                    DraftRow(row)
                }
            }

            if (state.hasScope && !state.isShedPartition && state.visibleRows.isNotEmpty()) {
                item { SectionTitle(stringResource(R.string.weighing_section_recent_updates)) }
                items(state.visibleRows.take(3), key = { it.id }) { row ->
                    RosterRow(row)
                }
            }
        }
    }
}

@Composable
internal fun ListLoadingFooter() {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 12.dp),
        horizontalArrangement = Arrangement.Center,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        CircularProgressIndicator(
            modifier = Modifier.size(16.dp),
            color = MeshaColors.Brand,
            strokeWidth = 2.dp,
        )
        Spacer(Modifier.width(8.dp))
        Text(
            text = stringResource(R.string.weighing_loading_more_work),
            color = MeshaColors.Muted,
            style = MaterialTheme.typography.bodySmall,
        )
    }
}

/**
 * One filter chip row, shared by every weighing surface that narrows a list.
 *
 * Filtering is deliberately how these lists narrow, rather than splitting them into per-group
 * sections: a sectioned list cannot paginate. A keyset page of ~20 splits mid-group, leaving a
 * header showing three of an operator's fourteen sheds with the rest arriving pages later. One
 * flat list behind a chip pages uniformly no matter how many groups exist.
 *
 * The leading pill clears the filter, so [onSelect] receives null for "show everything".
 */
@Composable
internal fun WeighingFilterChips(
    options: List<WeighingFilterChipUiRow>,
    allLabel: String,
    onSelect: (String?) -> Unit,
) {
    FlowRow(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        val anySelected = options.any { it.selected }
        WeighingFilterPill(
            label = allLabel,
            selected = !anySelected,
            onClick = { onSelect(null) },
        )
        options.forEach { option ->
            WeighingFilterPill(
                label = option.label,
                selected = option.selected,
                onClick = { onSelect(option.id) },
            )
        }
    }
}

/** Park chips. Kept as its own entry point so existing call sites read as what they filter. */
@Composable
internal fun WeighingParkFilters(
    filters: List<WeighingParkFilterUiRow>,
    onSelect: (String?) -> Unit,
) {
    WeighingFilterChips(
        options = filters.map { WeighingFilterChipUiRow(id = it.parkId, label = it.label, selected = it.selected) },
        allLabel = stringResource(R.string.weighing_all_parks),
        onSelect = onSelect,
    )
}

@Composable
internal fun WeighingFilterPill(label: String, selected: Boolean, onClick: () -> Unit) {
    val bg = if (selected) MeshaColors.Brand else MeshaColors.Surf2
    val edge = if (selected) MeshaColors.Brand else MeshaColors.Hair
    val fg = if (selected) MeshaColors.PageBg else MeshaColors.Ink
    Box(
        modifier = Modifier
            .height(48.dp)
            .clip(RoundedCornerShape(24.dp))
            .background(bg)
            .border(1.dp, edge, RoundedCornerShape(24.dp))
            .clickable(onClick = onClick)
            .padding(horizontal = 13.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(text = label, color = fg, style = MeshaType.pillStrong)
    }
}

@Composable
private fun AssignmentRow(
    row: WeighingAssignmentUiRow,
    onOpen: () -> Unit,
    onReopen: () -> Unit,
) {
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
                .clickable(
                    enabled = row.isClickable,
                    onClick = if (row.isClosed) onReopen else onOpen
                )
                .padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(9.dp),
        ) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                StatusPill(if (row.isRework) stringResource(R.string.weighing_status_sent_back) else mapWeighingStatusLabel(row.rawStatus))
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
            if (row.isClickable) {
                Text(
                    text = assignmentAction(row),
                    color = MeshaColors.BrandD,
                    style = MeshaType.cta,
                    modifier = Modifier
                        .minimumInteractiveComponentSize()
                        .clickable(onClick = onOpen)
                        .padding(top = 2.dp),
                )
            } else if (row.isClosed) {
                Text(
                    text = stringResource(R.string.weighing_reopen),
                    color = MeshaColors.BrandD,
                    style = MeshaType.cta,
                    modifier = Modifier
                        .minimumInteractiveComponentSize()
                        .clickable(onClick = onReopen)
                        .padding(top = 2.dp),
                )
            } else {
                // Submitted and waiting for verification - non-clickable
                Text(
                    text = pendingVerificationText(row),
                    color = MeshaColors.Muted,
                    style = MeshaType.cta,
                    modifier = Modifier
                        .minimumInteractiveComponentSize()
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
        text = status.ifBlank { stringResource(R.string.weighing_status_not_started) },
        color = color,
        style = MeshaType.pillStrong,
        modifier = Modifier
            .clip(RoundedCornerShape(9.dp))
            .background(bg)
            .padding(horizontal = 10.dp, vertical = 6.dp),
    )
}

@Composable
private fun CategoryPill(category: String) {
    // The pill's whole job is to tell lump-sum apart from individual at a glance, so the two
    // categories must not share a colour. Purple == lump-sum is the convention every other
    // weighing surface already uses (task detail, plan wizard, leadership videos); this pill
    // was painting BOTH purple, which made the only distinguishing chip on the work list
    // carry no information at all.
    val (fg, bg) = categoryPillTone(category)
    Text(
        text = weighingCategoryLabel(category),
        color = fg,
        style = MeshaType.pillStrong,
        modifier = Modifier
            .clip(RoundedCornerShape(9.dp))
            .background(bg)
            .padding(horizontal = 10.dp, vertical = 6.dp),
    )
}

/**
 * Foreground/background for the work-list category pill. Extracted so the ONE property that
 * makes the pill worth rendering -- lump-sum and individual do not look alike -- is assertable
 * without a screenshot. Paparazzi happily re-recorded the all-purple version as "correct".
 */
/**
 * The row's proof label for an upload status. Every status states its OWN truth.
 *
 * MISSING used to fall into a catch-all "Video captured" branch -- the only status that could
 * ever reach it, since the other three were matched first -- so the one row with no video was
 * the one that claimed to have one, while Submit stayed blocked demanding the operator record it.
 */
@StringRes
internal fun proofRowLabelRes(status: ProofUploadStatus): Int = when (status) {
    ProofUploadStatus.MISSING -> R.string.weighing_proof_required
    ProofUploadStatus.UPLOADING -> R.string.weighing_proof_syncing
    ProofUploadStatus.FAILED -> R.string.weighing_proof_upload_failed
    ProofUploadStatus.SYNCED -> R.string.weighing_proof_synced
}

internal fun categoryPillTone(category: String): Pair<Color, Color> =
    if (category.trim().equals("per_shed_partition", ignoreCase = true)) {
        MeshaColors.Purple to MeshaColors.PurpleX
    } else {
        MeshaColors.BrandD to MeshaColors.Surf3
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
private fun mapWeighingStatusLabel(backendStatus: String): String = when {
    backendStatus.equals("pending", ignoreCase = true) -> stringResource(R.string.weighing_status_scheduled)
    backendStatus.equals("in_progress", ignoreCase = true) -> stringResource(R.string.weighing_status_in_progress)
    backendStatus.equals("delayed", ignoreCase = true) -> stringResource(R.string.weighing_status_delayed)
    backendStatus.equals("completed", ignoreCase = true) -> stringResource(R.string.weighing_status_submitted)
    backendStatus.equals("closed", ignoreCase = true) -> stringResource(R.string.weighing_status_closed)
    backendStatus.equals("canceled", ignoreCase = true) -> stringResource(R.string.weighing_status_canceled)
    backendStatus.equals("cancelled", ignoreCase = true) -> stringResource(R.string.weighing_status_canceled)
    else -> backendStatus
}

internal fun assignmentSummaryRes(row: WeighingAssignmentUiRow): Int =
    if (row.isRework) {
        R.string.weighing_assignment_summary_sent_back
    } else if (row.isShedPartition) {
        R.string.weighing_assignment_summary_lumpsum
    } else {
        R.string.weighing_assignment_summary_individual
    }

internal fun assignmentActionRes(row: WeighingAssignmentUiRow): Int =
    if (row.isRework) {
        R.string.weighing_action_redo_proof
    } else if (row.isShedPartition) {
        R.string.weighing_action_record_lumpsum
    } else {
        R.string.weighing_action_scan_animals
    }

@Composable
private fun assignmentSummary(row: WeighingAssignmentUiRow): String =
    when {
        row.isRework -> {
            val date = row.displayBusinessDate()
            val reason = row.latestReworkReason.takeIf { it.isNotBlank() }
            when {
                date.isNotBlank() && reason != null -> stringResource(R.string.weighing_assignment_summary_sent_back_date_reason, date, reason)
                date.isNotBlank() -> stringResource(R.string.weighing_assignment_summary_sent_back_date, date)
                reason != null -> stringResource(R.string.weighing_assignment_summary_sent_back_reason, reason)
                else -> stringResource(assignmentSummaryRes(row))
            }
        }
        row.isSubmittedAndWaitingVerification -> {
            row.displayBusinessDate().takeIf { it.isNotBlank() }?.let {
                stringResource(R.string.weighing_pending_verification_with_date, it)
            } ?: stringResource(R.string.weighing_pending_verification)
        }
        else -> {
            val base = stringResource(assignmentSummaryRes(row))
            row.displayBusinessDate().takeIf { it.isNotBlank() }?.let { "$it · $base" } ?: base
        }
    }

@Composable
private fun assignmentAction(row: WeighingAssignmentUiRow): String =
    stringResource(assignmentActionRes(row))

@Composable
private fun pendingVerificationText(row: WeighingAssignmentUiRow): String =
    row.displayBusinessDate().takeIf { it.isNotBlank() }?.let {
        stringResource(R.string.weighing_pending_verification_with_date, it)
    } ?: stringResource(R.string.weighing_pending_verification)

private fun WeighingAssignmentUiRow.displayBusinessDate(): String =
    when {
        isRework -> plannedBusinessDate.ifBlank { dueBusinessDate }
        isDelayedBacklog -> plannedBusinessDate
        else -> dueBusinessDate.ifBlank { plannedBusinessDate }
	}.takeIf { it.isNotBlank() }?.let { raw ->
		// exception:exempt display date fallback; if server sends non-ISO text, show it raw.
		runCatching {
			LocalDate.parse(raw).format(DateTimeFormatter.ofPattern("d MMM", Locale.ENGLISH))
		}.getOrDefault(raw)
	}.orEmpty()

private fun String.isBeforeIsoDate(other: String): Boolean {
    val left = runCatching { LocalDate.parse(this) }.getOrNull() ?: return false
    val right = runCatching { LocalDate.parse(other) }.getOrNull() ?: return false
    return left.isBefore(right)
}

@Composable
private fun weighingCategoryLabel(category: String): String =
    if (category == "per_shed_partition") {
        stringResource(R.string.weighing_category_lumpsum)
    } else {
        stringResource(R.string.weighing_category_individual)
    }

@Composable
private fun EmptyWorkCard(title: String, body: String, showRefreshHint: Boolean = false) {
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
        if (showRefreshHint) {
            Text(
                text = stringResource(R.string.weighing_pull_to_refresh_hint),
                color = MeshaColors.BrandD,
                style = MeshaType.cta,
            )
        }
    }
}

@Composable
private fun MessageStrip(message: String) {
    // Server-authored blocks are errors too. Matching only on "failed"/"couldn't"/"error" styled
    // a rework bounce ("A video was sent back. Re-record that animal...") as neutral chrome, which
    // reads as confirmation of the very submit it just refused.
    val isError = message.contains("failed", ignoreCase = true) ||
        message.contains("couldn't", ignoreCase = true) ||
        message.contains("error", ignoreCase = true) ||
        message.contains("sent back", ignoreCase = true)
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
                iconLabel = stringResource(R.string.weighing_icon_kg),
                title = stringResource(R.string.weighing_record_shed_result),
                body = stringResource(R.string.weighing_record_shed_result_body),
                tone = MeshaColors.Purple,
                background = MeshaColors.PurpleX,
            )
        } else {
            WeighingActionCard(
                iconLabel = stringResource(R.string.weighing_icon_rfid),
                title = state.selectedAnimalLabel ?: stringResource(R.string.weighing_scan_tag_now),
                body = if (state.selectedAnimalLabel == null) {
                    stringResource(R.string.weighing_scan_keep_scanning)
                } else {
                    stringResource(R.string.weighing_scan_add_weight_and_video)
                },
                tone = MeshaColors.Brand,
                background = MeshaColors.BrandTint,
            )
            InlineEntryCard(
                label = stringResource(R.string.weighing_field_tag_label),
                value = state.scanInput,
                placeholder = stringResource(R.string.weighing_field_tag_placeholder),
                onValueChange = onScanInputChange,
                actionLabel = stringResource(R.string.weighing_field_tag_action),
                actionEnabled = !state.actionInFlight && state.scanInput.isNotBlank(),
                onAction = onScanSubmit,
            )
        }
        if (state.isShedPartition) {
            InlineEntryCard(
                label = stringResource(R.string.weighing_field_weight_label),
                value = state.weightInput,
                placeholder = stringResource(R.string.weighing_field_weight_placeholder),
                suffix = stringResource(R.string.weighing_kg),
                onValueChange = onWeightChange,
                actionLabel = stringResource(R.string.weighing_field_weight_action),
                actionEnabled = state.canRecordShedPartition,
                onAction = onRecordShedPartition,
            )
        }
    }
}

@Composable
private fun RosterPeekCard(
    total: Int,
    visible: Int,
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
            Text(
                stringResource(R.string.weighing_view_rows),
                color = MeshaColors.Ink,
                style = MeshaType.bodyStrong,
            )
            Text(
                text = rosterPeekSummary(total = total, visible = visible),
                color = MeshaColors.Muted,
                style = MeshaType.cardSubtitle,
                modifier = Modifier.padding(top = 3.dp),
            )
        }
        Text(
            text = stringResource(R.string.weighing_open),
            color = MeshaColors.BrandD,
            style = MeshaType.cta,
        )
    }
}

@Composable
private fun rosterPeekSummary(
    @Suppress("UNUSED_PARAMETER") total: Int,
    visible: Int,
): String = capturedRowsLabel(visible)

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
                row.displayAnimalId.contains(query, ignoreCase = true)
            }
        }
    }
    Surface(color = MeshaColors.Surf, modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(bottom = 22.dp)) {
            Column(Modifier.padding(horizontal = 20.dp)) {
                Text(title, color = MeshaColors.Ink, style = MeshaType.headerTitle)
                Text(
                    text = rosterSheetSubtitle(filtered.size, totalExpected),
                    color = MeshaColors.Muted,
                    style = MeshaType.cardSubtitle,
                    modifier = Modifier.padding(top = 2.dp),
                )
            }
            Spacer(Modifier.height(10.dp))
            OutlinedTextField(
                value = query,
                onValueChange = { query = it },
                singleLine = true,
                placeholder = {
                    Text(
                        stringResource(R.string.weighing_search_tag_or_shed),
                        color = MeshaColors.Faint,
                        style = MeshaType.cardSubtitle,
                    )
                },
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 16.dp),
            )
            Spacer(Modifier.height(6.dp))
            if (filtered.isEmpty()) {
                EmptyWorkCard(
                    title = stringResource(R.string.weighing_no_matching_rows_title),
                    body = stringResource(R.string.weighing_no_matching_rows_body),
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

@Composable
private fun rosterSheetSubtitle(
    visibleCount: Int,
    @Suppress("UNUSED_PARAMETER") totalExpected: Int,
): String = capturedRowsLabel(visibleCount)

@Composable
private fun capturedRowsLabel(count: Int): String = if (count == 1) {
    stringResource(R.string.weighing_captured_rows_one, count)
} else {
    stringResource(R.string.weighing_captured_rows_other, count)
}

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
    onConfirmSubmitIndividualScope: () -> Unit,
    onDismissSubmitConfirmation: () -> Unit,
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
    // ONE row owns the keypad, decided here for the whole list -- never per row.
    //
    // Each row used to request focus itself whenever it had a video and no saved weight. With
    // several unfinished animals on screen that is a race: every such row fires its effect and
    // whichever composes last steals the keypad, so the operator can be typing into an animal
    // they did not just scan. The newest unfinished capture is the one the camera just returned
    // from, so it is the only legitimate owner.
    val autoFocusRowId = remember(state.visibleRows) {
        state.visibleRows.lastOrNull { row ->
            !row.weightSaved && row.proofUploadStatus != ProofUploadStatus.MISSING
        }?.id
    }
    if (state.showSubmitConfirmation) {
        SubmitConfirmationDialog(
            title = stringResource(R.string.weighing_submit_confirm_title),
            subtitle = stringResource(R.string.weighing_submit_confirm_body),
            dismissLabel = stringResource(R.string.weighing_submit_confirm_dismiss),
            confirmLabel = stringResource(R.string.weighing_submit_confirm_action),
            onConfirm = onConfirmSubmitIndividualScope,
            onDismiss = onDismissSubmitConfirmation,
        )
    }
    Scaffold(
        modifier = modifier.fillMaxSize(),
        containerColor = MeshaColors.PageBg,
        topBar = {
            MeshaScreenHeader(
                title = state.title.ifBlank { stringResource(R.string.weighing_title) },
                eyebrow = stringResource(R.string.weighing_eyebrow),
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
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .background(MeshaColors.PageBg),
            ) {
                // Disabled-with-reason: never a dead button on its own. The line says which
                // single thing is holding the submission up (usually a video still going up).
                state.submitBlockedReason?.let { reason ->
                    Text(
                        text = stringResource(reason),
                        color = MeshaColors.Muted,
                        style = MeshaType.bodyStrong,
                        textAlign = TextAlign.Center,
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(horizontal = 16.dp),
                    )
                }
                ActionButton(
                    text = if (state.isShedPartition) {
                        stringResource(R.string.weighing_submit_lump_sum)
                    } else {
                        stringResource(R.string.weighing_submit)
                    },
                    enabled = if (state.isShedPartition) state.canRecordShedPartition else state.individualSubmitReady,
                    onClick = if (state.isShedPartition) onRecordShedPartition else onSubmitIndividualScope,
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(horizontal = 16.dp, vertical = 12.dp),
                    primary = true,
                )
            }
        },
    ) { padding ->
        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                // CONSUME THE KEYBOARD INSET. MainActivity calls enableEdgeToEdge(), and under
                // edge-to-edge the manifest's adjustResize NO LONGER resizes the window for the
                // IME -- the app must consume the inset itself. Without this the list keeps its
                // full height when the keyboard opens, so the bottom of the capture screen
                // (including Submit) sits BEHIND the keyboard with dead space above it and nothing
                // scrollable into view. Reported from the shed on 2026-08-08: "I can't scroll to
                // see submit, blank black space on top of keyboard".
                .imePadding(),
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
                // Show EVERY message, not only the duplicate notice. Filtering on the
                // "Already scanned" prefix silently dropped every other message this screen
                // produces — including the submit rejection ("a video was sent back, re-record
                // that animal first"), which left the operator tapping Submit on a shed that
                // could never go through, with nothing on screen to say why.
                state.message?.takeIf { it.isNotBlank() }?.let { message ->
                    item {
                        if (message.startsWith("Already scanned")) {
                            WeighingDuplicateNotice(message)
                        } else {
                            MessageStrip(message)
                        }
                    }
                }
                item {
                    Text(
                        text = if (state.visibleRows.size == 1) {
                            stringResource(R.string.weighing_animals_captured_one, state.visibleRows.size)
                        } else {
                            stringResource(R.string.weighing_animals_captured_other, state.visibleRows.size)
                        },
                        color = MeshaColors.Ink,
                        style = MeshaType.bodyStrong,
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
                if (state.visibleRows.isEmpty()) {
                    item {
                        Text(
                            text = stringResource(R.string.weighing_scan_to_begin),
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
                        // Per-ROW id, not the animal: a list can hold more than one row per
                        // animal, and duplicate lazy keys crash with "Key was already used"
                        // (android-compose-lists-guard, lazy-list-entity-id-key).
                        //
                        // This was briefly keyed on animalId while chasing a keyboard defect on the
                        // theory that row.id churn was tearing the weight field out from under the
                        // IME. It was not: the system IME is a separate Gboard window that the
                        // capture screen's transition tears down regardless, which is why the field
                        // now uses an in-app keypad instead. Do not key on the animal again.
                        key = { row -> row.id },
                    ) { row ->
                        WeighingFreeFlowFeedRow(
                            row = row,
                            autoFocusWeight = row.id == autoFocusRowId,
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
                text = reader?.readerName ?: stringResource(R.string.weighing_reader_default_name),
                color = MeshaColors.Ink,
                style = MeshaType.bodyStrong,
            )
            Text(
                text = reader?.statusLabel ?: stringResource(R.string.weighing_reader_checking),
                color = MeshaColors.Muted,
                style = MeshaType.caption,
            )
        }
        if (!connected) {
            ActionButton(
                text = reader?.actionLabel ?: stringResource(R.string.weighing_reader_reconnect),
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
    autoFocusWeight: Boolean,
    updating: Boolean,
    onWeightChange: (String) -> Unit,
    onWeightEntryActive: (Boolean) -> Unit,
    onSaveWeight: (String) -> Unit,
    onRetryVideo: () -> Unit,
    onReuploadVideo: () -> Unit,
) {
    val focusManager = LocalFocusManager.current
    val weightKeyboard = LocalSoftwareKeyboardController.current
    val windowInfo = LocalWindowInfo.current
    var weightFieldFocused by remember(row.animalId) { mutableStateOf(false) }
    val weightBringIntoView = remember(row.animalId) { BringIntoViewRequester() }
    val weightBringIntoViewScope = rememberCoroutineScope()
    val weightFieldView = LocalView.current
    val weightFocusRequester = remember(row.animalId) { FocusRequester() }

    // Select the freshly captured row so its keypad is up the moment the camera returns.
    //
    // The keypad renders only for the focused row (one keypad, not one per animal), so without
    // this nothing focuses and the operator still has to tap the field first -- the exact
    // complaint the keypad was meant to end. Requesting focus is safe HERE in a way it never was
    // with the system IME: the field is readOnly, so focus selects the row without asking Android
    // for a keyboard, and there is no InputConnection for the capture screen's window transition
    // to tear down.
    //
    // Gated on proofUploadStatus leaving MISSING, i.e. a video exists and the camera has already
    // come and gone -- focusing before that aims at a row the transition is about to replace.
    //
    // [autoFocusWeight] is decided ONCE for the whole list by the parent, so exactly one row can
    // claim the keypad. Deciding it here per row let every unfinished animal request focus and the
    // last one composed win, which put the keypad on an animal the operator had not just scanned.
    LaunchedEffect(row.id, autoFocusWeight, row.weightSaved) {
        if (!autoFocusWeight) return@LaunchedEffect
        if (row.weightSaved) return@LaunchedEffect
        runCatching { weightFocusRequester.requestFocus() }
    }
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
    // Open the keyboard on the freshly scanned animal. The field was already focused -- it even
    // drew its focused outline and caret -- but requesting focus does NOT raise the soft keyboard,
    // so the operator saw a live-looking field, typed nothing, and had to tap it a second time.
    // In a shed, holding a reader in one hand, that second tap is the whole interaction.
    // NO SYSTEM IME ON THIS FIELD. Eight attempts to make Android's keyboard behave here failed,
    // because the keyboard is not the app's to control: the row is composed the moment the tag is
    // scanned, the capture screen takes the window token a split second later, and the IME is
    // killed by that transition (dumpsys input_method: SHOW_SOFT_INPUT -> ATTACH_NEW_INPUT ->
    // HIDE_UNSPECIFIED_WINDOW 29ms later, softInputMode ADJUST_NOTHING). Focus retries, forcing the
    // IME, stable list keys, window-focus gating and post-camera gating all left the operator
    // tapping the field a second time in a shed.
    //
    // The weight is a number with a decimal point. An in-app keypad renders inside our own window,
    // so no InputConnection, no window-token race, and no interference from the RFID reader (which
    // pairs as a Bluetooth HID keyboard). It also keeps the scanner working while the operator
    // types, which the system IME could not.
    val canSaveDraftWeight = draftWeight.toDoubleOrNull()?.let { it > 0.0 } == true
    val complete = row.weightSaved &&
        row.proofUploadStatus == ProofUploadStatus.SYNCED &&
        row.backendSynced
    val tone = when {
        // Sent-back outranks "complete": the upload succeeded, which is exactly why this row
        // looked finished. A rejected animal is the one thing on this screen that still needs work.
        row.sentBack -> MeshaColors.Danger
        complete -> MeshaColors.Ok
        row.proofUploadStatus == ProofUploadStatus.FAILED -> MeshaColors.Danger
        else -> MeshaColors.Hair
    }
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(8.dp))
            .background(
                when {
                    row.sentBack -> MeshaColors.DangerX
                    complete -> MeshaColors.OkX
                    else -> MeshaColors.Surf
                },
            )
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
            if (complete && !row.sentBack) {
                Icon(
                    imageVector = MeshaIcons.CheckCircle,
                    contentDescription = null,
                    tint = MeshaColors.Ok,
                    modifier = Modifier.size(20.dp),
                )
            }
        }
        if (row.sentBack) {
            // The reason is the whole point: "re-record" without saying what was wrong sends the
            // operator back to the pen to shoot the same unusable video again.
            Text(
                text = row.sentBackReason?.takeIf { it.isNotBlank() }
                    ?.let { stringResource(R.string.weighing_row_sent_back_with_reason, it) }
                    ?: stringResource(R.string.weighing_row_sent_back),
                color = MeshaColors.Danger,
                style = MeshaType.bodyStrong,
            )
        }
        Text(
            text = row.scannedAtLabel.orEmpty(),
            color = MeshaColors.Muted,
            style = MeshaType.caption,
        )
        Text(
            text = when {
                complete -> stringResource(
                    R.string.weighing_proof_weight_saved_fmt,
                    row.proofStatusLabel ?: stringResource(R.string.weighing_proof_video_synced),
                )
                row.proofUploadStatus == ProofUploadStatus.SYNCED && !row.backendSynced ->
                    stringResource(
                        R.string.weighing_proof_weight_waiting_fmt,
                        row.proofStatusLabel ?: stringResource(R.string.weighing_proof_synced),
                    )
                else -> row.proofStatusLabel
                    ?: stringResource(proofRowLabelRes(row.proofUploadStatus))
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
                    text = stringResource(
                        R.string.weighing_weight_value_fmt,
                        row.savedWeightLabel ?: row.weightInput,
                    ),
                    color = MeshaColors.Ink,
                    style = MeshaType.bodyStrong,
                    modifier = Modifier.weight(1f),
                )
                ActionButton(
                    text = if (updating) {
                        stringResource(R.string.weighing_updating)
                    } else {
                        stringResource(R.string.weighing_edit_weight)
                    },
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
                    label = { Text(stringResource(R.string.weighing_field_weight_label)) },
                    suffix = { Text(stringResource(R.string.weighing_kg)) },
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                    singleLine = true,
                    enabled = !updating,
                    // Driven by the in-app keypad, never by the system IME. readOnly keeps the
                    // field focusable (so tapping it selects which animal the keypad types into)
                    // while guaranteeing Android never raises its own keyboard for it.
                    readOnly = true,
                    modifier = Modifier
                        .weight(1f)
                        .focusRequester(weightFocusRequester)
                        .onFocusChanged {
                            weightFieldFocused = it.isFocused
                            onWeightEntryActive(it.isFocused)
                        },
                )
                ActionButton(
                    text = when {
                        updating -> stringResource(R.string.weighing_updating)
                        row.weightSaved -> stringResource(R.string.weighing_update)
                        else -> stringResource(R.string.weighing_save)
                    },
                    enabled = canSaveDraftWeight && !updating,
                    onClick = {
                        focusManager.clearFocus()
                        onSaveWeight(draftWeight)
                    },
                    primary = true,
                )
            }
            // App-drawn keypad, not the system IME. See the note above the field: Android's IME is
            // a separate Gboard-owned window, and the capture screen's window transition tears it
            // down no matter how or when we ask for it. These are ordinary buttons in our own
            // window, so the operator can always type a weight the moment the row appears.
            // ONE keypad, on the row being entered -- never one per row. Rendering it for every
            // captured animal stacked a full keypad under each row: two animals meant two keypads,
            // twenty meant twenty, and the list became unusable. The operator types into one animal
            // at a time, so the keypad belongs to the selected row only.
            if (!updating && weightFieldFocused) {
                WeighingNumericKeypad(
                    value = draftWeight,
                    onValueChange = {
                        draftWeight = it
                        onWeightChange(it)
                    },
                    modifier = Modifier.padding(top = 8.dp),
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
                    text = stringResource(R.string.weighing_rescan_to_replace_video),
                    color = MeshaColors.Info,
                    style = MeshaType.caption,
                    modifier = Modifier.weight(1f),
                )
            }
        }
        when (row.proofUploadStatus) {
            ProofUploadStatus.FAILED -> ActionButton(
                text = stringResource(R.string.weighing_retry),
                enabled = true,
                onClick = onRetryVideo,
                modifier = Modifier.fillMaxWidth(),
                primary = false,
            )
            ProofUploadStatus.SYNCED -> ActionButton(
                text = if (row.reuploadRequested) {
                    stringResource(R.string.weighing_waiting_for_scan)
                } else {
                    stringResource(R.string.weighing_reupload)
                },
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
    val focusManager = LocalFocusManager.current
    val keyboardController = LocalSoftwareKeyboardController.current
    fun dismissKeyboard() {
        keyboardController?.hide()
        focusManager.clearFocus()
    }
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
            label = { Text(stringResource(R.string.weighing_field_total_weight)) },
            suffix = { Text(stringResource(R.string.weighing_kg)) },
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
            singleLine = true,
            modifier = Modifier
                .fillMaxWidth()
                .onFocusChanged { onWeightEntryActive(it.isFocused) },
        )
        OutlinedTextField(
            value = state.animalCountInput,
            onValueChange = onAnimalCountChange,
            label = { Text(stringResource(R.string.weighing_field_animal_count)) },
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
                text = stringResource(
                    R.string.weighing_average_per_animal_fmt,
                    "%.2f".format(totalWeight / animalCount),
                ),
                color = MeshaColors.Ok,
                style = MeshaType.bodyStrong,
            )
        }
        // What this shed's proof ACTUALLY holds, by upload state. Deliberately not "N / 5":
        // weighing is free-flow and there is no required video count — 5 is the MOST a shed can
        // hold, not a target — so "1 / 5" read as "4 still missing" when nothing was missing, and
        // it counted a video that had merely been ADDED as "uploaded" while it was still in flight.
        Text(
            text = shedVideoSummary(state.shedProofs),
            color = if (state.shedProofs.size >= SHED_PROOF_VIDEO_LIMIT) MeshaColors.Warn else MeshaColors.Muted,
            style = MeshaType.bodyStrong,
            modifier = Modifier.fillMaxWidth(),
        )
        if (state.shedProofs.size >= SHED_PROOF_VIDEO_LIMIT) {
            Text(
                text = stringResource(R.string.weighing_videos_limit_reached),
                color = MeshaColors.Warn,
                style = MeshaType.cardSubtitle,
                modifier = Modifier.fillMaxWidth(),
            )
        }
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
                    text = stringResource(R.string.weighing_video_index_fmt, index + 1),
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
                        text = stringResource(R.string.weighing_replace),
                        icon = MeshaIcons.Refresh,
                        enabled = !state.actionInFlight,
                        onClick = {
                            dismissKeyboard()
                            onReplaceShedVideo(proof.id)
                        },
                        modifier = Modifier.weight(1f),
                    )
                    ShedVideoAction(
                        text = stringResource(R.string.weighing_remove),
                        icon = MeshaIcons.Close,
                        enabled = !state.actionInFlight,
                        onClick = {
                            dismissKeyboard()
                            onRemoveShedVideo(proof.id)
                        },
                        modifier = Modifier.weight(1f),
                        danger = true,
                    )
                }
                ProofUploadStatus.FAILED -> ShedVideoAction(
                    text = stringResource(R.string.weighing_retry),
                    icon = MeshaIcons.Refresh,
                    enabled = !state.actionInFlight,
                    onClick = {
                        dismissKeyboard()
                        onRetryShedVideo(proof.id)
                    },
                    modifier = Modifier.fillMaxWidth(),
                    danger = true,
                )
                ProofUploadStatus.MISSING,
                ProofUploadStatus.UPLOADING -> Unit
            }
        }
        ActionButton(
            text = if (state.shedProofs.isEmpty()) {
                stringResource(R.string.weighing_capture_group_video)
            } else {
                stringResource(R.string.weighing_add_another_video)
            },
            enabled = !state.actionInFlight && state.shedProofs.size < SHED_PROOF_VIDEO_LIMIT,
            onClick = {
                dismissKeyboard()
                onCaptureShedVideo()
            },
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
        // design-system:ignore: deliberate W700 override on the W600 caption token; dropping it would lighten this action label.
        Text(text = text, color = color, style = MeshaType.caption, fontWeight = FontWeight.Bold)
    }
}

private fun WeighingRosterUiRow.weighingScanLabel(draft: WeighingDraftUiRow? = null): String = buildString {
    when {
        draft != null -> append(draft.weightLabel("Weight saved"))
        status.equals("Weighed", ignoreCase = true) -> append("Weighed")
        else -> append(status)
    }
    if (draft != null) {
        append(if (draft.proofReady) " · video ready" else " · video required")
    }
}

private fun WeighingDraftUiRow.weightLabel(fallback: String): String =
    label.substringAfter(" - ", missingDelimiterValue = "")
        .takeIf { it.isNotBlank() }
        ?: fallback

private fun WeighingRosterUiRow.weighingDetails(draft: WeighingDraftUiRow?): List<String> =
    buildList {
        draft?.label
            ?.substringAfter(" - ", missingDelimiterValue = "")
            ?.takeIf { it.isNotBlank() }
            ?.let { add("Weight: $it") }
        if (draft != null) {
            add(if (draft.proofReady) "Video proof: ready" else "Video proof: required")
        } else {
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
        state.isShedPartition ->
            state.scopeLabel.ifBlank { state.title.ifBlank { stringResource(R.string.weighing_selected_shed) } }
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
                    text = stringResource(R.string.weighing_icon_kg),
                    color = if (state.isShedPartition) MeshaColors.Purple else MeshaColors.Brand,
                    // design-system:ignore: 10sp/W900 glyph badge — no token pairs a ~10sp size with W900 (overline/dayName are W700), so any swap would visibly lighten the badge.
                    fontSize = 10.sp,
                    fontWeight = FontWeight.Black,
                )
            }
            Spacer(Modifier.width(11.dp))
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = if (state.isShedPartition) {
                        stringResource(R.string.weighing_shed_weight_and_proof)
                    } else {
                        stringResource(R.string.weighing_animal_weight_and_proof)
                    },
                    color = MeshaColors.Ink,
                    style = MeshaType.bodyStrong,
                    lineHeight = 17.sp,
                )
                Text(
                    text = selectedLabel ?: stringResource(R.string.weighing_tap_pending_hint),
                    color = if (selectedLabel == null) MeshaColors.Faint else MeshaColors.Muted,
                    style = MeshaType.caption,
                    lineHeight = 15.sp,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.padding(top = 3.dp),
                )
            }
        }
        OutlinedTextField(
            value = state.weightInput,
            onValueChange = onWeightChange,
            label = {
                Text(
                    if (state.isShedPartition) {
                        stringResource(R.string.weighing_field_total_weight)
                    } else {
                        stringResource(R.string.weighing_field_weight_label)
                    },
                )
            },
            suffix = { Text(stringResource(R.string.weighing_kg)) },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        Text(
            text = if (state.isShedPartition) {
                stringResource(R.string.weighing_shed_video_mandatory)
            } else {
                stringResource(R.string.weighing_animal_video_mandatory)
            },
            color = MeshaColors.Muted,
            style = MeshaType.caption,
            lineHeight = 15.sp,
        )
        ActionButton(
            text = if (state.isShedPartition) {
                stringResource(R.string.weighing_add_shed_camera_clip)
            } else {
                stringResource(R.string.weighing_add_camera_clip)
            },
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
            SectionTitle(
                if (state.isShedPartition) {
                    stringResource(R.string.weighing_section_shed_result_caps)
                } else {
                    stringResource(R.string.weighing_section_animal_weight_caps)
                },
            )
            Text(
                text = if (state.isShedPartition) {
                    stringResource(R.string.weighing_record_total_and_video)
                } else {
                    state.selectedAnimalLabel ?: stringResource(R.string.weighing_scan_or_select_first)
                },
                color = MeshaColors.Ink,
                style = MeshaType.cardTitle,
            )
            OutlinedTextField(
                value = state.weightInput,
                onValueChange = onWeightChange,
                label = {
                    Text(
                        if (state.isShedPartition) {
                            stringResource(R.string.weighing_field_total_weight)
                        } else {
                            stringResource(R.string.weighing_field_weight_label)
                        },
                    )
                },
                suffix = { Text(stringResource(R.string.weighing_kg)) },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            Text(
                text = if (state.isShedPartition) {
                    stringResource(R.string.weighing_proof_opens_after_saving)
                } else {
                    stringResource(R.string.weighing_animal_proof_opens_after_saving)
                },
                color = MeshaColors.Muted,
                style = MeshaType.cardSubtitle,
            )
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                ActionButton(
                    text = stringResource(R.string.weighing_cancel),
                    enabled = true,
                    onClick = onDismiss,
                    modifier = Modifier.weight(1f),
                    primary = false,
                )
                ActionButton(
                    text = if (state.isShedPartition) {
                        stringResource(R.string.weighing_save_shed)
                    } else {
                        stringResource(R.string.weighing_save_kid)
                    },
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
    val total = if (state.isShedPartition) 1 else state.visibleRows.size
    val pending = (total - done).coerceAtLeast(0)
    val proofReady = state.individualDrafts.count { it.proofReady } + state.shedDrafts.count { it.proofReady }
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        CaptureMetricTile(
            value = done.toString(),
            label = if (state.isShedPartition) {
                stringResource(R.string.weighing_tab_result)
            } else {
                stringResource(R.string.weighing_tab_done)
            },
            selected = done > 0,
            tone = if (state.isShedPartition) MeshaColors.Purple else MeshaColors.BrandD,
            modifier = Modifier.weight(1f),
        )
        CaptureMetricTile(
            value = pending.toString(),
            label = stringResource(R.string.weighing_tab_pending),
            selected = pending == 0 && total > 0,
            tone = MeshaColors.Ink,
            modifier = Modifier.weight(1f),
        )
        CaptureMetricTile(
            value = proofReady.toString(),
            label = stringResource(R.string.weighing_tab_proof),
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
                // design-system:ignore: 9sp/W900 glyph badge — no token pairs a ~9sp size with W900 (dayName is 9.5sp/W700), so any swap would visibly lighten the badge.
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
                                    // design-system:ignore: 17sp/W700 placeholder sits between headerTitle (16.5sp) and screenTitle (22sp); headerTitle also carries -0.3sp tracking, so a human should decide whether this placeholder should shrink to the field's bodyStrong scale instead.
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
                    text = if (row.proofReady) {
                        stringResource(R.string.weighing_proof_ready)
                    } else {
                        stringResource(R.string.weighing_proof_required)
                    },
                    style = MeshaType.cardSubtitle,
                    color = MeshaColors.Muted,
                )
            }
            Text(
                text = if (row.readyToSubmit) {
                    stringResource(R.string.weighing_ready)
                } else {
                    stringResource(R.string.weighing_draft)
                },
                style = MeshaType.caption,
                color = if (row.readyToSubmit) MeshaColors.BrandD else MeshaColors.Muted,
            )
        }
    }
}

/**
 * In-app numeric keypad for the weight field.
 *
 * Renders inside the app's own window, so it cannot be torn down by the capture screen's window
 * transition the way the system IME is, and it is unaffected by the RFID reader pairing as a
 * Bluetooth HID keyboard. Digits, one decimal point, backspace.
 */
@Composable
private fun WeighingNumericKeypad(
    value: String,
    onValueChange: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    val rows = listOf(
        listOf("1", "2", "3"),
        listOf("4", "5", "6"),
        listOf("7", "8", "9"),
        listOf(".", "0", "\u232B"),
    )
    Column(
        modifier = modifier.fillMaxWidth(),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        rows.forEach { keyRow ->
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                keyRow.forEach { key ->
                    Box(
                        modifier = Modifier
                            .weight(1f)
                            .heightIn(min = 48.dp)
                            .clip(RoundedCornerShape(8.dp))
                            .background(MeshaColors.Surf)
                            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(8.dp))
                            .clickable {
                                val next = when (key) {
                                    "\u232B" -> value.dropLast(1)
                                    // Exactly one decimal point, and never as the first character.
                                    "." -> if (value.contains(".") || value.isEmpty()) value else value + "."
                                    else -> value + key
                                }
                                onValueChange(next)
                            },
                        contentAlignment = Alignment.Center,
                    ) {
                        Text(text = key, color = MeshaColors.Ink, style = MeshaType.pillStrong)
                    }
                }
            }
        }
    }
}
