// telemetry:exempt: pure presentational Compose surface — renders state and forwards user intent
// through onEvent, doing no I/O of its own. Its telemetry is emitted where the behaviour lives, in
// AddHealthCaseViewModel (AnalyticsEvents.HEALTH_CASE_SUBMITTED / HEALTH_WRITE_FAILURE /
// HEALTH_READ_FAILURE).
package sg.mesha.goatos.feature.health

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneOffset
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.theme.MeshaColors

@Immutable
data class HealthGoatUi(
    val goatId: String,
    val displayId: String,
    val tag: String,
    val locationLabel: String,
    val sex: String,
)

@Immutable
data class AddHealthCaseUiState(
    val ageBand: String = "adult",
    val animalQuery: String = "",
    val matches: List<HealthGoatUi> = emptyList(),
    val selectedGoat: HealthGoatUi? = null,
    val lookingUp: Boolean = false,
    val lookupMessage: String? = null,
    val diseases: List<HealthFilterUi> = emptyList(),
    val diseaseKey: String = "",
    val startDate: String = "",
    val submitting: Boolean = false,
    val message: String? = null,
    val canSubmit: Boolean = false,
    val returnToList: Boolean = false,
)

sealed interface AddHealthCaseEvent {
    data class EditQuery(val value: String) : AddHealthCaseEvent
    data object Lookup : AddHealthCaseEvent
    data class SelectGoat(val goatId: String) : AddHealthCaseEvent
    /** Hands the selected animal to the observation form. */
    data class CheckAnimal(val goatId: String) : AddHealthCaseEvent
    data object Submit : AddHealthCaseEvent
    data object NavigationHandled : AddHealthCaseEvent
    data object Back : AddHealthCaseEvent
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AddHealthCaseScreen(
    state: AddHealthCaseUiState,
    onEvent: (AddHealthCaseEvent) -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = "Report sick goat",
            subtitle = if (state.ageBand == "kid") "Kids" else "Adults",
            onBack = { onEvent(AddHealthCaseEvent.Back) },
        )
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            state.message?.let { notice ->
                item("notice") {
                    Card(colors = CardDefaults.cardColors(containerColor = MeshaColors.BrandTint)) {
                        Text(notice, modifier = Modifier.padding(12.dp), color = MeshaColors.BrandD)
                    }
                }
            }
            item("animal-title") { FieldTitle("1. Find the sick goat") }
            item("lookup") {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedTextField(
                        value = state.animalQuery,
                        onValueChange = { onEvent(AddHealthCaseEvent.EditQuery(it)) },
                        label = { Text("RFID or goat tag") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    Button(
                        onClick = { onEvent(AddHealthCaseEvent.Lookup) },
                        enabled = state.animalQuery.isNotBlank() && !state.lookingUp,
                        modifier = Modifier.fillMaxWidth(),
                    ) { Text(if (state.lookingUp) "Searching…" else "Search goat") }
                    state.lookupMessage?.let { Text(it, color = MeshaColors.Warn) }
                }
            }
            // Deduplicated by goatId in AddHealthCaseViewModel.lookup() before reaching state.matches,
            // so this list holds at most one row per goat and goatId is a unique per-row key here.
            items(state.matches, key = { it.goatId }) { goat -> // compose-guard:ignore: goat lookup results deduped by goatId upstream; one row per goat
                val selected = goat.goatId == state.selectedGoat?.goatId
                Card(
                    modifier = Modifier.fillMaxWidth().clickable { onEvent(AddHealthCaseEvent.SelectGoat(goat.goatId)) },
                    colors = CardDefaults.cardColors(
                        containerColor = if (selected) MeshaColors.BrandTint else MeshaColors.Surf,
                    ),
                    shape = RoundedCornerShape(14.dp),
                ) {
                    Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                            Text(goat.displayId, fontWeight = FontWeight.Bold, color = MeshaColors.Ink)
                            if (selected) Text("Selected", color = MeshaColors.Brand, fontWeight = FontWeight.Bold)
                        }
                        Text(goat.tag, color = MeshaColors.Muted)
                        Text(listOf(goat.locationLabel, goat.sex).filter(String::isNotBlank).joinToString(" · "), color = MeshaColors.Faint)
                    }
                }
            }
            // The disease picker is GONE, and its absence is the point of the whole
            // engine: the manager records what they see and the register names the
            // illness. Step 2 is the observation form, and the Health Director
            // confirms whatever it proposes before any treatment opens.
            item("continue") {
                Button(
                    onClick = { state.selectedGoat?.let { onEvent(AddHealthCaseEvent.CheckAnimal(it.goatId)) } },
                    enabled = state.selectedGoat != null,
                    modifier = Modifier.fillMaxWidth(),
                ) { Text("Check this animal") }
            }
            if (state.selectedGoat == null) {
                item("continue-hint") {
                    Text("Find the animal first.", color = MeshaColors.Muted)
                }
            }
        }
    }

}

@Composable
private fun FieldTitle(value: String) {
    Text(value, color = MeshaColors.Ink, fontWeight = FontWeight.Bold)
}
