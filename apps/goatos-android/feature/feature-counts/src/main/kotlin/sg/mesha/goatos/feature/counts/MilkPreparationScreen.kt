package sg.mesha.goatos.feature.counts

// telemetry:exempt presentational renderer; capture/write telemetry lives in MilkPreparationViewModel.

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.theme.MeshaColors

@Immutable data class MilkPreparationParkUi(val id: String, val label: String)
@Immutable data class MilkPreparationStepUi(val code: String, val label: String, val captured: Boolean = false, val capturing: Boolean = false)
@Immutable data class MilkPreparationUiState(
    val preparationDate: String = "",
    val parks: List<MilkPreparationParkUi> = emptyList(),
    val selectedParkId: String = "",
    val goatMilkUsed: Boolean = true,
    val steps: List<MilkPreparationStepUi> = emptyList(),
    val loadingParks: Boolean = true,
    val message: String? = null,
    val submitting: Boolean = false,
	val submitted: Boolean = false,
) {
    val hasCaptured: Boolean get() = steps.any { it.captured }
    val canSubmit: Boolean get() = selectedParkId.isNotBlank() && steps.isNotEmpty() && steps.all { it.captured } && !submitting && !submitted
}

sealed interface MilkPreparationEvent {
    data class SelectPark(val parkId: String) : MilkPreparationEvent
    data class SetGoatMilkUsed(val used: Boolean) : MilkPreparationEvent
    data class CaptureStep(val stepCode: String) : MilkPreparationEvent
    data object Submit : MilkPreparationEvent
}

@Composable
fun MilkPreparationScreen(state: MilkPreparationUiState, onEvent: (MilkPreparationEvent) -> Unit) {
    LazyColumn(
        modifier = Modifier.fillMaxSize().background(MeshaColors.PageBg).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        item {
            Text("Milk Preparation", color = MeshaColors.Ink, fontSize = 22.sp, fontWeight = FontWeight.W800)
            Text("${state.preparationDate} · Each step needs its own live video. One verifier reviews the full set.", color = MeshaColors.Muted, fontSize = 13.sp)
        }
        item {
            Card(modifier = Modifier.fillMaxWidth(), shape = RoundedCornerShape(14.dp)) {
                Column(Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text("Park", fontWeight = FontWeight.W700)
                    if (state.loadingParks) CircularProgressIndicator()
                    state.parks.forEach { park ->
                        Row(
                            Modifier.fillMaxWidth().clickable(enabled = !state.hasCaptured) { onEvent(MilkPreparationEvent.SelectPark(park.id)) },
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            RadioButton(selected = state.selectedParkId == park.id, onClick = { if (!state.hasCaptured) onEvent(MilkPreparationEvent.SelectPark(park.id)) })
                            Text(park.label)
                        }
                    }
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Checkbox(checked = state.goatMilkUsed, onCheckedChange = { if (!state.hasCaptured) onEvent(MilkPreparationEvent.SetGoatMilkUsed(it)) }, enabled = !state.hasCaptured)
                        Text("Goat milk is used in this preparation")
                    }
                }
            }
        }
        items(state.steps, key = { it.code }) { step ->
            Card(modifier = Modifier.fillMaxWidth(), shape = RoundedCornerShape(14.dp)) {
                Row(Modifier.padding(14.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    Column(Modifier.weight(1f)) {
                        Text(step.label, color = MeshaColors.Ink, fontWeight = FontWeight.W700)
                        Text(if (step.captured) "Video saved" else "Live-camera video required", color = if (step.captured) MeshaColors.Ok else MeshaColors.Muted, fontSize = 12.sp)
                    }
                    Button(onClick = { onEvent(MilkPreparationEvent.CaptureStep(step.code)) }, enabled = !step.captured && !step.capturing && state.selectedParkId.isNotBlank()) {
                        Text(if (step.capturing) "Opening…" else if (step.captured) "Recorded" else "Record")
                    }
                }
            }
        }
        item {
            Button(onClick = { onEvent(MilkPreparationEvent.Submit) }, enabled = state.canSubmit, modifier = Modifier.fillMaxWidth()) {
                Text(if (state.submitting) "Submitting…" else "Submit all videos for verification")
            }
            state.message?.let { Text(it, color = if (it.startsWith("Submitted")) MeshaColors.Ok else Color.Unspecified, modifier = Modifier.padding(top = 8.dp)) }
        }
    }
}
