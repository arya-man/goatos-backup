package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import javax.inject.Inject
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.conflate
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.drop
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.capture.ProofCapturePrompt
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.MilkFeedingRepository
import sg.mesha.goatos.core.data.CaptureDraft
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.CaptureFlow
import sg.mesha.goatos.core.data.capture.buildMilkFeedingEvidenceSlot
import sg.mesha.goatos.core.data.capture.EvidenceSlot
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofFlow
import sg.mesha.goatos.core.data.capture.ProofIdentity
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.MilkFeedingAnswersDto
import sg.mesha.goatos.core.network.dto.MilkFeedingPageDto
import sg.mesha.goatos.core.network.dto.MilkFeedingNewRefusalDto
import sg.mesha.goatos.core.network.dto.MilkFeedingWatchlistAnswerDto
import sg.mesha.goatos.feature.counts.MilkFeedingEvent
import sg.mesha.goatos.feature.counts.MilkFeedingCardUi
import sg.mesha.goatos.feature.counts.MilkFeedingListEvent
import sg.mesha.goatos.feature.counts.MilkFeedingListUiState
import sg.mesha.goatos.feature.counts.MilkFeedingProofUi
import sg.mesha.goatos.feature.counts.MilkFeedingUiState
import sg.mesha.goatos.feature.counts.MilkFeedingWatchlistUi
import sg.mesha.goatos.feature.counts.MilkPreparationChipUi

private data class MilkFeedingDraft(
    val watchlistAnswers: Map<String, Boolean> = emptyMap(),
    val total: String = "", val attempt1: String = "", val attempt2: String = "", val ids: String = "", val remarks: String = "", val udder: String = "", val ors: String = "",
    val proofs: List<MilkFeedingProofUi> = listOf(MilkFeedingProofUi("clean_bottles", "Show clean bottles"), MilkFeedingProofUi("mixing_and_filling", "Mix milk and fill required bottles")),
    val submitting: Boolean = false, val queued: Boolean = false, val message: String? = null,
)

/**
 * The typed half of the draft, flattened to the field -> value map the durable store holds.
 *
 * Only operator input is persisted. Transient UI state (submitting/queued/message) and the proof
 * rows are deliberately excluded: the proofs have their own durable rows, and a stale "uploading"
 * flag restored from disk would lie about work that is no longer in flight.
 */
private fun MilkFeedingDraft.toAnswers(): Map<String, String> = buildMap {
    put(FIELD_TOTAL, total)
    put(FIELD_ATTEMPT_1, attempt1)
    put(FIELD_ATTEMPT_2, attempt2)
    put(FIELD_IDS, ids)
    put(FIELD_REMARKS, remarks)
    put(FIELD_UDDER, udder)
    put(FIELD_ORS, ors)
    watchlistAnswers.forEach { (goatId, drank) -> put("$WATCHLIST_PREFIX$goatId", drank.toString()) }
}

private fun MilkFeedingDraft.restoredFrom(answers: Map<String, String>): MilkFeedingDraft = copy(
    total = answers[FIELD_TOTAL].orEmpty(),
    attempt1 = answers[FIELD_ATTEMPT_1].orEmpty(),
    attempt2 = answers[FIELD_ATTEMPT_2].orEmpty(),
    ids = answers[FIELD_IDS].orEmpty(),
    remarks = answers[FIELD_REMARKS].orEmpty(),
    udder = answers[FIELD_UDDER].orEmpty(),
    ors = answers[FIELD_ORS].orEmpty(),
    watchlistAnswers = answers
        .filterKeys { it.startsWith(WATCHLIST_PREFIX) }
        .mapNotNull { (key, value) -> value.toBooleanStrictOrNull()?.let { key.removePrefix(WATCHLIST_PREFIX) to it } }
        .toMap(),
)

private val MILK_FEEDING_IST: ZoneId = ZoneId.of("Asia/Kolkata")
private val MILK_FEEDING_DAY_LABEL: DateTimeFormatter = DateTimeFormatter.ofPattern("d MMM")

private const val FIELD_TOTAL = "total"
private const val FIELD_ATTEMPT_1 = "attempt1"
private const val FIELD_ATTEMPT_2 = "attempt2"
private const val FIELD_IDS = "ids"
private const val FIELD_REMARKS = "remarks"
private const val FIELD_UDDER = "udder"
private const val FIELD_ORS = "ors"
private const val WATCHLIST_PREFIX = "watchlist:"

@HiltViewModel
class MilkFeedingListViewModel @Inject constructor(
    private val repo: MilkFeedingRepository,
    drafts: CaptureDraftRepository,
) : ViewModel() {
    // MutableStateFlow + flatMapLatest re-subscribe, mirroring the MilkPreparationListViewModel
    // fix: a plain `val` here never re-subscribed repo.observe() when the chevrons were tapped,
    // which was the device-reported bug (the earlier fix only reached Preparation, not Feeding).
    private val feedingDate = MutableStateFlow(LocalDate.now(MILK_FEEDING_IST).toString())
    private val refreshing = MutableStateFlow(false)
    private val selectedFilter = MutableStateFlow("all")

    @OptIn(ExperimentalCoroutinesApi::class)
    val state: StateFlow<MilkFeedingListUiState> = feedingDate
        .flatMapLatest { dateStr ->
            combine(
                repo.observe(dateStr),
                refreshing,
                selectedFilter,
                // ONE bounded Room observation for the whole page, never a per-row lookup — a per-card
                // draft read behind a list is the N+1 shape (docs/decisions/mobile-data-fetch-anti-patterns.md).
                drafts.observeProgress(CaptureFlow.MILK_FEEDING),
            ) { resource, busy, selected, capturedByTask ->
                buildMilkFeedingListUi(resource.data, selected, capturedByTask, selectedDate = dateStr).copy(
                    selectedDate = dateStr,
                    isRefreshing = busy,
                    lastSyncedAt = resource.lastSyncedAt,
                    isOffline = resource.data == null,
                )
            }
        }
        .stateIn(
            viewModelScope,
            SharingStarted.WhileSubscribed(5_000),
            MilkFeedingListUiState(dateLabel = milkFeedingDateLabel(feedingDate.value)),
        )
    init { refresh() }
    fun onEvent(event: MilkFeedingListEvent) {
        when (event) {
            MilkFeedingListEvent.Refresh -> refresh()
            is MilkFeedingListEvent.SelectFilter -> selectedFilter.value = event.key
            is MilkFeedingListEvent.NavigateDate -> navigateDate(event.delta)
            is MilkFeedingListEvent.OpenTask, MilkFeedingListEvent.Back -> Unit
        }
    }

    /** Business dates are capped at today IST, mirroring WorkflowListViewModel.selectDate — future
     *  days have no feeding tasks by definition. */
    private fun navigateDate(delta: Int) {
        val currentDate = LocalDate.parse(feedingDate.value)
        val today = LocalDate.now(MILK_FEEDING_IST)
        val requested = currentDate.plusDays(delta.toLong())
        val capped = if (requested.isAfter(today)) today else requested
        if (capped.toString() == feedingDate.value) return
        feedingDate.value = capped.toString()
        refresh()
    }

    private fun refresh() = viewModelScope.launch { refreshing.value = true; repo.refresh(feedingDate.value); refreshing.value = false }
}

/** "Today · 27 Jul" only when [dateIso] IS today IST; otherwise just the formatted date — matching
 *  the WorkflowListViewModel date-bar convention (a past/future selection is never mislabeled Today). */
private fun milkFeedingDateLabel(dateIso: String): String {
    // exception:exempt display-only fallback — unparseable date renders verbatim; nothing actionable to record
    val parsed = runCatching { LocalDate.parse(dateIso) }.getOrNull() ?: return dateIso
    val label = parsed.format(MILK_FEEDING_DAY_LABEL)
    return if (dateIso == LocalDate.now(MILK_FEEDING_IST).toString()) "Today · $label" else label
}

internal fun buildMilkFeedingListUi(
    page: MilkFeedingPageDto?,
    selectedFilter: String,
    capturedByTask: Map<String, Int> = emptyMap(),
    selectedDate: String = "",
): MilkFeedingListUiState {
    val allCards = page?.items.orEmpty()
        // taskId is the same key the detail screen writes its draft under (MilkFeedingViewModel).
        .map { MilkFeedingCardUi(it.taskId, it.parkLabel, it.sessionNo, it.dueTime, it.headCount, it.verificationStatus, it.reworkReason, it.available, it.blockedReason, capturedByTask[it.taskId] ?: 0) }
        .sortedWith(compareBy<MilkFeedingCardUi> { it.parkLabel }.thenBy { it.sessionNo })
    val cards = if (selectedFilter == "all") allCards else allCards.filter {
        if (selectedFilter == "not_submitted") it.status == selectedFilter && it.available else it.status == selectedFilter
    }
    val counts = allCards.groupingBy { it.status }.eachCount()
    val availableNotSubmitted = allCards.count { it.status == "not_submitted" && it.available }
    val needAction = allCards.count { it.canOpen }
    // The SELECTED date drives the label, not the page's own feedingDate echo — a page still
    // carrying yesterday's cached data (offline) must not silently relabel the date the operator
    // navigated to.
    val dateLabel = milkFeedingDateLabel(selectedDate.ifBlank { page?.feedingDate.orEmpty() })
    return MilkFeedingListUiState(
        subtitle = "${allCards.size} farm sessions · $needAction need action",
        dateLabel = dateLabel,
        chips = listOf(
            MilkPreparationChipUi("all", "All", allCards.size),
            MilkPreparationChipUi("not_submitted", "Need action", availableNotSubmitted),
            MilkPreparationChipUi("pending_verification", "In review", counts["pending_verification"] ?: 0),
            MilkPreparationChipUi("completed", "Completed", counts["completed"] ?: 0),
            MilkPreparationChipUi("rework", "Rework", counts["rework"] ?: 0),
        ),
        selectedFilter = selectedFilter,
        cards = cards,
        emptyMessage = if (cards.isEmpty()) "No Milk Feeding work in this status." else null,
    )
}

@HiltViewModel
class MilkFeedingViewModel @Inject constructor(
    private val repo: MilkFeedingRepository,
    private val sync: SyncRepository,
    private val capture: ProofCaptureSource,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val drafts: CaptureDraftRepository,
    private val analytics: AnalyticsPort,
    private val saved: SavedStateHandle,
) : ViewModel() {
    private val taskId = saved.get<String>(ARG_TASK_ID).orEmpty()
    private val feedingDate = saved.get<String>(ARG_FEEDING_DATE) ?: LocalDate.now(ZoneId.of("Asia/Kolkata")).toString()
    // Proof/draft field-key building now routes through the canonical ProofIdentity/EvidenceSlot
    // model (see evidenceSlot() below). identity.taskId is set to the EXISTING groupKey() literal
    // and fieldKey to the EXISTING "milk_feeding_$code" literal, so the strings written to
    // Room/outbox are byte-for-byte unchanged — only the plumbing carrying them is canonical now.
    private val submitKey = DraftIdempotencyKey(saved, "milkFeeding.submitKey.$taskId", "milk-feeding-submit")
    private val proofKeys = listOf("clean_bottles", "mixing_and_filling").associateWith { DraftIdempotencyKey(saved, "milkFeeding.proofKey.$taskId.$it", "milk-feeding-$it") }
    private val submitOutboxItemId = DraftOutboxItemId(saved, "milkFeeding.submitOutboxItemId.$taskId")
    private val draft = MutableStateFlow(MilkFeedingDraft())

    /**
     * The task's DURABLE captured evidence and submit key, in the shared capture-draft store. These
     * used to live in [saved], which dies with the nav backstack entry — so Back + re-entry lost the
     * recorded videos and asked for them again while the first clips uploaded anyway.
     */
    private var captureDraft = CaptureDraft()
    private var statusJob: Job? = null

    val state: StateFlow<MilkFeedingUiState> = combine(repo.observe(feedingDate), draft) { resource, local ->
        val task = resource.data?.items?.firstOrNull { it.taskId == taskId }
        MilkFeedingUiState(
            taskId = taskId, parkId = task?.parkId.orEmpty(), parkLabel = task?.parkLabel.orEmpty(), feedingDate = task?.feedingDate ?: feedingDate,
            sessionNo = task?.sessionNo ?: 0, dueTime = task?.dueTime.orEmpty(), status = task?.verificationStatus ?: "not_submitted",
            watchlist = task?.watchlist.orEmpty().map { kid -> MilkFeedingWatchlistUi(kid.goatId, "Kid ${kid.goatId} · tracked since ${kid.addedDate}", local.watchlistAnswers[kid.goatId]) },
            totalKidsFed = local.total, attempt1NotDrinking = local.attempt1, attempt2NotDrinking = local.attempt2, newRefusalIds = local.ids,
            remarks = local.remarks, udderNotDrinking = local.udder, orsNotDrinking = local.ors, proofs = local.proofs,
            submitting = local.submitting, queued = local.queued, message = local.message,
            available = task?.available ?: false, blockedReason = task?.blockedReason.orEmpty(),
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), MilkFeedingUiState(taskId = taskId, feedingDate = feedingDate))

    init {
        analytics.track(
            AnalyticsEvents.MILK_FEEDING_OPENED,
            mapOf(AnalyticsEvents.Params.ITEM_ID to taskId),
        )
        viewModelScope.launch {
            captureDraft = drafts.find(CaptureFlow.MILK_FEEDING, taskId)
            draft.update { current ->
                current
                    .restoredFrom(captureDraft.answers)
                    .copy(proofs = current.proofs.map { it.copy(captured = captureDraft.hasProof(it.code)) })
            }
            // Restore BEFORE the writer starts, or the empty initial state would immediately
            // overwrite the answers just read back.
            persistAnswers()
            repo.refresh(feedingDate)
            // Restore the submit outbox item ID from the durable store so process death doesn't
            // lose the in-flight submission state. If one exists, observe it for status changes.
            // Draft store may lag or be pruned — it must never CLOBBER a SavedStateHandle-restored
            // in-flight id back to null (that reopened a queued submit for editing; caught by the
            // process-death regression test 2026-08-16).
            captureDraft.submitOutboxItemId?.let { submitOutboxItemId.value = it }
            submitOutboxItemId.value?.let(::observeOutboxItem)
        }
    }

    /**
     * Mirrors every typed answer into the durable draft so Back + re-entry restores the sheet rather
     * than asking the operator to key it in again beside videos they can already see are recorded.
     */
    private fun persistAnswers() = viewModelScope.launch {
        draft
            .map { it.toAnswers() }
            .distinctUntilChanged()
            .drop(1)
            // conflate, NOT debounce: a time window would drop the pending write when Back cancels
            // viewModelScope — the very loss this exists to prevent. Conflate coalesces a burst of
            // keystrokes into the latest value without delaying it.
            .conflate()
            .collect { drafts.putAnswers(CaptureFlow.MILK_FEEDING, taskId, it) }
    }

    fun onEvent(event: MilkFeedingEvent) {
        // Block all edits if the task is not available or if this submit is already in-flight/completed.
        // `liveStatusLocked` reads the LIVE backend/Room verification_status (task?.verificationStatus,
        // wired straight into state.status) rather than the local submitOutboxItemId latch: a fresh
        // ViewModel that never itself queued a submit (no local latch) must still render read-only the
        // moment the server already has a submission recorded for this task — the applyLiveStatus
        // pattern from FeedDistributionCompleteViewModel, where live truth wins over remembered local
        // state. "not_submitted" and "rework" are the only statuses this screen may edit under.
        val isSubmitInFlightOrCompleted = submitOutboxItemId.value != null
        val liveStatusLocked = state.value.status !in EDITABLE_STATUSES
        if (event != MilkFeedingEvent.Back && (!state.value.available || isSubmitInFlightOrCompleted || liveStatusLocked)) return
        when (event) {
            is MilkFeedingEvent.SetWatchlistAnswer -> draft.update { it.copy(watchlistAnswers = it.watchlistAnswers + (event.goatId to event.drank)) }
            is MilkFeedingEvent.SetNumber -> draft.update { current -> when (event.field) { "total" -> current.copy(total = event.value); "attempt1" -> current.copy(attempt1 = event.value); "attempt2" -> current.copy(attempt2 = event.value); "udder" -> current.copy(udder = event.value); else -> current.copy(ors = event.value) } }
            is MilkFeedingEvent.SetText -> draft.update { if (event.field == "ids") it.copy(ids = event.value) else it.copy(remarks = event.value) }
            is MilkFeedingEvent.CaptureProof -> captureProof(event.code)
            is MilkFeedingEvent.ReCaptureProof -> reCaptureProof(event.code)
            MilkFeedingEvent.Submit -> submit()
            MilkFeedingEvent.Back -> Unit
        }
    }

    /**
     * Replaces one proof's clip: Manohar ordering ensures the new proof captures and stores
     * BEFORE the old one is deleted, so a cancelled or failed re-capture keeps the existing
     * good proof (the old "proof disappeared" defect).
     */
    private fun reCaptureProof(code: String) = viewModelScope.launch {
        val oldProofOutboxId = captureDraft.proofs[code]
        proofKeys[code]?.invalidate()
        draft.update { it.copy(proofs = it.proofs.map { row -> if (row.code == code) row.copy(captured = false) else row }) }
        val current = state.value
        val proof = current.proofs.firstOrNull { it.code == code } ?: return@launch
        draft.update { it.copy(proofs = it.proofs.map { row -> if (row.code == code) row.copy(capturing = true) else row }) }
        val caption = proofOverlayContextLine(
            feature = "Milk feeding",
            parkLabel = current.parkLabel.ifBlank { current.parkId },
            extraLabel = listOf("Session ${current.sessionNo}", proof.label).filter { it.isNotBlank() }.joinToString(" . "),
        )
        val video = capture.captureVideo(
            ProofCaptureContext(
                title = caption,
                primaryTag = current.parkLabel.ifBlank { current.parkId },
                workLabel = proof.label,
                prompt = ProofCapturePrompt.MILK_FEEDING,
                headerTitle = proof.label,
            ),
        )
        if (video == null) {
            draft.update { it.copy(proofs = it.proofs.map { row -> if (row.code == code) row.copy(capturing = false) else row }) }
            return@launch
        }
        val slot = evidenceSlot(code)
        when (val result = proofCaptureRepository.captureReplacingLatest(
            slot = slot,
            subject = ProofSubject.TASK,
            subjectId = taskId,
            localUri = video.localUri,
            mimeType = video.mimeType,
            caption = caption,
            scopeType = "park",
            scopeId = current.parkId,
            capturedStartMs = video.startedAtMs,
            capturedEndMs = video.endedAtMs,
            capturedByPrincipalId = null,
            proofPolicy = milkParkProofPolicy(video.captureSource),
            awaitUploadEnqueue = true,
            uploadGroupKey = groupKey(),
        )) {
            is AppResult.Ok -> {
                val proofOutboxId = result.value.outboxItemId
                if (proofOutboxId.isNullOrBlank()) {
                    draft.update { it.copy(message = "Proof upload could not be queued", proofs = it.proofs.map { row -> if (row.code == code) row.copy(capturing = false) else row }) }
                    return@launch
                }
                // NEW PROOF is durable before we remove the old one, so a process death here
                // cannot lose the clip (Manohar ordering).
                drafts.putProof(CaptureFlow.MILK_FEEDING, taskId, code, proofOutboxId)
                captureDraft = drafts.find(CaptureFlow.MILK_FEEDING, taskId)
                draft.update { it.copy(proofs = it.proofs.map { row -> if (row.code == code) row.copy(captured = true, capturing = false) else row }) }
                // ONLY NOW, after the new proof is stored, delete the old one so it never
                // reaches the verifier as a duplicate.
                oldProofOutboxId?.let { sync.deleteOutboxItem(it) }
            }
            is AppResult.Err -> {
                draft.update { it.copy(message = result.message, proofs = it.proofs.map { row -> if (row.code == code) row.copy(capturing = false) else row }) }
                // On error, keep the old proof: don't remove it.
            }
        }
    }

    private fun captureProof(code: String) = viewModelScope.launch {
        val proof = state.value.proofs.firstOrNull { it.code == code } ?: return@launch
        analytics.track(
            AnalyticsEvents.MILK_FEEDING_PROOF_CAPTURE_ATTEMPT,
            mapOf(
                AnalyticsEvents.Params.ITEM_ID to taskId,
                AnalyticsEvents.Params.FIELD to code,
            ),
        )
        draft.update { it.copy(proofs = it.proofs.map { row -> if (row.code == code) row.copy(capturing = true) else row }) }
        val current = state.value
        val caption = proofOverlayContextLine(
            feature = "Milk feeding",
            parkLabel = current.parkLabel.ifBlank { current.parkId },
            extraLabel = listOf("Session ${current.sessionNo}", proof.label).filter { it.isNotBlank() }.joinToString(" . "),
        )
        val video = capture.captureVideo(
            ProofCaptureContext(
                title = caption,
                primaryTag = current.parkLabel.ifBlank { current.parkId },
                workLabel = proof.label,
                prompt = ProofCapturePrompt.MILK_FEEDING,
                headerTitle = proof.label,
            ),
        )
        if (video == null) {
            analytics.track(
                AnalyticsEvents.MILK_FEEDING_PROOF_CAPTURE_FAILURE,
                mapOf(
                    AnalyticsEvents.Params.ITEM_ID to taskId,
                    AnalyticsEvents.Params.FIELD to code,
                    AnalyticsEvents.Params.REASON to "cancelled",
                ),
            )
            draft.update { it.copy(proofs = it.proofs.map { row -> if (row.code == code) row.copy(capturing = false) else row }) }
            return@launch
        }
        val slot = evidenceSlot(code)
        when (val result = proofCaptureRepository.captureReplacingLatest(
            slot = slot,
            subject = ProofSubject.TASK,
            subjectId = taskId,
            localUri = video.localUri,
            mimeType = video.mimeType,
            caption = caption,
            scopeType = "park",
            scopeId = current.parkId,
            capturedStartMs = video.startedAtMs,
            capturedEndMs = video.endedAtMs,
            capturedByPrincipalId = null,
            proofPolicy = milkParkProofPolicy(video.captureSource),
            awaitUploadEnqueue = true,
            uploadGroupKey = groupKey(),
        )) {
            is AppResult.Ok -> {
                val proofOutboxId = result.value.outboxItemId
                if (proofOutboxId.isNullOrBlank()) {
                    analytics.track(
                        AnalyticsEvents.MILK_FEEDING_PROOF_CAPTURE_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.ITEM_ID to taskId,
                            AnalyticsEvents.Params.FIELD to code,
                            AnalyticsEvents.Params.REASON to "enqueue_failed",
                        ),
                    )
                    draft.update { it.copy(message = "Proof upload could not be queued", proofs = it.proofs.map { row -> if (row.code == code) row.copy(capturing = false) else row }) }
                    return@launch
                }
                // Durable BEFORE the UI flips, so a process death here cannot lose the clip.
                drafts.putProof(CaptureFlow.MILK_FEEDING, taskId, code, proofOutboxId)
                captureDraft = drafts.find(CaptureFlow.MILK_FEEDING, taskId)
                analytics.track(
                    AnalyticsEvents.MILK_FEEDING_PROOF_CAPTURE_SUCCESS,
                    mapOf(
                        AnalyticsEvents.Params.ITEM_ID to taskId,
                        AnalyticsEvents.Params.FIELD to code,
                    ),
                )
                draft.update { it.copy(proofs = it.proofs.map { row -> if (row.code == code) row.copy(captured = true, capturing = false) else row }) }
            }
            is AppResult.Err -> {
                proofKeys.getValue(code).invalidate()
                analytics.track(
                    AnalyticsEvents.MILK_FEEDING_PROOF_CAPTURE_FAILURE,
                    mapOf(
                        AnalyticsEvents.Params.ITEM_ID to taskId,
                        AnalyticsEvents.Params.FIELD to code,
                        AnalyticsEvents.Params.REASON to result.message,
                    ),
                )
                draft.update { it.copy(message = result.message, proofs = it.proofs.map { row -> if (row.code == code) row.copy(capturing = false) else row }) }
            }
        }
    }

    private fun submit() = viewModelScope.launch {
        val current = state.value
        if (!current.canSubmit) return@launch
        // Prevent double-submit: if one is already queued/in-flight/succeeded, don't submit again
        if (submitOutboxItemId.value != null) return@launch
        val ids = current.newRefusalIds.split(',').map(String::trim).filter(String::isNotBlank).distinct()
        val answers = MilkFeedingAnswersDto(
            watchlistAnswers = current.watchlist.map { MilkFeedingWatchlistAnswerDto(it.goatId, it.drankMilk == true) }, totalKidsFed = current.totalKidsFed.toInt(),
            attempt1NotDrinking = current.attempt1NotDrinking.toInt(), attempt2NotDrinking = current.attempt2NotDrinking.toIntOrNull() ?: 0,
            newRefusals = ids.map { MilkFeedingNewRefusalDto(it, current.remarks) }, udderMilkNotDrinking = current.udderNotDrinking.toIntOrNull() ?: 0, orsNotDrinking = current.orsNotDrinking.toIntOrNull() ?: 0,
        )
        val clean = captureDraft.proofs["clean_bottles"].orEmpty(); val mixing = captureDraft.proofs["mixing_and_filling"].orEmpty()
        draft.update { it.copy(submitting = true, message = null) }
        // STABLE per task and durable: a re-entered screen resends the SAME key.
        val submitIdempotencyKey = captureDraft.submitIdempotencyKey ?: "milk-feeding-submit:$taskId"
        if (captureDraft.submitIdempotencyKey == null) {
            drafts.putSubmit(CaptureFlow.MILK_FEEDING, taskId, submitIdempotencyKey, null)
            captureDraft = drafts.find(CaptureFlow.MILK_FEEDING, taskId)
        }
        analytics.track(
            AnalyticsEvents.MILK_FEEDING_SUBMITTED,
            mapOf(AnalyticsEvents.Params.ITEM_ID to current.taskId),
        )
        when (val result = sync.enqueueMilkFeedingSubmit(groupKey(), submitIdempotencyKey, current.taskId, current.parkId, current.feedingDate, current.sessionNo, answers, clean, mixing)) {
            is AppResult.Ok -> {
                drafts.putSubmit(CaptureFlow.MILK_FEEDING, taskId, submitIdempotencyKey, result.value)
                captureDraft = drafts.find(CaptureFlow.MILK_FEEDING, taskId)
                // Store the outbox item ID durably so process death doesn't lose the in-flight state
                submitOutboxItemId.value = result.value
                observeOutboxItem(result.value)
                draft.update { it.copy(submitting = false, queued = true, message = "Answers and proofs are uploading in background.") }
            }
            is AppResult.Err -> {
                analytics.track(AnalyticsEvents.MILK_FEEDING_FAILURE, mapOf(AnalyticsEvents.Params.REASON to result.message))
                // Clear both in-memory latch and persisted draft key so the operator can retry after process death
                submitOutboxItemId.value = null
                drafts.putSubmit(CaptureFlow.MILK_FEEDING, taskId, submitIdempotencyKey, null)
                captureDraft = drafts.find(CaptureFlow.MILK_FEEDING, taskId)
                draft.update { it.copy(submitting = false, message = result.message) }
            }
        }
    }

    internal fun groupKey() = "milk-feeding:${state.value.parkId}:$feedingDate:${state.value.sessionNo}"

    /** Canonical slot grain for a milk-feeding proof capture. identity.taskId/fieldKey resolve to
     *  the SAME strings [groupKey] / the raw `"milk_feeding_$code"` literal already produced, so
     *  routing captures through this slot changes no on-disk value. */
    internal fun evidenceSlot(code: String): EvidenceSlot =
        buildMilkFeedingEvidenceSlot(state.value.parkId, feedingDate, state.value.sessionNo, taskId, code)

    /**
     * Observes the submit outbox item's status to detect when submission completes or fails,
     * so the screen can update its state durably and reflect the result to the operator.
     */
    private fun observeOutboxItem(itemId: String) {
        statusJob?.cancel()
        statusJob = viewModelScope.launch {
            sync.observeItem(itemId)
                .filterNotNull()
                .distinctUntilChanged()
                .collect { item ->
                    when (item.status) {
                        SyncItemStatus.QUEUED, SyncItemStatus.IN_FLIGHT -> {
                            // Still uploading in background
                            draft.update { it.copy(submitting = false, queued = true, message = "Answers and proofs are uploading in background.") }
                        }
                        SyncItemStatus.SUCCEEDED -> {
                            draft.update { it.copy(submitting = false, queued = false, message = "Submission complete.") }
                        }
                        SyncItemStatus.FAILED -> {
                            // Clear submitOutboxItemId so the operator can retry. The outbox item reached
                            // terminal FAILED, but the submit button was blocked while it was in-flight.
                            // Without this clear, the button stays dead forever (see FeedPackingCompleteViewModel:434-436).
                            submitOutboxItemId.value = null
                            draft.update { it.copy(submitting = false, queued = false, message = item.lastError ?: "Submission failed. Please retry.") }
                        }
                    }
                }
        }
    }

    companion object {
        const val ARG_TASK_ID = "task_id"
        const val ARG_FEEDING_DATE = "feeding_date"
        private val EDITABLE_STATUSES = setOf("not_submitted", "rework")
    }
}
