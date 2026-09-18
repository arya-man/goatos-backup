package sg.mesha.goatos.feature.counts

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Snackbar
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton

/** One startable general SOP as the list renders it. All words are backend copy. */
data class WorkInstructionRowUi(
    val code: String,
    val name: String,
    val description: String,
    val stepCount: Int,
    val starting: Boolean,
)

data class WorkInstructionsUiState(
    val title: String = "",
    val isRefreshing: Boolean = false,
    val loadedOnce: Boolean = false,
    val rows: List<WorkInstructionRowUi> = emptyList(),
    val message: String = "",
    val openWorkflowId: String = "",
)

sealed interface WorkInstructionsEvent {
    data object Refresh : WorkInstructionsEvent
    data class Start(val code: String) : WorkInstructionsEvent
    data object NavigationHandled : WorkInstructionsEvent
    data object DismissMessage : WorkInstructionsEvent
}

/**
 * The Work instructions module (SOP studio phase 2, docs/decisions/sop-studio.md): the general
 * SOPs a person may start by hand -- the gate visitor check and whatever the farm authors on
 * Configuration -> Work instructions. One card per SOP with a Start button; the run opens on the
 * ordinary workflow screen. Refresh on open, sync icon spins while refreshing, cached rows stay
 * visible when the network is down.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun WorkInstructionsScreen(
    state: WorkInstructionsUiState,
    onEvent: (WorkInstructionsEvent) -> Unit,
) {
    RefreshOnResume { onEvent(WorkInstructionsEvent.Refresh) }
    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(state.title.ifBlank { WORK_INSTRUCTIONS_FALLBACK_TITLE }) },
                actions = { SyncIconButton(isSyncing = state.isRefreshing, onSync = { onEvent(WorkInstructionsEvent.Refresh) }) },
            )
        },
        snackbarHost = {
            if (state.message.isNotBlank()) {
                Snackbar(modifier = Modifier.padding(12.dp).testTag("work_instructions_message")) { Text(state.message) }
            }
        },
    ) { padding ->
        if (state.rows.isEmpty()) {
            Column(
                modifier = Modifier.fillMaxSize().padding(padding).padding(24.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.Center,
            ) {
                if (state.isRefreshing && !state.loadedOnce) CircularProgressIndicator() else Text(WORK_INSTRUCTIONS_EMPTY, style = MaterialTheme.typography.bodyMedium)
            }
            return@Scaffold
        }
        LazyColumn(
            modifier = Modifier.fillMaxSize().padding(padding).testTag("work_instructions_list"),
            contentPadding = androidx.compose.foundation.layout.PaddingValues(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            items(state.rows, key = { it.code }) { row ->
                Card(modifier = Modifier.fillMaxWidth().testTag("work_instruction_${row.code}")) {
                    Column(modifier = Modifier.padding(16.dp)) {
                        Text(row.name, style = MaterialTheme.typography.titleMedium)
                        if (row.description.isNotBlank()) {
                            Spacer(Modifier.height(4.dp))
                            Text(row.description, style = MaterialTheme.typography.bodyMedium)
                        }
                        Spacer(Modifier.height(10.dp))
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Text("${row.stepCount} $WORK_INSTRUCTIONS_STEPS", style = MaterialTheme.typography.labelMedium)
                            Spacer(Modifier.weight(1f))
                            Button(onClick = { onEvent(WorkInstructionsEvent.Start(row.code)) }, enabled = !row.starting, modifier = Modifier.testTag("work_instruction_start_${row.code}")) {
                                Text(if (row.starting) WORK_INSTRUCTIONS_STARTING else WORK_INSTRUCTIONS_START)
                            }
                        }
                    }
                }
            }
        }
    }
}

// Screen copy (farm language, no internal words). The title comes from the backend nav label;
// these are the few words the list itself needs before a backend copy row exists for them.
internal const val WORK_INSTRUCTIONS_FALLBACK_TITLE = "Work instructions"
internal const val WORK_INSTRUCTIONS_EMPTY = "No work instructions to start yet."
internal const val WORK_INSTRUCTIONS_STEPS = "steps"
internal const val WORK_INSTRUCTIONS_START = "Start"
internal const val WORK_INSTRUCTIONS_STARTING = "Starting…"
