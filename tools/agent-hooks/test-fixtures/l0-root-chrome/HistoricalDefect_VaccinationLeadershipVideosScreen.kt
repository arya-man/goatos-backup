// Historical defect (2026-08-03): /vaccination/videos shipped without MeshaScreenHeader.
// This fixture represents what the screen looked like before the fix was applied.
// The guard must FAIL on this.

package sg.mesha.goatos.feature.vaccination.leadership

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.runtime.Composable
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipVideosUiState

/**
 * Historical defect: read-only gallery of vaccination verification items.
 * This screen was built WITHOUT MeshaScreenHeader, RefreshOnResume, or any chrome affordance.
 * The maintainer opened it on his phone and found a bare list floating above the bottom bar
 * with no title, no drawer access, and no way to refresh.
 *
 * Guard failure: L0 root screens MUST render MeshaScreenHeader.
 * Guard failure: read screens MUST call RefreshOnResume.
 */
@Composable
fun VaccinationLeadershipVideosScreen(
    state: VaccinationLeadershipVideosUiState,
    onEvent: (Any) -> Unit,
    modifier: Modifier = Modifier,
) {
    Box(
        modifier = modifier.fillMaxSize(),
    ) {
        when {
            state.items.isEmpty() -> {
                // Empty state rendering
            }
            else -> {
                Column {
                    LazyColumn {
                        itemsIndexed(state.items) { _, item ->
                            // Render each video card
                        }
                    }
                }
            }
        }
    }
}
