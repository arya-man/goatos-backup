package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CountsRepository
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.CountsApprovalSubmitResponseDto
import sg.mesha.goatos.core.network.dto.CountsBirthEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsDestinationParkDto
import sg.mesha.goatos.core.network.dto.CountsEvidenceRefDto
import sg.mesha.goatos.feature.counts.AddBirthEvent
import sg.mesha.goatos.feature.counts.AddBirthField
import sg.mesha.goatos.feature.counts.AddBirthUiState
import sg.mesha.goatos.feature.counts.CountsFilterOptionUi
import sg.mesha.goatos.feature.counts.CountsWriteResultUi
import sg.mesha.goatos.feature.counts.CountsWriteStatus
import sg.mesha.goatos.rfid.ScanSource
import java.time.LocalDate
import java.time.LocalTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import javax.inject.Inject

/**
 * Add-birth (`/counts/birth/add` — docs/decisions/birth-death-workflows.md §"Birth submit deltas").
 *
 * The retired combined form's BIRTH mode, split out and reworked per the 2026-07-27 decision:
 * NO identifier input (the server auto-generates a provisional `CBE-#####` or `CPT-#####` tag; the kid is tagged with
 * its permanent RFID later via the "Tag the kid" action → the existing promote flow), DOB LOCKED
 * to today (births are recorded as they happen), a NEW editable time-of-birth (`HH:MM` IST,
 * prefilled to now), and NO entry-date field (the client sends today). Everything else — the
 * offline outbox write with a stable SavedStateHandle-persisted idempotency key, the
 * QUEUED/SYNCED/FAILED banner lifecycle, the Room-cached park→shed and breed vocabularies, and
 * the validate-or-reject split (this VM gates submit; the BACKEND owns the business rules) — is
 * carried over from the combined form unchanged.
 */
@HiltViewModel
class AddBirthViewModel @Inject constructor(
    private val syncRepository: SyncRepository,
    private val countsRepository: CountsRepository,
    private val scanSource: ScanSource,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val idempotencyKey = DraftIdempotencyKey(
        savedStateHandle = savedStateHandle,
        stateKey = KEY_IDEMPOTENCY,
        prefix = "counts-birth-add",
    )
    private val outboxItemId = DraftOutboxItemId(savedStateHandle, KEY_OUTBOX_ITEM_ID)

    private val _state = MutableStateFlow(
        AddBirthUiState(dob = todayBusinessDate(), timeOfBirth = nowIstTime()),
    )
    val state: StateFlow<AddBirthUiState> = _state.asStateFlow()

    private var statusJob: Job? = null
    private var scanJob: Job? = null

    init {
        // A ViewModel recreated after process death resumes following its already-queued write
        // instead of showing a blank form that invites a duplicate entry.
        outboxItemId.value?.let(::observeOutboxItem)
        observeDestinations()
        refreshDestinations()
        observeBreedOptions()
        refreshBreedOptions()
        recomputeSubmitGate()
    }

    fun onEvent(event: AddBirthEvent) {
        when (event) {
            is AddBirthEvent.EditField -> onEditField(event.field, event.value)
            is AddBirthEvent.SelectPark -> onSelectPark(event.parkId)
            is AddBirthEvent.SelectShed -> onSelectShed(event.shedId)
            is AddBirthEvent.SelectLitterSize -> onSelectLitterSize(event.litterSize)
            AddBirthEvent.ToggleMotherRfidScan -> toggleMotherRfidScan()
            AddBirthEvent.Submit -> submit()
            AddBirthEvent.RecordAnother -> resetForNextEntry(confirmation = null)
            AddBirthEvent.NavigationHandled -> _state.update { it.copy(returnToBirthList = false) }
            AddBirthEvent.Back -> Unit // navigation — handled by the nav host.
        }
    }

    /** Shared pre-edit guard (same contract as the combined form): a committed draft is locked;
     *  correcting a terminally-rejected write re-keys it. */
    private fun beginEdit(): Boolean {
        val result = _state.value.result
        if (result.isCommitted) return false
        if (result.isCorrectable) {
            idempotencyKey.invalidate()
            outboxItemId.value = null
            statusJob?.cancel()
            _state.update { it.copy(result = CountsWriteResultUi()) }
        }
        if (_state.value.lastRecordedMessage != null) {
            _state.update { it.copy(lastRecordedMessage = null) }
        }
        return true
    }

    private fun onEditField(field: AddBirthField, value: String) {
        if (!beginEdit()) return
        _state.update { current ->
            when (field) {
                AddBirthField.SPECIES -> current.copy(species = value)
                AddBirthField.SEX -> current.copy(sex = value)
                AddBirthField.TIME_OF_BIRTH -> current.copy(timeOfBirth = value)
                AddBirthField.BREED -> current.copy(breed = value)
                AddBirthField.DAM_ID -> current.copy(damId = value)
            }
        }
        recomputeSubmitGate()
    }

    private fun observeDestinations() {
        viewModelScope.launch {
            countsRepository.observeShiftingDestinations().collect { resource ->
                val parks = resource.data?.parks?.map(CountsDestinationParkDto::toShiftingParkUi).orEmpty()
                _state.update { current ->
                    val parkStillOffered = parks.any { it.parkId == current.parkId }
                    val parkId = if (parkStillOffered) current.parkId else ""
                    val shedStillOffered = parks
                        .firstOrNull { it.parkId == parkId }
                        ?.sheds
                        // Match the PEN, not just the shed: a refresh that drops "Godel 1 - 7"
                        // while keeping "Godel 1" must not silently retain the stale partition.
                        ?.any { it.shedId == current.shedId && it.partitionLabel == current.partitionLabel } == true
                    current.copy(
                        destinationParks = parks,
                        parkId = parkId,
                        shedId = if (shedStillOffered) current.shedId else "",
                        partitionLabel = if (shedStillOffered) current.partitionLabel else null,
                        destinationsMessage = if (parks.isEmpty()) current.destinationsMessage else null,
                    )
                }
                recomputeSubmitGate()
            }
        }
    }

    private fun refreshDestinations() {
        viewModelScope.launch {
            countsRepository.refreshShiftingDestinations()
                .onFailure { error ->
                    crashReporter.recordException(error, "add-birth destinations refresh failed")
                    analytics.track(
                        AnalyticsEvents.COUNTS_READ_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.KIND to "birth_placement_destinations",
                            AnalyticsEvents.Params.REASON to (error.message ?: "unknown"),
                        ),
                    )
                    _state.update { current ->
                        if (current.destinationParks.isNotEmpty()) {
                            current
                        } else {
                            current.copy(destinationsMessage = DESTINATIONS_FAILED_MESSAGE)
                        }
                    }
                }
        }
    }

    private fun observeBreedOptions() {
        viewModelScope.launch {
            countsRepository.observeBirthBreeds().collect { resource ->
                val options = resource.data?.breeds
                    ?.map { CountsFilterOptionUi(it.key, it.label, it.count) }
                    .orEmpty()
                _state.update { current ->
                    val breedStillOffered = options.any { it.key == current.breed }
                    current.copy(
                        breedOptions = options,
                        breed = if (breedStillOffered) current.breed else "",
                    )
                }
                recomputeSubmitGate()
            }
        }
    }

    private fun refreshBreedOptions() {
        viewModelScope.launch {
            countsRepository.refreshBirthBreeds()
                .onFailure { error ->
                    crashReporter.recordException(error, "add-birth breed vocabulary refresh failed")
                    analytics.track(
                        AnalyticsEvents.COUNTS_READ_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.KIND to "birth_breed_vocabulary",
                            AnalyticsEvents.Params.REASON to (error.message ?: "unknown"),
                        ),
                    )
                }
        }
    }

    /** Choosing a park RESETS the shed — a shed id belongs to exactly one park. */
    private fun onSelectPark(parkId: String) {
        if (!beginEdit()) return
        _state.update { current ->
            if (current.parkId == parkId) current else current.copy(parkId = parkId, shedId = "")
        }
        recomputeSubmitGate()
    }

    /**
     * [optionKey] is the composite dropdown key `shedId|partitionLabel` (or a bare `shedId` for a
     * non-partitioned shed), not a shed id. Resolving the option rather than parsing the string
     * keeps the pen exactly as the backend spelled it, so a label containing "|" could never
     * corrupt the selection.
     */
    private fun onSelectShed(optionKey: String) {
        if (!beginEdit()) return
        _state.update { current ->
            val option = current.shedsForSelectedPark.firstOrNull { it.optionKey == optionKey }
            if (option != null) {
                current.copy(shedId = option.shedId, partitionLabel = option.partitionLabel)
            } else {
                current
            }
        }
        recomputeSubmitGate()
    }

    private fun onSelectLitterSize(litterSize: Int) {
        if (litterSize !in 1..3 || !beginEdit()) return
        _state.update { it.copy(litterSize = litterSize) }
        recomputeSubmitGate()
    }

    private fun toggleMotherRfidScan() {
        if (_state.value.scanningMotherRfid) {
            stopMotherRfidScan()
            return
        }
        if (!beginEdit()) return
        _state.update { it.copy(scanningMotherRfid = true) }
        scanSource.start()
        scanJob = viewModelScope.launch {
            scanSource.tags.collect { tag ->
                val value = tag.trim()
                if (value.isNotEmpty()) onEditField(AddBirthField.DAM_ID, value)
                stopMotherRfidScan()
            }
        }
    }

    private fun stopMotherRfidScan() {
        if (!_state.value.scanningMotherRfid) return
        scanSource.stop()
        scanJob?.cancel()
        scanJob = null
        _state.update { it.copy(scanningMotherRfid = false) }
    }

    override fun onCleared() {
        stopMotherRfidScan()
        super.onCleared()
    }

    private fun submit() {
        stopMotherRfidScan()
        val current = _state.value
        if (!current.canSubmit) return
        val key = idempotencyKey.current()
        viewModelScope.launch {
            val result = syncRepository.enqueueCountsBirth(
                // No client identifier exists any more (the server mints the provisional tag), so
                // the draft key itself partitions ordering — each birth draft is its own group.
                groupKey = key,
                idempotencyKey = key,
                request = CountsBirthEventRequestDto(
                    // Identifiers OMITTED entirely (decision 2026-07-27): the server auto-generates
                    // a provisional CBE-#####/CPT-##### temporary tag; the kid is promoted to its permanent RFID
                    // later through the "Tag the kid" action.
                    species = current.species,
                    parkId = current.parkId.ifBlank { null },
                    shedId = current.shedId.ifBlank { null },
                    partitionLabel = current.partitionLabel?.takeIf { it.isNotBlank() },
                    breed = current.breed.trim(),
                    sex = current.sex,
                    // DOB is locked to today (births are recorded as they happen) and the entry
                    // date is stamped to the same business day — both Asia/Kolkata.
                    dob = todayBusinessDate(),
                    timeOfBirth = current.timeOfBirth.trim().ifBlank { null },
                    entryDate = todayBusinessDate(),
                    damId = current.damId.trim(),
                    litterSize = current.litterSize,
                    evidenceRefs = listOf(
                        CountsEvidenceRefDto(evidenceId = key, description = EVIDENCE_DESCRIPTION),
                    ),
                ),
            )
            when (result) {
                is AppResult.Ok -> {
                    outboxItemId.value = result.value
                    observeOutboxItem(result.value)
                    analytics.track(AnalyticsEvents.COUNTS_BIRTH_SUBMITTED)
                }
                is AppResult.Err -> onEnqueueFailed(result)
            }
        }
    }

    private fun onEnqueueFailed(error: AppResult.Err) {
        error.cause?.let { crashReporter.recordException(it, "counts birth enqueue failed") }
        analytics.track(
            AnalyticsEvents.COUNTS_WRITE_FAILURE,
            mapOf(
                AnalyticsEvents.Params.KIND to "birth",
                AnalyticsEvents.Params.REASON to error.message,
            ),
        )
        _state.update {
            it.copy(result = CountsWriteResultUi(CountsWriteStatus.FAILED, error.message))
        }
        recomputeSubmitGate()
    }

    private fun observeOutboxItem(itemId: String) {
        statusJob?.cancel()
        statusJob = viewModelScope.launch {
            syncRepository.observeStatus()
                .map { status -> status.items.firstOrNull { it.id == itemId } }
                .filterNotNull()
                .distinctUntilChanged()
                .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)
                .collect { item ->
                    item ?: return@collect
                    val writeResult = item.toWriteResult(QUEUED_MESSAGE, SYNCED_MESSAGE)
                    if (writeResult.status == CountsWriteStatus.SYNCED) {
                        finishSuccessfulSubmission(item.resultJson)
                        return@collect
                    }
                    _state.update { it.copy(result = writeResult) }
                    recomputeSubmitGate()
                }
        }
    }

    /** Clear the completed delivery and return to the Birth list of newly created child workflows. */
    private fun finishSuccessfulSubmission(resultJson: String?) {
        statusJob?.cancel()
        statusJob = null
        idempotencyKey.invalidate()
        outboxItemId.value = null
        val response = resultJson?.let { raw ->
            runCatching { RESPONSE_JSON.decodeFromString<CountsApprovalSubmitResponseDto>(raw) }.getOrNull()
        }
        val createdCount = response?.children?.size?.takeIf { it > 0 } ?: _state.value.litterSize
        val notice = if (createdCount == 1) {
            "Birth recorded. 1 child workflow is ready; herd-count approval is separate."
        } else {
            "Birth recorded. $createdCount child workflows are ready; herd-count approval is separate."
        }
        _state.update { current ->
            AddBirthUiState(
                dob = todayBusinessDate(),
                timeOfBirth = nowIstTime(),
                destinationParks = current.destinationParks,
                breedOptions = current.breedOptions,
                returnToBirthList = true,
                submissionNotice = notice,
            )
        }
        recomputeSubmitGate()
    }

    /** Starts a separate delivery while the previous offline write remains queued. */
    private fun resetForNextEntry(confirmation: String?) {
        statusJob?.cancel()
        statusJob = null
        idempotencyKey.invalidate()
        outboxItemId.value = null
        _state.update { current ->
            AddBirthUiState(
                dob = todayBusinessDate(),
                timeOfBirth = nowIstTime(),
                destinationParks = current.destinationParks,
                breedOptions = current.breedOptions,
                lastRecordedMessage = confirmation,
            )
        }
        recomputeSubmitGate()
    }

    private fun recomputeSubmitGate() {
        _state.update { current ->
            if (current.result.isCommitted) {
                return@update current.copy(canSubmit = false, validationMessage = null)
            }
            val missing = validation(current)
            current.copy(canSubmit = missing == null, validationMessage = missing)
        }
    }

    private fun validation(state: AddBirthUiState): String? = when {
        // Time of birth is REQUIRED in HH:MM (prefilled to now, so this only fires after an edit).
        !TIME_24H.matches(state.timeOfBirth.trim()) -> "Enter the time of birth as HH:MM (24-hour)."
        state.breed.isBlank() -> "Choose the newborn's breed."
        state.damId.isBlank() -> "Scan or enter the mother's RFID."
        state.litterSize !in 1..3 -> "Choose 1, twins, or triplets."
        // Placement is REQUIRED and chosen from the catalog — a newborn is never recorded into no shed.
        state.parkId.isBlank() -> "Choose the park the newborn is placed in."
        state.shedId.isBlank() -> "Choose the shed the newborn is placed in."
        else -> null
    }

    /** Today's business date in Asia/Kolkata (AGENTS.md time semantics), as ISO `YYYY-MM-DD`. */
    private fun todayBusinessDate(): String =
        LocalDate.now(IST).format(DateTimeFormatter.ISO_LOCAL_DATE)

    /** The current IST wall time as `HH:MM` — the time-of-birth prefill. */
    private fun nowIstTime(): String = LocalTime.now(IST).format(HHMM)

    private companion object {
        val IST: ZoneId = ZoneId.of("Asia/Kolkata")
        val HHMM: DateTimeFormatter = DateTimeFormatter.ofPattern("HH:mm")

        const val KEY_IDEMPOTENCY = "countsAddBirth.idempotencyKey"
        const val KEY_OUTBOX_ITEM_ID = "countsAddBirth.outboxItemId"
        const val QUEUED_MESSAGE = "Saved on this phone. It will sync automatically."
        const val SYNCED_MESSAGE = "Submitted for approval."
        const val EVIDENCE_DESCRIPTION = "Recorded on the operator app"
        const val DESTINATIONS_FAILED_MESSAGE =
            "Couldn't load the list of parks and sheds. Check your connection and try again."

        /** 24-hour HH:MM — mirrors the contract's `time_of_birth` pattern. */
        val TIME_24H = Regex("""^([01][0-9]|2[0-3]):[0-5][0-9]$""")
        val RESPONSE_JSON = Json { ignoreUnknownKeys = true }
    }
}
