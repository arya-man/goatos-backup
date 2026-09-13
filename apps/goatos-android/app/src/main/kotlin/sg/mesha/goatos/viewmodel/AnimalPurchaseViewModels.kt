package sg.mesha.goatos.viewmodel

import android.content.Context
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.paging.PagingData
import androidx.paging.cachedIn
import androidx.paging.map
import dagger.hilt.android.lifecycle.HiltViewModel
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import sg.mesha.goatos.R
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsAnimalPurchase
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.analytics.ProofPreviewActionTrace
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.AnimalPurchaseRepository
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.SalesRepository
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.EvidenceSlot
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofFlow
import sg.mesha.goatos.core.data.capture.ProofIdentity
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.animalPurchaseLoadGroupKey
import sg.mesha.goatos.core.network.dto.AnimalPurchaseAnimalCreateRequestDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseAnimalDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseLoadCreateRequestDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseLoadDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseOptionsDto
import sg.mesha.goatos.feature.vendors.AnimalPurchaseAnimalCardUi
import sg.mesha.goatos.feature.vendors.AnimalPurchaseAnimalCreateEvent
import sg.mesha.goatos.feature.vendors.AnimalPurchaseAnimalCreateUiState
import sg.mesha.goatos.feature.vendors.AnimalPurchaseAnimalField
import sg.mesha.goatos.feature.vendors.AnimalPurchaseCountChipUi
import sg.mesha.goatos.feature.vendors.AnimalPurchaseLoadCardUi
import sg.mesha.goatos.feature.vendors.AnimalPurchaseLoadCreateEvent
import sg.mesha.goatos.feature.vendors.AnimalPurchaseLoadCreateUiState
import sg.mesha.goatos.feature.vendors.AnimalPurchaseLoadDetailEvent
import sg.mesha.goatos.feature.vendors.AnimalPurchaseLoadDetailUiState
import sg.mesha.goatos.feature.vendors.AnimalPurchaseLoadField
import sg.mesha.goatos.feature.vendors.AnimalPurchaseLoadsEvent
import sg.mesha.goatos.feature.vendors.AnimalPurchaseLoadsUiState
import sg.mesha.goatos.feature.vendors.AnimalPurchaseVideoStatus
import sg.mesha.goatos.feature.vendors.COPY_ANIMAL_DECIDED_BY
import sg.mesha.goatos.feature.vendors.COPY_ANIMAL_FORM_TITLE
import sg.mesha.goatos.feature.vendors.COPY_ANIMAL_VIDEO_HINT
import sg.mesha.goatos.feature.vendors.COPY_LOADS_ADD
import sg.mesha.goatos.feature.vendors.COPY_LOADS_EMPTY
import sg.mesha.goatos.feature.vendors.COPY_LOAD_ANIMALS_ADD
import sg.mesha.goatos.feature.vendors.COPY_LOAD_ANIMALS_EMPTY
import sg.mesha.goatos.feature.vendors.COPY_LOAD_ANIMALS_TITLE
import sg.mesha.goatos.feature.vendors.VendorsOptionUi
import sg.mesha.goatos.feature.vendors.VendorsTone
import sg.mesha.goatos.feature.vendors.VendorsWriteStatus
import sg.mesha.goatos.feature.vendors.animalPurchaseDecisionTone
import javax.inject.Inject

/**
 * The Animal purchases tab's state holders (maintainer decision 2026-09-13,
 * docs/decisions/animal-purchases.md). Offline-first: every read renders from the Room-backed
 * [AnimalPurchaseRepository]; every write rides the durable outbox and the form FOLLOWS the queued
 * row rather than closing on enqueue. Nothing here composes business copy — the backend's load /
 * animal fields and the options `copy` map are passed through verbatim.
 */

// ---------------------------------------------------------------------------------------------
// L0: the load list
// ---------------------------------------------------------------------------------------------

@HiltViewModel
class AnimalPurchaseLoadsViewModel @Inject constructor(
    private val repository: AnimalPurchaseRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private data class Scope(val title: String = "", val refreshNonce: Int = 0)

    private val scope = MutableStateFlow(Scope())
    private val _isRefreshing = MutableStateFlow(false)

    init {
        viewModelScope.launch { repository.refreshOptions() }
    }

    /** Binds the backend nav label the shell routed with. Idempotent — recomposition may repeat it. */
    fun bind(title: String) {
        if (scope.value.title == title) return
        scope.value = scope.value.copy(title = title)
        analytics.track(AnalyticsEventsAnimalPurchase.LIST_VIEWED)
    }

    val state: StateFlow<AnimalPurchaseLoadsUiState> = combine(
        _isRefreshing,
        scope,
        repository.observeCanRecord(),
        repository.observeOptions(),
    ) { refreshing, current, canRecord, options ->
        val copy = options?.copy.orEmpty()
        AnimalPurchaseLoadsUiState(
            title = current.title,
            isRefreshing = refreshing,
            emptyMessage = copy[COPY_LOADS_EMPTY],
            canRecord = canRecord,
            addLabel = copy[COPY_LOADS_ADD].orEmpty(),
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), AnimalPurchaseLoadsUiState())

    @OptIn(ExperimentalCoroutinesApi::class)
    val rows: Flow<PagingData<AnimalPurchaseLoadCardUi>> = scope
        .flatMapLatest { repository.loads().map { page -> page.map { it.toCardUi() } } }
        .cachedIn(viewModelScope)

    fun onEvent(event: AnimalPurchaseLoadsEvent) {
        when (event) {
            AnimalPurchaseLoadsEvent.Refresh -> refresh()
            is AnimalPurchaseLoadsEvent.OpenLoad -> analytics.track(
                AnalyticsEventsAnimalPurchase.LOAD_OPENED,
                mapOf(AnalyticsEventsAnimalPurchase.Params.LOAD_ID to event.loadId),
            )
            AnimalPurchaseLoadsEvent.AddLoad -> analytics.track(AnalyticsEventsAnimalPurchase.LOAD_ADD_OPENED)
        }
    }

    /** Paging surfaced a load failure. The cached rows keep serving; this only reports it. */
    fun onRowsLoadFailed(error: Throwable) {
        crashReporter.recordException(error, "animal purchase load list page load failed")
        analytics.track(
            AnalyticsEventsAnimalPurchase.FAILURE,
            mapOf(AnalyticsEvents.Params.REASON to (error.message ?: "unknown").take(MAX_REASON_CHARS)),
        )
    }

    /** Non-blocking by contract: a failure leaves the cached rows on screen. */
    private fun refresh() {
        viewModelScope.launch {
            _isRefreshing.value = true
            try {
                // exception:exempt local cache-marker delete; a failure just leaves the TTL skip
                runCatching { repository.invalidateLoads() }
                repository.refreshOptions()
                // A NEW value, not an equal one: MutableStateFlow conflates on equality.
                scope.value = scope.value.let { it.copy(refreshNonce = it.refreshNonce + 1) }
            } finally {
                _isRefreshing.value = false
            }
        }
    }
}

/** Maps one backend load row to its list card. Backend copy is rendered verbatim. */
internal fun AnimalPurchaseLoadDto.toCardUi(): AnimalPurchaseLoadCardUi = AnimalPurchaseLoadCardUi(
    listKey = loadId,
    loadId = loadId,
    title = title,
    summary = summary,
    pending = counts.pending,
    accepted = counts.accepted,
    rejected = counts.rejected,
)

// ---------------------------------------------------------------------------------------------
// L1: add a load
// ---------------------------------------------------------------------------------------------

@HiltViewModel
class AnimalPurchaseLoadCreateViewModel @Inject constructor(
    savedStateHandle: SavedStateHandle,
    private val repository: AnimalPurchaseRepository,
    private val salesRepository: SalesRepository,
    private val syncRepository: SyncRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private data class Local(
        val values: Map<AnimalPurchaseLoadField, String> = emptyMap(),
        val fieldErrors: Set<AnimalPurchaseLoadField> = emptySet(),
        val writeStatus: VendorsWriteStatus = VendorsWriteStatus.IDLE,
        val writeMessage: String = "",
        val submitInFlight: Boolean = false,
        val createdLoadId: String? = null,
    )

    private val local = MutableStateFlow(Local())

    /** STABLE across process death: a retry of this draft replays the SAME load on the server. */
    private val draftKey = DraftIdempotencyKey(savedStateHandle, KEY_LOAD_DRAFT, "ap-load")

    /** The queued row id, persisted so a recreated ViewModel keeps following the same write. */
    private val queuedItemId = DraftOutboxItemId(savedStateHandle, KEY_LOAD_OUTBOX_ITEM)

    init {
        analytics.track(AnalyticsEventsAnimalPurchase.LOAD_ADD_OPENED)
        viewModelScope.launch { repository.refreshOptions() }
        viewModelScope.launch { salesRepository.refreshVendorOptions() }
        queuedItemId.value?.let { followWrite(it) }
    }

    val state: StateFlow<AnimalPurchaseLoadCreateUiState> = combine(
        local,
        repository.observeOptions(),
        salesRepository.observeVendorOptions(),
    ) { l, options, vendorOptions ->
        val o = options ?: AnimalPurchaseOptionsDto()
        val farms = o.farms.map { VendorsOptionUi(it.value, it.label) }
        AnimalPurchaseLoadCreateUiState(
            copy = o.copy,
            // One farm to choose from is no choice at all: preselect it.
            values = if (l.values[AnimalPurchaseLoadField.FARM].isNullOrBlank() && farms.size == 1) l.values + (AnimalPurchaseLoadField.FARM to farms.first().value) else l.values,
            vendors = vendorOptions?.vendors.orEmpty().map { VendorsOptionUi(it.vendorId, it.businessName) },
            farms = farms,
            fieldErrors = l.fieldErrors,
            writeStatus = l.writeStatus,
            writeMessage = l.writeMessage,
            submitInFlight = l.submitInFlight,
            createdLoadId = l.createdLoadId,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), AnimalPurchaseLoadCreateUiState())

    fun onEvent(event: AnimalPurchaseLoadCreateEvent) {
        val locked = local.value.writeStatus == VendorsWriteStatus.QUEUED || local.value.writeStatus == VendorsWriteStatus.SYNCED
        when (event) {
            is AnimalPurchaseLoadCreateEvent.FieldChanged -> if (!locked) {
                local.update { it.copy(values = it.values + (event.field to event.value), fieldErrors = it.fieldErrors - event.field) }
            }
            AnimalPurchaseLoadCreateEvent.Submit -> if (!locked) submit()
            AnimalPurchaseLoadCreateEvent.DismissMessage -> local.update { it.copy(writeMessage = "", writeStatus = if (it.writeStatus == VendorsWriteStatus.FAILED) VendorsWriteStatus.IDLE else it.writeStatus) }
            AnimalPurchaseLoadCreateEvent.Back, is AnimalPurchaseLoadCreateEvent.OpenCreatedLoad -> Unit
        }
    }

    private fun submit() {
        val values = state.value.values
        val errors = buildSet {
            if (values[AnimalPurchaseLoadField.LOAD_REF].isNullOrBlank()) add(AnimalPurchaseLoadField.LOAD_REF)
            if (values[AnimalPurchaseLoadField.VENDOR].isNullOrBlank()) add(AnimalPurchaseLoadField.VENDOR)
            if (values[AnimalPurchaseLoadField.FARM].isNullOrBlank()) add(AnimalPurchaseLoadField.FARM)
            val expected = values[AnimalPurchaseLoadField.EXPECTED_COUNT].orEmpty().trim()
            if (expected.isNotBlank() && (expected.toIntOrNull() == null || expected.toInt() < 0)) add(AnimalPurchaseLoadField.EXPECTED_COUNT)
        }
        if (errors.isNotEmpty()) {
            local.update { it.copy(fieldErrors = errors) }
            return
        }
        val request = AnimalPurchaseLoadCreateRequestDto(
            loadRef = values[AnimalPurchaseLoadField.LOAD_REF].orEmpty().trim(),
            vendorId = values[AnimalPurchaseLoadField.VENDOR].orEmpty().trim(),
            farm = values[AnimalPurchaseLoadField.FARM].orEmpty().trim(),
            expectedCount = values[AnimalPurchaseLoadField.EXPECTED_COUNT].orEmpty().trim().toIntOrNull() ?: 0,
            notes = values[AnimalPurchaseLoadField.NOTES].orEmpty().trim(),
        )
        viewModelScope.launch {
            local.update { it.copy(submitInFlight = true, fieldErrors = emptySet()) }
            when (val result = syncRepository.enqueueAnimalPurchaseLoadCreate(draftKey.current(), request)) {
                is AppResult.Ok -> {
                    analytics.track(AnalyticsEventsAnimalPurchase.LOAD_QUEUED)
                    queuedItemId.value = result.value
                    local.update { it.copy(submitInFlight = false, writeStatus = VendorsWriteStatus.QUEUED, writeMessage = MESSAGE_SAVING) }
                    followWrite(result.value)
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "animal purchase load enqueue failed") }
                    analytics.track(AnalyticsEventsAnimalPurchase.FAILURE, mapOf(AnalyticsEvents.Params.REASON to result.message.take(MAX_REASON_CHARS)))
                    local.update { it.copy(submitInFlight = false, writeStatus = VendorsWriteStatus.FAILED, writeMessage = MESSAGE_NOT_SAVED) }
                }
            }
        }
    }

    /**
     * Follows the queued row. The form never closes on enqueue: it opens the recorded load only
     * once the server ACCEPTED it (the load id is the server's), stays with an offline banner
     * while the row is unsent, and shows the server's own sentence when the write was refused —
     * a 4xx is terminal and re-sending the same load number cannot succeed.
     */
    private fun followWrite(itemId: String) {
        viewModelScope.launch {
            syncRepository.followQueuedWrite(itemId).collect { outcome ->
                when (outcome) {
                    QueuedWriteOutcome.Saved -> {
                        val loadId = syncRepository.observeItem(itemId).first()?.resultJson
                            ?.let { json -> runCatching { syncJson.decodeFromString<AnimalPurchaseLoadDto>(json).loadId }.getOrNull() }
                            ?.takeIf { it.isNotBlank() }
                        track(outcome, "saved")
                        local.update { it.copy(writeStatus = VendorsWriteStatus.SYNCED, writeMessage = MESSAGE_SAVED, createdLoadId = loadId) }
                    }
                    QueuedWriteOutcome.StillQueued -> {
                        track(outcome, "queued")
                        local.update { it.copy(writeStatus = VendorsWriteStatus.QUEUED, writeMessage = MESSAGE_QUEUED) }
                    }
                    is QueuedWriteOutcome.Rejected -> {
                        track(outcome, "rejected")
                        queuedItemId.value = null
                        // A refused draft is spent: the next attempt is a NEW record under a new key.
                        draftKey.invalidate()
                        local.update { it.copy(writeStatus = VendorsWriteStatus.FAILED, writeMessage = outcome.reason?.takeIf { r -> r.isNotBlank() } ?: MESSAGE_NOT_SAVED) }
                    }
                }
            }
        }
    }

    private fun track(outcome: QueuedWriteOutcome, status: String) {
        analytics.track(
            AnalyticsEventsAnimalPurchase.WRITE_OUTCOME,
            buildMap {
                put(AnalyticsEvents.Params.KIND, "load")
                put(AnalyticsEvents.Params.STATUS, status)
                (outcome as? QueuedWriteOutcome.Rejected)?.reason?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.REASON, it.take(MAX_REASON_CHARS)) }
            },
        )
    }

    private companion object {
        const val KEY_LOAD_DRAFT = "animalPurchase.load.draftKey"
        const val KEY_LOAD_OUTBOX_ITEM = "animalPurchase.load.outboxItemId"
    }
}

// ---------------------------------------------------------------------------------------------
// L1: one load and its animals
// ---------------------------------------------------------------------------------------------

@HiltViewModel
class AnimalPurchaseLoadDetailViewModel @Inject constructor(
    private val repository: AnimalPurchaseRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val loadId: String = savedStateHandle.get<String>(ARG_LOAD_ID).orEmpty()
    private val _isRefreshing = MutableStateFlow(false)

    init {
        viewModelScope.launch { repository.refreshOptions() }
    }

    val state: StateFlow<AnimalPurchaseLoadDetailUiState> = combine(
        _isRefreshing,
        repository.observeLoad(loadId),
        repository.observeCanRecord(),
        repository.observeOptions(),
    ) { refreshing, load, canRecord, options ->
        val copy = options?.copy.orEmpty()
        AnimalPurchaseLoadDetailUiState(
            title = load?.title.orEmpty(),
            summary = load?.summary.orEmpty(),
            countChips = load?.let { it.countChips(copy) }.orEmpty(),
            isRefreshing = refreshing,
            canRecord = canRecord,
            addLabel = copy[COPY_LOAD_ANIMALS_ADD].orEmpty(),
            animalsTitle = copy[COPY_LOAD_ANIMALS_TITLE].orEmpty(),
            emptyMessage = copy[COPY_LOAD_ANIMALS_EMPTY],
            decidedByLabel = copy[COPY_ANIMAL_DECIDED_BY].orEmpty(),
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), AnimalPurchaseLoadDetailUiState())

    /** The load's animals, a Room window filled by a mediator whose refresh IS the detail read. */
    val rows: Flow<PagingData<AnimalPurchaseAnimalCardUi>> =
        repository.animals(loadId).map { page -> page.map { it.toCardUi() } }.cachedIn(viewModelScope)

    fun onEvent(event: AnimalPurchaseLoadDetailEvent) {
        when (event) {
            // The pager's own refresh (driven by the host) re-reads the header and first page in
            // ONE request; this side only keeps the options current and marks the sync indicator.
            AnimalPurchaseLoadDetailEvent.Refresh -> refresh()
            AnimalPurchaseLoadDetailEvent.AddAnimal -> analytics.track(
                AnalyticsEventsAnimalPurchase.ANIMAL_ADD_OPENED,
                mapOf(AnalyticsEventsAnimalPurchase.Params.LOAD_ID to loadId),
            )
            is AnimalPurchaseLoadDetailEvent.PreviewAction -> analytics.track(
                AnalyticsEventsAnimalPurchase.VIDEO_PREVIEW_ACTION,
                mapOf(AnalyticsEventsAnimalPurchase.Params.LOAD_ID to loadId, AnalyticsEventsAnimalPurchase.Params.ACTION to event.action),
            )
            AnimalPurchaseLoadDetailEvent.Back -> Unit
        }
    }

    fun onRowsLoadFailed(error: Throwable) {
        crashReporter.recordException(error, "animal purchase load detail page load failed")
        analytics.track(
            AnalyticsEventsAnimalPurchase.FAILURE,
            mapOf(
                AnalyticsEventsAnimalPurchase.Params.LOAD_ID to loadId,
                AnalyticsEvents.Params.REASON to (error.message ?: "unknown").take(MAX_REASON_CHARS),
            ),
        )
    }

    private fun refresh() {
        if (loadId.isBlank()) return
        viewModelScope.launch {
            _isRefreshing.value = true
            try {
                repository.refreshOptions()
            } finally {
                _isRefreshing.value = false
            }
        }
    }

    private companion object {
        const val ARG_LOAD_ID = "load_id"
    }
}

/**
 * The three whole-load counts as chips. The backend's `animal.decision.pending` copy names the
 * awaiting state; the accepted/rejected words are the backend's decision labels, which the load
 * header does not carry — so those two chips show the count alone in their tone.
 */
private fun AnimalPurchaseLoadDto.countChips(copy: Map<String, String>): List<AnimalPurchaseCountChipUi> = listOf(
    AnimalPurchaseCountChipUi(label = copy["animal.decision.pending"].orEmpty(), count = counts.pending, tone = VendorsTone.NEUTRAL),
    AnimalPurchaseCountChipUi(label = "", count = counts.accepted, tone = VendorsTone.OK),
    AnimalPurchaseCountChipUi(label = "", count = counts.rejected, tone = VendorsTone.DANGER),
)

/** Maps one backend animal to its card. Backend copy is rendered verbatim. */
internal fun AnimalPurchaseAnimalDto.toCardUi(): AnimalPurchaseAnimalCardUi = AnimalPurchaseAnimalCardUi(
    listKey = candidateId,
    candidateId = candidateId,
    title = title,
    breed = breed,
    ageWeightLine = listOfNotNull(
        ageMonths?.let { "$it $AGE_UNIT" },
        weightKg?.let { "${trimDecimal(it)} $WEIGHT_UNIT" },
    ).joinToString(" · "),
    conditionLabel = conditionLabel,
    tempTag = tempTag,
    decisionLabel = decisionLabel,
    decisionTone = animalPurchaseDecisionTone(decisionTone),
    decidedByName = decidedByName,
    decisionNote = decisionNote,
    mediaUrl = mediaUrl,
)

private fun trimDecimal(value: Double): String =
    if (value == value.toLong().toDouble()) value.toLong().toString() else String.format(java.util.Locale.ROOT, "%.1f", value)

// ---------------------------------------------------------------------------------------------
// L2: add an animal (video mandatory)
// ---------------------------------------------------------------------------------------------

@HiltViewModel
class AnimalPurchaseAnimalCreateViewModel @Inject constructor(
    savedStateHandle: SavedStateHandle,
    private val repository: AnimalPurchaseRepository,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val proofCaptureSource: ProofCaptureSource,
    private val syncRepository: SyncRepository,
    private val bootstrapRepository: BootstrapRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    @ApplicationContext private val appContext: Context,
) : ViewModel() {

    private val loadId: String = savedStateHandle.get<String>(ARG_LOAD_ID).orEmpty()

    /** STABLE across process death: this draft's video slot AND its write key hang off it. */
    private val draftKey = DraftIdempotencyKey(savedStateHandle, KEY_ANIMAL_DRAFT, "ap-animal")
    private val queuedItemId = DraftOutboxItemId(savedStateHandle, KEY_ANIMAL_OUTBOX_ITEM)

    private data class Local(
        val values: Map<AnimalPurchaseAnimalField, String> = emptyMap(),
        val fieldErrors: Set<AnimalPurchaseAnimalField> = emptySet(),
        val videoWorking: Boolean = false,
        val videoMissing: Boolean = false,
        val writeStatus: VendorsWriteStatus = VendorsWriteStatus.IDLE,
        val writeMessage: String = "",
        val submitInFlight: Boolean = false,
        val closeAfterSave: Boolean = false,
    )

    private val local = MutableStateFlow(Local())

    /**
     * The DURABLE slot the video occupies: one identity per draft, so a re-record replaces the clip
     * rather than accumulating, and a clip shot before a ViewModel death is still there to send.
     */
    private val videoSlot = EvidenceSlot(
        identity = ProofIdentity(flow = ProofFlow.ANIMAL_PURCHASE, taskId = loadId, subjectKey = draftKey.current()),
        fieldKey = draftKey.current(),
    )

    init {
        analytics.track(AnalyticsEventsAnimalPurchase.ANIMAL_ADD_OPENED, mapOf(AnalyticsEventsAnimalPurchase.Params.LOAD_ID to loadId))
        viewModelScope.launch { repository.refreshOptions() }
        queuedItemId.value?.let { followWrite(it) }
    }

    val state: StateFlow<AnimalPurchaseAnimalCreateUiState> = combine(
        local,
        repository.observeOptions(),
        proofCaptureRepository.observeLatest(videoSlot),
    ) { l, options, video ->
        val o = options ?: AnimalPurchaseOptionsDto()
        val species = o.species.map { VendorsOptionUi(it.value, it.label) }
        val sexes = o.sexes.map { VendorsOptionUi(it.value, it.label) }
        val conditions = o.conditions.map { VendorsOptionUi(it.value, it.label) }
        AnimalPurchaseAnimalCreateUiState(
            copy = o.copy,
            values = l.values,
            species = species,
            sexes = sexes,
            conditions = conditions,
            breedSuggestions = o.breedSuggestions,
            fieldErrors = l.fieldErrors,
            videoStatus = when {
                l.videoWorking -> AnimalPurchaseVideoStatus.WORKING
                video == null -> AnimalPurchaseVideoStatus.NONE
                video.syncStatus == CaptureSyncStatus.FAILED || video.outboxItemId.isNullOrBlank() -> AnimalPurchaseVideoStatus.FAILED
                else -> AnimalPurchaseVideoStatus.RECORDED
            },
            videoLocalUri = video?.localUri.orEmpty(),
            videoIdentity = video?.id.orEmpty(),
            videoMissing = l.videoMissing,
            writeStatus = l.writeStatus,
            writeMessage = l.writeMessage,
            submitInFlight = l.submitInFlight,
            closeAfterSave = l.closeAfterSave,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), AnimalPurchaseAnimalCreateUiState())

    fun onEvent(event: AnimalPurchaseAnimalCreateEvent) {
        val locked = local.value.writeStatus == VendorsWriteStatus.QUEUED || local.value.writeStatus == VendorsWriteStatus.SYNCED
        when (event) {
            is AnimalPurchaseAnimalCreateEvent.FieldChanged -> if (!locked) {
                local.update { it.copy(values = it.values + (event.field to event.value), fieldErrors = it.fieldErrors - event.field) }
            }
            AnimalPurchaseAnimalCreateEvent.RecordVideo -> if (!locked) recordVideo()
            is AnimalPurchaseAnimalCreateEvent.VideoPreviewAction -> trackPreviewAction(event.action)
            AnimalPurchaseAnimalCreateEvent.Submit -> if (!locked) submit()
            AnimalPurchaseAnimalCreateEvent.DismissMessage -> local.update { it.copy(writeMessage = "", writeStatus = if (it.writeStatus == VendorsWriteStatus.FAILED) VendorsWriteStatus.IDLE else it.writeStatus) }
            AnimalPurchaseAnimalCreateEvent.Back -> Unit
        }
    }

    /**
     * Records the animal's video with the in-app camera. Everything after a REAL recording is
     * durable bookkeeping and runs NonCancellable, so backing out of the screen can never orphan a
     * clip the person actually shot (the PC Care defect of 2026-08-21).
     */
    private fun recordVideo() {
        if (loadId.isBlank() || local.value.videoWorking) return
        viewModelScope.launch {
            local.update { it.copy(videoWorking = true, videoMissing = false) }
            try {
                val copy = state.value.copy
                val captured = try {
                    proofCaptureSource.captureVideo(
                        ProofCaptureContext(
                            title = copy[COPY_ANIMAL_FORM_TITLE].orEmpty(),
                            primaryTag = repository.observeLoad(loadId).first()?.title.orEmpty(),
                            workLabel = copy[COPY_ANIMAL_VIDEO_HINT].orEmpty(),
                            headerTitle = appContext.getString(R.string.proof_video_animal_purchase_header),
                        ),
                    )
                } catch (error: Exception) {
                    if (error is CancellationException) throw error
                    crashReporter.recordException(error, "animal purchase video capture failed")
                    trackFailure("capture_exception", error.message ?: "unknown")
                    null
                } ?: return@launch

                withContext(NonCancellable) {
                    // The proof platform requires a scope: the TENANT, as the maintainer's contract
                    // for this write names it; the load is the subject and there is no animal yet.
                    val tenantId = bootstrapRepository.actorTenantId().orEmpty()
                    if (tenantId.isBlank()) {
                        trackFailure("capture_failed", "tenant_unavailable")
                        local.update { it.copy(writeStatus = VendorsWriteStatus.FAILED, writeMessage = MESSAGE_VIDEO_NOT_SAVED) }
                        return@withContext
                    }
                    val result = proofCaptureRepository.captureReplacingLatest(
                        slot = videoSlot,
                        subject = ProofSubject.OTHER,
                        subjectId = loadId,
                        localUri = captured.localUri,
                        mimeType = captured.mimeType,
                        caption = copy[COPY_ANIMAL_FORM_TITLE].orEmpty(),
                        scopeType = SCOPE_TYPE_TENANT,
                        scopeId = tenantId,
                        capturedStartMs = captured.startedAtMs,
                        capturedEndMs = captured.endedAtMs,
                        capturedByPrincipalId = null,
                        proofPolicy = animalPurchaseProofPolicy(captured.captureSource),
                        awaitUploadEnqueue = true,
                        // ONE FIFO lane per load: the upload drains BEFORE the animal create that
                        // resolves it (AnimalPurchasePayloads.kt).
                        uploadGroupKey = animalPurchaseLoadGroupKey(loadId),
                    )
                    when (result) {
                        is AppResult.Err -> {
                            result.cause?.let { crashReporter.recordException(it, "animal purchase video capture write failed") }
                            trackFailure("capture_failed", result.message)
                            local.update { it.copy(writeStatus = VendorsWriteStatus.FAILED, writeMessage = MESSAGE_VIDEO_NOT_SAVED) }
                        }
                        is AppResult.Ok -> {
                            val outboxId = awaitUploadRow(result.value)
                            if (outboxId == null) {
                                crashReporter.recordException(
                                    IllegalStateException("animal purchase clip ${result.value.id} has no upload row"),
                                    "animal purchase video never enqueued its upload",
                                )
                                trackFailure("capture_saved_without_upload_row", "missing_proof_outbox_id")
                                local.update { it.copy(writeStatus = VendorsWriteStatus.FAILED, writeMessage = MESSAGE_VIDEO_NOT_SAVED) }
                            } else {
                                analytics.track(
                                    AnalyticsEventsAnimalPurchase.ANIMAL_VIDEO_CAPTURED,
                                    mapOf(
                                        AnalyticsEventsAnimalPurchase.Params.LOAD_ID to loadId,
                                        AnalyticsEvents.Params.SOURCE to captured.captureSource,
                                    ),
                                )
                                local.update { it.copy(writeStatus = VendorsWriteStatus.IDLE, writeMessage = "") }
                            }
                        }
                    }
                }
            } finally {
                local.update { it.copy(videoWorking = false) }
            }
        }
    }

    /**
     * The upload-row id can land in Room a beat AFTER the capture result is composed (PC Care's
     * settle wait), so a blank id here is usually a read race, not a lost clip.
     */
    private suspend fun awaitUploadRow(row: ProofCaptureRow): String? {
        var proofOutboxId = row.outboxItemId
        var waited = 0L
        while (proofOutboxId.isNullOrBlank() && waited < PROOF_ROW_SETTLE_MAX_MS) {
            delay(PROOF_ROW_SETTLE_STEP_MS)
            waited += PROOF_ROW_SETTLE_STEP_MS
            proofOutboxId = proofCaptureRepository.observeLatest(videoSlot).first()?.takeIf { it.id == row.id }?.outboxItemId
        }
        return proofOutboxId?.takeIf { it.isNotBlank() }
    }

    private fun submit() {
        val current = state.value
        val v = current.values
        val errors = buildSet {
            if (v[AnimalPurchaseAnimalField.SPECIES].isNullOrBlank()) add(AnimalPurchaseAnimalField.SPECIES)
            if (v[AnimalPurchaseAnimalField.SEX].isNullOrBlank()) add(AnimalPurchaseAnimalField.SEX)
            if (v[AnimalPurchaseAnimalField.CONDITION].isNullOrBlank()) add(AnimalPurchaseAnimalField.CONDITION)
            val age = v[AnimalPurchaseAnimalField.AGE_MONTHS].orEmpty().trim()
            if (age.isNotBlank() && (age.toIntOrNull() == null || age.toInt() < 0)) add(AnimalPurchaseAnimalField.AGE_MONTHS)
            val weight = v[AnimalPurchaseAnimalField.WEIGHT_KG].orEmpty().trim()
            if (weight.isNotBlank() && (weight.toDoubleOrNull() == null || weight.toDouble() <= 0.0)) add(AnimalPurchaseAnimalField.WEIGHT_KG)
        }
        viewModelScope.launch {
            // Read the clip from its DURABLE slot at submit time, so a video shot before a
            // ViewModel death still sends instead of the animal looking unfilmed.
            val video = proofCaptureRepository.observeLatest(videoSlot).first()
            val proofOutboxItemId = video?.takeIf { it.syncStatus != CaptureSyncStatus.FAILED }?.outboxItemId.orEmpty()
            if (errors.isNotEmpty() || proofOutboxItemId.isBlank()) {
                local.update { it.copy(fieldErrors = errors, videoMissing = proofOutboxItemId.isBlank()) }
                if (proofOutboxItemId.isBlank()) trackFailure("animal_not_queued", "missing_video")
                return@launch
            }
            val request = AnimalPurchaseAnimalCreateRequestDto(
                species = v[AnimalPurchaseAnimalField.SPECIES].orEmpty().trim(),
                sex = v[AnimalPurchaseAnimalField.SEX].orEmpty().trim(),
                breed = v[AnimalPurchaseAnimalField.BREED].orEmpty().trim(),
                ageMonths = v[AnimalPurchaseAnimalField.AGE_MONTHS].orEmpty().trim().toIntOrNull(),
                weightKg = v[AnimalPurchaseAnimalField.WEIGHT_KG].orEmpty().trim().toDoubleOrNull(),
                condition = v[AnimalPurchaseAnimalField.CONDITION].orEmpty().trim(),
                tempTag = v[AnimalPurchaseAnimalField.TEMP_TAG].orEmpty().trim(),
                notes = v[AnimalPurchaseAnimalField.NOTES].orEmpty().trim(),
            )
            local.update { it.copy(submitInFlight = true, fieldErrors = emptySet(), videoMissing = false) }
            when (val result = syncRepository.enqueueAnimalPurchaseAnimalCreate(draftKey.current(), loadId, request, proofOutboxItemId)) {
                is AppResult.Ok -> {
                    analytics.track(
                        AnalyticsEventsAnimalPurchase.ANIMAL_QUEUED,
                        mapOf(AnalyticsEventsAnimalPurchase.Params.LOAD_ID to loadId, AnalyticsEvents.Params.STATUS to "queued"),
                    )
                    queuedItemId.value = result.value
                    local.update { it.copy(submitInFlight = false, writeStatus = VendorsWriteStatus.QUEUED, writeMessage = current.copy[COPY_ANIMAL_SAVING_KEY] ?: MESSAGE_SAVING) }
                    followWrite(result.value)
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "animal purchase animal enqueue failed") }
                    trackFailure("animal_not_queued", result.message)
                    local.update { it.copy(submitInFlight = false, writeStatus = VendorsWriteStatus.FAILED, writeMessage = MESSAGE_NOT_SAVED) }
                }
            }
        }
    }

    /** Follows the queued row: return to the load once saved (or durably queued offline); show the
     *  server's own sentence when refused — a 4xx is terminal and never retried as-is. */
    private fun followWrite(itemId: String) {
        viewModelScope.launch {
            syncRepository.followQueuedWrite(itemId).collect { outcome ->
                when (outcome) {
                    QueuedWriteOutcome.Saved -> {
                        trackOutcome("saved", null)
                        local.update { it.copy(writeStatus = VendorsWriteStatus.SYNCED, writeMessage = MESSAGE_SAVED, closeAfterSave = true) }
                    }
                    QueuedWriteOutcome.StillQueued -> {
                        trackOutcome("queued", null)
                        local.update { it.copy(writeStatus = VendorsWriteStatus.QUEUED, writeMessage = MESSAGE_QUEUED, closeAfterSave = true) }
                    }
                    is QueuedWriteOutcome.Rejected -> {
                        trackOutcome("rejected", outcome.reason)
                        queuedItemId.value = null
                        local.update { it.copy(writeStatus = VendorsWriteStatus.FAILED, writeMessage = outcome.reason?.takeIf { r -> r.isNotBlank() } ?: MESSAGE_NOT_SAVED, closeAfterSave = false) }
                    }
                }
            }
        }
    }

    private fun trackPreviewAction(action: String) {
        val trace = ProofPreviewActionTrace.from(action)
        analytics.track(
            AnalyticsEventsAnimalPurchase.VIDEO_PREVIEW_ACTION,
            buildMap {
                put(AnalyticsEventsAnimalPurchase.Params.LOAD_ID, loadId)
                put(AnalyticsEvents.Params.ACTION, trace.action)
                put(AnalyticsEvents.Params.OUTCOME, trace.outcome)
                trace.reason?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.REASON, it.take(MAX_REASON_CHARS)) }
            },
        )
    }

    private fun trackOutcome(status: String, reason: String?) {
        analytics.track(
            AnalyticsEventsAnimalPurchase.WRITE_OUTCOME,
            buildMap {
                put(AnalyticsEventsAnimalPurchase.Params.LOAD_ID, loadId)
                put(AnalyticsEvents.Params.KIND, "animal")
                put(AnalyticsEvents.Params.STATUS, status)
                reason?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.REASON, it.take(MAX_REASON_CHARS)) }
            },
        )
    }

    private fun trackFailure(status: String, reason: String) {
        analytics.track(
            AnalyticsEventsAnimalPurchase.FAILURE,
            mapOf(
                AnalyticsEventsAnimalPurchase.Params.LOAD_ID to loadId,
                AnalyticsEvents.Params.STATUS to status,
                AnalyticsEvents.Params.REASON to reason.take(MAX_REASON_CHARS),
            ),
        )
    }

    private companion object {
        const val ARG_LOAD_ID = "load_id"
        const val KEY_ANIMAL_DRAFT = "animalPurchase.animal.draftKey"
        const val KEY_ANIMAL_OUTBOX_ITEM = "animalPurchase.animal.outboxItemId"
        const val SCOPE_TYPE_TENANT = "tenant"
        const val COPY_ANIMAL_SAVING_KEY = "animal.saving"
        const val PROOF_ROW_SETTLE_MAX_MS = 3_000L
        const val PROOF_ROW_SETTLE_STEP_MS = 100L
        const val MESSAGE_VIDEO_NOT_SAVED = "That video didn't save. Record it again."
    }
}

/** The capture policy of an animal-purchase video: one in-app-camera clip per draft slot. */
internal fun animalPurchaseProofPolicy(captureSource: String): ProofPolicy =
    ProofPolicy.Default.copy(
        proofMode = "animal_purchase_video",
        featureSurface = "animal_purchase",
        featureCategory = "procurement",
        subjectScope = ProofSubject.OTHER.wireValue,
        expectedSubjects = listOf(ProofSubject.OTHER.wireValue),
        captureSource = captureSource,
        maximumCountPerField = 1,
        // One load may hold many animals, each a slot, plus the transient replacement rows a
        // re-shoot makes.
        maximumCountPerSubject = 400,
    )

private val syncJson = kotlinx.serialization.json.Json { ignoreUnknownKeys = true }

private const val MAX_REASON_CHARS = 120
private const val AGE_UNIT = "months"
private const val WEIGHT_UNIT = "kg"
private const val MESSAGE_SAVING = "Saving..."
private const val MESSAGE_SAVED = "Saved."
private const val MESSAGE_QUEUED = "Saved on this phone. It will reach the server when the phone is online."
private const val MESSAGE_NOT_SAVED = "That didn't save. Try again."
