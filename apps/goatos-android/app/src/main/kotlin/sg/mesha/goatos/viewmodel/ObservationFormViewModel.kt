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
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CountsRepository
import sg.mesha.goatos.core.data.HealthRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.HealthDiagnosisProposalResponseDto
import sg.mesha.goatos.core.network.dto.HealthObservationContextDto
import sg.mesha.goatos.core.network.dto.HealthObservationFindingsDto
import sg.mesha.goatos.feature.health.missing
import sg.mesha.goatos.feature.health.AuthoredQuestion
import sg.mesha.goatos.feature.health.AuthoredPage
import sg.mesha.goatos.feature.health.AuthoredOption
import sg.mesha.goatos.feature.health.AuthoredAnswers
import sg.mesha.goatos.feature.health.AuthoredForm
import sg.mesha.goatos.core.network.dto.HealthObservationFormDto
import sg.mesha.goatos.feature.health.ObservationFormEvent
import sg.mesha.goatos.feature.health.ObservationFormState
import sg.mesha.goatos.feature.health.ObservationScreenState
import sg.mesha.goatos.feature.health.canSubmit
import sg.mesha.goatos.feature.health.kidFormClassForStage

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
    private val healthRepository: HealthRepository,
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

    /**
     * The form the SERVER published for this animal's type, and the operator's answers to it.
     *
     * Held beside the screen state rather than inside it because they are a different thing: the
     * state describes the animal and the submit, these are the authored questions and what has
     * been ticked. A refresh replaces the form; the answers survive it, keyed by question id, so
     * re-fetching does not throw away a half-walked animal.
     */
    private val _authoredForm = MutableStateFlow(AuthoredForm())
    val authoredForm: StateFlow<AuthoredForm> = _authoredForm.asStateFlow()

    private val _answers = MutableStateFlow(AuthoredAnswers())
    val answers: StateFlow<AuthoredAnswers> = _answers.asStateFlow()

    fun onAnswers(next: AuthoredAnswers) {
        _answers.value = next
        _state.value = _state.value.copy(message = null)
    }

    init {
        loadAnimal()
        loadAuthoredForm()
    }

    /**
     * Fetches the questions this animal's type asks.
     *
     * A REFUSAL IS THE POINT, not an edge case: an animal whose stage no diagnosis type covers, or
     * a type whose questions nobody has written, is turned away here -- while the animal is still
     * in front of the operator -- instead of after the whole form is walked and submitted. The
     * server's own sentence is shown, because it names the stage or the type and where to fix it.
     */
    fun loadAuthoredForm() {
        if (goatId.isBlank()) return
        viewModelScope.launch {
            healthRepository.observationForm(goatId)
                .onSuccess { dto ->
                    _authoredForm.value = dto.toAuthoredForm()
                    _state.value = _state.value.copy(message = null)
                }
                .onFailure {
                    _authoredForm.value = AuthoredForm()
                    _state.value = _state.value.copy(
                        message = "Could not load this animal's questions. Pull to refresh once there is signal.",
                    )
                }
        }
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
                    // A kid's slice decides which rows the form asks -- the same
                    // fail-closed stage mapping the server applies. An unmapped
                    // kid stage renders the adult form and the server refuses the
                    // submit with its own farm-worded reason.
                    val kid = if (animal.ageBand.equals("kid", ignoreCase = true)) {
                        kidFormClassForStage(animal.managementStage)
                    } else {
                        null
                    }
                    _state.value = _state.value.copy(
                        goatDisplayId = animal.displayId,
                        goatTag = animal.animalIdentifier1,
                        form = _state.value.form.copy(
                            sex = animal.sex,
                            kidClass = kid?.first.orEmpty(),
                            kidStage = kid?.second.orEmpty(),
                        ),
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
        val authoredNow = _authoredForm.value
        val complete = if (authoredNow.isEmpty) {
            current.form.canSubmit()
        } else {
            // Every applicable question on every page. The screen gates each page's Next, so this
            // is the belt to that braces -- a submit reached any other way is still refused here.
            authoredNow.pages.all {
                it.missing(_answers.value, current.form.sex, current.form.kidStage).isEmpty()
            }
        }
        if (!complete || current.submitting || goatId.isBlank()) return
        _state.value = current.copy(submitting = true, message = null)

        viewModelScope.launch {
            // The AUTHORED answers when the server published a form, the typed ones otherwise.
            // Both are sent: `findings` keeps an older server able to read the submit, and the
            // presence of `answers` is what makes the server evaluate against the published
            // register rather than the compiled form.
            val authored = _authoredForm.value
            val result = syncRepository.enqueueHealthObservationSubmit(
                goatId = goatId,
                findings = current.form.toFindingsDto(),
                answers = if (authored.isEmpty) current.form.toAuthoredAnswersJson()
                else _answers.value.toWireJson(_authoredForm.value),
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
        // Kid rows travel only when the kid form asked them: the server rejects
        // a landing value on a weaning kid, so an adult or wrong-slice form must
        // send ABSENT, not blank.
        suckle = suckle.takeIf { isKid && it.isNotBlank() },
        responsiveness = responsiveness.takeIf { isKid && it.isNotBlank() },
        navel = navel.takeIf { isMilkKid && it.isNotBlank() },
        landing = landing.takeIf { landingApplies && it.isNotBlank() },
        milkIntake = milkIntake.toList().takeIf { milkIntakeApplies && it.isNotEmpty() },
        refusalsToday = refusalsToday.toIntOrNull().takeIf { isKid },
        session = session.toIntOrNull().takeIf { isKid },
    )

/**
 * Maps the current fixed Android form to the v1 authored-register answer layer.
 *
 * Unlike findings, normal answers are still answers here: required yes/no authored
 * questions need an explicit "no" so the backend can distinguish "normal" from
 * "not examined".
 */
internal fun ObservationFormState.toAuthoredAnswersJson(): JsonObject? {
    return buildJsonObject {
        fun putString(id: String, value: String?) {
            if (!value.isNullOrBlank()) put(id, JsonPrimitive(value))
        }
        fun putNumber(id: String, value: Number?) {
            if (value != null) put(id, JsonPrimitive(value))
        }
        fun putBool(id: String, value: Boolean?) {
            if (value != null) put(id, JsonPrimitive(if (value) "yes" else "no"))
        }
        fun putValues(id: String, values: Collection<String>) {
            if (values.isNotEmpty()) put(id, JsonArray(values.map { JsonPrimitive(it) }))
        }

        putNumber("temp", temp.toDoubleOrNull())
        putValues("eating", eating)
        putString("activity", activity.ifBlank { null })
        putValues("breathing", breathing)
        putBool("nasal", nasal)

        putValues("left_stomach", leftStomach)
        putBool("frothy_mouth", frothyMouth)
        putString("rumen_movement", rumenMovement.ifBlank { null })
        putBool("diarrhea", diarrhea)
        putString("skin_tent", when (skinTent) {
            "2-4", "2-4s", "2–4s" -> "s2_4s"
            else -> skinTent.ifBlank { null }
        })

        if (isFemale) {
            putString("lactation", lactation.ifBlank { null })
            if (cmtApplies) putString("cmt", cmt.ifBlank { null })
            putString("udder", udder.ifBlank { null })
            putString("vulva", vulva.ifBlank { null })
        }
        putString("famacha", famacha.toIntOrNull()?.let { "f$it" })
        putBool("yellow", yellow)

        if (isMale) putString("straining", straining.ifBlank { null })
        putBool("red_urine", redUrine)
        putBool("body_edema", bodyEdema)
        putBool("competition", competition)
        putBool("stomach_inside", stomachInside)

        putString("mouth", mouth.ifBlank { null })
        putValues("eyes", eyes)
        putBool("locked_jaw", lockedJaw)
        putValues("neuro", neuro)
        putString("rash_character", rashCharacter.ifBlank { null })
        putValues("hairloss", if (hairloss == true) listOf("body") else if (hairloss == false) listOf("no") else emptyList())

        putString("leg", leg.ifBlank { null })
        putString("lumps", lumps.ifBlank { null })
        putValues("wounds", wounds)

        putBool("flystrike", flystrike)
        putBool("eartag_flystrike", eartagFlystrike)
        putBool("eartag_wound", eartagWound)
        putBool("ticks", ticks)

        if (isKid) {
            putString("suckle", suckle.ifBlank { null })
            putString("responsiveness", responsiveness.ifBlank { null })
            if (isMilkKid) putString("navel", navel.ifBlank { null })
            if (landingApplies && landing != "na") putString("landing", landing.ifBlank { null })
            if (isMilkKid || isWeaningKid) {
                val authoredMilkIntake = when {
                    milkIntake.isNotEmpty() -> milkIntake
                    (refusalsToday.toIntOrNull() ?: 0) > 0 -> setOf("not_drinking")
                    else -> setOf("normal")
                }
                putValues("milk_intake", authoredMilkIntake)
            }
        }
    }
}

/**
 * The answers as the wire carries them: question id -> list of values.
 *
 * It lives in the APP module, not beside the form model, because kotlinx-serialization-json is not
 * on the feature module's classpath -- and should not be. A feature draws questions; turning them
 * into a request body is the layer that already owns the wire.
 *
 * A LIST EITHER WAY, pick-one included. The backend's decoder accepts a bare string or an array,
 * so one shape on the wire is one shape in the tests -- the same reasoning the typed findings DTO
 * records for its own multi-value fields.
 */
/**
 * The answers as the engine reads them.
 *
 * A MEASUREMENT goes as a bare number, never as a list holding its own text. The engine keeps
 * `Values` and `Number` apart on purpose -- a temperature has to be compared against the
 * question's bands -- so `["102"]` reaches it as a ticked option named "102" and the whole
 * submit is refused with "Temperature takes a measurement". That shipped: an operator walked
 * eleven pages and the run was stored invalid behind a screen that read "Nothing found".
 *
 * A figure that will not parse is sent as typed rather than dropped, so the server names the
 * field instead of reporting it unanswered.
 */
private fun AuthoredAnswers.toWireJson(form: AuthoredForm): JsonObject {
    val numbers = form.pages.flatMap { it.questions }.filter { it.isNumber }.map { it.id }.toSet()
    return JsonObject(
        values.filterValues { it.isNotEmpty() }.mapValues { (id, picked) ->
            val one = picked.first().toDoubleOrNull()
            if (id in numbers && picked.size == 1 && one != null) JsonPrimitive(one)
            else JsonArray(picked.map { JsonPrimitive(it) })
        },
    )
}

/**
 * The published form, as this screen holds it.
 *
 * It lives in the APP module because feature-health does not depend on core-network, and should
 * not: a feature that draws questions has no business knowing the wire type they arrived in.
 *
 * A straight carry of what the server sent. Nothing is defaulted, reordered or filtered here: a
 * client that "helpfully" dropped a question it did not recognise would make an authored question
 * invisible, which is the failure this whole migration exists to end.
 */
private fun HealthObservationFormDto.toAuthoredForm(): AuthoredForm =
    AuthoredForm(
        goatId = goatId,
        displayId = displayId,
        tag = tag,
        typeKey = typeKey,
        typeLabel = typeLabel,
        registerVersion = registerVersion,
        sex = sex,
        stage = stage,
        pages = pages.map { page ->
            AuthoredPage(
                id = page.id,
                title = page.title,
                hint = page.hint,
                questions = page.questions.map { q ->
                    AuthoredQuestion(
                        id = q.id,
                        kind = q.kind,
                        title = q.title,
                        hint = q.hint,
                        options = q.options.map { AuthoredOption(it.value, it.label, it.conflictsWith) },
                        unit = q.unit,
                        min = q.min,
                        max = q.max,
                        onlyIfSex = q.onlyIfSex,
                        onlyIfStage = q.onlyIfStage,
                        onlyIfQuestion = q.onlyIf?.questionId.orEmpty(),
                        onlyIfIn = q.onlyIf?.inValues.orEmpty(),
                    )
                },
            )
        },
    )
