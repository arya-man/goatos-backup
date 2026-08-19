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
import sg.mesha.goatos.core.network.dto.CountsBirthEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsDeathEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsDestinationParkDto
import sg.mesha.goatos.core.network.dto.CountsEvidenceRefDto
import sg.mesha.goatos.feature.counts.BIRTH_ID_KIND_PERMANENT
import sg.mesha.goatos.feature.counts.BIRTH_ID_KIND_TEMPORARY
import sg.mesha.goatos.feature.counts.BirthDeathEvent
import sg.mesha.goatos.feature.counts.BirthDeathField
import sg.mesha.goatos.feature.counts.BirthDeathMode
import sg.mesha.goatos.feature.counts.BirthDeathUiState
import sg.mesha.goatos.feature.counts.CountsFilterOptionUi
import sg.mesha.goatos.feature.counts.CountsWriteResultUi
import sg.mesha.goatos.rfid.ScanSource
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import javax.inject.Inject

/**
 * Birth / death recording (`/counts/birth-death`).
 *
 * Offline-first WRITE path: submitting never calls the network from here. The request is handed to
 * the durable outbox ([SyncRepository.enqueueCountsBirth] / [SyncRepository.enqueueCountsDeath]),
 * which returns as soon as the row is committed to Room; the sync engine drains it later with the
 * SAME idempotency key on every attempt. The banner then follows that row's real status.
 *
 * Both modes are SELECTOR/SCAN-driven, sharing the Shifting screen's infrastructure so the two
 * write screens behave identically:
 *  - Birth PLACEMENT is a park -> shed cascade over the Room-cached shifting-destinations catalog
 *    ([CountsRepository.observeShiftingDestinations] / [CountsRepository.refreshShiftingDestinations]).
 *    Names render; ids submit. Placement is REQUIRED — a newborn can never be recorded into no shed.
 *  - Death TARGET is a tag/RFID search ([CountsRepository.lookupAnimals]) then a single selection.
 *    The selected animal carries its own `row_version` from the search result; the operator never
 *    types a record version or an internal goat id.
 *
 * Validation split, per AGENTS.md's validate-or-reject rule:
 *  - this ViewModel gates SUBMIT on the required selections/fields being present and well-formed;
 *  - the BACKEND remains the authority on business rules (dob <= entry_date, identifier
 *    uniqueness, the dead+died pairing, row-version freshness). Nothing here silently rewrites an
 *    out-of-range value into a default the operator never entered.
 *
 * Medical guardrail: the death path targets identity's critical-death exit, where
 * `lifecycle_status="dead"` + `exit_reason="died"` is enforced server-side. This ViewModel sends
 * the DTO's constants and never lets the operator choose another pairing.
 *
 * The birth path's two permanent identifiers can be SCANNED rather than typed: [scanSource] is the
 * same BT-HID keyboard-wedge port (`docs/mobile/rfid-keyboard-reader.md`) the Submit recording form
 * uses, so no Bluetooth/InputManager API reaches this layer. A scan is applied through the SAME
 * [onEditField] path a typed value takes, so the draft guard and submit gate cannot diverge between
 * the two input methods.
 */
@HiltViewModel
class BirthDeathViewModel @Inject constructor(
    private val syncRepository: SyncRepository,
    private val countsRepository: CountsRepository,
    private val scanSource: ScanSource,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val idempotencyKey = DraftIdempotencyKey(
        savedStateHandle = savedStateHandle,
        stateKey = KEY_IDEMPOTENCY,
        prefix = "counts-birth-death",
    )
    private val outboxItemId = DraftOutboxItemId(savedStateHandle, KEY_OUTBOX_ITEM_ID)

    private val _state = MutableStateFlow(BirthDeathUiState())
    val state: StateFlow<BirthDeathUiState> = _state.asStateFlow()

    private var statusJob: Job? = null
    private var scanJob: Job? = null

    init {
        // A ViewModel recreated after process death resumes following its already-queued write
        // instead of showing a blank form that invites a duplicate entry.
        outboxItemId.value?.let(::observeOutboxItem)
        observeDestinations()
        refreshDestinations()
        observeBreedOptions()
        refreshBreedOptions()
        recomputeSubmitGate()
    }

    fun onEvent(event: BirthDeathEvent) {
        when (event) {
            is BirthDeathEvent.SelectMode -> onSelectMode(event.mode)
            is BirthDeathEvent.EditField -> onEditField(event.field, event.value)
            is BirthDeathEvent.ToggleRfidScan -> toggleScan(event.field)
            is BirthDeathEvent.SelectPark -> onSelectPark(event.parkId)
            is BirthDeathEvent.SelectShed -> onSelectShed(event.shedId)
            is BirthDeathEvent.EditAnimalQuery -> onEditAnimalQuery(event.value)
            BirthDeathEvent.LookupAnimals -> lookupAnimals()
            is BirthDeathEvent.SelectAnimal -> onSelectAnimal(event.goatId)
            BirthDeathEvent.Submit -> submit()
            BirthDeathEvent.RecordAnother -> onRecordAnother()
            BirthDeathEvent.Back -> Unit // navigation — handled by the nav host.
        }
    }

    private fun onSelectMode(mode: BirthDeathMode) {
        if (_state.value.result.isCommitted) return
        // Death has no permanent-identifier field; never leave the reader listening into a form
        // that has nowhere to put a tag.
        stopScanning()
        // Switching mode makes this a different write; drop the draft key so the new event can
        // never inherit the other mode's identity.
        idempotencyKey.invalidate()
        _state.update {
            it.copy(mode = mode, result = CountsWriteResultUi(), validationMessage = null, lastRecordedMessage = null)
        }
        recomputeSubmitGate()
    }

    /**
     * Shared pre-edit guard (mirrors [ShiftingViewModel]): a committed draft is durable and must not
     * be mutated underneath the outbox row; correcting a terminally-rejected write re-keys it so the
     * outbox does not reject the corrected payload under the old key. Returns false when the current
     * draft is committed and no edit may proceed.
     */
    private fun beginEdit(): Boolean {
        val result = _state.value.result
        if (result.isCommitted) return false
        if (result.isCorrectable) {
            idempotencyKey.invalidate()
            outboxItemId.value = null
            statusJob?.cancel()
            _state.update { it.copy(result = CountsWriteResultUi()) }
        }
        // Starting the next entry dismisses the "Recorded" confirmation left by the previous write.
        if (_state.value.lastRecordedMessage != null) {
            _state.update { it.copy(lastRecordedMessage = null) }
        }
        return true
    }

    private fun onEditField(field: BirthDeathField, value: String) {
        if (!beginEdit()) return
        // Switching to the temporary-tag path hides both permanent-RFID fields (a provisional tag
        // has nothing to read), so any scan in progress has lost its destination.
        if (field == BirthDeathField.ID_KIND && value == BIRTH_ID_KIND_TEMPORARY) stopScanning()
        _state.update { current ->
            when (field) {
                BirthDeathField.ID_KIND -> current.copy(idKind = value)
                BirthDeathField.TAG -> current.copy(tag = value)
                BirthDeathField.TAG2 -> current.copy(tag2 = value)
                BirthDeathField.SPECIES -> current.copy(species = value)
                BirthDeathField.SEX -> current.copy(sex = value)
                BirthDeathField.BREED -> current.copy(breed = value)
                BirthDeathField.DOB -> current.copy(dob = value)
                BirthDeathField.ENTRY_DATE -> current.copy(entryDate = value)
                BirthDeathField.DAM_ID -> current.copy(damId = value)
                BirthDeathField.REASON -> current.copy(reason = value)
            }
        }
        recomputeSubmitGate()
    }

    // -----------------------------------------------------------------------
    // Bluetooth RFID scan — one permanent-identifier field at a time
    // -----------------------------------------------------------------------

    /**
     * Hands the BT-HID reader to [field], or stops it when [field] is already the one listening.
     * A completed tag fills that field and STOPS the reader: an ear tag is one identifier, so
     * leaving capture running would let the next animal's tag silently overwrite it.
     *
     * Only the two permanent identifiers are scannable; any other field is ignored rather than
     * silently starting a reader whose read has nowhere to land.
     */
    private fun toggleScan(field: BirthDeathField) {
        if (field != BirthDeathField.TAG && field != BirthDeathField.TAG2) return
        if (_state.value.result.isCommitted) return // the birth is durable; nothing left to edit
        if (_state.value.scanningField == field) {
            stopScanning()
            return
        }
        stopScanning()
        _state.update { it.copy(scanningField = field) }
        scanSource.start()
        analytics.track(
            AnalyticsEvents.COUNTS_RFID_SCAN_STARTED,
            mapOf(
                AnalyticsEvents.Params.KIND to "birth",
                AnalyticsEvents.Params.FIELD to field.name.lowercase(),
            ),
        )
        scanJob = viewModelScope.launch {
            scanSource.tags.collect { tag ->
                // Through the ordinary edit path: same draft/re-key guard and submit gate a typed
                // identifier gets, so scanning can never bypass a validation a keyboard cannot.
                onEditField(field, tag)
                analytics.track(
                    AnalyticsEvents.COUNTS_RFID_SCAN_CAPTURED,
                    mapOf(
                        AnalyticsEvents.Params.KIND to "birth",
                        AnalyticsEvents.Params.FIELD to field.name.lowercase(),
                    ),
                )
                stopScanning()
            }
        }
    }

    private fun stopScanning() {
        if (_state.value.scanningField == null) return
        scanSource.stop()
        scanJob?.cancel()
        scanJob = null
        _state.update { it.copy(scanningField = null) }
    }

    override fun onCleared() {
        // Leaving the screen must release the reader: capture consumes hardware key events
        // app-wide while enabled, so a leaked listener would eat another screen's input.
        stopScanning()
        super.onCleared()
    }

    // -----------------------------------------------------------------------
    // Birth placement — destination catalog (Room-backed read model)
    // -----------------------------------------------------------------------

    /**
     * Renders the cached catalog immediately and re-renders whenever a refresh upserts Room. A
     * refresh that drops the currently-chosen park or shed re-validates the selection and clears
     * what is gone, so submit can never name a placement the catalog no longer offers.
     */
    private fun observeDestinations() {
        viewModelScope.launch {
            countsRepository.observeShiftingDestinations().collect { resource ->
                val parks = resource.data?.parks?.map(CountsDestinationParkDto::toShiftingParkUi).orEmpty()
                _state.update { current ->
                    val parkStillOffered = parks.any { it.parkId == current.parkId }
                    val parkId = if (parkStillOffered) current.parkId else ""
                    val shedStillOffered = parks
                        .firstOrNull { it.parkId == parkId }
                        ?.sheds
                        ?.any { it.shedId == current.shedId } == true
                    current.copy(
                        destinationParks = parks,
                        parkId = parkId,
                        shedId = if (shedStillOffered) current.shedId else "",
                        destinationsMessage = if (parks.isEmpty()) current.destinationsMessage else null,
                    )
                }
                recomputeSubmitGate()
            }
        }
    }

    /**
     * Breed is chosen, not typed. The option list is the herd's OWN breed vocabulary, served by
     * `GET /app/counts/breeds` on the operator surface — so the app never invents a breed vocabulary
     * of its own, and a field operator (who holds CountsWrite but not the CountsRead the Counts
     * Breakdown screen needs) still gets real options. Cache-first and reactive: it emits from Room
     * immediately and re-emits after [refreshBreedOptions]. A refresh that drops the currently-selected
     * breed clears it so submit can never name a breed the vocabulary no longer offers. The option's
     * own `key` is what submits.
     */
    private fun observeBreedOptions() {
        viewModelScope.launch {
            countsRepository.observeBirthBreeds().collect { resource ->
                val options = resource.data?.breeds
                    ?.map { CountsFilterOptionUi(it.key, it.label, it.count) }
                    .orEmpty()
                _state.update { current ->
                    val breedStillOffered = options.any { it.key == current.breed }
                    current.copy(
                        breedOptions = options,
                        breed = if (breedStillOffered) current.breed else "",
                    )
                }
                recomputeSubmitGate()
            }
        }
    }

    /**
     * Warms the breed vocabulary from `GET /app/counts/breeds` so the breed picker is usable even for
     * a field operator with no Counts page access. [observeBreedOptions] reads only the Room-cached
     * vocabulary, so without this fetch the picker stays disabled on a cold cache. The operator surface
     * is deliberate: the Counts Breakdown breed facet is CountsRead and would 403 for the very users
     * who record births. Failure is non-fatal: the picker falls back to whatever was already cached.
     */
    private fun refreshBreedOptions() {
        viewModelScope.launch {
            countsRepository.refreshBirthBreeds()
                .onFailure { error ->
                    crashReporter.recordException(error, "counts birth-death breed vocabulary refresh failed")
                    analytics.track(
                        AnalyticsEvents.COUNTS_READ_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.KIND to "birth_breed_vocabulary",
                            AnalyticsEvents.Params.REASON to (error.message ?: "unknown"),
                        ),
                    )
                }
        }
    }

    private fun refreshDestinations() {
        viewModelScope.launch {
            countsRepository.refreshShiftingDestinations()
                .onFailure { error ->
                    crashReporter.recordException(error, "counts birth-death destinations refresh failed")
                    analytics.track(
                        AnalyticsEvents.COUNTS_READ_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.KIND to "birth_placement_destinations",
                            AnalyticsEvents.Params.REASON to (error.message ?: "unknown"),
                        ),
                    )
                    // Only surfaced when there is nothing cached to fall back on.
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

    /**
     * Choosing a park RESETS the shed. A shed id belongs to exactly one park, so carrying the old
     * shed forward would submit a park/shed pairing that does not exist — and because shed NAMES
     * repeat across parks, that mistake would look plausible on screen.
     */
    private fun onSelectPark(parkId: String) {
        if (!beginEdit()) return
        _state.update { current ->
            if (current.parkId == parkId) current else current.copy(parkId = parkId, shedId = "")
        }
        recomputeSubmitGate()
    }

    /**
     * [optionKey] is the composite `shedId|partitionLabel` dropdown key, not a shed id — the
     * destinations feed returns one option per PARTITION, so a shed id does not identify a choice.
     */
    private fun onSelectShed(optionKey: String) {
        if (!beginEdit()) return
        _state.update { current ->
            // Guard the pairing at selection too: only an option that belongs to the chosen park stores.
            val option = current.shedsForSelectedPark.firstOrNull { it.optionKey == optionKey }
            if (option != null) {
                current.copy(shedId = option.shedId, partitionLabel = option.partitionLabel)
            } else {
                current
            }
        }
        recomputeSubmitGate()
    }

    // -----------------------------------------------------------------------
    // Death target — animal search + single selection
    // -----------------------------------------------------------------------

    private fun onEditAnimalQuery(value: String) {
        if (!beginEdit()) return
        _state.update { it.copy(animalQuery = value, animalLookupMessage = null) }
    }

    /**
     * Resolves the typed/scanned tag to real animals. The tag itself is NOT a goat id, so this round
     * trip is what keeps an RFID string out of the death write and pins the animal's `row_version`.
     */
    private fun lookupAnimals() {
        val current = _state.value
        val query = current.animalQuery.trim()
        if (query.isEmpty() || current.isLookingUpAnimals) return
        _state.update { it.copy(isLookingUpAnimals = true, animalLookupMessage = null) }
        viewModelScope.launch {
            countsRepository.lookupAnimals(query = query)
                .onSuccess { matches ->
                    _state.update {
                        it.copy(
                            isLookingUpAnimals = false,
                            animalMatches = matches.toDistinctShiftingAnimalUi(),
                            animalLookupMessage = if (matches.isEmpty()) NO_MATCH_MESSAGE else null,
                        )
                    }
                }
                .onFailure { error ->
                    crashReporter.recordException(error, "counts death animal lookup failed")
                    analytics.track(
                        AnalyticsEvents.COUNTS_READ_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.KIND to "death_animal_lookup",
                            AnalyticsEvents.Params.REASON to (error.message ?: "unknown"),
                        ),
                    )
                    // A failed search must not wipe an animal the operator already found and selected.
                    _state.update {
                        it.copy(isLookingUpAnimals = false, animalLookupMessage = LOOKUP_FAILED_MESSAGE)
                    }
                }
            recomputeSubmitGate()
        }
    }

    /**
     * Selects THE animal whose death is being recorded. Single selection: this REPLACES any previous
     * choice rather than appending, so the screen can never carry an animal the operator stopped
     * looking at. A tap on the already-selected row is a no-op, not a deselect.
     */
    private fun onSelectAnimal(goatId: String) {
        if (!beginEdit()) return
        _state.update { current ->
            val match = current.animalMatches.firstOrNull { it.goatId == goatId } ?: return@update current
            current.copy(selectedAnimal = match)
        }
        recomputeSubmitGate()
    }

    // -----------------------------------------------------------------------
    // Submit
    // -----------------------------------------------------------------------

    private fun submit() {
        stopScanning() // the identifiers are settled; release the reader before the write
        val current = _state.value
        if (!current.canSubmit) return
        // One stable key for this draft, reused verbatim on every retry the sync engine makes.
        val key = idempotencyKey.current()
        val evidence = listOf(
            CountsEvidenceRefDto(
                evidenceId = key,
                description = "Recorded on the operator app",
            ),
        )
        viewModelScope.launch {
            val result = when (current.mode) {
                BirthDeathMode.BIRTH -> enqueueBirth(current, key, evidence)
                BirthDeathMode.DEATH -> enqueueDeath(current, key, evidence)
            }
            when (result) {
                is AppResult.Ok -> {
                    outboxItemId.value = result.value
                    observeOutboxItem(result.value)
                    analytics.track(
                        if (current.mode == BirthDeathMode.BIRTH) {
                            AnalyticsEvents.COUNTS_BIRTH_SUBMITTED
                        } else {
                            AnalyticsEvents.COUNTS_DEATH_SUBMITTED
                        },
                    )
                }
                is AppResult.Err -> onEnqueueFailed(current.mode, result)
            }
        }
    }

    private suspend fun enqueueBirth(
        current: BirthDeathUiState,
        key: String,
        evidence: List<CountsEvidenceRefDto>,
    ): AppResult<String> = syncRepository.enqueueCountsBirth(
        // The newborn's own tag partitions ordering: two writes about the same animal drain
        // strictly oldest-first, different animals drain concurrently.
        groupKey = current.tag.trim(),
        idempotencyKey = key,
        request = CountsBirthEventRequestDto(
            // Exactly one primary identity is sent: a permanent RFID or a provisional temporary tag,
            // per the operator's toggle. The backend rejects both-or-neither.
            animalIdentifier1 = current.tag.trim().takeIf { current.idKind == BIRTH_ID_KIND_PERMANENT },
            temporaryIdentifier = current.tag.trim().takeIf { current.idKind == BIRTH_ID_KIND_TEMPORARY },
            // An optional SECOND permanent RFID for a newborn given two ear tags. Permanent path only:
            // a temporary tag never carries a second permanent RFID (the backend rejects that pairing).
            animalIdentifier2 = current.tag2.trim()
                .takeIf { current.idKind == BIRTH_ID_KIND_PERMANENT && it.isNotBlank() },
            species = current.species,
            // Placement ids come from the destinations catalog, not free text — never a typed UUID.
            parkId = current.parkId.ifBlank { null },
            shedId = current.shedId.ifBlank { null },
            partitionLabel = current.partitionLabel?.takeIf { it.isNotBlank() },
            // Breed is the selected facet key from the herd's own vocabulary, never typed.
            breed = current.breed.trim(),
            sex = current.sex,
            dob = current.dob.trim(),
            // Entry date defaults to today's business date (Asia/Kolkata) — stamped on open so an
            // offline entry keeps its real recording date, not the later sync date — but is editable
            // via the M3 date picker, so the operator's chosen value is what ships. The backend
            // stays the authority on dob <= entry_date.
            entryDate = current.entryDate.trim().ifBlank { todayBusinessDate() },
            damId = current.damId.trim(),
            litterSize = 1,
            evidenceRefs = evidence,
        ),
    )

    private suspend fun enqueueDeath(
        current: BirthDeathUiState,
        key: String,
        evidence: List<CountsEvidenceRefDto>,
    ): AppResult<String> {
        // canSubmit already guarantees a selection; the elvis is defensive only.
        val animal = current.selectedAnimal ?: return AppResult.Err("Select the animal that died.")
        return syncRepository.enqueueCountsDeath(
            groupKey = animal.goatId,
            idempotencyKey = key,
            request = CountsDeathEventRequestDto(
                goatId = animal.goatId,
                // lifecycle_status / exit_reason are left at the DTO's guardrail constants on
                // purpose — the dead+died pairing is not an operator choice.
                reason = current.reason.trim(),
                evidenceRefs = evidence,
                // The animal's OWN optimistic-concurrency token from the search result — never a
                // hand-typed record version, never defaulted to a value that would overwrite a
                // concurrent edit. A stale token is caught server-side.
                rowVersion = animal.rowVersion,
            ),
        )
    }

    /**
     * Clears the form for the NEXT entry after a committed write. The committed row is durable in
     * the outbox and syncs on its own, so we drop only THIS ViewModel's references to it: stop
     * following its status, drop the persisted outbox id, and mint a fresh idempotency key so the
     * next animal is a genuinely new write (never a duplicate under the previous draft's key). The
     * draft resets to blank defaults while KEEPING the cached park/shed and breed vocabularies so the
     * form is immediately usable, and re-stamps today's entry date (the screen only auto-stamps on a
     * mode change, which does not happen here).
     */
    private fun onRecordAnother() = resetForNextEntry(confirmation = null)

    /**
     * Clears the form for the next entry. [confirmation], when non-null, is shown as a transient
     * success banner above the fresh form — used by the auto-reset once the write is server-confirmed
     * (synced) so the operator sees the record landed without the previous animal's values lingering
     * on a locked form. The committed row is durable in the outbox and syncs on its own, so we drop
     * only THIS ViewModel's references to it and mint a fresh idempotency key for the next animal.
     * The draft resets to blank defaults while KEEPING the cached park/shed and breed vocabularies so
     * the form is immediately usable, and re-stamps today's entry date.
     */
    private fun resetForNextEntry(confirmation: String?) {
        stopScanning()
        statusJob?.cancel()
        statusJob = null
        idempotencyKey.invalidate()
        outboxItemId.value = null
        _state.update { current ->
            BirthDeathUiState(
                mode = current.mode,
                destinationParks = current.destinationParks,
                breedOptions = current.breedOptions,
                entryDate = todayBusinessDate(),
                lastRecordedMessage = confirmation,
            )
        }
        recomputeSubmitGate()
    }

    private fun onEnqueueFailed(mode: BirthDeathMode, error: AppResult.Err) {
        val kind = if (mode == BirthDeathMode.BIRTH) "birth" else "death"
        error.cause?.let { crashReporter.recordException(it, "counts $kind enqueue failed") }
        analytics.track(
            AnalyticsEvents.COUNTS_WRITE_FAILURE,
            mapOf(
                AnalyticsEvents.Params.KIND to kind,
                AnalyticsEvents.Params.REASON to error.message,
            ),
        )
        _state.update {
            it.copy(
                result = CountsWriteResultUi(
                    status = sg.mesha.goatos.feature.counts.CountsWriteStatus.FAILED,
                    message = error.message,
                ),
            )
        }
        recomputeSubmitGate()
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
                    val writeResult = item.toWriteResult(QUEUED_MESSAGE, SYNCED_MESSAGE)
                    // Once the server confirms the write (synced), auto-clear the form for the next
                    // entry and show a transient "Recorded" confirmation, instead of leaving the
                    // previous animal's values on a locked form. A still-syncing (queued) write keeps
                    // its saved-offline banner plus the manual "Record another" action, and a
                    // terminally-rejected (failed) write keeps its error and values for correction.
                    if (writeResult.status == sg.mesha.goatos.feature.counts.CountsWriteStatus.SYNCED) {
                        resetForNextEntry(confirmation = writeResult.message)
                        return@collect
                    }
                    _state.update { it.copy(result = writeResult) }
                    recomputeSubmitGate()
                }
        }
    }

    /**
     * Submit is enabled only when every REQUIRED input for the active mode is present and
     * well-formed. Nothing here invents a value for a missing field.
     */
    private fun recomputeSubmitGate() {
        _state.update { current ->
            if (current.result.isCommitted) {
                return@update current.copy(canSubmit = false, validationMessage = null)
            }
            val missing = when (current.mode) {
                BirthDeathMode.BIRTH -> birthValidation(current)
                BirthDeathMode.DEATH -> deathValidation(current)
            }
            current.copy(canSubmit = missing == null, validationMessage = missing)
        }
    }

    private fun birthValidation(state: BirthDeathUiState): String? = when {
        state.tag.isBlank() -> "Enter the newborn's identifier."
        // Optional second permanent RFID (permanent path only). When present it must differ from the
        // first — the backend enforces this too, but catching it here saves the round trip.
        state.idKind == BIRTH_ID_KIND_PERMANENT &&
            state.tag2.isNotBlank() &&
            state.tag2.trim().equals(state.tag.trim(), ignoreCase = true) ->
            "The second RFID must differ from the first."
        !isIsoDate(state.dob) -> "Enter the date of birth as YYYY-MM-DD."
        // Entry date is auto-stamped to today's business date, so dob <= entry_date reduces to
        // dob <= today. Mirrors the backend rule so the operator sees it before the round trip; the
        // server still enforces it independently. String compare is safe on ISO YYYY-MM-DD dates.
        state.dob.trim() > todayBusinessDate() -> "Date of birth cannot be in the future."
        state.breed.isBlank() -> "Choose the newborn's breed."
        state.damId.isBlank() -> "Scan or enter the mother's RFID."
        // Placement is REQUIRED and chosen from the catalog — a newborn is never recorded into no
        // shed, and the ids can only ever be real park/shed ids the picker offered.
        state.parkId.isBlank() -> "Choose the park the newborn is placed in."
        state.shedId.isBlank() -> "Choose the shed the newborn is placed in."
        else -> null
    }

    private fun deathValidation(state: BirthDeathUiState): String? = when {
        // The animal is chosen from a tag search, which carries the goat_id AND the row_version —
        // the operator never types either.
        state.selectedAnimal == null -> "Find and select the animal that died."
        state.reason.trim().length < 3 -> "Describe what happened (at least 3 characters)."
        state.reason.trim().length > 500 -> "Keep the account under 500 characters."
        else -> null
    }

    /** Shape-only check; the backend parses and validates the calendar date itself. */
    private fun isIsoDate(value: String): Boolean = ISO_DATE.matches(value.trim())

    /**
     * Today's business date in India-business-calendar terms (Asia/Kolkata), as ISO `YYYY-MM-DD`.
     * Per AGENTS.md time semantics, a Goat OS business day is never defined by UTC. Used both as the
     * auto entry date on the birth write and as the "not in the future" ceiling for the date of birth.
     */
    private fun todayBusinessDate(): String =
        LocalDate.now(ZoneId.of("Asia/Kolkata")).format(DateTimeFormatter.ISO_LOCAL_DATE)

    private companion object {
        const val KEY_IDEMPOTENCY = "countsBirthDeath.idempotencyKey"
        const val KEY_OUTBOX_ITEM_ID = "countsBirthDeath.outboxItemId"
        const val QUEUED_MESSAGE = "Saved on this phone. It will sync automatically."
        const val SYNCED_MESSAGE = "Recorded."

        const val NO_MATCH_MESSAGE = "No live animal matches that tag. Check the tag and try again."
        const val LOOKUP_FAILED_MESSAGE =
            "Couldn't search for animals. Check your connection and try again."
        const val DESTINATIONS_FAILED_MESSAGE =
            "Couldn't load the list of parks and sheds. Check your connection and try again."

        val ISO_DATE = Regex("""\d{4}-\d{2}-\d{2}""")
    }
}
