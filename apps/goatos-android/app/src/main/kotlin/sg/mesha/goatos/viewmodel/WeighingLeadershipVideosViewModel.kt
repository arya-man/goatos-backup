package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale
import javax.inject.Inject
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import sg.mesha.goatos.BuildConfig
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

    private var nextCursor: String? = null

    fun refresh() {
        viewModelScope.launch {
            mutableState.value = mutableState.value.copy(loading = true, error = null)
            mutableState.value = when (val result = repository.listLeadershipVideos(cursor = null)) {
                is AppResult.Ok -> {
                    nextCursor = result.value.nextCursor
                    WeighingLeadershipVideosUiState(
                        loading = false,
                        sheds = result.value.items.map(::toUi),
                    )
                }
                is AppResult.Err -> {
                    nextCursor = null
                    WeighingLeadershipVideosUiState(
                        loading = false,
                        error = result.message,
                    )
                }
            }
        }
    }

    /**
     * Scroll-driven prefetch: the gallery reports the shed it just composed, and only a shed inside
     * the tail window of the loaded page asks for the next page. One page per trigger.
     */
    fun onShedVisible(index: Int) {
        val loaded = mutableState.value.sheds.size
        if (loaded == 0 || index < loaded - LIST_PREFETCH_DISTANCE) return
        appendNextPage()
    }

    private fun appendNextPage() {
        val cursor = nextCursor?.takeIf { it.isNotBlank() } ?: return
        val current = mutableState.value
        if (current.loading || current.loadingMore) return
        mutableState.value = current.copy(loadingMore = true)
        viewModelScope.launch {
            mutableState.value = when (val result = repository.listLeadershipVideos(cursor = cursor)) {
                is AppResult.Ok -> {
                    nextCursor = result.value.nextCursor?.takeIf { it.isNotBlank() && it != cursor }
                    val known = mutableState.value.sheds.map { it.id }.toSet()
                    val appended = result.value.items.map(::toUi).filter { it.id !in known }
                    mutableState.value.copy(
                        loadingMore = false,
                        sheds = mutableState.value.sheds + appended,
                    )
                }
                is AppResult.Err -> mutableState.value.copy(
                    loadingMore = false,
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
            periodLabel = formatPeriodLabel(shed.periodLabel),
            category = shed.category,
            animals = shed.animals.mapIndexed { index, animal ->
                WeighingLeadershipAnimalUi(
                    id = "${shed.campaignShedId}:${animal.rfid}:$index",
                    rfid = animal.rfid,
                    weight = formatWeight(animal.weightKg),
                    timestamp = formatTimestamp(animal.acceptedAt),
                    videos = animal.videos.mapIndexed { videoIndex, video ->
                        WeighingLeadershipVideoUi(video.proofId, "Video ${videoIndex + 1}", video.downloadUrl.toAbsoluteApiUrl())
                    },
                )
            },
            animalCount = shed.animalCount?.toString(),
            totalWeight = shed.totalWeightKg?.let(::formatWeight),
            averageWeight = shed.averageWeightKg?.let(::formatWeight),
            videos = shed.videos.mapIndexed { index, video ->
                WeighingLeadershipVideoUi(video.proofId, "Video ${index + 1}", video.downloadUrl.toAbsoluteApiUrl())
            },
        )

    private fun formatWeight(value: Double): String =
        String.format(Locale.US, "%.2f kg", value).replace(".00 kg", " kg")

    private fun formatTimestamp(value: String): String =
        runCatching { timestampFormatter.format(Instant.parse(value)) }.getOrDefault(value)

    private fun formatPeriodLabel(value: String): String {
        val parts = value.split(" - ")
        if (parts.size != 2) return value
        val start = runCatching { LocalDate.parse(parts[0].trim()) }.getOrNull() ?: return value
        val end = runCatching { LocalDate.parse(parts[1].trim()) }.getOrNull() ?: return value
        return "${periodFormatter.format(start)} - ${periodFormatter.format(end)}"
    }

    private fun String.toAbsoluteApiUrl(): String =
        when {
            startsWith("http://") || startsWith("https://") -> this
            startsWith("/") -> BuildConfig.API_BASE_URL.trimEnd('/') + this
            else -> this
        }

    private companion object {
        const val LIST_PREFETCH_DISTANCE = 3
        val timestampFormatter: DateTimeFormatter =
            DateTimeFormatter.ofPattern("dd MMM yyyy, h:mm a").withZone(ZoneId.systemDefault())
        val periodFormatter: DateTimeFormatter =
            DateTimeFormatter.ofPattern("d MMM", Locale.US)
    }
}
