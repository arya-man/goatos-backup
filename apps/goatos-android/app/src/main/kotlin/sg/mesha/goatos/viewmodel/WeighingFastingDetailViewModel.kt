package sg.mesha.goatos.viewmodel

import android.content.Context
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import sg.mesha.goatos.BuildConfig
import sg.mesha.goatos.capture.PhotoCaptureContext
import sg.mesha.goatos.capture.PhotoCaptureSource
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsWeighing
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.analytics.ProofPreviewActionTrace
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.capture.EvidenceSlot
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofFlow
import sg.mesha.goatos.core.data.capture.ProofIdentity
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.weighing.WeighingFastingRepository
import sg.mesha.goatos.core.network.dto.WeighingSopQuestionDto
import sg.mesha.goatos.feature.weighing.WeighingFastingAnswerUi
import sg.mesha.goatos.feature.weighing.WeighingFastingCaptureKind
import sg.mesha.goatos.feature.weighing.WeighingFastingDetailEvent
import sg.mesha.goatos.feature.weighing.WeighingFastingQuestionUi
import sg.mesha.goatos.feature.weighing.WeighingFastingDetailUiState
import sg.mesha.goatos.feature.weighing.WeighingFastingSlotKind
import sg.mesha.goatos.feature.weighing.WeighingFastingSlotStatus
import sg.mesha.goatos.feature.weighing.WeighingFastingSlotUi
import sg.mesha.goatos.feature.weighing.R as WeighingR
import sg.mesha.goatos.ui.Routes
import java.time.LocalDate
import java.time.ZoneId
import javax.inject.Inject

/**
 * The feed & water removal recording screen for ONE SHED (maintainer correction #2, 2026-09-03:
 * the list serves one card per shed and submit is PER SHED). The screen holds exactly two capture
 * slots — this shed's live-camera feed-removal and water-removal videos — and one submit that
 * sends THIS shed's pair. The verifier reviews one item per shed; a rejection hands back this
 * shed alone.
 *
 * The sync discipline copies [FeedDistributionCompleteViewModel], the canonical gated-completion
 * template: each clip goes through the ONE shared proof pipeline
 * ([ProofCaptureRepository.captureReplacingLatest], per-clip idempotency key, per-field cap of 1)
 * on its OWN per-slot upload group, and the submit rides the outbox
 * ([SyncRepository.enqueueWeighingFastingSubmit]) on a group scoped to the (fasting task,
 * campaign shed) pair, carrying the two clips' proof OUTBOX ITEM IDS — never proof ids — which
 * the sync engine resolves at drain time. A card in `rework` resets to Record video and requires
 * FRESH clips. No direct API call anywhere: the card is worked at night in a shed, where the
 * network is worst.
 *
 * The card's status itself is Room truth ([WeighingFastingRepository.observeCard]): a roll-forward
 * at midnight or a verifier's verdict lands as a re-read, never as client-derived clock logic.
 */
@HiltViewModel
class WeighingFastingDetailViewModel @Inject constructor(
    private val fastingRepository: WeighingFastingRepository,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val proofCaptureSource: ProofCaptureSource,
    private val photoCaptureSource: PhotoCaptureSource,
    private val syncRepository: SyncRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    @ApplicationContext private val appContext: Context,
    private val savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val fastingTaskId: String = savedStateHandle.get<String>(Routes.WEIGHING_FASTING_TASK_ARG).orEmpty()
    private val campaignShedId: String = savedStateHandle.get<String>(Routes.WEIGHING_FASTING_SHED_ARG).orEmpty()

    /** First-paint title from the tapped card; superseded by the Room card the moment it emits. */
    private val titleHint: String = savedStateHandle.get<String>(Routes.WEIGHING_FASTING_TITLE_ARG).orEmpty()

    private val submitOutboxItemId = DraftOutboxItemId(savedStateHandle, KEY_SUBMIT_OUTBOX_ITEM_ID)

    /**
     * WEIGHING SOP (2026-09-15): the card's authored questions as the backend served them (the
     * task's pinned version) and the operator's answers. Answers live in the SavedStateHandle so
     * a process death mid-card keeps what was typed alongside the recorded clips.
     */
    private var questionDtos: List<WeighingSopQuestionDto> = emptyList()
    private var answers: Map<String, WeighingFastingAnswerUi> = restoreAnswers()

    private var submitEnqueueInFlight = false
    private val proofJobs = mutableMapOf<String, Job>()
    private var submitJob: Job? = null
    private var refreshInFlight = false

    /**
     * The card's slots as the SOP authored them (WEIGHING SOP). Until the card lands the seeded
     * two stand in, so a draft restored after process death observes its proof items.
     */
    private var slotDtos: List<sg.mesha.goatos.core.network.dto.WeighingRemovalProofSlotDto> = seededSlotDtos()

    private val _state = MutableStateFlow(
        WeighingFastingDetailUiState(
            title = titleHint,
            slots = slotDtos.map { emptySlot(it) },
            submitQueued = submitOutboxItemId.value != null,
        ),
    )
    val state: StateFlow<WeighingFastingDetailUiState> = _state.asStateFlow()

    init {
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_REMOVAL_CARD_OPENED,
            mapOf(AnalyticsEvents.Params.SOURCE to "detail"),
        )
        observeCard()
        submitOutboxItemId.value?.let(::observeSubmitItem)
        _state.value.slots.forEach { slot ->
            slotItemId(slot.slotKey)?.let { observeProofItem(slot.slotKey, it) }
        }
        recomputeSubmit()
        refresh()
    }

    fun onEvent(event: WeighingFastingDetailEvent) {
        when (event) {
            is WeighingFastingDetailEvent.RecordSlot -> recordSlot(event.slotKey, event.photo)
            is WeighingFastingDetailEvent.PreviewAction -> trackPreviewAction(event.slotKey, event.action)
            is WeighingFastingDetailEvent.SetAnswer -> setAnswer(event.questionId) { it.copy(value = event.value) }
            is WeighingFastingDetailEvent.ToggleAnswer -> setAnswer(event.questionId) { current ->
                current.copy(values = if (event.value in current.values) current.values - event.value else current.values + event.value)
            }
            is WeighingFastingDetailEvent.SetOtherText -> setAnswer(event.questionId) { it.copy(otherText = event.text) }
            WeighingFastingDetailEvent.Submit -> submit()
            WeighingFastingDetailEvent.Refresh -> refresh()
            WeighingFastingDetailEvent.DismissMessage -> _state.update { it.copy(message = null) }
        }
    }

    private fun trackPreviewAction(slotKey: String, action: String) {
        val previewAction = ProofPreviewActionTrace.from(action)
        val slot = _state.value.slotOf(slotKey)
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_REMOVAL_PROOF_PREVIEW_ACTION,
            weighingFastingAnalyticsProps(
                slotKey = slotKey,
                action = previewAction.action,
                source = "proof_preview",
                outcome = previewAction.outcome,
                reason = previewAction.reason,
                proofRowId = slot.localProofRowId,
                serverProofId = slot.serverProofId,
            ),
        )
    }

    /** Room is the SSOT for this shed card; the network refresh only rewrites Room. */
    private fun observeCard() {
        viewModelScope.launch {
            fastingRepository.observeCard(fastingTaskId, campaignShedId).filterNotNull().collect { card ->
                val dto = card.dto
                val status = dto.status.trim().lowercase()
                val readOnly = status == STATUS_PENDING_VERIFICATION || status == STATUS_COMPLETED
                val todayIst = LocalDate.now(ZoneId.of("Asia/Kolkata")).toString()
                // The card came BACK from the verifier while a queued submit marker is still
                // held: that submit's clips were the judged act. Hand the card back — drop the
                // queued marker and reset both slots so THIS shed requires FRESH clips.
                if (status == STATUS_REWORK && submitOutboxItemId.value != null) {
                    submitOutboxItemId.value = null
                    submitJob?.cancel()
                    clearSlots()
                }
                // The SOP's slot list for this task; an older server sends none and the seeded
                // two stand.
                slotDtos = dto.proofs.ifEmpty { seededSlotDtos() }
                // After process death the constructor knew only the seeded two; an AUTHORED slot
                // whose capture is still in the outbox arrives only now, so its upload observer is
                // (re)connected here or its later success/failure would never reach the card
                // (PR #274 review round 2, finding 3).
                reconnectSlotObservers()
                _state.update { current ->
                    current.copy(
                        title = dto.subjectLabel.ifBlank { current.title },
                        dateLabel = if (dto.removalBusinessDate == todayIst) {
                            appContext.getString(WeighingR.string.weighing_removal_date_tonight)
                        } else {
                            farmRemovalDateLabel(dto.removalBusinessDate)
                        },
                        status = status,
                        isReadOnly = readOnly,
                        lockNotice = when (status) {
                            STATUS_PENDING_VERIFICATION ->
                                appContext.getString(WeighingR.string.weighing_removal_status_in_review)
                            STATUS_COMPLETED ->
                                appContext.getString(WeighingR.string.weighing_removal_status_done)
                            else -> ""
                        },
                        // The verifier's own sentence about THIS shed, verbatim, only while sent back.
                        reworkReason = if (status == STATUS_REWORK) dto.reworkReason.orEmpty() else "",
                        submitQueued = submitOutboxItemId.value != null && current.submitQueued,
                        slots = slotDtos.map { slot -> mergedSlot(slot, current) },
                        // The SOP's copy for this task, verbatim: the instruction and the questions.
                        instruction = dto.instruction,
                        questions = questionRows(),
                        answers = answers,
                    )
                }
                if (readOnly) {
                    // A submitted/approved card renders its captures from the server (a reinstall
                    // holds no local file). Enrichment only — fetched async, best effort, retried
                    // on the next Room emit; the feed-distribution screen's exact behaviour. AFTER
                    // the slot list above exists: updateSlot maps only slots already in state, so
                    // on the FIRST emit an earlier call touched nothing and the served kind was
                    // lost until the next emit (PR #274 review, finding 4).
                    val recorded = dto.proofRefs.ifEmpty {
                        buildMap {
                            dto.feedProofRef?.takeIf { it.isNotBlank() }?.let { put("feed_video", it) }
                            dto.waterProofRef?.takeIf { it.isNotBlank() }?.let { put("water_video", it) }
                        }
                    }
                    slotDtos.forEach { slot -> fetchRemotePreview(slot, recorded[slot.key].orEmpty(), dto.proofKinds[slot.key]) }
                }
                // A read-only card shows the answers the backend recorded, not this device's draft.
                questionDtos = dto.questions
                if (readOnly && dto.answers.isNotEmpty()) {
                    answers = recordedAnswers(dto.answers)
                }
                _state.update { it.copy(questions = questionRows(), answers = answers) }
                recomputeSubmit()
            }
        }
    }

    private fun fetchRemotePreview(slot: sg.mesha.goatos.core.network.dto.WeighingRemovalProofSlotDto, proofRef: String, recordedKind: String?) {
        if (proofRef.isBlank()) return
        if (_state.value.slotOf(slot.key).let { it.remoteUrl != null || it.previewPath != null }) return
        updateSlot(slot.key) {
            // A recorded capture on a photo slot is a photo; an `either` slot's kind is whatever
            // was captured -- the server names it per slot (`proof_kinds`, from the proof
            // register), so a card reopened with no local state still gets the right player
            // (PR #274 review, finding 4). The saved-state kind is the fallback for an older
            // backend that does not echo it.
            val kind = when {
                slot.kind == WeighingFastingCaptureKind.VIDEO -> WeighingFastingCaptureKind.VIDEO
                slot.kind == WeighingFastingCaptureKind.PHOTO -> WeighingFastingCaptureKind.PHOTO
                recordedKind == WeighingFastingCaptureKind.PHOTO || recordedKind == WeighingFastingCaptureKind.VIDEO -> recordedKind
                else -> slotCapturedKind(slot.key)
            }
            // Remember it where every slot rebuild reads the kind from (emptySlot ->
            // slotCapturedKind), so a later card emit cannot reset the player to the default.
            setSlotCapturedKind(slot.key, kind)
            it.copy(
                captured = true,
                status = WeighingFastingSlotStatus.SYNCED,
                statusLabel = proofStatusLabel(kind, WeighingFastingSlotStatus.SYNCED),
                remoteUrl = weighingBackendProofUrl(proofRef),
                serverProofId = proofRef,
                capturedKind = kind,
            )
        }
    }

    /** One slot, merging any live local capture state over a clean baseline of the SOP's copy. */
    private fun mergedSlot(
        slot: sg.mesha.goatos.core.network.dto.WeighingRemovalProofSlotDto,
        current: WeighingFastingDetailUiState,
    ): WeighingFastingSlotUi {
        // Reuse the live slot only while something real backs it (a capture in flight, a
        // failure to show, or a persisted outbox item). A card whose slots were just reset
        // for rework therefore falls through to a clean "Record" slot.
        val live = current.slots.firstOrNull { it.slotKey == slot.key }
            ?.takeIf { it.fieldKey == fieldKey(slot.key) }
            ?.takeIf {
                it.busy ||
                    it.status == WeighingFastingSlotStatus.FAILED ||
                    it.remoteUrl != null ||
                    it.serverProofId != null ||
                    slotItemId(slot.key) != null
            }
        val base = live ?: emptySlot(slot)
        return base.copy(
            title = slot.title.ifBlank { base.title },
            hint = slot.hint.ifBlank { base.hint },
            captureKind = slot.kind.ifBlank { WeighingFastingCaptureKind.VIDEO },
            required = slot.required,
        )
    }

    /** The seeded document's two slots, for a server that sends none. */
    private fun seededSlotDtos(): List<sg.mesha.goatos.core.network.dto.WeighingRemovalProofSlotDto> = listOf(
        sg.mesha.goatos.core.network.dto.WeighingRemovalProofSlotDto(
            key = WeighingFastingSlotKind.FEED.slotKey,
            title = appContext.getString(WeighingR.string.weighing_removal_feed_slot),
            hint = appContext.getString(WeighingR.string.weighing_removal_feed_hint),
        ),
        sg.mesha.goatos.core.network.dto.WeighingRemovalProofSlotDto(
            key = WeighingFastingSlotKind.WATER.slotKey,
            title = appContext.getString(WeighingR.string.weighing_removal_water_slot),
            hint = appContext.getString(WeighingR.string.weighing_removal_water_hint),
        ),
    )

    // ------------------------------------------------------------------ SOP questions

    private fun setAnswer(questionId: String, transform: (WeighingFastingAnswerUi) -> WeighingFastingAnswerUi) {
        val current = _state.value
        if (current.isReadOnly || current.submitQueued) return
        answers = answers + (questionId to transform(answers[questionId] ?: WeighingFastingAnswerUi()))
        persistAnswers()
        _state.update { it.copy(questions = questionRows(), answers = answers) }
        recomputeSubmit()
    }

    /** Whether a question's "ask only when" holds against the current answers. */
    private fun applies(q: WeighingSopQuestionDto): Boolean {
        val cond = q.onlyIf ?: return true
        return answers[cond.questionId]?.value?.trim() == cond.value
    }

    private fun questionRows(): List<WeighingFastingQuestionUi> = questionDtos.map { q ->
        WeighingFastingQuestionUi(
            id = q.id,
            kind = q.kind,
            title = q.title,
            hint = q.hint,
            required = q.required,
            options = q.options.map { it.value to it.label },
            allowOther = q.allowOther,
            unit = q.unit,
            rangeLabel = listOfNotNull(q.min, q.max).takeIf { it.size == 2 }?.let { (min, max) -> "${trimNumber(min)}–${trimNumber(max)}" }
                ?: q.min?.let { "≥ ${trimNumber(it)}" } ?: q.max?.let { "≤ ${trimNumber(it)}" } ?: "",
            applies = applies(q),
        )
    }

    /**
     * The first unanswered required question that applies, as the submit block reason -- the
     * same sentence the backend would answer with (422 fasting_answer_invalid), stated before
     * any queueing.
     */
    private fun firstMissingAnswer(): String? = questionDtos.firstOrNull { q ->
        if (!q.required || !applies(q)) return@firstOrNull false
        val a = answers[q.id]
        when (q.kind) {
            "multi" -> a == null || a.values.isEmpty()
            "choice" -> a == null || a.value.isBlank() || (a.value == "other" && q.allowOther && a.otherText.isBlank())
            else -> a == null || a.value.isBlank()
        }
    }?.title

    /** The answers as the backend's wire shape: only applicable questions, typed by kind. */
    private fun answersJson(): JsonObject = buildJsonObject {
        questionDtos.forEach { q ->
            if (!applies(q)) return@forEach
            val a = answers[q.id] ?: return@forEach
            when (q.kind) {
                "multi" -> if (a.values.isNotEmpty()) put(q.id, JsonArray(a.values.map { JsonPrimitive(it) }))
                "number" -> a.value.trim().toDoubleOrNull()?.let { put(q.id, JsonPrimitive(it)) }
                "choice" -> if (a.value.isNotBlank()) {
                    put(q.id, JsonPrimitive(a.value))
                    if (a.value == "other" && q.allowOther && a.otherText.isNotBlank()) put("${q.id}_other", JsonPrimitive(a.otherText))
                }
                else -> if (a.value.isNotBlank()) put(q.id, JsonPrimitive(a.value))
            }
        }
    }

    /** The recorded answers of a submitted card, re-read from the backend for display. */
    private fun recordedAnswers(recorded: JsonObject): Map<String, WeighingFastingAnswerUi> {
        val out = mutableMapOf<String, WeighingFastingAnswerUi>() // mobile-guard:ignore: bounded by the card's question count (<= 50 per SOP); rebuilt per emit, never accumulated
        recorded.forEach { (id, element) ->
            if (id.endsWith("_other")) {
                val base = id.removeSuffix("_other")
                out[base] = (out[base] ?: WeighingFastingAnswerUi()).copy(otherText = (element as? JsonPrimitive)?.content.orEmpty())
                return@forEach
            }
            out[id] = when (element) {
                is JsonArray -> (out[id] ?: WeighingFastingAnswerUi()).copy(values = element.mapNotNull { (it as? JsonPrimitive)?.content })
                is JsonPrimitive -> (out[id] ?: WeighingFastingAnswerUi()).copy(value = element.content)
                else -> out[id] ?: WeighingFastingAnswerUi()
            }
        }
        return out
    }

    private fun answersStateKey(): String = "$KEY_ANSWERS_PREFIX:$campaignShedId"

    private fun persistAnswers() {
        val encoded = buildJsonObject {
            answers.forEach { (id, a) ->
                put(id, buildJsonObject {
                    put("v", JsonPrimitive(a.value))
                    put("vs", JsonArray(a.values.map { JsonPrimitive(it) }))
                    put("o", JsonPrimitive(a.otherText))
                })
            }
        }
        savedStateHandle[answersStateKey()] = encoded.toString()
    }

    private fun restoreAnswers(): Map<String, WeighingFastingAnswerUi> {
        val raw = savedStateHandle.get<String>(answersStateKey())?.takeIf { it.isNotBlank() } ?: return emptyMap()
        // exception:exempt a draft that no longer parses is an empty draft; the operator answers again
        val obj = runCatching { Json.parseToJsonElement(raw) as? JsonObject }.getOrNull() ?: return emptyMap()
        return obj.mapValues { (_, el) ->
            val o = el as? JsonObject ?: return@mapValues WeighingFastingAnswerUi()
            WeighingFastingAnswerUi(
                value = (o["v"] as? JsonPrimitive)?.content.orEmpty(),
                values = (o["vs"] as? JsonArray)?.mapNotNull { (it as? JsonPrimitive)?.content }.orEmpty(),
                otherText = (o["o"] as? JsonPrimitive)?.content.orEmpty(),
            )
        }
    }

    private fun emptySlot(slot: sg.mesha.goatos.core.network.dto.WeighingRemovalProofSlotDto): WeighingFastingSlotUi = WeighingFastingSlotUi(
        fieldKey = fieldKey(slot.key),
        kind = WeighingFastingSlotKind.forKey(slot.key) ?: WeighingFastingSlotKind.FEED,
        slotKey = slot.key,
        captureKind = slot.kind.ifBlank { WeighingFastingCaptureKind.VIDEO },
        required = slot.required,
        capturedKind = slotCapturedKind(slot.key),
        title = slot.title,
        hint = slot.hint,
        captured = slotItemId(slot.key) != null,
        // This device's own recording survives process death alongside the outbox item id, so
        // the preview comes back with the draft (the feed-distribution screen's behaviour).
        previewPath = slotPreviewPath(slot.key),
    )

    private fun refresh() {
        if (refreshInFlight) return
        refreshInFlight = true
        _state.update { it.copy(isSyncing = true) }
        viewModelScope.launch {
            try {
                when (val result = fastingRepository.refresh()) {
                    is AppResult.Ok -> Unit
                    // The cached card stays on screen; a refresh miss at night in a shed is
                    // expected, not an error banner.
                    is AppResult.Err -> crashReporter.recordException(
                        result.cause ?: IllegalStateException(result.message),
                        "weighing removal detail refresh failed",
                    )
                }
                syncRepository.triggerDrain()
            } finally {
                refreshInFlight = false
                _state.update { it.copy(isSyncing = false) }
            }
        }
    }

    private fun recordSlot(slotKey: String, photo: Boolean) {
        val current = _state.value
        if (current.isReadOnly || current.submitQueued) return
        val slotUi = current.slots.firstOrNull { it.slotKey == slotKey } ?: return
        if (slotUi.busy) return
        // The slot decides the camera mode: a photo slot always photographs, a video slot always
        // records, an `either` slot follows the button the operator pressed.
        val takePhoto = when (slotUi.captureKind) {
            WeighingFastingCaptureKind.PHOTO -> true
            WeighingFastingCaptureKind.VIDEO -> false
            else -> photo
        }
        updateSlot(slotKey) { it.copy(busy = true) }
        viewModelScope.launch {
            try {
                val captured: CapturedSlotMedia? = try {
                    if (takePhoto) {
                        photoCaptureSource.capturePhoto(
                            PhotoCaptureContext(title = slotUi.title, instruction = slotUi.hint.ifBlank { current.title }),
                        )?.let { CapturedSlotMedia(it.localUri, it.mimeType, it.capturedAtMs, it.capturedAtMs, it.captureSource, WeighingFastingCaptureKind.PHOTO) }
                    } else {
                        proofCaptureSource.captureVideo(
                            ProofCaptureContext(
                                title = slotUi.title,
                                // The SHED the clip must show — the backend-owned card title, which
                                // names the shed — front and centre.
                                primaryTag = current.title.ifBlank { slotUi.title },
                                workLabel = slotUi.title,
                                headerTitle = appContext.getString(WeighingR.string.weighing_removal_screen_title),
                            ),
                        )?.let { CapturedSlotMedia(it.localUri, it.mimeType, it.startedAtMs, it.endedAtMs, it.captureSource, WeighingFastingCaptureKind.VIDEO) }
                    }
                } catch (error: Exception) {
                    crashReporter.recordException(error, "weighing removal capture failed")
                    trackFailure(slotUi.fieldKey, "camera_exception")
                    null
                }
                if (captured == null) {
                    analytics.track(
                        AnalyticsEventsWeighing.WEIGHING_REMOVAL_SLOT_CAPTURED,
                        weighingFastingAnalyticsProps(
                            slotKey = slotKey,
                            action = "capture_cancelled",
                            source = "camera",
                            outcome = "cancelled",
                            reason = "camera_cancelled",
                        ),
                    )
                    updateSlot(slotKey) { it.copy(busy = false) }
                    return@launch
                }
                val fieldKey = fieldKey(slotKey)
                val slot = EvidenceSlot(
                    identity = ProofIdentity(
                        flow = ProofFlow.WEIGHING_FASTING,
                        taskId = fastingTaskId,
                        subjectKey = fieldKey,
                    ),
                    fieldKey = fieldKey,
                )
                when (
                    val result = proofCaptureRepository.captureReplacingLatest(
                        slot = slot,
                        subject = ProofSubject.TASK,
                        subjectId = fastingTaskId,
                        localUri = captured.localUri,
                        mimeType = captured.mimeType,
                        caption = "${slotUi.title} · ${current.title}".trim(' ', '·'),
                        scopeType = "task",
                        scopeId = fastingTaskId,
                        capturedStartMs = captured.startedAtMs,
                        capturedEndMs = captured.endedAtMs,
                        capturedByPrincipalId = null,
                        proofPolicy = weighingFastingProofPolicy(captured.captureSource, captured.kind),
                        awaitUploadEnqueue = true,
                        // PER-SLOT upload group: each clip is independent field work, so one
                        // backed-off upload must never strand the other as "waiting". The submit
                        // waits for both by resolving their outbox ids at drain time instead.
                        uploadGroupKey = proofUploadGroupKey(fieldKey),
                    )
                ) {
                    is AppResult.Ok -> {
                        val proofOutboxId = result.value.outboxItemId
                        if (proofOutboxId.isNullOrBlank()) {
                            trackFailure(
                                fieldKey = fieldKey,
                                reason = "missing_upload_outbox",
                                slotKey = slotKey,
                                proofRowId = result.value.id,
                                serverProofId = result.value.serverProofId,
                            )
                            updateSlot(slotKey) {
                                it.copy(
                                    busy = false,
                                    status = WeighingFastingSlotStatus.FAILED,
                                    statusLabel = appContext.getString(WeighingR.string.weighing_removal_record_again),
                                )
                            }
                            return@launch
                        }
                        setSlotItemId(slotKey, proofOutboxId)
                        setSlotPreviewPath(slotKey, captured.localUri)
                        setSlotCapturedKind(slotKey, captured.kind)
                        observeProofItem(slotKey, proofOutboxId)
                        analytics.track(
                            AnalyticsEventsWeighing.WEIGHING_REMOVAL_SLOT_CAPTURED,
                            weighingFastingAnalyticsProps(
                                slotKey = slotKey,
                                action = "captured",
                                source = "capture_repository",
                                outcome = "success",
                                proofRowId = result.value.id,
                                proofOutboxItemId = proofOutboxId,
                                serverProofId = result.value.serverProofId,
                            ),
                        )
                        updateSlot(slotKey) {
                            it.copy(
                                busy = false,
                                captured = true,
                                status = WeighingFastingSlotStatus.QUEUED,
                                statusLabel = proofStatusLabel(captured.kind, WeighingFastingSlotStatus.QUEUED),
                                previewPath = captured.localUri,
                                capturedKind = captured.kind,
                                localProofRowId = result.value.id,
                                serverProofId = result.value.serverProofId,
                            )
                        }
                        recomputeSubmit()
                    }
                    is AppResult.Err -> {
                        result.cause?.let { crashReporter.recordException(it, "weighing removal proof enqueue failed") }
                        trackFailure(fieldKey, result.message, slotKey = slotKey)
                        updateSlot(slotKey) {
                            it.copy(
                                busy = false,
                                status = WeighingFastingSlotStatus.FAILED,
                                statusLabel = result.message,
                            )
                        }
                    }
                }
            } finally {
                updateSlot(slotKey) { it.copy(busy = false) }
            }
        }
    }

    private fun submit() {
        if (submitEnqueueInFlight) return
        val current = _state.value
        // Every COMPULSORY capture of this shed, always: a submit missing one would be refused
        // server-side anyway (proof-shaped 422, backend farm copy), so the block is stated here
        // before any queueing. Optional slots ride along only when captured.
        val proofItems = capturedProofItems()
        if (current.isReadOnly || current.submitQueued || firstMissingCapture() != null || firstMissingAnswer() != null) {
            recomputeSubmit()
            return
        }
        val feedItem = proofItems[WeighingFastingSlotKind.FEED.slotKey].orEmpty()
        val waterItem = proofItems[WeighingFastingSlotKind.WATER.slotKey].orEmpty()
        val answersPayload = answersJson()
        submitEnqueueInFlight = true
        viewModelScope.launch {
            when (
                val result = syncRepository.enqueueWeighingFastingSubmit(
                    // (task, shed)-grain group: two submits of the same shed card drain strictly
                    // in order. The proofs are NOT on this group — each rides its own per-slot
                    // upload lane and the dispatcher resolves them by outbox id.
                    groupKey = submitGroupKey(),
                    // STABLE per (task, shed, capture set, answers): a retry replays for free; a
                    // post-rework re-shoot names new proofs and is a genuinely new act under a new
                    // key -- and so is a CORRECTED answer after the backend refused one (PR #274
                    // review, finding 1): same captures, different payload, must not collide with
                    // the refused submit's outbox row.
                    idempotencyKey = submitIdempotencyKey(proofItems, answersPayload),
                    fastingTaskId = fastingTaskId,
                    campaignShedId = campaignShedId,
                    feedProofOutboxItemId = feedItem,
                    waterProofOutboxItemId = waterItem,
                    answers = answersPayload,
                    proofOutboxItems = proofItems,
                )
            ) {
                is AppResult.Ok -> {
                    submitOutboxItemId.value = result.value
                    observeSubmitItem(result.value)
                    analytics.track(
                        AnalyticsEventsWeighing.WEIGHING_REMOVAL_SUBMITTED,
                        weighingFastingSubmitAnalyticsProps(
                            status = current.status,
                            outcome = "queued",
                            source = "submit_button",
                            submitOutboxId = result.value,
                            feedProofOutboxItemId = feedItem,
                            waterProofOutboxItemId = waterItem,
                        ),
                    )
                    submitEnqueueInFlight = false
                    _state.update {
                        it.copy(
                            submitQueued = true,
                            submitEnabled = false,
                            message = SUBMIT_QUEUED_MESSAGE,
                        )
                    }
                }
                is AppResult.Err -> {
                    submitEnqueueInFlight = false
                    result.cause?.let { crashReporter.recordException(it, "weighing removal submit enqueue failed") }
                    trackFailure(
                        fieldKey = "submit",
                        reason = result.message,
                        outcome = "enqueue_failure",
                        source = "submit_button",
                        feedProofOutboxItemId = feedItem,
                        waterProofOutboxItemId = waterItem,
                    )
                    _state.update { it.copy(message = result.message) }
                }
            }
        }
    }

    /** Observe every slot that has a persisted outbox item but no live observer yet. */
    private fun reconnectSlotObservers() {
        slotDtos.forEach { slot ->
            val itemId = slotItemId(slot.key) ?: return@forEach
            val live = proofJobs[slot.key]?.isActive == true
            if (!live) observeProofItem(slot.key, itemId)
        }
    }

    private fun observeProofItem(slotKey: String, itemId: String) {
        proofJobs.remove(slotKey)?.cancel()
        proofJobs[slotKey] = viewModelScope.launch {
            syncRepository.observeItem(itemId)
                .filterNotNull()
                .distinctUntilChanged()
                .collect { item -> applyProofItem(slotKey, item) }
        }
    }

    private fun applyProofItem(slotKey: String, item: SyncQueueItem) {
        val status = when {
            item.status == SyncItemStatus.SUCCEEDED -> WeighingFastingSlotStatus.SYNCED
            item.isTerminalFailure -> WeighingFastingSlotStatus.FAILED
            item.status == SyncItemStatus.IN_FLIGHT -> WeighingFastingSlotStatus.UPLOADING
            else -> WeighingFastingSlotStatus.QUEUED
        }
        updateSlot(slotKey) {
            it.copy(
                captured = true,
                status = status,
                // The server's (or transport's) own failure reason, verbatim where one exists.
                statusLabel = item.lastError.takeIf { status == WeighingFastingSlotStatus.FAILED } ?: proofStatusLabel(it.capturedKind, status),
            )
        }
        if (item.status == SyncItemStatus.SUCCEEDED || item.isTerminalFailure) {
            val slot = _state.value.slotOf(slotKey)
            analytics.track(
                AnalyticsEventsWeighing.WEIGHING_REMOVAL_FAILURE.takeIf { item.isTerminalFailure }
                    ?: AnalyticsEventsWeighing.WEIGHING_REMOVAL_SLOT_CAPTURED,
                weighingFastingAnalyticsProps(
                    slotKey = slotKey,
                    action = "upload_${item.status.name.lowercase()}",
                    source = "proof_outbox_observer",
                    outcome = if (item.status == SyncItemStatus.SUCCEEDED) "sync_success" else "sync_terminal_failure",
                    reason = item.lastError,
                    proofOutboxItemId = item.id,
                    serverProofId = slot.serverProofId,
                ),
            )
        }
        recomputeSubmit()
    }

    private fun observeSubmitItem(itemId: String) {
        submitJob?.cancel()
        submitJob = viewModelScope.launch {
            syncRepository.observeItem(itemId)
                .filterNotNull()
                .distinctUntilChanged()
                .collect { item ->
                    val writeResult = item.toWriteResult(SUBMIT_QUEUED_MESSAGE, SUBMIT_SYNCED_MESSAGE)
                    _state.update {
                        it.copy(
                            submitQueued = writeResult.isCommitted,
                            message = writeResult.message,
                            submitEnabled = false,
                        )
                    }
                    if (writeResult.isCorrectable) {
                        // A terminal refusal hands the card back: drop the queued marker so the
                        // operator can re-record and submit again under a new proof set.
                        submitOutboxItemId.value = null
                        recomputeSubmit()
                    }
                    if (item.status == SyncItemStatus.SUCCEEDED || item.isTerminalFailure) {
                        analytics.track(
                            AnalyticsEventsWeighing.WEIGHING_REMOVAL_SUBMITTED,
                            weighingFastingSubmitAnalyticsProps(
                                status = _state.value.status,
                                outcome = if (item.status == SyncItemStatus.SUCCEEDED) "sync_success" else "sync_terminal_failure",
                                reason = item.lastError,
                                source = "submit_outbox_observer",
                                submitOutboxId = item.id,
                                feedProofOutboxItemId = slotItemId(WeighingFastingSlotKind.FEED.slotKey),
                                waterProofOutboxItemId = slotItemId(WeighingFastingSlotKind.WATER.slotKey),
                            ),
                        )
                    }
                }
        }
    }

    private fun recomputeSubmit() {
        _state.update { current ->
            // Every COMPULSORY capture recorded (its outbox item exists) — nothing else gates
            // the button; the drain waits for the uploads by resolving the outbox ids.
            // WEIGHING SOP: a missing compulsory capture, then a required authored question left
            // unanswered, each block the submit BY NAME.
            val missingCapture = firstMissingCapture()
            val missingAnswer = firstMissingAnswer()
            val enabled = missingCapture == null && missingAnswer == null && !current.isReadOnly && !current.submitQueued
            current.copy(
                submitEnabled = enabled,
                submitBlockedReason = when {
                    enabled || current.submitQueued || current.isReadOnly -> ""
                    missingCapture != null -> appContext.getString(WeighingR.string.weighing_removal_capture_required_fmt, missingCapture)
                    else -> appContext.getString(WeighingR.string.weighing_removal_answer_required_fmt, missingAnswer.orEmpty())
                },
            )
        }
    }

    private fun updateSlot(
        slotKey: String,
        transform: (WeighingFastingSlotUi) -> WeighingFastingSlotUi,
    ) {
        _state.update { current ->
            current.copy(slots = current.slots.map { if (it.slotKey == slotKey) transform(it) else it })
        }
    }

    /** {slot key: proof outbox item id} for every slot this device has captured. */
    private fun capturedProofItems(): Map<String, String> = buildMap {
        _state.value.slots.forEach { slot -> slotItemId(slot.slotKey)?.let { put(slot.slotKey, it) } }
    }

    /** The first compulsory slot still without a capture, by title -- the submit's block reason. */
    private fun firstMissingCapture(): String? =
        _state.value.slots.firstOrNull { it.required && slotItemId(it.slotKey) == null }?.title

    // ------------------------------------------------------------------ per-slot draft identity

    /**
     * Field keys are part of the durable capture identity — never rename casually. The seeded
     * two keep the exact keys the per-shed sections carried, so an in-flight draft survives the
     * move to authored slots; an authored slot is keyed by its SOP key.
     */
    internal fun fieldKey(slotKey: String): String = when (slotKey) {
        WeighingFastingSlotKind.FEED.slotKey -> "${FIELD_FEED_VIDEO_PREFIX}$campaignShedId"
        WeighingFastingSlotKind.WATER.slotKey -> "${FIELD_WATER_VIDEO_PREFIX}$campaignShedId"
        else -> "${FIELD_SLOT_PREFIX}${slotKey}_$campaignShedId"
    }

    internal fun fieldKey(kind: WeighingFastingSlotKind): String = fieldKey(kind.slotKey)

    /** The seeded slots keep their pre-SOP state keys ("feed" / "water") so drafts survive. */
    private fun slotStateSuffix(slotKey: String): String = when (slotKey) {
        WeighingFastingSlotKind.FEED.slotKey -> "feed"
        WeighingFastingSlotKind.WATER.slotKey -> "water"
        else -> slotKey
    }

    private fun slotStateKey(slotKey: String): String =
        "$KEY_SLOT_PROOF_ITEM_ID_PREFIX:$campaignShedId:${slotStateSuffix(slotKey)}"

    private fun slotItemId(slotKey: String): String? =
        savedStateHandle.get<String>(slotStateKey(slotKey))?.takeIf { it.isNotBlank() }

    private fun setSlotItemId(slotKey: String, itemId: String?) {
        val key = slotStateKey(slotKey)
        if (itemId == null) savedStateHandle.remove<String>(key) else savedStateHandle[key] = itemId
    }

    private fun slotPreviewKey(slotKey: String): String =
        "$KEY_SLOT_PREVIEW_PATH_PREFIX:$campaignShedId:${slotStateSuffix(slotKey)}"

    private fun slotPreviewPath(slotKey: String): String? =
        savedStateHandle.get<String>(slotPreviewKey(slotKey))?.takeIf { it.isNotBlank() }

    private fun setSlotPreviewPath(slotKey: String, path: String?) {
        val key = slotPreviewKey(slotKey)
        if (path == null) savedStateHandle.remove<String>(key) else savedStateHandle[key] = path
    }

    private fun slotCapturedKindKey(slotKey: String): String =
        "$KEY_SLOT_CAPTURED_KIND_PREFIX:$campaignShedId:${slotStateSuffix(slotKey)}"

    private fun slotCapturedKind(slotKey: String): String =
        savedStateHandle.get<String>(slotCapturedKindKey(slotKey))?.takeIf { it.isNotBlank() } ?: WeighingFastingCaptureKind.VIDEO

    private fun setSlotCapturedKind(slotKey: String, kind: String?) {
        val key = slotCapturedKindKey(slotKey)
        if (kind == null) savedStateHandle.remove<String>(key) else savedStateHandle[key] = kind
    }

    private fun clearSlots() {
        _state.value.slots.forEach { slot ->
            setSlotItemId(slot.slotKey, null)
            setSlotPreviewPath(slot.slotKey, null)
            setSlotCapturedKind(slot.slotKey, null)
            proofJobs.remove(slot.slotKey)?.cancel()
        }
        _state.update { current ->
            current.copy(slots = slotDtos.map { emptySlot(it) })
        }
    }

    /** Each clip drains on its OWN lane (commit e8a540e4f's parallel-slot contract); the submit
     *  never shares them — it waits by resolving the outbox ids at dispatch instead. */
    internal fun proofUploadGroupKey(fieldKey: String): String = "${submitGroupKey()}:$fieldKey"

    /** Scoped to the (fasting task, campaign shed) pair — the per-shed submit's own lane. */
    internal fun submitGroupKey(): String = "weighing-fasting:$fastingTaskId:$campaignShedId"

    /** STABLE across retries, NEW after any re-shoot: the shed plus its capture outbox ids. */
    internal fun submitIdempotencyKey(feedItemId: String, waterItemId: String): String =
        submitIdempotencyKey(mapOf(WeighingFastingSlotKind.FEED.slotKey to feedItemId, WeighingFastingSlotKind.WATER.slotKey to waterItemId))

    /**
     * The seeded pair keeps the pre-SOP key shape (`feed|water`) so a queued draft replays under
     * the key it was minted with; any other capture set is keyed by its sorted slot pairs.
     */
    internal fun submitIdempotencyKey(proofItems: Map<String, String>, answers: JsonObject = JsonObject(emptyMap())): String {
        val seededOnly = proofItems.keys == setOf(WeighingFastingSlotKind.FEED.slotKey, WeighingFastingSlotKind.WATER.slotKey)
        val tail = if (seededOnly) {
            "${proofItems[WeighingFastingSlotKind.FEED.slotKey]}|${proofItems[WeighingFastingSlotKind.WATER.slotKey]}"
        } else {
            proofItems.entries.sortedBy { it.key }.joinToString("|") { "${it.key}=${it.value}" }
        }
        // The answers are part of the submission's identity: a corrected answer over the same
        // captures is a new submission, not a conflicting replay of the refused one. A card with
        // no answers keeps the pre-SOP key shape so a queued draft replays under its own key.
        val answerTail = if (answers.isEmpty()) "" else ":a=" + answersDigest(answers)
        return "weighing-fasting-submit:$fastingTaskId:$campaignShedId:$tail$answerTail"
    }

    private fun answersDigest(answers: JsonObject): String {
        val canonical = answers.entries.sortedBy { it.key }.joinToString("&") { "${it.key}=${it.value}" }
        val digest = java.security.MessageDigest.getInstance("SHA-256").digest(canonical.toByteArray(Charsets.UTF_8))
        return digest.take(8).joinToString("") { "%02x".format(it) }
    }

    private fun trackFailure(
        fieldKey: String,
        reason: String,
        slotKey: String? = null,
        outcome: String = "failure",
        source: String = "viewmodel",
        proofRowId: String? = null,
        proofOutboxItemId: String? = slotKey?.let(::slotItemId),
        serverProofId: String? = slotKey?.let { _state.value.slotOf(it).serverProofId },
        feedProofOutboxItemId: String? = null,
        waterProofOutboxItemId: String? = null,
    ) {
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_REMOVAL_FAILURE,
            if (slotKey != null) {
                weighingFastingAnalyticsProps(
                    slotKey = slotKey,
                    action = "failure",
                    source = source,
                    outcome = outcome,
                    reason = reason,
                    proofRowId = proofRowId,
                    proofOutboxItemId = proofOutboxItemId,
                    serverProofId = serverProofId,
                )
            } else {
                weighingFastingSubmitAnalyticsProps(
                    status = _state.value.status,
                    outcome = outcome,
                    reason = reason,
                    source = source,
                    feedProofOutboxItemId = feedProofOutboxItemId,
                    waterProofOutboxItemId = waterProofOutboxItemId,
                ) + mapOf(AnalyticsEvents.Params.FIELD to fieldKey)
            },
        )
    }

    private fun weighingFastingAnalyticsProps(
        slotKey: String,
        action: String,
        source: String,
        outcome: String? = null,
        reason: String? = null,
        proofRowId: String? = null,
        proofOutboxItemId: String? = slotItemId(slotKey),
        serverProofId: String? = _state.value.slotOf(slotKey).serverProofId,
    ): Map<String, String> = buildMap {
        put(AnalyticsEvents.Params.SOURCE, source)
        put(AnalyticsEvents.Params.FIELD, fieldKey(slotKey))
        put(AnalyticsEvents.Params.KIND, slotStateSuffix(slotKey))
        put(AnalyticsEvents.Params.ACTION, action)
        put(AnalyticsEvents.Params.ITEM_ID, submitGroupKey())
        put(AnalyticsEvents.Params.CAMPAIGN_ID, fastingTaskId)
        put(AnalyticsEvents.Params.CAMPAIGN_SHED_ID, campaignShedId)
        put(AnalyticsEvents.Params.SHED_ID, campaignShedId)
        put(AnalyticsEvents.Params.SUBJECT_TYPE, "task")
        put("feature_surface", "weighing_fasting")
        put("task_id", fastingTaskId)
        put("scope_key", submitGroupKey())
        put("submit_group_key", submitGroupKey())
        outcome?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.OUTCOME, it) }
        reason?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.REASON, it.take(MAX_REASON_CHARS)) }
        proofRowId?.takeIf { it.isNotBlank() }?.let { put("local_proof_row_id", it) }
        proofOutboxItemId?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID, it) }
        serverProofId?.takeIf { it.isNotBlank() }?.let { put("server_proof_id", it) }
    }

    private fun weighingFastingSubmitAnalyticsProps(
        status: String,
        outcome: String,
        source: String,
        reason: String? = null,
        submitOutboxId: String? = submitOutboxItemId.value,
        feedProofOutboxItemId: String? = slotItemId(WeighingFastingSlotKind.FEED.slotKey),
        waterProofOutboxItemId: String? = slotItemId(WeighingFastingSlotKind.WATER.slotKey),
    ): Map<String, String> = buildMap {
        put(AnalyticsEvents.Params.SOURCE, source)
        put(AnalyticsEvents.Params.ACTION, "submit")
        put(AnalyticsEvents.Params.KIND, "weighing_fasting_submit")
        put(AnalyticsEvents.Params.ITEM_ID, submitGroupKey())
        put(AnalyticsEvents.Params.CAMPAIGN_ID, fastingTaskId)
        put(AnalyticsEvents.Params.CAMPAIGN_SHED_ID, campaignShedId)
        put(AnalyticsEvents.Params.SHED_ID, campaignShedId)
        put(AnalyticsEvents.Params.SUBJECT_TYPE, "task")
        put("feature_surface", "weighing_fasting")
        put("task_id", fastingTaskId)
        put("scope_key", submitGroupKey())
        put("submit_group_key", submitGroupKey())
        status.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.STATUS, it) }
        put(AnalyticsEvents.Params.OUTCOME, outcome)
        reason?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.REASON, it.take(MAX_REASON_CHARS)) }
        submitOutboxId?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.OUTBOX_ITEM_ID, it) }
        feedProofOutboxItemId?.takeIf { it.isNotBlank() }?.let { put("feed_proof_outbox_item_id", it) }
        waterProofOutboxItemId?.takeIf { it.isNotBlank() }?.let { put("water_proof_outbox_item_id", it) }
    }

    companion object {
        /** Per-shed field keys: `weighing_fasting_feed_video_<campaignShedId>` etc. — part of the
         *  durable capture identity, never renamed casually. */
        const val FIELD_FEED_VIDEO_PREFIX = "weighing_fasting_feed_video_"
        const val FIELD_WATER_VIDEO_PREFIX = "weighing_fasting_water_video_"
        /** An AUTHORED slot's field key: `weighing_fasting_slot_<slot key>_<campaignShedId>`. */
        const val FIELD_SLOT_PREFIX = "weighing_fasting_slot_"

        private const val KEY_SLOT_PROOF_ITEM_ID_PREFIX = "weighing_fasting_proof_item_id"
        private const val KEY_SLOT_PREVIEW_PATH_PREFIX = "weighing_fasting_proof_preview_path"
        private const val KEY_SLOT_CAPTURED_KIND_PREFIX = "weighing_fasting_proof_captured_kind"
        private const val KEY_SUBMIT_OUTBOX_ITEM_ID = "weighing_fasting_submit_outbox_item_id"
        private const val KEY_ANSWERS_PREFIX = "weighing_fasting_answers"

        private fun trimNumber(v: Double): String = if (v == v.toLong().toDouble()) v.toLong().toString() else v.toString()

        private const val STATUS_PENDING_VERIFICATION = "pending_verification"
        private const val STATUS_COMPLETED = "completed"
        private const val STATUS_REWORK = "rework"

        private const val MAX_REASON_CHARS = 96

        // Device-local pre-sync status copy (mobile-contract:ignore: device-local outbox state
        // has no backend contract to carry it; server copy rides lastError verbatim above).
        /**
         * Farm-worded live status for a slot's capture, naming what was captured: a photo slot
         * says "Photo sent", never "Video sent" (found on the Realme, 2026-09-15).
         */
        internal fun proofStatusLabel(kind: String?, status: WeighingFastingSlotStatus): String {
            val noun = if (kind == WeighingFastingCaptureKind.PHOTO) "Photo" else "Video"
            return when (status) {
                WeighingFastingSlotStatus.SYNCED -> "$noun sent"
                WeighingFastingSlotStatus.UPLOADING -> "$noun on its way…"
                WeighingFastingSlotStatus.FAILED -> "$noun didn't go through. ${if (kind == WeighingFastingCaptureKind.PHOTO) "Take it again." else "Record again."}"
                else -> "$noun saved. It will upload on its own."
            }
        }
        private const val SUBMIT_QUEUED_MESSAGE = "Saved. It will be sent when the network allows."
        private const val SUBMIT_SYNCED_MESSAGE = "Sent. The videos will be checked later."
    }
}

private fun weighingBackendProofUrl(proofRef: String): String =
    BuildConfig.API_BASE_URL.trimEnd('/') + "/app/proofs/${proofRef.trim()}/download"

private fun WeighingFastingDetailUiState.slotOf(slotKey: String): WeighingFastingSlotUi =
    slots.firstOrNull { it.slotKey == slotKey } ?: WeighingFastingSlotUi(fieldKey = "", slotKey = slotKey, title = "")

/** One capture the operator took for a slot, whichever camera mode produced it. */
private data class CapturedSlotMedia(
    val localUri: String,
    val mimeType: String,
    val startedAtMs: Long,
    val endedAtMs: Long,
    val captureSource: String,
    val kind: String,
)

/** One proof per slot: each capture is a distinct required step, never a repeat take of one
 *  thing — the same per-field cap of 1 feed distribution uses. */
internal fun weighingFastingProofPolicy(captureSource: String, kind: String = WeighingFastingCaptureKind.VIDEO): ProofPolicy =
    ProofPolicy.Default.copy(
        types = listOf("video", "photo"),
        proofMode = if (kind == WeighingFastingCaptureKind.PHOTO) "task_photo" else "task_video",
        subjectScope = ProofSubject.TASK.wireValue,
        expectedSubjects = listOf(ProofSubject.TASK.wireValue),
        captureSource = captureSource,
        maximumCountPerField = 1,
    )
