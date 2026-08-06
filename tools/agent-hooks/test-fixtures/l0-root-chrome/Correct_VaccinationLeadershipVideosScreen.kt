// Correct implementation: L0 root screen with all required chrome.
// The guard must PASS on this.

package sg.mesha.goatos.feature.vaccination.leadership

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipVideosUiState

/**
 * Correct L0 root screen: read-only gallery of vaccination verification items.
 * - Renders MeshaScreenHeader (shell-owned chrome with drawer affordance)
 * - Calls RefreshOnResume on mount (stale-while-revalidate pattern)
 * - Uses SyncIconButton for manual refresh (not hand-rolled IconButton)
 * - Deliberately NOT shared with verifier surface (separate module/VM/route)
 */
@Composable
fun VaccinationLeadershipVideosScreen(
    state: VaccinationLeadershipVideosUiState,
    onEvent: (Any) -> Unit,
    modifier: Modifier = Modifier,
) {
    // Read screen: cached rows show instantly, background refresh on every return.
    RefreshOnResume { onEvent(Any()) }

    Column(modifier = modifier.fillMaxSize()) {
        // L0 root chrome. MeshaScreenHeader is the shell-owned primitive from core-designsystem:
        // it reads LocalDrawerOpener itself and renders the drawer affordance, so this screen
        // never touches drawer state directly.
        MeshaScreenHeader(
            title = state.title,
            actions = {
                SyncIconButton(
                    isSyncing = state.loading,
                    onSync = { onEvent(Any()) },
                )
            },
        )

        Box(
            modifier = Modifier.fillMaxSize(),
        ) {
            when {
                state.items.isEmpty() -> {
                    // Empty state rendering
                }
                else -> {
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
