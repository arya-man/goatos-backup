package sg.mesha.goatos.feature.vaccination.leadership

import android.net.Uri
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.WarningAmber
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.ui.PlayerView
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipDriveClosureUi
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipItemUi
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipLocationOptionUi
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipMediaUi
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipVideoEvent
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipVideoPlaybackAction
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipVideoPlaybackEvent
import sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipVideosUiState
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.media.LocalProofPlayerFactory
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton

// telemetry:exempt This screen is a pure stateless renderer — screen-view
// (AnalyticsEvents.VACCINATION_LEADERSHIP_VIDEO_VIEWED) and every playback funnel step
// (AnalyticsFunnels.trackVaccinationLeadershipVideoPlayStarted/WatchSummary/PlaybackError, backed
// by AnalyticsEvents.VACCINATION_LEADERSHIP_VIDEO_PLAY_STARTED/WATCH_SUMMARY/PLAYBACK_ERROR) are
// wired in VaccinationLeadershipVideosViewModel (:app), which owns every side effect the
// [onPlayback] callback below triggers. This screen only forwards synchronous player-listener
// callbacks (VaccinationLeadershipVideoPlaybackEvent) at the exact moments the ViewModel needs;
// it has no verdict controls (approve/reject) to instrument as a primary_action.

/**
 * Read-only gallery of vaccination verification items showing full evidence trail
 * (pending/approved/rejected/closed) for leadership review: park/shed filter chips, a
 * drive-closure card (leadership `verification.act`), and proof-row cards that open a read-only
 * detail with a video player + Shed/Park/Operator/Captured context.
 *
 * No verdict controls (approve/reject) anywhere on this surface -- verdict casting is
 * verifier-only (docs/decisions/leadership-vs-verifier-surface-separation.md).
 *
 * DELIBERATE DUPLICATION NOTICE (maintainer decision, 2026-08-06): every composable in this file
 * is a deliberate COPY of the equivalent composable in
 * `feature-verify/.../VerifyQueueScreen.kt` / `VerifyDetailScreen.kt`, re-implemented here so this
 * module owns its own UI independent of the verifier's. This is intentional -- the two surfaces
 * are allowed, and expected, to diverge as leadership's screens get their own follow-up changes.
 * Do NOT refactor this into a shared composable in core-designsystem/core-ui, and do NOT make
 * `feature-vaccination` import anything from `sg.mesha.goatos.feature.verify` -- both are
 * machine-blocked by `check-leadership-verifier-surface-separation.mjs`.
 */
@Composable
fun VaccinationLeadershipVideosScreen(
    state: VaccinationLeadershipVideosUiState,
    onEvent: (VaccinationLeadershipVideoEvent) -> Unit,
    modifier: Modifier = Modifier,
) {
    val lazyListState = rememberLazyListState()

    RefreshOnResume { onEvent(VaccinationLeadershipVideoEvent.Refresh()) }

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
        MeshaScreenHeader(
            title = state.title.ifBlank { "Vaccination videos" },
            actions = {
                SyncIconButton(
                    isSyncing = state.loading,
                    onSync = { onEvent(VaccinationLeadershipVideoEvent.Refresh()) },
                )
            },
        )

        if (state.parkOptions.size > 1) {
            LeadershipLocationFilter(
                allLabel = "All parks",
                pickerTitle = "Choose a park",
                options = state.parkOptions,
                selected = state.selectedParkId,
                onSelect = { onEvent(VaccinationLeadershipVideoEvent.SelectPark(it)) },
            )
        }
        if (state.shedOptions.size > 1) {
            LeadershipLocationFilter(
                allLabel = "All sheds",
                pickerTitle = "Choose a shed",
                options = state.shedOptions,
                selected = state.selectedShedId,
                onSelect = { onEvent(VaccinationLeadershipVideoEvent.SelectShed(it)) },
            )
        }

        Box(modifier = Modifier.fillMaxSize()) {
            when {
                state.error != null && state.items.isEmpty() -> {
                    Column(
                        modifier = Modifier.fillMaxSize().padding(16.dp),
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
                    Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                        CircularProgressIndicator()
                    }
                }

                state.items.isEmpty() && state.driveClosures.isEmpty() -> {
                    Column(
                        modifier = Modifier.fillMaxSize().padding(16.dp),
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
                    LazyColumn(
                        modifier = Modifier.fillMaxSize(),
                        state = lazyListState,
                        contentPadding = PaddingValues(12.dp),
                        verticalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        items(state.driveClosures, key = { "drive-${it.batchId}" }) { closure ->
                            LeadershipDriveCloseCard(
                                closure = closure,
                                isClosing = state.closingBatchId == closure.batchId,
                                errorMessage = state.closeErrorMessage.takeIf { state.closeErrorBatchId == closure.batchId },
                                onClose = { onEvent(VaccinationLeadershipVideoEvent.CloseDrive(closure.batchId)) },
                            )
                        }
                        itemsIndexed(items = state.items, key = { _, item -> item.id }) { _, item ->
                            VaccinationLeadershipVideoItemCard(
                                item = item,
                                onClick = { onEvent(VaccinationLeadershipVideoEvent.OpenItem(item.id)) },
                            )
                        }
                        if (state.loadingMore) {
                            item {
                                Box(
                                    modifier = Modifier.fillMaxWidth().padding(16.dp),
                                    contentAlignment = Alignment.Center,
                                ) {
                                    CircularProgressIndicator(modifier = Modifier.padding(16.dp))
                                }
                            }
                        }
                    }

                    if (state.staleNotice.isNotEmpty()) {
                        Box(
                            modifier = Modifier.align(Alignment.TopCenter).fillMaxWidth().padding(8.dp),
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

    val openItem = state.items.firstOrNull { it.id == state.selectedItemId }
    if (openItem != null) {
        VaccinationLeadershipProofDetailScreen(
            item = openItem,
            onClose = { onEvent(VaccinationLeadershipVideoEvent.CloseItem) },
            onPlayback = { onEvent(VaccinationLeadershipVideoEvent.PlaybackEvent(it)) },
        )
    }
}

@Composable
private fun LeadershipLocationFilter(
    allLabel: String,
    pickerTitle: String,
    options: List<VaccinationLeadershipLocationOptionUi>,
    selected: String?,
    onSelect: (String?) -> Unit,
) {
    // A park can hold MANY sheds. Past LEADERSHIP_CHIP_ROW_MAX a horizontal chip row hides most of
    // them off-screen, so the filter becomes a searchable bottom sheet instead (maintainer rule:
    // wherever this park/shed pattern appears it must paginate and be searchable). Deliberate copy
    // of the verifier's equivalent -- the two surfaces are allowed to diverge, do NOT refactor
    // these into one shared component.
    if (options.size <= LEADERSHIP_CHIP_ROW_MAX) {
        LazyRow(
            contentPadding = PaddingValues(horizontal = 16.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            modifier = Modifier.padding(bottom = 8.dp),
        ) {
            item {
                LeadershipChip(label = allLabel, selected = selected == null, onClick = { onSelect(null) })
            }
            items(options.filter { it.id != null }, key = { it.id ?: "" }) { option ->
                LeadershipChip(label = option.label, selected = option.id == selected, onClick = { onSelect(option.id) })
            }
        }
        return
    }

    var pickerOpen by remember { mutableStateOf(false) }
    val selectedLabel = options.firstOrNull { it.id == selected }?.label ?: allLabel
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .padding(bottom = 8.dp)
            .clip(RoundedCornerShape(10.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(10.dp))
            .clickable { pickerOpen = true }
            .minimumInteractiveComponentSize()
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        Text(text = selectedLabel, color = MeshaColors.Ink, style = MeshaType.bodyStrong)
        Text(text = "Change", color = MeshaColors.BrandD, style = MeshaType.cta)
    }
    if (pickerOpen) {
        LeadershipLocationPickerSheet(
            title = pickerTitle,
            allLabel = allLabel,
            options = options,
            selected = selected,
            onSelect = { onSelect(it); pickerOpen = false },
            onDismiss = { pickerOpen = false },
        )
    }
}

/** Options up to this count stay a chip row; beyond it the filter becomes a searchable sheet. */
internal const val LEADERSHIP_CHIP_ROW_MAX = 6

/** Case-insensitive contains filter for the picker's search field. */
internal fun filterLeadershipLocationOptions(
    options: List<VaccinationLeadershipLocationOptionUi>,
    query: String,
): List<VaccinationLeadershipLocationOptionUi> {
    val trimmed = query.trim()
    if (trimmed.isEmpty()) return options
    return options.filter { it.label.contains(trimmed, ignoreCase = true) }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun LeadershipLocationPickerSheet(
    title: String,
    allLabel: String,
    options: List<VaccinationLeadershipLocationOptionUi>,
    selected: String?,
    onSelect: (String?) -> Unit,
    onDismiss: () -> Unit,
) {
    var query by remember { mutableStateOf("") }
    val visible = filterLeadershipLocationOptions(options, query)
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp)) {
            Text(text = title, color = MeshaColors.Ink, style = MeshaType.cardTitle, modifier = Modifier.padding(bottom = 8.dp))
            OutlinedTextField(
                value = query,
                onValueChange = { query = it },
                singleLine = true,
                placeholder = { Text(text = "Search", style = MeshaType.cardSubtitle) },
                modifier = Modifier.fillMaxWidth().padding(bottom = 8.dp),
            )
            // Bounded window: the sheet is scrollable and lazy, so a park with 100+ sheds renders
            // a page at a time instead of composing every row.
            LazyColumn(modifier = Modifier.fillMaxWidth().heightIn(max = 420.dp)) {
                items(visible, key = { it.id ?: "all" }) { option ->
                    Text(
                        text = option.label.ifBlank { allLabel },
                        color = if (option.id == selected) MeshaColors.BrandD else MeshaColors.Ink,
                        style = MeshaType.body,
                        modifier = Modifier
                            .fillMaxWidth()
                            .clickable { onSelect(option.id) }
                            .minimumInteractiveComponentSize()
                            .padding(vertical = 12.dp),
                    )
                }
            }
        }
    }
}

@Composable
private fun LeadershipChip(label: String, selected: Boolean, onClick: () -> Unit) {
    val bg = if (selected) MeshaColors.BrandTint else MeshaColors.Surf2
    val fg = if (selected) MeshaColors.BrandD else MeshaColors.Muted
    val border = if (selected) MeshaColors.Brand else MeshaColors.Hair
    Text(
        text = label,
        color = fg,
        style = MeshaType.pillStrong,
        modifier = Modifier
            // Filter chips are the primary control on this screen; without this the tap target is
            // the text bounds, below the accessibility minimum.
            .minimumInteractiveComponentSize()
            .clip(RoundedCornerShape(999.dp))
            .background(bg)
            .border(1.dp, border, RoundedCornerShape(999.dp))
            .clickable(onClick = onClick)
            .padding(horizontal = 14.dp, vertical = 8.dp),
    )
}

@Composable
private fun LeadershipDriveCloseCard(
    closure: VaccinationLeadershipDriveClosureUi,
    isClosing: Boolean,
    errorMessage: String?,
    onClose: () -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.OkX)
            .border(1.dp, MeshaColors.Ok.copy(alpha = 0.35f), RoundedCornerShape(16.dp))
            .padding(14.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text(
                    text = closure.batchLabel.ifBlank { "Vaccination drive ready" },
                    color = MeshaColors.Ink,
                    style = MeshaType.bodyStrong,
                )
                if (closure.driveLabel.isNotBlank()) {
                    Text(
                        text = closure.driveLabel,
                        color = MeshaColors.Muted,
                        style = MeshaType.cta,
                        modifier = Modifier.padding(top = 2.dp),
                    )
                }
                Text(
                    text = "${closure.totalCount} animals · ${closure.shedCount} sheds · " +
                        "${closure.approvedVideos}/${closure.videoCount} videos approved",
                    color = MeshaColors.Muted,
                    style = MeshaType.cardSubtitle,
                    modifier = Modifier.padding(top = 2.dp),
                )
                Text(
                    text = "Video pending ${closure.pendingVideos} · rejected ${closure.rejectedVideos}",
                    color = MeshaColors.Faint,
                    style = MeshaType.pill,
                    modifier = Modifier.padding(top = 6.dp),
                )
            }
            Row(
                modifier = Modifier
                    .clip(RoundedCornerShape(12.dp))
                    .background(MeshaColors.Ok)
                    .clickable(enabled = !isClosing, onClick = onClose)
                    .padding(horizontal = 12.dp, vertical = 10.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                if (isClosing) {
                    CircularProgressIndicator(modifier = Modifier.size(16.dp), color = MeshaColors.Surf, strokeWidth = 2.dp)
                } else {
                    Icon(MeshaIcons.CheckCircle, contentDescription = null, tint = MeshaColors.Surf, modifier = Modifier.size(16.dp))
                    Spacer(Modifier.size(6.dp))
                    Text(text = "Close", color = MeshaColors.Surf, style = MeshaType.pillStrong)
                }
            }
        }
        errorMessage?.let {
            Text(
                text = it,
                color = MeshaColors.Danger,
                style = MeshaType.cardSubtitle,
                modifier = Modifier.padding(top = 8.dp),
            )
        }
    }
}

/**
 * One verification item card in the leadership gallery: a video icon, "N goats · shed" title,
 * "park · operator · captured" metadata line, and a status pill. No verdict controls.
 */
@Composable
fun VaccinationLeadershipVideoItemCard(
    item: VaccinationLeadershipItemUi,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
            .clickable(onClick = onClick)
            .padding(14.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(
                modifier = Modifier.size(40.dp).clip(RoundedCornerShape(12.dp)).background(MeshaColors.Surf2),
                contentAlignment = Alignment.Center,
            ) {
                Icon(imageVector = MeshaIcons.Video, contentDescription = null, tint = MeshaColors.Brand, modifier = Modifier.size(18.dp))
            }
            Spacer(Modifier.size(12.dp))
            Column(Modifier.weight(1f)) {
                Text(
                    // The backend subject label ALREADY names the shed ("Gandhi 1 · G-003002"), so
                    // appending shedLabel printed it twice ("Gandhi 1 · G-003002 · Gandhi 1").
                    // Only add the shed when the label does not already carry it; the Context
                    // panel in the detail view still shows it as its own field.
                    text = listOf(item.title, item.shedLabel.takeUnless { item.title.contains(it, ignoreCase = true) }.orEmpty())
                        .filter { it.isNotBlank() }
                        .joinToString(" · ")
                        .ifBlank { "Proof" },
                    color = MeshaColors.Ink,
                    style = MeshaType.bodyStrong,
                )
                Text(
                    text = listOf(item.parkLabel, item.operatorLabel, item.timestamp).filter { it.isNotBlank() }.joinToString(" · "),
                    color = MeshaColors.Muted,
                    style = MeshaType.cardSubtitle,
                    modifier = Modifier.padding(top = 2.dp),
                )
                if (item.summary.isNotEmpty() && item.summary != item.title) {
                    Text(
                        text = item.summary,
                        color = MeshaColors.Faint,
                        style = MeshaType.pill,
                        modifier = Modifier.padding(top = 2.dp),
                    )
                }
            }
            LeadershipStatusPill(tone = item.statusTone, label = item.statusLabel)
        }
        Text(
            text = "Vaccination proof",
            color = MeshaColors.Faint,
            style = MeshaType.pill,
            modifier = Modifier.padding(top = 10.dp),
        )
    }
}

@Composable
private fun LeadershipStatusPill(tone: String, label: String) {
    val (bg, fg) = when (tone) {
        "success" -> MeshaColors.OkX to MeshaColors.Ok
        "error" -> MeshaColors.DangerX to MeshaColors.Danger
        "warn" -> MeshaColors.WarnX to MeshaColors.Warn
        else -> MeshaColors.WarnX to MeshaColors.Warn
    }
    Text(
        text = label,
        color = fg,
        style = MeshaType.pill,
        modifier = Modifier.clip(RoundedCornerShape(999.dp)).background(bg).padding(horizontal = 10.dp, vertical = 4.dp),
    )
}

/**
 * Read-only proof detail: video player (scrub + 5s/15s skip via the native ExoPlayer controller)
 * and a Shed/Park/Operator/Captured context panel. NO approve/reject controls exist here or
 * anywhere in this module -- verdict casting is verifier-only.
 */
@Composable
private fun VaccinationLeadershipProofDetailScreen(
    item: VaccinationLeadershipItemUi,
    onClose: () -> Unit,
    onPlayback: (VaccinationLeadershipVideoPlaybackEvent) -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(MeshaColors.PageBg),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth().padding(12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(onClick = onClose) {
                Icon(MeshaIcons.Close, contentDescription = "Close", tint = MeshaColors.Ink)
            }
            Column(Modifier.weight(1f).padding(start = 4.dp)) {
                Text(text = "Vaccination proof", color = MeshaColors.Ink, style = MeshaType.button)
                Text(
                    text = "${item.proofCount} goat${if (item.proofCount != 1) "s" else ""}",
                    color = MeshaColors.Muted,
                    style = MeshaType.cardSubtitle,
                )
            }
            LeadershipStatusPill(tone = item.statusTone, label = item.statusLabel)
        }
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(horizontal = 12.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            val media = item.media.ifEmpty {
                item.videoUrls.mapIndexed { idx, url -> VaccinationLeadershipMediaUi(proofId = "$idx", url = url, mimeType = "video/mp4") }
            }
            items(media, key = { it.proofId }) { m ->
                VaccinationLeadershipVideoPlayer(media = m, onPlayback = onPlayback)
            }
            item {
                VaccinationLeadershipContextCard(item = item)
            }
        }
    }
}

@Composable
private fun VaccinationLeadershipContextCard(item: VaccinationLeadershipItemUi) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(14.dp))
            .padding(14.dp),
    ) {
        Text(text = "Context", color = MeshaColors.Ink, style = MeshaType.listTitle, modifier = Modifier.padding(bottom = 8.dp))
        listOf(
            "Shed" to item.shedLabel,
            "Park" to item.parkLabel,
            "Operator" to item.operatorLabel,
            "Captured" to item.timestamp,
        ).filter { it.second.isNotBlank() }.forEach { (label, value) ->
            Row(modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp)) {
                Text(text = label, color = MeshaColors.Muted, style = MeshaType.cardSubtitle, modifier = Modifier.weight(1f))
                Text(text = value, color = MeshaColors.Ink, style = MeshaType.cardSubtitle)
            }
        }
    }
}

/**
 * Read-only video player using the NATIVE ExoPlayer controller (scrub bar + skip), streamed
 * through [LocalProofPlayerFactory] per AGENTS.md (media3 must fetch over the app's telemetry-
 * instrumented OkHttp client, never a plain `ExoPlayer.Builder(context).build()`).
 */
@Composable
private fun VaccinationLeadershipVideoPlayer(
    media: VaccinationLeadershipMediaUi,
    onPlayback: (VaccinationLeadershipVideoPlaybackEvent) -> Unit,
) {
    val context = LocalContext.current
    val playerFactory = LocalProofPlayerFactory.current
    val currentOnPlayback by rememberUpdatedState(onPlayback)
    var playStartReported by remember(media.proofId) { mutableStateOf(false) }
    var watchTimeMs by remember(media.proofId) { mutableLongStateOf(0L) }
    var lastResumeAtMs by remember(media.proofId) { mutableStateOf<Long?>(null) }

    val player = remember(media.url) {
        playerFactory.create(context).apply {
            // Media3's default seek increments are 5s back / 15s forward -- exactly what this
            // surface's spec calls for -- so the native PlayerView controller needs no override.
            setMediaItem(MediaItem.fromUri(Uri.parse(media.url)))
            playWhenReady = false
            prepare()
        }
    }

    DisposableEffect(player) {
        val listener = object : Player.Listener {
            override fun onIsPlayingChanged(isPlayingNow: Boolean) {
                val now = System.currentTimeMillis()
                if (isPlayingNow) {
                    lastResumeAtMs = now
                    if (!playStartReported) {
                        playStartReported = true
                        currentOnPlayback(
                            VaccinationLeadershipVideoPlaybackEvent(
                                action = VaccinationLeadershipVideoPlaybackAction.PLAY_STARTED,
                                proofId = media.proofId,
                                mimeType = media.mimeType,
                                durationMs = player.duration.coerceAtLeast(0L),
                            ),
                        )
                    }
                } else {
                    lastResumeAtMs?.let { watchTimeMs += (now - it).coerceAtLeast(0L) }
                    lastResumeAtMs = null
                }
            }

            override fun onPlayerError(error: PlaybackException) {
                currentOnPlayback(
                    VaccinationLeadershipVideoPlaybackEvent(
                        action = VaccinationLeadershipVideoPlaybackAction.PLAYBACK_ERROR,
                        proofId = media.proofId,
                        mimeType = media.mimeType,
                        reason = error.message ?: error.errorCodeName,
                    ),
                )
            }
        }
        player.addListener(listener)
        onDispose {
            lastResumeAtMs?.let { watchTimeMs += (System.currentTimeMillis() - it).coerceAtLeast(0L) }
            val durationMs = player.duration.coerceAtLeast(0L)
            if (playStartReported) {
                currentOnPlayback(
                    VaccinationLeadershipVideoPlaybackEvent(
                        action = VaccinationLeadershipVideoPlaybackAction.WATCH_SUMMARY,
                        proofId = media.proofId,
                        mimeType = media.mimeType,
                        durationMs = durationMs,
                        watchTimeMs = watchTimeMs,
                        positionMs = player.currentPosition.coerceAtLeast(0L),
                        percentWatched = if (durationMs > 0) (watchTimeMs.toFloat() / durationMs).coerceIn(0f, 1f) else 0f,
                    ),
                )
            }
            player.removeListener(listener)
            player.release()
        }
    }

    Box(
        modifier = Modifier
            .fillMaxWidth()
            .aspectRatio(16f / 9f)
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Bg),
    ) {
        AndroidView(
            factory = { ctx ->
                PlayerView(ctx).apply {
                    this.player = player
                    useController = true
                }
            },
            modifier = Modifier.fillMaxSize(),
        )
    }
}
