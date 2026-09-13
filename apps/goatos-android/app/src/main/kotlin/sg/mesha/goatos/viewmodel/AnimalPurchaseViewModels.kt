package sg.mesha.goatos.viewmodel

import android.content.Context
import androidx.lifecycle.SavedStateHandle
import sg.mesha.goatos.BuildConfig
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
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.put
import sg.mesha.goatos.R
import sg.mesha.goatos.capture.PhotoCaptureContext
import sg.mesha.goatos.capture.PhotoCaptureSource
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsAnimalPurchase
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.analytics.ProofPreviewActionTrace
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.AnimalPurchaseAnswers
import sg.mesha.goatos.core.data.AnimalPurchaseRepository
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.SalesRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofFlow
import sg.mesha.goatos.core.data.QueuedAnimalPurchaseAnimal
import sg.mesha.goatos.core.data.capture.ProofIdentity
import sg.mesha.goatos.feature.vendors.AnimalPurchaseQueuedAnimalUi
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.animalPurchaseDraftGroupKey
import sg.mesha.goatos.core.network.dto.AnimalPurchaseAnimalCreateRequestDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseAnimalDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseMediaItemDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseQuestionDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseLoadCreateRequestDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseLoadDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseOptionDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseOptionsDto
import sg.mesha.goatos.feature.vendors.AnimalPurchaseAnimalCardUi
import sg.mesha.goatos.feature.vendors.AnimalPurchaseAnimalDetailEvent
import sg.mesha.goatos.feature.vendors.AnimalPurchaseAnimalDetailUiState
import sg.mesha.goatos.feature.vendors.AnimalPurchaseAnswerRowUi
import sg.mesha.goatos.feature.vendors.AnimalPurchaseAnswerSectionUi
import sg.mesha.goatos.feature.vendors.AnimalPurchaseMediaItemUi
import sg.mesha.goatos.feature.vendors.AnimalPurchaseMediaSlotUi
import sg.mesha.goatos.feature.vendors.COPY_ANIMAL_DETAIL_ANSWERS
import sg.mesha.goatos.feature.vendors.COPY_ANIMAL_DETAIL_ATTENTION
import sg.mesha.goatos.feature.vendors.COPY_ANIMAL_DETAIL_EMPTY
import sg.mesha.goatos.feature.vendors.COPY_ANIMAL_DETAIL_MEDIA
import sg.mesha.goatos.feature.vendors.AnimalPurchaseAnimalCreateEvent
import sg.mesha.goatos.feature.vendors.AnimalPurchaseAnimalCreateUiState
import sg.mesha.goatos.feature.vendors.AnimalPurchaseCaptureUi
import sg.mesha.goatos.feature.vendors.AnimalPurchaseQuestionKind
import sg.mesha.goatos.feature.vendors.AnimalPurchaseQuestionUi
import sg.mesha.goatos.feature.vendors.AnimalPurchaseCountChipUi
import sg.mesha.goatos.feature.vendors.AnimalPurchaseLoadCardUi
import sg.mesha.goatos.feature.vendors.AnimalPurchaseLoadCreateEvent
import sg.mesha.goatos.feature.vendors.AnimalPurchaseLoadCreateUiState
import sg.mesha.goatos.feature.vendors.AnimalPurchaseLoadDetailEvent
import sg.mesha.goatos.feature.vendors.AnimalPurchaseLoadDetailUiState
import sg.mesha.goatos.feature.vendors.AnimalPurchaseLoadField
import sg.mesha.goatos.feature.vendors.AnimalPurchaseLoadsEvent
import sg.mesha.goatos.feature.vendors.AnimalPurchaseLoadsUiState
import sg.mesha.goatos.feature.vendors.COPY_ANIMAL_DECIDED_BY
import sg.mesha.goatos.feature.vendors.COPY_ANIMAL_FORM_HINT
import sg.mesha.goatos.feature.vendors.COPY_ANIMAL_FORM_TITLE
import sg.mesha.goatos.feature.vendors.COPY_ANIMAL_OTHER_HINT
import sg.mesha.goatos.feature.vendors.COPY_ANIMAL_SEND_FAILED
import sg.mesha.goatos.feature.vendors.COPY_REQUIRED_HINT
import sg.mesha.goatos.feature.vendors.OTHER_OPTION_VALUE
import sg.mesha.goatos.feature.vendors.OTHER_SUFFIX
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
    /** Typed values outlive a process death (the pen-visit lesson): the form comes back as left. */
    private val savedValues = SavedFormValues(savedStateHandle, KEY_LOAD_VALUES, AnimalPurchaseLoadField::valueOf)

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
                savedValues.write(local.value.values)
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
    private val syncRepository: SyncRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val loadId: String = savedStateHandle.get<String>(ARG_LOAD_ID).orEmpty()
    private val _isRefreshing = MutableStateFlow(false)

    /** Each not-yet-sent animal paired with whether ANY of its capture upload rows has given up. */
    private val queuedWithProofState: Flow<List<Pair<QueuedAnimalPurchaseAnimal, Boolean>>> =
        repository.observeQueuedAnimals(loadId).flatMapLatest { queued ->
            if (queued.isEmpty()) flowOf(emptyList())
            else combine(queued.map { animal -> animal.anyUploadGaveUp().map { gaveUp -> animal to gaveUp } }) { it.toList() }
        }

    private fun QueuedAnimalPurchaseAnimal.anyUploadGaveUp(): Flow<Boolean> =
        if (proofOutboxItemIds.isEmpty()) flowOf(false)
        else combine(proofOutboxItemIds.map { id -> syncRepository.observeItem(id).map { it?.isTerminalFailure == true } }) { flags -> flags.any { it } }

    /** Re-arms every exhausted capture upload; the animal create behind them drains on its own after. */
    private fun retryQueued(outboxItemId: String) {
        viewModelScope.launch {
            val animal = repository.observeQueuedAnimals(loadId).first().firstOrNull { it.outboxItemId == outboxItemId } ?: return@launch
            analytics.track(AnalyticsEventsAnimalPurchase.ANIMAL_RETRY, mapOf(AnalyticsEventsAnimalPurchase.Params.LOAD_ID to loadId))
            animal.proofOutboxItemIds.forEach { id ->
                if (syncRepository.observeItem(id).first()?.isTerminalFailure == true) syncRepository.retry(id)
            }
        }
    }

    init {
        viewModelScope.launch { repository.refreshOptions() }
    }

    val state: StateFlow<AnimalPurchaseLoadDetailUiState> = combine(
        _isRefreshing,
        repository.observeLoad(loadId),
        repository.observeCanRecord(),
        repository.observeOptions(),
        queuedWithProofState,
    ) { refreshing, load, canRecord, options, queued ->
        val copy = options?.copy.orEmpty()
        AnimalPurchaseLoadDetailUiState(
            queuedAnimals = queued.map { (animal, proofFailed) -> animal.toQueuedUi(options, proofFailed) },
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
            is AnimalPurchaseLoadDetailEvent.RetryQueued -> retryQueued(event.listKey)
            is AnimalPurchaseLoadDetailEvent.OpenAnimal -> analytics.track(
                AnalyticsEventsAnimalPurchase.ANIMAL_OPENED,
                mapOf(AnalyticsEventsAnimalPurchase.Params.LOAD_ID to loadId, AnalyticsEventsAnimalPurchase.Params.CANDIDATE_ID to event.candidateId),
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

/**
 * One recorded animal (L2 under the load): reads the SAME cached row the load's list renders, so
 * the record is on screen instantly and re-renders when the load's pager or a decision push
 * refreshes it. Refresh re-reads the load (header + first page), which rewrites this row.
 */
@HiltViewModel
class AnimalPurchaseAnimalDetailViewModel @Inject constructor(
    private val repository: AnimalPurchaseRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val loadId: String = savedStateHandle.get<String>(ARG_LOAD_ID).orEmpty()
    private val candidateId: String = savedStateHandle.get<String>(ARG_CANDIDATE_ID).orEmpty()
    private val _isRefreshing = MutableStateFlow(false)

    val state: StateFlow<AnimalPurchaseAnimalDetailUiState> = combine(
        _isRefreshing,
        repository.observeLoad(loadId),
        repository.observeAnimal(loadId, candidateId),
        repository.observeOptions(),
    ) { refreshing, load, animal, options ->
        val copy = options?.copy.orEmpty()
        animal?.toDetailUi(load, copy, refreshing) ?: AnimalPurchaseAnimalDetailUiState(
            loadTitle = load?.title.orEmpty(),
            emptyMessage = copy[COPY_ANIMAL_DETAIL_EMPTY],
            isRefreshing = refreshing,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), AnimalPurchaseAnimalDetailUiState())

    fun onEvent(event: AnimalPurchaseAnimalDetailEvent) {
        when (event) {
            AnimalPurchaseAnimalDetailEvent.Refresh -> refresh()
            is AnimalPurchaseAnimalDetailEvent.PreviewAction -> analytics.track(
                AnalyticsEventsAnimalPurchase.VIDEO_PREVIEW_ACTION,
                mapOf(AnalyticsEventsAnimalPurchase.Params.LOAD_ID to loadId, AnalyticsEventsAnimalPurchase.Params.ACTION to event.action),
            )
            AnimalPurchaseAnimalDetailEvent.Back -> Unit
        }
    }

    private fun refresh() {
        if (loadId.isBlank()) return
        viewModelScope.launch {
            _isRefreshing.value = true
            try {
                repository.refreshLoad(loadId)
            } catch (t: Throwable) {
                if (t is kotlinx.coroutines.CancellationException) throw t
                // The cached record stays on screen; the failure is recorded, never a blank wall.
                crashReporter.recordException(t, "animal purchase animal detail refresh failed")
                analytics.track(
                    AnalyticsEventsAnimalPurchase.FAILURE,
                    mapOf(
                        AnalyticsEventsAnimalPurchase.Params.LOAD_ID to loadId,
                        AnalyticsEvents.Params.REASON to (t.message ?: "unknown").take(MAX_REASON_CHARS),
                    ),
                )
            } finally {
                _isRefreshing.value = false
            }
        }
    }

    private companion object {
        const val ARG_LOAD_ID = "load_id"
        const val ARG_CANDIDATE_ID = "candidate_id"
    }
}

/** The animal's full record: answers grouped by their backend section IN ORDER, media by slot. */
internal fun AnimalPurchaseAnimalDto.toDetailUi(
    load: AnimalPurchaseLoadDto?,
    copy: Map<String, String>,
    refreshing: Boolean = false,
): AnimalPurchaseAnimalDetailUiState {
    val sections = mutableListOf<AnimalPurchaseAnswerSectionUi>()
    answerRows.forEach { row ->
        val ui = AnimalPurchaseAnswerRowUi(questionId = row.questionId, question = row.question, answer = row.answer, attention = row.attention)
        val last = sections.lastOrNull()
        if (last != null && last.title == row.section) {
            sections[sections.lastIndex] = last.copy(rows = last.rows + ui)
        } else {
            sections += AnimalPurchaseAnswerSectionUi(title = row.section, rows = listOf(ui))
        }
    }
    val slots = if (questionnaireVersion > 0) {
        mediaSlots.map { slot ->
            AnimalPurchaseMediaSlotUi(
                slot = slot.slot,
                title = slot.title,
                items = slot.items.filter { it.mediaUrl.isNotBlank() }.map { item ->
                    AnimalPurchaseMediaItemUi(
                        proofRef = item.proofRef.ifBlank { candidateId },
                        url = animalPurchaseMediaUrl(item.mediaUrl),
                        isPhoto = item.mediaMime.startsWith("image/", ignoreCase = true),
                    )
                },
            )
        }.filter { it.items.isNotEmpty() }
    } else {
        // A legacy row (questionnaire_version 0) has one video and no slot title of its own.
        listOfNotNull(
            mediaUrl.takeIf { it.isNotBlank() }?.let {
                AnimalPurchaseMediaSlotUi(
                    slot = "",
                    title = copy[COPY_ANIMAL_DETAIL_MEDIA].orEmpty(),
                    items = listOf(AnimalPurchaseMediaItemUi(proofRef = videoProofRef.ifBlank { candidateId }, url = animalPurchaseMediaUrl(it), isPhoto = mediaMime.startsWith("image/", ignoreCase = true))),
                )
            },
        )
    }
    val decidedByLabel = copy[COPY_ANIMAL_DECIDED_BY].orEmpty()
    return AnimalPurchaseAnimalDetailUiState(
        title = title,
        loadTitle = load?.title.orEmpty(),
        decisionLabel = decisionLabel,
        decisionTone = animalPurchaseDecisionTone(decisionTone),
        fieldVerdictLabel = fieldVerdictLabel,
        fieldVerdictTone = animalPurchaseFieldVerdictTone(fieldVerdict),
        decidedByLine = if (decidedByName.isBlank()) "" else listOf(decidedByLabel, decidedByName).filter { it.isNotBlank() }.joinToString(" "),
        decisionNote = decisionNote,
        sections = sections,
        mediaSlots = slots,
        answersTitle = copy[COPY_ANIMAL_DETAIL_ANSWERS].orEmpty(),
        mediaTitle = copy[COPY_ANIMAL_DETAIL_MEDIA].orEmpty(),
        attentionLabel = copy[COPY_ANIMAL_DETAIL_ATTENTION].orEmpty(),
        emptyMessage = copy[COPY_ANIMAL_DETAIL_EMPTY],
        isRefreshing = refreshing,
    )
}

/**
 * A signed proof link the phone can actually fetch. The API signs a ROOT-RELATIVE path
 * (`/app/proofs/<id>/download/signed?...`) because it does not know the host the phone reaches it
 * at; handed to the image loader as-is it is opened as a local FILE and reads "Photo unavailable".
 * Resolved against the app's own API base, exactly as the verify screen does.
 */
internal fun animalPurchaseMediaUrl(url: String, apiBaseUrl: String = BuildConfig.API_BASE_URL): String {
    val trimmed = url.trim()
    if (trimmed.isEmpty() || trimmed.startsWith("http://") || trimmed.startsWith("https://")) return trimmed
    if (!trimmed.startsWith("/")) return trimmed
    return apiBaseUrl.trimEnd('/') + trimmed
}

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
    fieldVerdictLabel = fieldVerdictLabel,
    fieldVerdictTone = animalPurchaseFieldVerdictTone(fieldVerdict),
    decidedByName = decidedByName,
    decisionNote = decisionNote,
    previewUrl = animalPurchaseMediaUrl(preview?.mediaUrl.orEmpty()),
    previewIsPhoto = preview?.mediaMime.orEmpty().startsWith("image/", ignoreCase = true),
    previewIdentity = preview?.proofRef?.takeIf { it.isNotBlank() } ?: candidateId,
)

/**
 * The card's one preview: a questionnaire row shows the first capture of its first media slot;
 * a legacy row (questionnaire_version 0) keeps its single video. Null when nothing can be served.
 */
private val AnimalPurchaseAnimalDto.preview: AnimalPurchaseMediaItemDto?
    get() = if (questionnaireVersion > 0) {
        mediaSlots.asSequence().flatMap { it.items.asSequence() }.firstOrNull { it.mediaUrl.isNotBlank() }
    } else {
        mediaUrl.takeIf { it.isNotBlank() }?.let { AnimalPurchaseMediaItemDto(proofRef = videoProofRef, mediaUrl = it, mediaMime = mediaMime) }
    }

/** The inspector's own verdict: `selected` reads OK, `on_hold` reads as a warning, none is neutral. */
internal fun animalPurchaseFieldVerdictTone(fieldVerdict: String): VendorsTone = when (fieldVerdict.trim().lowercase()) {
    "selected" -> VendorsTone.OK
    "on_hold" -> VendorsTone.WARN
    else -> VendorsTone.NEUTRAL
}

private fun trimDecimal(value: Double): String =
    if (value == value.toLong().toDouble()) value.toLong().toString() else String.format(java.util.Locale.ROOT, "%.1f", value)

// ---------------------------------------------------------------------------------------------
// L2: add an animal (the served SOP questionnaire)
// ---------------------------------------------------------------------------------------------

/**
 * The add-animal form over the backend-served Procurement SOP questionnaire. The catalog, its
 * order, wording, conditions and limits are the SERVER's; this holder renders them, keeps the
 * answers alive across a process death, owns every capture as its own durable proof slot on the
 * draft's outbox lane, mirrors the server's validation so the first refusal lands on screen before
 * a round trip, and maps the server's own `422 field` back onto the question it named.
 */
@HiltViewModel
class AnimalPurchaseAnimalCreateViewModel @Inject constructor(
    savedStateHandle: SavedStateHandle,
    private val repository: AnimalPurchaseRepository,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val proofCaptureSource: ProofCaptureSource,
    private val photoCaptureSource: PhotoCaptureSource,
    private val syncRepository: SyncRepository,
    private val bootstrapRepository: BootstrapRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    @ApplicationContext private val appContext: Context,
) : ViewModel() {

    private val loadId: String = savedStateHandle.get<String>(ARG_LOAD_ID).orEmpty()

    /** STABLE across process death: this draft's capture slots AND its write key hang off it. */
    private val draftKey = DraftIdempotencyKey(savedStateHandle, KEY_ANIMAL_DRAFT, "ap-animal")
    private val queuedItemId = DraftOutboxItemId(savedStateHandle, KEY_ANIMAL_OUTBOX_ITEM)
    /** Answers outlive a process death, beside the durable captures and draft key, so a form that
     *  died mid-inspection comes back exactly as left rather than as photos with no facts. */
    private val savedAnswers = SavedAnswers(savedStateHandle, KEY_ANIMAL_ANSWERS)
    private val savedPage = SavedPageIndex(savedStateHandle, KEY_ANIMAL_PAGE)

    private data class Local(
        val answers: AnimalPurchaseAnswers = AnimalPurchaseAnswers(),
        /** Client-side or server-named refusals, keyed by question id. */
        val questionErrors: Map<String, String> = emptyMap(),
        val scrollToQuestionId: String = "",
        val scrollRequest: Int = 0,
        /** The CATALOG page on screen (an index into the served questionnaire's pages, stable
         *  across answers), persisted beside the answers so a process death reopens the same page. */
        val page: Int = 0,
        /** The media question whose capture is in the camera right now, blank otherwise. */
        val captureWorkingFor: String = "",
        val writeStatus: VendorsWriteStatus = VendorsWriteStatus.IDLE,
        val writeMessage: String = "",
        val submitInFlight: Boolean = false,
        val closeAfterSave: Boolean = false,
    )

    private val local = MutableStateFlow(Local(answers = savedAnswers.read(), page = savedPage.read()))

    /** The draft's proof identity: one per draft, the SLOT + INDEX ride the field key. */
    private val proofIdentity = ProofIdentity(flow = ProofFlow.ANIMAL_PURCHASE, taskId = loadId, subjectKey = draftKey.current())

    /**
     * Every capture of THIS draft, EVEN ONE whose upload died: a dead capture is shown with a
     * retry rather than hidden, because hiding it would make a photo taken under a bad signal
     * vanish from the form. Rows of other drafts on the same load are filtered out by field key.
     */
    private fun draftCaptures(): Flow<List<ProofCaptureRow>> =
        proofCaptureRepository.observeProofs(proofIdentity.taskId, proofIdentity.partitionKey.takeUnless { it == "whole" })
            .map { rows -> rows.filter { animalPurchaseCaptureSlot(draftKey.current(), it.fieldKey) != null }.sortedBy { it.capturedAtMs } }

    /** Each capture beside its upload row's OWN terminal state (the proof mirror reads FAILED while
     *  merely retrying offline; only the row knows whether it has given up). */
    private val capturesWithUploadState: Flow<List<Pair<ProofCaptureRow, Boolean>>> =
        draftCaptures().flatMapLatest { rows ->
            if (rows.isEmpty()) flowOf(emptyList())
            else combine(rows.map { row ->
                val id = row.outboxItemId
                if (id.isNullOrBlank()) flowOf(row to true)
                else syncRepository.observeItem(id).map { item -> row to (item?.isTerminalFailure == true) }
            }) { it.toList() }
        }

    init {
        analytics.track(AnalyticsEventsAnimalPurchase.ANIMAL_ADD_OPENED, mapOf(AnalyticsEventsAnimalPurchase.Params.LOAD_ID to loadId))
        viewModelScope.launch { repository.refreshOptions() }
        queuedItemId.value?.let { followWrite(it) }
    }

    val state: StateFlow<AnimalPurchaseAnimalCreateUiState> = combine(
        local,
        repository.observeOptions(),
        capturesWithUploadState,
    ) { l, options, captures ->
        val o = options ?: AnimalPurchaseOptionsDto()
        val pages = animalPurchasePages(o.questionnaire)
        val visible = visiblePages(pages, l.answers)
        val current = resolvePage(visible, l.page)
        val page = current?.let { pages[it] }
        AnimalPurchaseAnimalCreateUiState(
            copy = o.copy,
            questions = page?.let { applicableQuestions(it.questions, l.answers) }.orEmpty().map { q -> q.toUi(l, captures) },
            stepCount = visible.size,
            stepIndex = visible.indexOf(current).coerceAtLeast(0),
            pageTitle = page?.section?.title ?: o.copy[COPY_ANIMAL_FORM_TITLE].orEmpty(),
            pageHint = page?.section?.hint ?: o.copy[COPY_ANIMAL_FORM_HINT].orEmpty(),
            isFirstPage = current == null || visible.firstOrNull() == current,
            isLastPage = current == null || visible.lastOrNull() == current,
            scalarAnswers = l.answers.scalar,
            multiAnswers = l.answers.multi,
            scrollToQuestionId = l.scrollToQuestionId,
            scrollRequest = l.scrollRequest,
            writeStatus = l.writeStatus,
            writeMessage = l.writeMessage,
            submitInFlight = l.submitInFlight,
            closeAfterSave = l.closeAfterSave,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), AnimalPurchaseAnimalCreateUiState())

    private fun AnimalPurchaseQuestionDto.toUi(l: Local, captures: List<Pair<ProofCaptureRow, Boolean>>): AnimalPurchaseQuestionUi {
        val kindUi = animalPurchaseQuestionKind(kind)
        val slotCaptures = if (kindUi == AnimalPurchaseQuestionKind.MEDIA) {
            captures.filter { (row, _) -> animalPurchaseCaptureSlot(draftKey.current(), row.fieldKey)?.slot == slot }
                .map { (row, gaveUp) ->
                    AnimalPurchaseCaptureUi(
                        proofId = row.id,
                        localUri = row.localUri,
                        isVideo = !row.mimeType.startsWith("image/", ignoreCase = true),
                        uploadFailed = gaveUp,
                    )
                }
        } else {
            emptyList()
        }
        return AnimalPurchaseQuestionUi(
            id = id,
            kind = kindUi,
            title = title,
            hint = hint,
            required = required,
            options = options.map { VendorsOptionUi(it.value, it.label) },
            allowOther = allowOther,
            slot = slot,
            maxFiles = maxFiles,
            acceptsPhoto = ACCEPTS_PHOTO in accepts,
            acceptsVideo = ACCEPTS_VIDEO in accepts,
            captures = slotCaptures,
            captureWorking = l.captureWorkingFor == id,
            unit = unit,
            rangeLine = animalPurchaseRangeLine(min, max, unit),
            error = l.questionErrors[id],
        )
    }

    fun onEvent(event: AnimalPurchaseAnimalCreateEvent) {
        val locked = local.value.writeStatus == VendorsWriteStatus.QUEUED || local.value.writeStatus == VendorsWriteStatus.SYNCED
        when (event) {
            is AnimalPurchaseAnimalCreateEvent.ChoiceChanged -> if (!locked) answer(event.questionId) { it.withScalar(event.questionId, event.value) }
            is AnimalPurchaseAnimalCreateEvent.MultiToggled -> if (!locked) answer(event.questionId) { it.withMultiToggled(event.questionId, event.value, event.checked) }
            is AnimalPurchaseAnimalCreateEvent.TextChanged -> if (!locked) answer(event.questionId) { it.withScalar(event.questionId, event.value) }
            is AnimalPurchaseAnimalCreateEvent.TakePhoto -> if (!locked) capture(event.questionId, video = false)
            is AnimalPurchaseAnimalCreateEvent.RecordVideo -> if (!locked) capture(event.questionId, video = true)
            is AnimalPurchaseAnimalCreateEvent.RemoveCapture -> if (!locked) removeCapture(event.questionId, event.proofId)
            is AnimalPurchaseAnimalCreateEvent.RetryCapture -> if (!locked) retryCapture(event.proofId)
            is AnimalPurchaseAnimalCreateEvent.PreviewAction -> trackPreviewAction(event.action)
            AnimalPurchaseAnimalCreateEvent.NextPage -> if (!locked) nextPage()
            AnimalPurchaseAnimalCreateEvent.PreviousPage -> if (!locked) previousPage()
            AnimalPurchaseAnimalCreateEvent.Submit -> if (!locked) submit()
            AnimalPurchaseAnimalCreateEvent.DismissMessage -> local.update { it.copy(writeMessage = "", writeStatus = if (it.writeStatus == VendorsWriteStatus.FAILED) VendorsWriteStatus.IDLE else it.writeStatus) }
            AnimalPurchaseAnimalCreateEvent.Back -> Unit
        }
    }

    /**
     * Applies one answer, drops the answers of every question that stopped applying because of it
     * (a `pregnant` answer has no meaning once the sex changed to male), clears the question's own
     * error, and persists the whole map for a process death. The "other" text's key maps back to
     * its question for the error clear.
     */
    private fun answer(questionId: String, change: (AnimalPurchaseAnswers) -> AnimalPurchaseAnswers) {
        val ownerId = questionId.removeSuffix(OTHER_SUFFIX)
        local.update { l ->
            val next = pruneInapplicable(latestQuestionnaire, change(l.answers))
            l.copy(answers = next, questionErrors = l.questionErrors - ownerId - questionId)
        }
        savedAnswers.write(local.value.answers)
    }

    /** The last served catalog, mirrored off the options flow so event handlers read it synchronously. */
    private var latestQuestionnaire: List<AnimalPurchaseQuestionDto> = emptyList()

    init {
        viewModelScope.launch {
            repository.observeOptions().collect { latestQuestionnaire = it?.questionnaire.orEmpty() }
        }
    }

    /**
     * Validates ONLY the current page's questions the way the server would, then advances to the
     * next page that still has an applicable question. A refusal highlights the first failing
     * question on this page and stays. The page index is persisted beside the answers.
     */
    private fun nextPage() {
        val catalog = latestQuestionnaire
        val pages = animalPurchasePages(catalog)
        viewModelScope.launch {
            val l = local.value
            val visible = visiblePages(pages, l.answers)
            val current = resolvePage(visible, l.page) ?: return@launch
            val failure = pageFailure(pages[current].questions)
            if (failure != null) {
                local.update { it.copy(questionErrors = mapOf(failure.questionId to failure.message), scrollToQuestionId = failure.questionId, scrollRequest = it.scrollRequest + 1) }
                return@launch
            }
            val next = visible.firstOrNull { it > current } ?: return@launch
            moveToPage(next)
        }
    }

    private fun previousPage() {
        val pages = animalPurchasePages(latestQuestionnaire)
        val l = local.value
        val visible = visiblePages(pages, l.answers)
        val current = resolvePage(visible, l.page) ?: return
        val previous = visible.lastOrNull { it < current } ?: return
        moveToPage(previous)
    }

    private fun moveToPage(page: Int) {
        local.update { it.copy(page = page, questionErrors = emptyMap(), scrollToQuestionId = "", scrollRequest = it.scrollRequest + 1) }
        savedPage.write(page)
    }

    /** The first refusal among [questions] (a page or the whole catalog), reading captures durably. */
    private suspend fun pageFailure(questions: List<AnimalPurchaseQuestionDto>): AnimalPurchaseQuestionFailure? {
        val answers = local.value.answers
        val captures = draftCaptures().first()
        val slotOutboxIds = captureOutboxIdsBySlot(captures)
        val deadOutboxIds = captures.mapNotNull { it.outboxItemId }.filter { it.isNotBlank() && uploadGaveUp(it) }.toSet()
        return animalPurchaseFirstFailure(
            catalog = questions,
            answers = answers,
            mediaOutboxIds = slotOutboxIds,
            deadOutboxIds = deadOutboxIds,
            copy = state.value.copy,
        )
    }

    /** Every capture's upload row id, keyed by slot in capture-index order. */
    private fun captureOutboxIdsBySlot(captures: List<ProofCaptureRow>): Map<String, List<String>> = captures
        .mapNotNull { row -> animalPurchaseCaptureSlot(draftKey.current(), row.fieldKey)?.let { it to row } }
        .sortedBy { (slotIndex, _) -> slotIndex.index }
        .groupBy({ (slotIndex, _) -> slotIndex.slot }, { (_, row) -> row.outboxItemId.orEmpty() })

    /**
     * Captures one photo or one video for a media question with the in-app camera, into a NEW
     * durable proof slot (draft, slot, next index). Everything after a REAL capture is durable
     * bookkeeping and runs NonCancellable, so backing out of the screen can never orphan a photo
     * the person actually took (the PC Care defect of 2026-08-21).
     */
    private fun capture(questionId: String, video: Boolean) {
        if (loadId.isBlank() || local.value.captureWorkingFor.isNotBlank()) return
        val question = latestQuestionnaire.firstOrNull { it.id == questionId } ?: return
        val slot = question.slot.takeIf { it.isNotBlank() } ?: return
        viewModelScope.launch {
            local.update { it.copy(captureWorkingFor = questionId, questionErrors = it.questionErrors - questionId) }
            try {
                val existing = draftCaptures().first().mapNotNull { animalPurchaseCaptureSlot(draftKey.current(), it.fieldKey) }.filter { it.slot == slot }
                if (question.maxFiles > 0 && existing.size >= question.maxFiles) return@launch
                val nextIndex = (existing.maxOfOrNull { it.index } ?: -1) + 1
                val headerTitle = repository.observeLoad(loadId).first()?.title.orEmpty()
                val captured: CapturedMedia = try {
                    if (video) {
                        proofCaptureSource.captureVideo(
                            ProofCaptureContext(
                                title = question.title,
                                primaryTag = headerTitle,
                                workLabel = question.hint,
                                headerTitle = appContext.getString(R.string.proof_video_animal_purchase_header),
                            ),
                        )?.let { CapturedMedia(it.localUri, it.mimeType, it.startedAtMs, it.endedAtMs, it.captureSource) }
                    } else {
                        photoCaptureSource.capturePhoto(PhotoCaptureContext(title = question.title, instruction = question.hint))
                            ?.let { CapturedMedia(it.localUri, it.mimeType, it.capturedAtMs, it.capturedAtMs, it.captureSource) }
                    }
                } catch (error: Exception) {
                    if (error is CancellationException) throw error
                    crashReporter.recordException(error, "animal purchase capture failed")
                    trackFailure("capture_exception", error.message ?: "unknown")
                    null
                } ?: return@launch

                withContext(NonCancellable) {
                    // The proof platform requires a scope: the TENANT, as the maintainer's contract
                    // for this write names it; the load is the subject and there is no animal yet.
                    val tenantId = bootstrapRepository.actorTenantId().orEmpty()
                    if (tenantId.isBlank()) {
                        trackFailure("capture_failed", "tenant_unavailable")
                        local.update { it.copy(writeStatus = VendorsWriteStatus.FAILED, writeMessage = MESSAGE_MEDIA_NOT_SAVED) }
                        return@withContext
                    }
                    val fieldKey = animalPurchaseCaptureFieldKey(draftKey.current(), slot, nextIndex)
                    val result = proofCaptureRepository.capture(
                        taskId = proofIdentity.taskId,
                        fieldKey = fieldKey,
                        subject = ProofSubject.OTHER,
                        subjectId = loadId,
                        localUri = captured.localUri,
                        mimeType = captured.mimeType,
                        caption = question.title,
                        scopeType = SCOPE_TYPE_TENANT,
                        scopeId = tenantId,
                        capturedStartMs = captured.startMs,
                        capturedEndMs = captured.endMs,
                        capturedByPrincipalId = null,
                        proofPolicy = animalPurchaseProofPolicy(captured.captureSource),
                        awaitUploadEnqueue = true,
                        // ONE FIFO lane per DRAFT: every upload drains BEFORE the animal create
                        // that resolves them (AnimalPurchasePayloads.kt).
                        uploadGroupKey = animalPurchaseDraftGroupKey(draftKey.current()),
                    )
                    when (result) {
                        is AppResult.Err -> {
                            result.cause?.let { crashReporter.recordException(it, "animal purchase capture write failed") }
                            trackFailure("capture_failed", result.message)
                            local.update { it.copy(writeStatus = VendorsWriteStatus.FAILED, writeMessage = MESSAGE_MEDIA_NOT_SAVED) }
                        }
                        is AppResult.Ok -> {
                            val outboxId = awaitUploadRow(result.value)
                            if (outboxId == null) {
                                crashReporter.recordException(
                                    IllegalStateException("animal purchase capture ${result.value.id} has no upload row"),
                                    "animal purchase capture never enqueued its upload",
                                )
                                trackFailure("capture_saved_without_upload_row", "missing_proof_outbox_id")
                                local.update { it.copy(writeStatus = VendorsWriteStatus.FAILED, writeMessage = MESSAGE_MEDIA_NOT_SAVED) }
                            } else {
                                analytics.track(
                                    AnalyticsEventsAnimalPurchase.ANIMAL_VIDEO_CAPTURED,
                                    mapOf(
                                        AnalyticsEventsAnimalPurchase.Params.LOAD_ID to loadId,
                                        AnalyticsEvents.Params.SOURCE to captured.captureSource,
                                        AnalyticsEvents.Params.KIND to if (video) ACCEPTS_VIDEO else ACCEPTS_PHOTO,
                                        AnalyticsEvents.Params.FIELD to slot,
                                    ),
                                )
                                local.update { it.copy(writeStatus = VendorsWriteStatus.IDLE, writeMessage = "") }
                            }
                        }
                    }
                }
            } finally {
                local.update { it.copy(captureWorkingFor = "") }
            }
        }
    }

    /** A capture as either camera port hands it back. */
    private data class CapturedMedia(val localUri: String, val mimeType: String, val startMs: Long, val endMs: Long, val captureSource: String)

    /**
     * The upload-row id can land in Room a beat AFTER the capture result is composed (PC Care's
     * settle wait), so a blank id here is usually a read race, not a lost capture.
     */
    private suspend fun awaitUploadRow(row: ProofCaptureRow): String? {
        var proofOutboxId = row.outboxItemId
        var waited = 0L
        while (proofOutboxId.isNullOrBlank() && waited < PROOF_ROW_SETTLE_MAX_MS) {
            delay(PROOF_ROW_SETTLE_STEP_MS)
            waited += PROOF_ROW_SETTLE_STEP_MS
            proofOutboxId = draftCaptures().first().firstOrNull { it.id == row.id }?.outboxItemId
        }
        return proofOutboxId?.takeIf { it.isNotBlank() }
    }

    /** Drops one capture (and cancels its pending upload); a refusal while it is mid-upload shows. */
    private fun removeCapture(questionId: String, proofId: String) {
        viewModelScope.launch {
            when (val result = proofCaptureRepository.remove(proofIdentity.taskId, proofId)) {
                is AppResult.Ok -> local.update { it.copy(questionErrors = it.questionErrors - questionId) }
                is AppResult.Err -> {
                    trackFailure("capture_remove_failed", result.message)
                    local.update { it.copy(writeStatus = VendorsWriteStatus.FAILED, writeMessage = result.message) }
                }
            }
        }
    }

    /** Re-arms one dead upload row; the capture is still on this phone. */
    private fun retryCapture(proofId: String) {
        viewModelScope.launch {
            val id = draftCaptures().first().firstOrNull { it.id == proofId }?.outboxItemId ?: return@launch
            analytics.track(AnalyticsEventsAnimalPurchase.ANIMAL_RETRY, mapOf(AnalyticsEventsAnimalPurchase.Params.LOAD_ID to loadId))
            syncRepository.retry(id)
        }
    }

    /** True only when the upload row is terminally dead (conflict, or every retry spent). */
    private suspend fun uploadGaveUp(proofOutboxItemId: String): Boolean =
        syncRepository.observeItem(proofOutboxItemId).first()?.isTerminalFailure == true

    /**
     * Validates against the served questionnaire exactly as the server does — in order, first
     * problem wins — then queues the create with every capture referenced by its upload row.
     * Captures are read from their DURABLE slots at submit time, so a photo taken before a
     * ViewModel death still sends instead of the animal looking unfilmed. A capture whose upload
     * is merely RETRYING offline is usable; only one whose upload GAVE UP blocks, on its question.
     */
    private fun submit() {
        val current = state.value
        val catalog = latestQuestionnaire
        if (catalog.isEmpty()) {
            local.update { it.copy(writeStatus = VendorsWriteStatus.FAILED, writeMessage = MESSAGE_NOT_SAVED) }
            return
        }
        viewModelScope.launch {
            val answers = local.value.answers
            val slotOutboxIds = captureOutboxIdsBySlot(draftCaptures().first())
            // The WHOLE questionnaire, not only the last page: an earlier page's answer may have
            // changed since it was checked, and the server checks everything.
            val failure = pageFailure(catalog)
            if (failure != null) {
                trackFailure("animal_not_queued", failure.questionId)
                val pageOfFailure = animalPurchasePages(catalog).indexOfFirst { page -> page.questions.any { it.id == failure.questionId } }
                local.update {
                    it.copy(
                        page = if (pageOfFailure >= 0) pageOfFailure else it.page,
                        questionErrors = mapOf(failure.questionId to failure.message),
                        scrollToQuestionId = failure.questionId,
                        scrollRequest = it.scrollRequest + 1,
                    )
                }
                if (pageOfFailure >= 0) savedPage.write(pageOfFailure)
                return@launch
            }
            val applicable = applicableQuestions(catalog, answers)
            val mediaBySlot = applicable
                .filter { it.kind == QUESTION_KIND_MEDIA && it.slot.isNotBlank() }
                .mapNotNull { q -> slotOutboxIds[q.slot]?.takeIf { ids -> ids.isNotEmpty() }?.let { q.slot to it } }
                .toMap()
            val request = AnimalPurchaseAnimalCreateRequestDto(answers = buildAnswersJson(applicable, answers))
            local.update { it.copy(submitInFlight = true, questionErrors = emptyMap()) }
            when (val result = syncRepository.enqueueAnimalPurchaseAnimalCreate(draftKey.current(), loadId, request, mediaBySlot)) {
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

    /**
     * Follows the queued row: return to the load once saved (or durably queued offline); show the
     * server's own sentence when refused — a 4xx is terminal and never retried as-is — and, when
     * the server named the question it refused, put that sentence on the question and scroll to it.
     */
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
                        val message = outcome.reason?.takeIf { r -> r.isNotBlank() } ?: MESSAGE_NOT_SAVED
                        val field = outcome.field?.takeIf { f -> latestQuestionnaire.any { q -> q.id == f } }
                        val pageOfField = field?.let { f -> animalPurchasePages(latestQuestionnaire).indexOfFirst { page -> page.questions.any { it.id == f } } }?.takeIf { it >= 0 }
                        local.update {
                            it.copy(
                                writeStatus = VendorsWriteStatus.FAILED,
                                writeMessage = message,
                                closeAfterSave = false,
                                page = pageOfField ?: it.page,
                                questionErrors = if (field != null) mapOf(field to message) else it.questionErrors,
                                scrollToQuestionId = field ?: it.scrollToQuestionId,
                                scrollRequest = if (field != null) it.scrollRequest + 1 else it.scrollRequest,
                            )
                        }
                        pageOfField?.let { savedPage.write(it) }
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
        const val KEY_ANIMAL_ANSWERS = "animalPurchase.animal.answers"
        const val KEY_ANIMAL_PAGE = "animalPurchase.animal.page"
        const val SCOPE_TYPE_TENANT = "tenant"
        const val COPY_ANIMAL_SAVING_KEY = "animal.saving"
        const val PROOF_ROW_SETTLE_MAX_MS = 3_000L
        const val PROOF_ROW_SETTLE_STEP_MS = 100L
        const val MESSAGE_MEDIA_NOT_SAVED = "That photo or video didn't save. Take it again."
    }
}

/** The capture policy of an animal-purchase capture: one in-app-camera file per (draft, slot, index). */
internal fun animalPurchaseProofPolicy(captureSource: String): ProofPolicy =
    ProofPolicy.Default.copy(
        proofMode = "animal_purchase_media",
        featureSurface = "animal_purchase",
        featureCategory = "procurement",
        subjectScope = ProofSubject.OTHER.wireValue,
        expectedSubjects = listOf(ProofSubject.OTHER.wireValue),
        captureSource = captureSource,
        maximumCountPerField = 1,
        // One load may hold many animals, each up to the questionnaire's slot caps, plus the
        // transient rows a removed-and-retaken capture makes.
        maximumCountPerSubject = 2_000,
    )

// ---------------------------------------------------------------------------------------------
// The questionnaire on the phone: answers, applicability, validation, the wire JSON
// ---------------------------------------------------------------------------------------------

private const val QUESTION_KIND_CHOICE = "choice"
private const val QUESTION_KIND_MULTI = "multi"
private const val QUESTION_KIND_TEXT = "text"
private const val QUESTION_KIND_NUMBER = "number"
private const val QUESTION_KIND_MEDIA = "media"
private const val QUESTION_KIND_SECTION = "section"
private const val ACCEPTS_PHOTO = "photo"
private const val ACCEPTS_VIDEO = "video"

internal fun animalPurchaseQuestionKind(wire: String): AnimalPurchaseQuestionKind = when (wire.trim().lowercase()) {
    QUESTION_KIND_CHOICE -> AnimalPurchaseQuestionKind.CHOICE
    QUESTION_KIND_MULTI -> AnimalPurchaseQuestionKind.MULTI
    QUESTION_KIND_NUMBER -> AnimalPurchaseQuestionKind.NUMBER
    QUESTION_KIND_MEDIA -> AnimalPurchaseQuestionKind.MEDIA
    QUESTION_KIND_SECTION -> AnimalPurchaseQuestionKind.SECTION
    else -> AnimalPurchaseQuestionKind.TEXT
}

/** Mirrors the server's `Answers.Applies`: shown unless an `only_if` names another question's value. */
internal fun AnimalPurchaseQuestionDto.appliesGiven(answers: AnimalPurchaseAnswers): Boolean {
    val condition = onlyIf ?: return true
    return answers.choice(condition.questionId) == condition.value.trim().lowercase()
}

/** The served questions that apply given [answers], in served order. */
internal fun applicableQuestions(catalog: List<AnimalPurchaseQuestionDto>, answers: AnimalPurchaseAnswers): List<AnimalPurchaseQuestionDto> =
    catalog.filter { it.appliesGiven(answers) }

/**
 * Drops the answers of questions that no longer apply, repeating until stable so a chain
 * (sex -> lactating -> mastitis) clears through in one call.
 */
internal fun pruneInapplicable(catalog: List<AnimalPurchaseQuestionDto>, answers: AnimalPurchaseAnswers): AnimalPurchaseAnswers {
    var current = answers
    repeat(catalog.size.coerceAtLeast(1)) {
        val next = catalog.filter { !it.appliesGiven(current) }.fold(current) { acc, q -> acc.without(q.id) }
        if (next == current) return next
        current = next
    }
    return current
}

/** "0.5–300 kg": the accepted range of a number question, numbers not copy; blank when unbounded. */
internal fun animalPurchaseRangeLine(min: Double?, max: Double?, unit: String): String {
    if (min == null && max == null) return ""
    val range = listOfNotNull(min?.let { trimNumber(it) }, max?.let { trimNumber(it) }).joinToString("–")
    return listOf(range, unit).filter { it.isNotBlank() }.joinToString(" ")
}

private fun trimNumber(value: Double): String =
    if (value == value.toLong().toDouble()) value.toLong().toString() else value.toString()

/** The first question the phone-side check refuses, with the message to show on it. */
internal data class AnimalPurchaseQuestionFailure(val questionId: String, val message: String)

/**
 * Mirrors the server's `ValidateAnswers` in order: applicable questions only; a required
 * question answered; a choice within its options and, for "other", carrying its text; a number
 * within range; a required media slot filled and no slot over its cap; every referenced capture
 * upload alive. The message is the backend's copy (`required.hint`, `animal.other.hint`,
 * `animal.send_failed`) or the numeric range line — never a farm sentence composed here.
 */
internal fun animalPurchaseFirstFailure(
    catalog: List<AnimalPurchaseQuestionDto>,
    answers: AnimalPurchaseAnswers,
    mediaOutboxIds: Map<String, List<String>>,
    deadOutboxIds: Set<String>,
    copy: Map<String, String>,
): AnimalPurchaseQuestionFailure? {
    val required = copy[COPY_REQUIRED_HINT].orEmpty()
    val otherHint = copy[COPY_ANIMAL_OTHER_HINT].orEmpty()
    val sendFailed = copy[COPY_ANIMAL_SEND_FAILED].orEmpty()
    for (q in catalog) {
        if (q.kind == QUESTION_KIND_SECTION || !q.appliesGiven(answers)) continue
        when (q.kind) {
            QUESTION_KIND_MEDIA -> {
                val ids = mediaOutboxIds[q.slot].orEmpty()
                if (q.required && ids.isEmpty()) return AnimalPurchaseQuestionFailure(q.id, required)
                if (q.maxFiles > 0 && ids.size > q.maxFiles) return AnimalPurchaseQuestionFailure(q.id, required)
                if (ids.any { it.isBlank() || it in deadOutboxIds }) return AnimalPurchaseQuestionFailure(q.id, sendFailed)
            }
            QUESTION_KIND_CHOICE -> {
                val v = answers.choice(q.id)
                if (v.isEmpty()) {
                    if (q.required) return AnimalPurchaseQuestionFailure(q.id, required)
                    continue
                }
                if (q.options.none { it.value == v }) return AnimalPurchaseQuestionFailure(q.id, required)
                if (v == OTHER_OPTION_VALUE && q.allowOther && answers.scalar[q.id + OTHER_SUFFIX].orEmpty().isBlank()) {
                    return AnimalPurchaseQuestionFailure(q.id, otherHint)
                }
            }
            QUESTION_KIND_MULTI -> {
                val vals = answers.multi[q.id].orEmpty()
                if (vals.isEmpty()) {
                    if (q.required) return AnimalPurchaseQuestionFailure(q.id, required)
                    continue
                }
                if (vals.any { v -> q.options.none { it.value == v } }) return AnimalPurchaseQuestionFailure(q.id, required)
            }
            QUESTION_KIND_NUMBER -> {
                val raw = answers.scalar[q.id].orEmpty().trim()
                if (raw.isEmpty()) {
                    if (q.required) return AnimalPurchaseQuestionFailure(q.id, required)
                    continue
                }
                val n = raw.toDoubleOrNull()
                val range = animalPurchaseRangeLine(q.min, q.max, q.unit).ifBlank { required }
                if (n == null || n.isNaN() || n.isInfinite()) return AnimalPurchaseQuestionFailure(q.id, range)
                val min = q.min
                val max = q.max
                if ((min != null && n < min) || (max != null && n > max)) return AnimalPurchaseQuestionFailure(q.id, range)
            }
            else -> {
                val t = answers.scalar[q.id].orEmpty()
                if (q.required && t.isBlank()) return AnimalPurchaseQuestionFailure(q.id, required)
            }
        }
    }
    return null
}

/**
 * The wire `answers` object: only APPLICABLE, ANSWERED questions — a choice as its value (plus
 * `<id>_other` text), a multi as an array, a number as a JSON number, text as a string.
 */
internal fun buildAnswersJson(applicable: List<AnimalPurchaseQuestionDto>, answers: AnimalPurchaseAnswers): JsonObject = buildJsonObject {
    for (q in applicable) {
        when (q.kind) {
            QUESTION_KIND_CHOICE -> {
                val v = answers.choice(q.id)
                if (v.isEmpty()) continue
                put(q.id, JsonPrimitive(v))
                if (v == OTHER_OPTION_VALUE && q.allowOther) {
                    answers.scalar[q.id + OTHER_SUFFIX]?.trim()?.takeIf { it.isNotEmpty() }?.let { put(q.id + OTHER_SUFFIX, JsonPrimitive(it)) }
                }
            }
            QUESTION_KIND_MULTI -> {
                val vals = answers.multi[q.id].orEmpty().map { it.trim().lowercase() }.filter { it.isNotEmpty() }
                if (vals.isEmpty()) continue
                put(q.id, JsonArray(vals.map { JsonPrimitive(it) }))
                if (OTHER_OPTION_VALUE in vals && q.allowOther) {
                    answers.scalar[q.id + OTHER_SUFFIX]?.trim()?.takeIf { it.isNotEmpty() }?.let { put(q.id + OTHER_SUFFIX, JsonPrimitive(it)) }
                }
            }
            QUESTION_KIND_NUMBER -> {
                answers.scalar[q.id]?.trim()?.toDoubleOrNull()?.let { put(q.id, JsonPrimitive(it)) }
            }
            QUESTION_KIND_TEXT -> {
                answers.scalar[q.id]?.trim()?.takeIf { it.isNotEmpty() }?.let { put(q.id, JsonPrimitive(it)) }
            }
        }
    }
}

/** A capture's place in the questionnaire, parsed back off its proof field key. */
internal data class AnimalPurchaseCaptureSlot(val slot: String, val index: Int)

private const val CAPTURE_KEY_SEPARATOR = '|'

/** Field key of one capture: `<draft>|<slot>|<index>`, unique per file so each is its own proof slot. */
internal fun animalPurchaseCaptureFieldKey(draftKey: String, slot: String, index: Int): String =
    "$draftKey$CAPTURE_KEY_SEPARATOR$slot$CAPTURE_KEY_SEPARATOR$index"

/** Parses a field key back; null when it is not one of THIS draft's captures. */
internal fun animalPurchaseCaptureSlot(draftKey: String, fieldKey: String): AnimalPurchaseCaptureSlot? {
    val prefix = "$draftKey$CAPTURE_KEY_SEPARATOR"
    if (!fieldKey.startsWith(prefix)) return null
    val rest = fieldKey.removePrefix(prefix)
    val sep = rest.lastIndexOf(CAPTURE_KEY_SEPARATOR)
    if (sep <= 0) return null
    val index = rest.substring(sep + 1).toIntOrNull() ?: return null
    return AnimalPurchaseCaptureSlot(slot = rest.substring(0, sep), index = index)
}

/**
 * Persists the answers in the SavedStateHandle as one JSON string, so a ViewModel recreated after
 * a process death rebuilds the same map. A corrupt or stale-shape value restores as empty rather
 * than crashing the restore.
 */
internal class SavedAnswers(private val savedStateHandle: SavedStateHandle, private val stateKey: String) {
    fun read(): AnimalPurchaseAnswers {
        val raw = savedStateHandle.get<String>(stateKey) ?: return AnimalPurchaseAnswers()
        // exception:exempt a stale-shape saved draft restores as a blank form; nothing to report
        return runCatching { syncJson.decodeFromString<AnimalPurchaseAnswers>(raw) }.getOrDefault(AnimalPurchaseAnswers())
    }

    fun write(answers: AnimalPurchaseAnswers) {
        savedStateHandle[stateKey] = syncJson.encodeToString(answers)
    }
}

/** The page on screen, persisted so a process death reopens the same page of the inspection. */
internal class SavedPageIndex(private val savedStateHandle: SavedStateHandle, private val stateKey: String) {
    fun read(): Int = savedStateHandle.get<Int>(stateKey) ?: 0
    fun write(page: Int) {
        savedStateHandle[stateKey] = page
    }
}

/**
 * One page of the questionnaire: the SOP form's own grouping. [section] is null on the first page
 * (the identity/condition questions before any section heading) and the heading item after;
 * [questions] are the items on it, the heading excluded.
 */
internal data class AnimalPurchasePage(val section: AnimalPurchaseQuestionDto?, val questions: List<AnimalPurchaseQuestionDto>)

/** Splits the served questionnaire by its `section` items, in order: each page runs from its
 *  heading (or the top, for the first) to the next heading. */
internal fun animalPurchasePages(catalog: List<AnimalPurchaseQuestionDto>): List<AnimalPurchasePage> {
    val headings = catalog.indices.filter { catalog[it].kind.trim().lowercase() == QUESTION_KIND_SECTION }
    // A leading run of questions before any heading is the SOP's first, unheaded page (-1: no heading item).
    val starts = if (headings.firstOrNull() == 0) headings else listOf(-1) + headings
    return starts.mapIndexed { i, start ->
        val end = starts.getOrNull(i + 1) ?: catalog.size
        AnimalPurchasePage(section = catalog.getOrNull(start), questions = catalog.subList(start + 1, end))
    }.filter { it.section != null || it.questions.isNotEmpty() }
}

/** Indices of the pages with at least one applicable question; a male-only/female-only page skips. */
internal fun visiblePages(pages: List<AnimalPurchasePage>, answers: AnimalPurchaseAnswers): List<Int> =
    pages.indices.filter { i -> pages[i].questions.any { it.appliesGiven(answers) } }

/** The page to show for a stored index: itself when visible, else the nearest visible one before
 *  it (then after); null when nothing is visible yet. */
internal fun resolvePage(visible: List<Int>, stored: Int): Int? =
    if (stored in visible) stored else visible.lastOrNull { it < stored } ?: visible.firstOrNull()

private val syncJson = kotlinx.serialization.json.Json { ignoreUnknownKeys = true }

private const val MAX_REASON_CHARS = 120
private const val AGE_UNIT = "months"
private const val WEIGHT_UNIT = "kg"
private const val MESSAGE_SAVING = "Saving..."
private const val MESSAGE_SAVED = "Saved."
private const val MESSAGE_QUEUED = "Saved on this phone. It will reach the server when the phone is online."
private const val MESSAGE_NOT_SAVED = "That didn't save. Try again."

/** The answered facts of a not-yet-sent animal, labelled from the backend vocabulary the form used. */
private fun QueuedAnimalPurchaseAnimal.toQueuedUi(options: AnimalPurchaseOptionsDto?, proofFailed: Boolean): AnimalPurchaseQueuedAnimalUi {
    val answers = request.answers
    fun text(id: String): String = (answers[id] as? JsonPrimitive)?.contentOrNull.orEmpty()
    fun label(list: List<AnimalPurchaseOptionDto>?, value: String) = list?.firstOrNull { it.value == value }?.label ?: value
    val sex = label(options?.sexes, text(QUESTION_ID_SEX))
    val species = label(options?.species, text(QUESTION_ID_SPECIES)).lowercase()
    val weight = text(QUESTION_ID_WEIGHT).toDoubleOrNull()
    return AnimalPurchaseQueuedAnimalUi(
        listKey = outboxItemId,
        title = "$sex $species".trim().replaceFirstChar { it.uppercase() },
        breed = text(QUESTION_ID_BREED),
        ageWeightLine = weight?.let { w -> "${trimDecimal(w)} $WEIGHT_UNIT" }.orEmpty(),
        conditionLabel = "",
        tempTag = text(QUESTION_ID_GOAT_ID),
        waitingLabel = options?.copy?.get(COPY_ANIMAL_QUEUED) ?: MESSAGE_WAITING_TO_SEND,
        sendFailed = proofFailed,
        failedLabel = options?.copy?.get(COPY_ANIMAL_SEND_FAILED) ?: MESSAGE_SEND_FAILED,
    )
}

/** The questionnaire ids the queued card reads its title line from (the SOP's own identity items). */
private const val QUESTION_ID_SPECIES = "species"
private const val QUESTION_ID_SEX = "sex"
private const val QUESTION_ID_BREED = "breed"
private const val QUESTION_ID_WEIGHT = "weight_kg"
private const val QUESTION_ID_GOAT_ID = "goat_id"

private const val COPY_ANIMAL_QUEUED = "animal.queued"
private const val MESSAGE_WAITING_TO_SEND = "Waiting to send"
private const val MESSAGE_SEND_FAILED = "Could not send · tap to retry"

/**
 * Persists a form's typed values in the SavedStateHandle as a flat "FIELD=value" list, so a
 * ViewModel recreated after process death rebuilds the same map. Enum keys go through [parse]
 * so a renamed field drops silently instead of crashing the restore.
 */
internal class SavedFormValues<F : Enum<F>>(
    private val savedStateHandle: SavedStateHandle,
    private val stateKey: String,
    private val parse: (String) -> F,
) {
    fun read(): Map<F, String> {
        val raw = savedStateHandle.get<ArrayList<String>>(stateKey) ?: return emptyMap()
        return raw.mapNotNull { entry ->
            val idx = entry.indexOf('=')
            if (idx <= 0) null else runCatching { parse(entry.substring(0, idx)) to entry.substring(idx + 1) }.getOrNull()
        }.toMap()
    }

    fun write(values: Map<F, String>) {
        savedStateHandle[stateKey] = ArrayList(values.map { (k, v) -> "${k.name}=$v" })
    }

    fun clear() {
        savedStateHandle.remove<ArrayList<String>>(stateKey)
    }
}

private const val KEY_LOAD_VALUES = "animal_purchase_load_values"
private const val KEY_ANIMAL_VALUES = "animal_purchase_animal_values"

