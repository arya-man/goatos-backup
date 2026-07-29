package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale
import javax.inject.Inject
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.weighing.WeighingLeadershipShed
import sg.mesha.goatos.core.data.weighing.WeighingRepository
import sg.mesha.goatos.feature.weighing.leadership.WeighingLeadershipAnimalUi
import sg.mesha.goatos.feature.weighing.leadership.WeighingLeadershipShedUi
import sg.mesha.goatos.feature.weighing.leadership.WeighingLeadershipVideoUi
import sg.mesha.goatos.feature.weighing.leadership.WeighingLeadershipVideosUiState

@HiltViewModel
class WeighingLeadershipVideosViewModel @Inject constructor(
    private val repository: WeighingRepository,
) : ViewModel() {
    private val mutableState = MutableStateFlow(WeighingLeadershipVideosUiState())
    val state: StateFlow<WeighingLeadershipVideosUiState> = mutableState.asStateFlow()

    init {
        refresh()
    }

    fun refresh() {
        viewModelScope.launch {
            mutableState.value = mutableState.value.copy(loading = true, error = null)
            mutableState.value = when (val result = repository.listLeadershipVideos()) {
                is AppResult.Ok -> WeighingLeadershipVideosUiState(
                    loading = false,
                    sheds = result.value.map(::toUi),
                )
                is AppResult.Err -> WeighingLeadershipVideosUiState(
                    loading = false,
                    error = result.message,
                )
            }
        }
    }

    private fun toUi(shed: WeighingLeadershipShed): WeighingLeadershipShedUi =
        WeighingLeadershipShedUi(
            id = shed.campaignShedId,
            name = shed.shedName,
            status = shed.status,
            periodLabel = shed.periodLabel,
            category = shed.category,
            animals = shed.animals.mapIndexed { index, animal ->
                WeighingLeadershipAnimalUi(
                    id = "${shed.campaignShedId}:${animal.rfid}:$index",
                    rfid = animal.rfid,
                    weight = formatWeight(animal.weightKg),
                    timestamp = formatTimestamp(animal.acceptedAt),
                    videos = animal.videos.mapIndexed { videoIndex, video ->
                        WeighingLeadershipVideoUi(video.proofId, "Video ${videoIndex + 1}", video.downloadUrl)
                    },
                )
            },
            animalCount = shed.animalCount?.toString(),
            totalWeight = shed.totalWeightKg?.let(::formatWeight),
            averageWeight = shed.averageWeightKg?.let(::formatWeight),
            videos = shed.videos.mapIndexed { index, video ->
                WeighingLeadershipVideoUi(video.proofId, "Video ${index + 1}", video.downloadUrl)
            },
        )

    private fun formatWeight(value: Double): String =
        String.format(Locale.US, "%.2f kg", value).replace(".00 kg", " kg")

    private fun formatTimestamp(value: String): String =
        runCatching { timestampFormatter.format(Instant.parse(value)) }.getOrDefault(value)

    private companion object {
        val timestampFormatter: DateTimeFormatter =
            DateTimeFormatter.ofPattern("dd MMM yyyy, h:mm a").withZone(ZoneId.systemDefault())
    }
}
