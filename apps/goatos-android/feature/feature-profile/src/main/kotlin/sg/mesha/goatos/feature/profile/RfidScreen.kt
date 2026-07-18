package sg.mesha.goatos.feature.profile

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsPressedAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.ripple
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors

// telemetry:exempt pure stateless renderer; AnalyticsPort wiring lives in RfidViewModel.

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

/**
 * RFID reader status for the detail screen (distinct from ProfileScreen's RfidRowStatus).
 * Maps to RfidReaderStatus from the app module. Feature-profile does not depend on the
 * app module — RfidViewModel maps the real reader status onto this enum before handing
 * it to the screen for localization.
 */
enum class RfidDetailStatus { READY, PAIRED_NOT_READY, NOT_PAIRED, PERMISSION_NEEDED, BLUETOOTH_OFF }

/** One discovered reader from a scan. Every field is backend/device-provided. */
data class RfidReaderRow(
    val id: String,
    val name: String,
    val detail: String,
    val signalLabel: String,
)

/**
 * Everything the RFID pairing surface renders. The header title and all status/action text
 * are static strings rendered here via stringResource() (keyed off the detail-status enum) —
 * NOT passed in from the ViewModel. Only genuinely dynamic data is state: [readerName] is the
 * real device name from the RfidReaderPort (rendered verbatim, never localized) or null when
 * the port has no standing name.
 */
// @Immutable: discovered: List<RfidReaderRow> otherwise marks this unstable (item 6,
// perf/stability pass).
@Immutable
data class RfidUiState(
    val detailStatus: RfidDetailStatus,
    val connectionState: RfidConnectionState,
    val readerName: String? = null,
    val pairedLabel: String = "Paired",
    val testReadValue: String = "982 000 4512 8830",
    val showHidNote: Boolean = true,
    val showTestRead: Boolean = true,
    val showPrimaryAction: Boolean = true,
    val primaryActionLabel: String? = null,
    val showBluetoothAction: Boolean = true,
    val discovered: List<RfidReaderRow> = emptyList(),
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

/** Localized status label and detail for RFID reader status. */
@Composable
private fun statusTextFor(status: RfidDetailStatus): Pair<String, String> = when (status) {
    RfidDetailStatus.READY ->
        stringResource(R.string.rfid_detail_status_ready) to stringResource(R.string.rfid_detail_status_ready_detail)
    RfidDetailStatus.PAIRED_NOT_READY ->
        stringResource(R.string.rfid_detail_status_paired_not_ready) to stringResource(R.string.rfid_detail_status_paired_not_ready_detail)
    RfidDetailStatus.NOT_PAIRED ->
        stringResource(R.string.rfid_detail_status_not_paired) to stringResource(R.string.rfid_detail_status_not_paired_detail)
    RfidDetailStatus.PERMISSION_NEEDED ->
        stringResource(R.string.rfid_detail_status_permission_needed) to stringResource(R.string.rfid_detail_status_permission_needed_detail)
    RfidDetailStatus.BLUETOOTH_OFF ->
        stringResource(R.string.rfid_detail_status_bluetooth_off) to stringResource(R.string.rfid_detail_status_bluetooth_off_detail)
}

/** Localized action button label for RFID reader status. */
@Composable
private fun actionLabelFor(status: RfidDetailStatus): String = when (status) {
    RfidDetailStatus.READY ->
        stringResource(R.string.rfid_detail_action_test_read)
    RfidDetailStatus.PAIRED_NOT_READY,
    RfidDetailStatus.PERMISSION_NEEDED,
    RfidDetailStatus.BLUETOOTH_OFF ->
        stringResource(R.string.rfid_detail_action_open_bluetooth)
    RfidDetailStatus.NOT_PAIRED ->
        stringResource(R.string.rfid_detail_action_pair_reader)
}

private fun glyphTint(state: RfidConnectionState): Color = when (state) {
    RfidConnectionState.CONNECTED -> MeshaColors.Brand
    RfidConnectionState.SCANNING -> MeshaColors.Brand
    RfidConnectionState.DISCONNECTED -> MeshaColors.Danger
}

/** Status-pill tone (bg, fg) per connection state — mirrors the mock's `.pill` variants. */
private fun statusPillTone(state: RfidConnectionState): Pair<Color, Color> = when (state) {
    RfidConnectionState.CONNECTED -> MeshaColors.OkX to MeshaColors.BrandD
    RfidConnectionState.SCANNING -> MeshaColors.OkX to MeshaColors.BrandD
    RfidConnectionState.DISCONNECTED -> MeshaColors.DangerX to MeshaColors.Danger
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
            .background(MeshaColors.Bg)
            .windowInsetsPadding(WindowInsets.safeDrawing),
        contentPadding = PaddingValues(bottom = 24.dp),
    ) {
        item { RfidHeader() }
        item { RfidDeviceHero(state) }
        if (state.showHidNote) {
            item { RfidInfoBox() }
        }
        if (state.showTestRead) {
            item { RfidTestField(state = state, onEvent = onEvent) }
        }
        if (state.discovered.isNotEmpty()) {
            // MOB-011: Use stable keys instead of index to avoid recomposition on reorder
            items(state.discovered, key = { row -> row.id }, contentType = { "reader_row" }) { row ->
                DiscoveredReaderRow(row = row, onEvent = onEvent)
            }
        }
        if (state.showPrimaryAction) {
            item {
                RfidPrimaryAction(
                    label = state.primaryActionLabel ?: stringResource(R.string.rfid_detail_action_rescan),
                    onEvent = onEvent,
                )
            }
        }
        if (state.showBluetoothAction) {
            item { RfidBluetoothAction(onEvent = onEvent) }
        }
    }
}

@Composable
private fun RfidHeader() {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .padding(start = 12.dp, end = 16.dp, top = 14.dp, bottom = 4.dp),
    ) {
        Box(
            modifier = Modifier
                .size(38.dp)
                .background(MeshaColors.Surf, shape = RoundedCornerShape(13.dp))
                .border(1.dp, MeshaColors.Hair, shape = RoundedCornerShape(13.dp)),
            contentAlignment = Alignment.Center,
        ) {
            Text(text = "‹", color = MeshaColors.Muted, fontSize = 26.sp, fontWeight = FontWeight.W700)
        }
        Spacer(Modifier.width(10.dp))
        Column {
            Text(
                text = stringResource(R.string.profile_settings_label),
                color = MeshaColors.Muted,
                fontSize = 11.sp,
                fontWeight = FontWeight.W700,
            )
            Text(
                text = stringResource(R.string.rfid_title),
                color = MeshaColors.Ink,
                fontSize = 22.sp,
                fontWeight = FontWeight.W700,
            )
        }
    }
}

@Composable
private fun RfidDeviceHero(state: RfidUiState) {
    val (pillBg, pillFg) = statusPillTone(state.connectionState)
    val (statusLabel, _) = statusTextFor(state.detailStatus)
    val deviceName = state.readerName ?: stringResource(R.string.rfid_title)
    val pillLabel = when {
        state.connectionState == RfidConnectionState.CONNECTED -> state.pairedLabel
        state.pairedLabel != "Paired" -> state.pairedLabel
        else -> statusLabel
    }
    Column(
        horizontalAlignment = Alignment.CenterHorizontally,
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 20.dp, bottom = 12.dp),
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
        Text(
            text = deviceName,
            color = MeshaColors.Ink,
            fontSize = 16.sp,
            fontWeight = FontWeight.W700,
            modifier = Modifier.padding(top = 12.dp),
        )
        Box(
            modifier = Modifier
                .padding(top = 9.dp)
                .background(pillBg, shape = RoundedCornerShape(999.dp))
                .padding(horizontal = 10.dp, vertical = 4.dp),
        ) {
            Text(text = pillLabel, color = pillFg, fontSize = 11.sp, fontWeight = FontWeight.W700)
        }
    }
}

@Composable
private fun RfidInfoBox() {
    Text(
        text = stringResource(R.string.rfid_detail_hid_note),
        color = MeshaColors.Muted,
        fontSize = 12.sp,
        lineHeight = 19.sp,
        modifier = Modifier
            .padding(horizontal = 16.dp)
            .fillMaxWidth()
            .background(MeshaColors.Surf, shape = RoundedCornerShape(16.dp))
            .border(1.dp, MeshaColors.Hair, shape = RoundedCornerShape(16.dp))
            .padding(14.dp),
    )
}

@Composable
private fun DiscoveredReaderRow(row: RfidReaderRow, onEvent: (RfidEvent) -> Unit) {
    val isReady = row.signalLabel.equals("Ready", ignoreCase = true) ||
        row.signalLabel.equals("Connected", ignoreCase = true)
    val isDisconnected = row.signalLabel.equals("Disconnected", ignoreCase = true)
    val interactionSource = remember { MutableInteractionSource() }
    val pressed by interactionSource.collectIsPressedAsState()
    val shape = RoundedCornerShape(15.dp)
    val rowBg = when {
        pressed && isReady -> MeshaColors.BrandTint
        pressed && isDisconnected -> MeshaColors.DangerX
        pressed -> MeshaColors.Surf3
        isReady -> MeshaColors.OkX
        isDisconnected -> MeshaColors.DangerX
        else -> MeshaColors.Surf
    }
    val rowBorder = when {
        isReady -> MeshaColors.Brand
        isDisconnected -> MeshaColors.Danger
        else -> MeshaColors.Hair
    }
    val iconColor = when {
        isReady -> MeshaColors.Brand
        isDisconnected -> MeshaColors.Danger
        else -> MeshaColors.Faint
    }
    val signalColor = when {
        isReady -> MeshaColors.BrandD
        isDisconnected -> MeshaColors.Danger
        else -> MeshaColors.Muted
    }
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .padding(start = 16.dp, end = 16.dp, top = 9.dp)
            .fillMaxWidth()
            .heightIn(min = 52.dp)
            .clip(shape)
            .background(rowBg, shape = shape)
            .border(1.dp, rowBorder, shape = shape)
            .clickable(
                interactionSource = interactionSource,
                indication = ripple(bounded = true),
            ) { onEvent(RfidEvent.SelectReader(row.id)) }
            .padding(horizontal = 15.dp, vertical = 11.dp),
    ) {
        Icon(imageVector = MeshaIcons.Bluetooth, contentDescription = null, tint = iconColor, modifier = Modifier.size(16.dp))
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
            color = signalColor,
            fontSize = 12.sp,
            fontWeight = FontWeight.W700,
        )
    }
}

@Composable
private fun RfidPrimaryAction(label: String, onEvent: (RfidEvent) -> Unit) {
    val interactionSource = remember { MutableInteractionSource() }
    val pressed by interactionSource.collectIsPressedAsState()
    val shape = RoundedCornerShape(15.dp)
    val bg = if (pressed) MeshaColors.Surf3 else MeshaColors.Surf2
    val icon = if (label.equals("Done", ignoreCase = true)) MeshaIcons.Check else MeshaIcons.Refresh
    Row(
        horizontalArrangement = Arrangement.Center,
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .padding(start = 16.dp, end = 16.dp, top = 10.dp)
            .fillMaxWidth()
            .heightIn(min = 52.dp)
            .clip(shape)
            .background(bg, shape = shape)
            .border(1.dp, MeshaColors.Hair, shape = shape)
            .clickable(
                interactionSource = interactionSource,
                indication = ripple(bounded = true),
            ) { onEvent(RfidEvent.TestRead) }
            .padding(15.dp),
    ) {
        Icon(imageVector = icon, contentDescription = null, tint = MeshaColors.Ink, modifier = Modifier.size(16.dp))
        Spacer(Modifier.width(8.dp))
        Text(
            text = label,
            color = MeshaColors.Ink,
            fontSize = 15.sp,
            fontWeight = FontWeight.W700,
            fontFamily = FontFamily.Default,
        )
    }
}

@Composable
private fun RfidBluetoothAction(onEvent: (RfidEvent) -> Unit) {
    val interactionSource = remember { MutableInteractionSource() }
    val pressed by interactionSource.collectIsPressedAsState()
    val shape = RoundedCornerShape(15.dp)
    val bg = if (pressed) MeshaColors.Surf2 else MeshaColors.Surf
    Row(
        horizontalArrangement = Arrangement.Center,
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .padding(start = 16.dp, end = 16.dp, top = 9.dp)
            .fillMaxWidth()
            .heightIn(min = 48.dp)
            .clip(shape)
            .background(bg, shape = shape)
            .border(1.dp, MeshaColors.Hair, shape = shape)
            .clickable(
                interactionSource = interactionSource,
                indication = ripple(bounded = true),
            ) { onEvent(RfidEvent.Pair) }
            .padding(13.dp),
    ) {
        Icon(imageVector = MeshaIcons.Bluetooth, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(16.dp))
        Spacer(Modifier.width(8.dp))
        Text(
            text = stringResource(R.string.rfid_detail_action_open_bluetooth),
            color = MeshaColors.Ink,
            fontSize = 14.sp,
            fontWeight = FontWeight.W700,
            fontFamily = FontFamily.Default,
        )
    }
}

@Composable
private fun RfidTestField(state: RfidUiState, onEvent: (RfidEvent) -> Unit) {
    val interactionSource = remember { MutableInteractionSource() }
    val pressed by interactionSource.collectIsPressedAsState()
    val shape = RoundedCornerShape(14.dp)
    val bg = if (pressed) MeshaColors.Surf2 else MeshaColors.Surf
    Column(
        modifier = Modifier
            .padding(start = 16.dp, end = 16.dp, top = 16.dp)
            .fillMaxWidth(),
    ) {
        Text(
            text = stringResource(R.string.rfid_detail_test_label),
            color = MeshaColors.Muted,
            fontSize = 12.sp,
            fontWeight = FontWeight.W700,
            modifier = Modifier.padding(start = 2.dp, bottom = 7.dp),
        )
        Row(
            verticalAlignment = Alignment.CenterVertically,
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(min = 50.dp)
                .clip(shape)
                .background(bg, shape = shape)
                .border(1.dp, MeshaColors.Hair, shape = shape)
                .clickable(
                    interactionSource = interactionSource,
                    indication = ripple(bounded = true),
                ) { onEvent(RfidEvent.TestRead) }
                .padding(horizontal = 15.dp, vertical = 13.dp),
        ) {
            Text(text = "↳", color = MeshaColors.Faint, fontSize = 15.sp, fontFamily = FontFamily.Monospace)
            Spacer(Modifier.width(8.dp))
            Text(
                text = state.testReadValue,
                color = MeshaColors.Ink,
                fontSize = 14.sp,
                fontWeight = FontWeight.W600,
                fontFamily = FontFamily.Monospace,
                modifier = Modifier.weight(1f),
            )
            Icon(imageVector = MeshaIcons.Check, contentDescription = null, tint = MeshaColors.Brand, modifier = Modifier.size(16.dp))
        }
    }
}

@Preview(backgroundColor = 0xFF0B100D, showBackground = true)
@Composable
private fun RfidScreenPreview() {
    GoatOsTheme {
        RfidScreen(
            state = RfidUiState(
                detailStatus = RfidDetailStatus.READY,
                connectionState = RfidConnectionState.CONNECTED,
                readerName = "Chainway R3", // a real device name from the port renders verbatim
            ),
        )
    }
}
