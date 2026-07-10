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
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors

// ---------------------------------------------------------------------------
// RFID reader pairing surface (screens.md: v-rfid; mock #v-rfid `.device2`).
//
// Per TRD §14 dumb-renderer: this screen RENDERS the reader/pairing state the
// backend + RfidReaderPort surfaced. It does NOT talk to Bluetooth, does NOT
// decide whether a reader is connected, does NOT compute battery/signal, and does
// NOT know which readers are in range — every visible label, status, action, and
// discovered-reader row is a field on RfidUiState, never a client-side literal.
// The only glue is mapping the (backend-provided) connection state to which
// RfidEvent the primary button emits, analogous to a StatusPill tone.
// ---------------------------------------------------------------------------

/** Backend-surfaced connection state. Drives the glyph tint + status-pill tone only. */
enum class RfidConnectionState { CONNECTED, DISCONNECTED, SCANNING }

/** One discovered reader from a scan. Every field is backend/device-provided. */
data class RfidReaderRow(
    val id: String,
    val name: String,
    val detail: String,
    val signalLabel: String,
)

/**
 * Everything the RFID pairing surface renders. Header title, status text, the
 * paired reader name/detail, the discovered list, the primary action label, and
 * the test-read label all come from the backend/RfidReaderPort — the app renders
 * whatever it is given.
 */
// @Immutable: discovered: List<RfidReaderRow> otherwise marks this unstable (item 6,
// perf/stability pass).
@Immutable
data class RfidUiState(
    val title: String,
    val statusLabel: String,
    val connectionState: RfidConnectionState,
    val readerName: String? = null,
    val readerDetail: String? = null,
    val discovered: List<RfidReaderRow> = emptyList(),
    val primaryActionLabel: String,
    val testLabel: String? = null,
)

sealed interface RfidEvent {
    data object Pair : RfidEvent
    data object Disconnect : RfidEvent
    data object TestRead : RfidEvent
    data class SelectReader(val id: String) : RfidEvent
}

/** UI glue: which event the primary button emits for the backend-provided state. */
// A ready/connected reader's primary action is a test read (Android owns the HID
// connection, so there is no in-app "disconnect"); otherwise it opens system pairing.
private fun primaryEventFor(state: RfidConnectionState): RfidEvent =
    if (state == RfidConnectionState.CONNECTED) RfidEvent.TestRead else RfidEvent.Pair

private fun glyphTint(state: RfidConnectionState): Color = when (state) {
    RfidConnectionState.CONNECTED -> MeshaColors.Brand
    RfidConnectionState.SCANNING -> MeshaColors.Teal
    RfidConnectionState.DISCONNECTED -> MeshaColors.Muted
}

/** Status-pill tone (bg, fg) per connection state — mirrors the mock's `.pill` variants. */
private fun statusPillTone(state: RfidConnectionState): Pair<Color, Color> = when (state) {
    RfidConnectionState.CONNECTED -> MeshaColors.OkX to MeshaColors.BrandD
    RfidConnectionState.SCANNING -> MeshaColors.TealX to MeshaColors.Teal
    RfidConnectionState.DISCONNECTED -> MeshaColors.Surf3 to MeshaColors.Muted
}

@Composable
fun RfidScreen(
    state: RfidUiState,
    onEvent: (RfidEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    LazyColumn(
        modifier = modifier
            .fillMaxSize()
            .background(MeshaColors.Bg),
        contentPadding = PaddingValues(bottom = 24.dp),
    ) {
        item { RfidHeader(state.title) }
        item { RfidDeviceHero(state) }
        if (state.discovered.isNotEmpty()) {
            items(state.discovered.size) { index ->
                DiscoveredReaderRow(row = state.discovered[index], onEvent = onEvent)
            }
        }
        item { RfidPrimaryAction(state = state, onEvent = onEvent) }
        state.testLabel?.let { label ->
            item { TestReadRow(label = label, onEvent = onEvent) }
        }
    }
}

@Composable
private fun RfidHeader(title: String) {
    Text(
        text = title,
        color = MeshaColors.Ink,
        fontSize = 22.sp,
        fontWeight = FontWeight.W700,
        modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 16.dp, bottom = 4.dp),
    )
}

@Composable
private fun RfidDeviceHero(state: RfidUiState) {
    val (pillBg, pillFg) = statusPillTone(state.connectionState)
    Column(
        horizontalAlignment = Alignment.CenterHorizontally,
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 16.dp, bottom = 8.dp),
    ) {
        Box(
            modifier = Modifier
                .size(80.dp)
                .background(MeshaColors.BrandGradientSoft, shape = RoundedCornerShape(24.dp))
                .border(1.dp, MeshaColors.Hair, shape = RoundedCornerShape(24.dp)),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = MeshaIcons.Bluetooth,
                contentDescription = null,
                tint = glyphTint(state.connectionState),
                modifier = Modifier.size(34.dp),
            )
        }
        state.readerName?.let {
            Text(
                text = it,
                color = MeshaColors.Ink,
                fontSize = 16.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier.padding(top = 12.dp),
            )
        }
        state.readerDetail?.let {
            Text(
                text = it,
                color = MeshaColors.Muted,
                fontSize = 12.sp,
                modifier = Modifier.padding(top = 3.dp),
            )
        }
        Box(
            modifier = Modifier
                .padding(top = 9.dp)
                .background(pillBg, shape = RoundedCornerShape(999.dp))
                .padding(horizontal = 10.dp, vertical = 4.dp),
        ) {
            Text(text = state.statusLabel, color = pillFg, fontSize = 11.sp, fontWeight = FontWeight.W700)
        }
    }
}

@Composable
private fun DiscoveredReaderRow(row: RfidReaderRow, onEvent: (RfidEvent) -> Unit) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .padding(start = 16.dp, end = 16.dp, top = 9.dp)
            .fillMaxWidth()
            .heightIn(min = 52.dp)
            .background(MeshaColors.Surf, shape = RoundedCornerShape(15.dp))
            .border(1.dp, MeshaColors.Hair, shape = RoundedCornerShape(15.dp))
            .clickable { onEvent(RfidEvent.SelectReader(row.id)) }
            .padding(horizontal = 15.dp, vertical = 11.dp),
    ) {
        Icon(imageVector = MeshaIcons.Bluetooth, contentDescription = null, tint = MeshaColors.Faint, modifier = Modifier.size(16.dp))
        Spacer(Modifier.width(11.dp))
        Column(modifier = Modifier.weight(1f)) {
            Text(text = row.name, color = MeshaColors.Ink, fontSize = 13.5.sp, fontWeight = FontWeight.W700)
            Text(
                text = row.detail,
                color = MeshaColors.Muted,
                fontSize = 11.sp,
                modifier = Modifier.padding(top = 2.dp),
            )
        }
        Spacer(Modifier.width(10.dp))
        Text(
            text = row.signalLabel,
            color = MeshaColors.Muted,
            fontSize = 12.sp,
            fontWeight = FontWeight.W700,
        )
    }
}

@Composable
private fun RfidPrimaryAction(state: RfidUiState, onEvent: (RfidEvent) -> Unit) {
    Row(
        horizontalArrangement = Arrangement.Center,
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .padding(start = 16.dp, end = 16.dp, top = 16.dp)
            .fillMaxWidth()
            .heightIn(min = 52.dp)
            .background(MeshaColors.Surf2, shape = RoundedCornerShape(15.dp))
            .border(1.dp, MeshaColors.Hair, shape = RoundedCornerShape(15.dp))
            .clickable { onEvent(primaryEventFor(state.connectionState)) }
            .padding(15.dp),
    ) {
        Text(
            text = state.primaryActionLabel,
            color = MeshaColors.BrandD,
            fontSize = 15.sp,
            fontWeight = FontWeight.W700,
            fontFamily = FontFamily.Default,
        )
    }
}

@Composable
private fun TestReadRow(label: String, onEvent: (RfidEvent) -> Unit) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .padding(start = 16.dp, end = 16.dp, top = 10.dp)
            .fillMaxWidth()
            .heightIn(min = 50.dp)
            .background(MeshaColors.Surf, shape = RoundedCornerShape(14.dp))
            .border(1.dp, MeshaColors.Hair, shape = RoundedCornerShape(14.dp))
            .clickable { onEvent(RfidEvent.TestRead) }
            .padding(horizontal = 15.dp, vertical = 13.dp),
    ) {
        Icon(imageVector = MeshaIcons.Search, contentDescription = null, tint = MeshaColors.Faint, modifier = Modifier.size(16.dp))
        Spacer(Modifier.width(10.dp))
        Text(text = label, color = MeshaColors.Ink, fontSize = 14.sp, fontWeight = FontWeight.W600, modifier = Modifier.weight(1f))
        Icon(imageVector = MeshaIcons.Check, contentDescription = null, tint = MeshaColors.Brand, modifier = Modifier.size(16.dp))
    }
}

@Preview(backgroundColor = 0xFF0B100D, showBackground = true)
@Composable
private fun RfidScreenPreview() {
    GoatOsTheme {
        RfidScreen(
            state = RfidUiState(
                title = "RFID reader",
                statusLabel = "Paired · battery 84%",
                connectionState = RfidConnectionState.CONNECTED,
                readerName = "Chainway R3",
                readerDetail = "Bluetooth keyboard · signal strong",
                primaryActionLabel = "Disconnect",
                testLabel = "Test read",
            ),
        )
    }
}
