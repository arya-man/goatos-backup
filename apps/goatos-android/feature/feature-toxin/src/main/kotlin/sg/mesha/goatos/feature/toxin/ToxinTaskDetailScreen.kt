package sg.mesha.goatos.feature.toxin

// telemetry:exempt pure stateless renderer; ToxinTaskDetailViewModel (in :app) owns the toxin_*
// AnalyticsEventsToxin + CrashReporter wiring for every refresh, capture, and submit.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import coil.compose.AsyncImage
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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton

/**
 * ONE aflatoxin test round's guided screen (`/toxin/tasks/{taskId}`) — a hosted drill with
 * Up/Back and NO L0 chrome (Android navigation-stack invariant).
 *
 * The seven steps render exactly as the SERVER composed them: its `state` decides what is
 * actionable, its `title`/`instruction` are the only words on each row, and a `waiting` step's
 * countdown is display-only text beside a DISABLED action. The screen never promotes a step
 * itself — the server re-checks and refuses, so an optimistic unlock would only produce a refusal
 * the operator cannot act on.
 *
 * Refresh-on-resume is load-bearing rather than polish: steps are person-independent, so the
 * previous step may have been completed on someone else's phone, and a settle window elapses with
 * no local event whatsoever.
 */
@Composable
fun ToxinTaskDetailScreen(
    state: ToxinTaskDetailUiState,
    onEvent: (ToxinTaskDetailEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(ToxinTaskDetailEvent.Refresh) }

    // One ticking clock for every countdown on the screen, running ONLY while a step is actually
    // waiting — a screen with nothing to count down recomposes not at all.
    val hasWaitingStep = state.steps.any { it.state == ToxinStepState.WAITING && it.availableAtEpochMs > 0L }
    var nowMs by remember { mutableLongStateOf(System.currentTimeMillis()) }
    LaunchedEffect(hasWaitingStep) {
        while (hasWaitingStep) {
            nowMs = System.currentTimeMillis()
            delay(1_000L)
        }
    }

    // When a wait ELAPSES, re-read the server's step states.
    //
    // Step state is server-composed and time-dependent, but the cache is only written on a
    // network event — so without this the countdown runs to zero and the screen stays frozen on
    // WAITING forever. That is not cosmetic: the reading step never opens, so "Send reading"
    // stays dead and the round cannot be submitted at all until someone happens to leave the
    // screen and come back, or finds the sync button. It is exactly what a tester reported.
    //
    // The phone still decides NOTHING: this only asks the server again. The refresh is armed off
    // the earliest waiting gate and re-arms when that gate value changes, so a screen with no
    // wait schedules no work. A little past the instant, because the SERVER's clock is the gate
    // and a device running fast would otherwise ask while it is still closed; if it does answer
    // "still waiting", the bounded retry asks again rather than freezing the way this fixes.
    val nextGateAtMs = nextGateInstantMs(state.steps)
    LaunchedEffect(nextGateAtMs) {
        if (nextGateAtMs <= 0L) return@LaunchedEffect
        val untilGate = nextGateAtMs - System.currentTimeMillis()
        if (untilGate > 0L) delay(untilGate)
        repeat(GATE_REFRESH_ATTEMPTS) {
            delay(GATE_REFRESH_SKEW_MS)
            onEvent(ToxinTaskDetailEvent.Refresh)
            // A refresh that opens the step changes this effect's key and cancels the loop; the
            // cap stops a server that keeps answering "waiting" from polling forever.
        }
    }

    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            // Backend-composed farm line for this round, rendered verbatim.
            title = state.contextLine,
            onBack = { onEvent(ToxinTaskDetailEvent.Back) },
            actions = {
                SyncIconButton(
                    isSyncing = state.isRefreshing,
                    onSync = { onEvent(ToxinTaskDetailEvent.Refresh) },
                )
            },
        )
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(top = 4.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "header") {
                Column(
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                    verticalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        ToxinStatusChip(label = state.statusChip)
                    }
                    // Every one of these is a BACKEND-owned sentence; the screen shows whichever
                    // the payload carried and composes none of them.
                    listOf(state.originLine, state.outcomeLabel).forEach { line ->
                        if (line.isNotBlank()) {
                            Text(text = line, color = MeshaColors.Muted, style = MeshaType.caption)
                        }
                    }
                    listOf(state.reviewReason, state.cancelReason).forEach { line ->
                        if (line.isNotBlank()) {
                            Text(text = line, color = MeshaColors.Danger, style = MeshaType.caption)
                        }
                    }
                    if (state.stepsTotal > 0) {
                        ToxinStepProgress(done = state.stepsDone, total = state.stepsTotal)
                    }
                }
            }

            items(count = state.steps.size, key = { index -> state.steps[index].stepNo }) { index ->
                val step = state.steps[index]
                ToxinStepRow(
                    step = step,
                    nowMs = nowMs,
                    state = state,
                    onEvent = onEvent,
                )
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
                            .clickable { onEvent(ToxinTaskDetailEvent.DismissMessage) },
                    )
                }
            }
        }
    }
}

@Composable
private fun ToxinStepRow(
    step: ToxinStepUi,
    nowMs: Long,
    state: ToxinTaskDetailUiState,
    onEvent: (ToxinTaskDetailEvent) -> Unit,
) {
    // A locked step is dimmed rather than hidden: the operator is owed the whole procedure, and
    // seeing what comes next is how they know a wait is normal instead of a failure.
    val dimmed = step.state == ToxinStepState.LOCKED
    Column(
        modifier = toxinCardModifier(enabled = false, onClick = null).alpha(if (dimmed) 0.55f else 1f),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            if (step.state == ToxinStepState.DONE) {
                Icon(
                    imageVector = MeshaIcons.Check,
                    contentDescription = null,
                    tint = MeshaColors.Ok,
                    modifier = Modifier.size(18.dp).padding(end = 2.dp),
                )
            }
            // Backend-owned step title, verbatim.
            Text(
                text = step.title,
                color = MeshaColors.Ink,
                style = MeshaType.cardTitle,
                modifier = Modifier.weight(1f),
            )
            if (step.state == ToxinStepState.WAITING) {
                val digits = toxinCountdownDigits(step.availableAtEpochMs, nowMs)
                if (digits.isNotBlank()) {
                    Text(text = digits, color = MeshaColors.Warn, style = MeshaType.pillStrong)
                }
            }
        }
        // Backend-owned instruction, verbatim — including the sentence that explains a wait.
        if (step.instruction.isNotBlank()) {
            Text(text = step.instruction, color = MeshaColors.Muted, style = MeshaType.caption)
        }
        // Who actually did this step, and when. Steps are person-independent, so this is the only
        // way the screen can attribute the work.
        if (step.state == ToxinStepState.DONE && step.completedLine.isNotBlank()) {
            Text(text = step.completedLine, color = MeshaColors.Muted, style = MeshaType.caption)
        }

        when (step.kind) {
            ToxinStepKind.VIDEO -> {
                if (step.state != ToxinStepState.DONE) {
                    ToxinPrimaryButton(
                        label = "Record video",
                        // ONLY the server's `available` opens this. A waiting/locked step keeps a
                        // visible, dead button rather than none at all.
                        enabled = step.state == ToxinStepState.AVAILABLE && !step.working,
                        onClick = { onEvent(ToxinTaskDetailEvent.RecordStepVideo(step.stepNo)) },
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            }
            ToxinStepKind.PHOTO_READING -> {
                if (step.state != ToxinStepState.DONE) {
                    ToxinStripReadingSection(
                        enabled = step.state == ToxinStepState.AVAILABLE,
                        working = step.working,
                        state = state,
                        onEvent = onEvent,
                    )
                }
            }
            // A wait carries no action at all: there is nothing for the operator to do but wait.
            ToxinStepKind.WAIT -> Unit
        }
    }
}

/**
 * Step 7: the strip photo, the backend's reading guide, its reading vocabulary, and one send.
 *
 * Both halves are required before the send opens — a reading with no strip photo proves nothing,
 * and a photo with no reading records nothing.
 */
@Composable
private fun ToxinStripReadingSection(
    enabled: Boolean,
    working: Boolean,
    state: ToxinTaskDetailUiState,
    onEvent: (ToxinTaskDetailEvent) -> Unit,
) {
    Column(
        modifier = Modifier.fillMaxWidth(),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        // Backend-owned reading guide, verbatim and in order.
        state.readingGuide.forEach { line ->
            Text(text = line, color = MeshaColors.Muted, style = MeshaType.caption)
        }

        if (state.stripPhotoCaptured && state.stripPhotoUri.isNotBlank()) {
            // The photograph itself, shown back. Without this the ONLY sign a capture landed was
            // the button label below flipping to "again" — which reads as "that did not take", so
            // the strip gets photographed over and over. The step's deliverable is the image, so
            // the image is the confirmation.
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .height(200.dp)
                    .clip(RoundedCornerShape(12.dp))
                    .background(MeshaColors.Surf),
                contentAlignment = Alignment.Center,
            ) {
                AsyncImage(
                    model = state.stripPhotoUri,
                    // Farm language describing the EVIDENCE, not the file.
                    contentDescription = "The strip photo you took for this test",
                    contentScale = ContentScale.Fit,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        }

        if (state.stripPhotoCaptured) {
            ToxinGhostButton(
                label = "Take the photo again",
                enabled = enabled && !working && !state.submitQueued,
                onClick = { onEvent(ToxinTaskDetailEvent.CaptureStripPhoto) },
                modifier = Modifier.fillMaxWidth(),
            )
        } else {
            ToxinPrimaryButton(
                label = "Take strip photo",
                enabled = enabled && !working && !state.submitQueued,
                onClick = { onEvent(ToxinTaskDetailEvent.CaptureStripPhoto) },
                modifier = Modifier.fillMaxWidth(),
            )
        }

        // The backend's own reading vocabulary — never a client-invented set of outcomes.
        state.outcomeOptions.forEach { option ->
            ToxinOutcomeRow(
                option = option,
                selected = state.selectedOutcome == option.value,
                enabled = enabled && !state.submitQueued,
                onClick = { onEvent(ToxinTaskDetailEvent.SelectOutcome(option.value)) },
            )
        }

        ToxinPrimaryButton(
            label = "Send reading",
            enabled = state.submitEnabled && !state.submitInFlight && !state.submitQueued,
            onClick = { onEvent(ToxinTaskDetailEvent.SubmitReading) },
            modifier = Modifier.fillMaxWidth(),
        )
    }
}

@Composable
private fun ToxinOutcomeRow(
    option: ToxinOutcomeOptionUi,
    selected: Boolean,
    enabled: Boolean,
    onClick: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(if (selected) MeshaColors.Surf2 else MeshaColors.Surf)
            .border(
                width = 1.dp,
                color = if (selected) MeshaColors.Brand else MeshaColors.Hair,
                shape = RoundedCornerShape(12.dp),
            )
            .clickable(enabled = enabled, role = Role.RadioButton, onClick = onClick)
            .padding(horizontal = 12.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Box(
            modifier = Modifier
                .size(16.dp)
                .clip(RoundedCornerShape(8.dp))
                .background(if (selected) MeshaColors.Brand else MeshaColors.Surf3),
        )
        // Backend-owned outcome label, verbatim.
        Text(
            text = option.label,
            color = if (enabled) MeshaColors.Ink else MeshaColors.Faint,
            style = MeshaType.bodyStrong,
        )
    }
}

/**
 * The earliest server gate the screen is waiting on, as epoch millis; 0 when nothing waits.
 *
 * EARLIEST, not any: a round can carry more than one waiting step, and arming on a later one
 * would leave the nearer gate to expire unnoticed — the freeze this exists to prevent.
 */
internal fun nextGateInstantMs(steps: List<ToxinStepUi>): Long = steps
    .filter { it.state == ToxinStepState.WAITING && it.availableAtEpochMs > 0L }
    .minOfOrNull { it.availableAtEpochMs } ?: 0L

/** How far past a gate's instant to wait before asking the server, absorbing clock skew. */
private const val GATE_REFRESH_SKEW_MS = 2_000L

/** Bounded retries for a gate the server still reports as closed, so a stuck wait never polls forever. */
private const val GATE_REFRESH_ATTEMPTS = 10
