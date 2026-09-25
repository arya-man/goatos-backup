package sg.mesha.goatos.feature.penvisits

// telemetry:exempt pure stateless renderer; PenVisitDetailViewModel (in :app) owns the
// pen_visit_* AnalyticsEventsPenVisits + CrashReporter wiring for every refresh, capture,
// upload and submit.

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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.ProofMediaPreview
import sg.mesha.goatos.core.ui.ProofMediaPreviewKind
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton

/**
 * ONE pen visit (`/pen-visits/{taskId}`) — a hosted drill with Up/Back and NO L0 chrome
 * (Android navigation-stack invariant).
 *
 * Everything on it renders as the SERVER composed it: the title, the pen label, the reason line,
 * the chip, the instruction, the done line, and whether the caller may submit. The screen decides
 * nothing about who the caller is.
 *
 * SUBMIT IS IMPLICIT (design choice, recorded here): the visit's deliverable is ONE video, so a
 * finished recording is written down and its submit queued in the same act — there is no
 * separate Submit button. A second tap would add nothing the recording did not already say, and
 * a park head standing at a pen with a signal that comes and goes should not be asked to press
 * anything twice. The queue drains the upload and then the submit on its own; the card shows
 * "Sending" until the server's own `Done` lands.
 */
@Composable
fun PenVisitDetailScreen(
    state: PenVisitDetailUiState,
    onEvent: (PenVisitDetailEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(PenVisitDetailEvent.Refresh) }

    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            // The pen leads (the backend's own label, verbatim); the park sits under it. The
            // backend's full title ("Visit Castro 2 · Coimbatore") is what the recorder chrome and
            // the burned-in overlay carry, where the whole sentence has room.
            title = state.penLabel.ifBlank { state.title },
            subtitle = state.parkName.takeIf { it.isNotBlank() },
            onBack = { onEvent(PenVisitDetailEvent.Back) },
            actions = {
                SyncIconButton(
                    isSyncing = state.isRefreshing,
                    onSync = { onEvent(PenVisitDetailEvent.Refresh) },
                    contentDescription = stringResource(R.string.pen_visits_action_refresh),
                )
            },
        )
        if (state.unavailable) {
            // Nothing cached and the server unreachable: say so and offer Try again — never an
            // endless spinner with no way back.
            EmptyState(
                title = stringResource(R.string.pen_visits_detail_unavailable),
                modifier = Modifier.fillMaxWidth().padding(16.dp),
                icon = MeshaIcons.Warn,
                tone = EmptyTone.Warn,
                action = {
                    PenVisitGhostButton(
                        label = stringResource(R.string.pen_visits_action_try_again),
                        enabled = !state.isRefreshing,
                        onClick = { onEvent(PenVisitDetailEvent.Refresh) },
                    )
                },
            )
            return@Column
        }
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
            item(key = "visit") {
                Column(
                    modifier = penVisitCardModifier(),
                    verticalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                        PenVisitStateChip(label = state.stateChip, tone = state.tone)
                        Spacer(Modifier.weight(1f))
                        if (state.capturing) PenVisitInlineSpinner()
                    }
                    // Backend-composed reason line ("Vaccination yesterday"), verbatim -- why this
                    // pen, today.
                    if (state.reasonLine.isNotBlank()) {
                        Text(text = state.reasonLine, color = MeshaColors.Ink, style = MeshaType.bodyStrong)
                    }
                    if (state.instruction.isNotBlank()) {
                        Text(text = state.instruction, color = MeshaColors.Muted, style = MeshaType.body)
                    }
                }
            }

            item(key = "video") {
                PenVisitVideoSection(state = state, onEvent = onEvent)
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
                            .clickable { onEvent(PenVisitDetailEvent.DismissMessage) },
                    )
                }
            }
        }
    }
}

/**
 * The visit's ONE video slot. The state comes from :app's read of Room — the durable proof row,
 * the queued submit and the server's task — so leaving and re-opening the screen, or a process
 * death mid-upload, shows the clip already taken instead of asking for it again.
 */
@Composable
private fun PenVisitVideoSection(
    state: PenVisitDetailUiState,
    onEvent: (PenVisitDetailEvent) -> Unit,
) {
    Column(
        modifier = penVisitCardModifier(),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text(
            text = stringResource(R.string.pen_visits_video_label),
            color = MeshaColors.Faint,
            style = MeshaType.sectionLabel,
        )
        if (state.previewPath.isNotBlank()) {
            // The clip itself, shown back. Without this the ONLY sign a recording landed would be
            // the button label flipping — which reads as "that did not take".
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .height(200.dp)
                    .clip(RoundedCornerShape(12.dp))
                    .background(MeshaColors.Surf2),
                contentAlignment = Alignment.Center,
            ) {
                ProofMediaPreview(
                    path = state.previewPath,
                    kind = ProofMediaPreviewKind.Video,
                    mediaIdentity = "pen_visit:${state.taskId}:visit_video",
                    onPreviewAction = { action -> onEvent(PenVisitDetailEvent.ProofPreviewAction(action)) },
                )
            }
        }
        when (state.videoState) {
            PenVisitVideoState.EMPTY -> {
                PenVisitPrimaryButton(
                    label = stringResource(R.string.pen_visits_action_record),
                    // ONLY the server's `can_submit` opens the camera. A visit the caller may not
                    // submit keeps a visible, dead button rather than none at all.
                    enabled = state.canSubmit && !state.capturing,
                    onClick = { onEvent(PenVisitDetailEvent.RecordVideo) },
                    modifier = Modifier.fillMaxWidth(),
                    icon = MeshaIcons.Video,
                )
            }
            PenVisitVideoState.WORKING -> {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    PenVisitInlineSpinner()
                    Text(
                        text = state.progressLabel.ifBlank { stringResource(R.string.pen_visits_state_sending) },
                        color = MeshaColors.Muted,
                        style = MeshaType.caption,
                    )
                }
            }
            PenVisitVideoState.FAILED -> {
                if (state.failureReason.isNotBlank()) {
                    Text(text = state.failureReason, color = MeshaColors.Danger, style = MeshaType.caption)
                }
                PenVisitGhostButton(
                    label = stringResource(R.string.pen_visits_action_record_again),
                    enabled = state.canSubmit && !state.capturing,
                    onClick = { onEvent(PenVisitDetailEvent.RecordVideo) },
                    modifier = Modifier.fillMaxWidth(),
                    icon = MeshaIcons.Video,
                )
            }
            PenVisitVideoState.IN_REVIEW -> {
                // The clip is on the server and the verifier has it; the backend done line says
                // when the pen was visited. Nothing to press.
                Text(
                    text = stringResource(R.string.pen_visits_state_in_review),
                    color = MeshaColors.Muted,
                    style = MeshaType.bodyStrong,
                )
                if (state.doneLine.isNotBlank()) {
                    Text(text = state.doneLine, color = MeshaColors.Muted, style = MeshaType.caption)
                }
            }
            PenVisitVideoState.REWORK -> {
                Text(
                    text = stringResource(R.string.pen_visits_state_sent_back),
                    color = MeshaColors.Danger,
                    style = MeshaType.bodyStrong,
                )
                // The verifier's own words, verbatim.
                if (state.reworkReason.isNotBlank()) {
                    Text(text = state.reworkReason, color = MeshaColors.Danger, style = MeshaType.caption)
                }
                PenVisitPrimaryButton(
                    label = stringResource(R.string.pen_visits_action_record_again),
                    enabled = state.canSubmit && !state.capturing,
                    onClick = { onEvent(PenVisitDetailEvent.RecordVideo) },
                    modifier = Modifier.fillMaxWidth(),
                    icon = MeshaIcons.Video,
                )
            }
            PenVisitVideoState.DONE -> {
                Text(
                    text = stringResource(R.string.pen_visits_state_submitted),
                    color = MeshaColors.Ok,
                    style = MeshaType.bodyStrong,
                )
                // Backend-composed done line ("Visited on 6 Sep by …"), verbatim.
                if (state.doneLine.isNotBlank()) {
                    Text(text = state.doneLine, color = MeshaColors.Muted, style = MeshaType.caption)
                }
            }
        }
    }
}
