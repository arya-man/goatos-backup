package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.weighing.IndividualWeighingCapture
import sg.mesha.goatos.core.data.weighing.ShedPartitionWeighingCapture
import sg.mesha.goatos.core.data.weighing.WeighingAssignment
import sg.mesha.goatos.core.data.weighing.WeighingRepository
import sg.mesha.goatos.core.data.weighing.WeighingRosterRowEntity
import sg.mesha.goatos.core.data.weighing.WeighingScopeState
import sg.mesha.goatos.core.data.weighing.weighingScopeKey
import sg.mesha.goatos.feature.weighing.WeighingAssignmentUiRow
import sg.mesha.goatos.feature.weighing.WeighingDraftUiRow
import sg.mesha.goatos.feature.weighing.WeighingRosterUiRow
import sg.mesha.goatos.feature.weighing.WeighingUiState
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.ui.Routes
import javax.inject.Inject

@HiltViewModel
class WeighingViewModel @Inject constructor(
    private val repository: WeighingRepository,
    private val reader: RfidReaderPort,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val proofCaptureSource: ProofCaptureSource,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {
    private val campaignId = savedStateHandle.get<String>(Routes.WEIGHING_CAMPAIGN_ARG).orEmpty()
    private val workGroupId = savedStateHandle.get<String>(Routes.WEIGHING_WORK_GROUP_ARG).orEmpty()
    private val campaignShedId = savedStateHandle.get<String>(Routes.WEIGHING_CAMPAIGN_SHED_ARG).orEmpty()
    private val category = savedStateHandle.get<String>(Routes.WEIGHING_CATEGORY_ARG).orEmpty()
    private val tenantId = savedStateHandle.get<String>(Routes.WEIGHING_TENANT_ARG).orEmpty()
    private val expectedLocationId = savedStateHandle.get<String>(Routes.WEIGHING_EXPECTED_LOCATION_ARG).orEmpty()
    private val expectedLocationLabel = savedStateHandle.get<String>(Routes.WEIGHING_EXPECTED_LOCATION_LABEL_ARG).orEmpty()
    private val routeTitle = savedStateHandle.get<String>(Routes.EXECUTION_SCAN_TITLE_ARG).orEmpty()
    private val scopeKey = listOf(campaignId, workGroupId, campaignShedId)
        .takeIf { parts -> parts.all { it.isNotBlank() } }
        ?.let { weighingScopeKey(campaignId, workGroupId, campaignShedId) }
    private val scanInput = MutableStateFlow("")
    private val weightInput = MutableStateFlow("")
    private val selectedRow = MutableStateFlow<WeighingRosterRowEntity?>(null)
    private val assignments = MutableStateFlow<List<WeighingAssignment>>(emptyList())
    private val message = MutableStateFlow<String?>(null)
    private val actionInFlight = MutableStateFlow(false)
    private var readerRefreshJob: Job? = null

    @OptIn(ExperimentalCoroutinesApi::class)
    private val scopeState: StateFlow<WeighingScopeState?> =
        flowOf(scopeKey).flatMapLatest { key ->
            if (key == null) {
                flowOf(null)
            } else {
                repository.observeScope(key, ROSTER_WINDOW_SIZE)
            }
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)

    private val formState: StateFlow<WeighingFormState> =
        combine(scanInput, weightInput, selectedRow, message, actionInFlight) { scan, weight, selected, currentMessage, busy ->
            WeighingFormState(scan, weight, selected, currentMessage, busy)
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeighingFormState())

    val state: StateFlow<WeighingUiState> =
        combine(scopeState, formState, assignments) { scope, form, availableAssignments ->
            scope.toUiState(form.scan, form.weight, form.selected, form.message, form.busy, availableAssignments)
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeighingUiState())

    init {
        if (scopeKey != null) {
            viewModelScope.launch {
                when (val refreshed = repository.refreshScope(campaignId, workGroupId, campaignShedId, ROSTER_SYNC_LIMIT)) {
                    is AppResult.Ok -> {
                        if (refreshed.value == 0 && category != PER_SHED_PARTITION_CATEGORY) {
                            message.value = "No animals are assigned to this weighing scope."
                        }
                    }
                    is AppResult.Err -> message.value = refreshed.message
                }
            }
            viewModelScope.launch {
                reader.reads.collect { read -> matchTag(read.tag) }
            }
            viewModelScope.launch {
                proofCaptureRepository.observeProofs(scopeKey).collect { proofs ->
                    proofs.forEach { proof ->
                        val serverProofId = proof.serverProofId?.takeIf { it.isNotBlank() } ?: return@forEach
                        when (proof.fieldKey) {
                            INDIVIDUAL_PROOF_FIELD_KEY -> {
                                val animalId = proof.subjectId?.takeIf { it.isNotBlank() } ?: return@forEach
                                repository.attachIndividualProof(scopeKey, animalId, proof.id, serverProofId)
                            }
                            SHED_PARTITION_PROOF_FIELD_KEY -> repository.attachShedPartitionProof(scopeKey, proof.id, serverProofId)
                        }
                    }
                }
            }
        } else {
            viewModelScope.launch {
                when (val loaded = repository.listAssignments()) {
                    is AppResult.Ok -> assignments.value = loaded.value
                    is AppResult.Err -> message.value = loaded.message
                }
            }
        }
    }

    fun setCaptureActive(active: Boolean) {
        reader.setCaptureEnabled(active)
        if (active) {
            reader.refreshStatus()
            if (readerRefreshJob?.isActive == true) return
            readerRefreshJob = viewModelScope.launch {
                while (true) {
                    reader.refreshStatus()
                    delay(READER_REFRESH_MS)
                }
            }
        } else {
            readerRefreshJob?.cancel()
            readerRefreshJob = null
        }
    }

    fun setCompletionKeySwallowActive(active: Boolean) {
        reader.setCompletionKeySwallowEnabled(active)
    }

    fun onScanInputChange(value: String) {
        scanInput.value = value
    }

    fun onWeightInputChange(value: String) {
        weightInput.value = value.filter { it.isDigit() || it == '.' }.take(8)
    }

    fun submitTypedScan() {
        val tag = scanInput.value
        if (tag.isNotBlank()) matchTag(tag)
    }

    fun recordIndividual() {
        val key = scopeKey ?: return
        val row = selectedRow.value ?: return
        val weightKg = weightInput.value.toDoubleOrNull()?.takeIf { it > 0.0 } ?: return
        if (actionInFlight.value) return
        actionInFlight.value = true
        viewModelScope.launch {
            try {
                when (val recorded = repository.recordIndividual(
                    IndividualWeighingCapture(
                        tenantId = row.tenantId,
                        campaignId = campaignId,
                        workGroupId = workGroupId,
                        campaignShedId = campaignShedId,
                        animalId = row.animalId,
                        scannedIdentifier = scanInput.value.ifBlank { row.primaryTag },
                        weightKg = weightKg,
                    ),
                )) {
                    is AppResult.Ok -> {
                        val captured = proofCaptureSource.captureVideo()
                        if (captured == null) {
                            message.value = "Weight saved locally. Video proof is still required."
                            return@launch
                        }
                        val proof = proofCaptureRepository.capture(
                            taskId = key,
                            fieldKey = INDIVIDUAL_PROOF_FIELD_KEY,
                            subject = ProofSubject.GOAT,
                            subjectId = row.animalId,
                            localUri = captured.localUri,
                            mimeType = captured.mimeType,
                            caption = null,
                            scopeType = "animal",
                            scopeId = row.animalId,
                            capturedStartMs = captured.startedAtMs,
                            capturedEndMs = captured.endedAtMs,
                            capturedByPrincipalId = null,
                            proofPolicy = ProofPolicy.Default,
                        )
                        if (proof is AppResult.Ok) {
                            repository.attachIndividualProof(key, row.animalId, proof.value.id, proof.value.serverProofId)
                            message.value = "Weight and proof saved locally for ${row.displayAnimalId}."
                            weightInput.value = ""
                            scanInput.value = ""
                        } else {
                            message.value = "Weight saved locally. Proof could not be stored."
                        }
                        recorded.value
                    }
                    is AppResult.Err -> message.value = recorded.message
                }
            } finally {
                actionInFlight.value = false
            }
        }
    }

    fun recordShedPartition() {
        val key = scopeKey ?: return
        val weightKg = weightInput.value.toDoubleOrNull()?.takeIf { it > 0.0 } ?: return
        if (actionInFlight.value) return
        if (category != PER_SHED_PARTITION_CATEGORY) {
            message.value = "This weighing scope expects animal RFID scans."
            return
        }
        if (tenantId.isBlank() || expectedLocationId.isBlank()) {
            message.value = "Shed / partition assignment is missing location details."
            return
        }
        actionInFlight.value = true
        viewModelScope.launch {
            try {
				val resultJson = buildJsonObject {
					put("weight", weightKg)
					put("category", PER_SHED_PARTITION_CATEGORY)
					put("expected_location_id", expectedLocationId)
					put("expected_location_label", expectedLocationLabel.ifBlank { routeTitle })
                }.toString()
                when (val recorded = repository.recordShedPartition(
                    ShedPartitionWeighingCapture(
                        tenantId = tenantId,
                        campaignId = campaignId,
                        workGroupId = workGroupId,
                        campaignShedId = campaignShedId,
                        expectedLocationId = expectedLocationId,
                        expectedLocationLabel = expectedLocationLabel.ifBlank { routeTitle },
                        resultJson = resultJson,
                    ),
                )) {
                    is AppResult.Ok -> {
                        val captured = proofCaptureSource.captureVideo()
                        if (captured == null) {
                            message.value = "Shed weight saved locally. Video proof is still required."
                            return@launch
                        }
                        val proof = proofCaptureRepository.capture(
                            taskId = key,
                            fieldKey = SHED_PARTITION_PROOF_FIELD_KEY,
                            subject = ProofSubject.SHED,
                            subjectId = campaignShedId,
                            localUri = captured.localUri,
                            mimeType = captured.mimeType,
                            caption = null,
                            scopeType = "shed_partition",
                            scopeId = campaignShedId,
                            capturedStartMs = captured.startedAtMs,
                            capturedEndMs = captured.endedAtMs,
                            capturedByPrincipalId = null,
                            proofPolicy = ProofPolicy.Default,
                        )
                        if (proof is AppResult.Ok) {
                            repository.attachShedPartitionProof(key, proof.value.id, proof.value.serverProofId)
                            message.value = "Shed weight and proof saved locally."
                            weightInput.value = ""
                        } else {
                            message.value = "Shed weight saved locally. Proof could not be stored."
                        }
                        recorded.value
                    }
                    is AppResult.Err -> message.value = recorded.message
                }
            } finally {
                actionInFlight.value = false
            }
        }
    }

    override fun onCleared() {
        readerRefreshJob?.cancel()
        reader.setCompletionKeySwallowEnabled(false)
        reader.setCaptureEnabled(false)
    }

    private fun matchTag(tag: String) {
        val key = scopeKey ?: return
        viewModelScope.launch {
            val match = repository.matchTag(key, tag)
            val row = match.row
            selectedRow.value = row
            scanInput.value = tag
            message.value = when {
                row == null -> "Tag not found in this weighing scope."
                match.outcome == "wrong_shed" ->
                    "Wrong shed scan: expected ${match.expectedLocationLabel}, currently ${match.actualLocationLabel}."
                else -> "Matched ${row.displayAnimalId}."
            }
        }
    }

    private fun WeighingScopeState?.toUiState(
        scan: String,
        weight: String,
        selected: WeighingRosterRowEntity?,
        currentMessage: String?,
        busy: Boolean,
        availableAssignments: List<WeighingAssignment>,
    ): WeighingUiState {
        val scope = this ?: return WeighingUiState(
            scanInput = scan,
            weightInput = weight,
            message = currentMessage,
            assignments = availableAssignments.map { it.toUiRow() },
            category = category,
        )
        return WeighingUiState(
            title = routeTitle.ifBlank { "Weighing" },
            scopeLabel = expectedLocationLabel
                .ifBlank { "Campaign $campaignId - Work group $workGroupId - Scope $campaignShedId" },
            hasScope = true,
            totalExpected = scope.totalExpected,
            selectedAnimalId = selected?.animalId,
            selectedAnimalLabel = selected?.let { "${it.displayAnimalId} in ${it.expectedLocationLabel}" },
            scanInput = scan,
            weightInput = weight,
            message = currentMessage,
            actionInFlight = busy,
            category = category,
            visibleRows = scope.rosterWindow.map { row ->
                WeighingRosterUiRow(
                    id = row.id,
                    displayAnimalId = row.displayAnimalId,
                    expectedLocationLabel = row.expectedLocationLabel,
                    actualLocationLabel = row.actualLocationLabel,
                    status = row.status,
                    availabilityStatus = row.availabilityStatus,
                    wrongShed = !row.actualLocationId.isNullOrBlank() &&
                        row.actualLocationId != row.expectedLocationId,
                )
            },
            individualDrafts = scope.individualDrafts.map { draft ->
                val animalLabel = scope.rosterWindow
                    .firstOrNull { it.animalId == draft.animalId }
                    ?.displayAnimalId
                    ?: "Matched animal"
                WeighingDraftUiRow(
                    id = draft.observationId,
                    label = "$animalLabel - ${draft.weightKg} kg",
                    proofReady = draft.proofReady,
                    readyToSubmit = draft.readyToSubmit,
                )
            },
            shedDrafts = scope.shedDrafts.map { draft ->
                WeighingDraftUiRow(
                    id = draft.shedObservationId,
                    label = "Shed / partition result",
                    proofReady = draft.proofReady,
                    readyToSubmit = draft.readyToSubmit,
                )
            },
        )
    }

    private companion object {
        const val ROSTER_WINDOW_SIZE = 40
        const val ROSTER_SYNC_LIMIT = 5000
        const val READER_REFRESH_MS = 5_000L
        const val INDIVIDUAL_PROOF_FIELD_KEY = "weighing_individual_video"
        const val SHED_PARTITION_PROOF_FIELD_KEY = "weighing_shed_partition_video"
        const val PER_SHED_PARTITION_CATEGORY = "per_shed_partition"
    }
}

private fun WeighingAssignment.toUiRow(): WeighingAssignmentUiRow =
    WeighingAssignmentUiRow(
        campaignId = campaignId,
        tenantId = tenantId,
        workGroupId = workGroupId,
        campaignShedId = campaignShedId,
        expectedLocationId = expectedLocationId,
        expectedLocationLabel = expectedLocationLabel,
        label = label,
        category = category,
        status = status,
        expectedCount = expectedCount,
        periodLabel = periodLabel,
    )

private data class WeighingFormState(
    val scan: String = "",
    val weight: String = "",
    val selected: WeighingRosterRowEntity? = null,
    val message: String? = null,
    val busy: Boolean = false,
)
