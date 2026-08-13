package sg.mesha.goatos.feature.counts

// telemetry:exempt pure stateless renderer; RfidPromoteViewModel (in :app) owns the
// counts_rfid_promote_* AnalyticsEvents (open / submitted) and the CrashReporter non-fatal on every
// promote-enqueue failure.

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.foundation.border
import sg.mesha.goatos.core.designsystem.theme.MeshaColors

/**
 * Birth's `Tag the kid` RFID screen — a hosted destination with Up/Back and no root chrome.
 *
 * This is where an operator assigns a permanent RFID to a goat that currently carries a temporary
 * tag. **Promote** is the only action that retags the animal: it enqueues the write to the durable
 * outbox, so a press in a dead-signal shed is safe and replays under one stable idempotency key
 * instead of retagging twice. The identity context comes from the Room-backed Birth workflow.
 */

/** Which of the two permanent-RFID inputs a Bluetooth scan is being routed into. */
enum class RfidPromoteField { PRIMARY, SECONDARY }

@Immutable
data class RfidPromoteUiState(
    val goatId: String = "",
    val loading: Boolean = true,
    val notFound: Boolean = false,
    val displayId: String = "",
    val temporaryIdentifier: String = "",
    val locationDisplay: String = "",
    /** The permanent RFID the operator is typing (animal_identifier_1, required). */
    val rfidInput: String = "",
    /** An optional second permanent RFID (animal_identifier_2). Blank = attach only the primary. */
    val rfid2Input: String = "",
    /**
     * Which input is currently listening to the Bluetooth reader, or null when nothing is scanning.
     * Only one at a time — Android holds a single BT-HID connection either way.
     */
    val scanningField: RfidPromoteField? = null,
    val inputError: String? = null,
    /** The promote write result. */
    val result: CountsWriteResultUi = CountsWriteResultUi(),
    val canSubmit: Boolean = false,
)

sealed interface RfidPromoteEvent {
    data class RfidChanged(val value: String) : RfidPromoteEvent
    data class Rfid2Changed(val value: String) : RfidPromoteEvent

    /**
     * Start/stop the Bluetooth RFID reader for one input. Tapping the field that is already
     * scanning stops it; tapping the other one hands the reader over.
     */
    data class ToggleRfidScan(val field: RfidPromoteField) : RfidPromoteEvent
    data object Submit : RfidPromoteEvent
    data object Back : RfidPromoteEvent
}

@Composable
fun RfidPromoteScreen(
    state: RfidPromoteUiState,
    onEvent: (RfidPromoteEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        CountsFormHeader(
            title = "Tag the kid",
            subtitle = "Assign the permanent RFID",
            onBack = { onEvent(RfidPromoteEvent.Back) },
        )
        Column(
            modifier = Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            when {
                state.loading -> Text("Loading goat…", color = MeshaColors.Muted, fontSize = 14.sp)
                state.notFound -> Text(
                    "This kid no longer has a temporary identifier. The permanent RFID may already be assigned.",
                    color = MeshaColors.Muted,
                    fontSize = 14.sp,
                )
                else -> {
                    GoatCard(state)
                    // Both permanent identifiers are scannable: promoting happens with the new ear
                    // tag in hand, so the operator holds the Bluetooth reader to it instead of
                    // transcribing a 15-digit RFID. Typing stays available — a flat reader battery
                    // must never block a retag.
                    CountsRfidField(
                        value = state.rfidInput,
                        onValueChange = { onEvent(RfidPromoteEvent.RfidChanged(it)) },
                        label = "Permanent RFID",
                        scanning = state.scanningField == RfidPromoteField.PRIMARY,
                        onToggleScan = { onEvent(RfidPromoteEvent.ToggleRfidScan(RfidPromoteField.PRIMARY)) },
                        required = true,
                        isError = state.inputError != null,
                        supporting = state.inputError
                            ?: stringResource(R.string.counts_rfid_promote_scan_hint),
                    )
                    CountsRfidField(
                        value = state.rfid2Input,
                        onValueChange = { onEvent(RfidPromoteEvent.Rfid2Changed(it)) },
                        label = "Second RFID",
                        scanning = state.scanningField == RfidPromoteField.SECONDARY,
                        onToggleScan = { onEvent(RfidPromoteEvent.ToggleRfidScan(RfidPromoteField.SECONDARY)) },
                        required = false,
                        supporting = "Optional — leave blank if the goat has only one tag.",
                    )
                    if (state.result.status != CountsWriteStatus.IDLE) {
                        CountsResultBanner(state.result)
                    }
                    CountsSubmitButton(
                        label = when (state.result.status) {
                            CountsWriteStatus.QUEUED, CountsWriteStatus.SYNCED -> "Done"
                            else -> "Save permanent RFID"
                        },
                        enabled = state.canSubmit,
                        onClick = { onEvent(RfidPromoteEvent.Submit) },
                    )
                }
            }
        }
    }
}

@Composable
private fun GoatCard(state: RfidPromoteUiState) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(modifier = Modifier.fillMaxWidth()) {
            Column(modifier = Modifier.weight(1f)) {
                Text("Goat", color = MeshaColors.Muted, fontSize = 11.sp)
                Text(state.displayId, color = MeshaColors.Ink, fontSize = 15.sp, fontWeight = FontWeight.W700)
            }
            if (state.locationDisplay.isNotBlank()) {
                Column {
                    Text("Location", color = MeshaColors.Muted, fontSize = 11.sp)
                    Text(state.locationDisplay, color = MeshaColors.Ink, fontSize = 15.sp, fontWeight = FontWeight.W700)
                }
            }
        }
        if (state.temporaryIdentifier.isNotBlank()) {
            Text(
                text = "Temp tag · ${state.temporaryIdentifier}",
                color = MeshaColors.Warn,
                fontSize = 11.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier
                    .clip(RoundedCornerShape(999.dp))
                    .background(MeshaColors.WarnX)
                    .padding(horizontal = 10.dp, vertical = 3.dp),
            )
            Text(
                text = "This temporary tag will be retired when the permanent RFID is saved.",
                color = MeshaColors.Faint,
                fontSize = 11.sp,
            )
        }
    }
}
