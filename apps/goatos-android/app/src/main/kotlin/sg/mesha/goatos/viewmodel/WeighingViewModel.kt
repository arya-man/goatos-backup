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
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.ScanCaptureRepository
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.weighing.IndividualWeighingCapture
import sg.mesha.goatos.core.data.weighing.ShedPartitionWeighingCapture
import sg.mesha.goatos.core.data.weighing.WeighingAssignment
import sg.mesha.goatos.core.data.weighing.WeighingPlanDraft
import sg.mesha.goatos.core.data.weighing.WeighingPlannerCatalog
import sg.mesha.goatos.core.data.weighing.WeighingPlannerShed
import sg.mesha.goatos.core.data.weighing.WeighingRepository
import sg.mesha.goatos.core.data.weighing.WeighingRosterRowEntity
import sg.mesha.goatos.core.data.weighing.WeighingScopeState
import sg.mesha.goatos.core.data.weighing.weighingScopeKey
import sg.mesha.goatos.feature.weighing.WeighingAssignmentUiRow
import sg.mesha.goatos.feature.weighing.WeighingDayTabUiRow
import sg.mesha.goatos.feature.weighing.WeighingDraftUiRow
import sg.mesha.goatos.feature.weighing.WeighingPlannerOperatorUiRow
import sg.mesha.goatos.feature.weighing.WeighingPlannerParkUiRow
import sg.mesha.goatos.feature.weighing.WeighingPlannerShedUiRow
import sg.mesha.goatos.feature.weighing.WeighingProofUiRow
import sg.mesha.goatos.feature.weighing.WeighingRosterUiRow
import sg.mesha.goatos.feature.weighing.WeighingUiState
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.rfid.RfidReaderStatus
import sg.mesha.goatos.feature.scan.ScanReaderConnection
import sg.mesha.goatos.ui.Routes
import java.time.Instant
import java.time.DayOfWeek
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.temporal.WeekFields
import java.util.Locale
import javax.inject.Inject

@HiltViewModel
class WeighingViewModel @Inject constructor(
    private val repository: WeighingRepository,
    private val bootstrapRepository: BootstrapRepository,
    private val reader: RfidReaderPort,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val scanCaptureRepository: ScanCaptureRepository,
    private val proofCaptureSource: ProofCaptureSource,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {
    private val campaignId = savedStateHandle.get<String>(Routes.WEIGHING_CAMPAIGN_ARG).orEmpty()
    private val workGroupId = savedStateHandle.get<String>(Routes.WEIGHING_WORK_GROUP_ARG).orEmpty()
    private val campaignShedId = savedStateHandle.get<String>(Routes.WEIGHING_CAMPAIGN_SHED_ARG).orEmpty()
    private val category = normalizeWeighingCategory(
        savedStateHandle.get<String>(Routes.WEIGHING_CATEGORY_ARG).orEmpty(),
    )
    private val tenantId = savedStateHandle.get<String>(Routes.WEIGHING_TENANT_ARG).orEmpty()
    private val expectedLocationId = savedStateHandle.get<String>(Routes.WEIGHING_EXPECTED_LOCATION_ARG).orEmpty()
    private val expectedLocationLabel = savedStateHandle.get<String>(Routes.WEIGHING_EXPECTED_LOCATION_LABEL_ARG).orEmpty()
    private val routeTitle = savedStateHandle.get<String>(Routes.EXECUTION_SCAN_TITLE_ARG).orEmpty()
    private val scopeKey = listOf(campaignId, workGroupId, campaignShedId)
        .takeIf { parts -> parts.all { it.isNotBlank() } }
        ?.let { weighingScopeKey(campaignId, workGroupId, campaignShedId) }
    private val scanInput = MutableStateFlow("")
    private val weightInput = MutableStateFlow("")
    private val animalCountInput = MutableStateFlow("")
    private val animalWeightInputs = MutableStateFlow<Map<String, String>>(emptyMap())
    private val selectedRow = MutableStateFlow<WeighingRosterRowEntity?>(null)
    private val scannedRows = MutableStateFlow<List<WeighingRosterRowEntity>>(emptyList())
    private val autoProofs = MutableStateFlow<Map<String, ProofCaptureRow>>(emptyMap())
    private val proofReplacementAnimalId = MutableStateFlow<String?>(null)
    private val observedProofs = MutableStateFlow<List<ProofCaptureRow>>(emptyList())
    private var currentPrincipalId: String? = null
    private val assignments = MutableStateFlow<List<WeighingAssignment>>(emptyList())
    private val plannerMode = MutableStateFlow(false)
    private val plannerCatalog = MutableStateFlow<WeighingPlannerCatalog?>(null)
    private val plannerSelections = MutableStateFlow<Map<String, String>>(emptyMap())
    private val message = MutableStateFlow<String?>(null)
    private val actionInFlight = MutableStateFlow(false)
    private val updatingWeightAnimalIds = MutableStateFlow<Set<String>>(emptySet())
    private val loadingAssignments = MutableStateFlow(false)
    private val plannerWeek = WeighingWeek.current()
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

    private val weightState: StateFlow<WeighingWeightState> =
        combine(weightInput, animalCountInput, animalWeightInputs, updatingWeightAnimalIds) {
                weight,
                animalCount,
                animalWeights,
                updatingAnimalIds,
            ->
            WeighingWeightState(weight, animalCount, animalWeights, updatingAnimalIds)
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeighingWeightState())

    private val formState: StateFlow<WeighingFormState> =
        combine(scanInput, weightState, selectedRow, message, actionInFlight) { scan, weights, selected, currentMessage, busy ->
            WeighingFormState(
                scan,
                weights.weight,
                weights.animalCount,
                weights.animalWeights,
                weights.updatingAnimalIds,
                selected,
                currentMessage,
                busy,
            )
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeighingFormState())

    private val rootState: StateFlow<WeighingRootState> =
        combine(assignments, loadingAssignments, plannerMode, plannerCatalog, plannerSelections) { availableAssignments, loading, isPlanner, catalog, selections ->
            WeighingRootState(availableAssignments, loading, isPlanner, catalog, selections)
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeighingRootState())

    private val readerConnectionState: StateFlow<ScanReaderConnection> =
        combine(reader.status, reader.devices) { readerStatus, devices ->
            readerStatus.toScanReaderConnection(devices.firstOrNull()?.name)
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), RfidReaderStatus.NOT_PAIRED.toScanReaderConnection(null))

    private val captureState: StateFlow<WeighingCaptureState> =
        combine(scannedRows, observedProofs, readerConnectionState) { scans, proofs, readerConnection ->
            WeighingCaptureState(scans, proofs, readerConnection)
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeighingCaptureState())

    val state: StateFlow<WeighingUiState> =
        combine(scopeState, formState, rootState, captureState) { scope, form, root, capture ->
            scope.toUiState(
                scan = form.scan,
                weight = form.weight,
                animalCount = form.animalCount,
                animalWeights = form.animalWeights,
                updatingAnimalIds = form.updatingAnimalIds,
                selected = form.selected,
                currentMessage = form.message,
                busy = form.busy,
                availableAssignments = root.assignments,
                loading = root.loading,
                isPlanner = root.plannerMode,
                catalog = root.catalog,
                selections = root.selections,
                localScans = capture.scans,
                proofs = capture.proofs,
                readerConnection = capture.readerConnection,
            )
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeighingUiState())

    init {
        analytics.track(
            AnalyticsEvents.WEIGHING_VIEWED,
            buildMap {
                put(AnalyticsEvents.Params.CATEGORY, category.ifBlank { "root" })
                scopeKey?.let { put(AnalyticsEvents.Params.ITEM_ID, it) }
            },
        )
        if (scopeKey != null) {
            refreshScope()
            viewModelScope.launch {
                val profile = runCatching { bootstrapRepository.operatorProfile() }.getOrNull()
                currentPrincipalId = profile?.operatorId?.takeIf { it.isNotBlank() }
            }
            viewModelScope.launch {
                scanCaptureRepository.observeScannedTags(scopeKey, WEIGHING_SCAN_FIELD_KEY).collect { scans ->
                    scannedRows.value = scans.map { unknownWeighingRow(scopeKey, it.tag, it.capturedAtMs) }
                }
            }
            viewModelScope.launch {
                reader.reads.collect { read -> matchTag(read.tag) }
            }
            viewModelScope.launch {
                proofCaptureRepository.observeProofs(scopeKey).collect { proofs ->
                    observedProofs.value = proofs
                    proofs.forEach { proof ->
                        val serverProofId = proof.serverProofId?.takeIf { it.isNotBlank() } ?: return@forEach
                        when (proof.fieldKey) {
                            INDIVIDUAL_PROOF_FIELD_KEY -> {
                                val animalId = proof.caption?.takeIf { it.isNotBlank() }
                                    ?: proof.subjectId?.takeIf { it.isNotBlank() }
                                    ?: return@forEach
                                repository.attachIndividualProof(scopeKey, animalId, proof.id, serverProofId)
                            }
                            SHED_PARTITION_PROOF_FIELD_KEY -> repository.attachShedPartitionProof(scopeKey, proof.id, serverProofId)
                        }
                    }
                }
            }
        } else {
            viewModelScope.launch {
                val isOperator = runCatching { bootstrapRepository.operatorProfile() != null }.getOrDefault(false)
                plannerMode.value = !isOperator
                if (isOperator) {
                    refreshAssignments()
                } else {
                    refreshPlanner()
                }
            }
        }
    }

    fun refresh() {
        if (scopeKey == null) {
            if (plannerMode.value) {
                refreshPlanner()
            } else {
                refreshAssignments()
            }
        } else {
            refreshScope()
        }
    }

    fun refreshAssignments() {
        if (scopeKey != null) return
        if (loadingAssignments.value) return
        loadingAssignments.value = true
        viewModelScope.launch {
            try {
                when (val loaded = repository.listAssignments()) {
                    is AppResult.Ok -> {
                        assignments.value = loaded.value
                        message.value = null
                    }
                    is AppResult.Err -> reportReadFailure(loaded.message)
                }
            } finally {
                loadingAssignments.value = false
            }
        }
    }

    fun createOrEditDefaultPlan() {
        if (scopeKey != null || actionInFlight.value) return
        val catalog = plannerCatalog.value
        if (catalog == null) {
            message.value = "Planner is still loading."
            refreshPlanner()
            return
        }
        val park = catalog.parks.firstOrNull()
        if (park == null) {
            message.value = "No kid parks are available for this week."
            return
        }
        val operator = catalog.operators.firstOrNull()
        if (operator == null) {
            message.value = "No weighing operator is available to assign."
            return
        }
        val selections = plannerSelections.value
        val selectedSheds = park.sheds
            .filter { selections.containsKey(it.locationId) }
            .map { shed ->
                shed.copy(category = selections[shed.locationId] ?: PER_SHED_PARTITION_CATEGORY)
            }
        if (selectedSheds.isEmpty()) {
            message.value = "Select at least one kid shed."
            return
        }
        actionInFlight.value = true
        viewModelScope.launch {
            try {
                val draft = WeighingPlanDraft(
                    parkId = park.parkId,
                    periodStartDate = plannerWeek.startDate,
                    periodEndDate = plannerWeek.endDate,
                    startBusinessDate = plannerWeek.startDate,
                    plannedCapPerDay = DEFAULT_PLANNED_CAP_PER_DAY,
                    operatorUserId = operator.userId,
                    sheds = selectedSheds,
                )
                val result = park.existingCampaign?.campaignId
                    ?.takeIf { it.isNotBlank() }
                    ?.let { repository.updatePlan(it, draft) }
                    ?: repository.createAndPublishPlan(draft)
                when (result) {
                    is AppResult.Ok -> {
                        message.value = if (park.existingCampaign == null) {
                            "Published ${park.name}: ${selectedSheds.size} shed tasks assigned to ${operator.displayName}."
                        } else {
                            "Updated ${park.name}: same campaign, ${selectedSheds.size} shed tasks assigned to ${operator.displayName}."
                        }
                        refreshPlanner()
                        refreshAssignments()
                    }
                    is AppResult.Err -> message.value = result.message
                }
            } finally {
                actionInFlight.value = false
            }
        }
    }

    fun togglePlannerShed(locationId: String) {
        if (scopeKey != null || locationId.isBlank()) return
        plannerSelections.value = plannerSelections.value.toMutableMap().also { selections ->
            if (selections.containsKey(locationId)) {
                selections.remove(locationId)
            } else {
                selections[locationId] = PER_SHED_PARTITION_CATEGORY
            }
        }
    }

    fun setPlannerShedCategory(locationId: String, category: String) {
        if (scopeKey != null || locationId.isBlank()) return
        if (category != INDIVIDUAL_ANIMAL_CATEGORY && category != PER_SHED_PARTITION_CATEGORY) return
        plannerSelections.value = plannerSelections.value.toMutableMap().also { selections ->
            selections[locationId] = category
        }
    }

    private fun refreshPlanner() {
        if (scopeKey != null) return
        if (loadingAssignments.value) return
        loadingAssignments.value = true
        viewModelScope.launch {
            try {
                when (val loaded = repository.plannerCatalog(plannerWeek.startDate)) {
                    is AppResult.Ok -> {
                        plannerCatalog.value = loaded.value
                        seedPlannerSelections(loaded.value)
                        message.value = null
                    }
                    is AppResult.Err -> reportReadFailure(loaded.message)
                }
            } finally {
                loadingAssignments.value = false
            }
        }
    }

    private fun refreshScope() {
        viewModelScope.launch {
            when (val refreshed = repository.refreshScope(campaignId, workGroupId, campaignShedId, ROSTER_SYNC_MAX_ROWS)) {
                is AppResult.Ok -> {
                    if (refreshed.value == 0 && category != PER_SHED_PARTITION_CATEGORY) {
                        message.value = "No animals are assigned to this weighing scope."
                    }
                }
                is AppResult.Err -> reportReadFailure(refreshed.message)
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

    fun setWeightEntryActive(active: Boolean) {
        val captureEnabled = !active && category != PER_SHED_PARTITION_CATEGORY
        reader.setCompletionKeySwallowEnabled(captureEnabled)
        reader.setCaptureEnabled(captureEnabled)
    }

    fun onScanInputChange(value: String) {
        scanInput.value = value
    }

    fun onWeightInputChange(value: String) {
        weightInput.value = sanitizeWeighingWeightInput(value)
    }

    fun onAnimalCountInputChange(value: String) {
        animalCountInput.value = sanitizeWeighingAnimalCountInput(value)
    }

    fun onAnimalWeightInputChange(animalId: String, value: String) {
        if (animalId.isBlank()) return
        val filtered = sanitizeWeighingWeightInput(value)
        animalWeightInputs.value = animalWeightInputs.value + (animalId to filtered)
    }

    fun selectAnimal(animalId: String) {
        if (scopeKey == null || animalId.isBlank()) return
        val row = scopeState.value
            ?.rosterWindow
            ?.firstOrNull { it.animalId == animalId || it.id == animalId }
            ?: return
        selectedRow.value = row
        scanInput.value = row.primaryTag
        message.value = "Selected ${row.displayAnimalId}. Enter weight, then capture video."
    }

    fun submitTypedScan() {
        val tag = scanInput.value
        if (tag.isNotBlank()) matchTag(tag)
    }

    fun recordIndividual() {
        val key = scopeKey ?: return
        val row = selectedRow.value ?: return
        recordIndividualRow(key, row)
    }

    fun recordIndividual(animalId: String, rawWeight: String) {
        val key = scopeKey ?: return
        val sanitizedWeight = sanitizeWeighingWeightInput(rawWeight)
        if (parsePositiveWeighingWeight(sanitizedWeight) == null) return
        if (animalId in updatingWeightAnimalIds.value) return
        val row = scannedRows.value.firstOrNull { it.animalId == animalId }
            ?: scopeState.value?.rosterWindow?.firstOrNull { it.animalId == animalId }
            ?: unknownWeighingRow(
                key = key,
                tag = animalId,
                capturedAtMs = System.currentTimeMillis(),
            )
        animalWeightInputs.value = animalWeightInputs.value + (animalId to sanitizedWeight)
        selectedRow.value = row
        recordIndividualRow(key, row, useGlobalBusyGate = false)
    }

    fun submitIndividualScope(onSubmitted: () -> Unit) {
        if (category == PER_SHED_PARTITION_CATEGORY || actionInFlight.value) return
        val drafts = scopeState.value?.individualDrafts.orEmpty()
        val capturedAnimalIds = (
            scannedRows.value.map { it.animalId } + drafts.map { it.animalId }
        ).distinct()
        val everyCaptureComplete = capturedAnimalIds.isNotEmpty() &&
            capturedAnimalIds.all { animalId ->
                val draft = drafts.firstOrNull { it.animalId == animalId }
                draft?.syncedToBackend == true &&
                    (
                        proofForAnimal(animalId)?.let {
                        it.syncStatus == CaptureSyncStatus.SYNCED &&
                            !it.serverProofId.isNullOrBlank()
                        } == true ||
                            !draft.serverProofId.isNullOrBlank()
                    )
            }
        if (!everyCaptureComplete) {
            message.value = "Wait for every weight and video to sync before submitting."
            return
        }
        actionInFlight.value = true
        viewModelScope.launch {
            try {
                when (
                    repository.submitIndividualScope(
                        campaignId,
                        campaignShedId,
                        capturedAnimalIds,
                    )
                ) {
                    is AppResult.Ok -> onSubmitted()
                    is AppResult.Err -> message.value = "Couldn't submit this shed. Try again."
                }
            } finally {
                actionInFlight.value = false
            }
        }
    }

    private fun recordIndividualRow(
        key: String,
        row: WeighingRosterRowEntity,
        useGlobalBusyGate: Boolean = true,
    ) {
        val weightKg = parsePositiveWeighingWeight(animalWeightInputs.value[row.animalId])
            ?: parsePositiveWeighingWeight(weightInput.value)
            ?: return
        if (useGlobalBusyGate && actionInFlight.value) return
        if (row.animalId in updatingWeightAnimalIds.value) return
        analytics.track(AnalyticsEvents.WEIGHING_CAPTURE_ATTEMPT, weighingCaptureProps(INDIVIDUAL_ANIMAL_CATEGORY))
        if (useGlobalBusyGate) actionInFlight.value = true
        updatingWeightAnimalIds.value = updatingWeightAnimalIds.value + row.animalId
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
                        val proof = proofForAnimal(row.animalId)
                        if (proof != null) {
                            repository.attachIndividualProof(key, row.animalId, proof.id, proof.serverProofId)
                        } else {
                            val restoredProofCaptureId = recorded.value.proofCaptureId
                            val restoredServerProofId = recorded.value.serverProofId
                            if (!restoredProofCaptureId.isNullOrBlank() && !restoredServerProofId.isNullOrBlank()) {
                            repository.attachIndividualProof(
                                key,
                                row.animalId,
                                restoredProofCaptureId,
                                restoredServerProofId,
                            )
                            }
                        }
                            message.value = if (proof == null) {
                                "Weight saved. Video is still required."
                            } else {
                                "Weight saved. Video continues syncing in the background."
                            }
                            analytics.track(
                                AnalyticsEvents.WEIGHING_CAPTURE_SUCCESS,
                                weighingCaptureProps(INDIVIDUAL_ANIMAL_CATEGORY),
                            )
                            weightInput.value = ""
                            animalWeightInputs.value = animalWeightInputs.value - row.animalId
                            scanInput.value = ""
                        recorded.value
                    }
                    is AppResult.Err -> {
                        updatingWeightAnimalIds.value = updatingWeightAnimalIds.value - row.animalId
                        message.value = recorded.message
                        reportCaptureFailure(INDIVIDUAL_ANIMAL_CATEGORY, recorded.message)
                    }
                }
            } finally {
                updatingWeightAnimalIds.value = updatingWeightAnimalIds.value - row.animalId
                if (useGlobalBusyGate) actionInFlight.value = false
            }
        }
    }

    fun recordShedPartition(onSubmitted: () -> Unit = {}) {
        val key = scopeKey ?: return
        val weightKg = parsePositiveWeighingWeight(weightInput.value) ?: return
        val animalCount = parsePositiveWeighingAnimalCount(animalCountInput.value) ?: return
        val averageWeightKg = weightKg / animalCount
        val syncedProof = observedProofs.value
            .filter { it.fieldKey == SHED_PARTITION_PROOF_FIELD_KEY }
            .filter { it.subjectId == expectedLocationId }
            .filter { it.syncStatus == CaptureSyncStatus.SYNCED && !it.serverProofId.isNullOrBlank() }
            .minByOrNull { it.capturedAtMs }
        if (syncedProof == null) {
            message.value = "Capture and sync at least one group video before submitting."
            return
        }
        if (actionInFlight.value) return
        if (category != PER_SHED_PARTITION_CATEGORY) {
            message.value = "This weighing scope expects animal RFID scans."
            return
        }
        if (tenantId.isBlank() || expectedLocationId.isBlank()) {
            message.value = "Shed / partition assignment is missing location details."
            return
        }
        analytics.track(AnalyticsEvents.WEIGHING_CAPTURE_ATTEMPT, weighingCaptureProps(PER_SHED_PARTITION_CATEGORY))
        actionInFlight.value = true
        viewModelScope.launch {
            try {
                val resultJson = buildJsonObject {
                    put("total_weight_kg", weightKg)
                    put("animal_count", animalCount)
                    put("average_weight_kg", averageWeightKg)
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
                        repository.attachShedPartitionProof(key, syncedProof.id, syncedProof.serverProofId)
                        message.value = "Lump-sum weighing submitted."
                        analytics.track(
                            AnalyticsEvents.WEIGHING_CAPTURE_SUCCESS,
                            weighingCaptureProps(PER_SHED_PARTITION_CATEGORY),
                        )
                        onSubmitted()
                        recorded.value
                    }
                    is AppResult.Err -> {
                        message.value = recorded.message
                        reportCaptureFailure(PER_SHED_PARTITION_CATEGORY, recorded.message)
                    }
                }
            } finally {
                actionInFlight.value = false
            }
        }
    }

    fun captureShedVideo() {
        val key = scopeKey ?: return
        if (category != PER_SHED_PARTITION_CATEGORY || actionInFlight.value) return
        val existing = observedProofs.value.count { it.fieldKey == SHED_PARTITION_PROOF_FIELD_KEY }
        if (existing >= MAX_SHED_GROUP_VIDEOS) {
            message.value = "Maximum 5 group videos reached."
            return
        }
        actionInFlight.value = true
        viewModelScope.launch {
            try {
                val captured = proofCaptureSource.captureVideo(
                    ProofCaptureContext(
                        title = "Lump-sum weighing proof",
                        primaryTag = expectedLocationLabel.ifBlank { routeTitle },
                        secondaryTag = null,
                        workLabel = "Group video ${existing + 1} of 5",
                    ),
                ) ?: return@launch
                val principalId = currentPrincipalId
                    ?: runCatching { bootstrapRepository.operatorProfile()?.operatorId }.getOrNull()
                        ?.takeIf { it.isNotBlank() }
                        ?.also { currentPrincipalId = it }
                    ?: return@launch
                when (
                    val proof = proofCaptureRepository.capture(
                        taskId = key,
                        fieldKey = SHED_PARTITION_PROOF_FIELD_KEY,
                        subject = ProofSubject.SHED,
                        subjectId = expectedLocationId,
                        localUri = captured.localUri,
                        mimeType = captured.mimeType,
                        caption = "Lump-sum group video ${existing + 1}",
                        scopeType = "shed",
                        scopeId = expectedLocationId,
                        capturedStartMs = captured.startedAtMs,
                        capturedEndMs = captured.endedAtMs,
                        capturedByPrincipalId = principalId,
                        proofPolicy = ProofPolicy.Default.copy(
                            proofMode = "shed_level_video",
                            subjectScope = "shed",
                            expectedSubjects = listOf("shed"),
                            minimumCount = 1,
                            maximumCount = MAX_SHED_GROUP_VIDEOS,
                            maximumCountPerSubject = MAX_SHED_GROUP_VIDEOS,
                        ),
                    )
                ) {
                    is AppResult.Ok -> message.value = "Group video saved locally."
                    is AppResult.Err -> message.value = proof.message
                }
            } finally {
                actionInFlight.value = false
            }
        }
    }

    private fun reportReadFailure(reason: String) {
        message.value = reason.toWeighingReadMessage()
        crashReporter.recordException(IllegalStateException(reason), "weighing refresh failed")
        analytics.track(
            AnalyticsEvents.WEIGHING_READ_FAILURE,
            buildMap {
                put(AnalyticsEvents.Params.REASON, reason.take(MAX_ANALYTICS_REASON_CHARS))
                put(AnalyticsEvents.Params.CATEGORY, category.ifBlank { "root" })
            },
        )
    }

    private fun reportCaptureFailure(category: String, reason: String) {
        crashReporter.recordException(IllegalStateException(reason), "weighing capture failed")
        analytics.track(
            AnalyticsEvents.WEIGHING_CAPTURE_FAILURE,
            weighingCaptureProps(category) + (AnalyticsEvents.Params.REASON to reason.take(MAX_ANALYTICS_REASON_CHARS)),
        )
    }

    private fun weighingCaptureProps(captureCategory: String): Map<String, String> =
        buildMap {
            put(AnalyticsEvents.Params.CATEGORY, captureCategory)
            put(AnalyticsEvents.Params.ITEM_ID, scopeKey.orEmpty())
            put(AnalyticsEvents.Params.SHED_ID, campaignShedId)
        }

    override fun onCleared() {
        readerRefreshJob?.cancel()
        reader.setCompletionKeySwallowEnabled(false)
        reader.setCaptureEnabled(false)
    }

    private fun seedPlannerSelections(catalog: WeighingPlannerCatalog) {
        val firstPark = catalog.parks.firstOrNull() ?: return
        val validIds = firstPark.sheds.map { it.locationId }.toSet()
        val preserved = plannerSelections.value.filterKeys { it in validIds }
        if (preserved.isNotEmpty()) {
            plannerSelections.value = preserved
            return
        }
        plannerSelections.value = firstPark.sheds
            .filter { it.kidCount > 0 }
            .take(3)
            .mapIndexed { index, shed ->
                shed.locationId to if (index == 0) INDIVIDUAL_ANIMAL_CATEGORY else PER_SHED_PARTITION_CATEGORY
            }
            .toMap()
    }

    private fun matchTag(tag: String) {
        val key = scopeKey ?: return
        val normalizedTag = normalizeFreeFlowTag(tag)
        if (normalizedTag.isBlank() || actionInFlight.value) return
        viewModelScope.launch {
            val existingRow = scannedRows.value.firstOrNull {
                normalizeFreeFlowTag(it.animalId) == normalizedTag ||
                    normalizeFreeFlowTag(it.displayAnimalId) == normalizedTag
            }
            if (existingRow != null && proofReplacementAnimalId.value == existingRow.animalId) {
                proofReplacementAnimalId.value = null
                message.value = null
                captureVideoForRow(key, existingRow)
                return@launch
            }
            if (existingRow != null && proofForAnimal(existingRow.animalId) == null) {
                message.value = null
                captureVideoForRow(key, existingRow)
                return@launch
            }
            val alreadyRecorded = scopeState.value?.individualDrafts.orEmpty().any { draft ->
                normalizeFreeFlowTag(draft.scannedIdentifier.ifBlank { draft.animalId }) == normalizedTag
            }
            if (alreadyRecorded || existingRow != null) {
                scanInput.value = normalizedTag
                message.value = "Already scanned · $normalizedTag"
                return@launch
            }
            val inserted = scanCaptureRepository.recordLocalScanIfAbsent(
                taskId = key,
                fieldKey = WEIGHING_SCAN_FIELD_KEY,
                tag = normalizedTag,
            )
            scanInput.value = normalizedTag
            if (!inserted) {
                message.value = "Already scanned · $normalizedTag"
                return@launch
            }
            val row = unknownWeighingRow(key, normalizedTag)
            selectedRow.value = row
            message.value = "RFID captured. Record video, then enter weight."
            captureVideoForRow(key, row)
        }
    }

    fun retryVideo(animalId: String) {
        val key = scopeKey ?: return
        val proof = proofForAnimal(animalId)
        if (proof?.syncStatus != CaptureSyncStatus.FAILED) return
        viewModelScope.launch {
            when (val result = proofCaptureRepository.retryUpload(key, proof.id)) {
                is AppResult.Ok -> message.value = "Video retry queued."
                is AppResult.Err -> message.value = result.message
            }
        }
    }

    fun reuploadVideo(animalId: String) {
        val row = scannedRows.value.firstOrNull { it.animalId == animalId } ?: return
        proofReplacementAnimalId.value = animalId
        message.value = "Re-upload armed · tap RFID ${row.displayAnimalId} again"
    }

    private fun captureVideoForRow(key: String, row: WeighingRosterRowEntity) {
        if (actionInFlight.value) return
        actionInFlight.value = true
        viewModelScope.launch {
            try {
                when (val proof = captureProofForRow(key, row)) {
                    is AppResult.Ok -> {
                        autoProofs.value = autoProofs.value + (row.animalId to proof.value)
                        message.value = "Video saved for ${row.displayAnimalId}. Enter weight."
                    }
                    is AppResult.Err -> {
                        message.value = "RFID captured. Video proof is still required."
                        reportCaptureFailure(INDIVIDUAL_ANIMAL_CATEGORY, proof.message)
                    }
                }
            } finally {
                actionInFlight.value = false
            }
        }
    }

    private suspend fun captureProofForRow(key: String, row: WeighingRosterRowEntity): AppResult<ProofCaptureRow> {
        val captured = proofCaptureSource.captureVideo(
            ProofCaptureContext(
                title = "Weighing proof",
                primaryTag = row.displayAnimalId,
                secondaryTag = null,
                workLabel = "Weight needed",
            ),
        )
        if (captured == null) {
            return AppResult.Err("missing_video")
        }
        val principalId = currentPrincipalId
            ?: runCatching { bootstrapRepository.operatorProfile()?.operatorId }.getOrNull()
                ?.takeIf { it.isNotBlank() }
                ?.also { currentPrincipalId = it }
            ?: return AppResult.Err("missing_operator")
        return proofCaptureRepository.capture(
            taskId = key,
            fieldKey = INDIVIDUAL_PROOF_FIELD_KEY,
            subject = ProofSubject.OTHER,
            subjectId = null,
            localUri = captured.localUri,
            mimeType = captured.mimeType,
            caption = row.animalId,
            scopeType = "shed",
            scopeId = row.expectedLocationId,
            capturedStartMs = captured.startedAtMs,
            capturedEndMs = captured.endedAtMs,
            capturedByPrincipalId = principalId,
            proofPolicy = ProofPolicy.Default.copy(
                proofMode = "free_flow_video",
                subjectScope = "other",
                expectedSubjects = listOf("other"),
                maximumCount = ROSTER_SYNC_MAX_ROWS,
                maximumCountPerSubject = ROSTER_SYNC_MAX_ROWS,
            ),
        )
    }

    private fun unknownWeighingRow(key: String, tag: String, capturedAtMs: Long = System.currentTimeMillis()): WeighingRosterRowEntity {
        val normalizedTag = tag.trim()
        val label = normalizedTag.ifBlank { "Unidentified animal" }
        return WeighingRosterRowEntity(
            id = "$key:$label",
            scopeKey = key,
            tenantId = tenantId,
            campaignId = campaignId,
            workGroupId = workGroupId,
            campaignShedId = campaignShedId,
            expectedLocationId = expectedLocationId.ifBlank { campaignShedId },
            expectedLocationLabel = expectedLocationLabel.ifBlank { routeTitle.ifBlank { "Assigned shed" } },
            actualLocationId = expectedLocationId.takeIf { it.isNotBlank() },
            actualLocationLabel = expectedLocationLabel.takeIf { it.isNotBlank() },
            animalId = label,
            displayAnimalId = label,
            primaryTag = label,
            secondaryTag = null,
            normalizedPrimaryTag = normalizedTag.lowercase(),
            normalizedSecondaryTag = null,
            status = "pending",
            availabilityStatus = null,
            seq = Long.MAX_VALUE,
            updatedAt = capturedAtMs,
        )
    }

    private fun WeighingScopeState?.toUiState(
        scan: String,
        weight: String,
        animalCount: String,
        animalWeights: Map<String, String>,
        updatingAnimalIds: Set<String>,
        selected: WeighingRosterRowEntity?,
        currentMessage: String?,
        busy: Boolean,
        availableAssignments: List<WeighingAssignment>,
        loading: Boolean,
        isPlanner: Boolean,
        catalog: WeighingPlannerCatalog?,
        selections: Map<String, String>,
        localScans: List<WeighingRosterRowEntity>,
        proofs: List<ProofCaptureRow>,
        readerConnection: ScanReaderConnection?,
    ): WeighingUiState {
        if (this == null && scopeKey != null) {
            return WeighingUiState(
                title = routeTitle.ifBlank { "Weighing" },
                scopeLabel = expectedLocationLabel
                    .ifBlank { routeTitle }
                    .ifBlank { if (category == PER_SHED_PARTITION_CATEGORY) "Shed / partition weighing" else "Animal weighing" },
                hasScope = true,
                scanInput = scan,
                weightInput = weight,
                animalCountInput = animalCount,
                selectedAnimalId = selected?.animalId,
                selectedAnimalLabel = selected?.let { "${it.displayAnimalId} in ${it.expectedLocationLabel}" },
                message = currentMessage,
                actionInFlight = busy,
                loading = true,
                category = category,
                visibleRows = localScans.toUiRows(proofs = proofs),
                shedProofs = proofs.toShedProofUiRows(),
                readerConnection = readerConnection,
            )
        }
        val scope = this ?: return WeighingUiState(
            scanInput = scan,
            weightInput = weight,
            animalCountInput = animalCount,
            message = currentMessage,
            assignments = availableAssignments.map { it.toUiRow() },
            loading = loading,
            category = category,
            plannerMode = isPlanner,
            plannerWeekLabel = plannerWeek.label,
            plannerPeriodLabel = plannerWeek.periodLabel,
            plannerDayTabs = plannerWeek.dayTabs,
            readerConnection = readerConnection,
            shedProofs = proofs.toShedProofUiRows(),
            plannerParks = catalog?.parks.orEmpty().map { park ->
                WeighingPlannerParkUiRow(
                    parkId = park.parkId,
                    name = park.name,
                    kidCount = park.kidCount,
                    existingCampaignId = park.existingCampaign?.campaignId,
                    existingCampaignStatus = park.existingCampaign?.status?.readableWeighingStatus(),
                    existingCampaignShedCount = park.existingCampaign?.shedCount ?: 0,
                    sheds = park.sheds.map { shed ->
                        WeighingPlannerShedUiRow(
                            locationId = shed.locationId,
                            name = shed.name,
                            kidCount = shed.kidCount,
                            category = selections[shed.locationId] ?: shed.category,
                            selected = selections.containsKey(shed.locationId),
                        )
                    },
                )
            },
            plannerOperators = catalog?.operators.orEmpty().map { operator ->
                WeighingPlannerOperatorUiRow(
                    userId = operator.userId,
                    displayName = operator.displayName,
                    displayCode = operator.displayCode,
                )
            },
        )
        val effectiveScans = (
            localScans + scope.individualDrafts.map { draft ->
                unknownWeighingRow(
                    key = scopeKey.orEmpty(),
                    tag = draft.scannedIdentifier.ifBlank { draft.animalId },
                    capturedAtMs = draft.capturedAtMs,
                )
            }
        )
            .distinctBy { it.animalId }
            .sortedWith(
                compareByDescending<WeighingRosterRowEntity> { it.updatedAt }
                    .thenByDescending { it.animalId },
            )
        return WeighingUiState(
            title = routeTitle.ifBlank { "Weighing" },
            scopeLabel = expectedLocationLabel
                .ifBlank { routeTitle }
                .ifBlank { if (category == PER_SHED_PARTITION_CATEGORY) "Shed / partition weighing" else "Animal weighing" },
            hasScope = true,
            totalExpected = scope.totalExpected,
            selectedAnimalId = selected?.animalId,
            selectedAnimalLabel = selected?.let { "${it.displayAnimalId} in ${it.expectedLocationLabel}" },
            scanInput = scan,
            weightInput = weight,
            animalCountInput = animalCount,
            message = currentMessage,
            actionInFlight = busy,
            category = category,
            readerConnection = readerConnection,
            shedProofs = proofs.toShedProofUiRows(),
            visibleRows = effectiveScans.toUiRows(
                animalWeights = animalWeights,
                updatingAnimalIds = updatingAnimalIds,
                busy = busy,
                drafts = scope.individualDrafts,
                proofs = proofs,
            ),
            individualDrafts = scope.individualDrafts.map { draft ->
                val animalLabel = scope.rosterWindow
                    .firstOrNull { it.animalId == draft.animalId }
                    ?.displayAnimalId
                    ?: draft.animalId
                WeighingDraftUiRow(
                    id = draft.observationId,
                    animalId = draft.animalId,
                    label = "$animalLabel - ${draft.weightKg} kg",
                    proofReady = draft.proofReady,
                    readyToSubmit = draft.readyToSubmit,
                    syncedToBackend = draft.syncedToBackend,
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

    private fun List<WeighingRosterRowEntity>.toUiRows(
        animalWeights: Map<String, String> = emptyMap(),
        updatingAnimalIds: Set<String> = emptySet(),
        busy: Boolean = false,
        drafts: List<sg.mesha.goatos.core.data.weighing.IndividualWeighingDraft> = emptyList(),
        proofs: List<ProofCaptureRow> = emptyList(),
    ): List<WeighingRosterUiRow> =
        map { row ->
            val draft = drafts.firstOrNull { it.animalId == row.animalId }
            val savedWeight = draft?.weightKg?.toString()
            val weight = animalWeights[row.animalId] ?: savedWeight.orEmpty()
            val proof = proofs
                .filter { it.fieldKey == INDIVIDUAL_PROOF_FIELD_KEY }
                .filter { it.caption == row.animalId || it.id == draft?.proofCaptureId }
                .maxByOrNull { it.capturedAtMs }
                ?: autoProofs.value[row.animalId]
            val proofStatus = when {
                proof == null && !draft?.proofCaptureId.isNullOrBlank() && draft?.syncedToBackend == true ->
                    sg.mesha.goatos.feature.scan.ProofUploadStatus.SYNCED
                else -> when (proof?.syncStatus) {
                CaptureSyncStatus.SYNCED -> sg.mesha.goatos.feature.scan.ProofUploadStatus.SYNCED
                CaptureSyncStatus.FAILED -> sg.mesha.goatos.feature.scan.ProofUploadStatus.FAILED
                CaptureSyncStatus.PENDING, CaptureSyncStatus.IN_FLIGHT -> sg.mesha.goatos.feature.scan.ProofUploadStatus.UPLOADING
                null -> sg.mesha.goatos.feature.scan.ProofUploadStatus.MISSING
                }
            }
            val weightSaved = draft != null
            WeighingRosterUiRow(
                id = row.id,
                animalId = row.animalId,
                displayAnimalId = row.displayAnimalId,
                expectedLocationLabel = row.expectedLocationLabel,
                actualLocationLabel = null,
                status = if (draft?.syncedToBackend == true) "Completed" else "Scanned",
                availabilityStatus = null,
                wrongShed = false,
                scannedAtLabel = scanTimeLabel(row.updatedAt),
                weightInput = weight,
                savedWeightLabel = savedWeight,
                canSaveWeight = row.animalId !in updatingAnimalIds &&
                    weight.toDoubleOrNull()?.let { it > 0.0 } == true,
                weightSaved = weightSaved,
                proofCaptureId = proof?.id ?: draft?.proofCaptureId,
                proofUploadStatus = proofStatus,
                proofStatusLabel = proof?.proofStatusLabel()
                    ?: if (draft?.syncedToBackend == true) "Video synced ${timeOnlyLabel(draft.capturedAtMs)}" else null,
                backendSynced = draft?.syncedToBackend == true,
                weightUpdating = row.animalId in updatingAnimalIds,
            )
        }

    private fun scanTimeLabel(epochMs: Long): String =
        runCatching {
            "Scanned " + WEIGHING_SCAN_TIME_FORMATTER.format(Instant.ofEpochMilli(epochMs))
        }.getOrDefault("just now")

    private fun List<ProofCaptureRow>.toShedProofUiRows(): List<WeighingProofUiRow> =
        filter {
            it.fieldKey == SHED_PARTITION_PROOF_FIELD_KEY &&
                it.subjectId == expectedLocationId
        }
            .sortedBy { it.capturedAtMs }
            .mapIndexed { index, proof ->
                WeighingProofUiRow(
                    id = proof.id,
                    label = when (proof.syncStatus) {
                        CaptureSyncStatus.PENDING, CaptureSyncStatus.IN_FLIGHT -> "uploading"
                        CaptureSyncStatus.SYNCED -> "synced"
                        CaptureSyncStatus.FAILED -> "upload failed · retry"
                    },
                    status = when (proof.syncStatus) {
                        CaptureSyncStatus.PENDING, CaptureSyncStatus.IN_FLIGHT ->
                            sg.mesha.goatos.feature.scan.ProofUploadStatus.UPLOADING
                        CaptureSyncStatus.SYNCED -> sg.mesha.goatos.feature.scan.ProofUploadStatus.SYNCED
                        CaptureSyncStatus.FAILED -> sg.mesha.goatos.feature.scan.ProofUploadStatus.FAILED
                    },
                )
            }

    private fun ProofCaptureRow.proofStatusLabel(): String = when (syncStatus) {
        CaptureSyncStatus.PENDING, CaptureSyncStatus.IN_FLIGHT ->
            "Video uploading ${timeOnlyLabel(capturedAtMs)}"
        CaptureSyncStatus.SYNCED -> "Video synced ${timeOnlyLabel(capturedAtMs)}"
        CaptureSyncStatus.FAILED -> "Upload failed ${timeOnlyLabel(capturedAtMs)}"
    }

    private fun timeOnlyLabel(epochMs: Long): String =
        WEIGHING_TIME_ONLY_FORMATTER.format(Instant.ofEpochMilli(epochMs))

    private fun proofForAnimal(animalId: String): ProofCaptureRow? {
        val draftProofId = scopeState.value?.individualDrafts
            ?.firstOrNull { it.animalId == animalId }
            ?.proofCaptureId
        return observedProofs.value
            .filter { it.fieldKey == INDIVIDUAL_PROOF_FIELD_KEY }
            .filter { it.caption == animalId || it.id == draftProofId }
            .maxByOrNull { it.capturedAtMs }
            ?: autoProofs.value[animalId]
    }

    private fun RfidReaderStatus.toScanReaderConnection(readerName: String?): ScanReaderConnection =
        ScanReaderConnection(
            readerName = readerName ?: "RFID reader",
            statusLabel = when (this) {
                RfidReaderStatus.READY -> "Reader connected"
                RfidReaderStatus.PAIRED_NOT_READY -> "Reader disconnected"
                RfidReaderStatus.NOT_PAIRED -> "Reader not paired"
                RfidReaderStatus.PERMISSION_NEEDED -> "Bluetooth permission needed"
                RfidReaderStatus.BLUETOOTH_OFF -> "Bluetooth off"
            },
            connected = this == RfidReaderStatus.READY,
            actionLabel = "Reconnect",
        )

    private companion object {
        const val ROSTER_WINDOW_SIZE = 40
        const val ROSTER_SYNC_MAX_ROWS = 10_000
        const val READER_REFRESH_MS = 5_000L
        const val INDIVIDUAL_PROOF_FIELD_KEY = "weighing_individual_video"
        const val WEIGHING_SCAN_FIELD_KEY = "weighing_free_flow_scan"
        const val SHED_PARTITION_PROOF_FIELD_KEY = "weighing_shed_partition_video"
        const val INDIVIDUAL_ANIMAL_CATEGORY = "individual_animal"
        const val PER_SHED_PARTITION_CATEGORY = "per_shed_partition"
        const val MAX_ANALYTICS_REASON_CHARS = 96
        const val MAX_SHED_GROUP_VIDEOS = 5
        const val DEFAULT_PLANNED_CAP_PER_DAY = 100
        val WEIGHING_SCAN_TIME_FORMATTER: DateTimeFormatter =
            DateTimeFormatter.ofPattern("d MMM, h:mm a 'IST'", Locale.ENGLISH)
                .withZone(ZoneId.of("Asia/Kolkata"))
        val WEIGHING_TIME_ONLY_FORMATTER: DateTimeFormatter =
            DateTimeFormatter.ofPattern("h:mm a 'IST'", Locale.ENGLISH)
                .withZone(ZoneId.of("Asia/Kolkata"))
    }
}

private fun normalizeWeighingCategory(raw: String): String =
    when (raw.trim().lowercase(Locale.ROOT)) {
        "per shed partition", "per-shed-partition", "per_shed_partition" -> "per_shed_partition"
        "individual animal", "individual-animal", "individual_animal" -> "individual_animal"
        else -> raw.trim()
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
        status = status.readableWeighingStatus(),
        expectedCount = expectedCount,
        periodLabel = periodLabel.readableWeighingPeriodLabel(),
    )

private fun String.readableWeighingPeriodLabel(): String {
    val parts = split(" - ")
    if (parts.size != 2) return this
    return runCatching {
        val start = LocalDate.parse(parts[0], DateTimeFormatter.ISO_LOCAL_DATE)
        val end = LocalDate.parse(parts[1], DateTimeFormatter.ISO_LOCAL_DATE)
        val week = start.get(WeekFields.ISO.weekOfWeekBasedYear())
        val shortFormatter = DateTimeFormatter.ofPattern("d MMM", Locale.ENGLISH)
        "Week $week - ${start.format(shortFormatter)}-${end.format(shortFormatter)}"
    }.getOrDefault(this)
}

private fun String.readableWeighingStatus(): String = when (trim().lowercase()) {
    "draft" -> "Draft"
    "published" -> "Published"
    "scheduled" -> "Scheduled"
    "pending" -> "Pending"
    "in_progress" -> "In progress"
    "needs_review" -> "Needs review"
    "accepted" -> "Accepted"
    "completed" -> "Completed"
    "cancelled", "canceled" -> "Cancelled"
    else -> replace('_', ' ')
        .split(' ')
        .filter { it.isNotBlank() }
        .joinToString(" ") { part -> part.replaceFirstChar { char -> char.uppercase() } }
        .ifBlank { "Not started" }
}

private fun String.toWeighingReadMessage(): String {
    val normalized = lowercase()
    return when {
        normalized.contains("failed to connect") ||
            normalized.contains("unable to resolve host") ||
        normalized.contains("timeout") ||
        normalized.contains("timed out") ||
            normalized.contains("unexpected end of stream") ||
            normalized.contains("unexpected eof") ||
            normalized.contains("connection reset") ||
            normalized.contains("connection refused") ->
            "Couldn't load weighing. Check the laptop backend or network, then refresh."
        isBlank() -> "Couldn't load weighing. Pull to refresh or try again."
        else -> this
    }
}

private data class WeighingFormState(
    val scan: String = "",
    val weight: String = "",
    val animalCount: String = "",
    val animalWeights: Map<String, String> = emptyMap(),
    val updatingAnimalIds: Set<String> = emptySet(),
    val selected: WeighingRosterRowEntity? = null,
    val message: String? = null,
    val busy: Boolean = false,
)

private data class WeighingWeightState(
    val weight: String = "",
    val animalCount: String = "",
    val animalWeights: Map<String, String> = emptyMap(),
    val updatingAnimalIds: Set<String> = emptySet(),
)

private data class WeighingRootState(
    val assignments: List<WeighingAssignment> = emptyList(),
    val loading: Boolean = false,
    val plannerMode: Boolean = false,
    val catalog: WeighingPlannerCatalog? = null,
    val selections: Map<String, String> = emptyMap(),
)

private data class WeighingCaptureState(
    val scans: List<WeighingRosterRowEntity> = emptyList(),
    val proofs: List<ProofCaptureRow> = emptyList(),
    val readerConnection: ScanReaderConnection? = null,
)

internal fun sanitizeWeighingWeightInput(value: String): String =
    value.take(8).takeIf { candidate ->
        candidate.all { it.isDigit() || it == '.' } && candidate.count { it == '.' } <= 1
    }.orEmpty()

internal fun sanitizeWeighingAnimalCountInput(value: String): String =
    value.take(6).takeIf { candidate -> candidate.all(Char::isDigit) }.orEmpty()

internal fun parsePositiveWeighingWeight(value: String?): Double? =
    value?.toDoubleOrNull()?.takeIf { it.isFinite() && it > 0.0 }

internal fun parsePositiveWeighingAnimalCount(value: String?): Int? =
    value?.toIntOrNull()?.takeIf { it > 0 }

private fun normalizeFreeFlowTag(tag: String): String =
    tag.filter { it.isLetterOrDigit() }.lowercase()

private data class WeighingWeek(
    val startDate: String,
    val endDate: String,
    val label: String,
    val periodLabel: String,
    val dayTabs: List<WeighingDayTabUiRow>,
) {
    companion object {
        private val isoFormatter = DateTimeFormatter.ISO_LOCAL_DATE
        private val shortFormatter = DateTimeFormatter.ofPattern("d MMM", Locale.ENGLISH)

        fun current(today: LocalDate = LocalDate.now(ZoneId.of("Asia/Kolkata"))): WeighingWeek {
            val start = today.with(DayOfWeek.MONDAY)
            val end = start.plusDays(6)
            val week = start.get(WeekFields.ISO.weekOfWeekBasedYear())
            return WeighingWeek(
                startDate = start.format(isoFormatter),
                endDate = end.format(isoFormatter),
                label = "Week $week",
                periodLabel = "Week $week - ${start.format(shortFormatter)}-${end.format(shortFormatter)}",
                dayTabs = (0L..6L).map { offset ->
                    val date = start.plusDays(offset)
                    WeighingDayTabUiRow(
                        dayLabel = date.dayOfWeek.name.take(3),
                        dateLabel = date.dayOfMonth.toString(),
                        selected = date == today,
                    )
                },
            )
        }
    }
}
