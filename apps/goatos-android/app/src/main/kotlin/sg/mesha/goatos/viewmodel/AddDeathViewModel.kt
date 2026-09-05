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
import sg.mesha.goatos.core.data.DeathCauseVocabulary
import sg.mesha.goatos.core.network.dto.CountsDeathEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsEvidenceRefDto
import sg.mesha.goatos.feature.counts.AddDeathEvent
import sg.mesha.goatos.feature.counts.AddDeathUiState
import sg.mesha.goatos.feature.counts.CountsWriteResultUi
import sg.mesha.goatos.feature.counts.CountsWriteStatus
import sg.mesha.goatos.feature.counts.DeathCauseKind
import sg.mesha.goatos.feature.counts.DeathCauseOptionUi
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
    // The disease vocabulary is HEALTH's, borrowed rather than copied so a death and a treatment
    // case can never be filed under two spellings of one illness. Held as the NARROW port: this
    // form fills one dropdown and has no business reaching Health's paging or diagnosis surface.
    private val deathCauses: DeathCauseVocabulary,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    private val savedStateHandle: SavedStateHandle,
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

    /**
     * The animal and the disease this form was opened WITH, when it was reached from a treatment
     * screen because the animal died mid-course. Both blank for the ordinary ＋ path.
     *
     * The animal arrives as a SEARCH TERM, never a resolved id: recording a death is terminal, and
     * the confirm-the-animal step exists precisely so the operator sees the tag, pen and status of
     * what they are about to record. Pre-filling the search saves the typing; it does not skip the
     * check.
     */
    private val prefilledAnimal: String =
        savedStateHandle.get<String>(ARG_ANIMAL).orEmpty().trim()
    private val prefilledCause: String =
        savedStateHandle.get<String>(ARG_CAUSE).orEmpty().trim()

    init {
        outboxItemId.value?.let(::observeOutboxItem)
        observeDeathCauses()
        refreshDeathCauses()
        applyPrefill()
        recomputeSubmitGate()
    }

    fun onEvent(event: AddDeathEvent) {
        when (event) {
            is AddDeathEvent.EditAnimalQuery -> onEditAnimalQuery(event.value)
            AddDeathEvent.LookupAnimals -> lookupAnimals()
            is AddDeathEvent.SelectAnimal -> onSelectAnimal(event.goatId)
            is AddDeathEvent.EditReason -> onEditReason(event.value)
            is AddDeathEvent.SelectDeathCauseKind -> onSelectDeathCauseKind(event.kind)
            is AddDeathEvent.EditDeathCauseQuery -> onEditDeathCauseQuery(event.value)
            is AddDeathEvent.SelectDeathCause -> onSelectDeathCause(event.key)
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
                            animalMatches = matches.toDistinctShiftingAnimalUi(),
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
                    // THE CAUSE, or nothing at all. A death the operator called normal sends
                    // neither field, which is what every death recorded before this feature
                    // existed also carries — absence is the complete answer for "no disease was
                    // established", never a placeholder. The pair travels together because the
                    // server and the database both refuse one without the other.
                    deathCauseKey = current.submittedDeathCause?.key,
                    deathCauseKind = current.submittedDeathCause?.kind,
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

    /**
     * Follows the Room-cached disease vocabulary. Emits whatever the device already holds
     * immediately, so the dropdown is usable on a cold network, and again after
     * [refreshDeathCauses].
     *
     * A refresh that no longer offers the disease the operator had already picked CLEARS the
     * selection rather than submitting a key the register has stopped naming. The server would
     * reject it either way — but only AFTER the operator had left the form believing the death
     * was recorded, which for a terminal action is the worst place to find out.
     */
    private fun observeDeathCauses() {
        viewModelScope.launch {
            deathCauses.observeDeathCauses().collect { catalog ->
                val options = catalog?.options
                    ?.filter { it.key.isNotBlank() && it.label.isNotBlank() }
                    ?.map { DeathCauseOptionUi(key = it.key, kind = it.kind, label = it.label) }
                    .orEmpty()
                _state.update { current ->
                    val stillOffered = options.any { it.key == current.selectedDeathCause?.key }
                    // The disease a treatment screen handed over is resolved HERE, from a real
                    // catalog row, so its KIND travels with its key. A rule the register no longer
                    // names simply does not resolve and the operator searches as usual.
                    //
                    // It fills a selection only when there is NONE. Using it as the fallback for a
                    // selection that stopped being offered would resurrect exactly the retired
                    // disease this branch exists to drop.
                    val handedOver = if (current.selectedDeathCause == null) {
                        options.firstOrNull { it.key == prefilledCause }
                    } else {
                        null
                    }
                    current.copy(
                        deathCauseOptions = options,
                        selectedDeathCause = if (stillOffered) current.selectedDeathCause else handedOver,
                        // Only clear the unavailable notice once a list actually arrived.
                        deathCauseMessage = if (options.isEmpty()) current.deathCauseMessage else null,
                    )
                }
                recomputeSubmitGate()
            }
        }
    }

    /**
     * Warms the disease vocabulary from `GET /app/health/death-causes`.
     *
     * Non-fatal by design: a phone that cannot reach the server keeps the cached list, and one
     * that has never had it shows the disease choice as unavailable WITH ITS REASON rather than as
     * an empty dropdown — empty reads as "this farm has no diseases" and would file a disease
     * death as normal with nobody noticing.
     */
    private fun refreshDeathCauses() {
        viewModelScope.launch {
            deathCauses.refreshDeathCauses()
                .onFailure { error ->
                    crashReporter.recordException(error, "counts add-death disease vocabulary refresh failed")
                    analytics.track(
                        AnalyticsEvents.COUNTS_READ_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.KIND to "death_cause_vocabulary",
                            AnalyticsEvents.Params.REASON to (error.message ?: "unknown"),
                        ),
                    )
                    _state.update { current ->
                        if (current.deathCauseOptions.isNotEmpty()) {
                            current
                        } else {
                            current.copy(deathCauseMessage = DEATH_CAUSES_UNAVAILABLE_MESSAGE)
                        }
                    }
                    recomputeSubmitGate()
                }
        }
    }

    /**
     * The normal/disease toggle. Switching back to NORMAL DROPS the disease and the search text:
     * keeping them would leave a chosen disease invisibly attached to a death the operator has
     * just said was not caused by one.
     */
    private fun onSelectDeathCauseKind(kind: DeathCauseKind) {
        if (_state.value.result.isCommitted) return
        _state.update { current ->
            if (kind == DeathCauseKind.NORMAL) {
                current.copy(deathCauseKind = kind, deathCauseQuery = "", selectedDeathCause = null)
            } else {
                current.copy(deathCauseKind = kind)
            }
        }
        recomputeSubmitGate()
    }

    /**
     * Filtering is LOCAL — the whole vocabulary is on the device — so typing costs no round trip.
     * Editing the query does NOT clear a chosen disease: the operator may type to look at a
     * neighbouring entry and change their mind back.
     */
    private fun onEditDeathCauseQuery(value: String) {
        if (_state.value.result.isCommitted) return
        _state.update { it.copy(deathCauseQuery = value) }
    }

    /**
     * Records the chosen disease WITH ITS KIND, taken verbatim from the catalog row. The phone
     * never composes a kind: the same string can live in two vocabularies, so a key that arrives
     * without the kind it came with cannot be read back.
     */
    private fun onSelectDeathCause(key: String) {
        if (_state.value.result.isCommitted) return
        _state.update { current ->
            val option = current.deathCauseOptions.firstOrNull { it.key == key } ?: return@update current
            current.copy(selectedDeathCause = option)
        }
        recomputeSubmitGate()
    }

    /**
     * Applies what the treatment screen handed over: the animal's identifier into the search box,
     * and the case's disease as the cause.
     *
     * THE DISEASE IS NOT SELECTED HERE, only remembered. The catalog may not have arrived yet, and
     * a selection has to come from a real catalog row so its KIND travels with its key —
     * synthesising an option from a bare string would put a key on the wire that no list ever
     * offered. [observeDeathCauses] resolves it the moment the vocabulary lands, and if the
     * register no longer names that rule the form simply opens on the search, which is the same
     * honest outcome as a pre-engine case that never had a rule at all.
     *
     * The search is NOT run automatically. The operator taps Search themselves, because a lookup
     * firing on its own would put a list of animals on screen that they did not ask for, on the
     * one form where an unconsidered tap is irreversible.
     */
    private fun applyPrefill() {
        if (prefilledAnimal.isEmpty() && prefilledCause.isEmpty()) return
        _state.update { current ->
            current.copy(
                animalQuery = prefilledAnimal.ifEmpty { current.animalQuery },
                deathCauseKind = if (prefilledCause.isEmpty()) {
                    current.deathCauseKind
                } else {
                    DeathCauseKind.DISEASE
                },
            )
        }
    }

    private fun resetForNextEntry(confirmation: String?) {
        statusJob?.cancel()
        statusJob = null
        idempotencyKey.invalidate()
        outboxItemId.value = null
        _state.update { current ->
            // The VOCABULARY survives the reset, the SELECTION does not. Re-fetching between two
            // deaths would leave the second animal's form without a dropdown on a phone that has
            // since lost signal; carrying the disease forward would file a diagnosis nobody made
            // for it. The toggle returns to NORMAL with the rest of the defaults.
            AddDeathUiState(
                deathCauseOptions = current.deathCauseOptions,
                deathCauseMessage = current.deathCauseMessage,
                lastRecordedMessage = confirmation,
            )
        }
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
                // A disease death must NAME the disease. Letting it through with the toggle on and
                // nothing chosen would record a death the operator said was caused by something,
                // as one caused by nothing — the exact fact this feature exists to stop losing.
                current.deathCauseKind == DeathCauseKind.DISEASE && current.selectedDeathCause == null ->
                    "Choose the disease, or record this as a normal death."
                // The written account is the ONLY record of why a normal death happened, so it is
                // required there. Once a disease is named it has already answered that and the
                // note becomes optional — but a note that IS written is still held to its length.
                current.deathCauseKind == DeathCauseKind.NORMAL && current.reason.trim().length < 3 ->
                    "Describe what happened (at least 3 characters)."
                current.reason.trim().length > 500 -> "Keep the account under 500 characters."
                else -> null
            }
            current.copy(canSubmit = missing == null, validationMessage = missing)
        }
    }

    private companion object {
        const val DEATH_CAUSES_UNAVAILABLE_MESSAGE =
            "The disease list isn't on this phone yet. Connect once to load it, or record a normal " +
                "death and describe what you saw."
        /** Route query args, mirroring Routes.COUNTS_DEATH_ADD_*_ARG. */
        const val ARG_ANIMAL = "animal"
        const val ARG_CAUSE = "cause"

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
