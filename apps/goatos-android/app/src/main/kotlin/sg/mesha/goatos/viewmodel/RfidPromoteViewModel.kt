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
import sg.mesha.goatos.core.data.AwaitingRfidRepository
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.TemporaryTaggedGoatDto
import sg.mesha.goatos.feature.counts.CountsWriteResultUi
import sg.mesha.goatos.feature.counts.CountsWriteStatus
import sg.mesha.goatos.feature.counts.RfidPromoteEvent
import sg.mesha.goatos.feature.counts.RfidPromoteField
import sg.mesha.goatos.feature.counts.RfidPromoteUiState
import sg.mesha.goatos.rfid.ScanSource
import javax.inject.Inject

/**
 * Birth's final `Tag the kid` RFID screen — an operator assigning a permanent RFID to the
 * canonical kid created with a provisional identifier at birth.
 *
 * Offline-first write (docs/decisions/android-offline-first.md): **Promote** enqueues the retag to the
 * durable outbox via [SyncRepository.enqueuePromoteIdentifier]. Idempotency is a STABLE
 * `SavedStateHandle`-persisted key keyed to the goat, so a resend after process death collapses onto
 * the original promotion instead of retagging twice. The Birth workflow passes the same backend-
 * owned kid identity and goat row version that it rendered; restored legacy routes may fall back to
 * the old bounded Room cache.
 *
 * The two permanent identifiers can be SCANNED rather than typed: [scanSource] is the same BT-HID
 * keyboard-wedge port (`docs/mobile/rfid-keyboard-reader.md`) the Submit recording form uses, so no
 * Bluetooth/InputManager API reaches this layer. A scan writes the tag through the SAME
 * [RfidPromoteEvent.RfidChanged] / [RfidPromoteEvent.Rfid2Changed] path a typed value takes — the
 * submit gate and validation cannot diverge between the two input methods.
 */
@HiltViewModel
class RfidPromoteViewModel @Inject constructor(
    private val repo: AwaitingRfidRepository,
    private val syncRepository: SyncRepository,
    private val scanSource: ScanSource,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val goatId: String = savedStateHandle[ARG_GOAT_ID] ?: ""
    private val routeDisplayId: String = savedStateHandle[ARG_DISPLAY_ID] ?: ""
    private val routeTemporaryIdentifier: String = savedStateHandle[ARG_TEMPORARY_IDENTIFIER] ?: ""
    private val routeLocationDisplay: String = savedStateHandle[ARG_LOCATION_DISPLAY] ?: ""
    private val routeRowVersion: Int = savedStateHandle[ARG_ROW_VERSION] ?: 0

    // The goat's row_version from the Birth workflow, sent back verbatim so a stale in-hand record
    // is rejected. A restored legacy route may resolve it from Room; submit never accepts it from UI.
    private var rowVersion: Int = routeRowVersion

    private val promoteKey = DraftIdempotencyKey(savedStateHandle, KEY_PROMOTE_IDEMPOTENCY, "counts-promote-identifier")
    private val outboxItemId = DraftOutboxItemId(savedStateHandle, KEY_OUTBOX_ITEM_ID)

    private val _state = MutableStateFlow(
        RfidPromoteUiState(
            goatId = goatId,
            loading = routeContextAvailable().not(),
            displayId = routeDisplayId,
            temporaryIdentifier = routeTemporaryIdentifier,
            locationDisplay = routeLocationDisplay,
        ),
    )
    val state: StateFlow<RfidPromoteUiState> = _state.asStateFlow()

    private var statusJob: Job? = null
    private var scanJob: Job? = null

    init {
        analytics.track(AnalyticsEvents.COUNTS_RFID_PROMOTE_OPENED)
        if (!routeContextAvailable()) loadGoat()
        outboxItemId.value?.let(::observeOutboxItem)
    }

    fun onEvent(event: RfidPromoteEvent) {
        when (event) {
            is RfidPromoteEvent.RfidChanged -> onRfidChanged(event.value)
            is RfidPromoteEvent.Rfid2Changed -> _state.update { it.copy(rfid2Input = event.value, inputError = null) }
            is RfidPromoteEvent.ToggleRfidScan -> toggleScan(event.field)
            RfidPromoteEvent.Submit -> submit()
            RfidPromoteEvent.Back -> Unit // navigation — handled by the nav host.
        }
    }

    // -----------------------------------------------------------------------
    // Bluetooth RFID scan — one field at a time
    // -----------------------------------------------------------------------

    /**
     * Hands the BT-HID reader to [field], or stops it when [field] is already the one listening.
     * A completed tag fills that field and STOPS the reader: an ear tag is one identifier, so
     * leaving capture running would let the next animal's tag silently overwrite it.
     */
    private fun toggleScan(field: RfidPromoteField) {
        if (_state.value.result.isCommitted) return // the retag is durable; nothing left to edit
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
                AnalyticsEvents.Params.KIND to "rfid_promote",
                AnalyticsEvents.Params.FIELD to field.name.lowercase(),
            ),
        )
        scanJob = viewModelScope.launch {
            scanSource.tags.collect { tag ->
                when (field) {
                    RfidPromoteField.PRIMARY -> onRfidChanged(tag)
                    RfidPromoteField.SECONDARY -> _state.update { it.copy(rfid2Input = tag, inputError = null) }
                }
                analytics.track(
                    AnalyticsEvents.COUNTS_RFID_SCAN_CAPTURED,
                    mapOf(
                        AnalyticsEvents.Params.KIND to "rfid_promote",
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

    private fun loadGoat() {
        viewModelScope.launch {
            val cached = repo.findCached(goatId)
            if (cached == null) {
                _state.update { it.copy(loading = false, notFound = true, canSubmit = false) }
                return@launch
            }
            rowVersion = cached.rowVersion
            _state.update { current -> cached.toUiState(current) }
        }
    }

    private fun routeContextAvailable(): Boolean =
        goatId.isNotBlank() && routeDisplayId.isNotBlank() && routeTemporaryIdentifier.isNotBlank() && routeRowVersion > 0

    private fun onRfidChanged(value: String) {
        _state.update {
            it.copy(
                rfidInput = value,
                inputError = null,
                // A committed write stays disabled; otherwise enable once there is a non-blank RFID.
                canSubmit = value.isNotBlank() && !it.result.isCommitted,
            )
        }
    }

    private fun submit() {
        stopScanning() // the identifiers are settled; release the reader before the write
        val current = _state.value
        val rfid = current.rfidInput.trim()
        val rfid2 = current.rfid2Input.trim()
        if (rfid.isEmpty()) {
            _state.update { it.copy(inputError = "Enter the permanent RFID.", canSubmit = false) }
            return
        }
        if (rfid2.isNotEmpty() && rfid2 == rfid) {
            _state.update { it.copy(inputError = "The second RFID must differ from the first.", canSubmit = false) }
            return
        }
        if (!current.canSubmit) return
        _state.update { it.copy(canSubmit = false) }
        viewModelScope.launch {
            val result = syncRepository.enqueuePromoteIdentifier(
                // The goat id partitions ordering: two promotes of the SAME goat drain strictly
                // oldest-first, so they can never race.
                groupKey = goatId,
                idempotencyKey = promoteKey.current(),
                permanentIdentifier = rfid,
                rowVersion = rowVersion,
                secondaryIdentifier = rfid2.ifEmpty { null },
            )
            when (result) {
                is AppResult.Ok -> {
                    outboxItemId.value = result.value
                    observeOutboxItem(result.value)
                    // Remove any legacy cached row the moment the promote is durable so a restored
                    // old route cannot promote the same goat while its first write drains.
                    repo.forgetPromoted(goatId)
                    analytics.track(AnalyticsEvents.COUNTS_RFID_PROMOTE_SUBMITTED)
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "rfid promote enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.COUNTS_WRITE_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.KIND to "rfid_promote",
                            AnalyticsEvents.Params.REASON to result.message,
                        ),
                    )
                    _state.update {
                        it.copy(result = CountsWriteResultUi(CountsWriteStatus.FAILED, result.message), canSubmit = true)
                    }
                }
            }
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
                        // A terminal server rejection (e.g. a stale row_version) drops the persisted key
                        // so a corrected resubmission mints a fresh one; a queued/synced write is done.
                        if (writeResult.isCorrectable) promoteKey.invalidate()
                        // Once the promote is server-confirmed, clear the typed RFID(s) so the page does
                        // not keep showing the entered values after the goat has been retagged. The goat
                        // Birth will refresh its workflow state, so there is nothing to re-submit.
                        val syncedDone = writeResult.status == CountsWriteStatus.SYNCED
                        it.copy(
                            result = writeResult,
                            rfidInput = if (syncedDone) "" else it.rfidInput,
                            rfid2Input = if (syncedDone) "" else it.rfid2Input,
                            canSubmit = writeResult.isCorrectable && it.rfidInput.isNotBlank(),
                        )
                    }
                }
        }
    }

    private fun TemporaryTaggedGoatDto.toUiState(current: RfidPromoteUiState): RfidPromoteUiState =
        current.copy(
            loading = false,
            notFound = false,
            displayId = displayId,
            temporaryIdentifier = temporaryIdentifier,
            locationDisplay = locationDisplay,
            // A committed write already disables the button; a fresh open enables it once an RFID is typed.
            canSubmit = current.rfidInput.isNotBlank() && !current.result.isCommitted,
        )

    private companion object {
        const val ARG_GOAT_ID = "goat_id"
        const val ARG_DISPLAY_ID = "display_id"
        const val ARG_TEMPORARY_IDENTIFIER = "temporary_identifier"
        const val ARG_LOCATION_DISPLAY = "location_display"
        const val ARG_ROW_VERSION = "row_version"
        const val KEY_PROMOTE_IDEMPOTENCY = "rfidPromote.promoteKey"
        const val KEY_OUTBOX_ITEM_ID = "rfidPromote.outboxItemId"
        const val QUEUED_MESSAGE = "Saved on this phone. The permanent RFID will sync automatically."
        const val SYNCED_MESSAGE = "Done. The goat now carries its permanent RFID."
    }
}
