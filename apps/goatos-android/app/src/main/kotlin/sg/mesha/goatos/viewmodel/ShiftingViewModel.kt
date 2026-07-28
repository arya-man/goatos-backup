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
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CountsRepository
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.CountsDestinationParkDto
import sg.mesha.goatos.core.network.dto.CountsShiftingEventRequestDto
import sg.mesha.goatos.core.network.dto.GoatSearchItemDto
import sg.mesha.goatos.feature.counts.CountsWriteResultUi
import sg.mesha.goatos.feature.counts.CountsWriteStatus
import sg.mesha.goatos.feature.counts.SHIFTING_CATEGORY_BREEDING
import sg.mesha.goatos.feature.counts.SHIFTING_CATEGORY_DELIVERY
import sg.mesha.goatos.feature.counts.SHIFTING_CATEGORY_GROWTH
import sg.mesha.goatos.feature.counts.SHIFTING_CATEGORY_HEALTH
import sg.mesha.goatos.feature.counts.SHIFTING_PRIORITY_HIGH
import sg.mesha.goatos.feature.counts.SHIFTING_PRIORITY_LOW
import sg.mesha.goatos.feature.counts.ShiftingAnimalUi
import sg.mesha.goatos.feature.counts.ShiftingEvent
import sg.mesha.goatos.feature.counts.ShiftingParkUi
import sg.mesha.goatos.feature.counts.ShiftingShedUi
import sg.mesha.goatos.feature.counts.ShiftingUiState
import javax.inject.Inject

/**
 * Shifting / movement recording (`/counts/shifting`).
 *
 * Offline-first WRITE path, identical in shape to [BirthDeathViewModel]: submit enqueues to the
 * durable outbox and returns; the sync engine drains it later reusing the SAME idempotency key on
 * every attempt. That key is stable and `SavedStateHandle`-persisted (see [DraftIdempotencyKey])
 * because the backend derives the movement's LOGICAL key from it — a fresh key on resend would
 * record a second, duplicate movement of the same animal.
 *
 * Authorization boundary: the backend records the event `authorization_state=pending` /
 * `verification_state=unverified`. An operator REPORTS a movement; they do not self-authorize it,
 * so this ViewModel exposes no approve/verify action.
 *
 * The destination CATALOG is a Room-backed read model observed as a Flow
 * (docs/decisions/android-offline-first.md): the dropdowns render from cache immediately and a
 * background refresh re-emits, so a phone with no signal in a shed still opens a usable picker
 * instead of two empty menus.
 */
@HiltViewModel
class ShiftingViewModel @Inject constructor(
    private val syncRepository: SyncRepository,
    private val countsRepository: CountsRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val idempotencyKey = DraftIdempotencyKey(
        savedStateHandle = savedStateHandle,
        stateKey = KEY_IDEMPOTENCY,
        prefix = "counts-shifting",
    )
    private val outboxItemId = DraftOutboxItemId(savedStateHandle, KEY_OUTBOX_ITEM_ID)

    private val _state = MutableStateFlow(ShiftingUiState())
    val state: StateFlow<ShiftingUiState> = _state.asStateFlow()

    private var statusJob: Job? = null

    init {
        outboxItemId.value?.let(::observeOutboxItem)
        observeDestinations()
        refreshDestinations()
        recomputeSubmitGate()
    }

    fun onEvent(event: ShiftingEvent) {
        when (event) {
            is ShiftingEvent.EditAnimalQuery -> onEditAnimalQuery(event.value)
            ShiftingEvent.LookupAnimals -> lookupAnimals()
            is ShiftingEvent.SelectAnimal -> onSelectAnimal(event.goatId)
            is ShiftingEvent.SelectDestinationPark -> onSelectDestinationPark(event.parkId)
            is ShiftingEvent.SelectDestinationShed -> onSelectDestinationShed(event.shedId)
            is ShiftingEvent.SelectManagementStageMode -> onSelectManagementStageMode(event.mode)
            is ShiftingEvent.SelectTargetManagementStage -> onSelectTargetManagementStage(event.stage)
            is ShiftingEvent.SelectPriority -> onSelectPriority(event.priority)
            is ShiftingEvent.SelectCategory -> onSelectCategory(event.category)
            ShiftingEvent.Submit -> submit()
            ShiftingEvent.NavigationHandled -> _state.update {
                it.copy(returnToActions = false, submissionNotice = null)
            }
            ShiftingEvent.Back -> Unit // navigation — handled by the nav host.
        }
    }

    // -----------------------------------------------------------------------
    // Destination catalog (Room-backed read model)
    // -----------------------------------------------------------------------

    /**
     * Renders the cached catalog immediately and re-renders whenever a refresh upserts Room. A
     * cold cache leaves the dropdowns empty-but-disabled rather than showing invented options.
     */
    private fun observeDestinations() {
        viewModelScope.launch {
            countsRepository.observeShiftingDestinations().collect { resource ->
                val parks = resource.data?.parks?.map(CountsDestinationParkDto::toShiftingParkUi).orEmpty()
				val managementStages = resource.data?.managementStages.orEmpty()
                _state.update { current ->
                    // A refresh that drops the currently-chosen park or shed must not leave a
                    // stale id selected: the submit would name a destination the catalog no longer
                    // offers. Re-validate both against the new catalog and clear what is gone.
                    val parkStillOffered = parks.any { it.parkId == current.destinationParkId }
                    val parkId = if (parkStillOffered) current.destinationParkId else ""
                    val shedStillOffered = parks
                        .firstOrNull { it.parkId == parkId }
                        ?.sheds
                        ?.any { it.shedId == current.destinationShedId } == true
                    current.copy(
                        destinationParks = parks,
						managementStages = managementStages,
                        destinationParkId = parkId,
                        destinationShedId = if (shedStillOffered) current.destinationShedId else "",
                        destinationsMessage = if (parks.isEmpty()) current.destinationsMessage else null,
                    )
                }
                recomputeSubmitGate()
            }
        }
    }

    private fun refreshDestinations() {
        viewModelScope.launch {
            countsRepository.refreshShiftingDestinations()
                .onFailure { error ->
                    crashReporter.recordException(error, "counts shifting destinations refresh failed")
                    analytics.track(
                        AnalyticsEvents.COUNTS_READ_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.KIND to "shifting_destinations",
                            AnalyticsEvents.Params.REASON to (error.message ?: "unknown"),
                        ),
                    )
                    // Only surfaced when there is nothing cached to fall back on. With a cached
                    // catalog in hand the operator can still complete the movement, so a failed
                    // background refresh is not worth an error message.
                    _state.update { current ->
                        if (current.destinationParks.isNotEmpty()) {
                            current
                        } else {
                            current.copy(destinationsMessage = DESTINATIONS_FAILED_MESSAGE)
                        }
                    }
                }
        }
    }

    // -----------------------------------------------------------------------
    // Animal search + single selection
    // -----------------------------------------------------------------------

    /** Shared pre-edit guard: see [BirthDeathViewModel] for why a corrected draft re-keys. */
    private fun beginEdit(): Boolean {
        val result = _state.value.result
        if (result.isCommitted) return false
        if (result.isCorrectable) {
            idempotencyKey.invalidate()
            outboxItemId.value = null
            statusJob?.cancel()
            _state.update { it.copy(result = CountsWriteResultUi()) }
        }
        // Starting the next movement dismisses the confirmation left by the previous one.
        if (_state.value.lastRecordedMessage != null) {
            _state.update { it.copy(lastRecordedMessage = null) }
        }
        return true
    }

    private fun onEditAnimalQuery(value: String) {
        if (!beginEdit()) return
        // Editing the query clears the previous outcome copy so a stale "no match" never sits
        // under a field the operator has already corrected.
        _state.update { it.copy(animalQuery = value, animalLookupMessage = null) }
    }

    /**
     * Resolves the typed/scanned tag to real animals. The tag itself is NOT a goat id, so this
     * round trip is what keeps an RFID string out of `goat_ids`.
     *
     * Deliberately UNSCOPED by location: the operator searches the herd, not a shed they have
     * already named — there is no source input on this screen any more, because the source IS
     * whatever shed the found animal currently stands in.
     */
    private fun lookupAnimals() {
        val current = _state.value
        val query = current.animalQuery.trim()
        if (query.isEmpty() || current.isLookingUpAnimals) return
        _state.update { it.copy(isLookingUpAnimals = true, animalLookupMessage = null) }
        viewModelScope.launch {
            countsRepository.lookupAnimals(query = query)
                .onSuccess { matches ->
                    val eligible = matches.filter(GoatSearchItemDto::isEligibleForShifting)
                    _state.update {
                        it.copy(
                            isLookingUpAnimals = false,
                            animalMatches = eligible.map(GoatSearchItemDto::toShiftingAnimalUi),
                            animalLookupMessage = when {
                                eligible.isNotEmpty() -> null
                                matches.isNotEmpty() -> INELIGIBLE_ANIMAL_MESSAGE
                                else -> NO_MATCH_MESSAGE
                            },
                        )
                    }
                }
                .onFailure { error ->
                    crashReporter.recordException(error, "counts shifting animal lookup failed")
                    analytics.track(
                        AnalyticsEvents.COUNTS_READ_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.KIND to "shifting_animal_lookup",
                            AnalyticsEvents.Params.REASON to (error.message ?: "unknown"),
                        ),
                    )
                    // The cached match list is left alone: a failed search must not wipe an
                    // animal the operator already found and selected.
                    _state.update {
                        it.copy(isLookingUpAnimals = false, animalLookupMessage = LOOKUP_FAILED_MESSAGE)
                    }
                }
            recomputeSubmitGate()
        }
    }

    /**
     * Selects THE animal being moved. Single selection: this REPLACES any previous choice rather
     * than appending to a list, so the screen can never carry an animal the operator stopped
     * looking at. A tap on the already-selected row is a no-op, not a deselect — clearing the
     * selection is not a state an operator ever wants on the way to submitting a movement.
     */
    private fun onSelectAnimal(goatId: String) {
        if (!beginEdit()) return
        _state.update { current ->
            val match = current.animalMatches.firstOrNull { it.goatId == goatId }
                ?: return@update current
            current.copy(
                selectedAnimal = match,
                // A shed move is intra-farm by contract. The animal's current park is canonical,
                // so selecting the animal also selects the only legal destination farm.
                destinationParkId = match.parkId,
                destinationShedId = "",
            )
        }
        recomputeSubmitGate()
    }

    // -----------------------------------------------------------------------
    // Destination selection
    // -----------------------------------------------------------------------

    /**
     * The farm is never operator-editable. Keep this event as a defensive compatibility no-op for
     * any stale composition/test that still emits it; only selecting an animal may set the farm.
     */
    private fun onSelectDestinationPark(parkId: String) {
        if (!beginEdit()) return
        _state.update { current ->
            val currentFarm = current.selectedAnimal?.parkId.orEmpty()
            if (parkId == currentFarm) current.copy(destinationParkId = currentFarm) else current
        }
        recomputeSubmitGate()
    }

    private fun onSelectDestinationShed(shedId: String) {
        if (!beginEdit()) return
        _state.update { current ->
            // Guard the pairing at the point of selection too: only a shed that belongs to the
            // chosen park may be stored.
            val belongsToPark = current.shedsForSelectedPark.any { it.shedId == shedId }
            if (belongsToPark) current.copy(destinationShedId = shedId, targetManagementStage = "") else current
        }
        recomputeSubmitGate()
    }

    private fun onSelectManagementStageMode(mode: String) {
        if (!beginEdit() || mode !in setOf("keep_current", "select_stage", "destination_stage")) return
        _state.update { it.copy(managementStageMode = mode, targetManagementStage = "") }
        recomputeSubmitGate()
    }

    private fun onSelectTargetManagementStage(stage: String) {
        if (!beginEdit()) return
        _state.update { current ->
            val allowed = if (current.managementStageMode == "destination_stage")
                current.shedsForSelectedPark.firstOrNull { it.shedId == current.destinationShedId }?.managementStages.orEmpty()
            else current.managementStages
            if (stage in allowed) current.copy(targetManagementStage = stage) else current
        }
        recomputeSubmitGate()
    }

    private fun onSelectPriority(priority: String) {
        if (!beginEdit()) return
        if (priority !in ALLOWED_PRIORITIES) return
        _state.update { it.copy(priority = priority) }
        recomputeSubmitGate()
    }

    private fun onSelectCategory(category: String) {
        if (!beginEdit()) return
        if (category !in ALLOWED_CATEGORIES) return
        _state.update { it.copy(category = category) }
        recomputeSubmitGate()
    }

    // -----------------------------------------------------------------------
    // Submit
    // -----------------------------------------------------------------------

    private fun submit() {
        val current = _state.value
        if (!current.canSubmit) return
        val key = idempotencyKey.current()
        viewModelScope.launch {
            val result = syncRepository.enqueueCountsShifting(
                // Destination shed partitions ordering: two movements INTO the same shed drain
                // strictly oldest-first so their effects never land out of order.
                groupKey = current.destinationShedId,
                idempotencyKey = key,
                request = current.toRequest(),
            )
            when (result) {
                is AppResult.Ok -> {
                    outboxItemId.value = result.value
                    observeOutboxItem(result.value)
                    analytics.track(AnalyticsEvents.COUNTS_SHIFTING_SUBMITTED)
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "counts shifting enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.COUNTS_WRITE_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.KIND to "shifting",
                            AnalyticsEvents.Params.REASON to result.message,
                        ),
                    )
                    _state.update {
                        it.copy(result = CountsWriteResultUi(CountsWriteStatus.FAILED, result.message))
                    }
                    recomputeSubmitGate()
                }
            }
        }
    }

    /**
     * The wire body. `impacts` and `effective_at` are no longer sent at all: the backend derives
     * the movement's impact from the selected animal's own canonical breed/stage and stamps the
     * recording time itself. Source park/shed are likewise absent — the server reads the source
     * from the animal, which is the only place it was ever authoritative.
     */
    private fun ShiftingUiState.toRequest(): CountsShiftingEventRequestDto = CountsShiftingEventRequestDto(
        destinationParkId = destinationParkId,
        destinationShedId = destinationShedId,
        managementStageMode = managementStageMode,
        targetManagementStage = targetManagementStage.takeIf { managementStageMode != "keep_current" },
        priority = priority,
        category = category,
        // A list of exactly one: the contract's shape is a list and the client does not narrow a
        // server contract it does not own.
        goatIds = listOfNotNull(selectedAnimal?.goatId),
    )

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
                    val writeResult = item.toWriteResult(QUEUED_MESSAGE, SYNCED_MESSAGE)
                    // Once the movement is server-confirmed (synced), auto-clear the form for the next
                    // one and show a transient confirmation, instead of leaving the previous animal's
                    // values on a locked form. A still-syncing (queued) or terminally-rejected (failed)
                    // write keeps its banner and values.
                    if (writeResult.status == CountsWriteStatus.SYNCED) {
                        resetForNextEntry(confirmation = writeResult.message)
                        return@collect
                    }
                    _state.update { it.copy(result = writeResult) }
                    recomputeSubmitGate()
                }
        }
    }

    /**
     * Clears the form for the next movement after a server-confirmed sync. [confirmation] is shown as
     * a transient success banner above the fresh form. The committed row is durable in the outbox and
     * syncs on its own, so we drop only THIS ViewModel's references to it and mint a fresh idempotency
     * key for the next movement, while KEEPING the cached destination catalog so the form stays usable.
     */
    private fun resetForNextEntry(confirmation: String?) {
        statusJob?.cancel()
        statusJob = null
        idempotencyKey.invalidate()
        outboxItemId.value = null
        _state.update { current ->
            ShiftingUiState(
                destinationParks = current.destinationParks,
				managementStages = current.managementStages,
                lastRecordedMessage = null,
                returnToActions = true,
                submissionNotice = confirmation,
            )
        }
        recomputeSubmitGate()
    }

    private fun recomputeSubmitGate() {
        _state.update { current ->
            if (current.result.isCommitted) {
                return@update current.copy(canSubmit = false, validationMessage = null)
            }
            val problem = validate(current)
            current.copy(canSubmit = problem == null, validationMessage = problem)
        }
    }

    /**
     * Presence/shape gate only. Every rule mirrored here is ALSO enforced by the backend, which
     * stays the authority; this just spares the operator a round trip.
     */
    private fun validate(state: ShiftingUiState): String? {
        // goat_ids is REQUIRED (minItems 1). Without this gate the operator could queue a write
        // the backend is certain to reject with `missing_goat_ids` — durable in the outbox,
        // terminal on first dispatch, and only visible as a failure long after they walked away
        // from the shed.
        val animal = state.selectedAnimal ?: return "Find and select the animal that moved."
        if (!animal.lifecycleStatus.equals("alive", ignoreCase = true)) {
            return INELIGIBLE_ANIMAL_MESSAGE
        }
        if (animal.parkId.isBlank() || state.destinationParkId != animal.parkId) {
            return "This animal's current farm is unavailable. Refresh and try again."
        }
        if (state.destinationShedId.isBlank()) return "Choose the shed the animal moved to."
        if (state.managementStageMode.isBlank()) return "Choose what should happen to the animal's management stage."
        if (state.managementStageMode != "keep_current" && state.targetManagementStage.isBlank()) return "Choose the new management stage."
        return null
    }

    private companion object {
        const val KEY_IDEMPOTENCY = "countsShifting.idempotencyKey"
        const val KEY_OUTBOX_ITEM_ID = "countsShifting.outboxItemId"
        const val QUEUED_MESSAGE = "Saved on this phone. It will sync automatically."
        const val SYNCED_MESSAGE = "Movement submitted for review."

        const val NO_MATCH_MESSAGE = "No live animal matches that tag. Check the tag and try again."
        const val INELIGIBLE_ANIMAL_MESSAGE = "This animal is no longer active and cannot be shifted."
        const val LOOKUP_FAILED_MESSAGE =
            "Couldn't search for animals. Check your connection and try again."
        const val DESTINATIONS_FAILED_MESSAGE =
            "Couldn't load the list of farms and sheds. Check your connection and try again."

        /** Mirrors the two options the screen renders; a value outside it is never stored. */
        val ALLOWED_PRIORITIES = setOf(SHIFTING_PRIORITY_HIGH, SHIFTING_PRIORITY_LOW)

        val ALLOWED_CATEGORIES = setOf(
            SHIFTING_CATEGORY_GROWTH,
            SHIFTING_CATEGORY_HEALTH,
            SHIFTING_CATEGORY_BREEDING,
            SHIFTING_CATEGORY_DELIVERY,
        )
    }
}

/**
 * Wire match -> picker row.
 *
 * Park and shed are carried as SEPARATE fields (the backend's `LocationPath` exposes `park_name`
 * and `shed_name` alongside its composed `display`), which is what lets the screen show the
 * animal's current location as two labelled read-only facts. `display` is kept as the fallback for
 * a response that omits the parts — it is the backend's own composed copy, rendered verbatim, and
 * the app never assembles a location label of its own.
 */
internal fun GoatSearchItemDto.toShiftingAnimalUi(): ShiftingAnimalUi = ShiftingAnimalUi(
    goatId = goatId,
    displayId = displayId.ifBlank { animalIdentifier1 },
    tag = animalIdentifier1,
    parkId = locationPath.parkId.orEmpty(),
    shedId = locationPath.shedId.orEmpty(),
    parkName = locationPath.parkName.orEmpty(),
    shedName = locationPath.shedName.orEmpty(),
    locationLabel = locationPath.display,
    // Carried for the death target (Birth/Death screen): the write sends this row_version verbatim
    // and the confirmation card shows sex + status. Shifting ignores all three.
    rowVersion = rowVersion,
    sex = sex,
    lifecycleStatus = lifecycleStatus,
)

internal fun GoatSearchItemDto.isEligibleForShifting(): Boolean =
    lifecycleStatus.equals("alive", ignoreCase = true) &&
        !locationPath.parkId.isNullOrBlank() &&
        !locationPath.shedId.isNullOrBlank()

/** Wire catalog -> dropdown vocabulary. Both levels keep their ids: names are display only. */
internal fun CountsDestinationParkDto.toShiftingParkUi(): ShiftingParkUi = ShiftingParkUi(
    parkId = parkId,
    name = name,
    sheds = sheds.map { ShiftingShedUi(shedId = it.shedId, name = it.name, managementStages = it.managementStages) },
)
