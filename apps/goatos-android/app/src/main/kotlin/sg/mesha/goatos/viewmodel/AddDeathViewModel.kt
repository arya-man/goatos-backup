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
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CountsRepository
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.CountsDeathEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsEvidenceRefDto
import sg.mesha.goatos.core.network.dto.GoatSearchItemDto
import sg.mesha.goatos.feature.counts.AddDeathEvent
import sg.mesha.goatos.feature.counts.AddDeathUiState
import sg.mesha.goatos.feature.counts.CountsWriteResultUi
import sg.mesha.goatos.feature.counts.CountsWriteStatus
import javax.inject.Inject

/**
 * Add-death (`/counts/death/add` — docs/decisions/birth-death-workflows.md). The retired combined
 * form's DEATH mode UNCHANGED: tag/RFID search → select ONE animal (its own `goat_id` +
 * `row_version` from the search result) → reason (3–500 chars) → durable outbox write under a
 * stable SavedStateHandle-persisted idempotency key. The `lifecycle_status="dead"` +
 * `exit_reason="died"` pairing stays a SERVER-enforced guardrail — this VM only sends the DTO's
 * constants. Submit durably opens the two-upload death workflow while the animal remains alive;
 * admin approval later applies the exit/count change and releases both videos to Verify.
 */
@HiltViewModel
class AddDeathViewModel @Inject constructor(
    private val syncRepository: SyncRepository,
    private val countsRepository: CountsRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val idempotencyKey = DraftIdempotencyKey(
        savedStateHandle = savedStateHandle,
        stateKey = KEY_IDEMPOTENCY,
        prefix = "counts-death-add",
    )
    private val outboxItemId = DraftOutboxItemId(savedStateHandle, KEY_OUTBOX_ITEM_ID)

    private val _state = MutableStateFlow(AddDeathUiState())
    val state: StateFlow<AddDeathUiState> = _state.asStateFlow()

    private var statusJob: Job? = null

    init {
        outboxItemId.value?.let(::observeOutboxItem)
        recomputeSubmitGate()
    }

    fun onEvent(event: AddDeathEvent) {
        when (event) {
            is AddDeathEvent.EditAnimalQuery -> onEditAnimalQuery(event.value)
            AddDeathEvent.LookupAnimals -> lookupAnimals()
            is AddDeathEvent.SelectAnimal -> onSelectAnimal(event.goatId)
            is AddDeathEvent.EditReason -> onEditReason(event.value)
            AddDeathEvent.Submit -> submit()
            AddDeathEvent.RecordAnother -> resetForNextEntry(confirmation = null)
            AddDeathEvent.Back -> Unit // navigation — handled by the nav host.
        }
    }

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

    private fun onEditAnimalQuery(value: String) {
        if (!beginEdit()) return
        _state.update { it.copy(animalQuery = value, animalLookupMessage = null) }
    }

    private fun onEditReason(value: String) {
        if (!beginEdit()) return
        _state.update { it.copy(reason = value) }
        recomputeSubmitGate()
    }

    /** Resolves the typed/scanned tag to real animals — pins the goat_id AND the row_version. */
    private fun lookupAnimals() {
        val current = _state.value
        val query = current.animalQuery.trim()
        if (query.isEmpty() || current.isLookingUpAnimals) return
        _state.update { it.copy(isLookingUpAnimals = true, animalLookupMessage = null) }
        viewModelScope.launch {
            countsRepository.lookupAnimals(query = query)
                .onSuccess { matches ->
                    _state.update {
                        it.copy(
                            isLookingUpAnimals = false,
                            animalMatches = matches.map(GoatSearchItemDto::toShiftingAnimalUi),
                            animalLookupMessage = if (matches.isEmpty()) NO_MATCH_MESSAGE else null,
                        )
                    }
                }
                .onFailure { error ->
                    crashReporter.recordException(error, "add-death animal lookup failed")
                    analytics.track(
                        AnalyticsEvents.COUNTS_READ_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.KIND to "death_animal_lookup",
                            AnalyticsEvents.Params.REASON to (error.message ?: "unknown"),
                        ),
                    )
                    // A failed search must not wipe an animal the operator already found/selected.
                    _state.update {
                        it.copy(isLookingUpAnimals = false, animalLookupMessage = LOOKUP_FAILED_MESSAGE)
                    }
                }
            recomputeSubmitGate()
        }
    }

    /** Single selection: REPLACES any previous choice. A tap on the selected row is a no-op. */
    private fun onSelectAnimal(goatId: String) {
        if (!beginEdit()) return
        _state.update { current ->
            val match = current.animalMatches.firstOrNull { it.goatId == goatId } ?: return@update current
            current.copy(selectedAnimal = match)
        }
        recomputeSubmitGate()
    }

    private fun submit() {
        val current = _state.value
        if (!current.canSubmit) return
        val animal = current.selectedAnimal ?: return
        val key = idempotencyKey.current()
        viewModelScope.launch {
            val result = syncRepository.enqueueCountsDeath(
                groupKey = animal.goatId,
                idempotencyKey = key,
                request = CountsDeathEventRequestDto(
                    goatId = animal.goatId,
                    // lifecycle_status / exit_reason stay the DTO's guardrail constants — the
                    // dead+died pairing is not an operator choice.
                    reason = current.reason.trim(),
                    evidenceRefs = listOf(
                        CountsEvidenceRefDto(evidenceId = key, description = EVIDENCE_DESCRIPTION),
                    ),
                    // The animal's OWN optimistic-concurrency token from the search result.
                    rowVersion = animal.rowVersion,
                ),
            )
            when (result) {
                is AppResult.Ok -> {
                    outboxItemId.value = result.value
                    observeOutboxItem(result.value)
                    analytics.track(AnalyticsEvents.COUNTS_DEATH_SUBMITTED)
                }
                is AppResult.Err -> onEnqueueFailed(result)
            }
        }
    }

    private fun onEnqueueFailed(error: AppResult.Err) {
        error.cause?.let { crashReporter.recordException(it, "counts death enqueue failed") }
        analytics.track(
            AnalyticsEvents.COUNTS_WRITE_FAILURE,
            mapOf(
                AnalyticsEvents.Params.KIND to "death",
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
                        resetForNextEntry(confirmation = writeResult.message)
                        return@collect
                    }
                    _state.update { it.copy(result = writeResult) }
                    recomputeSubmitGate()
                }
        }
    }

    private fun resetForNextEntry(confirmation: String?) {
        statusJob?.cancel()
        statusJob = null
        idempotencyKey.invalidate()
        outboxItemId.value = null
        _state.update { AddDeathUiState(lastRecordedMessage = confirmation) }
        recomputeSubmitGate()
    }

    /** Same validation rules the combined form's death mode enforced (backend re-enforces both). */
    private fun recomputeSubmitGate() {
        _state.update { current ->
            if (current.result.isCommitted) {
                return@update current.copy(canSubmit = false, validationMessage = null)
            }
            val missing = when {
                current.selectedAnimal == null -> "Find and select the animal that died."
                current.reason.trim().length < 3 -> "Describe what happened (at least 3 characters)."
                current.reason.trim().length > 500 -> "Keep the account under 500 characters."
                else -> null
            }
            current.copy(canSubmit = missing == null, validationMessage = missing)
        }
    }

    private companion object {
        const val KEY_IDEMPOTENCY = "countsAddDeath.idempotencyKey"
        const val KEY_OUTBOX_ITEM_ID = "countsAddDeath.outboxItemId"
        const val QUEUED_MESSAGE = "Saved on this phone. It will sync automatically."
        const val SYNCED_MESSAGE = "Submitted. Open Death and upload both required videos."
        const val EVIDENCE_DESCRIPTION = "Recorded on the operator app"
        const val NO_MATCH_MESSAGE = "No live animal matches that tag. Check the tag and try again."
        const val LOOKUP_FAILED_MESSAGE =
            "Couldn't search for animals. Check your connection and try again."
    }
}
