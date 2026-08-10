package sg.mesha.goatos.feature.counts

// telemetry:exempt pure stateless renderer; WorkflowDetailViewModel (in :app) owns the
// workflow_* AnalyticsEvents + the CrashReporter non-fatal on every refresh/enqueue failure.

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton

/**
 * The drill-in for one Birth/Death workflow (`/counts/{birth,death}/workflows/{workflow_id}` —
 * docs/decisions/birth-death-workflows.md, mock/birth-death-mobile-mock.html): a context card
 * (avatar, id + role, template label, facts grid, n/N progress), then the action rows in
 * Overdue / Scheduled / Completed sections. Birth-time-derived colostrum rounds are normal
 * operator tasks in those sections, with the same video-proof controls as every other kid task.
 * Internal approval and verification are deliberately absent from this operator screen. Questions
 * answer inline (Yes/No, numeric kilograms, or backend-owned question_select options);
 * `requires_video` actions capture through the shared proof pipeline;
 * `tag_the_kid` opens Birth's RFID assignment until a permanent identifier is recorded, then keeps
 * only its required tagging-video control.
 *
 * All copy — titles, details, tags, facts, status labels — is backend-owned and rendered verbatim.
 */

/** Which section an action row renders under. VM-derived from backend fields. */
enum class WorkflowActionSection { OVERDUE, SCHEDULED, COMPLETED }

/** One action row. Every display string is backend copy; flags say which controls render. */
@Immutable
data class WorkflowActionUi(
    val actionId: String,
    val actionKey: String,
    val title: String,
    val detail: String,
    /** Human type tag ("Question" / "Pick a value" / "Do & confirm" / "Approval"). */
    val typeLabel: String,
    /** Type glyph rendered next to the title (?, ≡, ▣, ⚖, ✓ when done). */
    val glyph: String,
    val requiresVideo: Boolean,
    /**
     * Inline answer choices. For question_select these are the backend's own bands (value ==
     * label, verbatim); for a plain question the VM supplies the yes/no pair. [WorkflowAnswerOptionUi.value]
     * is what submits; [WorkflowAnswerOptionUi.label] is what renders.
     */
    val options: List<WorkflowAnswerOptionUi>,
    /** Non-null renders one numeric answer field with this unit instead of answer-choice buttons. */
    val numericAnswerUnit: String? = null,
    /** Status chip copy + tone ("1h late" / "Scheduled" / "Done" / "Blocked" / "In review"). */
    val statusLabel: String,
    val statusTone: WorkflowStatusTone,
    val section: WorkflowActionSection,
    /** True renders the inline Yes/No answer buttons. */
    val canAnswer: Boolean,
    /** True renders the "Mark done" confirm control (non-video action type). */
    val canComplete: Boolean,
    /** True renders the "Record video" button. */
    val canRecordVideo: Boolean,
    /** True also exposes the promote flow; video completion remains a separate required control. */
    val opensPromote: Boolean,
    /** Footer line with backend-owned completion attribution; blank hides it. */
    val footer: String,
    val answerValue: String?,
    val hasVideoDraft: Boolean = false,
)

enum class WorkflowStatusTone { OVERDUE, SCHEDULED, DONE, BLOCKED, IN_REVIEW }

/** One inline answer choice: [value] submits, [label] renders. */
@Immutable
data class WorkflowAnswerOptionUi(val value: String, val label: String)

/** Copy state for the death evidence acknowledgment; uploading action 2 is the real submission. */
enum class WorkflowDeathSubmissionLabel { SUBMIT, UPLOADING, UPLOAD_FAILED, SUBMITTED }

@Immutable
data class WorkflowDetailUiState(
    val workflowId: String = "",
    val loading: Boolean = true,
    val notFound: Boolean = false,
    val isDeath: Boolean = false,
    val displayId: String = "",
    val roleLabel: String = "",
    /** "Birth · WF-0521"-style template line, VM-built from backend fields. */
    val templateLine: String = "",
    val facts: List<Pair<String, String>> = emptyList(),
    /**
     * Operator progress. On Death this counts a pre-submit LOCAL draft as done: the operator has
     * recorded that video and nothing more is asked of them for that row until Submit. It is
     * therefore NOT backend truth and must never gate submission — see [deathBackendActionsDone].
     */
    val actionsDone: Int = 0,
    val actionsTotal: Int = 0,
    /**
     * Backend-confirmed finished actions, the only count the death submission gate may read.
     * With two local drafts and nothing uploaded, [actionsDone] is already 2 while this is still
     * 0 — and that is exactly the moment Submit has to be tappable.
     */
    val deathBackendActionsDone: Int = 0,
    val actions: List<WorkflowActionUi> = emptyList(),
    val isRefreshing: Boolean = false,
    val isCapturingVideo: Boolean = false,
    val isSubmittingDeath: Boolean = false,
    val deathDraftCount: Int = 0,
    val deathDraftsSubmitting: Boolean = false,
    val deathUploadFailed: Boolean = false,
    /** Transient action-write feedback (queued offline / failure), rendered verbatim. */
    val message: String? = null,
    val isErrorMessage: Boolean = false,
    /** The subject goat id, threaded to the promote route by `tag_the_kid`. */
    val subjectGoatId: String = "",
    val subjectGoatRowVersion: Int = 0,
    val subjectTemporaryIdentifier: String = "",
    val subjectLocationDisplay: String = "",
) {
    val showDeathSubmissionButton: Boolean get() = isDeath
    val deathSubmissionLabel: WorkflowDeathSubmissionLabel
        get() = when {
            actionsTotal > 0 && deathBackendActionsDone >= actionsTotal -> WorkflowDeathSubmissionLabel.SUBMITTED
            deathUploadFailed -> WorkflowDeathSubmissionLabel.UPLOAD_FAILED
            deathDraftsSubmitting || isSubmittingDeath -> WorkflowDeathSubmissionLabel.UPLOADING
            else -> WorkflowDeathSubmissionLabel.SUBMIT
        }

    val deathSubmissionEnabled: Boolean
        get() = deathSubmissionLabel == WorkflowDeathSubmissionLabel.SUBMIT &&
            actionsTotal == 2 && deathDraftCount == 2 && !isCapturingVideo && !isSubmittingDeath
}

sealed interface WorkflowDetailEvent {
    data object Back : WorkflowDetailEvent
    data object Refresh : WorkflowDetailEvent
    data class Answer(val actionId: String, val value: String) : WorkflowDetailEvent
    data class Complete(val actionId: String) : WorkflowDetailEvent
    data class RecordVideo(val actionId: String) : WorkflowDetailEvent
    data object SubmitDeath : WorkflowDetailEvent

    /** `tag_the_kid` — open the Birth-owned permanent RFID assignment for this canonical kid. */
    data class OpenPromote(
        val goatId: String,
        val displayId: String,
        val temporaryIdentifier: String,
        val locationDisplay: String,
        val rowVersion: Int,
    ) : WorkflowDetailEvent
}

@Composable
fun WorkflowDetailScreen(
    state: WorkflowDetailUiState,
    onEvent: (WorkflowDetailEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(WorkflowDetailEvent.Refresh) }

    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = state.displayId.ifBlank { stringResource(R.string.counts_workflow_detail_title) },
            subtitle = listOf(state.roleLabel, state.templateLine).filter { it.isNotBlank() }
                .joinToString(" · ")
                .ifBlank { null },
            onBack = { onEvent(WorkflowDetailEvent.Back) },
            actions = {
                SyncIconButton(
                    isSyncing = state.isRefreshing,
                    onSync = { onEvent(WorkflowDetailEvent.Refresh) },
                )
            },
        )
        if (state.notFound) {
            EmptyState(
                title = stringResource(R.string.counts_workflow_not_found),
                modifier = Modifier.fillMaxWidth().padding(16.dp),
                icon = MeshaIcons.Warn,
                tone = EmptyTone.Warn,
            )
            return@Column
        }
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "context") { WorkflowContextCard(state) }
            state.message?.let { message ->
                item(key = "message") {
                    Text(
                        text = message,
                        color = if (state.isErrorMessage) MeshaColors.Danger else MeshaColors.BrandD,
                        fontSize = 12.sp,
                        fontWeight = FontWeight.W600,
                    )
                }
            }

            renderSection(state, WorkflowActionSection.OVERDUE, R.string.counts_workflow_sect_overdue, onEvent)
            renderSection(state, WorkflowActionSection.SCHEDULED, R.string.counts_workflow_sect_scheduled, onEvent)

            renderSection(state, WorkflowActionSection.COMPLETED, R.string.counts_workflow_sect_completed, onEvent)
            if (state.showDeathSubmissionButton) {
                item(key = "death-submission") {
                    CountsSubmitButton(
                        label = when (state.deathSubmissionLabel) {
                            WorkflowDeathSubmissionLabel.SUBMITTED -> stringResource(R.string.counts_workflow_submitted)
                            WorkflowDeathSubmissionLabel.UPLOADING -> "Uploading…"
                            WorkflowDeathSubmissionLabel.UPLOAD_FAILED -> "Upload failed · open Sync status"
                            WorkflowDeathSubmissionLabel.SUBMIT -> stringResource(R.string.counts_workflow_submit)
                        },
                        enabled = state.deathSubmissionEnabled,
                        onClick = { onEvent(WorkflowDetailEvent.SubmitDeath) },
                    )
                }
            }
        }
    }
}

private fun androidx.compose.foundation.lazy.LazyListScope.renderSection(
    state: WorkflowDetailUiState,
    section: WorkflowActionSection,
    titleRes: Int,
    onEvent: (WorkflowDetailEvent) -> Unit,
) {
    val rows = state.actions.filter { it.section == section }
    if (rows.isEmpty()) return
    item(key = "sect-$section") {
        WorkflowSectionTitle(
            title = stringResource(titleRes),
            tone = if (section == WorkflowActionSection.OVERDUE) {
                if (state.isDeath) MeshaColors.Danger else MeshaColors.Warn
            } else {
                MeshaColors.Faint
            },
        )
    }
    rows.forEach { action ->
        item(key = "action-${action.actionId}") {
            WorkflowActionRow(action, state, onEvent)
        }
    }
}

@Composable
private fun WorkflowSectionTitle(title: String, tone: Color = MeshaColors.Faint) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(top = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(text = title, color = tone, fontSize = 11.sp, fontWeight = FontWeight.W800)
        Box(modifier = Modifier.weight(1f).height(1.dp).background(MeshaColors.Hair))
    }
}

@Composable
private fun WorkflowContextCard(state: WorkflowDetailUiState) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            Box(
                modifier = Modifier
                    .size(38.dp)
                    .clip(CircleShape)
                    .background(if (state.isDeath) MeshaColors.DangerX else MeshaColors.BrandTint),
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    imageVector = if (state.isDeath) MeshaIcons.Warn else MeshaIcons.Goat,
                    contentDescription = null,
                    tint = if (state.isDeath) MeshaColors.Danger else MeshaColors.BrandD,
                    modifier = Modifier.size(19.dp),
                )
            }
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = listOf(state.displayId, state.roleLabel).filter { it.isNotBlank() }.joinToString(" · "),
                    color = MeshaColors.Ink,
                    fontSize = 16.sp,
                    fontWeight = FontWeight.W800,
                )
                if (state.templateLine.isNotBlank()) {
                    Text(text = state.templateLine, color = MeshaColors.Faint, fontSize = 11.sp)
                }
            }
        }
        if (state.facts.isNotEmpty()) {
            // Two-column facts grid, mock's `.facts`.
            state.facts.chunked(2).forEach { pair ->
                Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                    pair.forEach { (label, value) ->
                        Row(
                            modifier = Modifier
                                .weight(1f)
                                .clip(RoundedCornerShape(10.dp))
                                .background(MeshaColors.Surf2)
                                .padding(horizontal = 10.dp, vertical = 7.dp),
                            horizontalArrangement = Arrangement.SpaceBetween,
                        ) {
                            Text(text = label, color = MeshaColors.Faint, fontSize = 11.sp)
                            Text(
                                text = value,
                                color = MeshaColors.Ink,
                                fontSize = 11.sp,
                                fontWeight = FontWeight.W700,
                            )
                        }
                    }
                    if (pair.size == 1) Box(modifier = Modifier.weight(1f))
                }
            }
        }
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            val fraction = if (state.actionsTotal > 0) state.actionsDone.toFloat() / state.actionsTotal else 0f
            Box(
                modifier = Modifier
                    .weight(1f)
                    .height(5.dp)
                    .clip(RoundedCornerShape(999.dp))
                    .background(MeshaColors.Surf3),
            ) {
                Box(
                    modifier = Modifier
                        .fillMaxWidth(fraction.coerceIn(0f, 1f))
                        .height(5.dp)
                        .clip(RoundedCornerShape(999.dp))
                        .background(if (state.isDeath) MeshaColors.Danger else MeshaColors.Brand),
                )
            }
            Text(
                text = stringResource(R.string.counts_workflow_progress_fmt, state.actionsDone, state.actionsTotal),
                color = MeshaColors.Muted,
                fontSize = 11.sp,
                fontWeight = FontWeight.W700,
            )
        }
    }
}

@Composable
private fun WorkflowActionRow(
    action: WorkflowActionUi,
    state: WorkflowDetailUiState,
    onEvent: (WorkflowDetailEvent) -> Unit,
) {
    val hasDetail = action.detail.isNotBlank()
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf)
            .clickable(enabled = hasDetail || action.opensPromote) {
                if (action.opensPromote && state.subjectGoatId.isNotBlank() && state.subjectGoatRowVersion > 0) {
                    onEvent(
                        WorkflowDetailEvent.OpenPromote(
                            goatId = state.subjectGoatId,
                            displayId = state.displayId,
                            temporaryIdentifier = state.subjectTemporaryIdentifier,
                            locationDisplay = state.subjectLocationDisplay,
                            rowVersion = state.subjectGoatRowVersion,
                        ),
                    )
                }
            }
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(
                text = action.glyph,
                color = if (action.section == WorkflowActionSection.COMPLETED) MeshaColors.Ok else MeshaColors.Muted,
                fontSize = 13.sp,
                fontWeight = FontWeight.W800,
                modifier = Modifier
                    .size(26.dp)
                    .clip(RoundedCornerShape(8.dp))
                    .background(MeshaColors.Surf2)
                    .padding(top = 3.dp),
                textAlign = androidx.compose.ui.text.style.TextAlign.Center,
            )
            Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                Text(
                    text = action.title,
                    color = MeshaColors.Ink,
                    fontSize = 13.sp,
                    fontWeight = FontWeight.W700,
                )
                Row(horizontalArrangement = Arrangement.spacedBy(5.dp)) {
                    WorkflowTag(action.typeLabel, MeshaColors.Surf3, MeshaColors.Muted)
                    if (action.requiresVideo) {
                        WorkflowTag(stringResource(R.string.counts_workflow_tag_video), MeshaColors.WarnX, MeshaColors.Warn)
                    }
                    action.answerValue?.takeIf { it.isNotBlank() }?.let { answer ->
                        val displayedAnswer = answer + action.numericAnswerUnit?.let { " $it" }.orEmpty()
                        WorkflowTag(
                            stringResource(R.string.counts_workflow_answered_fmt, displayedAnswer),
                            MeshaColors.OkX,
                            MeshaColors.Ok,
                        )
                    }
                }
            }
            StatusChip(
                if (action.hasVideoDraft) stringResource(R.string.counts_workflow_recorded) else action.statusLabel,
                if (action.hasVideoDraft) WorkflowStatusTone.DONE else action.statusTone,
                state.isDeath,
            )
        }
        if (hasDetail) {
            Text(
                text = action.detail,
                color = MeshaColors.Muted,
                fontSize = 12.sp,
                lineHeight = 17.sp,
            )
        }
        if (action.canAnswer && action.options.isNotEmpty()) {
            // Question_select bands can be many; wrap two per row so long band lists stay tappable.
            action.options.chunked(2).forEach { pair ->
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    pair.forEach { option ->
                        Text(
                            text = option.label,
                            color = MeshaColors.BrandD,
                            fontSize = 13.sp,
                            fontWeight = FontWeight.W800,
                            textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                            modifier = Modifier
                                .weight(1f)
                                .clip(RoundedCornerShape(10.dp))
                                .background(MeshaColors.BrandTint)
                                .clickable { onEvent(WorkflowDetailEvent.Answer(action.actionId, option.value)) }
                                .minimumInteractiveComponentSize()
                                .padding(vertical = 9.dp),
                        )
                    }
                    if (pair.size == 1) Box(modifier = Modifier.weight(1f))
                }
            }
        }
        if (action.canAnswer && action.numericAnswerUnit != null) {
            var numericAnswer by rememberSaveable(action.actionId) { mutableStateOf("") }
            CountsTextField(
                value = numericAnswer,
                onValueChange = { candidate ->
                    if (candidate.all { it.isDigit() || it == '.' } && candidate.count { it == '.' } <= 1) {
                        numericAnswer = candidate
                    }
                },
                label = stringResource(R.string.counts_workflow_weight_kg),
                required = true,
                numeric = true,
            )
            val validWeight = numericAnswer.toDoubleOrNull()?.let { it > 0.0 && it.isFinite() } == true
            Text(
                text = stringResource(R.string.counts_workflow_weight_continue),
                color = if (validWeight) MeshaColors.OnBrand else MeshaColors.Faint,
                fontSize = 13.sp,
                fontWeight = FontWeight.W800,
                textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(10.dp))
                    .background(if (validWeight) MeshaColors.Brand else MeshaColors.Surf3)
                    .clickable(enabled = validWeight) {
                        onEvent(WorkflowDetailEvent.Answer(action.actionId, numericAnswer))
                    }
                    .minimumInteractiveComponentSize()
                    .padding(vertical = 10.dp),
            )
        }
        if (action.canRecordVideo) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(10.dp))
                    .background(if (state.isDeath) MeshaColors.Danger else MeshaColors.Brand)
                    .clickable(enabled = !state.isCapturingVideo) {
                        onEvent(WorkflowDetailEvent.RecordVideo(action.actionId))
                    }
                    .padding(vertical = 10.dp),
                horizontalArrangement = Arrangement.Center,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(
                    imageVector = MeshaIcons.Video,
                    contentDescription = null,
                    tint = MeshaColors.OnBrand,
                    modifier = Modifier.size(15.dp),
                )
                Text(
                    text = stringResource(
                        if (state.isCapturingVideo) {
                            R.string.counts_workflow_recording
                        } else {
                            if (action.hasVideoDraft) R.string.counts_workflow_rerecord_video else R.string.counts_workflow_record_video
                        },
                    ),
                    color = MeshaColors.OnBrand,
                    fontSize = 13.sp,
                    fontWeight = FontWeight.W800,
                    modifier = Modifier.padding(start = 6.dp),
                )
            }
        }
        if (action.canComplete) {
            Text(
                text = stringResource(R.string.counts_workflow_mark_done),
                color = MeshaColors.OnBrand,
                fontSize = 13.sp,
                fontWeight = FontWeight.W800,
                textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(10.dp))
                    .background(if (state.isDeath) MeshaColors.Danger else MeshaColors.Brand)
                    .clickable { onEvent(WorkflowDetailEvent.Complete(action.actionId)) }
                    .padding(vertical = 10.dp),
            )
        }
        if (action.footer.isNotBlank()) {
            Text(text = action.footer, color = MeshaColors.Faint, fontSize = 11.sp)
        }
    }
}

@Composable
private fun WorkflowTag(text: String, bg: Color, fg: Color) {
    if (text.isBlank()) return
    Text(
        text = text,
        color = fg,
        fontSize = 10.sp,
        fontWeight = FontWeight.W700,
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(bg)
            .padding(horizontal = 7.dp, vertical = 2.dp),
    )
}

@Composable
private fun StatusChip(label: String, tone: WorkflowStatusTone, isDeath: Boolean) {
    if (label.isBlank()) return
    val (fg, bg) = when (tone) {
        WorkflowStatusTone.OVERDUE ->
            if (isDeath) MeshaColors.Danger to MeshaColors.DangerX else MeshaColors.Warn to MeshaColors.WarnX
        WorkflowStatusTone.DONE -> MeshaColors.Ok to MeshaColors.OkX
        WorkflowStatusTone.IN_REVIEW -> MeshaColors.BrandD to MeshaColors.BrandTint
        WorkflowStatusTone.BLOCKED, WorkflowStatusTone.SCHEDULED -> MeshaColors.Muted to MeshaColors.Surf3
    }
    Text(
        text = label,
        color = fg,
        fontSize = 11.sp,
        fontWeight = FontWeight.W700,
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(bg)
            .padding(horizontal = 9.dp, vertical = 3.dp),
    )
}
