package sg.mesha.goatos.feature.profile

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors

// ---------------------------------------------------------------------------
// Alerts / notifications surface (screens.md: v-alerts; mock #v-alerts `.notif`).
//
// Per TRD §14 dumb-renderer: this screen RENDERS the alert rows the backend
// (FCM / notification history) surfaced. It does NOT decide severity, does NOT
// compute read/unread, does NOT sort or classify — every visible title, body,
// timestamp label, tone, and unread flag is a field on AlertRow. The only glue is
// mapping the backend-provided AlertTone to a pill colour + short severity word,
// the same way a StatusPill renders a tone variant.
// ---------------------------------------------------------------------------

private object AlertsTokens {
    val Bg = MeshaColors.Bg
    val Surf2 = MeshaColors.Surf2
    val Surf3 = MeshaColors.Surf3
    val Hair = MeshaColors.Hair
    val Ink = MeshaColors.Ink
    val Muted = MeshaColors.Muted
    val Faint = MeshaColors.Faint
    val Brand = MeshaColors.Brand
    val BrandD = MeshaColors.BrandD
    val Teal = MeshaColors.Teal
    val Warn = MeshaColors.Warn
    val Danger = MeshaColors.Danger
    val TealX = MeshaColors.TealX
    val WarnX = MeshaColors.WarnX
    val DangerX = MeshaColors.DangerX
    val Ok = MeshaColors.Ok
    val OkX = MeshaColors.OkX
}

/** Backend-provided severity. Drives pill colour + short severity word + accent only. */
enum class AlertTone { INFO, WARN, CRITICAL }

/** Delivery-channel chip tone (mock `.pill.ok` / `.pill.mut` / `.pill.teal`). */
enum class AlertChannelTone { SENT, PENDING, TEAL }

/** One delivery-channel chip on a notification (mock `.chans` — Call/Push/Slack/Email). */
data class AlertChannel(val label: String, val tone: AlertChannelTone)

/** One notification row. Every field is backend-provided; the app renders it verbatim.
 *  @Immutable: channels: List<AlertChannel> otherwise marks this unstable (item 6,
 *  perf/stability pass). */
@Immutable
data class AlertRow(
    val id: String,
    val title: String,
    val body: String,
    val timeLabel: String,
    val tone: AlertTone,
    val unread: Boolean = false,
    /** Empty when the backend has no delivery-channel breakdown for this row (mock omits `.chans` too). */
    val channels: List<AlertChannel> = emptyList(),
)

/**
 * Everything the Alerts surface renders. Header title, the alert rows, the empty
 * copy, and the mark-all label are all backend-provided (dumb renderer). A null
 * [markAllLabel] means the backend surfaced no mark-all action for this principal.
 */
// @Immutable: rows: List<AlertRow> otherwise marks this unstable (item 6, perf/stability pass).
@Immutable
data class AlertsUiState(
    val title: String,
    val rows: List<AlertRow> = emptyList(),
    val emptyLabel: String,
    val markAllLabel: String? = null,
)

sealed interface AlertsEvent {
    data object MarkAllRead : AlertsEvent
    data class OpenAlert(val id: String) : AlertsEvent
}

/** Tone pill (bg, fg) — mirrors the mock's `.pill` tone variants. */
private fun tonePill(tone: AlertTone): Pair<Color, Color> = when (tone) {
    AlertTone.INFO -> AlertsTokens.TealX to AlertsTokens.Teal
    AlertTone.WARN -> AlertsTokens.WarnX to AlertsTokens.Warn
    AlertTone.CRITICAL -> AlertsTokens.DangerX to AlertsTokens.Danger
}

/** Short severity word rendered from the backend tone (like a StatusPill label). */
private fun toneLabel(tone: AlertTone): String = when (tone) {
    AlertTone.INFO -> "Info"
    AlertTone.WARN -> "Warn"
    AlertTone.CRITICAL -> "Critical"
}

/** Channel-chip pill colours (mock `.pill.ok` / `.pill.mut` / `.pill.teal`). */
private fun channelPill(tone: AlertChannelTone): Pair<Color, Color> = when (tone) {
    AlertChannelTone.SENT -> AlertsTokens.OkX to AlertsTokens.Ok
    AlertChannelTone.PENDING -> AlertsTokens.Surf3 to AlertsTokens.Muted
    AlertChannelTone.TEAL -> AlertsTokens.TealX to AlertsTokens.Teal
}

/** Title tint follows the tone when unread; read rows dim to muted. */
private fun titleColor(tone: AlertTone, unread: Boolean): Color {
    if (!unread) return AlertsTokens.Muted
    return when (tone) {
        AlertTone.INFO -> AlertsTokens.Ink
        AlertTone.WARN -> AlertsTokens.Warn
        AlertTone.CRITICAL -> AlertsTokens.Danger
    }
}

@Composable
fun AlertsScreen(
    state: AlertsUiState,
    onEvent: (AlertsEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    LazyColumn(
        modifier = modifier
            .fillMaxSize()
            .background(AlertsTokens.Bg),
        contentPadding = PaddingValues(bottom = 24.dp),
    ) {
        item { AlertsHeader(title = state.title, markAllLabel = state.markAllLabel, onEvent = onEvent) }
        if (state.rows.isEmpty()) {
            item { AlertsEmpty(state.emptyLabel) }
        } else {
            items(state.rows.size) { index ->
                AlertCard(row = state.rows[index], onEvent = onEvent)
            }
        }
    }
}

@Composable
private fun AlertsHeader(title: String, markAllLabel: String?, onEvent: (AlertsEvent) -> Unit) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .padding(start = 16.dp, end = 16.dp, top = 16.dp, bottom = 8.dp),
    ) {
        Text(
            text = title,
            color = AlertsTokens.Ink,
            fontSize = 22.sp,
            fontWeight = FontWeight.W700,
            modifier = Modifier.weight(1f),
        )
        markAllLabel?.let {
            Text(
                text = it,
                color = AlertsTokens.BrandD,
                fontSize = 13.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier
                    .clickable { onEvent(AlertsEvent.MarkAllRead) }
                    .padding(vertical = 6.dp, horizontal = 4.dp),
            )
        }
    }
}

@Composable
private fun AlertCard(row: AlertRow, onEvent: (AlertsEvent) -> Unit) {
    val (pillBg, pillFg) = tonePill(row.tone)
    val borderColor = if (row.unread) pillFg else AlertsTokens.Hair
    Column(
        modifier = Modifier
            .padding(start = 16.dp, end = 16.dp, top = 11.dp)
            .fillMaxWidth()
            .background(AlertsTokens.Surf2, shape = RoundedCornerShape(16.dp))
            .border(1.dp, borderColor, shape = RoundedCornerShape(16.dp))
            .clickable { onEvent(AlertsEvent.OpenAlert(row.id)) }
            .padding(horizontal = 14.dp, vertical = 13.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            if (row.unread) {
                Box(
                    modifier = Modifier
                        .size(8.dp)
                        .background(pillFg, shape = RoundedCornerShape(999.dp)),
                )
                Spacer(Modifier.width(8.dp))
            }
            Box(
                modifier = Modifier
                    .background(pillBg, shape = RoundedCornerShape(999.dp))
                    .padding(horizontal = 10.dp, vertical = 4.dp),
            ) {
                Text(text = toneLabel(row.tone), color = pillFg, fontSize = 11.sp, fontWeight = FontWeight.W700)
            }
            Spacer(Modifier.weight(1f))
            Text(text = row.timeLabel, color = AlertsTokens.Muted, fontSize = 10.5.sp, fontWeight = FontWeight.W700)
        }
        Text(
            text = row.title,
            color = titleColor(row.tone, row.unread),
            fontSize = 13.5.sp,
            fontWeight = FontWeight.W700,
            modifier = Modifier.padding(top = 8.dp),
        )
        Text(
            text = row.body,
            color = AlertsTokens.Muted,
            fontSize = 12.sp,
            modifier = Modifier.padding(top = 3.dp),
        )
        if (row.channels.isNotEmpty()) {
            Row(
                horizontalArrangement = Arrangement.spacedBy(6.dp),
                modifier = Modifier.padding(top = 9.dp),
            ) {
                row.channels.forEach { channel ->
                    val (bg, fg) = channelPill(channel.tone)
                    Box(
                        modifier = Modifier
                            .background(bg, shape = RoundedCornerShape(999.dp))
                            .padding(horizontal = 10.dp, vertical = 4.dp),
                    ) {
                        Text(text = channel.label, color = fg, fontSize = 10.5.sp, fontWeight = FontWeight.W700)
                    }
                }
            }
        }
    }
}

@Composable
private fun AlertsEmpty(message: String) {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 48.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = message,
            color = AlertsTokens.Faint,
            fontSize = 13.sp,
            textAlign = TextAlign.Center,
        )
    }
}

@Preview(backgroundColor = 0xFF0B100D, showBackground = true)
@Composable
private fun AlertsScreenPreview() {
    GoatOsTheme {
        AlertsScreen(
            state = AlertsUiState(
                title = "Alerts",
                markAllLabel = "Mark all read",
                emptyLabel = "You're all caught up.",
                rows = listOf(
                    AlertRow(
                        id = "a1",
                        title = "Vaccination drive in 2 days",
                        body = "FMD + HS · Thu 9 Jul · Sheds Gandhi 1, Castro 1, Mandela 1 · 118 animals. Confirm stock & staffing.",
                        timeLabel = "now",
                        tone = AlertTone.INFO,
                        unread = true,
                        channels = listOf(
                            AlertChannel("Call", AlertChannelTone.SENT),
                            AlertChannel("Push", AlertChannelTone.PENDING),
                            AlertChannel("Slack", AlertChannelTone.TEAL),
                            AlertChannel("Email", AlertChannelTone.PENDING),
                        ),
                    ),
                    AlertRow(
                        id = "a2",
                        title = "Submit today's drive",
                        body = "Shed Gandhi 1 · 50/50 done but not submitted. Reminder call in 30 min if not sent.",
                        timeLabel = "8:00",
                        tone = AlertTone.WARN,
                        unread = true,
                    ),
                    AlertRow(
                        id = "a3",
                        title = "Overdue: Castro 2 not started",
                        body = "Booster dose window closes today. No scans recorded — chase the operator.",
                        timeLabel = "Mon",
                        tone = AlertTone.CRITICAL,
                        unread = false,
                    ),
                ),
            ),
        )
    }
}
