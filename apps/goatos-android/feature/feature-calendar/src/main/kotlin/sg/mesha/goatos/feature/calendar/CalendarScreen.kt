package sg.mesha.goatos.feature.calendar

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp

/**
 * Universal landing for every role (screens.md): the calendar of due work. Week /
 * month / history segments come from the backend presentationConfig, never a
 * client `role ==` check. Card tap branches by role via backend-supplied targets.
 * Placeholder content until the calendar contract is wired in the feature pass.
 */
@Composable
fun CalendarScreen(modifier: Modifier = Modifier) {
    Column(modifier = modifier.fillMaxSize().padding(16.dp)) {
        Text(text = "Calendar", style = MaterialTheme.typography.headlineSmall)
        Text(text = "Due work — universal landing", style = MaterialTheme.typography.bodyMedium)
    }
}
