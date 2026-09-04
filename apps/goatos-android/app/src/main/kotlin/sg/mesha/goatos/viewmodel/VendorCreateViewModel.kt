package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import sg.mesha.goatos.capture.AudioCaptureContext
import sg.mesha.goatos.capture.AudioCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsVendors
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.VendorsRepository
import sg.mesha.goatos.core.data.capture.EvidenceSlot
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofFlow
import sg.mesha.goatos.core.data.capture.ProofIdentity
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.vendorCreateGroupKey
import sg.mesha.goatos.core.network.dto.VendorCatalogDto
import sg.mesha.goatos.core.network.dto.VendorWriteDto
import sg.mesha.goatos.feature.vendors.VendorCreateEvent
import sg.mesha.goatos.feature.vendors.VendorCreateUiState
import sg.mesha.goatos.feature.vendors.VendorField
import sg.mesha.goatos.feature.vendors.VendorsOptionUi
import sg.mesha.goatos.feature.vendors.VendorsWriteStatus
import sg.mesha.goatos.feature.vendors.VoiceNoteSlotState
import java.util.UUID
import javax.inject.Inject

/**
 * The add-vendor wizard (module vendors, maintainer decision 2026-09-03).
 *
 * The write is OFFLINE-FIRST: a vendor is durable in the outbox the moment Save is tapped
 * (QUEUED), and the sync engine records it when the network allows. The voice note is a proof:
 * it is captured into the proof outbox on the same per-vendor group so it uploads BEFORE the
 * vendor write that references it, exactly the toxin step shape.
 *
 * The client id is minted ONCE per form and kept in SavedStateHandle, so a double tap, a lost
 * response or a process death all replay the SAME vendor rather than recording a second one.
 */
@HiltViewModel
class VendorCreateViewModel @Inject constructor(
    private val savedStateHandle: SavedStateHandle,
    private val repository: VendorsRepository,
    private val syncRepository: SyncRepository,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val audioCaptureSource: AudioCaptureSource,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private data class Local(
        val step: Int = 0,
        val values: Map<VendorField, String> = mapOf(VendorField.STATUS to "active"),
        val fieldErrors: Map<VendorField, String> = emptyMap(),
        val voiceNote: VoiceNoteSlotState = VoiceNoteSlotState.EMPTY,
        val voiceNoteLength: String = "",
        val voiceNoteOutboxItemId: String = "",
        val writeStatus: VendorsWriteStatus = VendorsWriteStatus.IDLE,
        val writeMessage: String = "",
        val closeAfterSave: Boolean = false,
        val submitInFlight: Boolean = false,
        val message: String? = null,
    )

    private val local = MutableStateFlow(Local())

    private val clientId: String
        get() = savedStateHandle.get<String>(KEY_CLIENT_ID) ?: UUID.randomUUID().toString().also { savedStateHandle[KEY_CLIENT_ID] = it }

    init {
        analytics.track(AnalyticsEventsVendors.VENDORS_ADD_OPENED)
        viewModelScope.launch { repository.refreshCatalog() }
    }

    val state: StateFlow<VendorCreateUiState> = combine(local, repository.observeCatalog()) { l, catalog ->
        val c = catalog ?: VendorCatalogDto()
        VendorCreateUiState(
            step = l.step,
            stepCount = STEP_COUNT,
            values = l.values,
            recordTypes = c.recordTypes.options(),
            states = c.states.options(),
            statuses = c.statuses.options(),
            capacityUnits = c.capacityUnits.options(),
            supplyFrequencies = c.supplyFrequencies.options(),
            feeds = c.feeds.options(),
            breeds = c.breeds.options(),
            fieldErrors = l.fieldErrors,
            contextLine = contextLine(l.values, c),
            voiceNote = l.voiceNote,
            voiceNoteLength = l.voiceNoteLength,
            writeStatus = l.writeStatus,
            writeMessage = l.writeMessage,
            closeAfterSave = l.closeAfterSave,
            submitInFlight = l.submitInFlight,
            message = l.message,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), VendorCreateUiState())

    fun onEvent(event: VendorCreateEvent) {
        // A queued or accepted write is read-only: the last step stays on screen for the banner
        // and closes by itself, so an edit or a second submit in that window has nothing to land on.
        val locked = local.value.writeStatus == VendorsWriteStatus.QUEUED || local.value.writeStatus == VendorsWriteStatus.SYNCED
        if (locked && event !is VendorCreateEvent.Back && event !is VendorCreateEvent.RecordAnother && event !is VendorCreateEvent.DismissMessage) return
        when (event) {
            is VendorCreateEvent.FieldChanged -> local.update {
                it.copy(values = it.values + (event.field to event.value), fieldErrors = it.fieldErrors - event.field)
            }
            VendorCreateEvent.Next -> next()
            VendorCreateEvent.Previous -> local.update { it.copy(step = (it.step - 1).coerceAtLeast(0)) }
            VendorCreateEvent.RecordVoiceNote -> recordVoiceNote()
            VendorCreateEvent.RemoveVoiceNote -> local.update { it.copy(voiceNote = VoiceNoteSlotState.EMPTY, voiceNoteLength = "", voiceNoteOutboxItemId = "") }
            VendorCreateEvent.Submit -> submit()
            VendorCreateEvent.RecordAnother -> reset()
            VendorCreateEvent.DismissMessage -> local.update { it.copy(message = null) }
            VendorCreateEvent.Back -> Unit
        }
    }

    private fun next() {
        val errors = validate(local.value.step, local.value.values)
        if (errors.isNotEmpty()) {
            local.update { it.copy(fieldErrors = errors) }
            return
        }
        local.update { it.copy(step = (it.step + 1).coerceAtMost(STEP_COUNT - 1), fieldErrors = emptyMap()) }
    }

    private fun submit() {
        val current = local.value
        val errors = (0 until STEP_COUNT).fold(emptyMap<VendorField, String>()) { acc, step -> acc + validate(step, current.values) }
        if (errors.isNotEmpty()) {
            // Jump back to the first step that still has a problem, with its refusal shown.
            val firstStep = (0 until STEP_COUNT).first { step -> validate(step, current.values).isNotEmpty() }
            local.update { it.copy(step = firstStep, fieldErrors = errors) }
            return
        }
        viewModelScope.launch {
            local.update { it.copy(submitInFlight = true, message = null) }
            val result = syncRepository.enqueueVendorCreate(
                clientId = clientId,
                request = current.values.toWrite(),
                voiceNoteOutboxItemId = current.voiceNoteOutboxItemId,
            )
            when (result) {
                is AppResult.Ok -> {
                    analytics.track(AnalyticsEventsVendors.VENDORS_VENDOR_QUEUED)
                    local.update { it.copy(submitInFlight = false, writeStatus = VendorsWriteStatus.QUEUED, writeMessage = MESSAGE_SAVING) }
                    followWrite(result.value)
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "vendor create enqueue failed") }
                    analytics.track(AnalyticsEventsVendors.VENDORS_FAILURE, mapOf(AnalyticsEvents.Params.REASON to result.message.take(MAX_REASON_CHARS)))
                    local.update { it.copy(submitInFlight = false, writeStatus = VendorsWriteStatus.FAILED, writeMessage = MESSAGE_NOT_SAVED) }
                }
            }
        }
    }


    /** Upgrades the banner as the queued row moves: saved, still unsent (offline wording), or rejected. */
    private fun followWrite(itemId: String) {
        viewModelScope.launch {
            syncRepository.followQueuedWrite(itemId).collect { outcome ->
                local.update {
                    when (outcome) {
                        QueuedWriteOutcome.Saved -> it.copy(writeStatus = VendorsWriteStatus.SYNCED, writeMessage = MESSAGE_SAVED, closeAfterSave = true)
                        QueuedWriteOutcome.StillQueued -> it.copy(writeStatus = VendorsWriteStatus.QUEUED, writeMessage = MESSAGE_QUEUED, closeAfterSave = true)
                        is QueuedWriteOutcome.Rejected -> it.copy(writeStatus = VendorsWriteStatus.FAILED, writeMessage = MESSAGE_NOT_SAVED)
                    }
                }
            }
        }
    }
    private fun reset() {
        savedStateHandle[KEY_CLIENT_ID] = UUID.randomUUID().toString()
        local.value = Local()
    }

    private fun recordVoiceNote() {
        viewModelScope.launch {
            val captured = audioCaptureSource.captureAudio(AudioCaptureContext()) ?: return@launch
            // Backing out of the screen (which cancels this scope) can never orphan a note the
            // operator actually spoke.
            withContext(NonCancellable) {
                local.update { it.copy(voiceNote = VoiceNoteSlotState.WORKING) }
                val outboxId = captureNote(captured.localUri, captured.mimeType, captured.startedAtMs, captured.endedAtMs, captured.captureSource)
                if (outboxId == null) {
                    local.update { it.copy(voiceNote = VoiceNoteSlotState.FAILED) }
                } else {
                    analytics.track(AnalyticsEventsVendors.VENDORS_VOICE_NOTE_CAPTURED)
                    local.update {
                        it.copy(
                            voiceNote = VoiceNoteSlotState.RECORDED,
                            voiceNoteLength = formatLength(captured.endedAtMs - captured.startedAtMs),
                            voiceNoteOutboxItemId = outboxId,
                        )
                    }
                }
            }
        }
    }

    /** Writes the durable proof row and returns the PROOF_UPLOAD outbox row id the vendor write references. */
    private suspend fun captureNote(localUri: String, mimeType: String, startMs: Long, endMs: Long, captureSource: String): String? {
        val id = clientId
        val slot = EvidenceSlot(
            identity = ProofIdentity(flow = ProofFlow.VENDOR_VOICE_NOTE, taskId = id, subjectKey = FIELD_VOICE_NOTE),
            fieldKey = FIELD_VOICE_NOTE,
        )
        val result = proofCaptureRepository.captureReplacingLatest(
            slot = slot,
            subject = ProofSubject.OTHER,
            // The proof platform requires a UUID subject/scope; the vendor being recorded has no
            // server id yet, so the form's own client id is the subject.
            subjectId = id,
            localUri = localUri,
            mimeType = mimeType,
            caption = null,
            scopeType = SCOPE_TYPE_TASK,
            scopeId = id,
            capturedStartMs = startMs,
            capturedEndMs = endMs,
            capturedByPrincipalId = null,
            proofPolicy = vendorVoiceNotePolicy(captureSource),
            awaitUploadEnqueue = true,
            uploadGroupKey = vendorCreateGroupKey(id),
        )
        when (result) {
            is AppResult.Err -> {
                result.cause?.let { crashReporter.recordException(it, "vendor voice note capture write failed") }
                analytics.track(AnalyticsEventsVendors.VENDORS_FAILURE, mapOf(AnalyticsEvents.Params.REASON to result.message.take(MAX_REASON_CHARS)))
                return null
            }
            is AppResult.Ok -> Unit
        }
        var outboxId = result.value.outboxItemId
        var waited = 0L
        while (outboxId.isNullOrBlank() && waited < PROOF_ROW_SETTLE_MAX_MS) {
            delay(PROOF_ROW_SETTLE_STEP_MS)
            waited += PROOF_ROW_SETTLE_STEP_MS
            outboxId = proofCaptureRepository.observeProofs(id).first().firstOrNull { it.id == result.value.id }?.outboxItemId
        }
        if (outboxId.isNullOrBlank()) {
            crashReporter.recordException(IllegalStateException("vendor voice note ${result.value.id} has no upload row after ${waited}ms"), "vendor voice note never enqueued its upload")
            return null
        }
        return outboxId
    }

    private fun validate(step: Int, v: Map<VendorField, String>): Map<VendorField, String> {
        val errors = mutableMapOf<VendorField, String>() // mobile-guard:ignore: per-call validation result, at most one entry per form field, returned and dropped
        fun blank(f: VendorField) = v[f].isNullOrBlank()
        when (step) {
            0 -> {
                if (blank(VendorField.BUSINESS_NAME)) errors[VendorField.BUSINESS_NAME] = REQUIRED
                if (blank(VendorField.RECORD_TYPE)) errors[VendorField.RECORD_TYPE] = REQUIRED
                if (blank(VendorField.CONTACT_PERSON)) errors[VendorField.CONTACT_PERSON] = REQUIRED
                if (blank(VendorField.PHONE)) errors[VendorField.PHONE] = REQUIRED
            }
            1 -> {
                if (blank(VendorField.STATE)) errors[VendorField.STATE] = REQUIRED
                if (blank(VendorField.CITY)) errors[VendorField.CITY] = REQUIRED
            }
            else -> {
                val qty = v[VendorField.CAPACITY_QUANTITY].orEmpty().trim()
                // Capacity is optional in every part (maintainer instruction 2026-09-03); only a
                // typed quantity is shape-checked.
                if (qty.isNotBlank() && (qty.toDoubleOrNull() == null || qty.toDouble() <= 0.0)) errors[VendorField.CAPACITY_QUANTITY] = MORE_THAN_ZERO
                val price = v[VendorField.PRICE_PER_GOAT].orEmpty().trim()
                if (price.isNotBlank() && !Regex("""^\d{1,10}(\.\d{1,2})?$""").matches(price)) errors[VendorField.PRICE_PER_GOAT] = AMOUNT
                val eta = v[VendorField.ETA_DAYS].orEmpty().trim()
                if (eta.isNotBlank() && (eta.toIntOrNull() == null || eta.toInt() < 0)) errors[VendorField.ETA_DAYS] = WHOLE_DAYS
            }
        }
        return errors
    }

    private fun contextLine(v: Map<VendorField, String>, c: VendorCatalogDto): String {
        val type = c.recordTypes.firstOrNull { it.value == v[VendorField.RECORD_TYPE] }?.label ?: v[VendorField.RECORD_TYPE]
        val state = c.states.firstOrNull { it.value == v[VendorField.STATE] }?.label ?: v[VendorField.STATE]
        return dotJoin(v[VendorField.BUSINESS_NAME], type, dotJoin(v[VendorField.CITY], state))
    }

    private fun Map<VendorField, String>.toWrite(): VendorWriteDto = VendorWriteDto(
        recordType = get(VendorField.RECORD_TYPE).orEmpty().trim(),
        businessName = get(VendorField.BUSINESS_NAME).orEmpty().trim(),
        contactPersonName = get(VendorField.CONTACT_PERSON).orEmpty().trim(),
        phoneNumber = get(VendorField.PHONE).orEmpty().trim(),
        breed = get(VendorField.BREED).orEmpty(),
        feed = get(VendorField.FEED).orEmpty(),
        status = get(VendorField.STATUS).orEmpty().ifBlank { "active" },
        pricePerGoat = get(VendorField.PRICE_PER_GOAT).orEmpty().trim().ifBlank { null },
        etaAfterOrderDays = get(VendorField.ETA_DAYS).orEmpty().trim().toIntOrNull(),
        state = get(VendorField.STATE).orEmpty(),
        city = get(VendorField.CITY).orEmpty().trim(),
        comments = get(VendorField.NOTE).orEmpty().trim(),
        capacityQuantity = get(VendorField.CAPACITY_QUANTITY).orEmpty().trim().ifBlank { null },
        capacityUnit = get(VendorField.CAPACITY_UNIT).orEmpty(),
        supplyFrequency = get(VendorField.SUPPLY_FREQUENCY).orEmpty(),
    )

    private companion object {
        const val KEY_CLIENT_ID = "vendor_create_client_id"
        const val STEP_COUNT = 3
        const val FIELD_VOICE_NOTE = "voice_note"
        const val SCOPE_TYPE_TASK = "task"
        const val MAX_REASON_CHARS = 120
        const val PROOF_ROW_SETTLE_MAX_MS = 5_000L
        const val PROOF_ROW_SETTLE_STEP_MS = 100L
        const val REQUIRED = "Required"
        const val MORE_THAN_ZERO = "Must be more than zero"
        const val AMOUNT = "Enter an amount, up to two decimals"
        const val WHOLE_DAYS = "Whole days, zero or more"
        const val MESSAGE_SAVING = "Saving vendor…"
        const val MESSAGE_SAVED = "Vendor saved to the register."
        const val MESSAGE_QUEUED = "Saved on this phone. It will reach the register when the phone is online."
        const val MESSAGE_NOT_SAVED = "Could not save this vendor. Try again."
    }
}

internal fun List<sg.mesha.goatos.core.network.dto.VendorCatalogEntryDto>.options(): List<VendorsOptionUi> =
    filter { it.isActive }.map { VendorsOptionUi(it.value, it.label) }

internal fun formatLength(ms: Long): String {
    val s = (ms / 1000L).coerceAtLeast(0L)
    return "%d:%02d".format(s / 60L, s % 60L)
}

internal fun vendorVoiceNotePolicy(captureSource: String): ProofPolicy = ProofPolicy.Default.copy(
    types = listOf("audio"),
    proofMode = "vendor_voice_note",
    subjectScope = ProofSubject.OTHER.wireValue,
    expectedSubjects = listOf(ProofSubject.OTHER.wireValue),
    captureSource = captureSource,
    maximumCountPerField = 1,
    maximumCountPerSubject = 5,
)
