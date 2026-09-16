package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import sg.mesha.goatos.capture.PhotoCaptureContext
import sg.mesha.goatos.capture.PhotoCaptureSource
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.EvidenceSlot
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofIdentity
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.sync.FeedSlotProofSourcePayload
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.FeedSopCardDto
import sg.mesha.goatos.feature.feed.FeedDistributionProofStatus
import sg.mesha.goatos.feature.feed.FeedDistributionQuestionUi
import sg.mesha.goatos.feature.feed.FeedDistributionSlotUi
import sg.mesha.goatos.feature.feed.FeedSlotCaptureKind
import sg.mesha.goatos.feature.feed.FeedSopCardUi
import java.util.UUID

/**
 * FEED SOP (maintainer decision 2026-09-16): the capture-slot machinery ONE feed stage screen
 * needs to follow its authored card -- shared by the packing, wastage and transport ViewModels,
 * which each own a single operator's proof group. (The distribution ViewModel carries the same
 * shape inline because it also adopts teammates' proofs by server id.)
 *
 * Owns: the card as rendered ([state]), one durable draft per slot key in the SavedStateHandle
 * (outbox row id + proof row id, so a card with N slots survives process death), the capture path
 * for a video, a photo or an `either` slot, Room re-hydration of this group's proof rows, the
 * upload-status observer per slot, the answers, and the submit's `{slot key: source}` map.
 *
 * Every slot is an independent offline-first proof write on the SAME outbox group as the stage's
 * completion, so the completion drains behind every upload. A slot is enabled by ITS OWN state
 * only -- the caller adds the stage lock (submitted / decided elsewhere).
 */
internal class FeedSopSlotController(
    private val scope: CoroutineScope,
    private val savedStateHandle: SavedStateHandle,
    private val syncRepository: SyncRepository,
    private val proofCaptureSource: ProofCaptureSource,
    private val photoCaptureSource: PhotoCaptureSource,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    /** Names the stage in saved-state keys and crash notes, e.g. `feedPacking`. */
    private val stageKey: String,
    /** The proof group every upload and the completion share. */
    private val groupKey: String,
    private val shedId: String,
    /** The storage identity for this group's evidence slots (field key is the slot key). */
    private val evidenceIdentity: ProofIdentity,
    private val proofPolicy: (captureSource: String) -> sg.mesha.goatos.core.data.forms.ProofPolicy,
    private val caption: (slotTitle: String) -> String,
    private val videoContext: (slotTitle: String) -> ProofCaptureContext,
    private val photoContext: (slot: FeedDistributionSlotUi) -> PhotoCaptureContext,
    /** Analytics: the stage's `*_captured` / `*_failure` / `*_upload_synced` events and base props. */
    private val events: Events,
    private val baseProps: (slotKey: String?, action: String) -> Map<String, String>,
    /** The screen may be locked (submitted / decided); a capture tap is then a no-op. */
    private val locked: () -> Boolean,
    private val onChanged: () -> Unit,
    /**
     * The shared Room-backed capture-draft store (see its kdoc for why SavedStateHandle alone lost
     * a clip): each slot's outbox row is kept under step = slot key, and the answers under their
     * question ids. [legacySteps] maps a step name an older build wrote (`video`) to the seeded
     * slot key it meant, so a clip recorded before this build still fills its slot.
     */
    private val durableDrafts: sg.mesha.goatos.core.data.CaptureDraftRepository? = null,
    private val durableFlowKey: String = "",
    private val legacySteps: Map<String, String> = emptyMap(),
) {
    internal class Events(
        val captureTapped: String,
        val captured: String,
        val uploadSynced: String,
        val failure: String,
        val reuploadTapped: String,
        val cardApplied: String,
    )

    private inner class SlotDraft(val key: String) {
        val proofItemId = DraftOutboxItemId(savedStateHandle, "$stageKey.slot.$key.proofItemId")
        val proofRowId = DraftOutboxItemId(savedStateHandle, "$stageKey.slot.$key.proofRowId")
        var statusJob: Job? = null
        /** Outbox rows whose terminal upload outcome was already reported. */
        val terminalTracked = mutableSetOf<String>() // mobile-guard:ignore: bounded to this slot's (few) re-takes
    }

    private val drafts = mutableMapOf<String, SlotDraft>() // mobile-guard:ignore: bounded by the card's slot count (<= 12 per SOP) plus rows already in Room for this group
    private fun draft(key: String) = drafts.getOrPut(key) { SlotDraft(key) }

    private var cardRank = CARD_RANK_SEEDED
    private var cardVersion = -1
    private var lastProofRows: List<ProofCaptureRow> = emptyList()

    private val _state = MutableStateFlow(FeedSopCardUi())
    val state: StateFlow<FeedSopCardUi> = _state.asStateFlow()

    /** Starts the Room observers (durable draft, then this group's proof rows); call once from init. */
    fun start() {
        scope.launch {
            durableDrafts?.find(durableFlowKey, groupKey)?.let { saved ->
                saved.proofs.forEach { (step, outboxId) ->
                    val key = legacySteps[step] ?: step
                    val d = draft(key)
                    if (d.proofItemId.value == null && outboxId.isNotBlank()) d.proofItemId.value = outboxId
                }
                if (saved.answers.isNotEmpty()) _state.update { it.copy(answers = saved.answers + it.answers) }
                _state.value.slots.forEach { slot ->
                    val d = draft(slot.slotKey)
                    d.proofItemId.value?.let {
                        if (!slot.captured) updateSlot(slot.slotKey) { s -> s.copy(captured = true, status = FeedDistributionProofStatus.QUEUED, previewIdentity = previewIdentity(slot.slotKey)) }
                        if (d.statusJob == null) observeProofItem(slot.slotKey, it)
                    }
                }
                onChanged()
            }
        }
        scope.launch {
            // No partitionLabel filter: the group key already carries the pen (see
            // FeedCaptureGroupKey.kt) and capture() writes partitionKey "whole".
            proofCaptureRepository.observeProofs(groupKey).collect { rows ->
                lastProofRows = rows
                hydrateSlotsFromRows(rows)
                onChanged()
            }
        }
    }

    /**
     * Re-shapes the slot list to [card], keeping each slot's capture state by key. Weaker sources
     * (seeded < Room-cached sheet card < live read) never overwrite a stronger one.
     */
    fun applyCard(card: FeedSopCardDto, rank: Int, source: String) {
        if (rank < cardRank || card.proofs.isEmpty()) return
        val changed = rank != cardRank || card.version != cardVersion || card.proofs.map { it.key } != _state.value.slots.map { it.slotKey }
        cardRank = rank
        cardVersion = card.version
        _state.update { st ->
            val byKey = st.slots.associateBy { it.slotKey }
            st.copy(
                instruction = card.instruction,
                slots = card.proofs.map { p ->
                    val prev = byKey[p.key]
                    val kind = p.kind.ifBlank { FeedSlotCaptureKind.VIDEO }
                    (prev ?: FeedDistributionSlotUi(slotKey = p.key, title = p.title, capturedKind = if (kind == FeedSlotCaptureKind.PHOTO) kind else FeedSlotCaptureKind.VIDEO))
                        .copy(title = p.title.ifBlank { prev?.title ?: p.key }, hint = p.hint, captureKind = kind, required = p.required)
                },
                questions = card.questions.map { q ->
                    FeedDistributionQuestionUi(
                        id = q.id, kind = q.kind, title = q.title, hint = q.hint, required = q.required,
                        options = q.options.map { it.value to it.label }, allowOther = q.allowOther, unit = q.unit,
                        onlyIfQuestion = q.onlyIf?.questionId.orEmpty(), onlyIfValue = q.onlyIf?.value.orEmpty(),
                    )
                },
            )
        }
        // A slot the card just added may already hold work: a draft restored from the saved state,
        // this phone's own rows in Room, or an outbox row still uploading.
        _state.value.slots.forEach { slot ->
            val d = draft(slot.slotKey)
            if (d.proofItemId.value != null && !slot.captured) {
                updateSlot(slot.slotKey) { it.copy(captured = true, status = FeedDistributionProofStatus.QUEUED, previewIdentity = previewIdentity(slot.slotKey)) }
            }
        }
        hydrateSlotsFromRows(lastProofRows)
        _state.value.slots.forEach { slot ->
            val d = draft(slot.slotKey)
            d.proofItemId.value?.let { if (d.statusJob == null) observeProofItem(slot.slotKey, it) }
        }
        onChanged()
        if (changed) {
            analytics.track(
                events.cardApplied,
                baseProps(null, ACTION_CARD_APPLIED) + mapOf(
                    AnalyticsEvents.Params.SOURCE to source,
                    "sop_version" to card.version.toString(),
                    "slot_count" to card.proofs.size.toString(),
                    "question_count" to card.questions.size.toString(),
                ),
            )
        }
    }

    fun answer(questionId: String, value: String) {
        if (locked()) return
        _state.update { it.copy(answers = it.answers + (questionId to value)) }
        durableDrafts?.let { store -> scope.launch { store.putAnswers(durableFlowKey, groupKey, _state.value.answers) } }
        onChanged()
    }

    private fun mediumFor(slot: FeedDistributionSlotUi, requested: String?): String = when (slot.captureKind) {
        FeedSlotCaptureKind.PHOTO -> FeedSlotCaptureKind.PHOTO
        FeedSlotCaptureKind.VIDEO -> FeedSlotCaptureKind.VIDEO
        else -> if (requested == FeedSlotCaptureKind.PHOTO) FeedSlotCaptureKind.PHOTO else FeedSlotCaptureKind.VIDEO
    }

    /**
     * ONE slot from the LIVE in-app camera as an offline-first proof write on the group. The gate
     * is the slot's OWN state plus the stage lock -- never a sibling slot. `captureReplacingLatest`
     * removes the previous take only after the new capture succeeds.
     */
    fun captureSlot(slotKey: String, requestedKind: String?) {
        val slot = _state.value.slot(slotKey) ?: return
        if (!slot.captureEnabled || locked() || shedId.isBlank()) return
        val medium = mediumFor(slot, requestedKind)
        val replacing = slot.captured
        val d = draft(slotKey)
        if (replacing) analytics.track(events.reuploadTapped, baseProps(slotKey, ACTION_RE_RECORD_PROOF))
        analytics.track(events.captureTapped, baseProps(slotKey, ACTION_RECORD_PROOF) + ("medium" to medium))
        updateSlot(slotKey) { it.copy(isCapturing = true, message = if (replacing) it.message else null, status = if (replacing) it.status else FeedDistributionProofStatus.QUEUED) }
        onChanged()
        scope.launch {
            var captureThrew = false
            val captured: Media? = try {
                if (medium == FeedSlotCaptureKind.PHOTO) {
                    photoCaptureSource.capturePhoto(photoContext(slot))
                        ?.let { Media(it.localUri, it.mimeType, it.capturedAtMs, it.capturedAtMs, it.captureSource) }
                } else {
                    proofCaptureSource.captureVideo(videoContext(slot.title))
                        ?.let { Media(it.localUri, it.mimeType, it.startedAtMs, it.endedAtMs, it.captureSource) }
                }
            } catch (error: Exception) {
                captureThrew = true
                crashReporter.recordException(error, "$stageKey $slotKey capture failed")
                trackCaptureFailure(slotKey, "camera_exception")
                null
            }
            if (captured == null) {
                if (!captureThrew) trackCaptureFailure(slotKey, "camera_returned_null")
                // A camera FAILURE is told to the operator whether or not a take already exists
                // (the old take stays); a cancel while replacing keeps the old message.
                updateSlot(slotKey) {
                    it.copy(
                        isCapturing = false,
                        status = if (replacing) it.status else FeedDistributionProofStatus.FAILED,
                        message = when {
                            captureThrew -> CAPTURE_FAILED
                            replacing -> it.message
                            else -> PROOF_FAILED
                        },
                    )
                }
                onChanged()
                return@launch
            }
            when (
                val result = proofCaptureRepository.captureReplacingLatest(
                    slot = EvidenceSlot(identity = evidenceIdentity, fieldKey = slotKey),
                    subject = ProofSubject.SHED,
                    subjectId = shedId,
                    localUri = captured.localUri,
                    mimeType = captured.mimeType,
                    caption = caption(slot.title),
                    scopeType = "shed",
                    scopeId = shedId,
                    capturedStartMs = captured.startMs,
                    capturedEndMs = captured.endMs,
                    capturedByPrincipalId = null,
                    proofPolicy = proofPolicy(captured.captureSource),
                    awaitUploadEnqueue = true,
                    uploadGroupKey = groupKey,
                )
            ) {
                is AppResult.Ok -> {
                    val proofOutboxId = result.value.outboxItemId
                    if (proofOutboxId.isNullOrBlank()) {
                        trackCaptureFailure(slotKey, "missing_upload_outbox")
                        updateSlot(slotKey) { it.copy(isCapturing = false, status = FeedDistributionProofStatus.FAILED, message = PROOF_FAILED) }
                        onChanged()
                        return@launch
                    }
                    d.proofItemId.value = proofOutboxId
                    d.proofRowId.value = result.value.id
                    durableDrafts?.putProof(durableFlowKey, groupKey, slotKey, proofOutboxId)
                    observeProofItem(slotKey, proofOutboxId)
                    analytics.track(
                        events.captured,
                        baseProps(slotKey, ACTION_CAPTURED) + mapOf(
                            "proof_id" to result.value.id,
                            "outbox_item_id" to proofOutboxId,
                            AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID to proofOutboxId,
                            "medium" to medium,
                        ),
                    )
                    updateSlot(slotKey) {
                        it.copy(
                            isCapturing = false, captured = true, capturedKind = medium,
                            previewPath = captured.localUri, previewIdentity = previewIdentity(slotKey),
                            status = FeedDistributionProofStatus.QUEUED, message = PROOF_QUEUED,
                        )
                    }
                    onChanged()
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "$stageKey $slotKey enqueue failed") }
                    analytics.track(events.failure, baseProps(slotKey, ACTION_CAPTURE_FAILED) + (AnalyticsEvents.Params.REASON to result.message))
                    updateSlot(slotKey) { it.copy(isCapturing = false, status = FeedDistributionProofStatus.FAILED, message = result.message) }
                    onChanged()
                }
            }
        }
    }

    private class Media(val localUri: String, val mimeType: String, val startMs: Long, val endMs: Long, val captureSource: String)

    /**
     * The submit's `{slot key: source}` in card order, or null when a compulsory slot is empty.
     * Optional slots ride along only when captured.
     */
    fun submitRefs(): Map<String, FeedSlotProofSourcePayload>? {
        val out = linkedMapOf<String, FeedSlotProofSourcePayload>() // mobile-guard:ignore: bounded by the card's slot count
        for (slot in _state.value.slots) {
            val own = draft(slot.slotKey).proofItemId.value
            when {
                !own.isNullOrBlank() -> out[slot.slotKey] = FeedSlotProofSourcePayload(outboxItemId = own)
                slot.required -> return null
            }
        }
        return out.takeIf { it.isNotEmpty() }
    }

    /** Re-tries every slot's proof upload (the manual Sync tap), before the caller drains the outbox. */
    suspend fun retryUploads() {
        _state.value.slots.forEach { slot ->
            drafts[slot.slotKey]?.proofRowId?.value?.takeIf { it.isNotBlank() }?.let { proofCaptureRepository.retryUpload(groupKey, it) }
        }
    }

    /** This phone's outbox row for one slot (the seeded slot's legacy mirror on the submit). */
    fun outboxItemIdFor(slotKey: String): String? = drafts[slotKey]?.proofItemId?.value
    fun proofRowIdFor(slotKey: String): String? = drafts[slotKey]?.proofRowId?.value

    /** The answers as the backend's wire shape: only applicable questions, typed by kind. */
    fun answersJson(): JsonObject = buildJsonObject {
        val st = _state.value
        st.questions.forEach { q ->
            if (!st.appliesTo(q)) return@forEach
            val a = st.answers[q.id].orEmpty().trim()
            when (q.kind) {
                "multi" -> a.split(",").map { it.trim() }.filter { it.isNotBlank() }
                    .takeIf { it.isNotEmpty() }?.let { put(q.id, JsonArray(it.map { v -> JsonPrimitive(v) })) }
                "number" -> a.toDoubleOrNull()?.let { put(q.id, JsonPrimitive(it)) }
                "choice" -> if (a.isNotBlank()) {
                    put(q.id, JsonPrimitive(a))
                    val other = st.answers[q.id + "_other"].orEmpty().trim()
                    if (a == "other" && q.allowOther && other.isNotBlank()) put("${q.id}_other", JsonPrimitive(other))
                }
                else -> if (a.isNotBlank()) put(q.id, JsonPrimitive(a))
            }
        }
    }

    /** A stable digest of the proof set + answers for the stage's submit idempotency key. */
    fun submitDigest(refs: Map<String, FeedSlotProofSourcePayload>, answers: JsonObject): String {
        val canonical = buildList {
            refs.forEach { (key, ref) -> add("$key=${ref.outboxItemId ?: ref.proofRef.orEmpty()}") }
            if (answers.isNotEmpty()) add("answers=$answers")
        }.joinToString("|")
        return UUID.nameUUIDFromBytes(canonical.toByteArray()).toString()
    }

    private fun observeProofItem(slotKey: String, itemId: String) {
        val d = draft(slotKey)
        d.statusJob?.cancel()
        d.statusJob = scope.launch {
            syncRepository.observeItem(itemId).filterNotNull().distinctUntilChanged().collect { item -> updateProofStatus(slotKey, item) }
        }
    }

    private fun hydrateSlotsFromRows(rows: List<ProofCaptureRow>) {
        if (rows.isEmpty()) return
        _state.value.slots.forEach { slot ->
            val row = rows.filter { it.fieldKey == slot.slotKey && it.syncStatus != CaptureSyncStatus.FAILED }.maxByOrNull { it.capturedAtMs } ?: return@forEach
            val d = draft(slot.slotKey)
            row.outboxItemId?.takeIf { it.isNotBlank() }?.let { outboxId ->
                if (d.proofItemId.value != outboxId || d.statusJob == null) {
                    if (d.proofItemId.value != outboxId) {
                        durableDrafts?.let { store -> scope.launch { store.putProof(durableFlowKey, groupKey, slot.slotKey, outboxId) } }
                    }
                    d.proofItemId.value = outboxId
                    observeProofItem(slot.slotKey, outboxId)
                }
            }
            d.proofRowId.value = row.id
            val status = row.toProofStatus()
            val preview = row.previewUri()
            updateSlot(slot.slotKey) {
                it.copy(
                    captured = true,
                    capturedKind = if (row.mimeType.startsWith("image/")) FeedSlotCaptureKind.PHOTO else FeedSlotCaptureKind.VIDEO,
                    previewPath = preview ?: it.previewPath,
                    previewIdentity = previewIdentity(slot.slotKey),
                    status = status,
                    message = row.toProofMessage(status, PROOF_QUEUED, PROOF_UPLOADING, PROOF_SYNCED, PROOF_FAILED),
                )
            }
        }
    }

    private fun updateProofStatus(slotKey: String, item: SyncQueueItem) {
        val proofStatus = when (item.status) {
            SyncItemStatus.QUEUED -> FeedDistributionProofStatus.QUEUED
            SyncItemStatus.IN_FLIGHT -> FeedDistributionProofStatus.UPLOADING
            SyncItemStatus.SUCCEEDED -> FeedDistributionProofStatus.SYNCED
            SyncItemStatus.FAILED -> FeedDistributionProofStatus.FAILED
        }
        val message = when (proofStatus) {
            FeedDistributionProofStatus.QUEUED -> PROOF_QUEUED
            FeedDistributionProofStatus.UPLOADING -> PROOF_UPLOADING
            FeedDistributionProofStatus.SYNCED -> PROOF_SYNCED
            FeedDistributionProofStatus.FAILED -> item.lastError ?: PROOF_FAILED
            FeedDistributionProofStatus.EMPTY -> null
        }
        updateSlot(slotKey) {
            it.copy(
                captured = it.captured || item.localFilePath != null,
                previewPath = item.localFilePath ?: it.previewPath,
                previewIdentity = previewIdentity(slotKey),
                status = proofStatus,
                message = message,
            )
        }
        val d = draft(slotKey)
        // Terminal upload outcome, once per outbox row: SUCCESS on the stage's upload-synced event,
        // FAILURE on the same event with outcome=failure (the historical shape) plus the failure event.
        val outcome = when (item.status) { SyncItemStatus.SUCCEEDED -> "success"; SyncItemStatus.FAILED -> "failure"; else -> null }
        if (outcome != null && d.terminalTracked.add(item.id)) {
            analytics.track(
                events.uploadSynced,
                baseProps(slotKey, ACTION_PROOF_UPLOAD_SYNC) + buildMap {
                    put(AnalyticsEvents.Params.OUTCOME, outcome)
                    put(AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID, item.id)
                    put("proof_outbox_item_id", item.id)
                    d.proofRowId.value?.takeIf { it.isNotBlank() }?.let { put("local_proof_row_id", it) }
                    if (outcome == "failure") put(AnalyticsEvents.Params.REASON, item.lastError?.takeIf { it.isNotBlank() } ?: if (item.conflict) "conflict" else "attempts_exhausted")
                },
            )
            if (outcome == "failure") {
                analytics.track(
                    events.failure,
                    baseProps(slotKey, ACTION_UPLOAD_FAILED) + mapOf(
                        AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID to item.id,
                        AnalyticsEvents.Params.REASON to (item.lastError ?: "").take(96),
                    ),
                )
            }
        }
        onChanged()
    }

    private fun trackCaptureFailure(slotKey: String, reason: String) {
        analytics.track(events.failure, baseProps(slotKey, ACTION_CAPTURE_FAILED) + (AnalyticsEvents.Params.REASON to reason))
    }

    private fun previewIdentity(slotKey: String): String {
        val d = draft(slotKey)
        return d.proofItemId.value ?: d.proofRowId.value ?: "$stageKey:$slotKey"
    }

    private inline fun updateSlot(slotKey: String, crossinline transform: (FeedDistributionSlotUi) -> FeedDistributionSlotUi) {
        _state.update { st ->
            if (st.slots.none { it.slotKey == slotKey }) st
            else st.copy(slots = st.slots.map { if (it.slotKey == slotKey) transform(it) else it })
        }
    }

    companion object {
        const val CARD_RANK_SEEDED = 0
        const val CARD_RANK_CACHED = 1
        const val CARD_RANK_LIVE = 2

        private const val ACTION_CARD_APPLIED = "card_applied"
        private const val ACTION_RECORD_PROOF = "record_proof"
        private const val ACTION_RE_RECORD_PROOF = "re_record_proof"
        private const val ACTION_CAPTURED = "captured"
        private const val ACTION_CAPTURE_FAILED = "capture_failed"
        private const val ACTION_PROOF_UPLOAD_SYNC = "proof_upload_sync"
        private const val ACTION_UPLOAD_FAILED = "upload_failed"
        private const val PROOF_QUEUED = "Proof saved on this phone. It will upload automatically."
        private const val PROOF_UPLOADING = "Proof upload is in progress."
        private const val PROOF_SYNCED = "Proof is ready."
        private const val PROOF_FAILED = "Couldn't save that proof. Please capture it again."
        private const val CAPTURE_FAILED = "Video capture failed. Try again."
    }
}
