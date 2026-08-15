package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.HealthRepository
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.HealthConfirmableProblemDto
import sg.mesha.goatos.core.network.dto.HealthDiagnosisProposalResponseDto
import sg.mesha.goatos.feature.health.DiagnosisProposalEvent
import sg.mesha.goatos.feature.health.DiagnosisProposalState
import sg.mesha.goatos.feature.health.HousingDirective
import sg.mesha.goatos.feature.health.ProposedProblem
import sg.mesha.goatos.feature.health.toggleSelection

/**
 * Drives the assessment screen.
 *
 * Room is the source of truth (docs/decisions/android-offline-first.md): the cached
 * assessment renders immediately and the refresh runs behind it, because this is
 * read in a shed where a request may never complete.
 *
 * The wire-to-model mapping lives here rather than in the feature module, which
 * does not depend on core-network.
 */
@HiltViewModel
class DiagnosisProposalViewModel @Inject constructor(
    savedStateHandle: SavedStateHandle,
    private val healthRepository: HealthRepository,
    private val syncRepository: SyncRepository,
    private val analytics: AnalyticsPort,
) : ViewModel() {

    private val diagnosisRunId: String = savedStateHandle.get<String>("diagnosisRunId").orEmpty()

    /**
     * Minted once and held across process death, so a decision whose response is
     * lost replays as the SAME decision rather than being cast twice.
     */
    private val idempotencyKey =
        DraftIdempotencyKey(savedStateHandle, "diagnosis.confirm.idempotencyKey", "health-diagnosis-confirm")

    private val _state = MutableStateFlow(DiagnosisProposalState(loading = true))
    val state: StateFlow<DiagnosisProposalState> = _state.asStateFlow()

    init {
        observeCached()
        refresh()
    }

    fun onEvent(event: DiagnosisProposalEvent) {
        when (event) {
            DiagnosisProposalEvent.Refresh -> refresh()
            is DiagnosisProposalEvent.ToggleProblem -> _state.value = _state.value.copy(
                selected = toggleSelection(_state.value.selected, event.id),
                message = null,
            )
            DiagnosisProposalEvent.Send -> send()
            DiagnosisProposalEvent.Back -> Unit
        }
    }

    /**
     * Renders whatever Room already holds, and keeps rendering it as the row
     * changes — including after a confirmation is projected back.
     *
     * The user's own ticks are preserved across a re-emit: losing them because a
     * background refresh landed mid-decision would be maddening in a shed.
     */
    private fun observeCached() {
        if (diagnosisRunId.isBlank()) return
        viewModelScope.launch {
            healthRepository.observeDiagnosisRun(diagnosisRunId).collect { cached ->
                if (cached == null) return@collect
                val current = _state.value
                _state.value = cached.proposal.toProposalState().copy(
                    // The animal's name comes from the CACHED RUN, which the server
                    // filled. It used to be carried over from the previous state --
                    // which starts blank and nothing ever set -- so the assessment
                    // header never named the animal on any device.
                    goatDisplayId = cached.goatDisplayId,
                    refreshing = current.refreshing,
                    sending = current.sending,
                    selected = current.selected,
                    message = current.message,
                )
            }
        }
    }

    private fun refresh() {
        if (diagnosisRunId.isBlank() || _state.value.refreshing) return
        _state.value = _state.value.copy(refreshing = true)
        viewModelScope.launch {
            healthRepository.refreshDiagnosisRun(diagnosisRunId)
                .onFailure { error ->
                    analytics.track(
                        AnalyticsEvents.HEALTH_READ_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.KIND to "diagnosis_run",
                            AnalyticsEvents.Params.REASON to (error.message ?: "unknown"),
                        ),
                    )
                }
            // The cached copy stays on screen either way; a failed refresh must never
            // replace a readable assessment with an error.
            _state.value = _state.value.copy(refreshing = false, loading = false)
        }
    }

    private fun send() {
        val current = _state.value
        if (!current.canSend || diagnosisRunId.isBlank()) return
        _state.value = current.copy(sending = true, message = null)

        viewModelScope.launch {
            val result = syncRepository.enqueueHealthDiagnosisConfirm(
                diagnosisRunId = diagnosisRunId,
                // Order is not meaningful to the backend, but a stable order keeps the
                // queued payload identical across a retry of the same decision.
                confirmedProblems = current.selected.sorted(),
                idempotencyKey = idempotencyKey.current(),
            )
            when (result) {
                is AppResult.Ok -> {
                    analytics.track(
                        AnalyticsEvents.HEALTH_DIAGNOSIS_CONFIRMED,
                        mapOf(AnalyticsEvents.Params.COUNT to current.selected.size.toString()),
                    )
                    _state.value = _state.value.copy(
                        sending = false,
                        // Does NOT claim a course has opened. That happens on the server and
                        // may be minutes away; promising it here would be a lie the operator acts on.
                        message = "Decision recorded. Treatment starts once this syncs.",
                    )
                }
                is AppResult.Err -> {
                    analytics.track(
                        AnalyticsEvents.HEALTH_WRITE_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.KIND to "diagnosis_confirm",
                            AnalyticsEvents.Params.REASON to result.message,
                        ),
                    )
                    _state.value = _state.value.copy(
                        sending = false,
                        message = "Could not record the decision. Try again.",
                    )
                }
            }
        }
    }
}

/**
 * Maps the wire proposal onto what the screen shows.
 *
 * Every list is taken as the backend ordered it. The ranking is severity first and
 * confidence second, and that is the backend's to own — re-sorting here would put a
 * mild certainty above a serious maybe.
 */
internal fun HealthDiagnosisProposalResponseDto.toProposalState(): DiagnosisProposalState {
    val byId = confirmable.associateBy { it.id }
    return DiagnosisProposalState(
        loading = false,
        status = status,
        emergencies = proposal.emergencies,
        unexplained = proposal.unexplained,
        problems = proposal.problems.map { id -> problemFor(id, byId[id]) },
        fieldActions = proposal.fieldActions,
        rechecks = proposal.rechecks,
        covered = proposal.covered,
        housing = HousingDirective(
            acuity = housingAcuityLabel(proposal.housing.acuity),
            containment = housingContainmentLabel(proposal.housing.containment),
            lowCompetition = proposal.housing.lowCompetition,
        ),
        notes = proposal.hints,
        mayConfirm = mayConfirm,
    )
}

/**
 * One problem row.
 *
 * A problem with no entry in `confirmable` is shown and NOT confirmable. That
 * combination is real rather than defensive: a decided run carries an empty
 * confirmable list, so its problems still render while nothing is offered.
 */
private fun problemFor(id: String, confirmable: HealthConfirmableProblemDto?): ProposedProblem =
    ProposedProblem(
        id = id,
        label = diagnosisLabel(id),
        confidence = confidenceLabel(confirmable?.tier.orEmpty()),
        canConfirm = confirmable?.sopAvailable == true,
        blockedReason = confirmable?.blockedReason.orEmpty(),
    )

/**
 * Turns a register id into farm words.
 *
 * The register's ids are config tokens (`pregnancy_toxemia`, `fly_strike`) and the
 * copy firewall keeps those off an operator's screen. A generic
 * underscores-to-spaces pass with title case is used rather than a hand-written
 * table, so a register that learns a new diagnosis is readable the same day
 * instead of rendering a raw token until someone adds a mapping.
 */
internal fun diagnosisLabel(id: String): String =
    id.split('_', '-')
        .filter { it.isNotBlank() }
        .joinToString(" ") { word -> word.replaceFirstChar { it.uppercase() } }

/** CONFIRMED / PROBABLE / POSSIBLE as a person would say them. */
internal fun confidenceLabel(tier: String): String = when (tier.lowercase()) {
    "confirmed" -> "Confirmed"
    "probable" -> "Likely"
    "possible" -> "Possible"
    else -> "Possible"
}

/** How closely the animal needs watching. Blank when the register said nothing. */
internal fun housingAcuityLabel(acuity: String): String = when (acuity.lowercase()) {
    "icu" -> "Keep in the sick pen, checked morning and evening."
    "ward" -> "Keep in the sick pen, checked every morning."
    "field" -> "Can stay with its group."
    else -> ""
}

/** Whether it must be kept away from the others. */
internal fun housingContainmentLabel(containment: String): String = when (containment.lowercase()) {
    "quarantine" -> "Keep away from the other animals."
    "isolate" -> "Keep on its own."
    else -> ""
}
