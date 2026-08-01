package sg.mesha.goatos.feature.weighing.leadership

import android.net.Uri
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.media3.common.MediaItem
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.AspectRatioFrameLayout
import androidx.media3.ui.PlayerView
import androidx.compose.ui.res.stringResource
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.feature.weighing.R

// telemetry:exempt Weighing leadership evidence gallery is read-only in V1; verifier decisions are tracked in the verification surface.

data class WeighingLeadershipVideosUiState(
    val loading: Boolean = true,
    val loadingMore: Boolean = false,
    val sheds: List<WeighingLeadershipShedUi> = emptyList(),
    val error: String? = null,
    /**
     * Quiet staleness note shown ABOVE the cached gallery when the last refresh did not land. A
     * failed refresh never clears the gallery, so this replaces the error page whenever there is
     * still something cached to read.
     */
    val staleNotice: String = "",
)

data class WeighingLeadershipShedUi(
    val id: String,
    val name: String,
    val status: String,
    val periodLabel: String,
    val category: String,
    val animals: List<WeighingLeadershipAnimalUi> = emptyList(),
    val animalCount: String? = null,
    val totalWeight: String? = null,
    val averageWeight: String? = null,
    val videos: List<WeighingLeadershipVideoUi> = emptyList(),
)

data class WeighingLeadershipAnimalUi(
    val id: String,
    val rfid: String,
    val weight: String,
    val timestamp: String,
    val videos: List<WeighingLeadershipVideoUi>,
)

data class WeighingLeadershipVideoUi(
    val id: String,
    val label: String,
    val url: String,
)

private enum class WeighingVideoScope {
    ALL,
    INDIVIDUAL,
    LUMP_SUM,
}

private data class SelectedLeadershipVideo(
    val title: String,
    val subtitle: String,
    val video: WeighingLeadershipVideoUi,
)

// media3's player APIs are opt-in. Containing that here -- rather than at file scope -- keeps the
// requirement from propagating to callers: with @file:UnstableApi the annotation rode out on this
// screen's public signature, so AppNavHost had to opt in to a media concern just to navigate to it.
// Moving weighing into its own module is what surfaced that leak.
@OptIn(androidx.media3.common.util.UnstableApi::class)
@Composable
fun WeighingLeadershipVideosScreen(
    state: WeighingLeadershipVideosUiState,
    onShedVisible: (Int) -> Unit = {},
    onRefresh: () -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onRefresh() }
    var selectedScope by remember { mutableStateOf(WeighingVideoScope.ALL) }
    var selectedShedId by remember { mutableStateOf<String?>(null) }
    var selectedVideo by remember { mutableStateOf<SelectedLeadershipVideo?>(null) }
    val selectableSheds = remember(state.sheds, selectedScope) {
        state.sheds.filter { shed ->
            when (selectedScope) {
                WeighingVideoScope.ALL -> shed.videoCountForChip > 0
                WeighingVideoScope.INDIVIDUAL -> shed.animals.isNotEmpty()
                WeighingVideoScope.LUMP_SUM -> shed.isLumpSum && shed.videos.isNotEmpty()
            }
        }
    }
    LaunchedEffect(selectableSheds, selectedShedId) {
        if (selectedShedId != null && selectableSheds.none { it.id == selectedShedId }) {
            selectedShedId = null
        }
    }
    val filteredSheds = remember(selectableSheds, selectedShedId) {
        selectedShedId?.let { shedId -> selectableSheds.filter { it.id == shedId } } ?: selectableSheds
    }
    val prefetchShedIds = remember(state.sheds) {
        state.sheds.takeLast(LIST_PREFETCH_DISTANCE).map { it.id }.toSet()
    }
    val shedIndexById = remember(state.sheds) {
        state.sheds.withIndex().associate { (index, shed) -> shed.id to index }
    }
    val individualCount = state.sheds.sumOf { it.animals.size }
    val lumpVideoCount = state.sheds.filter { it.isLumpSum }.sumOf { it.videos.size }

    Column(modifier = modifier.fillMaxSize().background(MeshaColors.Bg)) {
        MeshaScreenHeader(
            eyebrow = stringResource(R.string.weighing_eyebrow),
            title = stringResource(R.string.weighing_videos_title),
            actions = {
                SyncIconButton(
                    isSyncing = state.loading,
                    onSync = onRefresh,
                    contentDescription = stringResource(R.string.weighing_videos_refresh),
                )
            },
        )
        when {
            // A loading wall only when there is nothing cached to read. With a cached gallery the
            // reader keeps it on screen and the refresh spins in the header instead.
            state.loading && state.sheds.isEmpty() -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                CircularProgressIndicator(color = MeshaColors.Brand)
            }
            state.error != null && state.sheds.isEmpty() -> EmptyMessage(state.error)
            state.sheds.isEmpty() -> EmptyMessage(stringResource(R.string.weighing_videos_empty))
            else -> Column(modifier = Modifier.fillMaxSize()) {
                VideoFilters(
                    selectedScope = selectedScope,
                    onScopeSelected = { selectedScope = it },
                    selectedShedId = selectedShedId,
                    onShedSelected = { selectedShedId = it },
                    sheds = selectableSheds,
                    individualCount = individualCount,
                    lumpVideoCount = lumpVideoCount,
                )
                LazyColumn(
                    modifier = Modifier.fillMaxSize(),
                    contentPadding = PaddingValues(16.dp),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    if (state.staleNotice.isNotBlank()) {
                        item(key = "gallery-stale") { EmptySection(state.staleNotice) }
                    }
                    if (selectedScope != WeighingVideoScope.LUMP_SUM) {
                        val individualSheds = filteredSheds.filter { it.animals.isNotEmpty() }
                        item(key = "individual-header") {
                            val animalTotal = individualSheds.sumOf { it.animals.size }
                            SectionHeader(
                                title = stringResource(R.string.weighing_videos_individual_header),
                                subtitle = stringResource(R.string.weighing_videos_individual_subtitle),
                                meta = animalCountLabel(animalTotal),
                                accent = MeshaColors.Brand,
                            )
                        }
                        if (individualSheds.isEmpty()) {
                            item(key = "individual-empty") { EmptySection(stringResource(R.string.weighing_videos_individual_empty)) }
                        } else {
                            items(individualSheds, key = { "individual-${it.id}" }) { shed ->
                                // The gallery itself pulls the next page as the reader scrolls near the end.
                                if (shed.id in prefetchShedIds) {
                                    LaunchedEffect(shed.id, state.sheds.size) {
                                        shedIndexById[shed.id]?.let(onShedVisible)
                                    }
                                }
                                IndividualShedCard(shed, onVideoSelected = { selectedVideo = it })
                            }
                        }
                    }
                    if (selectedScope != WeighingVideoScope.INDIVIDUAL) {
                        val lumpSheds = filteredSheds.filter { it.isLumpSum && it.videos.isNotEmpty() }
                        item(key = "lump-header") {
                            val videoTotal = lumpSheds.sumOf { it.videos.size }
                            SectionHeader(
                                title = stringResource(R.string.weighing_videos_lump_header),
                                subtitle = stringResource(R.string.weighing_videos_lump_subtitle),
                                meta = videoCountLabel(videoTotal),
                                accent = MeshaColors.Purple,
                            )
                        }
                        if (lumpSheds.isEmpty()) {
                            item(key = "lump-empty") { EmptySection(stringResource(R.string.weighing_videos_lump_empty)) }
                        } else {
                            items(lumpSheds, key = { "lump-${it.id}" }) { shed ->
                                // The gallery itself pulls the next page as the reader scrolls near the end.
                                if (shed.id in prefetchShedIds) {
                                    LaunchedEffect(shed.id, state.sheds.size) {
                                        shedIndexById[shed.id]?.let(onShedVisible)
                                    }
                                }
                                LumpSumShedCard(shed, onVideoSelected = { selectedVideo = it })
                            }
                        }
                    }
                    if (state.loadingMore) {
                        item(key = "videos-loading-more") { LoadingMoreFooter() }
                    }
                }
            }
        }
    }
    selectedVideo?.let { video ->
        LeadershipVideoPlayer(video = video, onDismiss = { selectedVideo = null })
    }
}

@Composable
private fun VideoFilters(
    selectedScope: WeighingVideoScope,
    onScopeSelected: (WeighingVideoScope) -> Unit,
    selectedShedId: String?,
    onShedSelected: (String?) -> Unit,
    sheds: List<WeighingLeadershipShedUi>,
    individualCount: Int,
    lumpVideoCount: Int,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(MeshaColors.Bg)
            .padding(top = 6.dp, bottom = 10.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        LazyRow(
            contentPadding = PaddingValues(horizontal = 16.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            item {
                FilterChip(
                    label = stringResource(R.string.weighing_videos_filter_all),
                    count = (individualCount + lumpVideoCount).toString(),
                    selected = selectedScope == WeighingVideoScope.ALL,
                    accent = MeshaColors.Brand,
                ) {
                    onScopeSelected(WeighingVideoScope.ALL)
                }
            }
            item {
                FilterChip(
                    label = stringResource(R.string.weighing_category_individual_title),
                    count = individualCount.toString(),
                    selected = selectedScope == WeighingVideoScope.INDIVIDUAL,
                    accent = MeshaColors.Brand,
                ) {
                    onScopeSelected(WeighingVideoScope.INDIVIDUAL)
                }
            }
            item {
                FilterChip(
                    label = stringResource(R.string.weighing_category_lump_sum_title),
                    count = lumpVideoCount.toString(),
                    selected = selectedScope == WeighingVideoScope.LUMP_SUM,
                    accent = MeshaColors.Purple,
                ) {
                    onScopeSelected(WeighingVideoScope.LUMP_SUM)
                }
            }
        }
        LazyRow(
            contentPadding = PaddingValues(horizontal = 16.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            item {
                FilterChip(
                    label = stringResource(R.string.weighing_videos_filter_all_sheds),
                    count = sheds.size.toString(),
                    selected = selectedShedId == null,
                    accent = MeshaColors.Brand,
                ) { onShedSelected(null) }
            }
            items(sheds, key = { it.id }) { shed ->
                FilterChip(
                    label = shed.name,
                    count = shed.videoCountForChip.takeIf { it > 0 }?.toString(),
                    selected = selectedShedId == shed.id,
                    accent = if (shed.isLumpSum) MeshaColors.Purple else MeshaColors.Brand,
                ) { onShedSelected(shed.id) }
            }
        }
    }
}

@Composable
private fun FilterChip(
    label: String,
    count: String? = null,
    selected: Boolean,
    accent: Color,
    onClick: () -> Unit,
) {
    val shape = RoundedCornerShape(8.dp)
    val background = if (selected) {
        Brush.horizontalGradient(listOf(accent.copy(alpha = 0.95f), accent.copy(alpha = 0.68f)))
    } else {
        SolidColor(MeshaColors.Surf2)
    }
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        modifier = Modifier
            .clip(shape)
            .background(background)
            .border(1.dp, if (selected) accent else MeshaColors.Hair, shape)
            .clickable(onClick = onClick)
            .padding(horizontal = 11.dp, vertical = 8.dp),
    ) {
        Text(
            text = label,
            color = if (selected) MaterialTheme.colorScheme.onPrimary else MaterialTheme.colorScheme.onSurface,
            style = MeshaType.pill,
        )
        count?.let {
            Text(
                text = it,
                color = if (selected) MeshaColors.OnBrand else accent,
                style = MeshaType.pill,
                modifier = Modifier
                    .clip(RoundedCornerShape(6.dp))
                    .background(if (selected) MeshaColors.Overlay else accent.copy(alpha = 0.14f))
                    .padding(horizontal = 7.dp, vertical = 3.dp),
            )
        }
    }
}

@Composable
private fun SectionHeader(title: String, subtitle: String, meta: String, accent: Color) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 4.dp)
            .clip(RoundedCornerShape(8.dp))
            .background(MeshaColors.Surf2)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(8.dp))
            .padding(12.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Row(
            modifier = Modifier.weight(1f),
            horizontalArrangement = Arrangement.spacedBy(10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                modifier = Modifier
                    .size(width = 4.dp, height = 34.dp)
                    .clip(RoundedCornerShape(4.dp))
                    .background(accent),
            )
            Column {
                Text(title, color = MaterialTheme.colorScheme.onSurface, style = MeshaType.cardTitle)
                Text(subtitle, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MeshaType.cardSubtitle)
            }
        }
        Text(
            text = meta,
            color = accent,
            style = MeshaType.pill,
            modifier = Modifier
                .clip(RoundedCornerShape(8.dp))
                .background(accent.copy(alpha = 0.14f))
                .padding(horizontal = 9.dp, vertical = 5.dp),
        )
    }
}

@Composable
private fun IndividualShedCard(
    shed: WeighingLeadershipShedUi,
    onVideoSelected: (SelectedLeadershipVideo) -> Unit,
) {
    ShedFrame(shed = shed, trailing = animalCountLabel(shed.animals.size)) {
        Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
            shed.animals.forEach { animal ->
                IndividualAnimal(
                    shedName = shed.name,
                    animal = animal,
                    onVideoSelected = onVideoSelected,
                )
            }
        }
    }
}

@Composable
private fun LumpSumShedCard(
    shed: WeighingLeadershipShedUi,
    onVideoSelected: (SelectedLeadershipVideo) -> Unit,
) {
    ShedFrame(shed = shed, trailing = videoCountLabel(shed.videos.size)) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(8.dp))
                .background(MeshaColors.Surf2)
                .border(1.dp, MeshaColors.Hair, RoundedCornerShape(8.dp))
                .padding(12.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            LumpSumSummary(shed)
            VideoActionRow(
                title = shed.name,
                subtitle = listOf(shed.periodLabel, stringResource(R.string.weighing_category_lump_sum_title))
                    .filter { it.isNotBlank() }
                    .joinToString(" · "),
                videos = shed.videos,
                onVideoSelected = onVideoSelected,
            )
        }
    }
}

@Composable
private fun ShedFrame(
    shed: WeighingLeadershipShedUi,
    trailing: String,
    content: @Composable () -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(8.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(8.dp))
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(shed.name, color = MaterialTheme.colorScheme.onSurface, style = MeshaType.cardTitle)
                if (shed.periodLabel.isNotBlank()) {
                    Text(shed.periodLabel, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MeshaType.cardSubtitle)
                }
            }
            Column(horizontalAlignment = Alignment.End) {
                Text(
                    trailing,
                    color = if (shed.isLumpSum) MeshaColors.Purple else MeshaColors.BrandD,
                    style = MeshaType.bodyStrong,
                )
                Text(shed.status.replaceFirstChar(Char::uppercase), color = MaterialTheme.colorScheme.onSurfaceVariant, style = MeshaType.cardSubtitle)
            }
        }
        content()
    }
}

@Composable
private fun IndividualAnimal(
    shedName: String,
    animal: WeighingLeadershipAnimalUi,
    onVideoSelected: (SelectedLeadershipVideo) -> Unit,
) {
    Column(
        verticalArrangement = Arrangement.spacedBy(9.dp),
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(8.dp))
            .background(MeshaColors.Surf2)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(8.dp))
            .padding(12.dp),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(animal.rfid, color = MaterialTheme.colorScheme.onSurface, style = MeshaType.listTitle)
                Text(animal.timestamp, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MeshaType.cardSubtitle)
            }
            Column(horizontalAlignment = Alignment.End) {
                Text(animal.weight, color = MeshaColors.BrandD, style = MeshaType.listTitle)
                Text(
                    videoCountLabel(animal.videos.size),
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    style = MeshaType.cardSubtitle,
                )
            }
        }
        VideoActionRow(
            title = animal.rfid,
            subtitle = listOf(shedName, animal.weight, animal.timestamp).filter { it.isNotBlank() }.joinToString(" · "),
            videos = animal.videos,
            onVideoSelected = onVideoSelected,
        )
    }
}

@Composable
private fun LumpSumSummary(shed: WeighingLeadershipShedUi) {
    Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
        Metric(stringResource(R.string.weighing_videos_metric_animals), shed.animalCount.orEmpty())
        Metric(stringResource(R.string.weighing_videos_metric_total), shed.totalWeight.orEmpty())
        Metric(stringResource(R.string.weighing_videos_metric_average), shed.averageWeight.orEmpty())
    }
}

@Composable
private fun Metric(label: String, value: String) {
    Column(horizontalAlignment = Alignment.CenterHorizontally) {
        Text(value, color = MaterialTheme.colorScheme.onSurface, style = MeshaType.bodyStrong)
        Text(label, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MeshaType.caption)
    }
}

@Composable
private fun VideoActionRow(
    title: String,
    subtitle: String,
    videos: List<WeighingLeadershipVideoUi>,
    onVideoSelected: (SelectedLeadershipVideo) -> Unit,
) {
    if (videos.isEmpty()) {
        Text(
            stringResource(R.string.weighing_videos_none_attached),
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            style = MeshaType.cardSubtitle,
        )
        return
    }
    LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        items(videos, key = { it.id }) { video ->
            Text(
                text = video.label.ifBlank { stringResource(R.string.weighing_videos_play) },
                color = MeshaColors.BrandD,
                style = MeshaType.cta,
                modifier = Modifier
                    .widthIn(min = 96.dp)
                    .clip(RoundedCornerShape(8.dp))
                    .background(MeshaColors.BrandTint)
                    .border(1.dp, MeshaColors.Brand, RoundedCornerShape(8.dp))
                    .clickable {
                        onVideoSelected(
                            SelectedLeadershipVideo(
                                title = title,
                                subtitle = subtitle,
                                video = video,
                            ),
                        )
                    }
                    .padding(horizontal = 12.dp, vertical = 10.dp),
            )
        }
    }
}

@Composable
private fun LeadershipVideoPlayer(video: SelectedLeadershipVideo, onDismiss: () -> Unit) {
    var expanded by remember { mutableStateOf(false) }
    val context = LocalContext.current
    val player = remember(video.video.url) {
        ExoPlayer.Builder(context).build().apply {
            setMediaItem(MediaItem.fromUri(Uri.parse(video.video.url)))
            prepare()
            playWhenReady = false
        }
    }
    DisposableEffect(player) {
        onDispose { player.release() }
    }
    Dialog(
        onDismissRequest = onDismiss,
        properties = DialogProperties(usePlatformDefaultWidth = false),
    ) {
        Box(
            modifier = Modifier
                .fillMaxSize()
                .background(Color.Black),
        ) {
            AndroidView(
                factory = {
                    PlayerView(it).apply {
                        this.player = player
                        useController = true
                        controllerShowTimeoutMs = 0
                        controllerHideOnTouch = false
                        resizeMode = if (expanded) {
                            AspectRatioFrameLayout.RESIZE_MODE_ZOOM
                        } else {
                            AspectRatioFrameLayout.RESIZE_MODE_FIT
                        }
                    }
                },
                update = {
                    it.resizeMode = if (expanded) {
                        AspectRatioFrameLayout.RESIZE_MODE_ZOOM
                    } else {
                        AspectRatioFrameLayout.RESIZE_MODE_FIT
                    }
                },
                modifier = Modifier
                    .fillMaxSize()
                    .background(Color.Black),
            )
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .statusBarsPadding()
                    .background(Brush.verticalGradient(listOf(Color.Black.copy(alpha = 0.88f), Color.Transparent)))
                    .padding(12.dp),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.Top,
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(video.title, color = MaterialTheme.colorScheme.onSurface, style = MeshaType.bodyStrong)
                    if (!expanded && video.subtitle.isNotBlank()) {
                        Text(video.subtitle, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MeshaType.cardSubtitle)
                    }
                }
                Row {
                    IconButton(onClick = { expanded = !expanded }, modifier = Modifier.size(48.dp)) {
                        Icon(
                            imageVector = MeshaIcons.Expand,
                            contentDescription = if (expanded) {
                                stringResource(R.string.weighing_videos_exit_full_screen)
                            } else {
                                stringResource(R.string.weighing_videos_full_screen)
                            },
                            tint = Color.White,
                        )
                    }
                    IconButton(onClick = onDismiss, modifier = Modifier.size(48.dp)) {
                        Icon(imageVector = MeshaIcons.Close, contentDescription = stringResource(R.string.weighing_videos_close), tint = Color.White)
                    }
                }
            }
        }
    }
}

private const val LIST_PREFETCH_DISTANCE = 3

@Composable
private fun LoadingMoreFooter() {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 12.dp),
        horizontalArrangement = Arrangement.Center,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        CircularProgressIndicator(
            modifier = Modifier.size(16.dp),
            color = MeshaColors.Brand,
            strokeWidth = 2.dp,
        )
        Text(
            text = stringResource(R.string.weighing_videos_loading_more),
            color = MeshaColors.Muted,
            style = MeshaType.cardSubtitle,
            modifier = Modifier.padding(start = 8.dp),
        )
    }
}

@Composable
private fun EmptyMessage(message: String) {
    Box(Modifier.fillMaxSize().padding(24.dp), contentAlignment = Alignment.Center) {
        Text(message, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MeshaType.body)
    }
}

@Composable
private fun EmptySection(message: String) {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(8.dp))
            .background(MeshaColors.Surf)
            .padding(18.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(message, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MeshaType.body)
    }
}

private val WeighingLeadershipShedUi.isLumpSum: Boolean
    get() = category == "per_shed_partition"

private val WeighingLeadershipShedUi.videoCountForChip: Int
    get() = if (isLumpSum) videos.size else animals.sumOf { it.videos.size }

@Composable
private fun animalCountLabel(count: Int): String = if (count == 1) {
    stringResource(R.string.weighing_videos_animal_one, count)
} else {
    stringResource(R.string.weighing_videos_animal_other, count)
}

@Composable
private fun videoCountLabel(count: Int): String = if (count == 1) {
    stringResource(R.string.weighing_videos_video_one, count)
} else {
    stringResource(R.string.weighing_videos_video_other, count)
}
