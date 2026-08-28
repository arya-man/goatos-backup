package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale
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
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.filterNotNull
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
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.submittedGrainKey
import sg.mesha.goatos.core.data.MilkPreparationRepository
import sg.mesha.goatos.core.data.CaptureDraft
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.CaptureFlow
import sg.mesha.goatos.core.data.capture.buildMilkPreparationEvidenceSlot
import sg.mesha.goatos.core.data.capture.EvidenceSlot
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofFlow
import sg.mesha.goatos.core.data.capture.ProofIdentity
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.sync.SubmittedGrainsSource
import sg.mesha.goatos.core.data.sync.MilkPreparationAnswersPayload
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.network.isConnectivityFailure
import sg.mesha.goatos.core.network.dto.MilkPreparationPageDto
import sg.mesha.goatos.core.network.dto.MilkPreparationFarmTaskDto
import sg.mesha.goatos.feature.counts.MilkPreparationCardBucket
import sg.mesha.goatos.feature.counts.MilkPreparationCardUi
import sg.mesha.goatos.feature.counts.MilkPreparationChipUi
import sg.mesha.goatos.feature.counts.MilkPreparationEvent
import sg.mesha.goatos.feature.counts.MilkPreparationListEvent
import sg.mesha.goatos.feature.counts.MilkPreparationListUiState
import sg.mesha.goatos.feature.counts.MilkPreparationStepUi
import sg.mesha.goatos.feature.counts.MilkPreparationUiState

private val MILK_IST: ZoneId = ZoneId.of("Asia/Kolkata")
private val MILK_DAY_LABEL: DateTimeFormatter = DateTimeFormatter.ofPattern("d MMM")

private data class MilkPreparationRefreshState(
    val isRefreshing: Boolean = false,
    val isOffline: Boolean = false,
)

@HiltViewModel
class MilkPreparationListViewModel @Inject constructor(
    private val repo: MilkPreparationRepository,
    private val submittedGrains: SubmittedGrainsSource,
    drafts: CaptureDraftRepository,
) : ViewModel() {
    private val selectedDate = MutableStateFlow(LocalDate.now(MILK_IST).toString())
    private val selectedFilter = MutableStateFlow("all")
    private val refresh = MutableStateFlow(MilkPreparationRefreshState())

    @OptIn(ExperimentalCoroutinesApi::class)
    val state: StateFlow<MilkPreparationListUiState> = selectedDate
        .flatMapLatest { dateStr ->
            combine(
                repo.observe(dateStr),
                selectedFilter,
                refresh,
                // ONE bounded Room observation for the whole page, never a per-row lookup — a per-card
                // draft read behind a list is the N+1 shape (docs/decisions/mobile-data-fetch-anti-patterns.md).
                drafts.observeProgress(CaptureFlow.MILK_PREPARATION),
                submittedGrains.observe(),
            ) { resource, selected, sync, capturedByEntity, locallySubmitted ->
                val page = resource.data
                if (page == null) {
                    MilkPreparationListUiState(
                        selectedFilter = selected,
                        dateLabel = milkPreparationDateLabel(dateStr),
                        isRefreshing = sync.isRefreshing,
                        isOffline = sync.isOffline,
                        emptyMessage = if (sync.isOffline) "Couldn't load Milk Preparation. It will appear once you're back online." else null,
                    )
                } else {
                    buildMilkPreparationListUi(page, selected, capturedByEntity, draftDate = dateStr).copy(
                        selectedDate = dateStr,
                        isRefreshing = sync.isRefreshing,
                        isOffline = sync.isOffline,
                        lastSyncedAt = resource.lastSyncedAt,
                    )
                }
            }
        }
        .stateIn(
            viewModelScope,
            SharingStarted.WhileSubscribed(5_000),
            MilkPreparationListUiState(dateLabel = milkPreparationDateLabel(selectedDate.value)),
        )

    init { refresh() }

    fun onEvent(event: MilkPreparationListEvent) {
        when (event) {
            MilkPreparationListEvent.Refresh -> refresh()
            is MilkPreparationListEvent.SelectFilter -> selectedFilter.value = event.key
            is MilkPreparationListEvent.NavigateDate -> navigateDate(event.delta)
            is MilkPreparationListEvent.OpenFarm, MilkPreparationListEvent.Back -> Unit
        }
    }

    /** Business dates are capped at today IST, mirroring WorkflowListViewModel.selectDate — future
     *  days have no preparation tasks by definition. */
    private fun navigateDate(delta: Int) {
        val currentDate = LocalDate.parse(selectedDate.value)
        val today = LocalDate.now(MILK_IST)
        val requested = currentDate.plusDays(delta.toLong())
        val capped = if (requested.isAfter(today)) today else requested
        if (capped.toString() == selectedDate.value) return
        selectedDate.value = capped.toString()
        refresh()
    }

    private fun refresh() = viewModelScope.launch {
        refresh.value = MilkPreparationRefreshState(isRefreshing = true)
        val preparationResult = repo.refresh(selectedDate.value)
        refresh.value = MilkPreparationRefreshState(isOffline = preparationResult.exceptionOrNull().isConnectivityFailure())
    }
}

/** "Today · 27 Jul" only when [dateIso] IS today IST; otherwise just the formatted date — matching
 *  the WorkflowListViewModel date-bar convention (a past/future selection is never mislabeled Today). */
private fun milkPreparationDateLabel(dateIso: String): String {
    // exception:exempt display-only fallback — unparseable date renders verbatim; nothing actionable to record
    val parsed = runCatching { LocalDate.parse(dateIso) }.getOrNull() ?: return dateIso
    val label = parsed.format(MILK_DAY_LABEL)
    return if (dateIso == LocalDate.now(MILK_IST).toString()) "Today · $label" else label
}

internal fun buildMilkPreparationListUi(
    page: MilkPreparationPageDto?,
    selectedFilter: String,
    capturedByEntity: Map<String, Int> = emptyMap(),
    draftDate: String = "",
    locallySubmitted: Set<String> = emptySet(),
): MilkPreparationListUiState {
    val allCards = page?.farmTasks.orEmpty()
        .map { task ->
            // MUST be the same key the detail screen writes its draft under
            // (MilkPreparationViewModel.entityId = "$parkId:$preparationDate", where the date is the
            // LOCAL IST business day). Keying off page.preparationDate instead would silently miss
            // every draft on any day the backend's sheet date differs from the phone's.
            milkPreparationCard(
                task,
                capturedByEntity["${task.parkId}:$draftDate"] ?: 0,
                locallySubmitted.contains(
                    task.submittedGrainKey(draftDate),
                ),
            )
        }
        .sortedBy { it.parkLabel }
    val cards = if (selectedFilter == "all") allCards else allCards.filter { card ->
        when (selectedFilter) {
            "not_submitted" -> card.bucket == MilkPreparationCardBucket.TO_PREPARE
            "pending_verification" -> card.bucket == MilkPreparationCardBucket.IN_REVIEW
            "completed" -> card.bucket == MilkPreparationCardBucket.COMPLETED
            "rework" -> card.bucket == MilkPreparationCardBucket.REWORK
            else -> true
        }
    }
    val counts = allCards.groupingBy { it.bucket }.eachCount()
    val toPrepare = counts[MilkPreparationCardBucket.TO_PREPARE] ?: 0
    return MilkPreparationListUiState(
        subtitle = "${allCards.size} ${if (allCards.size == 1) "farm" else "farms"} · $toPrepare need action",
        // The SELECTED date drives the label, not the page's own preparationDate echo — a page
        // still carrying yesterday's cached data (offline) must not silently relabel the date the
        // operator navigated to.
        dateLabel = milkPreparationDateLabel(draftDate.ifBlank { page?.preparationDate.orEmpty() }),
        feedingDateLabel = page?.feedingDate?.toMilkDateLabel().orEmpty(),
        chips = listOf(
            MilkPreparationChipUi("all", "All", allCards.size),
            MilkPreparationChipUi("not_submitted", "Need action", toPrepare),
            MilkPreparationChipUi("pending_verification", "In review", counts[MilkPreparationCardBucket.IN_REVIEW] ?: 0),
            MilkPreparationChipUi("completed", "Completed", counts[MilkPreparationCardBucket.COMPLETED] ?: 0),
            MilkPreparationChipUi("rework", "Rework", counts[MilkPreparationCardBucket.REWORK] ?: 0),
        ),
        selectedFilter = selectedFilter,
        cards = cards,
        emptyMessage = if (cards.isEmpty()) "No Milk Preparation work in this status." else null,
    )
}

private fun milkPreparationCard(
    task: MilkPreparationFarmTaskDto,
    capturedProofCount: Int,
    isLocallySubmittedForReview: Boolean = false,
): MilkPreparationCardUi {
    // A submit still in the outbox leaves verificationStatus at "not_submitted", so the card read
    // "To prepare" for work already sent (254.mp4 class). Milk Preparation is FARM-DAY grain.
    val bucket = when (
        overlayVerificationStatus(
            backendStatus = task.verificationStatus,
            reworkReason = task.reworkReason,
            isLocallySubmitted = isLocallySubmittedForReview,
            inReviewToken = IN_REVIEW_PENDING_VERIFICATION,
        )
    ) {
        "pending_verification" -> MilkPreparationCardBucket.IN_REVIEW
        "completed" -> MilkPreparationCardBucket.COMPLETED
        "rework" -> MilkPreparationCardBucket.REWORK
        else -> MilkPreparationCardBucket.TO_PREPARE
    }
    val (status, action) = when (bucket) {
        MilkPreparationCardBucket.TO_PREPARE -> "To prepare" to "Prepare and record videos"
        MilkPreparationCardBucket.IN_REVIEW -> "In review" to "Proof submitted"
        MilkPreparationCardBucket.COMPLETED -> "Completed" to "Preparation verified"
        MilkPreparationCardBucket.REWORK -> "Rework" to "Record a fresh proof set"
    }
    // Unfinished work the operator can pick up where they left off. Only meaningful before submit —
    // once the sheet is with the verifier the draft is history, not a resume point.
    val resumable = capturedProofCount > 0 && bucket == MilkPreparationCardBucket.TO_PREPARE
    return MilkPreparationCardUi(
        parkId = task.parkId,
        parkLabel = task.parkLabel,
        shedId = task.parkId,
        shedLabel = "",
        cohortCount = task.cohortCount,
        headCount = task.headCount,
        totalMilkLabel = "${formatLitres(task.totalRequiredMl)} milk",
        citricAcidLabel = "${formatDecimal(task.citricAcidGrams)} g citric acid",
        statusLabel = status,
        actionLabel = when {
            resumable -> "Resume · ${capturedProofCount.videosRecorded()} saved"
            bucket == MilkPreparationCardBucket.TO_PREPARE -> "Prepare"
            else -> action
        },
        reworkReason = task.reworkReason,
        bucket = bucket,
        detailLabel = "${task.cohortCount} cohorts · ${task.headCount} animals",
        capturedProofCount = capturedProofCount,
    )
}

/** "1 video" / "3 videos" — operator-facing copy, never a bare count. */
internal fun Int.videosRecorded(): String = if (this == 1) "1 video" else "$this videos"

private fun String.toMilkDateLabel(prefix: String = ""): String =
    runCatching { prefix + LocalDate.parse(this).format(MILK_DAY_LABEL) }.getOrDefault(this)

private fun formatLitres(millilitres: Long): String = formatDecimal(millilitres.toDouble() / 1_000.0) + " L"
private fun formatDecimal(value: Double): String =
    if (value % 1.0 == 0.0) value.toLong().toString() else String.format(Locale.US, "%.1f", value)

private const val FIELD_GOAT_MILK_USED = "goat_milk_used"
private const val FIELD_MORNING = "morning"
private const val FIELD_EVENING = "evening"
private const val STEP_PREFIX = "step:"

private data class MilkPreparationDraftState(
    val goatMilkUsed: Boolean?,
    val steps: List<MilkPreparationStepUi>,
    val morningMilkCollected: String = "",
    val eveningMilkCollected: String = "",
    val message: String? = null,
    val submitting: Boolean = false,
    val queued: Boolean = false,
)

@HiltViewModel
class MilkPreparationViewModel @Inject constructor(
    private val sync: SyncRepository,
    private val repo: MilkPreparationRepository,
    private val capture: ProofCaptureSource,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val drafts: CaptureDraftRepository,
    private val analytics: AnalyticsPort,
    private val saved: SavedStateHandle,
) : ViewModel() {
    private val parkId = saved.get<String>(ARG_PARK_ID).orEmpty()
    private val preparationDate = saved.get<String>(ARG_PREPARATION_DATE) ?: LocalDate.now(MILK_IST).toString()
    // Proof/draft field-key building now routes through the canonical ProofIdentity/EvidenceSlot
    // model (see evidenceSlot() below). identity.taskId is set to the EXISTING groupKey() literal
    // and fieldKey to the EXISTING "milk_preparation_$stepCode" literal, so the strings written to
    // Room/outbox are byte-for-byte unchanged — only the plumbing carrying them is canonical now.
    // DraftIdempotencyKey/SavedStateHandle process-death survival for the mint-random-UUID
    // idempotency keys is unrelated to identity addressing and is unaffected by this migration.
    private val submitKey = DraftIdempotencyKey(saved, "milkPreparation.submitKey", "milk-preparation-submit")
    private val proofKeys = allSteps.associateWith { DraftIdempotencyKey(saved, "milkPreparation.proofKey.$it", "milk-preparation-$it") }
    private val submitOutboxItemId = DraftOutboxItemId(saved, "milkPreparation.submitOutboxItemId")
    private val refresh = MutableStateFlow(MilkPreparationRefreshState())

    // Starts empty and is rehydrated from the DURABLE draft in init. Answers used to be mirrored
    // into SavedStateHandle, which dies with the nav backstack entry: Back + re-entry showed the
    // recorded videos beside blank fields, and every step gates on answerComplete && captured, so
    // the whole sheet had to be retyped before Submit re-enabled.
    private val draft = MutableStateFlow(MilkPreparationDraftState(goatMilkUsed = null, steps = emptyList()))
    private var statusJob: Job? = null

    val state: StateFlow<MilkPreparationUiState> = combine(repo.observe(preparationDate), draft, refresh) { resource, local, syncState ->
        val page = resource.data
        val task = page?.farmTasks?.firstOrNull { it.parkId == parkId }
        val backendSubmitted = task?.verificationStatus == "pending_verification" || task?.verificationStatus == "completed"
        MilkPreparationUiState(
            preparationDate = page?.preparationDate ?: preparationDate,
            feedingDate = page?.feedingDate.orEmpty(),
            selectedParkId = task?.parkId ?: parkId,
            parkLabel = task?.parkLabel.orEmpty(),
            goatMilkUsed = local.goatMilkUsed,
            morningMilkCollected = local.morningMilkCollected,
            eveningMilkCollected = local.eveningMilkCollected,
            steps = sequenceMilkPreparationSteps(local.steps.map { step ->
                if (step.code == "citric_acid_mixing" && task != null) {
                    step.copy(label = "Add ${formatDecimal(task.citricAcidGrams)} g citric acid, mix it and store outside")
                } else {
                    step
                }
            }, questionsComplete = local.goatMilkUsed != null && local.morningMilkCollected.toDoubleOrNull()?.let { it >= 0 } == true && local.eveningMilkCollected.toDoubleOrNull()?.let { it >= 0 } == true),
            message = local.message,
            submitting = local.submitting,
            submitted = local.queued || backendSubmitted,
            verificationStatus = task?.verificationStatus.orEmpty(),
            reworkReason = task?.reworkReason.orEmpty(),
            cohortCount = task?.cohortCount ?: 0,
            headCount = task?.headCount ?: 0,
            totalMilkLabel = task?.let { "${formatLitres(it.totalRequiredMl)} milk" }.orEmpty(),
            citricAcidLabel = task?.let { "${formatDecimal(it.citricAcidGrams)} g citric acid" }.orEmpty(),
            citricAcidGrams = task?.citricAcidGrams ?: 0.0,
            milkDirectionLines = task?.milkDirection.orEmpty().map { line ->
                "${line.managementStage}: ${line.headCount} kids × ${line.perHeadMl}ml × ${line.sessionCount} sessions = ${formatLitres(line.requiredMl)}"
            },
            citricAcidRateLabel = task?.let { "${formatDecimal(it.citricAcidGramsPerLitre)} g/L" }.orEmpty(),
            isRefreshing = syncState.isRefreshing,
            isOffline = syncState.isOffline,
            lastSyncedAt = resource.lastSyncedAt,
        )
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        MilkPreparationUiState(preparationDate = preparationDate, selectedParkId = parkId),
    )

    init { refresh() }

    fun onEvent(event: MilkPreparationEvent) {
        // Block all edits if this submit is already in-flight/completed
        val isSubmitInFlightOrCompleted = submitOutboxItemId.value != null
        if (event != MilkPreparationEvent.Refresh && event != MilkPreparationEvent.Back && isSubmitInFlightOrCompleted) return
        when (event) {
            is MilkPreparationEvent.SetGoatMilkUsed -> if (state.value.goatMilkQuestionEnabled) {
                // Answers already typed are carried across the step-set change, so toggling the
                // question back and forth does not silently discard them.
                draft.update { current ->
                    val answered = current.steps.associate { it.code to it.answer }
                    val captured = current.steps.filter { it.captured }.map { it.code }.toSet()
                    current.copy(
                        goatMilkUsed = event.used,
                        steps = applicableSteps(event.used).map { step ->
                            step.copy(answer = answered[step.code].orEmpty(), captured = step.code in captured)
                        },
                    )
                }
            }
            is MilkPreparationEvent.CaptureStep -> captureStep(event.stepCode)
            is MilkPreparationEvent.ReCaptureStep -> reCaptureStep(event.stepCode)
            is MilkPreparationEvent.SetCollectedMilk -> {
                val current = state.value
                if ((event.shift == "morning" && !current.morningQuestionEnabled) || (event.shift == "evening" && !current.eveningQuestionEnabled)) return
                draft.update { if (event.shift == "morning") it.copy(morningMilkCollected = event.value) else it.copy(eveningMilkCollected = event.value) }
            }
            is MilkPreparationEvent.SetStepAnswer -> {
                if (state.value.steps.firstOrNull { it.code == event.stepCode }?.enabled != true) return
                draft.update { current -> current.copy(steps = current.steps.map { if (it.code == event.stepCode) it.copy(answer = event.value) else it }) }
            }
            MilkPreparationEvent.Submit -> submit()
            MilkPreparationEvent.Refresh -> refresh()
            MilkPreparationEvent.Back -> Unit
        }
    }

    /**
     * The park-day's DURABLE captured evidence: each step's PROOF_UPLOAD outbox item id plus the
     * submit key, in the shared capture-draft store. Held here (not in [saved]) so leaving and
     * re-entering the screen keeps the videos already recorded.
     */
    private var captureDraft = CaptureDraft()

    init {
        analytics.track(
            AnalyticsEvents.MILK_PREPARATION_OPENED,
            mapOf(AnalyticsEvents.Params.PARK_ID to parkId),
        )
        viewModelScope.launch {
            captureDraft = drafts.find(CaptureFlow.MILK_PREPARATION, entityId)
            val answers = captureDraft.answers
            val goatMilkUsed = answers[FIELD_GOAT_MILK_USED]?.toBooleanStrictOrNull()
            draft.update { current ->
                current.copy(
                    goatMilkUsed = goatMilkUsed,
                    morningMilkCollected = answers[FIELD_MORNING].orEmpty(),
                    eveningMilkCollected = answers[FIELD_EVENING].orEmpty(),
                    steps = goatMilkUsed?.let(::applicableSteps).orEmpty().map { step ->
                        step.copy(
                            answer = answers["$STEP_PREFIX${step.code}"].orEmpty(),
                            captured = captureDraft.hasProof(step.code),
                        )
                    },
                )
            }
            // Restore BEFORE the writer starts, or the empty initial state would immediately
            // overwrite the answers just read back.
            persistAnswers()
            // Restore the submit outbox item ID from the durable store so process death doesn't
            // lose the in-flight submission state. If one exists, observe it for status changes.
            // Draft store may lag or be pruned — it must never CLOBBER a SavedStateHandle-restored
            // in-flight id back to null (that reopened a queued submit for editing; caught by the
            // process-death regression test 2026-08-16).
            captureDraft.submitOutboxItemId?.let { submitOutboxItemId.value = it }
            submitOutboxItemId.value?.let(::observeOutboxItem)
            observeProofChanges()
        }
    }

    private fun observeProofChanges() = viewModelScope.launch {
        drafts.observe(CaptureFlow.MILK_PREPARATION, entityId)
            .distinctUntilChanged { old, new -> old.proofs == new.proofs }
            .collect { newCaptureDraft ->
                captureDraft = newCaptureDraft
                draft.update { current ->
                    current.copy(steps = current.steps.map { it.copy(captured = newCaptureDraft.hasProof(it.code)) })
                }
            }
    }

    /**
     * Mirrors every typed answer into the durable draft so Back + re-entry restores the sheet rather
     * than asking the operator to key it in again beside videos they can already see are recorded.
     */
    private fun persistAnswers() = viewModelScope.launch {
        draft
            .map { local ->
                buildMap {
                    local.goatMilkUsed?.let { put(FIELD_GOAT_MILK_USED, it.toString()) }
                    put(FIELD_MORNING, local.morningMilkCollected)
                    put(FIELD_EVENING, local.eveningMilkCollected)
                    local.steps.forEach { put("$STEP_PREFIX${it.code}", it.answer) }
                }
            }
            .distinctUntilChanged()
            .drop(1)
            // conflate, NOT debounce: a time window would drop the pending write when Back cancels
            // viewModelScope — the very loss this exists to prevent. Conflate coalesces a burst of
            // keystrokes into the latest value without delaying it.
            .conflate()
            .collect { drafts.putAnswers(CaptureFlow.MILK_PREPARATION, entityId, it) }
    }

    private fun refresh() = viewModelScope.launch {
        refresh.value = MilkPreparationRefreshState(isRefreshing = true)
        val preparationResult = repo.refresh(preparationDate)
        refresh.value = MilkPreparationRefreshState(isOffline = preparationResult.exceptionOrNull().isConnectivityFailure())
    }

    /**
     * Replaces one step's clip: Manohar ordering ensures the new proof captures and stores
     * BEFORE the old one is deleted, so a cancelled or failed re-capture keeps the existing
     * good proof (the old "proof disappeared" defect).
     */
    private fun reCaptureStep(stepCode: String) {
        val current = state.value
        if (!current.isEditable) return
        val oldProofOutboxId = captureDraft.proofs[stepCode]
        viewModelScope.launch {
            proofKeys[stepCode]?.invalidate()
            val caption = proofOverlayContextLine(
                feature = "Milk preparation",
                parkLabel = current.parkLabel.ifBlank { parkId },
                extraLabel = current.steps.firstOrNull { it.code == stepCode }?.label.orEmpty(),
            )
            draft.update { it.copy(steps = it.steps.map { row -> if (row.code == stepCode) row.copy(capturing = true) else row }) }
            var captureThrew = false
            val video = try {
                capture.captureVideo(
                    ProofCaptureContext(
                        title = caption,
                        primaryTag = current.parkLabel.ifBlank { parkId },
                        workLabel = current.steps.firstOrNull { it.code == stepCode }?.label.orEmpty(),
                        prompt = ProofCapturePrompt.MILK_PREPARATION,
                        headerTitle = current.steps.firstOrNull { it.code == stepCode }?.label.orEmpty(),
                    ),
                )
            } catch (error: Exception) {
                captureThrew = true
                analytics.track(
                    AnalyticsEvents.MILK_PREPARATION_PROOF_CAPTURE_FAILURE,
                    mapOf(
                        AnalyticsEvents.Params.PARK_ID to parkId,
                        AnalyticsEvents.Params.FIELD to stepCode,
                        AnalyticsEvents.Params.REASON to "camera_exception",
                    ),
                )
                null
            }
            if (video == null) {
                if (!captureThrew) {
                    analytics.track(
                        AnalyticsEvents.MILK_PREPARATION_PROOF_CAPTURE_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.PARK_ID to parkId,
                            AnalyticsEvents.Params.FIELD to stepCode,
                            AnalyticsEvents.Params.REASON to "cancelled",
                        ),
                    )
                }
                draft.update { it.copy(steps = it.steps.map { row -> if (row.code == stepCode) row.copy(capturing = false) else row }) }
                return@launch
            }
            val slot = evidenceSlot(stepCode)
            when (val result = proofCaptureRepository.captureReplacingLatest(
                slot = slot,
                // subject_id is the park uuid, NOT a shed. Milk prep has no backend SOP
                // task uuid to bind to at capture time (unlike milk feeding), and stamping
                // subject_type='shed' here would poison the real subject_type='shed' lookups
                // used by vaccination/weighing/sop (WHERE subject_type='shed' AND subject_id=
                // <real shed uuid>) with a park id that resolves to no shed. 'other' carries
                // no false identity claim; the park identity is already correct via
                // scope_type='park'/scope_id=parkId above.
                subject = ProofSubject.OTHER,
                subjectId = parkId,
                localUri = video.localUri,
                mimeType = video.mimeType,
                caption = caption,
                scopeType = "park",
                scopeId = parkId,
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
                        setCapturing(stepCode, false)
                        draft.update { it.copy(message = "Proof upload could not be queued") }
                        return@launch
                    }
                    // NEW PROOF is durable before we remove the old one, so a process death here
                    // cannot lose the clip (Manohar ordering).
                    drafts.putProof(CaptureFlow.MILK_PREPARATION, entityId, stepCode, proofOutboxId)
                    captureDraft = drafts.find(CaptureFlow.MILK_PREPARATION, entityId)
                    draft.update { it.copy(steps = it.steps.map { row -> if (row.code == stepCode) row.copy(captured = true, capturing = false) else row }) }
                    // ONLY NOW, after the new proof is stored, delete the old one so it never
                    // reaches the verifier as a duplicate.
                    oldProofOutboxId?.let { sync.deleteOutboxItem(it) }
                }
                is AppResult.Err -> {
                    analytics.track(
                        AnalyticsEvents.MILK_PREPARATION_PROOF_CAPTURE_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.PARK_ID to parkId,
                            AnalyticsEvents.Params.FIELD to stepCode,
                            AnalyticsEvents.Params.REASON to result.message,
                        ),
                    )
                    setCapturing(stepCode, false)
                    draft.update { it.copy(message = result.message) }
                    // On error, keep the old proof: don't remove it.
                }
            }
        }
    }

    private fun captureStep(stepCode: String) {
        val current = state.value
        val step = current.steps.firstOrNull { it.code == stepCode } ?: return
        if (!current.isEditable || parkId.isBlank() || !step.enabled || !step.answerComplete || step.captured || step.capturing) return
        analytics.track(
            AnalyticsEvents.MILK_PREPARATION_PROOF_CAPTURE_ATTEMPT,
            mapOf(
                AnalyticsEvents.Params.PARK_ID to parkId,
                AnalyticsEvents.Params.FIELD to stepCode,
            ),
        )
        draft.update { it.copy(steps = it.steps.map { row -> if (row.code == stepCode) row.copy(capturing = true) else row }) }
        viewModelScope.launch {
            val caption = proofOverlayContextLine(
                feature = "Milk preparation",
                parkLabel = current.parkLabel.ifBlank { parkId },
                extraLabel = step.label,
            )
            val video = capture.captureVideo(
                ProofCaptureContext(
                    title = caption,
                    primaryTag = current.parkLabel.ifBlank { parkId },
                    workLabel = step.label,
                    prompt = ProofCapturePrompt.MILK_PREPARATION,
                    headerTitle = step.label,
                ),
            )
            if (video == null) {
                analytics.track(
                    AnalyticsEvents.MILK_PREPARATION_PROOF_CAPTURE_FAILURE,
                    mapOf(
                        AnalyticsEvents.Params.PARK_ID to parkId,
                        AnalyticsEvents.Params.FIELD to stepCode,
                        AnalyticsEvents.Params.REASON to "cancelled",
                    ),
                )
                setCapturing(stepCode, false)
                return@launch
            }
            val slot = evidenceSlot(stepCode)
            when (val result = proofCaptureRepository.captureReplacingLatest(
                slot = slot,
                // subject_id is the park uuid, NOT a shed. Milk prep has no backend SOP
                // task uuid to bind to at capture time (unlike milk feeding), and stamping
                // subject_type='shed' here would poison the real subject_type='shed' lookups
                // used by vaccination/weighing/sop (WHERE subject_type='shed' AND subject_id=
                // <real shed uuid>) with a park id that resolves to no shed. 'other' carries
                // no false identity claim; the park identity is already correct via
                // scope_type='park'/scope_id=parkId above.
                subject = ProofSubject.OTHER,
                subjectId = parkId,
                localUri = video.localUri,
                mimeType = video.mimeType,
                caption = caption,
                scopeType = "park",
                scopeId = parkId,
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
                            AnalyticsEvents.MILK_PREPARATION_PROOF_CAPTURE_FAILURE,
                            mapOf(
                                AnalyticsEvents.Params.PARK_ID to parkId,
                                AnalyticsEvents.Params.FIELD to stepCode,
                                AnalyticsEvents.Params.REASON to "enqueue_failed",
                            ),
                        )
                        setCapturing(stepCode, false)
                        draft.update { it.copy(message = "Proof upload could not be queued") }
                        return@launch
                    }
                    // Durable BEFORE the UI flips, so a process death here cannot lose the clip.
                    drafts.putProof(CaptureFlow.MILK_PREPARATION, entityId, stepCode, proofOutboxId)
                    captureDraft = drafts.find(CaptureFlow.MILK_PREPARATION, entityId)
                    analytics.track(
                        AnalyticsEvents.MILK_PREPARATION_PROOF_CAPTURE_SUCCESS,
                        mapOf(
                            AnalyticsEvents.Params.PARK_ID to parkId,
                            AnalyticsEvents.Params.FIELD to stepCode,
                        ),
                    )
                    draft.update { it.copy(steps = it.steps.map { row -> if (row.code == stepCode) row.copy(captured = true, capturing = false) else row }) }
                }
                is AppResult.Err -> {
                    proofKeys.getValue(stepCode).invalidate()
                    analytics.track(
                        AnalyticsEvents.MILK_PREPARATION_PROOF_CAPTURE_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.PARK_ID to parkId,
                            AnalyticsEvents.Params.FIELD to stepCode,
                            AnalyticsEvents.Params.REASON to result.message,
                        ),
                    )
                    setCapturing(stepCode, false)
                    draft.update { it.copy(message = result.message) }
                }
            }
        }
    }

    private fun submit() {
        val current = state.value
        if (!current.canSubmit) return
        // Prevent double-submit: if one is already queued/in-flight/succeeded, don't submit again
        if (submitOutboxItemId.value != null) return
        val proofItems = current.steps.associate { it.code to captureDraft.proofs[it.code].orEmpty() }
        if (proofItems.values.any(String::isBlank)) return
        draft.update { it.copy(submitting = true, message = null) }
        viewModelScope.launch {
            val answers = milkPreparationAnswers(current)
            val goatMilkUsed = current.goatMilkUsed ?: return@launch
            // STABLE per park-day and durable: a re-entered screen resends the SAME key.
            val submitIdempotencyKey = captureDraft.submitIdempotencyKey ?: "milk-preparation-submit:$entityId"
            if (captureDraft.submitIdempotencyKey == null) {
                drafts.putSubmit(CaptureFlow.MILK_PREPARATION, entityId, submitIdempotencyKey, null)
                captureDraft = drafts.find(CaptureFlow.MILK_PREPARATION, entityId)
            }
            analytics.track(
                AnalyticsEvents.MILK_PREPARATION_SUBMITTED,
                mapOf(AnalyticsEvents.Params.PARK_ID to current.selectedParkId),
            )
            when (val result = sync.enqueueMilkPreparationSubmit(groupKey(), submitIdempotencyKey, current.selectedParkId, current.preparationDate, goatMilkUsed, answers, proofItems)) {
                is AppResult.Ok -> {
                    drafts.putSubmit(CaptureFlow.MILK_PREPARATION, entityId, submitIdempotencyKey, result.value)
                    captureDraft = drafts.find(CaptureFlow.MILK_PREPARATION, entityId)
                    // Store the outbox item ID durably so process death doesn't lose the in-flight state
                    submitOutboxItemId.value = result.value
                    observeOutboxItem(result.value)
                    draft.update { it.copy(submitting = false, queued = true, message = "Proof uploads in background.") }
                }
                is AppResult.Err -> {
                    analytics.track(AnalyticsEvents.MILK_PREPARATION_FAILURE, mapOf(AnalyticsEvents.Params.REASON to result.message))
                    // Clear both in-memory latch and persisted draft key so the operator can retry after process death
                    submitOutboxItemId.value = null
                    drafts.putSubmit(CaptureFlow.MILK_PREPARATION, entityId, submitIdempotencyKey, null)
                    captureDraft = drafts.find(CaptureFlow.MILK_PREPARATION, entityId)
                    draft.update { it.copy(submitting = false, message = result.message) }
                }
            }
        }
    }

    internal fun groupKey() = "milk-preparation:$parkId:$preparationDate"

    /** The work item the durable draft belongs to: this park's preparation for this business day. */
    private val entityId get() = "$parkId:$preparationDate"

    /** Canonical slot grain for a milk-preparation step capture. identity.taskId/fieldKey resolve
     *  to the SAME strings [groupKey] / the raw `"milk_preparation_$stepCode"` literal already
     *  produced, so routing captures through this slot changes no on-disk value. */
    internal fun evidenceSlot(stepCode: String): EvidenceSlot =
        buildMilkPreparationEvidenceSlot(parkId, preparationDate, stepCode)
    private fun setCapturing(step: String, value: Boolean) = draft.update { current ->
        current.copy(steps = current.steps.map { row -> if (row.code == step) row.copy(capturing = value) else row })
    }

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
                            draft.update { it.copy(submitting = false, queued = true, message = "Proof uploads in background.") }
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
        const val ARG_PARK_ID = "park_id"
        const val ARG_PREPARATION_DATE = "preparation_date"
        private val allSteps = listOf("goat_milk_quantity", "boiling_temperature", "cooled_temperature", "uht_milk_quantity", "citric_acid_mixing")
        private val labels = mapOf(
            "goat_milk_quantity" to ("Quantity of goat milk used" to "L"),
            "boiling_temperature" to ("Boiling temperature" to "°C"),
            "cooled_temperature" to ("Cooled temperature" to "°C"),
            "uht_milk_quantity" to ("Quantity of UHT milk" to "L"),
            "citric_acid_mixing" to ("Citric acid added, mixed and stored outside" to ""),
        )
        private fun applicableSteps(goatMilk: Boolean) = allSteps
            .filter { goatMilk || it !in allSteps.take(3) }
            .map { code ->
                labels.getValue(code).let { (label, unit) ->
                    MilkPreparationStepUi(
                        code = code,
                        label = label,
                        unit = unit,
                        requiresAnswer = code != "citric_acid_mixing",
                    )
                }
            }
    }
}

internal fun milkPreparationAnswers(state: MilkPreparationUiState): MilkPreparationAnswersPayload {
    val answerByStep = state.steps.associate { it.code to (it.answer.toDoubleOrNull() ?: 0.0) }
    return MilkPreparationAnswersPayload(
        morningMilkCollectedLitres = state.morningMilkCollected.toDouble(),
        eveningMilkCollectedLitres = state.eveningMilkCollected.toDouble(),
        goatMilkQuantityLitres = answerByStep["goat_milk_quantity"] ?: 0.0,
        boilingTemperatureC = answerByStep["boiling_temperature"] ?: 0.0,
        cooledTemperatureC = answerByStep["cooled_temperature"] ?: 0.0,
        uhtMilkQuantityLitres = answerByStep.getValue("uht_milk_quantity"),
        citricAcidGrams = state.citricAcidGrams,
    )
}

internal fun milkParkProofPolicy(captureSource: String): ProofPolicy =
    ProofPolicy.Default.copy(
        proofMode = "park_step_video",
        // Matches the subject actually written by captureStep/reCaptureStep (ProofSubject.OTHER,
        // subjectId=parkId): milk prep has no backend-valid 'park' subject type and no backend
        // task uuid to bind to at capture time. Keeping this aligned with the real wire value
        // matters because ProofPolicy.defaultSubject falls back to the first entry here for any
        // future caller that does not pass an explicit subject.
        subjectScope = ProofSubject.OTHER.wireValue,
        expectedSubjects = listOf(ProofSubject.OTHER.wireValue),
        captureSource = captureSource,
    )

internal fun sequenceMilkPreparationSteps(
    steps: List<MilkPreparationStepUi>,
    questionsComplete: Boolean,
): List<MilkPreparationStepUi> {
    var unlocked = questionsComplete
    return steps.map { step ->
        val sequenced = step.copy(enabled = unlocked)
        unlocked = unlocked && step.answerComplete && step.captured
        sequenced
    }
}
