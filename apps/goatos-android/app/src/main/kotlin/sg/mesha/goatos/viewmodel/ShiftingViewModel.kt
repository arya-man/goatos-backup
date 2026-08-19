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
import sg.mesha.goatos.feature.counts.SHIFTING_STAGE_MODE_DESTINATION
import sg.mesha.goatos.feature.counts.SHIFTING_STAGE_MODE_KEEP_CURRENT
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
            is ShiftingEvent.RemoveAnimal -> onRemoveAnimal(event.goatId)
            is ShiftingEvent.SelectDestinationPark -> onSelectDestinationPark(event.parkId)
            is ShiftingEvent.SelectDestinationShed -> onSelectDestinationShed(event.shedId, event.partitionLabel)
            is ShiftingEvent.SelectStageMode -> onSelectStageMode(event.stageMode)
            is ShiftingEvent.SelectPriority -> onSelectPriority(event.priority)
            is ShiftingEvent.SelectCategory -> onSelectCategory(event.category)
            is ShiftingEvent.EditComment -> onEditComment(event.value)
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
                _state.update { current ->
                    // A refresh that drops the currently-chosen park or shed must not leave a
                    // stale id selected: the submit would name a destination the catalog no longer
                    // offers. Re-validate both against the new catalog and clear what is gone.
                    val parkStillOffered = parks.any { it.parkId == current.destinationParkId }
                    val parkId = if (parkStillOffered) current.destinationParkId else ""
                    // Identity is shed_id + partition_label together — a partitioned shed offers
                    // several entries sharing one shed_id, so checking shed_id alone would treat a
                    // now-gone partition as still offered.
                    val destinationStillOffered = parks
                        .firstOrNull { it.parkId == parkId }
                        ?.sheds
                        ?.any {
                            it.shedId == current.destinationShedId &&
                                it.partitionLabel == current.destinationPartitionLabel
                        } == true
                    current.copy(
                        destinationParks = parks,
                        destinationParkId = parkId,
                        destinationShedId = if (destinationStillOffered) current.destinationShedId else "",
                        destinationPartitionLabel = if (destinationStillOffered) {
                            current.destinationPartitionLabel
                        } else {
                            null
                        },
                        destinationsMessage = if (parks.isEmpty()) current.destinationsMessage else null,
                    )
                        // A refresh can also change a pen's TAG -- someone re-tags it in the
                        // Counts Breakdown, or a second animal arrives and makes a
                        // single-cohort pen mixed. Re-resolve the toggle against the refreshed
                        // catalog for the same reason the ids above are re-validated: a mode the
                        // destination no longer supports is as stale as a shed that no longer
                        // exists.
                        .withStageModeValidForDestination()
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
    // Animal search + basket (multi-animal, one source pen)
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
                            animalMatches = eligible.toDistinctShiftingAnimalUi(),
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
     * ADDS the tapped match to the basket (maintainer decision 2026-08-18: one shifting moves the
     * whole group, so selection is a basket, not a single slot). A tap on an already-added animal
     * is a no-op — removal is the basket row's explicit remove control, never a re-tap, so an
     * accidental second tap can never silently drop an animal from the movement.
     *
     * ONE SOURCE PEN: every animal in one shifting must currently stand in the same park + shed +
     * pen as the animals already in the basket. A mismatched add is REFUSED with a message naming
     * the fix (the backend enforces the same rule with `mixed_source_sheds`, so accepting it here
     * would only queue a write certain to fail after the operator walked away).
     *
     * A successful add clears the query and match list so the next scan/search starts clean —
     * the gun-scan rhythm is scan → search → tap → scan again.
     */
    private fun onSelectAnimal(goatId: String) {
        if (!beginEdit()) return
        _state.update { current ->
            val match = current.animalMatches.firstOrNull { it.goatId == goatId }
                ?: return@update current
            if (current.selectedAnimals.any { it.goatId == match.goatId }) return@update current
            val anchor = current.selectedAnimals.firstOrNull()
            if (anchor != null && !samePen(anchor, match)) {
                return@update current.copy(animalLookupMessage = MIXED_PEN_MESSAGE)
            }
            current.copy(
                selectedAnimals = current.selectedAnimals + match,
                animalQuery = "",
                animalMatches = emptyList(),
                animalLookupMessage = null,
                // A shed move is intra-farm by contract. The animals' current park is canonical,
                // so the first added animal also selects the only legal destination farm. Later
                // adds are guaranteed same-park by the pen guard above, so the pinned farm and any
                // chosen destination shed stay valid.
                destinationParkId = match.parkId,
                destinationShedId = if (anchor == null) "" else current.destinationShedId,
                destinationPartitionLabel = if (anchor == null) null else current.destinationPartitionLabel,
            )
        }
        recomputeSubmitGate()
    }

    /**
     * Removes one animal from the basket. Removing the LAST animal also clears the pinned farm and
     * destination, because the next group may stand somewhere else entirely.
     */
    private fun onRemoveAnimal(goatId: String) {
        if (!beginEdit()) return
        _state.update { current ->
            val remaining = current.selectedAnimals.filterNot { it.goatId == goatId }
            if (remaining.size == current.selectedAnimals.size) return@update current
            if (remaining.isEmpty()) {
                current.copy(
                    selectedAnimals = remaining,
                    destinationParkId = "",
                    destinationShedId = "",
                    destinationPartitionLabel = null,
                )
            } else {
                current.copy(selectedAnimals = remaining)
            }
        }
        recomputeSubmitGate()
    }

    // -----------------------------------------------------------------------
    // Destination selection
    // -----------------------------------------------------------------------

    /**
     * The farm is never operator-editable. Keep this event as a defensive compatibility no-op for
     * any stale composition/test that still emits it; only adding an animal may set the farm.
     */
    private fun onSelectDestinationPark(parkId: String) {
        if (!beginEdit()) return
        _state.update { current ->
            val currentFarm = current.selectedAnimals.firstOrNull()?.parkId.orEmpty()
            if (parkId == currentFarm) current.copy(destinationParkId = currentFarm) else current
        }
        recomputeSubmitGate()
    }

    private fun onSelectDestinationShed(shedId: String, partitionLabel: String?) {
        if (!beginEdit()) return
        _state.update { current ->
            // Guard the pairing at the point of selection too: only an operational location
            // (shed_id + partition_label together) that belongs to the chosen park's catalog may
            // be stored — never just a shed_id, since a partitioned shed offers several entries
            // that share one shed_id.
            val belongsToPark = current.shedsForSelectedPark.any {
                it.shedId == shedId && it.partitionLabel == partitionLabel
            }
            if (belongsToPark) {
                current.copy(destinationShedId = shedId, destinationPartitionLabel = partitionLabel)
                    // Changing the pen changes which tags are on offer, so the toggle is
                    // re-resolved against the NEW pen. Without this, picking a tagged pen, choosing
                    // "use destination tag", then switching to an untagged pen would leave the form
                    // showing a mode that pen cannot honour -- the raise would fall back to
                    // keep-current and the operator would never be told.
                    .withStageModeValidForDestination()
            } else {
                current
            }
        }
        recomputeSubmitGate()
    }


    /**
     * Flips the tag toggle.
     *
     * Asking for the destination pen's tag when that pen cannot supply one is IGNORED rather than
     * accepted-and-quietly-downgraded. The option is greyed out for exactly those pens, so reaching
     * here means a stale composition or a race with a catalog refresh — and storing a mode the
     * destination does not support would make the form claim a tag the raise then would not apply.
     */
    private fun onSelectStageMode(stageMode: String) {
        if (!beginEdit()) return
        if (stageMode !in ALLOWED_STAGE_MODES) return
        _state.update { current ->
            if (stageMode == SHIFTING_STAGE_MODE_DESTINATION && !current.canUseDestinationStage) {
                current
            } else {
                current.copy(stageMode = stageMode)
            }
        }
        // Deliberately no recomputeSubmitGate(): the toggle can never make a movement submittable
        // or block one. Both positions are valid for every movement.
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

    /**
     * The optional raise note. Deliberately NOT followed by recomputeSubmitGate(): the comment can
     * never make a movement submittable or block one, so re-running the gate on every keystroke
     * would be work that cannot change its answer.
     *
     * Capped at the server's limit so an over-long note is stopped while the operator is still
     * typing, instead of being accepted here and rejected as comment_too_long after they submit.
     */
    private fun onEditComment(value: String) {
        if (!beginEdit()) return
        _state.update { it.copy(comment = value.take(MAX_COMMENT_LENGTH)) }
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
    /**
     * Re-resolves the tag toggle against the CURRENTLY selected destination, snapping it back to
     * keep-current whenever that pen cannot supply a tag.
     *
     * Called from every place the destination or the catalog can change, so the invariant is
     * "[ShiftingUiState.stageMode] is always a mode the selected pen supports" rather than a rule
     * each call site has to remember. Snapping DOWN to keep-current only — it never silently
     * promotes a keep-current choice back to the pen's tag, because that is the operator's decision
     * and re-making it for them would override a deliberate choice on a catalog refresh.
     */
    private fun ShiftingUiState.withStageModeValidForDestination(): ShiftingUiState = when {
        // NO DESTINATION CHOSEN YET -- leave the mode alone. A mode is only invalid RELATIVE to a
        // pen, and there is no pen here to judge it against.
        //
        // This branch is load-bearing, not defensive. The destination catalog lands while the form
        // is still empty (it is fetched on open, before the operator has looked an animal up), so
        // without it the very first refresh would see "no destination, so the pen's tag is
        // unavailable", snap the mode to keep-current, and -- because this helper only ever snaps
        // DOWN -- leave it there for the rest of the form. The default would be unreachable.
        selectedDestination == null -> this
        stageMode == SHIFTING_STAGE_MODE_DESTINATION && !canUseDestinationStage ->
            copy(stageMode = SHIFTING_STAGE_MODE_KEEP_CURRENT)
        else -> this
    }

    private fun ShiftingUiState.toRequest(): CountsShiftingEventRequestDto = CountsShiftingEventRequestDto(
        destinationParkId = destinationParkId,
        destinationShedId = destinationShedId,
        destinationPartitionLabel = destinationPartitionLabel,
        priority = priority,
        category = category,
        // Sent EXPLICITLY, like priority/category, because the screen shows the choice visibly. A
        // form that displays a selected toggle must send what it displays rather than lean on a
        // server default.
        stageMode = stageMode,
        // Blank normalizes to absent: "left empty" and "typed then cleared" are the same intent,
        // and sending "" for one of them would change the request fingerprint of an otherwise
        // identical resubmission.
        comment = comment.trim().ifBlank { null },
        // The whole basket: one movement carries every added animal, and the backend derives one
        // truthful impact row per distinct cohort from these very ids.
        goatIds = selectedAnimals.map { it.goatId },
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
        val animals = state.selectedAnimals
        if (animals.isEmpty()) return "Find and add the animals that moved."
        if (animals.any { !it.lifecycleStatus.equals("alive", ignoreCase = true) }) {
            return INELIGIBLE_ANIMAL_MESSAGE
        }
        // The add path already refuses a mixed pen; this re-checks the invariant at submit so a
        // future code path that mutates the basket some other way still cannot queue a write the
        // backend rejects with mixed_source_sheds.
        val anchor = animals.first()
        if (animals.any { !samePen(anchor, it) }) return MIXED_PEN_MESSAGE
        if (anchor.parkId.isBlank() || state.destinationParkId != anchor.parkId) {
            return "These animals' current farm is unavailable. Refresh and try again."
        }
        if (state.destinationShedId.isBlank()) return "Choose the shed the animals moved to."
        return null
    }

    /**
     * Whether two animals stand in the same source pen — park + shed + partition together, the
     * same operational-location identity the backend's source derivation requires before it will
     * record a single truthful "from". Blank and null partition both mean "not partitioned".
     */
    private fun samePen(a: ShiftingAnimalUi, b: ShiftingAnimalUi): Boolean =
        a.parkId == b.parkId &&
            a.shedId == b.shedId &&
            a.partitionLabel?.trim().orEmpty() == b.partitionLabel?.trim().orEmpty()

    private companion object {
        const val KEY_IDEMPOTENCY = "countsShifting.idempotencyKey"
        const val KEY_OUTBOX_ITEM_ID = "countsShifting.outboxItemId"
        const val QUEUED_MESSAGE = "Saved on this phone. It will sync automatically."
        const val SYNCED_MESSAGE = "Movement submitted for review."

        const val NO_MATCH_MESSAGE = "No live animal matches that tag. Check the tag and try again."
        const val INELIGIBLE_ANIMAL_MESSAGE = "This animal is no longer active and cannot be shifted."

        /**
         * Mirrors the backend's `mixed_source_sheds` rule: one shifting moves one pen's animals.
         * Refusing at ADD time — while the operator is standing at the pen — beats queueing a
         * write that is certain to fail hours later in the outbox.
         */
        const val MIXED_PEN_MESSAGE =
            "This animal is in a different shed. All animals in one shifting must come from the same shed — submit this one, then raise another shifting for the other shed."
        const val LOOKUP_FAILED_MESSAGE =
            "Couldn't search for animals. Check your connection and try again."
        const val DESTINATIONS_FAILED_MESSAGE =
            "Couldn't load the list of farms and sheds. Check your connection and try again."

        /**
         * Mirrors the server's comment bound (maxShiftingCommentRunes / the
         * shifting_events_raise_comment_length_check constraint). Kept in sync deliberately: the
         * client stops the operator at the same length the server would reject.
         */
        const val MAX_COMMENT_LENGTH = 1000

        /** Mirrors the two options the screen renders; a value outside it is never stored. */
        val ALLOWED_PRIORITIES = setOf(SHIFTING_PRIORITY_HIGH, SHIFTING_PRIORITY_LOW)

        val ALLOWED_CATEGORIES = setOf(
            SHIFTING_CATEGORY_GROWTH,
            SHIFTING_CATEGORY_HEALTH,
            SHIFTING_CATEGORY_BREEDING,
            SHIFTING_CATEGORY_DELIVERY,
        )

        /** The tag toggle's two positions; a value outside it is never stored. */
        val ALLOWED_STAGE_MODES = setOf(
            SHIFTING_STAGE_MODE_DESTINATION,
            SHIFTING_STAGE_MODE_KEEP_CURRENT,
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
    partitionLabel = locationPath.partitionLabel,
    locationLabel = locationPath.operationalLocationDisplay,
    // Carried for the death target (Birth/Death screen): the write sends this row_version verbatim
    // and the confirmation card shows sex + status. Shifting ignores all three.
    rowVersion = rowVersion,
    sex = sex,
    lifecycleStatus = lifecycleStatus,
)

internal fun List<GoatSearchItemDto>.toDistinctShiftingAnimalUi(): List<ShiftingAnimalUi> =
    distinctBy { it.goatId }.map(GoatSearchItemDto::toShiftingAnimalUi)

internal fun GoatSearchItemDto.isEligibleForShifting(): Boolean =
    lifecycleStatus.equals("alive", ignoreCase = true) &&
        !locationPath.parkId.isNullOrBlank() &&
        !locationPath.shedId.isNullOrBlank()

/**
 * Wire catalog -> dropdown vocabulary. Both levels keep their ids: names are display only.
 *
 * The backend emits one [CountsDestinationShedDto] row per selectable OPERATIONAL LOCATION — one
 * row per partition for a partitioned shed, one row (null `partition_label`) for a shed with none.
 * This mapping is a straight pass-through of that shape; it never invents or collapses rows.
 */
internal fun CountsDestinationParkDto.toShiftingParkUi(): ShiftingParkUi = ShiftingParkUi(
    parkId = parkId,
    name = name,
    sheds = sheds.map {
        ShiftingShedUi(
            shedId = it.shedId,
            // `name` IS the dropdown label the screen renders, so it must carry the
            // backend-composed display -- a partitioned shed's bare name repeats once per
            // partition ("Godel 1" three times) and the operator cannot tell them apart.
            // Falls back to name for legacy API responses that omit the display field.
            // operationalLocationDisplay is also passed through for callers that want the
            // raw parts; the two are deliberately the same string here.
            name = it.operationalLocationDisplay.ifBlank { it.name },
            partitionLabel = it.partitionLabel,
            operationalLocationDisplay = it.operationalLocationDisplay,
            // Both carried verbatim for the tag toggle. Exactly one is non-blank on a response
            // from a current backend; both blank means an older backend that predates the toggle,
            // which reads as "this pen offers no tag" and leaves the form on keep-current -- the
            // safe direction, since that build's server would ignore the mode anyway.
            destinationStage = it.destinationStage,
            destinationStageReason = it.destinationStageReason,
        )
    },
)
