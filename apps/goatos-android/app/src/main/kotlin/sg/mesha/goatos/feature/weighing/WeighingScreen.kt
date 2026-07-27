package sg.mesha.goatos.feature.weighing

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextField
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

data class WeighingUiState(
    val title: String = "Weighing",
    val scopeLabel: String = "",
    val hasScope: Boolean = false,
    val assignments: List<WeighingAssignmentUiRow> = emptyList(),
    val visibleRows: List<WeighingRosterUiRow> = emptyList(),
    val totalExpected: Int = 0,
    val individualDrafts: List<WeighingDraftUiRow> = emptyList(),
    val shedDrafts: List<WeighingDraftUiRow> = emptyList(),
    val selectedAnimalId: String? = null,
    val selectedAnimalLabel: String? = null,
    val scanInput: String = "",
    val weightInput: String = "",
    val message: String? = null,
    val actionInFlight: Boolean = false,
) {
    val individualCompleted: Int get() = individualDrafts.count { it.readyToSubmit }
    val progress: Float get() = if (totalExpected <= 0) 0f else individualCompleted.toFloat() / totalExpected.toFloat()
    val canRecordIndividual: Boolean get() =
        hasScope && !actionInFlight && !selectedAnimalId.isNullOrBlank() && weightInput.toDoubleOrNull()?.let { it > 0.0 } == true
}

data class WeighingRosterUiRow(
    val id: String,
    val displayAnimalId: String,
    val expectedLocationLabel: String,
    val actualLocationLabel: String?,
    val status: String,
    val availabilityStatus: String?,
    val wrongShed: Boolean,
)

data class WeighingAssignmentUiRow(
    val campaignId: String,
    val workGroupId: String,
    val campaignShedId: String,
    val label: String,
    val category: String,
    val status: String,
    val expectedCount: Int,
    val periodLabel: String,
)

data class WeighingDraftUiRow(
    val id: String,
    val label: String,
    val proofReady: Boolean,
    val readyToSubmit: Boolean,
)

@Composable
fun WeighingScreen(
    state: WeighingUiState,
    onScanInputChange: (String) -> Unit = {},
    onScanSubmit: () -> Unit = {},
    onWeightChange: (String) -> Unit = {},
    onRecordIndividual: () -> Unit = {},
    onOpenAssignment: (WeighingAssignmentUiRow) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Scaffold(modifier = modifier.fillMaxSize()) { padding ->
        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .padding(horizontal = 16.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text(
                        text = state.title,
                        style = MaterialTheme.typography.headlineSmall,
                        fontWeight = FontWeight.SemiBold,
                    )
                    if (state.scopeLabel.isNotBlank()) {
                        Text(
                            text = state.scopeLabel,
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            maxLines = 2,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                    if (state.hasScope) {
                        LinearProgressIndicator(
                            progress = { state.progress },
                            modifier = Modifier.fillMaxWidth(),
                        )
                        Text(
                            text = "${state.individualCompleted}/${state.totalExpected} individual weights ready",
                            style = MaterialTheme.typography.labelLarge,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        WeighingCapturePanel(
                            state = state,
                            onScanInputChange = onScanInputChange,
                            onScanSubmit = onScanSubmit,
                            onWeightChange = onWeightChange,
                            onRecordIndividual = onRecordIndividual,
                        )
                    } else {
                        if (state.assignments.isEmpty()) {
                            Text(
                                text = "No assigned weighing work groups are available.",
                                style = MaterialTheme.typography.bodyMedium,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                    state.message?.takeIf { it.isNotBlank() }?.let {
                        Text(
                            text = it,
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }

            if (!state.hasScope && state.assignments.isNotEmpty()) {
                item { SectionTitle("Assigned work groups") }
                items(state.assignments, key = { it.campaignShedId }) { row ->
                    AssignmentRow(row = row, onOpen = { onOpenAssignment(row) })
                }
            }

            if (state.shedDrafts.isNotEmpty()) {
                item { SectionTitle("Shed / partition result") }
                items(state.shedDrafts, key = { it.id }) { row ->
                    DraftRow(row)
                }
            }

            if (state.individualDrafts.isNotEmpty()) {
                item { SectionTitle("Queued animal weights") }
                items(state.individualDrafts, key = { it.id }) { row ->
                    DraftRow(row)
                }
            }

            if (state.visibleRows.isNotEmpty()) {
                item { SectionTitle("Roster window") }
                items(state.visibleRows, key = { it.id }) { row ->
                    RosterRow(row)
                }
            }
        }
    }
}

@Composable
private fun AssignmentRow(row: WeighingAssignmentUiRow, onOpen: () -> Unit) {
    Card(
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow),
        shape = MaterialTheme.shapes.small,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(12.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
            ) {
                Text(
                    text = row.label,
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.SemiBold,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                Text(
                    text = row.status,
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Text(
                text = listOf(row.category, "${row.expectedCount} expected", row.periodLabel)
                    .filter { it.isNotBlank() }
                    .joinToString(" | "),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Button(onClick = onOpen, modifier = Modifier.fillMaxWidth()) {
                Text("Open")
            }
        }
    }
}

@Composable
private fun WeighingCapturePanel(
    state: WeighingUiState,
    onScanInputChange: (String) -> Unit,
    onScanSubmit: () -> Unit,
    onWeightChange: (String) -> Unit,
    onRecordIndividual: () -> Unit,
) {
    Card(
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow),
        shape = MaterialTheme.shapes.small,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(12.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Text(
                text = state.selectedAnimalLabel ?: "Scan an animal tag",
                style = MaterialTheme.typography.titleSmall,
                fontWeight = FontWeight.SemiBold,
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                TextField(
                    value = state.scanInput,
                    onValueChange = onScanInputChange,
                    label = { Text("RFID / animal tag") },
                    singleLine = true,
                    modifier = Modifier.weight(1f),
                )
                OutlinedButton(
                    onClick = onScanSubmit,
                    enabled = !state.actionInFlight && state.scanInput.isNotBlank(),
                ) {
                    Text("Match")
                }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                TextField(
                    value = state.weightInput,
                    onValueChange = onWeightChange,
                    label = { Text("Weight kg") },
                    singleLine = true,
                    modifier = Modifier.weight(1f),
                )
                Button(
                    onClick = onRecordIndividual,
                    enabled = state.canRecordIndividual,
                ) {
                    Text("Record")
                }
            }
        }
    }
}

@Composable
private fun SectionTitle(text: String) {
    Text(
        text = text,
        style = MaterialTheme.typography.titleSmall,
        fontWeight = FontWeight.SemiBold,
        modifier = Modifier.padding(top = 8.dp),
    )
}

@Composable
private fun RosterRow(row: WeighingRosterUiRow) {
    Card(
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow),
        shape = MaterialTheme.shapes.small,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(12.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
            ) {
                Text(
                    text = row.displayAnimalId,
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.SemiBold,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    text = row.status,
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Text(
                text = "Expected: ${row.expectedLocationLabel}",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            row.actualLocationLabel?.takeIf { it.isNotBlank() && it != row.expectedLocationLabel }?.let {
                Text(
                    text = "Current: $it",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error,
                )
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                if (row.wrongShed) {
                    AssistChip(onClick = {}, label = { Text("Wrong shed") })
                }
                row.availabilityStatus?.takeIf { it.isNotBlank() }?.let {
                    AssistChip(onClick = {}, label = { Text(it) })
                }
            }
        }
    }
}

@Composable
private fun DraftRow(row: WeighingDraftUiRow) {
    Card(
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow),
        shape = MaterialTheme.shapes.small,
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(12.dp),
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = row.label,
                    style = MaterialTheme.typography.bodyMedium,
                    fontWeight = FontWeight.Medium,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    text = if (row.proofReady) "Proof ready" else "Proof required",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Text(
                text = if (row.readyToSubmit) "Ready" else "Draft",
                style = MaterialTheme.typography.labelLarge,
                color = if (row.readyToSubmit) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}
