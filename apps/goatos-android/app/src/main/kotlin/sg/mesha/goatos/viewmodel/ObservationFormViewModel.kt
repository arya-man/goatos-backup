package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.mapNotNull
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.launch
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CountsRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.HealthDiagnosisProposalResponseDto
import sg.mesha.goatos.core.network.dto.HealthObservationContextDto
import sg.mesha.goatos.core.network.dto.HealthObservationFindingsDto
import sg.mesha.goatos.feature.health.ObservationFormEvent
import sg.mesha.goatos.feature.health.ObservationFormState
import sg.mesha.goatos.feature.health.ObservationScreenState
import sg.mesha.goatos.feature.health.canSubmit

/**
 * Drives the observation form.
 *
 * The form-to-wire mapping lives here rather than in the feature module because
 * feature modules do not depend on core-network. Nothing in that mapping invents
 * a value: a finding the engine acts on must be one a person actually ticked.
 */
@HiltViewModel
class ObservationFormViewModel @Inject constructor(
    savedStateHandle: SavedStateHandle,
    private val countsRepository: CountsRepository,
    private val syncRepository: SyncRepository,
    private val analytics: AnalyticsPort,
) : ViewModel() {

    private val goatId: String = savedStateHandle.get<String>("observationGoatId").orEmpty()

    /**
     * Decodes the stored sync response. Lenient about unknown keys on purpose: this
     * reads only the run id, and a server that adds a field must not stop the
     * manager from reaching their assessment.
     */
    private val wireJson = Json { ignoreUnknownKeys = true }

    /**
     * Minted once per form, held across process death, and rotated only after a
     * confirmed enqueue. That is what makes a retry safe: the same observation
     * cannot become two.
     */
    private val idempotencyKey =
        DraftIdempotencyKey(savedStateHandle, "observation.idempotencyKey", "health-observation")

    private val _state = MutableStateFlow(ObservationScreenState())
    val state: StateFlow<ObservationScreenState> = _state.asStateFlow()

    init {
        loadAnimal()
    }

    /**
     * The assessment, once the queued observation has actually reached the server.
     *
     * A one-shot channel rather than screen state: it is an EVENT ("this arrived
     * now"), and replaying it from state would re-open the assessment every time
     * the screen recomposed or the user came back to it.
     */
    private val _assessed = Channel<String>(Channel.BUFFERED)
    val assessed: Flow<String> = _assessed.receiveAsFlow()

    fun onEvent(event: ObservationFormEvent) {
        when (event) {
            is ObservationFormEvent.UpdateForm -> _state.value = _state.value.copy(form = event.form, message = null)
            ObservationFormEvent.Submit -> submit()
            is ObservationFormEvent.Assessed -> Unit
            ObservationFormEvent.Back -> Unit
        }
    }

    /**
     * Watches the queued write until the server answers.
     *
     * The assessment cannot be shown at the moment of submit -- the write goes to
     * the durable outbox and may sit there for hours out of a shed with no signal.
     * So the row is observed, and the run id is read off the response the sync
     * engine stored when it finally went through.
     *
     * A row that is still being retried emits nothing: the operator's work is safe in
     * the queue, and opening an assessment that does not exist would be worse than
     * waiting.
     *
     * A row the queue has GIVEN UP ON is a different thing entirely, and used to be
     * treated as the same. `isActive` is false once the write is a business rejection
     * (`conflict`) or has burned its retry budget -- in both cases no drain will ever
     * claim it again. Waiting on that silently left "Recorded. The assessment will
     * appear once this syncs." on screen forever, in the affirmative, for a check the
     * server had already refused. An operator in a shed cannot tell that apart from a
     * slow sync, so they stand there waiting on an assessment that is never coming.
     * Surfacing the server's own refusal is the honest answer.
     */
    private fun awaitAssessment(outboxItemId: String) {
        viewModelScope.launch {
            syncRepository.observeItem(outboxItemId)
                .mapNotNull { item ->
                    if (item == null) return@mapNotNull null
                    if (item.status == SyncItemStatus.SUCCEEDED) {
                        val result = item.resultJson ?: return@mapNotNull null
                        // exception:exempt navigation trigger; an undecodable result simply does
                        // not navigate, leaving the manager on the form with the submission
                        // still queued.
                        return@mapNotNull runCatching {
                            wireJson.decodeFromString<HealthDiagnosisProposalResponseDto>(result)
                        }.getOrNull()?.diagnosisRunId?.takeIf { it.isNotBlank() }
                            ?.let(AwaitOutcome::Assessed)
                    }
                    // Not succeeded and never going to be retried.
                    if (!item.isActive) AwaitOutcome.Refused(item.lastError) else null
                }
                .first()
                .let { outcome ->
                    when (outcome) {
                        is AwaitOutcome.Assessed -> {
                            _state.value = _state.value.copy(message = null)
                            _assessed.send(outcome.runId)
                        }
                        is AwaitOutcome.Refused -> {
                            analytics.track(
                                AnalyticsEvents.HEALTH_WRITE_FAILURE,
                                mapOf(
                                    AnalyticsEvents.Params.KIND to "observation_refused",
                                    AnalyticsEvents.Params.REASON to (outcome.reason ?: "unknown"),
                                ),
                            )
                            _state.value = _state.value.copy(
                                submitting = false,
                                // The server's own words when it gave them: it is the only side
                                // that knows WHY this animal was refused, and a generic sentence
                                // would send the manager back to re-tick the same form.
                                message = outcome.reason?.takeIf { it.isNotBlank() }
                                    ?: "This check could not be assessed. Try again.",
                            )
                        }
                    }
                }
        }
    }

    /** What the queued observation finally resolved to: an assessment, or a refusal. */
    private sealed interface AwaitOutcome {
        data class Assessed(val runId: String) : AwaitOutcome
        data class Refused(val reason: String?) : AwaitOutcome
    }

    /**
     * Reads the animal so the form can show who it is and ask the right
     * sex-specific questions.
     *
     * The SEX comes from GoatOS, never from the operator. It decides which
     * questions are asked, and the server independently reads it again when the
     * register runs — this copy is for the screen only.
     */
    private fun loadAnimal() {
        if (goatId.isBlank()) return
        viewModelScope.launch {
            countsRepository.lookupAnimals(query = goatId)
                .onSuccess { rows ->
                    val animal = rows.firstOrNull { it.goatId == goatId } ?: return@onSuccess
                    _state.value = _state.value.copy(
                        goatDisplayId = animal.displayId,
                        goatTag = animal.animalIdentifier1,
                        form = _state.value.form.copy(sex = animal.sex),
                    )
                }
                .onFailure { error ->
                    analytics.track(
                        AnalyticsEvents.HEALTH_READ_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.KIND to "observation_animal",
                            AnalyticsEvents.Params.REASON to (error.message ?: "unknown"),
                        ),
                    )
                    _state.value = _state.value.copy(
                        message = "Could not load this animal. Check the connection and try again.",
                    )
                }
        }
    }

    private fun submit() {
        val current = _state.value
        if (!current.form.canSubmit() || current.submitting || goatId.isBlank()) return
        _state.value = current.copy(submitting = true, message = null)

        viewModelScope.launch {
            val result = syncRepository.enqueueHealthObservationSubmit(
                goatId = goatId,
                findings = current.form.toFindingsDto(),
                context = HealthObservationContextDto(),
                idempotencyKey = idempotencyKey.current(),
                goatDisplayId = current.goatDisplayId,
            )
            when (result) {
                is AppResult.Ok -> {
                    analytics.track(AnalyticsEvents.HEALTH_OBSERVATION_SUBMITTED, emptyMap())
                    awaitAssessment(result.value)
                    _state.value = _state.value.copy(
                        submitting = false,
                        // Deliberately does NOT promise a diagnosis. The assessment
                        // comes from the server and may arrive minutes later out of
                        // a shed with no signal; claiming one here would be a lie
                        // the operator acts on.
                        message = "Recorded. The assessment will appear once this syncs.",
                    )
                }
                is AppResult.Err -> {
                    analytics.track(
                        AnalyticsEvents.HEALTH_WRITE_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.KIND to "observation",
                            AnalyticsEvents.Params.REASON to result.message,
                        ),
                    )
                    _state.value = _state.value.copy(
                        submitting = false,
                        message = "Could not save this check. Try again.",
                    )
                }
            }
        }
    }
}

/**
 * Maps the form to the wire shape.
 *
 * Every value is sent exactly as recorded — nothing defaulted, rounded or
 * inferred. A client-invented finding would be indistinguishable from something
 * a person saw, and the engine would act on it.
 */
internal fun ObservationFormState.toFindingsDto(): HealthObservationFindingsDto =
    HealthObservationFindingsDto(
        temp = temp.toDoubleOrNull(),
        eating = eating.toList().ifEmpty { null },
        activity = activity.ifBlank { null },
        breathing = breathing.toList().ifEmpty { null },
        nasal = nasal,
        leftStomach = leftStomach.toList().ifEmpty { null },
        frothyMouth = frothyMouth,
        rumenMovement = rumenMovement.ifBlank { null },
        diarrhea = diarrhea,
        skinTent = skinTent.ifBlank { null },
        cmt = cmt.ifBlank { null },
        lactation = lactation.ifBlank { null },
        udder = udder.ifBlank { null },
        vulva = vulva.ifBlank { null },
        famacha = famacha.toIntOrNull(),
        yellow = yellow,
        straining = straining.ifBlank { null },
        redUrine = redUrine,
        bodyEdema = bodyEdema,
        competition = competition,
        stomachInside = stomachInside,
        mouth = mouth.ifBlank { null },
        eyes = eyes.toList().ifEmpty { null },
        lockedJaw = lockedJaw,
        neuro = neuro.toList().ifEmpty { null },
        // "none" is the operator saying there is no rash, which is not a finding
        // the engine reads; it is the absence of one.
        rashCharacter = rashCharacter.takeIf { it.isNotBlank() && it != "none" },
        hairloss = hairloss,
        leg = leg.ifBlank { null },
        lumps = lumps.ifBlank { null },
        wounds = wounds.toList().ifEmpty { null },
        flystrike = flystrike,
        eartagFlystrike = eartagFlystrike,
        eartagWound = eartagWound,
        ticks = ticks,
    )
