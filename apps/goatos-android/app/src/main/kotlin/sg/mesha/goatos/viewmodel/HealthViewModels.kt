package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.paging.PagingData
import androidx.paging.cachedIn
import androidx.paging.map
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.HealthFilters
import sg.mesha.goatos.core.data.HealthRepository
import sg.mesha.goatos.core.data.CountsRepository
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.PendingHealthCaseOpen
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.network.dto.HealthSummaryDto
import sg.mesha.goatos.core.network.dto.HealthTreatmentStepDto
import sg.mesha.goatos.core.network.dto.HealthWorkItemDto
import sg.mesha.goatos.core.network.dto.GoatSearchItemDto
import sg.mesha.goatos.feature.health.AddHealthCaseEvent
import sg.mesha.goatos.feature.health.AddHealthCaseUiState
import sg.mesha.goatos.feature.health.HealthGoatUi
import sg.mesha.goatos.feature.health.HealthDetailUiState
import sg.mesha.goatos.feature.health.HealthFilterUi
import sg.mesha.goatos.feature.health.HealthListEvent
import sg.mesha.goatos.feature.health.HealthListUiState
import sg.mesha.goatos.feature.health.HealthPendingCaseUi
import sg.mesha.goatos.feature.health.HealthStepUi
import sg.mesha.goatos.feature.health.HealthSummaryUi
import sg.mesha.goatos.feature.health.HealthWorkItemUi
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import javax.inject.Inject

abstract class HealthListViewModel(
    private val ageBand: String,
    private val repo: HealthRepository,
    syncRepository: SyncRepository,
) : ViewModel() {
    private val filters = MutableStateFlow(HealthFilters(ageBand = ageBand, date = today()))
    private val error = MutableStateFlow<String?>(null)
    private val refreshing = MutableStateFlow(false)

    @OptIn(ExperimentalCoroutinesApi::class)
    val rows: Flow<PagingData<HealthWorkItemUi>> = filters.flatMapLatest(repo::workItems)
        .map { page -> page.map(HealthWorkItemDto::toUi) }
        .cachedIn(viewModelScope)

    @OptIn(ExperimentalCoroutinesApi::class)
    private val meta = filters.flatMapLatest(repo::observePageMeta)
    private val pendingCases = syncRepository.observePendingHealthCaseOpens()

    val state: StateFlow<HealthListUiState> = combine(
        filters,
        meta,
        error,
        refreshing,
        pendingCases,
    ) { selected, page, failure, loading, pending ->
        HealthListUiState(
            pageLabel = if (ageBand == "kid") "Kids" else "Adults",
            dateIso = selected.date,
            dateLabel = dateLabel(selected.date),
            isToday = selected.date == today(),
            status = selected.status,
            diseaseKey = selected.diseaseKey,
            parkId = selected.parkId,
            shedId = selected.shedId,
            session = selected.session,
            summary = (page?.summary ?: HealthSummaryDto()).toUi(),
            diseases = page?.filterOptions?.diseases.orEmpty().map { HealthFilterUi(it.key, it.label) },
            parks = page?.filterOptions?.parks.orEmpty().map { HealthFilterUi(it.key, it.label) },
            sheds = page?.filterOptions?.sheds.orEmpty().map { HealthFilterUi(it.key, it.label) },
            pendingCases = visiblePendingHealthCases(selected, pending),
            refreshing = loading,
            error = failure,
        )
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        HealthListUiState(
            pageLabel = if (ageBand == "kid") "Kids" else "Adults",
            dateIso = today(),
            dateLabel = dateLabel(today()),
        ),
    )

    fun onEvent(event: HealthListEvent) {
        when (event) {
            is HealthListEvent.SelectDate -> filters.value = filters.value.copy(date = event.value)
            is HealthListEvent.SelectStatus -> filters.value = filters.value.copy(status = event.value)
            is HealthListEvent.SelectDisease -> filters.value = filters.value.copy(diseaseKey = event.value)
            is HealthListEvent.SelectPark -> filters.value = filters.value.copy(parkId = event.value, shedId = "")
            is HealthListEvent.SelectShed -> filters.value = filters.value.copy(shedId = event.value)
            is HealthListEvent.SelectSession -> filters.value = filters.value.copy(session = event.value)
            HealthListEvent.PrevDay -> shiftDate(-1)
            HealthListEvent.NextDay -> shiftDate(1)
            HealthListEvent.Today -> filters.value = filters.value.copy(date = today())
            HealthListEvent.Refresh -> error.value = null
            HealthListEvent.Back, HealthListEvent.AddNew, is HealthListEvent.OpenItem -> Unit
        }
    }

    fun onRowsLoading() { refreshing.value = true }
    fun onRowsLoaded() { refreshing.value = false }
    fun onLoadFailed(t: Throwable) {
        refreshing.value = false
        error.value = t.message ?: "Could not refresh Health actions"
    }

    private fun shiftDate(days: Long) {
        val next = runCatching { LocalDate.parse(filters.value.date).plusDays(days).toString() }.getOrElse { today() }
        filters.value = filters.value.copy(date = next)
    }

    private companion object {
        val zone: ZoneId = ZoneId.of("Asia/Kolkata")
        val labelFormat: DateTimeFormatter = DateTimeFormatter.ofPattern("EEE, d MMM yyyy")
        fun today(): String = LocalDate.now(zone).toString()
        fun dateLabel(value: String): String = runCatching { LocalDate.parse(value).format(labelFormat) }.getOrDefault(value)
    }
}

@HiltViewModel
class AdultHealthViewModel @Inject constructor(
    repo: HealthRepository,
    syncRepository: SyncRepository,
) : HealthListViewModel("adult", repo, syncRepository)

@HiltViewModel
class KidsHealthViewModel @Inject constructor(
    repo: HealthRepository,
    syncRepository: SyncRepository,
) : HealthListViewModel("kid", repo, syncRepository)

@HiltViewModel
class AddHealthCaseViewModel @Inject constructor(
    savedStateHandle: SavedStateHandle,
    private val healthRepository: HealthRepository,
    private val countsRepository: CountsRepository,
    private val syncRepository: SyncRepository,
) : ViewModel() {
    private val ageBand: String = savedStateHandle.get<String>("healthAgeBand")
        ?.takeIf { it == "adult" || it == "kid" } ?: "adult"
    private val date = LocalDate.now(ZoneId.of("Asia/Kolkata")).toString()
    private val idempotencyKey = DraftIdempotencyKey(savedStateHandle, "healthCase.idempotencyKey", "health-case")
    private val _state = MutableStateFlow(AddHealthCaseUiState(ageBand = ageBand, startDate = date))
    val state: StateFlow<AddHealthCaseUiState> = _state.asStateFlow()

    init {
        viewModelScope.launch {
            healthRepository.observePageMeta(HealthFilters(ageBand = ageBand, date = date)).collect { page ->
                val diseases = page?.filterOptions?.diseases.orEmpty().map { HealthFilterUi(it.key, it.label) }
                _state.value = _state.value.copy(diseases = diseases)
                recompute()
            }
        }
        viewModelScope.launch {
            healthRepository.refreshCaseOptions(ageBand, date)
                .onFailure { _state.value = _state.value.copy(message = "Could not refresh disease protocols. Cached options are still available.") }
        }
    }

    fun onEvent(event: AddHealthCaseEvent) {
        when (event) {
            is AddHealthCaseEvent.EditQuery -> {
                _state.value = _state.value.copy(animalQuery = event.value, lookupMessage = null)
                recompute()
            }
            AddHealthCaseEvent.Lookup -> lookup()
            is AddHealthCaseEvent.SelectGoat -> {
                _state.value = _state.value.copy(selectedGoat = _state.value.matches.firstOrNull { it.goatId == event.goatId })
                recompute()
            }
            is AddHealthCaseEvent.SelectDisease -> {
                _state.value = _state.value.copy(diseaseKey = event.diseaseKey)
                recompute()
            }
            is AddHealthCaseEvent.SelectStartDate -> {
                _state.value = _state.value.copy(startDate = event.date)
                recompute()
            }
            AddHealthCaseEvent.Submit -> submit()
            AddHealthCaseEvent.NavigationHandled -> _state.value = _state.value.copy(returnToList = false)
            AddHealthCaseEvent.Back -> Unit
        }
    }

    private fun lookup() {
        val query = _state.value.animalQuery.trim()
        if (query.isEmpty() || _state.value.lookingUp) return
        _state.value = _state.value.copy(lookingUp = true, lookupMessage = null)
        viewModelScope.launch {
            countsRepository.lookupAnimals(query = query)
                .onSuccess { rows ->
                    val eligible = rows.filter { healthGoatMatchesAgeBand(it, ageBand) }
                    _state.value = _state.value.copy(
                        lookingUp = false,
                        // One row per goat. The search's display joins fan out when a goat holds
                        // more than one active identifier of the same type -- goat_identifiers is
                        // unique on (tenant_id, normalized_value), NOT on
                        // (tenant_id, goat_id, identifier_type) -- so the same goat can arrive
                        // twice. Two rows for one animal is wrong on screen and duplicates the
                        // lazy-list key, which crashes Compose with "Key was already used".
                        matches = eligible.map(GoatSearchItemDto::toHealthGoatUi).distinctBy { it.goatId },
                        lookupMessage = when {
                            rows.isEmpty() -> "No live goat matched that RFID or tag."
                            eligible.isEmpty() && ageBand == "adult" -> "That goat belongs in Kids Health."
                            eligible.isEmpty() -> "That goat belongs in Adults Health."
                            else -> null
                        },
                    )
                }
                .onFailure {
                    _state.value = _state.value.copy(lookingUp = false, lookupMessage = "Could not search goats. Check the connection and retry.")
                }
            recompute()
        }
    }

    private fun submit() {
        val current = _state.value
        val goat = current.selectedGoat ?: return
        if (!current.canSubmit || current.submitting) return
        _state.value = current.copy(submitting = true, message = null)
        viewModelScope.launch {
            when (val result = syncRepository.enqueueHealthCaseOpen(
                goatId = goat.goatId,
                diseaseKey = current.diseaseKey,
                ageBand = ageBand,
                startDate = current.startDate,
                idempotencyKey = idempotencyKey.current(),
                goatDisplayId = goat.displayId,
                diseaseName = current.diseases.firstOrNull { it.key == current.diseaseKey }?.label
                    ?: current.diseaseKey.replace('_', ' ').replaceFirstChar { it.uppercase() },
            )) {
                is AppResult.Ok -> _state.value = _state.value.copy(
                    submitting = false,
                    returnToList = true,
                    message = "Sick goat recorded. The treatment plan is queued and will sync automatically.",
                )
                is AppResult.Err -> _state.value = _state.value.copy(submitting = false, message = result.message)
            }
        }
    }

    private fun recompute() {
        val value = _state.value
        _state.value = value.copy(
            canSubmit = value.selectedGoat != null && value.diseaseKey.isNotBlank() &&
                runCatching { LocalDate.parse(value.startDate) }.isSuccess,
        )
    }
}

@HiltViewModel
class HealthDetailViewModel @Inject constructor(
    savedStateHandle: SavedStateHandle,
    private val repo: HealthRepository,
    private val syncRepository: SyncRepository,
) : ViewModel() {
    private val sessionId: String = checkNotNull(savedStateHandle["healthSessionId"])
    private val submitting = MutableStateFlow(false)
    private val message = MutableStateFlow<String?>(null)

    val state: StateFlow<HealthDetailUiState> = combine(
        repo.observeDetail(sessionId), submitting, message,
    ) { detail, saving, notice ->
        if (detail == null) HealthDetailUiState(loading = true, submitting = saving, message = notice)
        else HealthDetailUiState(
            loading = false,
            goatDisplayId = detail.goatDisplayId,
            diseaseName = detail.diseaseName,
            dayLabel = "Day ${detail.dayNo} of ${detail.durationDays} · ${detail.session.replaceFirstChar { it.uppercase() }}",
            locationLabel = listOf(detail.parkLabel, detail.shedLabel).filter(String::isNotBlank).joinToString(" · "),
            status = detail.status,
            steps = detail.steps.map(HealthTreatmentStepDto::toUi),
            submitting = saving,
            message = notice,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), HealthDetailUiState())

    init { refresh() }

    fun refresh() = viewModelScope.launch { repo.refreshDetail(sessionId) }

    fun complete() = viewModelScope.launch {
        if (submitting.value) return@launch
        submitting.value = true
        when (val result = syncRepository.enqueueHealthTreatmentComplete(
            healthSessionId = sessionId,
            idempotencyKey = "health-complete:$sessionId",
        )) {
            is AppResult.Ok -> {
                repo.markCompleted(sessionId)
                message.value = "Saved offline. Sync will finish automatically."
            }
            is AppResult.Err -> message.value = result.message
        }
        submitting.value = false
    }
}

private fun HealthWorkItemDto.toUi() = HealthWorkItemUi(
    healthSessionId = healthSessionId,
    goatDisplayId = goatDisplayId.ifBlank { goatId },
    diseaseName = diseaseName,
    dayLabel = "Day $dayNo of $durationDays",
    dayNo = dayNo,
    durationDays = durationDays,
    locationLabel = listOf(parkLabel, shedLabel).filter(String::isNotBlank).joinToString(" · "),
    sessionLabel = session.replaceFirstChar { it.uppercase() },
    medicineLabel = if (medicationCount == 0) "$stepCount actions" else "$medicationCount medicines · $stepCount actions",
    status = status,
    hasCriticalStep = hasCriticalStep,
)

private fun GoatSearchItemDto.toHealthGoatUi() = HealthGoatUi(
    goatId = goatId,
    displayId = displayId.ifBlank { animalIdentifier1 },
    tag = animalIdentifier1,
    locationLabel = locationPath.display,
    sex = sex,
)

internal fun healthGoatMatchesAgeBand(goat: GoatSearchItemDto, requiredAgeBand: String): Boolean =
    goat.ageBand?.trim()?.lowercase() == requiredAgeBand

private fun HealthSummaryDto.toUi() = HealthSummaryUi(total, due, scheduled, completed, held)

/**
 * Pending reports have only goat/disease/date grain. Scope or canonical-session status filters
 * therefore hide them rather than pretending the local row has a park, shed, session, or action
 * state assigned by the backend.
 */
internal fun visiblePendingHealthCases(
    filters: HealthFilters,
    pending: List<PendingHealthCaseOpen>,
): List<HealthPendingCaseUi> {
    if (filters.status.isNotBlank() || filters.parkId.isNotBlank() ||
        filters.shedId.isNotBlank() || filters.session.isNotBlank()
    ) return emptyList()
    return pending.asSequence()
        .filter { it.ageBand == filters.ageBand && it.startDate == filters.date }
        .filter { filters.diseaseKey.isBlank() || it.diseaseKey == filters.diseaseKey }
        .map { item ->
            HealthPendingCaseUi(
                outboxItemId = item.outboxItemId,
                goatDisplayId = item.goatDisplayId,
                diseaseName = item.diseaseName,
                startDate = item.startDate,
                statusLabel = when (item.syncStatus) {
                    SyncItemStatus.QUEUED -> "Waiting to sync"
                    SyncItemStatus.IN_FLIGHT -> "Syncing"
                    SyncItemStatus.FAILED -> "Retrying sync"
                    SyncItemStatus.SUCCEEDED -> "Synced"
                },
            )
        }
        .toList()
}

private fun HealthTreatmentStepDto.toUi(): HealthStepUi {
    val title = medicineName ?: when (recordType) {
        "critical_action" -> "Critical action"
        else -> "Care instruction"
    }
    val details = listOfNotNull(
        dosageText?.takeIf(String::isNotBlank),
        dosageDenominator?.takeIf(String::isNotBlank),
        medicineRoute?.takeIf(String::isNotBlank),
        instruction?.takeIf(String::isNotBlank),
    ).joinToString(" · ")
    return HealthStepUi(stepId.ifBlank { "$dayNo-$session-$seq" }, title, details, criticalActionType != null)
}
