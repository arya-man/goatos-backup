// Exempt screen: scan/capture screen where RefreshOnResume would disrupt recording.
// The guard must PASS on this (with ignore directive).

package sg.mesha.goatos.feature.scan

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier

/**
 * Scan screen is exempt from RefreshOnResume requirement.
 * Reason: a resume refresh could disrupt mid-scan user input (camera recording).
 * Chrome-guard ignore directives are used for such exemptions.
 *
 * Guard pass: ignore directive prevents MeshaScreenHeader/RefreshOnResume checks.
 */
// chrome-guard:ignore: capture screen, resume refresh would disrupt recording
@Composable
fun ScanScreen(
    state: Any,
    onEvent: (Any) -> Unit,
    modifier: Modifier = Modifier,
) {
    Box(
        modifier = modifier.fillMaxSize(),
    ) {
        // Camera preview and recording UI
    }
}
