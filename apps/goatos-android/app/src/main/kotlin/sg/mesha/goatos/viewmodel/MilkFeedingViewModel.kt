package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import javax.inject.Inject
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonPrimitive
import sg.mesha.goatos.capture.ProofCapturePrompt
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.MilkFeedingRepository
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.MilkFeedingAnswersDto
import sg.mesha.goatos.core.network.dto.MilkFeedingPageDto
import sg.mesha.goatos.core.network.dto.MilkFeedingNewRefusalDto
import sg.mesha.goatos.core.network.dto.MilkFeedingWatchlistAnswerDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.feature.counts.MilkFeedingEvent
import sg.mesha.goatos.feature.counts.MilkFeedingCardUi
import sg.mesha.goatos.feature.counts.MilkFeedingListEvent
import sg.mesha.goatos.feature.counts.MilkFeedingListUiState
import sg.mesha.goatos.feature.counts.MilkFeedingProofUi
import sg.mesha.goatos.feature.counts.MilkFeedingUiState
import sg.mesha.goatos.feature.counts.MilkFeedingWatchlistUi
import sg.mesha.goatos.feature.counts.MilkPreparationChipUi

private data class MilkFeedingDraft(
    val watchlistAnswers: Map<String, Boolean> = emptyMap(),
    val total: String = "", val attempt1: String = "", val attempt2: String = "", val ids: String = "", val remarks: String = "", val udder: String = "", val ors: String = "",
    val proofs: List<MilkFeedingProofUi> = listOf(MilkFeedingProofUi("clean_bottles", "Show clean bottles"), MilkFeedingProofUi("mixing_and_filling", "Mix milk and fill required bottles")),
    val submitting: Boolean = false, val queued: Boolean = false, val message: String? = null,
)

@HiltViewModel
class MilkFeedingListViewModel @Inject constructor(private val repo: MilkFeedingRepository) : ViewModel() {
    private val feedingDate = LocalDate.now(ZoneId.of("Asia/Kolkata")).toString()
    private val refreshing = MutableStateFlow(false)
    private val selectedFilter = MutableStateFlow("all")
    val state: StateFlow<MilkFeedingListUiState> = combine(repo.observe(feedingDate), refreshing, selectedFilter) { resource, busy, selected ->
        buildMilkFeedingListUi(resource.data, selected).copy(
            isRefreshing = busy,
            lastSyncedAt = resource.lastSyncedAt,
            isOffline = resource.data == null,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), MilkFeedingListUiState(dateLabel = feedingDate))
    init { refresh() }
    fun onEvent(event: MilkFeedingListEvent) {
        when (event) {
            MilkFeedingListEvent.Refresh -> refresh()
            is MilkFeedingListEvent.SelectFilter -> selectedFilter.value = event.key
            is MilkFeedingListEvent.OpenTask, MilkFeedingListEvent.Back -> Unit
        }
    }
    private fun refresh() = viewModelScope.launch { refreshing.value = true; repo.refresh(feedingDate); refreshing.value = false }
}

internal fun buildMilkFeedingListUi(page: MilkFeedingPageDto?, selectedFilter: String): MilkFeedingListUiState {
    val allCards = page?.items.orEmpty()
        .map { MilkFeedingCardUi(it.taskId, it.parkLabel, it.sessionNo, it.dueTime, it.headCount, it.verificationStatus, it.reworkReason, it.available, it.blockedReason) }
        .sortedWith(compareBy<MilkFeedingCardUi> { it.parkLabel }.thenBy { it.sessionNo })
    val cards = if (selectedFilter == "all") allCards else allCards.filter {
        if (selectedFilter == "not_submitted") it.status == selectedFilter && it.available else it.status == selectedFilter
    }
    val counts = allCards.groupingBy { it.status }.eachCount()
    val availableNotSubmitted = allCards.count { it.status == "not_submitted" && it.available }
    val needAction = allCards.count { it.canOpen }
    val date = page?.feedingDate.orEmpty()
    val dateLabel = runCatching {
        "Today · ${LocalDate.parse(date).format(DateTimeFormatter.ofPattern("d MMM"))}"
    }.getOrDefault(date)
    return MilkFeedingListUiState(
        subtitle = "${allCards.size} farm sessions · $needAction need action",
        dateLabel = dateLabel,
        chips = listOf(
            MilkPreparationChipUi("all", "All", allCards.size),
            MilkPreparationChipUi("not_submitted", "Need action", availableNotSubmitted),
            MilkPreparationChipUi("pending_verification", "In review", counts["pending_verification"] ?: 0),
            MilkPreparationChipUi("completed", "Completed", counts["completed"] ?: 0),
            MilkPreparationChipUi("rework", "Rework", counts["rework"] ?: 0),
        ),
        selectedFilter = selectedFilter,
        cards = cards,
        emptyMessage = if (cards.isEmpty()) "No Milk Feeding work in this status." else null,
    )
}

@HiltViewModel
class MilkFeedingViewModel @Inject constructor(
    private val repo: MilkFeedingRepository,
    private val sync: SyncRepository,
    private val capture: ProofCaptureSource,
    private val saved: SavedStateHandle,
) : ViewModel() {
    private val taskId = saved.get<String>(ARG_TASK_ID).orEmpty()
    private val feedingDate = LocalDate.now(ZoneId.of("Asia/Kolkata")).toString()
    private val submitKey = DraftIdempotencyKey(saved, "milkFeeding.submitKey.$taskId", "milk-feeding-submit")
    private val proofKeys = listOf("clean_bottles", "mixing_and_filling").associateWith { DraftIdempotencyKey(saved, "milkFeeding.proofKey.$taskId.$it", "milk-feeding-$it") }
    private val draft = MutableStateFlow(MilkFeedingDraft(proofs = MilkFeedingDraft().proofs.map { it.copy(captured = !saved.get<String>(proofItemKey(it.code)).isNullOrBlank()) }))

    val state: StateFlow<MilkFeedingUiState> = combine(repo.observe(feedingDate), draft) { resource, local ->
        val task = resource.data?.items?.firstOrNull { it.taskId == taskId }
        MilkFeedingUiState(
            taskId = taskId, parkId = task?.parkId.orEmpty(), parkLabel = task?.parkLabel.orEmpty(), feedingDate = task?.feedingDate ?: feedingDate,
            sessionNo = task?.sessionNo ?: 0, dueTime = task?.dueTime.orEmpty(), status = task?.verificationStatus ?: "not_submitted",
            watchlist = task?.watchlist.orEmpty().map { kid -> MilkFeedingWatchlistUi(kid.goatId, "Kid ${kid.goatId} · tracked since ${kid.addedDate}", local.watchlistAnswers[kid.goatId]) },
            totalKidsFed = local.total, attempt1NotDrinking = local.attempt1, attempt2NotDrinking = local.attempt2, newRefusalIds = local.ids,
            remarks = local.remarks, udderNotDrinking = local.udder, orsNotDrinking = local.ors, proofs = local.proofs,
            submitting = local.submitting, queued = local.queued, message = local.message,
            available = task?.available ?: false, blockedReason = task?.blockedReason.orEmpty(),
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), MilkFeedingUiState(taskId = taskId, feedingDate = feedingDate))

    init { viewModelScope.launch { repo.refresh(feedingDate) } }

    fun onEvent(event: MilkFeedingEvent) {
        if (event != MilkFeedingEvent.Back && !state.value.available) return
        when (event) {
            is MilkFeedingEvent.SetWatchlistAnswer -> draft.update { it.copy(watchlistAnswers = it.watchlistAnswers + (event.goatId to event.drank)) }
            is MilkFeedingEvent.SetNumber -> draft.update { current -> when (event.field) { "total" -> current.copy(total = event.value); "attempt1" -> current.copy(attempt1 = event.value); "attempt2" -> current.copy(attempt2 = event.value); "udder" -> current.copy(udder = event.value); else -> current.copy(ors = event.value) } }
            is MilkFeedingEvent.SetText -> draft.update { if (event.field == "ids") it.copy(ids = event.value) else it.copy(remarks = event.value) }
            is MilkFeedingEvent.CaptureProof -> captureProof(event.code)
            MilkFeedingEvent.Submit -> submit()
            MilkFeedingEvent.Back -> Unit
        }
    }

    private fun captureProof(code: String) = viewModelScope.launch {
        val proof = state.value.proofs.firstOrNull { it.code == code } ?: return@launch
        draft.update { it.copy(proofs = it.proofs.map { row -> if (row.code == code) row.copy(capturing = true) else row }) }
        val video = capture.captureVideo(ProofCapturePrompt.MILK_FEEDING, proof.label)
        if (video == null) { draft.update { it.copy(proofs = it.proofs.map { row -> if (row.code == code) row.copy(capturing = false) else row }) }; return@launch }
        val request = ProofUploadRequestDto(proofType = "video", mimeType = video.mimeType, scopeType = "park", scopeId = state.value.parkId, subjectType = "park", subjectId = state.value.parkId, metadata = mapOf("capture_source" to JsonPrimitive(video.captureSource), "captured_start_ms" to JsonPrimitive(video.startedAtMs), "captured_end_ms" to JsonPrimitive(video.endedAtMs), "milk_feeding_step" to JsonPrimitive(code), "verification_label" to JsonPrimitive(proof.label)))
        when (val result = sync.enqueueProofUpload(groupKey(), proofKeys.getValue(code).current(), request, video.localUri, video.endedAtMs - video.startedAtMs)) {
            is AppResult.Ok -> { saved[proofItemKey(code)] = result.value; draft.update { it.copy(proofs = it.proofs.map { row -> if (row.code == code) row.copy(captured = true, capturing = false) else row }) } }
            is AppResult.Err -> { proofKeys.getValue(code).invalidate(); draft.update { it.copy(message = result.message, proofs = it.proofs.map { row -> if (row.code == code) row.copy(capturing = false) else row }) } }
        }
    }

    private fun submit() = viewModelScope.launch {
        val current = state.value
        if (!current.canSubmit) return@launch
        val ids = current.newRefusalIds.split(',').map(String::trim).filter(String::isNotBlank).distinct()
        val answers = MilkFeedingAnswersDto(
            watchlistAnswers = current.watchlist.map { MilkFeedingWatchlistAnswerDto(it.goatId, it.drankMilk == true) }, totalKidsFed = current.totalKidsFed.toInt(),
            attempt1NotDrinking = current.attempt1NotDrinking.toInt(), attempt2NotDrinking = current.attempt2NotDrinking.toIntOrNull() ?: 0,
            newRefusals = ids.map { MilkFeedingNewRefusalDto(it, current.remarks) }, udderMilkNotDrinking = current.udderNotDrinking.toIntOrNull() ?: 0, orsNotDrinking = current.orsNotDrinking.toIntOrNull() ?: 0,
        )
        val clean = saved.get<String>(proofItemKey("clean_bottles")).orEmpty(); val mixing = saved.get<String>(proofItemKey("mixing_and_filling")).orEmpty()
        draft.update { it.copy(submitting = true, message = null) }
        when (val result = sync.enqueueMilkFeedingSubmit(groupKey(), submitKey.current(), current.taskId, current.parkId, current.feedingDate, current.sessionNo, answers, clean, mixing)) {
            is AppResult.Ok -> draft.update { it.copy(submitting = false, queued = true, message = "Answers and proofs are uploading in background.") }
            is AppResult.Err -> draft.update { it.copy(submitting = false, message = result.message) }
        }
    }

    private fun groupKey() = "milk-feeding:${state.value.parkId}:$feedingDate:${state.value.sessionNo}"
    private fun proofItemKey(code: String) = "milkFeeding.proofItem.$taskId.$code"
    companion object { const val ARG_TASK_ID = "task_id" }
}
