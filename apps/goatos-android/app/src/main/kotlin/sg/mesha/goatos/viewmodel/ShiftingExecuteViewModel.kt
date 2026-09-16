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
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import sg.mesha.goatos.capture.PhotoCaptureContext
import sg.mesha.goatos.capture.PhotoCaptureSource
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCapturePrompt
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CaptureDraft
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.CaptureFlow
import sg.mesha.goatos.core.data.ShiftingPendingRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofFlow
import sg.mesha.goatos.core.data.capture.ProofIdentity
import sg.mesha.goatos.core.data.sync.FeedSlotProofSourcePayload
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.CountsShiftingPendingExecutionItemDto
import sg.mesha.goatos.feature.counts.CountsWriteResultUi
import sg.mesha.goatos.feature.counts.CountsWriteStatus
import sg.mesha.goatos.feature.counts.SECTION_HIGH_PRIORITY
import sg.mesha.goatos.feature.counts.ShiftingExecuteAnimalUi
import sg.mesha.goatos.feature.counts.ShiftingExecuteEvent
import sg.mesha.goatos.feature.counts.ShiftingExecuteUiState
import sg.mesha.goatos.feature.counts.ShiftingFeedItemUi
import sg.mesha.goatos.feature.feed.FeedDistributionProofStatus
import sg.mesha.goatos.feature.feed.FeedSlotCaptureKind
import sg.mesha.goatos.feature.feed.FeedSopCardUi
import sg.mesha.goatos.feature.feed.isQueuedForSubmit
import java.util.Locale
import java.util.UUID
import javax.inject.Inject

/**
 * The Shifting EXECUTE screen (`/counts/shifting/execute/{shifting_event_id}`) — an operator
 * recording operator completion for an approved movement.
 *
 * SHIFTING SOP (maintainer decision 2026-09-16, docs/decisions/shifting-sop.md): WHAT the operator
 * captures and answers is the movement's PINNED shifting SOP -- the completion card, plus the
 * high-priority card for a high movement -- served on the cached pending row. Two slot controllers
 * ([FeedSopSlotController], the machinery the feed stages share) run them: every slot is an
 * independent offline-first proof write on the MOVEMENT'S outbox group, so each capture drains
 * strictly before the completion that names it.
 *
 * Durability (Back / process death): the completion card's captures and answers live in the
 * capture-draft store under the movement; the high-priority card's under `<movement>:high` so the two
 * answer sets never overwrite each other. Clips an older build recorded under the step names
 * `shifting` / `packing` / `feeding` fill their seeded slots.
 *
 * Resets are cutoff-based and durable: a verifier REWORK empties both cards once (answers kept) and a
 * changed Feed Config empties only the high-priority card; the cutoff is stored so re-opening the
 * movement never wipes a clip recorded after the reset, and Room never re-fills a slot with a rejected
 * take.
 *
 * **Mark done** (`enqueueShiftingComplete`) relocates only when Park Head approval exists. Its
 * idempotency key is STABLE per movement and durable, so a resend collapses onto the original
 * relocation; answers or authored slots fold a digest into it.
 */
@HiltViewModel
class ShiftingExecuteViewModel @Inject constructor(
    private val repo: ShiftingPendingRepository,
    private val drafts: CaptureDraftRepository,
    private val syncRepository: SyncRepository,
    private val proofCaptureSource: ProofCaptureSource,
    private val photoCaptureSource: PhotoCaptureSource,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    private val savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val shiftingEventId: String = savedStateHandle[ARG_SHIFTING_EVENT_ID] ?: ""

    // The movement's destination shed is the proof subject; resolved from the cached row on load.
    private var destinationShedId: String = ""
    private var destinationParkLabel: String = ""
    private var destinationLocationLabel: String = ""
    private var loaded = false

    private var draft = CaptureDraft()

    private val _state = MutableStateFlow(ShiftingExecuteUiState(shiftingEventId = shiftingEventId))
    val state: StateFlow<ShiftingExecuteUiState> = _state.asStateFlow()

    private var statusJob: Job? = null

    private fun controller(section: String, entity: String, prompt: ProofCapturePrompt, legacy: Map<String, String>) = FeedSopSlotController(
        scope = viewModelScope,
        savedStateHandle = savedStateHandle,
        syncRepository = syncRepository,
        proofCaptureSource = proofCaptureSource,
        photoCaptureSource = photoCaptureSource,
        proofCaptureRepository = proofCaptureRepository,
        analytics = analytics,
        crashReporter = crashReporter,
        stageKey = "shiftingExecute.$section",
        groupKey = shiftingEventId,
        shedId = "",
        evidenceIdentity = ProofIdentity(flow = ProofFlow.SHIFTING, taskId = shiftingEventId, subjectKey = shiftingEventId),
        proofPolicy = ::feedShedProofPolicy,
        caption = { title -> shiftingProofCaption(title) },
        videoContext = { title ->
            ProofCaptureContext(
                title = shiftingProofCaption(title),
                primaryTag = destinationLocationLabel.ifBlank { destinationShedId },
                workLabel = title,
                prompt = prompt,
            )
        },
        photoContext = { slot -> PhotoCaptureContext(title = slot.title, instruction = slot.hint.ifBlank { slot.title }) },
        events = FeedSopSlotController.Events(
            captureTapped = AnalyticsEvents.COUNTS_SHIFTING_CAPTURE_TAPPED,
            captured = AnalyticsEvents.COUNTS_SHIFTING_EXECUTE_VIDEO_CAPTURED,
            uploadSynced = AnalyticsEvents.COUNTS_SHIFTING_PROOF_SYNCED,
            failure = AnalyticsEvents.COUNTS_SHIFTING_CAPTURE_FAILURE,
            reuploadTapped = AnalyticsEvents.COUNTS_SHIFTING_REUPLOAD_TAPPED,
            cardApplied = AnalyticsEvents.COUNTS_SHIFTING_CARD_APPLIED,
        ),
        baseProps = { slotKey, action -> eventProps(section, slotKey, action) },
        locked = { _state.value.result.isCommitted },
        onChanged = ::recompute,
        durableDrafts = drafts,
        durableFlowKey = CaptureFlow.SHIFTING,
        legacySteps = legacy,
        durableEntityId = entity,
        shedIdProvider = { destinationShedId },
    )

    private val completion = controller(
        section = "completion",
        entity = shiftingEventId,
        prompt = ProofCapturePrompt.SHIFTING,
        legacy = mapOf("shifting" to ShiftingSopSeed.SLOT_SHIFTING_VIDEO),
    )
    private val highPriority = controller(
        section = SECTION_HIGH_PRIORITY,
        entity = highEntity(shiftingEventId),
        prompt = ProofCapturePrompt.FEED_PACKING,
        legacy = mapOf("packing" to ShiftingSopSeed.SLOT_PACKING_VIDEO, "feeding" to ShiftingSopSeed.SLOT_FEEDING_VIDEO),
    )

    init {
        analytics.track(AnalyticsEvents.COUNTS_SHIFTING_EXECUTE_OPENED)
        viewModelScope.launch { completion.state.collect { recompute() } }
        viewModelScope.launch { highPriority.state.collect { recompute() } }
        loadMovement()
    }

    fun onEvent(event: ShiftingExecuteEvent) {
        when (event) {
            is ShiftingExecuteEvent.CaptureSlot ->
                (if (event.section == SECTION_HIGH_PRIORITY) highPriority else completion).captureSlot(event.slotKey, event.kind)
            is ShiftingExecuteEvent.Answer ->
                (if (event.section == SECTION_HIGH_PRIORITY) highPriority else completion).answer(event.questionId, event.value)
            ShiftingExecuteEvent.MarkDone -> markDone()
            ShiftingExecuteEvent.Back -> Unit // navigation — handled by the nav host.
            ShiftingExecuteEvent.NavigationHandled -> _state.update { it.copy(returnToActions = false) }
        }
    }

    private fun loadMovement() {
        viewModelScope.launch {
            migrateLegacyHighPrioritySteps()
            draft = drafts.find(CaptureFlow.SHIFTING, shiftingEventId)
            draft.submitOutboxItemId?.let(::observeOutboxItem)
            val cached = repo.findCached(shiftingEventId)
            if (cached == null) {
                _state.update { it.copy(loading = false, notFound = true, canComplete = false) }
                return@launch
            }
            destinationShedId = cached.destinationShedId
            destinationParkLabel = cached.destinationParkName.ifBlank { cached.destinationParkId }
            destinationLocationLabel = cached.destinationOperationalLocationDisplay.ifBlank { cached.destinationShedName }
            val high = cached.priority.equals("high", ignoreCase = true)
            // The PINNED cards; a row cached before this build carries none and runs the seed.
            completion.applyCard(cached.sop ?: ShiftingSopSeed.completion(), FeedSopSlotController.CARD_RANK_LIVE, source = if (cached.sop != null) "pinned" else "seeded")
            if (high) highPriority.applyCard(cached.highPrioritySop ?: ShiftingSopSeed.highPriority(), FeedSopSlotController.CARD_RANK_LIVE, source = if (cached.highPrioritySop != null) "pinned" else "seeded")
            // A rework keeps the answers the rejected completion recorded.
            cached.sopAnswers?.let { stored ->
                val flat = stored.flattenAnswers()
                completion.seedAnswers(flat)
                if (high) highPriority.seedAnswers(flat)
            }
            applyResets(cached, high)
            completion.start()
            if (high) highPriority.start()
            loaded = true
            _state.update { current -> cached.toUiState(current, high) }
            recompute()
        }
    }

    /**
     * Rework and Feed Config resets, each applied ONCE: the cutoff is written to the draft store, so a
     * clip recorded after the reset survives re-opening the movement while every older take stays
     * history.
     */
    private suspend fun applyResets(cached: CountsShiftingPendingExecutionItemDto, high: Boolean) {
        val markers = drafts.find(CaptureFlow.SHIFTING_MARKERS, shiftingEventId).proofs
        if (cached.verificationState == "rejected") {
            val cutoff = markers[MARKER_REWORK_CUTOFF]?.toLongOrNull() ?: nowMs().also {
                drafts.putProof(CaptureFlow.SHIFTING_MARKERS, shiftingEventId, MARKER_REWORK_CUTOFF, it.toString())
                drafts.putSubmit(CaptureFlow.SHIFTING, shiftingEventId, null, null)
                draft = drafts.find(CaptureFlow.SHIFTING, shiftingEventId)
                _state.update { s -> s.copy(videoMessage = REWORK_REQUIRED) }
            }
            if (markers[MARKER_REWORK_CUTOFF] == null) {
                completion.resetSlots(cutoff)
                highPriority.resetSlots(cutoff)
            } else {
                // Re-opened mid-rework: only older takes are history; a clip recorded since stays.
                completion.ignoreCapturesBefore(cutoff)
                highPriority.ignoreCapturesBefore(cutoff)
            }
        }
        if (high) {
            val fingerprint = cached.feedRequirement?.fingerprint.orEmpty()
            val stored = markers[MARKER_FEED_FINGERPRINT]
            when {
                stored == null -> if (fingerprint.isNotBlank()) {
                    drafts.putProof(CaptureFlow.SHIFTING_MARKERS, shiftingEventId, MARKER_FEED_FINGERPRINT, fingerprint)
                }
                stored != fingerprint && fingerprint.isNotBlank() -> {
                    val cutoff = nowMs()
                    drafts.putProof(CaptureFlow.SHIFTING_MARKERS, shiftingEventId, MARKER_FEED_FINGERPRINT, fingerprint)
                    drafts.putProof(CaptureFlow.SHIFTING_MARKERS, shiftingEventId, MARKER_FEED_CUTOFF, cutoff.toString())
                    drafts.putSubmit(CaptureFlow.SHIFTING, shiftingEventId, null, null)
                    draft = drafts.find(CaptureFlow.SHIFTING, shiftingEventId)
                    highPriority.resetSlots(cutoff)
                    _state.update { it.copy(result = CountsWriteResultUi(), videoMessage = FEED_CONFIG_REFRESHED) }
                }
                else -> markers[MARKER_FEED_CUTOFF]?.toLongOrNull()?.let { highPriority.ignoreCapturesBefore(it) }
            }
        }
    }

    /** Clips an older build stored under the movement as `packing` / `feeding` move to the high card's entity. */
    private suspend fun migrateLegacyHighPrioritySteps() {
        val legacy = drafts.find(CaptureFlow.SHIFTING, shiftingEventId)
        listOf("packing", "feeding").forEach { step ->
            val outbox = legacy.proofs[step] ?: return@forEach
            drafts.putProof(CaptureFlow.SHIFTING, highEntity(shiftingEventId), step, outbox, legacy.fingerprint)
            drafts.clearProof(CaptureFlow.SHIFTING, shiftingEventId, step)
            val legacyFingerprint = legacy.fingerprint
            if (legacyFingerprint != null && drafts.find(CaptureFlow.SHIFTING_MARKERS, shiftingEventId).proofs[MARKER_FEED_FINGERPRINT] == null) {
                drafts.putProof(CaptureFlow.SHIFTING_MARKERS, shiftingEventId, MARKER_FEED_FINGERPRINT, legacyFingerprint)
            }
        }
    }

    private fun recompute() {
        val c = completion.state.value
        val h = highPriority.state.value
        _state.update { st ->
            val high = st.highPriority
            val ready = loaded && c.cardReady() && (!high || (st.feedConfigStatus == "ready" && h.cardReady())) && !st.result.isCommitted
            st.copy(completionCard = c.toCountsUi(), highPriorityCard = h.toCountsUi(), canComplete = ready)
        }
    }

    private fun markDone() {
        val current = _state.value
        if (!current.canComplete) return
        val completionRefs = completion.submitRefs()
        val highRefs = if (current.highPriority) highPriority.submitRefs() else emptyMap()
        if (completionRefs == null || highRefs == null) {
            _state.update { it.copy(videoMessage = VIDEO_REQUIRED) }
            return
        }
        val slotProofs = LinkedHashMap<String, FeedSlotProofSourcePayload>().apply { putAll(completionRefs); putAll(highRefs) } // mobile-guard:ignore: local to one submit, bounded by the pinned cards (at most 8 completion + 8 high-priority slots)
        val answers = JsonObject(completion.answersJson() + if (current.highPriority) highPriority.answersJson() else JsonObject(emptyMap()))
        val legacyMirror = slotProofs[ShiftingSopSeed.SLOT_SHIFTING_VIDEO]?.outboxItemId ?: slotProofs.values.first().outboxItemId.orEmpty()
        viewModelScope.launch {
            if (current.result.isCorrectable) {
                // A terminal rejection committed nothing server-side: the corrected request is a NEW
                // operation and gets a fresh key; transport retries reuse the stored one.
                statusJob?.cancel()
                statusJob = null
                drafts.putSubmit(CaptureFlow.SHIFTING, shiftingEventId, "$COMPLETE_KEY_PREFIX:$shiftingEventId:${UUID.randomUUID()}", null)
                draft = drafts.find(CaptureFlow.SHIFTING, shiftingEventId)
            }
            // STABLE per movement and durable. A seeded submission with no answers keeps the pre-SOP
            // key; answers or authored slots fold their digest in, so a different submission can
            // never collide with a stored one.
            val seededOnly = slotProofs.keys.all { it in ShiftingSopSeed.LEGACY_STEPS.values } && answers.isEmpty()
            val completeIdempotencyKey = draft.submitIdempotencyKey
                ?: if (seededOnly) "$COMPLETE_KEY_PREFIX:$shiftingEventId"
                else "$COMPLETE_KEY_PREFIX:$shiftingEventId:${completion.submitDigest(slotProofs, answers)}"
            if (draft.submitIdempotencyKey == null) {
                drafts.putSubmit(CaptureFlow.SHIFTING, shiftingEventId, completeIdempotencyKey, null)
                draft = drafts.find(CaptureFlow.SHIFTING, shiftingEventId)
            }
            val result = syncRepository.enqueueShiftingComplete(
                groupKey = shiftingEventId,
                idempotencyKey = completeIdempotencyKey,
                proofOutboxItemId = legacyMirror,
                feedPackingProofOutboxItemId = slotProofs[ShiftingSopSeed.SLOT_PACKING_VIDEO]?.outboxItemId,
                feedGivenProofOutboxItemId = slotProofs[ShiftingSopSeed.SLOT_FEEDING_VIDEO]?.outboxItemId,
                feedConfigFingerprint = current.feedConfigFingerprint,
                slotProofs = slotProofs,
                answers = answers,
            )
            when (result) {
                is AppResult.Ok -> {
                    drafts.putSubmit(CaptureFlow.SHIFTING, shiftingEventId, completeIdempotencyKey, result.value)
                    draft = drafts.find(CaptureFlow.SHIFTING, shiftingEventId)
                    observeOutboxItem(result.value)
                    analytics.track(AnalyticsEvents.COUNTS_SHIFTING_EXECUTE_COMPLETED, mapOf("slot_count" to slotProofs.size.toString(), "answer_count" to answers.size.toString()))
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "shifting complete enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.COUNTS_WRITE_FAILURE,
                        mapOf(AnalyticsEvents.Params.KIND to "shifting_complete", AnalyticsEvents.Params.REASON to result.message),
                    )
                    _state.update { it.copy(result = CountsWriteResultUi(CountsWriteStatus.FAILED, result.message), canComplete = true) }
                }
            }
        }
    }

    private fun shiftingProofCaption(title: String): String =
        proofOverlayContextLine(
            feature = "Shifting",
            parkLabel = destinationParkLabel,
            locationLabel = destinationLocationLabel.ifBlank { destinationShedId },
            extraLabel = title,
        )

    private fun eventProps(section: String, slotKey: String?, action: String): Map<String, String> = buildMap {
        put(AnalyticsEvents.Params.SOURCE, SCREEN_SHIFTING_EXECUTE)
        put(AnalyticsEvents.Params.KIND, KIND_SHIFTING_COMPLETE)
        put(AnalyticsEvents.Params.ACTION, action)
        put(AnalyticsEvents.Params.SHED_ID, destinationShedId)
        put(PARAM_GROUP_KEY, shiftingEventId)
        put("section", section)
        slotKey?.let {
            put(AnalyticsEvents.Params.FIELD, it)
            put(PARAM_SLOT_KEY, it)
        }
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
                        val writeResult = item.toWriteResult(QUEUED_MESSAGE, SYNCED_MESSAGE)
                        it.copy(result = writeResult)
                    }
                    recompute()
                    if (item.status == SyncItemStatus.SUCCEEDED) {
                        // Keep the cached task until the server accepts it. A feed-config conflict
                        // must remain reopenable so the operator can refresh the new ration.
                        repo.forgetExecuted(shiftingEventId)
                        // The movement is done: its drafts have nothing left to protect.
                        drafts.clear(CaptureFlow.SHIFTING, shiftingEventId)
                        drafts.clear(CaptureFlow.SHIFTING, highEntity(shiftingEventId))
                        drafts.clear(CaptureFlow.SHIFTING_MARKERS, shiftingEventId)
                        _state.update { it.copy(returnToActions = true, submissionNotice = SYNCED_MESSAGE) }
                    }
                }
        }
    }

    private fun CountsShiftingPendingExecutionItemDto.toUiState(current: ShiftingExecuteUiState, high: Boolean): ShiftingExecuteUiState =
        current.copy(
            loading = false,
            notFound = false,
            // Backend-composed label first, so the operator walking the animals sees the PEN.
            sourceLabel = sourceOperationalLocationDisplay?.takeIf { it.isNotBlank() }
                ?: (sourceShedName ?: sourceParkName)?.takeIf { it.isNotBlank() } ?: UNKNOWN_LOCATION,
            destinationLabel = destinationOperationalLocationDisplay.takeIf { it.isNotBlank() }
                ?: destinationShedName.takeIf { it.isNotBlank() }
                ?: destinationParkName.takeIf { it.isNotBlank() } ?: UNKNOWN_LOCATION,
            priority = priority.titleCase(),
            category = category.titleCase(),
            animalCount = animalCount,
            animals = animals.map { ShiftingExecuteAnimalUi(it.goatId, it.displayId, it.tag) },
            animalsTruncated = animalsTruncated,
            highPriority = high,
            feedConfigStatus = feedRequirement?.status ?: if (high) "blocked" else "not_required",
            feedConfigBlockedReason = feedRequirement?.blockedReason,
            feedConfigFingerprint = feedRequirement?.fingerprint,
            feedTargetStage = feedRequirement?.targetManagementStage,
            feedItems = feedRequirement?.items?.map { ShiftingFeedItemUi(it.feedItemLabel, it.quantityGrams) }.orEmpty(),
        )

    private fun String.titleCase(): String =
        if (isEmpty()) this else replaceFirstChar { if (it.isLowerCase()) it.titlecase(Locale.getDefault()) else it.toString() }

    internal companion object {
        const val ARG_SHIFTING_EVENT_ID = "shifting_event_id"
        const val COMPLETE_KEY_PREFIX = "counts-shifting-complete"
        const val SCREEN_SHIFTING_EXECUTE = "shifting_execute"
        const val KIND_SHIFTING_COMPLETE = "shifting_complete"
        const val PARAM_GROUP_KEY = "group_key"
        const val PARAM_SLOT_KEY = "slot_key"
        const val MARKER_REWORK_CUTOFF = "rework_cutoff_ms"
        const val MARKER_FEED_FINGERPRINT = "feed_fingerprint"
        const val MARKER_FEED_CUTOFF = "feed_cutoff_ms"
        const val UNKNOWN_LOCATION = "—"
        const val QUEUED_MESSAGE = "Saved on this phone. The move will sync automatically."
        const val SYNCED_MESSAGE = "Completion recorded. The move applies when Park Head approval is also present."
        const val VIDEO_REQUIRED = "Record every required capture first — it is the evidence for this task."
        const val FEED_CONFIG_REFRESHED = "Feed configuration changed. Record the high-priority captures again for the updated ration."
        const val REWORK_REQUIRED = "Verification requested rework. Record fresh evidence before submitting again."

        fun highEntity(shiftingEventId: String): String = "$shiftingEventId:high"

        internal var clock: () -> Long = System::currentTimeMillis
        fun nowMs(): Long = clock()
    }
}

/** A card is ready when every compulsory slot is captured and durable, nothing failed, and required answers are in. */
private fun FeedSopCardUi.cardReady(): Boolean =
    slots.isNotEmpty() &&
        slots.filter { it.required }.all { it.readyForSubmit } &&
        slots.filter { it.captured }.all { it.status.isQueuedForSubmit() } &&
        requiredAnswersGiven

private fun FeedSopCardUi.toCountsUi(): sg.mesha.goatos.feature.counts.ShiftingSopCardUi =
    sg.mesha.goatos.feature.counts.ShiftingSopCardUi(
        instruction = instruction,
        slots = slots.map {
            sg.mesha.goatos.feature.counts.ShiftingSopSlotUi(
                key = it.slotKey,
                title = it.title,
                hint = it.hint,
                kind = it.captureKind,
                required = it.required,
                captured = it.captured,
                capturedPhoto = it.capturedKind == FeedSlotCaptureKind.PHOTO,
                isCapturing = it.isCapturing,
                failed = it.status == FeedDistributionProofStatus.FAILED,
                message = it.message,
            )
        },
        questions = questions.filter { appliesTo(it) }.map { q ->
            sg.mesha.goatos.feature.counts.ShiftingSopQuestionUi(
                id = q.id, kind = q.kind, title = q.title, hint = q.hint, required = q.required,
                options = q.options, allowOther = q.allowOther, unit = q.unit,
            )
        },
        answers = answers,
    )

/** The stored answers JSON as the text map the controllers edit (multi = comma-joined values). */
internal fun JsonObject.flattenAnswers(): Map<String, String> = buildMap {
    this@flattenAnswers.forEach { (id, value) ->
        when (value) {
            is JsonArray -> put(id, value.mapNotNull { (it as? JsonPrimitive)?.contentOrNull }.joinToString(","))
            is JsonPrimitive -> value.contentOrNull?.let { put(id, it) }
            else -> Unit
        }
    }
}
