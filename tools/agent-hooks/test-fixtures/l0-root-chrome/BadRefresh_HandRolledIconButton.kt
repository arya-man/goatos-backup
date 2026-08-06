// Bad practice: hand-rolled refresh IconButton instead of SyncIconButton.
// The guard must FAIL on this.

package sg.mesha.goatos.feature.weighing.leadership

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.IconButton
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.Icon
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.ui.RefreshOnResume

/**
 * Bad practice: attempts to use hand-rolled IconButton for refresh.
 * Problems:
 * - Hand-rolled refresh IconButton doesn't auto-disable while syncing
 * - Duplicates the icon rotation logic that SyncIconButton owns
 * - Inconsistent UX across screens (some screens may use different animations)
 *
 * Guard failure: refresh buttons must use SyncIconButton, never hand-rolled IconButton.
 */
@Composable
fun WeighingLeadershipVideosScreen(
    state: Any,
    onEvent: (Any) -> Unit,
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(Any()) }

    Column(modifier = modifier.fillMaxSize()) {
        MeshaScreenHeader(
            title = "Weighing Videos",
            actions = {
                // WRONG: hand-rolled IconButton instead of SyncIconButton
                IconButton(
                    onClick = { onEvent(Any()) },
                ) {
                    Icon(
                        imageVector = Icons.Outlined.Refresh,
                        contentDescription = "Refresh",
                    )
                }
            },
        )

        LazyColumn {
            // Items
        }
    }
}
