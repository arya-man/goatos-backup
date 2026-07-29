package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonPrimitive
import sg.mesha.goatos.capture.ProofCapturePrompt
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CountsRepository
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.feature.counts.MilkPreparationEvent
import sg.mesha.goatos.feature.counts.MilkPreparationParkUi
import sg.mesha.goatos.feature.counts.MilkPreparationStepUi
import sg.mesha.goatos.feature.counts.MilkPreparationUiState
import java.time.LocalDate
import java.time.ZoneId
import javax.inject.Inject

@HiltViewModel
class MilkPreparationViewModel @Inject constructor(
    private val sync: SyncRepository,
    private val counts: CountsRepository,
    private val capture: ProofCaptureSource,
    private val saved: SavedStateHandle,
) : ViewModel() {
    private val submitKey = DraftIdempotencyKey(saved, "milkPreparation.submitKey", "milk-preparation-submit")
    private val proofKeys = allSteps.associateWith { DraftIdempotencyKey(saved, "milkPreparation.proofKey.$it", "milk-preparation-$it") }
	private val initialGoatMilkUsed = saved.get<Boolean>("milkPreparation.goatMilkUsed") ?: true
    private val _state = MutableStateFlow(MilkPreparationUiState(
		preparationDate = LocalDate.now(ZoneId.of("Asia/Kolkata")).toString(),
		selectedParkId = saved.get<String>("milkPreparation.parkId").orEmpty(),
		goatMilkUsed = initialGoatMilkUsed,
		steps = applicableSteps(initialGoatMilkUsed),
		submitted = !saved.get<String>("milkPreparation.submitItem").isNullOrBlank(),
	))
    val state: StateFlow<MilkPreparationUiState> = _state.asStateFlow()

    init {
        allSteps.forEach { step -> if (!saved.get<String>(proofItemKey(step)).isNullOrBlank()) markCaptured(step) }
        viewModelScope.launch {
            counts.observeShiftingDestinations().collect { resource ->
                val parks = resource.data?.parks.orEmpty().map { MilkPreparationParkUi(it.parkId, it.name) }
                _state.update { current -> current.copy(parks = parks, loadingParks = false, selectedParkId = current.selectedParkId.ifBlank { parks.singleOrNull()?.id.orEmpty() }) }
            }
        }
        viewModelScope.launch { counts.refreshShiftingDestinations() }
    }

    fun onEvent(event: MilkPreparationEvent) { when (event) {
        is MilkPreparationEvent.SelectPark -> if (!_state.value.hasCaptured) { saved["milkPreparation.parkId"] = event.parkId; _state.update { it.copy(selectedParkId = event.parkId) } }
        is MilkPreparationEvent.SetGoatMilkUsed -> if (!_state.value.hasCaptured) { saved["milkPreparation.goatMilkUsed"] = event.used; _state.update { it.copy(goatMilkUsed = event.used, steps = applicableSteps(event.used)) } }
        is MilkPreparationEvent.CaptureStep -> captureStep(event.stepCode)
        MilkPreparationEvent.Submit -> submit()
    } }

    private fun captureStep(stepCode: String) {
        val current = _state.value
        val parkId = current.selectedParkId
        val step = current.steps.firstOrNull { it.code == stepCode } ?: return
        if (parkId.isBlank() || step.captured || step.capturing) return
        _state.update { it.copy(steps = it.steps.map { row -> if (row.code == stepCode) row.copy(capturing = true) else row }) }
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
                is AppResult.Ok -> { saved[proofItemKey(stepCode)] = result.value; markCaptured(stepCode) }
                is AppResult.Err -> { proofKeys.getValue(stepCode).invalidate(); setCapturing(stepCode, false); _state.update { it.copy(message = result.message) } }
            }
        }
    }

    private fun submit() {
        val current = _state.value
        if (!current.canSubmit) return
        val proofItems = current.steps.associate { it.code to saved.get<String>(proofItemKey(it.code)).orEmpty() }
        if (proofItems.values.any(String::isBlank)) return
        _state.update { it.copy(submitting = true, message = null) }
        viewModelScope.launch {
            when (val result = sync.enqueueMilkPreparationSubmit(groupKey(), submitKey.current(), current.selectedParkId, current.preparationDate, current.goatMilkUsed, proofItems)) {
				is AppResult.Ok -> { saved["milkPreparation.submitItem"] = result.value; _state.update { it.copy(submitting = false, submitted = true, message = "Submitted for verifier approval.") } }
                is AppResult.Err -> _state.update { it.copy(submitting = false, message = result.message) }
            }
        }
    }

    private fun groupKey() = "milk-preparation:${_state.value.selectedParkId}:${_state.value.preparationDate}"
    private fun proofItemKey(step: String) = "milkPreparation.proofItem.$step"
    private fun setCapturing(step: String, value: Boolean) = _state.update { it.copy(steps = it.steps.map { row -> if (row.code == step) row.copy(capturing = value) else row }) }
    private fun markCaptured(step: String) = _state.update { it.copy(steps = it.steps.map { row -> if (row.code == step) row.copy(captured = true, capturing = false) else row }) }

    companion object {
        private val allSteps = listOf("goat_milk_quantity", "boiling_temperature", "cooled_temperature", "uht_milk_quantity", "citric_acid_mixing")
        private val labels = mapOf(
            "goat_milk_quantity" to "Goat milk quantity",
            "boiling_temperature" to "Boiling temperature",
            "cooled_temperature" to "Cooled temperature",
            "uht_milk_quantity" to "UHT milk quantity",
            "citric_acid_mixing" to "Citric acid mixing",
        )
        private fun applicableSteps(goatMilk: Boolean) = allSteps.filter { goatMilk || it !in allSteps.take(3) }.map { MilkPreparationStepUi(it, labels.getValue(it)) }
    }
}
