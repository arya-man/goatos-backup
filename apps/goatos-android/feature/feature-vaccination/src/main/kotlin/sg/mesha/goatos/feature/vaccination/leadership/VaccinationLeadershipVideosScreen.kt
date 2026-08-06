package sg.mesha.goatos.feature.vaccination.leadership

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.WarningAmber
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipItemUi
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipVideoEvent
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipVideoPlaybackEvent
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipVideosUiState

/**
 * Read-only gallery of vaccination verification items showing full evidence trail
 * (pending/approved/rejected/closed) for leadership review.
 *
 * No verdict controls (approve/reject). Drive-closure cards rendered separately if applicable.
 */
@Composable
fun VaccinationLeadershipVideosScreen(
    state: VaccinationLeadershipVideosUiState,
    onEvent: (VaccinationLeadershipVideoEvent) -> Unit,
    modifier: Modifier = Modifier,
) {
    val lazyListState = rememberLazyListState()

    // Read screen: cached rows show instantly and a background refresh fires on every return to
    // this destination, so a retained ViewModel never leaves stale evidence on screen.
    RefreshOnResume { onEvent(VaccinationLeadershipVideoEvent.Refresh()) }

    // Scroll-driven prefetch: ask for the next page when near the bottom
    LaunchedEffect(lazyListState) {
        snapshotFlow { lazyListState.layoutInfo.visibleItemsInfo.lastOrNull()?.index }
            .map { it ?: -1 }
            .distinctUntilChanged()
            .collect { lastVisibleIndex ->
                if (lastVisibleIndex >= 0) {
                    onEvent(VaccinationLeadershipVideoEvent.ItemVisible(lastVisibleIndex))
                }
            }
    }

    Column(modifier = modifier.fillMaxSize()) {
        // L0 root chrome. MeshaScreenHeader is the SHELL-owned primitive from core-designsystem:
        // it reads LocalDrawerOpener itself and renders the drawer affordance, so this screen
        // never touches drawer state directly. It is deliberately NOT a feature-verify composable
        // -- this module shares no UI code with the verifier surface.
        MeshaScreenHeader(
            title = state.title,
            actions = {
                SyncIconButton(
                    isSyncing = state.loading,
                    onSync = { onEvent(VaccinationLeadershipVideoEvent.Refresh()) },
                )
            },
        )

    Box(
        modifier = Modifier.fillMaxSize(),
    ) {
        when {
            state.error != null && state.items.isEmpty() -> {
                // Failure when nothing is cached
                Column(
                    modifier = Modifier
                        .fillMaxSize()
                        .padding(16.dp),
                    horizontalAlignment = Alignment.CenterHorizontally,
                    verticalArrangement = Arrangement.Center,
                ) {
                    Icon(
                        imageVector = Icons.Outlined.WarningAmber,
                        contentDescription = null,
                        modifier = Modifier.padding(bottom = 16.dp),
                        tint = MaterialTheme.colorScheme.error,
                    )
                    Text(
                        text = state.error ?: "Error loading videos",
                        style = MaterialTheme.typography.bodyMedium,
                        textAlign = TextAlign.Center,
                        color = MaterialTheme.colorScheme.error,
                    )
                }
            }

            state.loading && state.items.isEmpty() -> {
                // Loading spinner when nothing cached yet
                Box(
                    modifier = Modifier.fillMaxSize(),
                    contentAlignment = Alignment.Center,
                ) {
                    CircularProgressIndicator()
                }
            }

            state.items.isEmpty() -> {
                // Empty state with cached data
                Column(
                    modifier = Modifier
                        .fillMaxSize()
                        .padding(16.dp),
                    horizontalAlignment = Alignment.CenterHorizontally,
                    verticalArrangement = Arrangement.Center,
                ) {
                    Text(
                        text = "No videos yet",
                        style = MaterialTheme.typography.bodyMedium,
                        textAlign = TextAlign.Center,
                    )
                }
            }

            else -> {
                // Gallery of verification items
                LazyColumn(
                    modifier = Modifier.fillMaxSize(),
                    state = lazyListState,
                    contentPadding = PaddingValues(8.dp),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    itemsIndexed(
                        items = state.items,
                        key = { _, item -> item.id },
                    ) { index, item ->
                        VaccinationLeadershipVideoItemCard(
                            item = item,
                            onPlayback = { event ->
                                onEvent(VaccinationLeadershipVideoEvent.PlaybackEvent(event))
                            },
                        )
                    }

                    if (state.loadingMore) {
                        item {
                            Box(
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .padding(16.dp),
                                contentAlignment = Alignment.Center,
                            ) {
                                CircularProgressIndicator(
                                    modifier = Modifier.padding(16.dp),
                                )
                            }
                        }
                    }
                }

                // Stale notice overlay
                if (state.staleNotice.isNotEmpty()) {
                    Box(
                        modifier = Modifier
                            .align(Alignment.TopCenter)
                            .fillMaxWidth()
                            .padding(8.dp),
                        contentAlignment = Alignment.TopCenter,
                    ) {
                        Text(
                            text = state.staleNotice,
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.secondary,
                            textAlign = TextAlign.Center,
                        )
                    }
                }
            }
        }
    }
    }
}

/**
 * One verification item card in the leadership gallery.
 * Shows status badge, metadata, and video thumbnails/links without any verdict controls.
 */
@Composable
fun VaccinationLeadershipVideoItemCard(
    item: VaccinationLeadershipItemUi,
    onPlayback: (VaccinationLeadershipVideoPlaybackEvent) -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .padding(8.dp),
    ) {
        // Header: title and status badge
        Text(
            text = item.title,
            style = MaterialTheme.typography.titleMedium,
            modifier = Modifier.padding(bottom = 4.dp),
        )
        Text(
            text = item.statusLabel,
            style = MaterialTheme.typography.labelSmall,
            modifier = Modifier.padding(bottom = 8.dp),
        )

        // Metadata
        Text(
            text = item.timestamp,
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.secondary,
        )

        // Summary (user-facing, no technical vocabulary)
        if (item.summary.isNotEmpty()) {
            Text(
                text = item.summary,
                style = MaterialTheme.typography.bodySmall,
                modifier = Modifier.padding(top = 4.dp),
            )
        }

        // Video links
        if (item.videoUrls.isNotEmpty()) {
            Text(
                text = "${item.proofCount} proof${if (item.proofCount != 1) "s" else ""}",
                style = MaterialTheme.typography.labelSmall,
                modifier = Modifier.padding(top = 8.dp),
                color = MaterialTheme.colorScheme.primary,
            )
        }
    }
}
