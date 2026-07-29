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
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofSubject
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
import sg.mesha.goatos.feature.weighing.WeighingRosterUiRow
import sg.mesha.goatos.feature.weighing.WeighingUiState
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.ui.Routes
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
    private val proofCaptureSource: ProofCaptureSource,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
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
    private val plannerMode = MutableStateFlow(false)
    private val plannerCatalog = MutableStateFlow<WeighingPlannerCatalog?>(null)
    private val plannerSelections = MutableStateFlow<Map<String, String>>(emptyMap())
    private val message = MutableStateFlow<String?>(null)
    private val actionInFlight = MutableStateFlow(false)
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

    private val formState: StateFlow<WeighingFormState> =
        combine(scanInput, weightInput, selectedRow, message, actionInFlight) { scan, weight, selected, currentMessage, busy ->
            WeighingFormState(scan, weight, selected, currentMessage, busy)
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeighingFormState())

    private val rootState: StateFlow<WeighingRootState> =
        combine(assignments, loadingAssignments, plannerMode, plannerCatalog, plannerSelections) { availableAssignments, loading, isPlanner, catalog, selections ->
            WeighingRootState(availableAssignments, loading, isPlanner, catalog, selections)
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeighingRootState())

    val state: StateFlow<WeighingUiState> =
        combine(scopeState, formState, rootState) { scope, form, root ->
            scope.toUiState(
                scan = form.scan,
                weight = form.weight,
                selected = form.selected,
                currentMessage = form.message,
                busy = form.busy,
                availableAssignments = root.assignments,
                loading = root.loading,
                isPlanner = root.plannerMode,
                catalog = root.catalog,
                selections = root.selections,
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

    fun onScanInputChange(value: String) {
        scanInput.value = value
    }

    fun onWeightInputChange(value: String) {
        weightInput.value = value.filter { it.isDigit() || it == '.' }.take(8)
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
        val weightKg = weightInput.value.toDoubleOrNull()?.takeIf { it > 0.0 } ?: return
        if (actionInFlight.value) return
        analytics.track(AnalyticsEvents.WEIGHING_CAPTURE_ATTEMPT, weighingCaptureProps(INDIVIDUAL_ANIMAL_CATEGORY))
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
                            reportCaptureFailure(INDIVIDUAL_ANIMAL_CATEGORY, "missing_video")
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
                            analytics.track(
                                AnalyticsEvents.WEIGHING_CAPTURE_SUCCESS,
                                weighingCaptureProps(INDIVIDUAL_ANIMAL_CATEGORY),
                            )
                            weightInput.value = ""
                            scanInput.value = ""
                        } else {
                            message.value = "Weight saved locally. Proof could not be stored."
                            reportCaptureFailure(INDIVIDUAL_ANIMAL_CATEGORY, "proof_store_failed")
                        }
                        recorded.value
                    }
                    is AppResult.Err -> {
                        message.value = recorded.message
                        reportCaptureFailure(INDIVIDUAL_ANIMAL_CATEGORY, recorded.message)
                    }
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
        analytics.track(AnalyticsEvents.WEIGHING_CAPTURE_ATTEMPT, weighingCaptureProps(PER_SHED_PARTITION_CATEGORY))
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
                            reportCaptureFailure(PER_SHED_PARTITION_CATEGORY, "missing_video")
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
                            analytics.track(
                                AnalyticsEvents.WEIGHING_CAPTURE_SUCCESS,
                                weighingCaptureProps(PER_SHED_PARTITION_CATEGORY),
                            )
                            weightInput.value = ""
                        } else {
                            message.value = "Shed weight saved locally. Proof could not be stored."
                            reportCaptureFailure(PER_SHED_PARTITION_CATEGORY, "proof_store_failed")
                        }
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
        viewModelScope.launch {
            val match = repository.matchTag(key, tag)
            val row = match.row ?: unknownWeighingRow(key, tag)
            selectedRow.value = row
            scanInput.value = tag
            message.value = when {
                match.row == null -> "New RFID captured for this shed. Enter weight, then capture video."
                match.outcome == "wrong_shed" ->
                    "Wrong shed scan: expected ${match.expectedLocationLabel}, currently ${match.actualLocationLabel}."
                else -> "Matched ${row.displayAnimalId}."
            }
        }
    }

    private fun unknownWeighingRow(key: String, tag: String): WeighingRosterRowEntity {
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
            updatedAt = System.currentTimeMillis(),
        )
    }

    private fun WeighingScopeState?.toUiState(
        scan: String,
        weight: String,
        selected: WeighingRosterRowEntity?,
        currentMessage: String?,
        busy: Boolean,
        availableAssignments: List<WeighingAssignment>,
        loading: Boolean,
        isPlanner: Boolean,
        catalog: WeighingPlannerCatalog?,
        selections: Map<String, String>,
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
                selectedAnimalId = selected?.animalId,
                selectedAnimalLabel = selected?.let { "${it.displayAnimalId} in ${it.expectedLocationLabel}" },
                message = currentMessage,
                actionInFlight = busy,
                loading = true,
                category = category,
            )
        }
        val scope = this ?: return WeighingUiState(
            scanInput = scan,
            weightInput = weight,
            message = currentMessage,
            assignments = availableAssignments.map { it.toUiRow() },
            loading = loading,
            category = category,
            plannerMode = isPlanner,
            plannerWeekLabel = plannerWeek.label,
            plannerPeriodLabel = plannerWeek.periodLabel,
            plannerDayTabs = plannerWeek.dayTabs,
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
            message = currentMessage,
            actionInFlight = busy,
            category = category,
            visibleRows = scope.rosterWindow.map { row ->
                WeighingRosterUiRow(
                    id = row.id,
                    animalId = row.animalId,
                    displayAnimalId = row.displayAnimalId,
                    expectedLocationLabel = row.expectedLocationLabel,
                    actualLocationLabel = row.actualLocationLabel,
                    status = row.status.readableWeighingStatus(),
                    availabilityStatus = row.availabilityStatus?.readableWeighingStatus(),
                    wrongShed = row.availabilityStatus.equals("moved_other_shed", ignoreCase = true) ||
                        (
                            row.availabilityStatus.isNullOrBlank() &&
                                !row.actualLocationId.isNullOrBlank() &&
                                row.actualLocationId != row.expectedLocationId
                            ),
                )
            },
            individualDrafts = scope.individualDrafts.map { draft ->
                val animalLabel = scope.rosterWindow
                    .firstOrNull { it.animalId == draft.animalId }
                    ?.displayAnimalId
                    ?: "Matched animal"
                WeighingDraftUiRow(
                    id = draft.observationId,
                    animalId = draft.animalId,
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
        const val ROSTER_SYNC_MAX_ROWS = 10_000
        const val READER_REFRESH_MS = 5_000L
        const val INDIVIDUAL_PROOF_FIELD_KEY = "weighing_individual_video"
        const val SHED_PARTITION_PROOF_FIELD_KEY = "weighing_shed_partition_video"
        const val INDIVIDUAL_ANIMAL_CATEGORY = "individual_animal"
        const val PER_SHED_PARTITION_CATEGORY = "per_shed_partition"
        const val MAX_ANALYTICS_REASON_CHARS = 96
        const val DEFAULT_PLANNED_CAP_PER_DAY = 100
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
    val selected: WeighingRosterRowEntity? = null,
    val message: String? = null,
    val busy: Boolean = false,
)

private data class WeighingRootState(
    val assignments: List<WeighingAssignment> = emptyList(),
    val loading: Boolean = false,
    val plannerMode: Boolean = false,
    val catalog: WeighingPlannerCatalog? = null,
    val selections: Map<String, String> = emptyMap(),
)

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
