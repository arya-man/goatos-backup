package sg.mesha.goatos.feature.weighing.leadership

import android.net.Uri
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.ui.unit.dp
import androidx.media3.common.MediaItem
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.PlayerView
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.theme.MeshaColors

data class WeighingLeadershipVideosUiState(
    val loading: Boolean = true,
    val sheds: List<WeighingLeadershipShedUi> = emptyList(),
    val error: String? = null,
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

@Composable
fun WeighingLeadershipVideosScreen(
    state: WeighingLeadershipVideosUiState,
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.Bg)) {
        MeshaScreenHeader(eyebrow = "WEIGHING", title = "Videos")
        when {
            state.loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                CircularProgressIndicator(color = MeshaColors.Teal)
            }
            state.error != null -> EmptyMessage(state.error)
            state.sheds.isEmpty() -> EmptyMessage("No weighing videos yet")
            else -> LazyColumn(
                modifier = Modifier.fillMaxSize(),
                contentPadding = androidx.compose.foundation.layout.PaddingValues(16.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                items(state.sheds, key = { it.id }) { shed ->
                    LeadershipShedCard(shed)
                }
            }
        }
    }
}

@Composable
private fun LeadershipShedCard(shed: WeighingLeadershipShedUi) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(8.dp))
            .background(MeshaColors.Surf)
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(shed.name, color = Color.White, fontWeight = FontWeight.SemiBold)
                if (shed.periodLabel.isNotBlank()) {
                    Text(shed.periodLabel, color = MeshaColors.Muted)
                }
            }
            Text(shed.status.replaceFirstChar(Char::uppercase), color = MeshaColors.Teal)
        }
        HorizontalDivider(color = MeshaColors.Hair)
        if (shed.category == "per_shed_partition") {
            LumpSumSummary(shed)
            shed.videos.forEach { LeadershipVideo(it) }
        } else {
            shed.animals.forEachIndexed { index, animal ->
                if (index > 0) HorizontalDivider(color = MeshaColors.Hair)
                IndividualAnimal(animal)
            }
            if (shed.animals.isEmpty()) {
                Text("No animals submitted", color = MeshaColors.Muted)
            }
        }
    }
}

@Composable
private fun IndividualAnimal(animal: WeighingLeadershipAnimalUi) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
            Text(animal.rfid, color = Color.White, fontWeight = FontWeight.Medium)
            Text(animal.weight, color = MeshaColors.Teal, fontWeight = FontWeight.SemiBold)
        }
        Text(animal.timestamp, color = MeshaColors.Muted)
        animal.videos.forEach { LeadershipVideo(it) }
    }
}

@Composable
private fun LumpSumSummary(shed: WeighingLeadershipShedUi) {
    Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
        Metric("Animals", shed.animalCount.orEmpty())
        Metric("Total", shed.totalWeight.orEmpty())
        Metric("Average", shed.averageWeight.orEmpty())
    }
}

@Composable
private fun Metric(label: String, value: String) {
    Column(horizontalAlignment = Alignment.CenterHorizontally) {
        Text(value, color = Color.White, fontWeight = FontWeight.SemiBold)
        Text(label, color = MeshaColors.Muted)
    }
}

@Composable
private fun LeadershipVideo(video: WeighingLeadershipVideoUi) {
    val context = LocalContext.current
    val player = remember(video.url) {
        ExoPlayer.Builder(context).build().apply {
            setMediaItem(MediaItem.fromUri(Uri.parse(video.url)))
            prepare()
            playWhenReady = false
        }
    }
    DisposableEffect(player) {
        onDispose { player.release() }
    }
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text(video.label, color = MeshaColors.Muted)
        AndroidView(
            factory = { PlayerView(it).apply { this.player = player } },
            modifier = Modifier
                .fillMaxWidth()
                .aspectRatio(16f / 9f)
                .clip(RoundedCornerShape(8.dp))
                .background(Color.Black),
        )
    }
}

@Composable
private fun EmptyMessage(message: String) {
    Box(Modifier.fillMaxSize().padding(24.dp), contentAlignment = Alignment.Center) {
        Text(message, color = MeshaColors.Muted)
    }
}
