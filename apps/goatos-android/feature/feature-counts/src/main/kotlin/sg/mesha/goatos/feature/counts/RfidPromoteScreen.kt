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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.foundation.border
import sg.mesha.goatos.core.designsystem.theme.MeshaColors

/**
 * The promote screen (`/counts/promote/{goat_id}`) — an L2 hosted destination with Up/Back and no
 * root chrome (Android navigation-stack invariant).
 *
 * This is where an operator assigns a permanent RFID to a goat that currently carries a temporary
 * tag. **Promote** is the only action that retags the animal: it enqueues the write to the durable
 * outbox, so a press in a dead-signal shed is safe and replays under one stable idempotency key
 * instead of retagging twice. The goat detail is read from the Room-cached awaiting-RFID row
 * (offline-first open): the operator tapped a row already in Room, so no refetch is needed.
 */

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
    val inputError: String? = null,
    /** The promote write result. */
    val result: CountsWriteResultUi = CountsWriteResultUi(),
    val canSubmit: Boolean = false,
)

sealed interface RfidPromoteEvent {
    data class RfidChanged(val value: String) : RfidPromoteEvent
    data class Rfid2Changed(val value: String) : RfidPromoteEvent
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
            title = "Promote to RFID",
            subtitle = "Assign a permanent tag",
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
                    "This goat is no longer awaiting a permanent RFID. It may have been promoted already.",
                    color = MeshaColors.Muted,
                    fontSize = 14.sp,
                )
                else -> {
                    GoatCard(state)
                    CountsTextField(
                        value = state.rfidInput,
                        onValueChange = { onEvent(RfidPromoteEvent.RfidChanged(it)) },
                        label = "Permanent RFID",
                        required = true,
                        isError = state.inputError != null,
                        supporting = state.inputError,
                    )
                    CountsTextField(
                        value = state.rfid2Input,
                        onValueChange = { onEvent(RfidPromoteEvent.Rfid2Changed(it)) },
                        label = "Second RFID",
                        required = false,
                        supporting = "Optional — leave blank if the goat has only one tag.",
                    )
                    if (state.result.status != CountsWriteStatus.IDLE) {
                        CountsResultBanner(state.result)
                    }
                    CountsSubmitButton(
                        label = when (state.result.status) {
                            CountsWriteStatus.QUEUED, CountsWriteStatus.SYNCED -> "Done"
                            else -> "Promote to permanent RFID"
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
                text = "This temporary tag will be retired when you promote.",
                color = MeshaColors.Faint,
                fontSize = 11.sp,
            )
        }
    }
}
