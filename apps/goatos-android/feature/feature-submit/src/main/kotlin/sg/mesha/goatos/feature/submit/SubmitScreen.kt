package sg.mesha.goatos.feature.submit

// telemetry:exempt pure stateless renderer; SubmitViewModel owns submit/proof intents and the sync/outbox layer owns write lifecycle telemetry.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.res.stringResource
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType

// ---------------------------------------------------------------------------
// Submit (v-submit) — ONE shed record covering every due vaccine in a shed.
//
// TRD §14 dumb-renderer: this screen renders backend-provided data only. It
// never counts, never derives eligibility/grouping, never role-checks. Every
// visible label / count / status / action arrives as a field on SubmitUiState;
// a later ViewModel fills it from the app-api. The write lifecycle (draft →
// queued → syncing → acked / conflict / dead-letter) is likewise a backend-fed
// SyncState — the banner only paints what it is told.
// ---------------------------------------------------------------------------

/** Write-path lifecycle of the shed record in the Room outbox / sync engine. */
enum class SyncState { DRAFT, QUEUED, SYNCING, ACKED, CONFLICT, DEAD_LETTER }

/** Snackbar outcome message types for submit feedback. */
enum class SubmitSnackbarMessage {
    QUEUED,        // Submission queued for sync when online
    SUCCEEDED,     // Submission synced successfully
    CONFLICT,      // Server rejected the submission
    DEAD_LETTER,   // Submission failed after retries
}

/**
 * One due vaccine group inside the shed record. Proof is deliberately not attached to a vaccine
 * row: one camera clip can cover every vaccine administered to a goat in the same handling.
 */
data class VaccineGroup(
    val name: String,
    val given: Int,
    val due: Int,
    val dose: String,
)

/** One vaccine in the shed completion summary breakdown. */
data class VaccineSummaryItem(
    val vaccine: String,
    val count: Int,
)

/** One backend-composed, locale-aware fact in the task summary card. */
data class SubmitSummaryItem(val key: String, val label: String, val value: String)

/** Hoisted state for [SubmitScreen]. Every visible string is a field.
 *  @Immutable: groups, vaccines, summaryItems otherwise mark this unstable.
 */
@Immutable
data class SubmitUiState(
    val eyebrow: String,
    val title: String,
    val shed: String,
    val cohort: String,
    val date: String,
    val summaryItems: List<SubmitSummaryItem> = emptyList(),
    val dueSectionLabel: String,
    val groups: List<VaccineGroup>,
    val syncState: SyncState,
    val syncLabel: String,
    val submitLabel: String,
    val canSubmit: Boolean,
    /** 0f..1f, rendered as a bar while [syncState] is [SyncState.SYNCING]. */
    val syncProgress: Float = 0f,
    /** Room-first proof progress for this shed. In per-goat mode these are goat clips; in
     *  shed-level mode these are shed videos required by SOP. Finalize never uploads files. */
    val proofSummaryTitle: String = "",
    val proofSummarySyncedLabel: String = "",
    val proofSummaryFinalizeHint: String = "",
    val proofTotal: Int = 0,
    val proofSynced: Int = 0,
    val proofUploading: Int = 0,
    val proofFailed: Int = 0,
    /** Current retry attempt count (used in format strings for localization). */
    val attemptCount: Int = 0,
    /** Maximum retry attempts allowed (used in format strings for localization). */
    val maxAttempts: Int = 0,
    /** Backend-provided rejection reason, if any. When present, rendered verbatim; otherwise a localized fallback. */
    val lastError: String? = null,
    /** True when the initial task load is in progress (DRAFT state). */
    val isLoadingTask: Boolean = false,
    /**
     * Raised when the operator taps Finalize: submitting hands the shed to the video check and
     * the operator cannot reopen it himself, so he is asked once before it goes.
     */
    val showSubmitConfirmation: Boolean = false,
    /** True when the task was not found / no task assigned to this operator (DRAFT state). */
    val isNoTaskAssigned: Boolean = false,
    /** True when the task load failed (DEAD_LETTER state). */
    val isTaskLoadFailed: Boolean = false,
    /** True when enqueueing to the sync queue failed (DEAD_LETTER state). */
    val isQueueFailed: Boolean = false,
    /** True when a retry request failed (DEAD_LETTER state). */
    val isRetryFailed: Boolean = false,
    /** MOB-002 role gate: true when the signed-in principal is not a ground operator (for
     *  example, a verifier or leadership-only principal). Only operators capture and submit
     *  (docs/mobile/proof-capture-sync-and-e2e.md §5). */
    val isCaptureRoleBlocked: Boolean = false,
    /** MOB-002: the SOP `form_dsl` recording form for this task, rendered inline below the
     *  shed-record summary. Null when the task's form has no fields to capture — the shed
     *  record is then just the vaccine-group summary, as before. */
    val formRunner: FormRunnerState? = null,
    /** Shed completion summary from backend: human shed name, drive name, counts. */
    val shedCompletionSummary: ShedCompletionSummary? = null,
    /** Vaccine breakdown from shed completion summary. */
    val vaccineBreakdown: List<VaccineSummaryItem> = emptyList(),
    /** Human-readable blocking reason when submit not enabled. */
    val blockingReason: String? = null,
    /** Snackbar message type for submit outcomes (success, failure, offline). Null when no snackbar should be shown. */
    val snackbarMessage: SubmitSnackbarMessage? = null,
)

/** Shed completion summary data class (mirror of backend ShedCompletionSummaryDto). */
data class ShedCompletionSummary(
    val taskId: String,
    val shedName: String,
    val driveName: String,
    val expectedCount: Int,
    val handledCount: Int,
    val proofReadyCount: Int,
    val proofMode: String,
    val submitState: String,
)

/** User intents. The ViewModel layer maps these to sync-engine commands. */
sealed interface SubmitEvent {
    data object Submit : SubmitEvent
    /** Operator answered the "are you sure" gate raised by [SubmitUiState.showSubmitConfirmation]. */
    data object ConfirmSubmit : SubmitEvent
    data object DismissSubmitConfirmation : SubmitEvent
    data object Retry : SubmitEvent
    /** Operator answered a boolean recording-form field (e.g. cold-chain verified). */
    data class FormToggle(val key: String, val checked: Boolean) : SubmitEvent
    /** Operator typed a text/number recording-form field. */
    data class FormText(val key: String, val value: String) : SubmitEvent
    /** Operator picked an option for a picker recording-form field (option VALUE, not label). */
    data class FormPick(val key: String, val value: String) : SubmitEvent
    /** Tapped a `goat_scan` field's scan zone — starts capture if idle, stops if scanning
     *  (MOB-002, docs/mobile/proof-capture-sync-and-e2e.md §1). */
    data class ScanToggled(val key: String) : SubmitEvent
    /** Tapped a `video_proof` field's proof box — launches one video capture (MOB-002 §2). */
    data class CaptureVideoRequested(val key: String, val source: String = "in_app_camera") : SubmitEvent
    /** Edited the caption of an operator-added EXTRA proof video. */
    data class ProofCaptionChanged(val key: String, val proofId: String, val caption: String) : SubmitEvent
    /** Removed an operator-added EXTRA proof video (named/mandatory subjects cannot be removed). */
    data class ProofRemoved(val key: String, val proofId: String) : SubmitEvent
    /** Re-armed a terminal failed proof upload without changing the captured file. */
    data class ProofRetryRequested(val key: String, val proofId: String) : SubmitEvent
}

// --- Goat OS dark tokens (exact values from docs/mobile/design-system.md). ---
// The shared theme currently exposes only M3 slots; these are the mock's field
// palette used for the status banner / pills / bars until core-designsystem
// surfaces them as GoatOsTokens.
private object T {
    val bg = MeshaColors.Bg
    val surf = MeshaColors.Surf
    val surf2 = MeshaColors.Surf2
    val surf3 = MeshaColors.Surf3
    val ink = MeshaColors.Ink
    val muted = MeshaColors.Muted
    val faint = MeshaColors.Faint
    val hair = MeshaColors.Hair
    val brand = MeshaColors.Brand
    val brandD = MeshaColors.BrandD
    val danger = MeshaColors.Danger
    val dangerX = MeshaColors.DangerX // rgba(251,111,99,.15)
    val warn = MeshaColors.Warn
    val warnX = MeshaColors.WarnX // rgba(240,181,75,.15)
    val ok = MeshaColors.Ok
    val okX = MeshaColors.OkX // rgba(138,212,87,.16)
}

private data class BannerTone(val fg: Color, val bg: Color)

private fun SyncState.tone(): BannerTone = when (this) {
    SyncState.ACKED -> BannerTone(T.ok, T.okX)
    SyncState.SYNCING -> BannerTone(T.warn, T.warnX)
    SyncState.CONFLICT, SyncState.DEAD_LETTER -> BannerTone(T.danger, T.dangerX)
    SyncState.DRAFT, SyncState.QUEUED -> BannerTone(T.muted, T.surf2)
}

/** Resolves snackbar message enum to localized string resource ID. */
@Composable
private fun snackbarMessageStringFor(message: SubmitSnackbarMessage?): String? = when (message) {
    SubmitSnackbarMessage.QUEUED -> stringResource(R.string.submit_snackbar_queued)
    SubmitSnackbarMessage.SUCCEEDED -> stringResource(R.string.submit_snackbar_succeeded)
    SubmitSnackbarMessage.CONFLICT -> stringResource(R.string.submit_snackbar_conflict)
    SubmitSnackbarMessage.DEAD_LETTER -> stringResource(R.string.submit_snackbar_dead_letter)
    null -> null
}

/** Localized label for the sync state banner. Renders based on SyncState + ancillary state. */
@Composable
private fun syncLabelFor(state: SubmitUiState): String = when {
    state.isLoadingTask ->
        stringResource(R.string.submit_sync_loading)
    state.isCaptureRoleBlocked ->
        stringResource(R.string.submit_sync_role_blocked)
    state.syncState == SyncState.DRAFT && state.canSubmit ->
        stringResource(R.string.submit_sync_ready)
    state.isNoTaskAssigned ->
        stringResource(R.string.submit_sync_no_task)
    state.isTaskLoadFailed ->
        stringResource(R.string.submit_sync_load_failed)
    state.isQueueFailed ->
        stringResource(R.string.submit_sync_queue_failed)
    state.isRetryFailed ->
        stringResource(R.string.submit_sync_retry_failed)
    state.syncState == SyncState.QUEUED ->
        stringResource(R.string.submit_sync_queued)
    state.syncState == SyncState.SYNCING && state.attemptCount > 0 ->
        stringResource(R.string.submit_sync_retrying, state.attemptCount, state.maxAttempts)
    state.syncState == SyncState.SYNCING ->
        stringResource(R.string.submit_sync_syncing)
    state.syncState == SyncState.ACKED ->
        stringResource(R.string.submit_sync_acked)
    state.syncState == SyncState.CONFLICT ->
        state.lastError ?: stringResource(R.string.submit_sync_conflict)
    state.syncState == SyncState.DEAD_LETTER ->
        stringResource(R.string.submit_sync_dead_letter, state.attemptCount)
    else -> ""
}

@Composable
fun SubmitScreen(
    state: SubmitUiState,
    onEvent: (SubmitEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    // Asked once, before the shed leaves the operator's hands. Rendered here rather than at the
    // footer so it covers every path that raises it, and dismissing it changes nothing.
    if (state.showSubmitConfirmation) {
        SubmitConfirmGate(
            title = stringResource(R.string.submit_confirm_title),
            body = stringResource(R.string.submit_confirm_body),
            cancelLabel = stringResource(R.string.submit_confirm_cancel),
            confirmLabel = stringResource(R.string.submit_confirm_confirm),
            onConfirm = { onEvent(SubmitEvent.ConfirmSubmit) },
            onDismiss = { onEvent(SubmitEvent.DismissSubmitConfirmation) },
        )
    }
    val proofFields = state.formRunner?.fields.orEmpty().filter { it.kind == FieldKindUi.VIDEO_PROOF }
    val recordingFields = state.formRunner?.fields.orEmpty()
        .filterNot { it.kind == FieldKindUi.VIDEO_PROOF }
        .filterNot { state.shedCompletionSummary != null && it.kind == FieldKindUi.GOAT_SCAN }
    Column(
        modifier = modifier
            .fillMaxSize()
            .background(T.bg),
    ) {
        SubmitHeader(state)
        SyncBanner(state)
        SubmitSnackbar(state)

        LazyColumn(
            modifier = Modifier
                .weight(1f)
                .fillMaxWidth()
                .imePadding(),
            contentPadding = androidx.compose.foundation.layout.PaddingValues(
                start = 16.dp, end = 16.dp, top = 8.dp, bottom = 12.dp,
            ),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            // Vaccination shed completion summary (READ-ONLY)
            state.shedCompletionSummary?.let { summary ->
                item { ShedCompletionSummaryCard(summary) }
                if (state.vaccineBreakdown.isNotEmpty()) {
                    item {
                        Text(
                            text = stringResource(R.string.submit_vaccine_breakdown_label),
                            color = T.faint,
                            style = MeshaType.cardSubtitle,
                            modifier = Modifier.padding(top = 6.dp, bottom = 2.dp),
                        )
                    }
                }
                items(state.vaccineBreakdown, key = { "${it.vaccine}|${it.count}" }, contentType = { "vaccine_breakdown" }) { item ->
                    VaccineBreakdownRow(item)
                }
            }
            if (state.summaryItems.any { it.label.isNotBlank() && it.value.isNotBlank() } ||
                state.shed.isNotBlank() || state.cohort.isNotBlank() || state.date.isNotBlank()
            ) {
                item { RecordSummary(state) }
            }
            if (proofFields.isEmpty() && state.proofTotal > 0 && state.proofSummaryTitle.isNotBlank()) {
                item {
                    ProofSummary(state)
                }
            }
            state.formRunner?.let { runner ->
                if (proofFields.isNotEmpty()) {
                    item {
                        Text(
                            text = state.proofSummaryTitle.ifBlank { stringResource(R.string.submit_summary_proof_ready) },
                            color = T.faint,
                            style = MeshaType.cardSubtitle,
                            modifier = Modifier.padding(top = 6.dp, bottom = 2.dp),
                        )
                    }
                    item {
                        FormFieldsColumn(
                            fields = proofFields,
                            onToggle = { key, checked -> onEvent(SubmitEvent.FormToggle(key, checked)) },
                            onText = { key, value -> onEvent(SubmitEvent.FormText(key, value)) },
                            onScan = { key -> onEvent(SubmitEvent.ScanToggled(key)) },
                            onPick = { key, value -> onEvent(SubmitEvent.FormPick(key, value)) },
                            onCaptureVideo = { key, source -> onEvent(SubmitEvent.CaptureVideoRequested(key, source)) },
                            onCaption = { key, proofId, caption -> onEvent(SubmitEvent.ProofCaptionChanged(key, proofId, caption)) },
                            onRemoveProof = { key, proofId -> onEvent(SubmitEvent.ProofRemoved(key, proofId)) },
                            onRetryProof = { key, proofId -> onEvent(SubmitEvent.ProofRetryRequested(key, proofId)) },
                        )
                    }
                    state.blockingReason?.let { reason ->
                        item {
                            Text(
                                text = reason,
                                color = T.warn,
                                style = MeshaType.caption,
                                modifier = Modifier.padding(vertical = 4.dp),
                            )
                        }
                    }
                }
                if (recordingFields.isNotEmpty()) {
                    item {
                        Text(
                            text = runner.title,
                            color = T.faint,
                            style = MeshaType.cardSubtitle,
                            modifier = Modifier.padding(top = 6.dp, bottom = 2.dp),
                        )
                    }
                    item {
                        FormFieldsColumn(
                            fields = recordingFields,
                            onToggle = { key, checked -> onEvent(SubmitEvent.FormToggle(key, checked)) },
                            onText = { key, value -> onEvent(SubmitEvent.FormText(key, value)) },
                            onScan = { key -> onEvent(SubmitEvent.ScanToggled(key)) },
                            onPick = { key, value -> onEvent(SubmitEvent.FormPick(key, value)) },
                            onCaptureVideo = { key, source -> onEvent(SubmitEvent.CaptureVideoRequested(key, source)) },
                            onCaption = { key, proofId, caption -> onEvent(SubmitEvent.ProofCaptionChanged(key, proofId, caption)) },
                            onRemoveProof = { key, proofId -> onEvent(SubmitEvent.ProofRemoved(key, proofId)) },
                            onRetryProof = { key, proofId -> onEvent(SubmitEvent.ProofRetryRequested(key, proofId)) },
                        )
                    }
                }
                runner.blockedReason
                    ?.takeUnless { it.trim() == state.blockingReason?.trim() }
                    ?.let { reason ->
                        item {
                            Text(
                                text = reason,
                                color = T.warn,
                                style = MeshaType.caption,
                                modifier = Modifier.padding(vertical = 4.dp),
                            )
                        }
                    }
            }
            if (state.groups.isNotEmpty() && state.dueSectionLabel.isNotBlank()) {
                item {
                    Text(
                        text = state.dueSectionLabel,
                        color = T.faint,
                        style = MeshaType.cardSubtitle,
                        modifier = Modifier.padding(top = 6.dp, bottom = 2.dp),
                    )
                }
            }
            // MOB-011: Use stable keys for vaccine groups to avoid recomposition on insert/reorder
            items(state.groups, key = { group -> "${group.name}|${group.dose}" }, contentType = { "vaccine_group" }) { group ->
                VaccineGroupCard(group)
            }
            item(contentType = "submit_footer") {
                SubmitFooter(state, onEvent)
            }
        }
    }
}

@Composable
private fun SubmitHeader(state: SubmitUiState) {
    val title = when (state.syncState) {
        SyncState.ACKED -> stringResource(R.string.submit_record_submitted_title)
        else -> state.shedCompletionSummary?.shedName?.takeIf { it.isNotBlank() }
            ?: state.title.ifBlank { stringResource(R.string.submit_record_title) }
    }
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(T.surf)
            .padding(horizontal = 16.dp, vertical = 12.dp),
    ) {
        Text(
            state.eyebrow.ifBlank { stringResource(R.string.submit_eyebrow) },
            color = T.brandD,
            style = MeshaType.caption,
        )
        Spacer(Modifier.height(2.dp))
        Text(
            title,
            color = T.ink,
            style = MeshaType.screenTitle,
        )
    }
}

@Composable
private fun SyncBanner(state: SubmitUiState) {
    val tone = state.syncState.tone()
    val label = syncLabelFor(state)
    // An unanswered DRAFT form has no sync lifecycle to announce yet. Rendering the banner shell
    // with an empty label leaves a large blank strip and a lone status dot above the form.
    if (label.isBlank()) return
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(tone.bg)
            .padding(horizontal = 16.dp, vertical = 10.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(
                modifier = Modifier
                    .size(8.dp)
                    .background(tone.fg, RoundedCornerShape(50)),
            )
            Spacer(Modifier.width(8.dp))
            Text(
                text = label,
                color = tone.fg,
                style = MeshaType.pillStrong,
            )
        }
        if (state.syncState == SyncState.SYNCING) {
            Spacer(Modifier.height(8.dp))
            ProgressBar(fraction = state.syncProgress, color = tone.fg)
        }
    }
}

/** Displays a snackbar notification for submit outcomes (success, failure, offline queued). */
@Composable
private fun SubmitSnackbar(state: SubmitUiState) {
    val message = snackbarMessageStringFor(state.snackbarMessage)
    if (message == null) return
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(T.surf3)
            .padding(horizontal = 16.dp, vertical = 12.dp),
        verticalArrangement = Arrangement.Center,
    ) {
        Text(
            text = message,
            color = T.ink,
            style = MeshaType.bodyStrong,
            lineHeight = 18.sp,
        )
    }
}

@Composable
private fun RecordSummary(state: SubmitUiState) {
    val items = state.summaryItems
        .filter { it.label.isNotBlank() && it.value.isNotBlank() }
        .ifEmpty {
            listOf(
                SubmitSummaryItem("shed", stringResource(R.string.submit_summary_shed), state.shed),
                SubmitSummaryItem("cohort", stringResource(R.string.submit_summary_cohort), state.cohort),
                SubmitSummaryItem("date", stringResource(R.string.submit_summary_date), state.date),
            ).filter { it.value.isNotBlank() }
        }
    GoatCard {
        items.forEachIndexed { index, item ->
            SummaryRow(item.label, item.value)
            if (index != items.lastIndex) HairLine()
        }
    }
}

@Composable
private fun SummaryRow(key: String, value: String) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(key, color = T.muted, style = MeshaType.listTitle)
        Spacer(Modifier.width(16.dp))
        Text(
            value,
            color = T.ink,
            style = MeshaType.listTitle,
            lineHeight = 17.sp,
            textAlign = androidx.compose.ui.text.style.TextAlign.End,
            modifier = Modifier.weight(1f),
        )
    }
}

@Composable
private fun VaccineGroupCard(group: VaccineGroup) {
    GoatCard {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(modifier = Modifier.weight(1f)) {
                Text(group.name, color = T.ink, style = MeshaType.cardTitle)
                Spacer(Modifier.height(2.dp))
                Text(stringResource(R.string.submit_dose_label) + " · ${group.dose}", color = T.muted, style = MeshaType.cardSubtitle)
            }
            Text(
                text = "${group.given} / ${group.due}",
                color = if (group.given >= group.due) T.brandD else T.warn,
                style = MeshaType.cardTitle,
                fontFamily = FontFamily.Monospace,
            )
        }
        Spacer(Modifier.height(10.dp))
        ProgressBar(
            fraction = if (group.due > 0) group.given.toFloat() / group.due else 0f,
            color = T.brand,
        )
    }
}

@Composable
private fun ProofSummary(state: SubmitUiState) {
    val complete = state.proofSynced >= state.proofTotal && state.proofFailed == 0
    val statusColor = when {
        state.proofFailed > 0 -> T.danger
        complete -> T.brandD
        else -> T.warn
    }
    GoatCard {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(
                modifier = Modifier
                    .size(34.dp)
                    .background(if (complete) T.okX else T.warnX, RoundedCornerShape(10.dp)),
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    imageVector = MeshaIcons.Video,
                    contentDescription = null,
                    tint = statusColor,
                    modifier = Modifier.size(17.dp),
                )
            }
            Spacer(Modifier.width(10.dp))
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = state.proofSummaryTitle,
                    color = T.ink,
                    style = MeshaType.bodyStrong,
                )
                Text(
                    text = state.proofSummarySyncedLabel,
                    color = statusColor,
                    style = MeshaType.caption,
                )
            }
        }
        if (state.proofUploading > 0 || state.proofFailed > 0) {
            Spacer(Modifier.height(8.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                if (state.proofUploading > 0) {
                    Text(
                        stringResource(R.string.submit_goat_proof_uploading, state.proofUploading),
                        color = T.warn,
                        style = MeshaType.pill,
                    )
                }
                if (state.proofFailed > 0) {
                    Text(
                        stringResource(R.string.submit_goat_proof_failed, state.proofFailed),
                        color = T.danger,
                        style = MeshaType.pill,
                    )
                }
            }
        }
        if (complete) {
            Spacer(Modifier.height(8.dp))
            Text(
                text = state.proofSummaryFinalizeHint,
                color = T.muted,
                style = MeshaType.pill,
            )
        }
    }
}

@Composable
private fun SubmitFooter(state: SubmitUiState, onEvent: (SubmitEvent) -> Unit) {
    val needsRetry = state.syncState == SyncState.CONFLICT || state.syncState == SyncState.DEAD_LETTER
    val submitLabel = when (state.syncState) {
        SyncState.ACKED -> stringResource(R.string.submit_submitted_label)
        SyncState.QUEUED, SyncState.SYNCING -> stringResource(R.string.submit_submitting_label)
        else -> state.submitLabel
    }
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(T.surf)
            .padding(16.dp),
    ) {
        Button(
            onClick = { onEvent(SubmitEvent.Submit) },
            enabled = state.canSubmit,
            modifier = Modifier
                .fillMaxWidth()
                .height(48.dp),
            shape = RoundedCornerShape(12.dp),
            colors = ButtonDefaults.buttonColors(
                containerColor = T.brand,
                contentColor = MeshaColors.OnBrand,
                disabledContainerColor = T.surf3,
                disabledContentColor = T.faint,
            ),
        ) {
            Text(submitLabel, style = MeshaType.button)
        }
        if (needsRetry) {
            Spacer(Modifier.height(10.dp))
            OutlinedButton(
                onClick = { onEvent(SubmitEvent.Retry) },
                modifier = Modifier
                    .fillMaxWidth()
                    .height(44.dp),
                shape = RoundedCornerShape(12.dp),
                colors = ButtonDefaults.outlinedButtonColors(contentColor = T.danger),
            ) {
                Text(stringResource(R.string.submit_retry_label), style = MeshaType.button)
            }
        }
    }
}

@Composable
private fun ShedCompletionSummaryCard(summary: ShedCompletionSummary) {
    GoatCard {
        // Shed name
        SummaryRow(stringResource(R.string.submit_summary_shed), summary.shedName)
        HairLine()
        // Drive name
        SummaryRow(stringResource(R.string.submit_summary_drive), summary.driveName)
        HairLine()
        // Expected animals
        SummaryRow(stringResource(R.string.submit_summary_expected), summary.expectedCount.toString())
        HairLine()
        // Handled (scanned) animals
        SummaryRow(stringResource(R.string.submit_summary_handled), summary.handledCount.toString())
        HairLine()
        val proofReadyLabel = if (summary.proofMode == "shed_level_video") {
            stringResource(R.string.submit_summary_shed_videos_ready)
        } else {
            stringResource(R.string.submit_summary_proof_ready)
        }
        SummaryRow(proofReadyLabel, summary.proofReadyCount.toString())
    }
}

@Composable
private fun VaccineBreakdownRow(item: VaccineSummaryItem) {
    GoatCard {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(item.vaccine, color = T.ink, style = MeshaType.bodyStrong)
            Spacer(Modifier.width(16.dp))
            Text(
                text = "${item.count}",
                color = T.brandD,
                style = MeshaType.bodyStrong,
                fontFamily = FontFamily.Monospace,
                modifier = Modifier.weight(1f),
                textAlign = androidx.compose.ui.text.style.TextAlign.End,
            )
        }
    }
}

// --- small private primitives (mock .card / .barp / hairline) ---------------

@Composable
private fun GoatCard(content: @Composable androidx.compose.foundation.layout.ColumnScope.() -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(T.surf, RoundedCornerShape(18.dp))
            .border(1.dp, T.hair, RoundedCornerShape(18.dp))
            .padding(horizontal = 16.dp, vertical = 6.dp),
        content = content,
    )
}

@Composable
private fun HairLine() {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .height(1.dp)
            .background(T.hair),
    )
}

@Composable
private fun ProgressBar(fraction: Float, color: Color) {
    val clamped = fraction.coerceIn(0f, 1f)
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .height(6.dp)
            .background(T.surf3, RoundedCornerShape(999.dp)),
    ) {
        Box(
            modifier = Modifier
                .fillMaxWidth(clamped)
                .height(6.dp)
                .background(color, RoundedCornerShape(999.dp)),
        )
    }
}

// ---------------------------------------------------------------------------
// Preview — fake backend-shaped state with goat-level proof sync + a SYNCING banner.
// ---------------------------------------------------------------------------

private val previewState = SubmitUiState(
    eyebrow = "Vaccination · Gandhi 1",
    title = "Submit shed record",
    shed = "Gandhi 1",
    cohort = "Adult does · 50 in shed",
    date = "Thu, 9 Jul 2026",
    dueSectionLabel = "Due in this shed",
    groups = listOf(
        VaccineGroup(
            name = "FMD",
            given = 50,
            due = 50,
            dose = "2 ml S/C",
        ),
        VaccineGroup(
            name = "HS",
            given = 48,
            due = 50,
            dose = "2 ml S/C",
        ),
        VaccineGroup(
            name = "PPR · Booster",
            given = 50,
            due = 50,
            dose = "1 ml S/C",
        ),
    ),
    syncState = SyncState.SYNCING,
    syncLabel = "",
    submitLabel = "Submit shed record",
    canSubmit = true,
    syncProgress = 0.66f,
    proofSummaryTitle = "Goat camera proof",
    proofSummarySyncedLabel = "48 of 50 goats synced",
    proofSummaryFinalizeHint = "Finalize checks the synced records. It does not upload files.",
    proofTotal = 50,
    proofSynced = 48,
    proofUploading = 2,
    attemptCount = 0,
    maxAttempts = 0,
    lastError = null,
    isLoadingTask = false,
    isNoTaskAssigned = false,
    isTaskLoadFailed = false,
    isQueueFailed = false,
    isRetryFailed = false,
)

@Preview(name = "Submit — syncing", showBackground = true, backgroundColor = 0xFF0B100D)
@Composable
private fun SubmitScreenPreview() {
    GoatOsTheme {
        SubmitScreen(state = previewState, onEvent = {})
    }
}

@Preview(name = "Submit — conflict", showBackground = true, backgroundColor = 0xFF0B100D)
@Composable
private fun SubmitScreenConflictPreview() {
    GoatOsTheme {
        SubmitScreen(
            state = previewState.copy(
                syncState = SyncState.CONFLICT,
                syncLabel = "",
                canSubmit = false,
                lastError = null,
            ),
            onEvent = {},
        )
    }
}


/**
 * The "are you sure" gate before a shed is finalized.
 *
 * Deliberately plain: it collects no input, it just makes the operator pause. The confirm action
 * sits on the right and the safe one on the left, matching the reject-with-reason dialog the
 * verifier already uses, so the muscle memory is the same across the app.
 *
 * The copy tells the truth about who can undo it: the operator cannot reopen his own shed, but a
 * manager or director can — saying "cannot be undone" would be a lie the first time leadership
 * reopens one.
 */
@Composable
private fun SubmitConfirmGate(
    title: String,
    body: String,
    cancelLabel: String,
    confirmLabel: String,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        containerColor = T.surf2,
        title = { Text(text = title, color = T.ink, style = MeshaType.cardTitle) },
        text = { Text(text = body, color = T.muted, style = MeshaType.cardSubtitle) },
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text(text = cancelLabel, color = T.muted, style = MeshaType.button)
            }
        },
        confirmButton = {
            TextButton(onClick = onConfirm) {
                Text(text = confirmLabel, color = T.brand, style = MeshaType.button)
            }
        },
    )
}
