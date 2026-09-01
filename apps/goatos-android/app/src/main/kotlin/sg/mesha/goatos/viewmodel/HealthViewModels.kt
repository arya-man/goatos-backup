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
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.update
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CaptureDraft
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.CaptureFlow
import sg.mesha.goatos.core.data.HealthFilters
import sg.mesha.goatos.core.data.HealthRepository
import sg.mesha.goatos.core.data.CountsRepository
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.EvidenceSlot
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofFlow
import sg.mesha.goatos.core.data.capture.ProofIdentity
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.PendingHealthCaseOpen
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.network.dto.HealthWorkItemDetailDto
import sg.mesha.goatos.core.network.dto.HealthSummaryDto
import sg.mesha.goatos.core.network.dto.HealthTreatmentStepDto
import sg.mesha.goatos.core.network.dto.HealthWorkItemDto
import sg.mesha.goatos.core.network.dto.GoatSearchItemDto
import sg.mesha.goatos.feature.health.AddHealthCaseEvent
import sg.mesha.goatos.feature.health.AddHealthCaseUiState
import sg.mesha.goatos.feature.health.HealthGoatUi
import sg.mesha.goatos.feature.health.HealthDateMarkerUi
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
            summary = (page?.page?.summary ?: HealthSummaryDto()).toUi(),
            diseases = page?.page?.filterOptions?.diseases.orEmpty().map { HealthFilterUi(it.key, it.label) },
            parks = page?.page?.filterOptions?.parks.orEmpty().map { HealthFilterUi(it.key, it.label) },
            sheds = page?.page?.filterOptions?.sheds.orEmpty().map { HealthFilterUi(it.key, it.label) },
            dateMarkers = page?.page?.dateMarkers.orEmpty()
                .filter { it.date != selected.date && it.count > 0 }
                .map { HealthDateMarkerUi(it.date, dateLabel(it.date), it.count) },
            pendingCases = visiblePendingHealthCases(selected, pending),
            refreshing = loading,
            lastSyncedAt = page?.updatedAtMs,
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
            // Navigation only: the queue is its own destination with its own view model.
            HealthListEvent.OpenDiagnosisQueue -> Unit
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
    private val analytics: AnalyticsPort,
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
                val diseases = page?.page?.filterOptions?.diseases.orEmpty().map { HealthFilterUi(it.key, it.label) }
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
            // Navigation only: the screen hands the animal to the observation form.
            is AddHealthCaseEvent.CheckAnimal -> Unit
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
                .onFailure { error ->
                    analytics.track(
                        AnalyticsEvents.HEALTH_READ_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.KIND to "case_animal_lookup",
                            AnalyticsEvents.Params.REASON to (error.message ?: "unknown"),
                        ),
                    )
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
                // Tracked on the DURABLE enqueue, not on a network round trip -- the operator's
                // work being safe in the outbox is the moment worth measuring. Same convention as
                // COUNTS_BIRTH_SUBMITTED / COUNTS_DEATH_SUBMITTED.
                is AppResult.Ok -> {
                    analytics.track(AnalyticsEvents.HEALTH_CASE_SUBMITTED, mapOf(AnalyticsEvents.Params.KIND to ageBand))
                    _state.value = _state.value.copy(
                        submitting = false,
                        returnToList = true,
                        message = "Sick goat recorded. The treatment plan is queued and will sync automatically.",
                    )
                }
                is AppResult.Err -> {
                    analytics.track(
                        AnalyticsEvents.HEALTH_WRITE_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.KIND to "case",
                            AnalyticsEvents.Params.REASON to result.message,
                        ),
                    )
                    _state.value = _state.value.copy(submitting = false, message = result.message)
                }
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
    private val proofCaptureSource: ProofCaptureSource,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val drafts: CaptureDraftRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {
    private val sessionId: String = checkNotNull(savedStateHandle["healthSessionId"])
    private val ops = MutableStateFlow(HealthDetailOps())
    private val message = MutableStateFlow<String?>(null)
    private val video = MutableStateFlow(HealthVideoState())

    /** Latest Room-backed detail, for the capture/close context (goat, case, band, date). */
    private var latestDetail: HealthWorkItemDetailDto? = null

    /** Durable per session; survives process death and re-entry, so a recorded clip renders
     *  "recorded" instead of asking for the camera again. */
    private var draft = CaptureDraft()
    private var proofStatusJob: Job? = null

    val state: StateFlow<HealthDetailUiState> = combine(
        repo.observeDetail(sessionId), ops, message, video,
    ) { detail, opsState, notice, videoState ->
        val saving = opsState.submitting
        val closingCase = opsState.closing
        latestDetail = detail
        if (detail == null) HealthDetailUiState(loading = true, submitting = saving, message = notice)
        else HealthDetailUiState(
            loading = false,
            goatDisplayId = detail.goatDisplayId,
            diseaseName = detail.diseaseName,
            dayLabel = healthCourseDayLabel(detail.dayNo, detail.durationDays) +
                " · ${detail.session.replaceFirstChar { it.uppercase() }}",
            locationLabel = listOf(detail.parkLabel, detail.shedLabel).filter(String::isNotBlank).joinToString(" · "),
            status = detail.status,
            steps = detail.steps.map(HealthTreatmentStepDto::toUi),
            submitting = saving,
            closing = closingCase,
            refreshing = opsState.refreshing,
            message = notice,
            // Backend-owned capability gating (can_complete mirrors health.execute): the audit
            // found the button rendering for principals whose tap could only ever 403.
            canComplete = detail.canComplete && detail.status in OPEN_STATUSES && !saving &&
                videoState.captured && !videoState.capturing,
            canRecordVideo = detail.canComplete && detail.status in OPEN_STATUSES && !saving,
            canCloseCase = detail.canCloseCase && detail.status !in CLOSED_SESSION_STATUSES && !closingCase,
            videoCaptured = videoState.captured,
            isCapturingVideo = videoState.capturing,
            videoMessage = videoState.message,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), HealthDetailUiState())

    init {
        analytics.track(AnalyticsEvents.HEALTH_VIEWED, mapOf(AnalyticsEvents.Params.KIND to "work_item"))
        refresh()
        viewModelScope.launch {
            draft = drafts.find(CaptureFlow.HEALTH_TREATMENT, sessionId)
            val proofItem = draft.proofs[STEP_VIDEO]
            if (proofItem != null) {
                video.update { it.copy(captured = true, message = VIDEO_QUEUED) }
                observeProofItem(proofItem)
            }
        }
        observeDurableProof()
    }

    fun refresh() = viewModelScope.launch {
        if (ops.value.refreshing) return@launch
        ops.update { it.copy(refreshing = true) }
        try {
            repo.refreshDetail(sessionId)
        } finally {
            ops.update { it.copy(refreshing = false) }
        }
    }

    /** MANDATORY treatment video — a LIVE in-app camera clip, enqueued as a PROOF_UPLOAD on the
     *  SESSION group so it drains before the completion that references it. Camera-only. */
    fun recordVideo(replacing: Boolean = false) {
        val detail = latestDetail ?: return
        if (video.value.capturing) return
        if (!replacing && video.value.captured) return
        video.update { it.copy(capturing = true, message = null) }
        viewModelScope.launch {
            val captured = try {
                proofCaptureSource.captureVideo(
                    ProofCaptureContext(
                        title = "Health treatment · ${detail.diseaseName}",
                        primaryTag = detail.goatDisplayId.ifBlank { detail.goatId },
                        workLabel = "Day ${detail.dayNo} · ${detail.session}",
                    ),
                )
            } catch (error: Exception) {
                crashReporter.recordException(error, "health treatment video capture failed")
                null
            }
            if (captured == null) {
                video.update { it.copy(capturing = false) }
                return@launch
            }
            val slot = EvidenceSlot(
                identity = ProofIdentity(flow = ProofFlow.HEALTH, taskId = sessionId),
                fieldKey = FIELD_HEALTH_TREATMENT_VIDEO,
            )
            when (
                val result = proofCaptureRepository.captureReplacingLatest(
                    slot = slot,
                    subject = ProofSubject.GOAT,
                    subjectId = detail.goatId,
                    localUri = captured.localUri,
                    mimeType = captured.mimeType,
                    caption = "Health · ${detail.diseaseName} · ${detail.goatDisplayId.ifBlank { detail.goatId }} · Day ${detail.dayNo}",
                    scopeType = "goat",
                    scopeId = detail.goatId,
                    capturedStartMs = captured.startedAtMs,
                    capturedEndMs = captured.endedAtMs,
                    capturedByPrincipalId = null,
                    proofPolicy = healthTreatmentProofPolicy(captured.captureSource),
                    awaitUploadEnqueue = true,
                    // SAME group as the completion (the session id), so the upload drains
                    // strictly before the completion write that references it.
                    uploadGroupKey = sessionId,
                )
            ) {
                is AppResult.Ok -> {
                    val proofOutboxId = result.value.outboxItemId
                    if (proofOutboxId.isNullOrBlank()) {
                        video.update { it.copy(capturing = false, captured = false, message = PROOF_FAILED) }
                        return@launch
                    }
                    drafts.putProof(CaptureFlow.HEALTH_TREATMENT, sessionId, STEP_VIDEO, proofOutboxId)
                    draft = drafts.find(CaptureFlow.HEALTH_TREATMENT, sessionId)
                    observeProofItem(proofOutboxId)
                    analytics.track(AnalyticsEvents.HEALTH_TREATMENT_VIDEO_CAPTURED)
                    video.update { it.copy(capturing = false, captured = true, message = VIDEO_QUEUED) }
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "health treatment video enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.HEALTH_WRITE_FAILURE,
                        mapOf(AnalyticsEvents.Params.KIND to "work_item", AnalyticsEvents.Params.REASON to result.message),
                    )
                    video.update { it.copy(capturing = false, captured = false, message = PROOF_FAILED) }
                }
            }
        }
    }

    private fun observeProofItem(itemId: String) {
        proofStatusJob?.cancel()
        proofStatusJob = viewModelScope.launch {
            syncRepository.observeItem(itemId)
                .filterNotNull()
                .distinctUntilChanged()
                .collect { item ->
                    val note = when (item.status) {
                        SyncItemStatus.QUEUED -> VIDEO_QUEUED
                        SyncItemStatus.IN_FLIGHT -> PROOF_UPLOADING
                        SyncItemStatus.SUCCEEDED -> PROOF_SYNCED
                        SyncItemStatus.FAILED -> item.lastError ?: PROOF_FAILED
                    }
                    video.update {
                        it.copy(
                            captured = it.captured || item.status != SyncItemStatus.FAILED,
                            message = note,
                        )
                    }
                    if (item.status == SyncItemStatus.FAILED) {
                        video.update { it.copy(captured = false) }
                    }
                }
        }
    }

    /** Re-hydrate a clip recorded before process death from the durable capture rows. */
    private fun observeDurableProof() {
        viewModelScope.launch {
            proofCaptureRepository.observeProofs(sessionId).collect { rows ->
                val row = rows
                    .filter { it.fieldKey == FIELD_HEALTH_TREATMENT_VIDEO && it.syncStatus != CaptureSyncStatus.FAILED }
                    .maxByOrNull { it.capturedAtMs }
                    ?: return@collect
                row.outboxItemId?.takeIf { it.isNotBlank() }?.let { outboxId ->
                    if (draft.proofs[STEP_VIDEO] != outboxId) {
                        drafts.putProof(CaptureFlow.HEALTH_TREATMENT, sessionId, STEP_VIDEO, outboxId)
                        draft = drafts.find(CaptureFlow.HEALTH_TREATMENT, sessionId)
                    }
                    observeProofItem(outboxId)
                }
                video.update { it.copy(captured = true) }
            }
        }
    }

    fun complete() = viewModelScope.launch {
        if (ops.value.submitting) return@launch
        val videoItem = draft.proofs[STEP_VIDEO]
        if (videoItem.isNullOrBlank()) {
            video.update { it.copy(message = NEED_VIDEO_MESSAGE) }
            return@launch
        }
        ops.update { it.copy(submitting = true) }
        // The PROOF is part of the key: a retry of the same video replays for free, while a
        // rework re-shoot (new video) is a NEW completion that must not collide with the first
        // one's SUCCEEDED outbox row — the exact silent-drop defect of the 2026-08-29 audit.
        when (val result = syncRepository.enqueueHealthTreatmentComplete(
            healthSessionId = sessionId,
            idempotencyKey = "health-complete:$sessionId:$videoItem",
            proofOutboxItemId = videoItem,
        )) {
            is AppResult.Ok -> {
                repo.markCompleted(sessionId)
                analytics.track(AnalyticsEvents.HEALTH_TREATMENT_SUBMITTED)
                message.value = "Saved offline. Sync will finish automatically."
            }
            is AppResult.Err -> {
                analytics.track(
                    AnalyticsEvents.HEALTH_WRITE_FAILURE,
                    mapOf(AnalyticsEvents.Params.KIND to "work_item", AnalyticsEvents.Params.REASON to result.message),
                )
                message.value = result.message
            }
        }
        ops.update { it.copy(submitting = false) }
    }

    /** Clinical case closure (health.diagnose): recovered / referred / canceled. */
    fun closeCase(outcome: String, note: String) = viewModelScope.launch {
        val detail = latestDetail ?: return@launch
        if (ops.value.closing) return@launch
        ops.update { it.copy(closing = true) }
        when (val result = syncRepository.enqueueHealthCaseClose(
            healthCaseId = detail.caseId,
            outcome = outcome,
            note = note,
            // Outcome + note are part of the key so a retry replays while a corrected decision
            // is a new act; the backend refuses a second closure of a closed case regardless.
            idempotencyKey = "health-close:${detail.caseId}:$outcome:${note.hashCode()}",
            ageBand = detail.ageBand,
            businessDate = detail.businessDate,
            healthSessionId = sessionId,
        )) {
            is AppResult.Ok -> {
                analytics.track(AnalyticsEvents.HEALTH_CASE_CLOSED, mapOf(AnalyticsEvents.Params.KIND to outcome))
                message.value = "Outcome recorded. Sync will finish automatically."
            }
            is AppResult.Err -> {
                analytics.track(
                    AnalyticsEvents.HEALTH_WRITE_FAILURE,
                    mapOf(AnalyticsEvents.Params.KIND to "case_close", AnalyticsEvents.Params.REASON to result.message),
                )
                message.value = result.message
            }
        }
        ops.update { it.copy(closing = false) }
    }

    private data class HealthDetailOps(
        val submitting: Boolean = false,
        val closing: Boolean = false,
        val refreshing: Boolean = false,
    )

    private data class HealthVideoState(
        val captured: Boolean = false,
        val capturing: Boolean = false,
        val message: String? = null,
    )

    companion object {
        private val OPEN_STATUSES = setOf("due", "scheduled", "in_progress", "rework")
        private val CLOSED_SESSION_STATUSES = setOf("canceled_death", "canceled")
        private const val STEP_VIDEO = "video"
        private const val FIELD_HEALTH_TREATMENT_VIDEO = "health_treatment_video"
        private const val VIDEO_QUEUED = "Treatment video saved on this phone. It will upload automatically."
        private const val PROOF_UPLOADING = "Treatment video upload is in progress."
        private const val PROOF_SYNCED = "Treatment video is ready."
        private const val PROOF_FAILED = "Couldn't save that video. Please record it again."
        private const val NEED_VIDEO_MESSAGE = "Record the treatment video before completing."
    }
}

internal fun healthTreatmentProofPolicy(captureSource: String): ProofPolicy =
    ProofPolicy.Default.copy(
        proofMode = "per_session_video",
        subjectScope = ProofSubject.GOAT.wireValue,
        expectedSubjects = listOf(ProofSubject.GOAT.wireValue),
        captureSource = captureSource,
        // One active clip per session (the slot's task id IS the session); the per-subject budget
        // covers one goat's whole multi-day course plus transient replacement rows.
        maximumCountPerField = 1,
        maximumCountPerSubject = 200,
    )


/** Open-ended courses (no fixed duration) never render "of 0" — they read as ongoing. */
internal fun healthCourseDayLabel(dayNo: Int, durationDays: Int): String =
    if (durationDays > 0) "Day $dayNo of $durationDays" else "Day $dayNo · Ongoing until healed"

private fun HealthWorkItemDto.toUi() = HealthWorkItemUi(
    healthSessionId = healthSessionId,
    goatDisplayId = goatDisplayId.ifBlank { goatId },
    diseaseName = diseaseName,
    dayLabel = healthCourseDayLabel(dayNo, durationDays),
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
    locationLabel = locationPath.operationalLocationDisplay,
    sex = sex,
)

internal fun healthGoatMatchesAgeBand(goat: GoatSearchItemDto, requiredAgeBand: String): Boolean =
    goat.ageBand?.trim()?.lowercase() == requiredAgeBand

private fun HealthSummaryDto.toUi() = HealthSummaryUi(
    total = total,
    due = due,
    scheduled = scheduled,
    inProgress = inProgress,
    completed = completed,
    rework = rework,
    held = held,
    canceledDeath = canceledDeath,
)

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
    return HealthStepUi(stepId.ifBlank { "$dayNo-$session-$seq" }, title, details, criticalActionType != null, status)
}
