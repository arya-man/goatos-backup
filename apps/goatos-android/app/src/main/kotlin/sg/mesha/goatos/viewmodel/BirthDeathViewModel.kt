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
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.CountsBirthEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsDeathEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsEvidenceRefDto
import sg.mesha.goatos.feature.counts.BirthDeathEvent
import sg.mesha.goatos.feature.counts.BirthDeathField
import sg.mesha.goatos.feature.counts.BirthDeathMode
import sg.mesha.goatos.feature.counts.BirthDeathUiState
import sg.mesha.goatos.feature.counts.CountsWriteResultUi
import javax.inject.Inject

/**
 * Birth / death recording (`/counts/birth-death`).
 *
 * Offline-first WRITE path: submitting never calls the network from here. The request is handed to
 * the durable outbox ([SyncRepository.enqueueCountsBirth] / [SyncRepository.enqueueCountsDeath]),
 * which returns as soon as the row is committed to Room; the sync engine drains it later with the
 * SAME idempotency key on every attempt. The banner then follows that row's real status.
 *
 * Validation split, per AGENTS.md's validate-or-reject rule:
 *  - this ViewModel gates SUBMIT on the fields being PRESENT and well-formed, so an operator is
 *    not sent on a pointless round trip;
 *  - the BACKEND remains the authority on business rules (dob <= entry_date, identifier
 *    uniqueness, the dead+died pairing, row-version freshness). Nothing here silently rewrites an
 *    out-of-range value into a default the operator never entered — a blank numeric field stays
 *    blank and simply blocks submit.
 *
 * Medical guardrail: the death path targets identity's critical-death exit, where
 * `lifecycle_status="dead"` + `exit_reason="died"` is enforced server-side. This ViewModel sends
 * the DTO's constants and never lets the operator choose another pairing.
 */
@HiltViewModel
class BirthDeathViewModel @Inject constructor(
    private val syncRepository: SyncRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val idempotencyKey = DraftIdempotencyKey(
        savedStateHandle = savedStateHandle,
        stateKey = KEY_IDEMPOTENCY,
        prefix = "counts-birth-death",
    )
    private val outboxItemId = DraftOutboxItemId(savedStateHandle, KEY_OUTBOX_ITEM_ID)

    private val _state = MutableStateFlow(BirthDeathUiState())
    val state: StateFlow<BirthDeathUiState> = _state.asStateFlow()

    private var statusJob: Job? = null

    init {
        // A ViewModel recreated after process death resumes following its already-queued write
        // instead of showing a blank form that invites a duplicate entry.
        outboxItemId.value?.let(::observeOutboxItem)
        recomputeSubmitGate()
    }

    fun onEvent(event: BirthDeathEvent) {
        when (event) {
            is BirthDeathEvent.SelectMode -> onSelectMode(event.mode)
            is BirthDeathEvent.EditField -> onEditField(event.field, event.value)
            BirthDeathEvent.Submit -> submit()
            BirthDeathEvent.Back -> Unit // navigation — handled by the nav host.
        }
    }

    private fun onSelectMode(mode: BirthDeathMode) {
        if (_state.value.result.isCommitted) return
        // Switching mode makes this a different write; drop the draft key so the new event can
        // never inherit the other mode's identity.
        idempotencyKey.invalidate()
        _state.update { it.copy(mode = mode, result = CountsWriteResultUi(), validationMessage = null) }
        recomputeSubmitGate()
    }

    private fun onEditField(field: BirthDeathField, value: String) {
        // Already queued/synced: the entry is durable and must not be mutated underneath the
        // outbox row. Reopening a new draft is an explicit navigation, not a silent edit.
        if (_state.value.result.isCommitted) return
        // Correcting a terminally-rejected write makes it a NEW logical write (see
        // DraftIdempotencyKey): mint a fresh key so the outbox does not reject the corrected
        // payload under the old key.
        if (_state.value.result.isCorrectable) {
            idempotencyKey.invalidate()
            outboxItemId.value = null
            statusJob?.cancel()
            _state.update { it.copy(result = CountsWriteResultUi()) }
        }
        _state.update { current ->
            when (field) {
                BirthDeathField.TAG -> current.copy(tag = value)
                BirthDeathField.SECOND_TAG -> current.copy(secondTag = value)
                BirthDeathField.SPECIES -> current.copy(species = value)
                BirthDeathField.SEX -> current.copy(sex = value)
                BirthDeathField.BREED -> current.copy(breed = value)
                BirthDeathField.DOB -> current.copy(dob = value)
                BirthDeathField.ENTRY_DATE -> current.copy(entryDate = value)
                BirthDeathField.PARK_ID -> current.copy(parkId = value)
                BirthDeathField.SHED_ID -> current.copy(shedId = value)
                BirthDeathField.DAM_ID -> current.copy(damId = value)
                BirthDeathField.GOAT_ID -> current.copy(goatId = value)
                BirthDeathField.ROW_VERSION -> current.copy(rowVersion = value.filter(Char::isDigit))
                BirthDeathField.REASON -> current.copy(reason = value)
            }
        }
        recomputeSubmitGate()
    }

    private fun submit() {
        val current = _state.value
        if (!current.canSubmit) return
        // One stable key for this draft, reused verbatim on every retry the sync engine makes.
        val key = idempotencyKey.current()
        val evidence = listOf(
            CountsEvidenceRefDto(
                evidenceId = key,
                description = "Recorded on the operator app",
            ),
        )
        viewModelScope.launch {
            val result = when (current.mode) {
                BirthDeathMode.BIRTH -> enqueueBirth(current, key, evidence)
                BirthDeathMode.DEATH -> enqueueDeath(current, key, evidence)
            }
            when (result) {
                is AppResult.Ok -> {
                    outboxItemId.value = result.value
                    observeOutboxItem(result.value)
                    analytics.track(
                        if (current.mode == BirthDeathMode.BIRTH) {
                            AnalyticsEvents.COUNTS_BIRTH_SUBMITTED
                        } else {
                            AnalyticsEvents.COUNTS_DEATH_SUBMITTED
                        },
                    )
                }
                is AppResult.Err -> onEnqueueFailed(current.mode, result)
            }
        }
    }

    private suspend fun enqueueBirth(
        current: BirthDeathUiState,
        key: String,
        evidence: List<CountsEvidenceRefDto>,
    ): AppResult<String> = syncRepository.enqueueCountsBirth(
        // The newborn's own tag partitions ordering: two writes about the same animal drain
        // strictly oldest-first, different animals drain concurrently.
        groupKey = current.tag.trim(),
        idempotencyKey = key,
        request = CountsBirthEventRequestDto(
            animalIdentifier1 = current.tag.trim(),
            animalIdentifier2 = current.secondTag.trim().ifBlank { null },
            species = current.species,
            parkId = current.parkId.trim().ifBlank { null },
            shedId = current.shedId.trim().ifBlank { null },
            breed = current.breed.trim().ifBlank { null },
            sex = current.sex,
            dob = current.dob.trim(),
            entryDate = current.entryDate.trim(),
            damId = current.damId.trim().ifBlank { null },
            evidenceRefs = evidence,
        ),
    )

    private suspend fun enqueueDeath(
        current: BirthDeathUiState,
        key: String,
        evidence: List<CountsEvidenceRefDto>,
    ): AppResult<String> = syncRepository.enqueueCountsDeath(
        groupKey = current.goatId.trim(),
        idempotencyKey = key,
        request = CountsDeathEventRequestDto(
            goatId = current.goatId.trim(),
            // lifecycle_status / exit_reason are left at the DTO's guardrail constants on
            // purpose — the dead+died pairing is not an operator choice.
            reason = current.reason.trim(),
            evidenceRefs = evidence,
            rowVersion = current.rowVersion.toIntOrNull() ?: 0,
        ),
    )

    private fun onEnqueueFailed(mode: BirthDeathMode, error: AppResult.Err) {
        val kind = if (mode == BirthDeathMode.BIRTH) "birth" else "death"
        error.cause?.let { crashReporter.recordException(it, "counts $kind enqueue failed") }
        analytics.track(
            AnalyticsEvents.COUNTS_WRITE_FAILURE,
            mapOf(
                AnalyticsEvents.Params.KIND to kind,
                AnalyticsEvents.Params.REASON to error.message,
            ),
        )
        _state.update {
            it.copy(
                result = CountsWriteResultUi(
                    status = sg.mesha.goatos.feature.counts.CountsWriteStatus.FAILED,
                    message = error.message,
                ),
            )
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
                    _state.update {
                        it.copy(result = item.toWriteResult(QUEUED_MESSAGE, SYNCED_MESSAGE))
                    }
                    recomputeSubmitGate()
                }
        }
    }

    /**
     * Submit is enabled only when every REQUIRED field for the active mode is present and
     * well-formed. Nothing here invents a value for a missing field.
     */
    private fun recomputeSubmitGate() {
        _state.update { current ->
            if (current.result.isCommitted) {
                return@update current.copy(canSubmit = false, validationMessage = null)
            }
            val missing = when (current.mode) {
                BirthDeathMode.BIRTH -> birthValidation(current)
                BirthDeathMode.DEATH -> deathValidation(current)
            }
            current.copy(canSubmit = missing == null, validationMessage = missing)
        }
    }

    private fun birthValidation(state: BirthDeathUiState): String? = when {
        state.tag.isBlank() -> "Enter the newborn's identifier."
        !isIsoDate(state.dob) -> "Enter the date of birth as YYYY-MM-DD."
        !isIsoDate(state.entryDate) -> "Enter the entry date as YYYY-MM-DD."
        // Mirrors the backend rule so the operator sees it before the round trip; the server
        // still enforces it independently.
        state.dob > state.entryDate -> "Date of birth cannot be after the entry date."
        // Park and shed are backend-owned placement values. The backend requires both as UUIDs
        // when present (or both null). Do not send a non-UUID value.
        // TODO(counts-followup): replace free-text park/shed + manual UUID/row_version with backend-owned
        // selectors and tag/scan lookup (PR#11 review follow-up #2/#3).
        !state.parkId.isBlank() && !isValidUuid(state.parkId) -> "Selector-backed park/shed and animal lookup not yet available — coming soon"
        !state.shedId.isBlank() && !isValidUuid(state.shedId) -> "Selector-backed park/shed and animal lookup not yet available — coming soon"
        else -> null
    }

    private fun deathValidation(state: BirthDeathUiState): String? = when {
        state.goatId.isBlank() -> "Enter the animal that died."
        // The goatId is a backend-owned identifier that must be a valid UUID. Operators cannot
        // know or guess valid UUIDs.
        // TODO(counts-followup): replace free-text park/shed + manual UUID/row_version with backend-owned
        // selectors and tag/scan lookup (PR#11 review follow-up #2/#3).
        !isValidUuid(state.goatId) -> "Selector-backed park/shed and animal lookup not yet available — coming soon"
        // A blank/zero row version is REJECTED, never defaulted to 1 — defaulting would silently
        // overwrite whatever concurrent edit the guard exists to catch.
        (state.rowVersion.toIntOrNull() ?: 0) < 1 -> "Enter the animal's current record version."
        state.reason.trim().length < 3 -> "Describe what happened (at least 3 characters)."
        state.reason.trim().length > 500 -> "Keep the account under 500 characters."
        else -> null
    }

    /** Shape-only check; the backend parses and validates the calendar date itself. */
    private fun isIsoDate(value: String): Boolean = ISO_DATE.matches(value.trim())

    /** Check if a string is a valid UUID (standard 8-4-4-4-12 hex format). */
    private fun isValidUuid(value: String): Boolean = UUID_FORMAT.matches(value.trim())

    private companion object {
        const val KEY_IDEMPOTENCY = "countsBirthDeath.idempotencyKey"
        const val KEY_OUTBOX_ITEM_ID = "countsBirthDeath.outboxItemId"
        const val QUEUED_MESSAGE = "Saved on this phone. It will sync automatically."
        const val SYNCED_MESSAGE = "Recorded."
        val ISO_DATE = Regex("""\d{4}-\d{2}-\d{2}""")
        // UUID format: 8-4-4-4-12 hex digits with hyphens (standard RFC 4122).
        val UUID_FORMAT = Regex("""[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}""", RegexOption.IGNORE_CASE)
    }
}
