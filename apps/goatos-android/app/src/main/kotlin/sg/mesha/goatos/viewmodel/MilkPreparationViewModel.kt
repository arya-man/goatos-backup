package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale
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
import sg.mesha.goatos.core.data.MilkPreparationRepository
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.MilkPreparationAnswersPayload
import sg.mesha.goatos.core.network.dto.MilkPreparationPageDto
import sg.mesha.goatos.core.network.dto.MilkPreparationFarmTaskDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.feature.counts.MilkPreparationCardBucket
import sg.mesha.goatos.feature.counts.MilkPreparationCardUi
import sg.mesha.goatos.feature.counts.MilkPreparationChipUi
import sg.mesha.goatos.feature.counts.MilkPreparationEvent
import sg.mesha.goatos.feature.counts.MilkPreparationListEvent
import sg.mesha.goatos.feature.counts.MilkPreparationListUiState
import sg.mesha.goatos.feature.counts.MilkPreparationStepUi
import sg.mesha.goatos.feature.counts.MilkPreparationUiState

private val MILK_IST: ZoneId = ZoneId.of("Asia/Kolkata")
private val MILK_DAY_LABEL: DateTimeFormatter = DateTimeFormatter.ofPattern("d MMM")

private data class MilkPreparationRefreshState(
    val isRefreshing: Boolean = false,
    val isOffline: Boolean = false,
)

@HiltViewModel
class MilkPreparationListViewModel @Inject constructor(
    private val repo: MilkPreparationRepository,
) : ViewModel() {
    private val today = LocalDate.now(MILK_IST).toString()
    private val selectedFilter = MutableStateFlow("all")
    private val refresh = MutableStateFlow(MilkPreparationRefreshState())

    val state: StateFlow<MilkPreparationListUiState> = combine(
        repo.observe(today),
        selectedFilter,
        refresh,
    ) { resource, selected, sync ->
        val page = resource.data
        if (page == null) {
            MilkPreparationListUiState(
                selectedFilter = selected,
                dateLabel = "Today · ${LocalDate.parse(today).format(MILK_DAY_LABEL)}",
                isRefreshing = sync.isRefreshing,
                isOffline = sync.isOffline,
                emptyMessage = if (sync.isOffline) "Couldn't load Milk Preparation. It will appear once you're back online." else null,
            )
        } else {
            buildMilkPreparationListUi(page, selected).copy(
                isRefreshing = sync.isRefreshing,
                isOffline = sync.isOffline,
                lastSyncedAt = resource.lastSyncedAt,
            )
        }
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        MilkPreparationListUiState(dateLabel = "Today · ${LocalDate.parse(today).format(MILK_DAY_LABEL)}"),
    )

    init { refresh() }

    fun onEvent(event: MilkPreparationListEvent) {
        when (event) {
            MilkPreparationListEvent.Refresh -> refresh()
            is MilkPreparationListEvent.SelectFilter -> selectedFilter.value = event.key
            is MilkPreparationListEvent.OpenFarm, MilkPreparationListEvent.Back -> Unit
        }
    }

    private fun refresh() = viewModelScope.launch {
        refresh.value = MilkPreparationRefreshState(isRefreshing = true)
        val preparationResult = repo.refresh(today)
        refresh.value = MilkPreparationRefreshState(isOffline = preparationResult.isFailure)
    }
}

internal fun buildMilkPreparationListUi(
    page: MilkPreparationPageDto?,
    selectedFilter: String,
): MilkPreparationListUiState {
    val allCards = page?.farmTasks.orEmpty().map(::milkPreparationCard).sortedBy { it.parkLabel }
    val cards = if (selectedFilter == "all") allCards else allCards.filter { card ->
        when (selectedFilter) {
            "not_submitted" -> card.bucket == MilkPreparationCardBucket.TO_PREPARE
            "pending_verification" -> card.bucket == MilkPreparationCardBucket.IN_REVIEW
            "completed" -> card.bucket == MilkPreparationCardBucket.COMPLETED
            "rework" -> card.bucket == MilkPreparationCardBucket.REWORK
            else -> true
        }
    }
    val counts = allCards.groupingBy { it.bucket }.eachCount()
    val toPrepare = counts[MilkPreparationCardBucket.TO_PREPARE] ?: 0
    return MilkPreparationListUiState(
        subtitle = "${allCards.size} ${if (allCards.size == 1) "farm" else "farms"} · $toPrepare need action",
        dateLabel = page?.preparationDate.orEmpty().toMilkDateLabel("Today · "),
        feedingDateLabel = page?.feedingDate?.toMilkDateLabel().orEmpty(),
        chips = listOf(
            MilkPreparationChipUi("all", "All", allCards.size),
            MilkPreparationChipUi("not_submitted", "Need action", toPrepare),
            MilkPreparationChipUi("pending_verification", "In review", counts[MilkPreparationCardBucket.IN_REVIEW] ?: 0),
            MilkPreparationChipUi("completed", "Completed", counts[MilkPreparationCardBucket.COMPLETED] ?: 0),
            MilkPreparationChipUi("rework", "Rework", counts[MilkPreparationCardBucket.REWORK] ?: 0),
        ),
        selectedFilter = selectedFilter,
        cards = cards,
        emptyMessage = if (cards.isEmpty()) "No Milk Preparation work in this status." else null,
    )
}

private fun milkPreparationCard(task: MilkPreparationFarmTaskDto): MilkPreparationCardUi {
    val bucket = when (task.verificationStatus) {
        "pending_verification" -> MilkPreparationCardBucket.IN_REVIEW
        "completed" -> MilkPreparationCardBucket.COMPLETED
        "rework" -> MilkPreparationCardBucket.REWORK
        else -> MilkPreparationCardBucket.TO_PREPARE
    }
    val (status, action) = when (bucket) {
        MilkPreparationCardBucket.TO_PREPARE -> "To prepare" to "Prepare and record videos"
        MilkPreparationCardBucket.IN_REVIEW -> "In review" to "Proof submitted"
        MilkPreparationCardBucket.COMPLETED -> "Completed" to "Preparation verified"
        MilkPreparationCardBucket.REWORK -> "Rework" to "Record a fresh proof set"
    }
    return MilkPreparationCardUi(
        parkId = task.parkId,
        parkLabel = task.parkLabel,
        shedId = task.parkId,
        shedLabel = "",
        cohortCount = task.cohortCount,
        headCount = task.headCount,
        totalMilkLabel = "${formatLitres(task.totalRequiredMl)} milk",
        citricAcidLabel = "${formatDecimal(task.citricAcidGrams)} g citric acid",
        statusLabel = status,
        actionLabel = if (bucket == MilkPreparationCardBucket.TO_PREPARE) "Prepare" else action,
        reworkReason = task.reworkReason,
        bucket = bucket,
        detailLabel = "${task.cohortCount} cohorts · ${task.headCount} animals",
    )
}

private fun String.toMilkDateLabel(prefix: String = ""): String =
    runCatching { prefix + LocalDate.parse(this).format(MILK_DAY_LABEL) }.getOrDefault(this)

private fun formatLitres(millilitres: Long): String = formatDecimal(millilitres.toDouble() / 1_000.0) + " L"
private fun formatDecimal(value: Double): String =
    if (value % 1.0 == 0.0) value.toLong().toString() else String.format(Locale.US, "%.1f", value)

private data class MilkPreparationDraftState(
    val goatMilkUsed: Boolean?,
    val steps: List<MilkPreparationStepUi>,
    val morningMilkCollected: String = "",
    val eveningMilkCollected: String = "",
    val message: String? = null,
    val submitting: Boolean = false,
    val queued: Boolean = false,
)

@HiltViewModel
class MilkPreparationViewModel @Inject constructor(
    private val sync: SyncRepository,
    private val repo: MilkPreparationRepository,
    private val capture: ProofCaptureSource,
    private val saved: SavedStateHandle,
) : ViewModel() {
    private val parkId = saved.get<String>(ARG_PARK_ID).orEmpty()
    private val preparationDate = LocalDate.now(MILK_IST).toString()
    private val submitKey = DraftIdempotencyKey(saved, "milkPreparation.submitKey", "milk-preparation-submit")
    private val proofKeys = allSteps.associateWith { DraftIdempotencyKey(saved, "milkPreparation.proofKey.$it", "milk-preparation-$it") }
    private val initialGoatMilkUsed = saved.get<Boolean>("milkPreparation.goatMilkUsed")
    private val refresh = MutableStateFlow(MilkPreparationRefreshState())
    private val draft = MutableStateFlow(
        MilkPreparationDraftState(
            goatMilkUsed = initialGoatMilkUsed,
            steps = initialGoatMilkUsed?.let(::applicableSteps).orEmpty().map { step ->
                step.copy(
                    captured = !saved.get<String>(proofItemKey(step.code)).isNullOrBlank(),
                    answer = saved.get<String>(answerKey(step.code)).orEmpty(),
                )
            },
            morningMilkCollected = saved.get<String>("milkPreparation.answer.morning").orEmpty(),
            eveningMilkCollected = saved.get<String>("milkPreparation.answer.evening").orEmpty(),
            queued = !saved.get<String>("milkPreparation.submitItem").isNullOrBlank(),
        ),
    )

    val state: StateFlow<MilkPreparationUiState> = combine(repo.observe(preparationDate), draft, refresh) { resource, local, syncState ->
        val page = resource.data
        val task = page?.farmTasks?.firstOrNull { it.parkId == parkId }
        val backendSubmitted = task?.verificationStatus == "pending_verification" || task?.verificationStatus == "completed"
        MilkPreparationUiState(
            preparationDate = page?.preparationDate ?: preparationDate,
            feedingDate = page?.feedingDate.orEmpty(),
            selectedParkId = task?.parkId ?: parkId,
            parkLabel = task?.parkLabel.orEmpty(),
            goatMilkUsed = local.goatMilkUsed,
            morningMilkCollected = local.morningMilkCollected,
            eveningMilkCollected = local.eveningMilkCollected,
            steps = sequenceMilkPreparationSteps(local.steps.map { step ->
                if (step.code == "citric_acid_mixing" && task != null) {
                    step.copy(label = "Add ${formatDecimal(task.citricAcidGrams)} g citric acid, mix it and store outside")
                } else {
                    step
                }
            }, questionsComplete = local.goatMilkUsed != null && local.morningMilkCollected.toDoubleOrNull()?.let { it >= 0 } == true && local.eveningMilkCollected.toDoubleOrNull()?.let { it >= 0 } == true),
            message = local.message,
            submitting = local.submitting,
            submitted = local.queued || backendSubmitted,
            verificationStatus = task?.verificationStatus.orEmpty(),
            reworkReason = task?.reworkReason.orEmpty(),
            cohortCount = task?.cohortCount ?: 0,
            headCount = task?.headCount ?: 0,
            totalMilkLabel = task?.let { "${formatLitres(it.totalRequiredMl)} milk" }.orEmpty(),
            citricAcidLabel = task?.let { "${formatDecimal(it.citricAcidGrams)} g citric acid" }.orEmpty(),
            citricAcidGrams = task?.citricAcidGrams ?: 0.0,
            milkDirectionLines = task?.milkDirection.orEmpty().map { line ->
                "${line.managementStage}: ${line.headCount} kids × ${line.perHeadMl}ml × ${line.sessionCount} sessions = ${formatLitres(line.requiredMl)}"
            },
            citricAcidRateLabel = task?.let { "${formatDecimal(it.citricAcidGramsPerLitre)} g/L" }.orEmpty(),
            isRefreshing = syncState.isRefreshing,
            isOffline = syncState.isOffline,
            lastSyncedAt = resource.lastSyncedAt,
        )
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        MilkPreparationUiState(preparationDate = preparationDate, selectedParkId = parkId, goatMilkUsed = initialGoatMilkUsed, steps = initialGoatMilkUsed?.let(::applicableSteps).orEmpty()),
    )

    init { refresh() }

    fun onEvent(event: MilkPreparationEvent) {
        when (event) {
            is MilkPreparationEvent.SetGoatMilkUsed -> if (state.value.goatMilkQuestionEnabled) {
                saved["milkPreparation.goatMilkUsed"] = event.used
                draft.update { current -> current.copy(goatMilkUsed = event.used, steps = applicableSteps(event.used).map { step -> step.copy(answer = saved.get<String>(answerKey(step.code)).orEmpty()) }) }
            }
            is MilkPreparationEvent.CaptureStep -> captureStep(event.stepCode)
            is MilkPreparationEvent.SetCollectedMilk -> {
                val current = state.value
                if ((event.shift == "morning" && !current.morningQuestionEnabled) || (event.shift == "evening" && !current.eveningQuestionEnabled)) return
                val key = "milkPreparation.answer.${event.shift}"
                saved[key] = event.value
                draft.update { if (event.shift == "morning") it.copy(morningMilkCollected = event.value) else it.copy(eveningMilkCollected = event.value) }
            }
            is MilkPreparationEvent.SetStepAnswer -> {
                if (state.value.steps.firstOrNull { it.code == event.stepCode }?.enabled != true) return
                saved[answerKey(event.stepCode)] = event.value
                draft.update { current -> current.copy(steps = current.steps.map { if (it.code == event.stepCode) it.copy(answer = event.value) else it }) }
            }
            MilkPreparationEvent.Submit -> submit()
            MilkPreparationEvent.Refresh -> refresh()
            MilkPreparationEvent.Back -> Unit
        }
    }

    private fun refresh() = viewModelScope.launch {
        refresh.value = MilkPreparationRefreshState(isRefreshing = true)
        val preparationResult = repo.refresh(preparationDate)
        refresh.value = MilkPreparationRefreshState(isOffline = preparationResult.isFailure)
    }

    private fun captureStep(stepCode: String) {
        val current = state.value
        val step = current.steps.firstOrNull { it.code == stepCode } ?: return
        if (!current.isEditable || parkId.isBlank() || !step.enabled || !step.answerComplete || step.captured || step.capturing) return
        draft.update { it.copy(steps = it.steps.map { row -> if (row.code == stepCode) row.copy(capturing = true) else row }) }
        viewModelScope.launch {
            val video = capture.captureVideo(ProofCapturePrompt.MILK_PREPARATION, step.label)
            if (video == null) { setCapturing(stepCode, false); return@launch }
            val request = ProofUploadRequestDto(
                proofType = "video", mimeType = video.mimeType, scopeType = "park", scopeId = parkId,
                subjectType = "park", subjectId = parkId,
                metadata = mapOf(
                    "capture_source" to JsonPrimitive(video.captureSource),
                    "captured_start_ms" to JsonPrimitive(video.startedAtMs),
                    "captured_end_ms" to JsonPrimitive(video.endedAtMs),
                    "milk_preparation_step" to JsonPrimitive(stepCode),
                    "verification_label" to JsonPrimitive(step.label),
                ),
            )
            when (val result = sync.enqueueProofUpload(groupKey(), proofKeys.getValue(stepCode).current(), request, video.localUri, video.endedAtMs - video.startedAtMs)) {
                is AppResult.Ok -> {
                    saved[proofItemKey(stepCode)] = result.value
                    draft.update { it.copy(steps = it.steps.map { row -> if (row.code == stepCode) row.copy(captured = true, capturing = false) else row }) }
                }
                is AppResult.Err -> {
                    proofKeys.getValue(stepCode).invalidate()
                    setCapturing(stepCode, false)
                    draft.update { it.copy(message = result.message) }
                }
            }
        }
    }

    private fun submit() {
        val current = state.value
        if (!current.canSubmit) return
        val proofItems = current.steps.associate { it.code to saved.get<String>(proofItemKey(it.code)).orEmpty() }
        if (proofItems.values.any(String::isBlank)) return
        draft.update { it.copy(submitting = true, message = null) }
        viewModelScope.launch {
            val answers = milkPreparationAnswers(current)
            val goatMilkUsed = current.goatMilkUsed ?: return@launch
            when (val result = sync.enqueueMilkPreparationSubmit(groupKey(), submitKey.current(), current.selectedParkId, current.preparationDate, goatMilkUsed, answers, proofItems)) {
                is AppResult.Ok -> {
                    saved["milkPreparation.submitItem"] = result.value
                    draft.update { it.copy(submitting = false, queued = true, message = "Proof uploads in background.") }
                }
                is AppResult.Err -> draft.update { it.copy(submitting = false, message = result.message) }
            }
        }
    }

    private fun groupKey() = "milk-preparation:$parkId:$preparationDate"
    private fun proofItemKey(step: String) = "milkPreparation.proofItem.$step"
    private fun answerKey(step: String) = "milkPreparation.answer.$step"
    private fun setCapturing(step: String, value: Boolean) = draft.update { current ->
        current.copy(steps = current.steps.map { row -> if (row.code == step) row.copy(capturing = value) else row })
    }

    companion object {
        const val ARG_PARK_ID = "park_id"
        private val allSteps = listOf("goat_milk_quantity", "boiling_temperature", "cooled_temperature", "uht_milk_quantity", "citric_acid_mixing")
        private val labels = mapOf(
            "goat_milk_quantity" to ("Quantity of goat milk used" to "L"),
            "boiling_temperature" to ("Boiling temperature" to "°C"),
            "cooled_temperature" to ("Cooled temperature" to "°C"),
            "uht_milk_quantity" to ("Quantity of UHT milk" to "L"),
            "citric_acid_mixing" to ("Citric acid added, mixed and stored outside" to ""),
        )
        private fun applicableSteps(goatMilk: Boolean) = allSteps
            .filter { goatMilk || it !in allSteps.take(3) }
            .map { code ->
                labels.getValue(code).let { (label, unit) ->
                    MilkPreparationStepUi(
                        code = code,
                        label = label,
                        unit = unit,
                        requiresAnswer = code != "citric_acid_mixing",
                    )
                }
            }
    }
}

internal fun milkPreparationAnswers(state: MilkPreparationUiState): MilkPreparationAnswersPayload {
    val answerByStep = state.steps.associate { it.code to (it.answer.toDoubleOrNull() ?: 0.0) }
    return MilkPreparationAnswersPayload(
        morningMilkCollectedLitres = state.morningMilkCollected.toDouble(),
        eveningMilkCollectedLitres = state.eveningMilkCollected.toDouble(),
        goatMilkQuantityLitres = answerByStep["goat_milk_quantity"] ?: 0.0,
        boilingTemperatureC = answerByStep["boiling_temperature"] ?: 0.0,
        cooledTemperatureC = answerByStep["cooled_temperature"] ?: 0.0,
        uhtMilkQuantityLitres = answerByStep.getValue("uht_milk_quantity"),
        citricAcidGrams = state.citricAcidGrams,
    )
}

internal fun sequenceMilkPreparationSteps(
    steps: List<MilkPreparationStepUi>,
    questionsComplete: Boolean,
): List<MilkPreparationStepUi> {
    var unlocked = questionsComplete
    return steps.map { step ->
        val sequenced = step.copy(enabled = unlocked)
        unlocked = unlocked && step.answerComplete && step.captured
        sequenced
    }
}
