package sg.mesha.goatos.feature.leadership

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme

// ─────────────────────────────────────────────────────────────────────────────
// Reschedule (v-reschedule). Segmented reschedule / mark-scheduled + a date field
// whose options are BACKEND-PROVIDED — each option already carries its in/out-of-
// buffer state and label (policySnapshot). The app renders the option list and
// never computes which dates are allowed or whether one is in buffer.
// ─────────────────────────────────────────────────────────────────────────────

data class RescheduleUiState(
    val eyebrow: String,
    val title: String,
    val bufferMessage: String,
    val actionTitle: String,
    val segments: List<RescheduleSegment>,
    val selectedSegmentId: String,
    val dateFieldLabel: String,
    val dateOptionsTitle: String,
    val dateOptions: List<DateOption>,
    val selectedDateId: String?,
    val assignFieldLabel: String,
    val assignPrimary: String,
    val assignBackupLabel: String,
    val assignEnabled: Boolean,
    val confirmLabel: String,
    val confirmEnabled: Boolean,
    val channelsNote: String,
)

data class RescheduleSegment(val id: String, val label: String)

data class DateOption(
    val id: String,
    /** Short day glyph e.g. "10" (backend). */
    val dayLabel: String,
    /** e.g. "Fri, 10 Jul" (backend). */
    val label: String,
    /** e.g. "In buffer" / "Out of buffer · counts as missed" (backend). */
    val sub: String,
    /** Backend-precomputed buffer state; the app renders, never computes it. */
    val inBuffer: Boolean,
)

@Composable
fun RescheduleScreen(
    state: RescheduleUiState,
    onEvent: (LeadershipEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Column(
        modifier
            .fillMaxSize()
            .background(LeadTokens.pageBg),
    ) {
        LeadTopBar(
            eyebrow = state.eyebrow,
            title = state.title,
            leading = TopBarLeading.BACK,
            onLeading = { onEvent(LeadershipEvent.Back) },
        )
        Column(
            Modifier
                .fillMaxWidth()
                .weight(1f)
                .verticalScroll(rememberScrollState())
                .padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            BufferBanner(state.bufferMessage)

            SectionLabel(state.actionTitle)
            SegmentedControl(
                segments = state.segments,
                selectedId = state.selectedSegmentId,
                onSelect = { onEvent(LeadershipEvent.SegmentSelected(it)) },
            )

            if (state.dateOptions.isNotEmpty()) {
                FieldLabel(state.dateFieldLabel)
                Text(state.dateOptionsTitle, color = LeadTokens.faint, fontSize = 11.sp)
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    state.dateOptions.forEach { opt ->
                        DateOptionRow(
                            opt = opt,
                            selected = opt.id == state.selectedDateId,
                            onClick = { onEvent(LeadershipEvent.DateSelected(opt.id)) },
                        )
                    }
                }
            }

            FieldLabel(state.assignFieldLabel)
            AssignField(
                primary = state.assignPrimary,
                backupLabel = state.assignBackupLabel,
                enabled = state.assignEnabled,
                onClick = { onEvent(LeadershipEvent.OpenAssignPicker) },
            )

            ConfirmButton(
                label = state.confirmLabel,
                enabled = state.confirmEnabled,
                onClick = { onEvent(LeadershipEvent.ConfirmReschedule) },
            )
            Text(
                state.channelsNote,
                color = LeadTokens.faint,
                fontSize = 11.sp,
                lineHeight = 16.sp,
            )
        }
    }
}

// region ── form parts ──

@Composable
private fun BufferBanner(message: String) {
    Row(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(LeadTokens.warnX)
            .padding(13.dp),
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Icon(imageVector = MeshaIcons.Warn, contentDescription = null, tint = LeadTokens.warn, modifier = Modifier.size(16.dp))
        Text(message, color = LeadTokens.muted, fontSize = 12.sp, lineHeight = 17.sp)
    }
}

@Composable
private fun FieldLabel(text: String) {
    Text(
        text,
        color = LeadTokens.muted,
        fontSize = 12.sp,
        fontWeight = FontWeight.SemiBold,
        modifier = Modifier.padding(top = 4.dp),
    )
}

@Composable
private fun SegmentedControl(
    segments: List<RescheduleSegment>,
    selectedId: String,
    onSelect: (String) -> Unit,
) {
    Row(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(11.dp))
            .background(LeadTokens.surf2)
            .border(1.dp, LeadTokens.hair, RoundedCornerShape(11.dp))
            .padding(3.dp),
        horizontalArrangement = Arrangement.spacedBy(3.dp),
    ) {
        segments.forEach { seg ->
            val on = seg.id == selectedId
            Box(
                Modifier
                    .weight(1f)
                    .clip(RoundedCornerShape(9.dp))
                    .background(if (on) LeadTokens.brand else Color_Transparent)
                    .clickable { onSelect(seg.id) }
                    .padding(vertical = 9.dp),
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    seg.label,
                    color = if (on) LeadTokens.heroInk else LeadTokens.muted,
                    fontSize = 12.5.sp,
                    fontWeight = FontWeight.Bold,
                )
            }
        }
    }
}

@Composable
private fun DateOptionRow(opt: DateOption, selected: Boolean, onClick: () -> Unit) {
    val accent = if (selected) LeadTokens.brand else LeadTokens.hair
    val dayColor = if (opt.inBuffer) LeadTokens.ink else LeadTokens.warn
    val subColor = if (opt.inBuffer) LeadTokens.muted else LeadTokens.warn
    Row(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(13.dp))
            .background(LeadTokens.surf)
            .border(1.dp, accent, RoundedCornerShape(13.dp))
            .clickable { onClick() }
            .padding(12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Box(
            Modifier.size(34.dp).clip(RoundedCornerShape(10.dp)).background(LeadTokens.surf3),
            contentAlignment = Alignment.Center,
        ) {
            Text(opt.dayLabel, color = dayColor, fontSize = 14.sp, fontWeight = FontWeight.Bold)
        }
        Column(Modifier.weight(1f)) {
            Text(opt.label, color = LeadTokens.ink, fontSize = 13.sp, fontWeight = FontWeight.SemiBold)
            Text(opt.sub, color = subColor, fontSize = 11.5.sp)
        }
        if (selected) {
            Icon(imageVector = MeshaIcons.Check, contentDescription = null, tint = LeadTokens.brand, modifier = Modifier.size(16.dp))
        }
    }
}

@Composable
private fun AssignField(
    primary: String,
    backupLabel: String,
    enabled: Boolean,
    onClick: () -> Unit,
) {
    Row(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(13.dp))
            .background(LeadTokens.surf)
            .border(1.dp, LeadTokens.hair, RoundedCornerShape(13.dp))
            .then(if (enabled) Modifier.clickable { onClick() } else Modifier)
            .padding(13.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Icon(imageVector = MeshaIcons.User, contentDescription = null, tint = LeadTokens.muted, modifier = Modifier.size(16.dp))
        Text(
            primary,
            color = if (enabled) LeadTokens.ink else LeadTokens.faint,
            fontSize = 13.sp,
            fontWeight = FontWeight.SemiBold,
        )
        Spacer(Modifier.weight(1f))
        Text(
            backupLabel,
            color = LeadTokens.muted,
            fontSize = 11.5.sp,
            fontWeight = FontWeight.SemiBold,
            modifier = Modifier.clip(CircleShape).background(LeadTokens.surf3).padding(horizontal = 9.dp, vertical = 4.dp),
        )
        Icon(imageVector = MeshaIcons.Chevron, contentDescription = null, tint = LeadTokens.faint, modifier = Modifier.size(14.dp))
    }
}

@Composable
private fun ConfirmButton(label: String, enabled: Boolean, onClick: () -> Unit) {
    Box(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(13.dp))
            .background(if (enabled) LeadTokens.brand else LeadTokens.surf3)
            .then(if (enabled) Modifier.clickable { onClick() } else Modifier)
            .padding(vertical = 14.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            label,
            color = if (enabled) LeadTokens.heroInk else LeadTokens.faint,
            fontSize = 14.sp,
            fontWeight = FontWeight.Bold,
        )
    }
}

private val Color_Transparent = androidx.compose.ui.graphics.Color.Transparent

// endregion

// region ── Preview ──

internal fun sampleRescheduleState() = RescheduleUiState(
    eyebrow = "Vaccination",
    title = "Goat Pox · overdue",
    bufferMessage = "Reschedule into a compatible drive while the animal is still recoverable. " +
        "The in-buffer window is computed by backend policy (policySnapshot); past it the dose is " +
        "missed and leadership is alerted ahead.",
    actionTitle = "Action",
    segments = listOf(
        RescheduleSegment("reschedule", "Reschedule"),
        RescheduleSegment("mark", "Mark scheduled"),
    ),
    selectedSegmentId = "reschedule",
    dateFieldLabel = "New date",
    dateOptionsTitle = "Within the backend-computed buffer window from the due date",
    dateOptions = listOf(
        DateOption("d8", "8", "Wed, 8 Jul", "Tomorrow · in buffer", inBuffer = true),
        DateOption("d9", "9", "Thu, 9 Jul", "In buffer", inBuffer = true),
        DateOption("d10", "10", "Fri, 10 Jul", "In buffer", inBuffer = true),
        DateOption("d11", "11", "Sat, 11 Jul", "Last day in buffer", inBuffer = true),
        DateOption("d13", "13", "Mon, 13 Jul", "Out of buffer · counts as missed", inBuffer = false),
    ),
    selectedDateId = "d10",
    assignFieldLabel = "Assign to",
    assignPrimary = "Arun Kumar",
    assignBackupLabel = "+ backup Indradev",
    assignEnabled = true,
    confirmLabel = "Confirm — notify team",
    confirmEnabled = true,
    channelsNote = "Team gets a phone call, push, Slack alert and email — 2 days before, and again the morning of.",
)

@Preview(name = "Reschedule", widthDp = 380, heightDp = 900, backgroundColor = 0xFF0A0F0C, showBackground = true)
@Composable
private fun RescheduleScreenPreview() {
    GoatOsTheme {
        RescheduleScreen(state = sampleRescheduleState())
    }
}

// endregion
