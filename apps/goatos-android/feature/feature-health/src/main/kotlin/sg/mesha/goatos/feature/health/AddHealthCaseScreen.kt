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
    data class SelectDisease(val diseaseKey: String) : AddHealthCaseEvent
    data class SelectStartDate(val date: String) : AddHealthCaseEvent
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
    var diseaseMenuOpen by remember { mutableStateOf(false) }
    var datePickerOpen by remember { mutableStateOf(false) }
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
            item("disease-title") { FieldTitle("2. Select disease") }
            item("disease") {
                Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Box(Modifier.fillMaxWidth()) {
                        OutlinedButton(
                            onClick = { diseaseMenuOpen = true },
                            enabled = state.diseases.isNotEmpty(),
                            modifier = Modifier.fillMaxWidth(),
                        ) {
                            Text(state.diseases.firstOrNull { it.key == state.diseaseKey }?.label ?: "Choose disease")
                        }
                        DropdownMenu(
                            expanded = diseaseMenuOpen,
                            onDismissRequest = { diseaseMenuOpen = false },
                            modifier = Modifier.fillMaxWidth().heightIn(max = 320.dp),
                        ) {
                            state.diseases.forEach { disease ->
                                DropdownMenuItem(
                                    text = { Text(disease.label) },
                                    onClick = {
                                        diseaseMenuOpen = false
                                        onEvent(AddHealthCaseEvent.SelectDisease(disease.key))
                                    },
                                )
                            }
                        }
                    }
                    if (state.diseases.isEmpty()) Text("Loading Health protocols…", color = MeshaColors.Muted)
                }
            }
            item("date-title") { FieldTitle("3. Sickness start date") }
            item("date") {
                OutlinedButton(onClick = { datePickerOpen = true }, modifier = Modifier.fillMaxWidth()) {
                    Text(state.startDate)
                }
            }
            item("submit") {
                Button(
                    onClick = { onEvent(AddHealthCaseEvent.Submit) },
                    enabled = state.canSubmit && !state.submitting,
                    modifier = Modifier.fillMaxWidth(),
                ) { Text(if (state.submitting) "Saving…" else "Start treatment plan") }
            }
        }
    }

    if (datePickerOpen) {
        val selected = runCatching {
            LocalDate.parse(state.startDate).atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli()
        }.getOrNull()
        val picker = rememberDatePickerState(initialSelectedDateMillis = selected)
        DatePickerDialog(
            onDismissRequest = { datePickerOpen = false },
            confirmButton = {
                TextButton(onClick = {
                    picker.selectedDateMillis?.let { millis ->
                        onEvent(AddHealthCaseEvent.SelectStartDate(Instant.ofEpochMilli(millis).atZone(ZoneOffset.UTC).toLocalDate().toString()))
                    }
                    datePickerOpen = false
                }) { Text("Select") }
            },
            dismissButton = { TextButton(onClick = { datePickerOpen = false }) { Text("Cancel") } },
        ) { DatePicker(picker) }
    }
}

@Composable
private fun FieldTitle(value: String) {
    Text(value, color = MeshaColors.Ink, fontWeight = FontWeight.Bold)
}
