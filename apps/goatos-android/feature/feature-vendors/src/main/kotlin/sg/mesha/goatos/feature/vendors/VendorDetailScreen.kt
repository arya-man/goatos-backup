package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure stateless renderer; VendorDetailViewModel (in :app) owns the vendors_*
// AnalyticsEventsVendors + CrashReporter wiring.

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.LoadingSkeletonList
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton

/** One vendor (L1 drill): identity, supply, notes — and the voice note's player. */
@Composable
fun VendorDetailScreen(
    state: VendorDetailUiState,
    onEvent: (VendorDetailEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(VendorDetailEvent.Refresh) }
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = state.title,
            subtitle = state.subtitle.ifBlank { null },
            onBack = { onEvent(VendorDetailEvent.Back) },
            actions = { SyncIconButton(isSyncing = state.isRefreshing, onSync = { onEvent(VendorDetailEvent.Refresh) }) },
        )
        if (state.isLoading) {
            LoadingSkeletonList(modifier = Modifier.padding(MeshaDimens.gutter))
            return@Column
        }
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(horizontal = MeshaDimens.gutter, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "status") {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    VendorsChip(label = state.statusLabel, tone = state.statusTone)
                }
            }
            state.message?.let { message ->
                item(key = "message") {
                    VendorsResultBanner(status = VendorsWriteStatus.FAILED, message = message)
                }
            }
            if (state.voiceNote != VoiceNotePlayback.NONE) {
                item(key = "voice_note") { VoiceNoteCard(state.voiceNote, onEvent) }
            }
            items(count = state.sections.size, key = { "section_${state.sections[it].title}" }) { index ->
                VendorsDetailSection(state.sections[index])
            }
        }
    }
}

@Composable
private fun VoiceNoteCard(playback: VoiceNotePlayback, onEvent: (VendorDetailEvent) -> Unit) {
    val playing = playback == VoiceNotePlayback.PLAYING
    val loading = playback == VoiceNotePlayback.LOADING
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(MeshaDimens.radiusCard))
            .background(MeshaColors.Surf)
            .clickable(enabled = !loading, role = Role.Button) {
                onEvent(if (playing) VendorDetailEvent.StopVoiceNote else VendorDetailEvent.PlayVoiceNote)
            }
            .padding(14.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Box(
            modifier = Modifier
                .size(44.dp)
                .clip(RoundedCornerShape(MeshaDimens.radiusIcon))
                .background(if (playing) MeshaColors.Brand else MeshaColors.TealX),
            contentAlignment = Alignment.Center,
        ) {
            if (loading) {
                CircularProgressIndicator(color = MeshaColors.Teal, modifier = Modifier.size(MeshaDimens.iconMd))
            } else {
                Icon(
                    if (playing) MeshaIcons.Pause else MeshaIcons.Play,
                    contentDescription = if (playing) STOP_LABEL else PLAY_LABEL,
                    tint = if (playing) MeshaColors.OnBrand else MeshaColors.Teal,
                    modifier = Modifier.size(MeshaDimens.iconMd),
                )
            }
        }
        Column(Modifier.weight(1f)) {
            Text(text = VOICE_NOTE_TITLE, color = MeshaColors.Ink, style = MeshaType.listTitle)
            Spacer(Modifier.height(2.dp))
            Text(
                text = when (playback) {
                    VoiceNotePlayback.PLAYING -> PLAYING_HINT
                    VoiceNotePlayback.LOADING -> LOADING_HINT
                    VoiceNotePlayback.FAILED -> FAILED_HINT
                    else -> PLAY_HINT
                },
                color = if (playback == VoiceNotePlayback.FAILED) MeshaColors.Danger else MeshaColors.Muted,
                style = MeshaType.cardSubtitle,
            )
        }
        Icon(MeshaIcons.Microphone, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(MeshaDimens.iconMd))
    }
}

private const val VOICE_NOTE_TITLE = "Voice note"
private const val PLAY_LABEL = "Play voice note"
private const val STOP_LABEL = "Stop voice note"
private const val PLAY_HINT = "Tap to listen"
private const val PLAYING_HINT = "Playing… tap to stop"
private const val LOADING_HINT = "Loading…"
private const val FAILED_HINT = "Could not play this note right now"
