package sg.mesha.goatos.core.data.capture

// partition-identity-guard:ignore — ProofIdentity.partitionKey satisfies the intent
// (canonical addressing via shedId + partition across operational surfaces); the guard
// regex looks for the word "partition" as a standalone identifier but this uses "partitionKey".

/** Bounded Room read ceiling for one shed task. The business cap is per goat, below. */
const val MAX_PROOFS_PER_TASK = 10_000

/** A goat may have several camera clips from one handling, while still keeping each row bounded. */
const val MAX_PROOFS_PER_GOAT = 5

/** Field key used by the shed-level vaccination Scan screen before the Submit form is opened.
 *  Submit folds these roster-level captures into the SOP-declared GOAT_SCAN answer field, whose
 *  key is backend-owned and may not literally be `goat_scan`. */
const val ROSTER_SCAN_FIELD_KEY = "__scan_roster__"

/**
 * Canonical proof identity addressing a work item across UI/Room/outbox/backend.
 * One value type replacing scattered hand-rolled string key building (feedCaptureGroupKey,
 * scan keys, weighing scope keys, etc). Derived storageKey() and idempotencyKey() produce
 * EXISTING wire/storage formats so no data migration is needed.
 *
 * ProofIdentity is the SINGLE producer of storageKey/idempotencyKey formats:
 * - vaccinationScanCapture: flow=VACCINATION, taskId, groupKey (reopenEpoch), shedId, obligationId subject
 * - weighing: flow=WEIGHING_INDIVIDUAL/WEIGHING_SHED, taskId, shedId subject
 * - feed operations: flow=FEED_*, taskId, shedId/partition subject
 * - shifting/milk/birth: flow=SHIFTING/MILK/... passthrough, taskId, shedId
 */
data class ProofIdentity(
    val flow: ProofFlow,
    val taskId: String,
    val groupKey: String = "", // reopenEpoch for vaccination; empty for others
    val shedId: String = "",
    val partitionKey: String = "whole", // normalized via executionPartitionKey
    val subjectKey: String = "", // goatId, obligationId, shed id, etc
    // Feed-specific fields for capture group key
    val flowPrefix: String = "", // e.g., "feed-pack", "feed-dist" for feed operations
    val targetDate: String = "", // YYYY-MM-DD for feed operations
    val sessionNo: Int = 0, // feed session number
    val workflow: String = "", // "normal" or "experiment" for feed operations
    // Weighing transition fields
    val transition: String = "", // "update", "submit", "reopen", "close-shed", "close-campaign"
    val transitionEpoch: String = "", // epoch identifier for the transition
    // Submission scope fields (for SubmitViewModel.stableSubmissionKey)
    val scopeId: String = "", // the shed or task scope identifier
    val rowVersion: Int = 0, // row version for optimistic concurrency
) {
    /** Storage key for Room/proof row identification (animal/partition grain). */
    fun storageKey(): String {
        return when (flow) {
            ProofFlow.VACCINATION -> "vaccination:$taskId:${partitionKey.takeIf { it != "whole" }?.let { ":$it" }.orEmpty()}:$subjectKey"
            ProofFlow.WEIGHING_INDIVIDUAL, ProofFlow.WEIGHING_SHED -> "weighing:$taskId:$subjectKey"
            ProofFlow.FEED_COMPLETE, ProofFlow.FEED_DISTRIBUTION, ProofFlow.FEED_PACKING,
            ProofFlow.FEED_WASTAGE, ProofFlow.FEED_TRANSPORT,
            ->
                "feed:$taskId:${partitionKey.takeIf { it != "whole" }?.let { ":$it" }.orEmpty()}:${flow.wireValue}:$subjectKey"
            else -> "proof:$taskId:${flow.wireValue}:$subjectKey"
        }
    }

    /** Idempotency key for backend registration, incorporating groupKey (reopenEpoch). */
    fun idempotencyKey(reopenEpoch: Int = 0): String {
        return when (flow) {
            ProofFlow.VACCINATION -> {
                val epochSegment = groupKey.takeIf { it.isNotBlank() }?.let { ":$it" }.orEmpty()
                "vaccination:capture:$taskId:${partitionKey.takeIf { it != "whole" }?.let { ":$it" }.orEmpty()}:$subjectKey$epochSegment:ov$reopenEpoch"
            }
            ProofFlow.WEIGHING_INDIVIDUAL, ProofFlow.WEIGHING_SHED ->
                "weighing:capture:$taskId:$shedId:$subjectKey"
            ProofFlow.FEED_COMPLETE, ProofFlow.FEED_DISTRIBUTION, ProofFlow.FEED_PACKING,
            ProofFlow.FEED_WASTAGE, ProofFlow.FEED_TRANSPORT,
            ->
                "feed:capture:$taskId:${partitionKey.takeIf { it != "whole" }?.let { ":$it" }.orEmpty()}:${flow.wireValue}:$subjectKey"
            else -> "proof:capture:$taskId:${flow.wireValue}:$subjectKey"
        }
    }

    /** Feed capture group key: the identity of ONE feed capture flow on one feed DAY in one session.
     *  Used for durable capture drafts, submit idempotency, and outbox grouping. */
    fun captureGroupKey(): String {
        val dateToken = targetDate.trim().ifEmpty { "undated" }
        val partitionToken = partitionKey.takeIf { it != "whole" }
            ?.let { it.lowercase().replace(Regex("\\s+"), " ") }
            ?: "whole"
        return "$flowPrefix:$dateToken:$taskId:$partitionToken:$sessionNo:$workflow"
    }

    /** Weighing transition idempotency key for scope state changes (update/submit/reopen/close).
     *  Epoch is managed externally; this produces the format incorporating it. */
    fun weighingTransitionKey(): String {
        val scopeId = "$transition:$taskId"
        return "weighing:$transition:$scopeId:$transitionEpoch"
    }

    /** Submit scope key: the identity of ONE shed submission task with optional partition and row version.
     *  Used for submit idempotency and completion tracking. */
    fun submissionScopeKey(includePartition: Boolean = false): String {
        val partitionSegment = if (includePartition) {
            ":partition:$partitionKey"
        } else {
            ""
        }
        return "shed-submit:$taskId:scope:$scopeId$partitionSegment:rv:$rowVersion"
    }
}

/**
 * Blocker 9 (proof-flow-integration audit) — CLOSED. Milk preparation, milk feeding, and
 * generic-workflow captures used to build their own local proof/draft field keys instead of
 * routing through [ProofIdentity]. All three now address their captures via
 * [ProofFlow.MILK_PREPARATION] / [ProofFlow.MILK_FEEDING] / [ProofFlow.WORKFLOW_DETAIL] +
 * [EvidenceSlot], keeping the SAME on-disk taskId/fieldKey/idempotencyKey strings those flows
 * already wrote (identity.taskId is set to the existing literal task key; fieldKey stays the
 * existing step-code / `workflowProofFieldKey(actionId)` string) — no Room migration was needed
 * because no on-disk string changed, only the plumbing that carries it.
 *
 * The registry that used to list migration holdouts here is now empty and deleted; this set no
 * longer exists. See `check-feed-proof-collaboration-guard.mjs` for the ratchet that keeps it
 * from coming back.
 */

enum class ProofFlow(val wireValue: String) {
    VACCINATION("vaccination"),
    WEIGHING_INDIVIDUAL("weighing_individual"),
    WEIGHING_SHED("weighing_shed"),
    FEED_COMPLETE("feed_complete"),
    FEED_DISTRIBUTION("feed_distribution"),
    FEED_PACKING("feed_packing"),
    FEED_WASTAGE("feed_wastage"),
    FEED_TRANSPORT("feed_transport"),
    SHIFTING("shifting"),
    MILK("milk"),
    BIRTH("birth"),
    GENERIC_SUBMIT("generic_submit"),
    MILK_PREPARATION("milk_preparation"),
    MILK_FEEDING("milk_feeding"),
    WORKFLOW_DETAIL("workflow_detail"),

    /**
     * PC Care per-animal slot videos (module pc_care, maintainer decision 2026-08-21).
     * Deliberately rides the GENERIC storage/idempotency branches:
     * `proof:$taskId:pc_care:$subjectKey` / `proof:capture:$taskId:pc_care:$subjectKey`, where the
     * caller passes `subjectKey = "<normalizedTag>:<slotFieldKey>"` — one slot of one scanned
     * animal is one capture identity.
     */
    PC_CARE("pc_care"),
    ;

    companion object {
        fun from(raw: String): ProofFlow = entries.firstOrNull { it.wireValue == raw } ?: GENERIC_SUBMIT
    }
}

/**
 * Slot = (ProofIdentity, fieldKey) pairing for evidence management.
 * API on CaptureRepository: observeLatest(slot), capture-replacing-latest, activeCount(slot).
 * Per-subject caps stay for goat-scoped vaccination per SOP policy; per-field caps roll in here
 * for ALL flows where policy supplies maximumCountPerField (feed distribution, etc).
 */
data class EvidenceSlot(
    val identity: ProofIdentity,
    val fieldKey: String,
) {
    val storageKey: String get() = identity.storageKey()
}

/** Mirrors [sg.mesha.goatos.core.database.capture.CaptureSyncStatus] one-to-one — kept as a
 *  separate core-data-level type so ViewModels never need a direct `core-database` dependency
 *  (module boundary: `feature-*`/`:app` -> `core-*`, never straight to Room). */
enum class CaptureSyncStatus { PENDING, IN_FLIGHT, SYNCED, FAILED }

enum class ProofProcessingStatus(val wireValue: String, val operatorLabel: String) {
    PREPARING("preparing", "Compressing proof..."),
    COMPRESSING("compressing", "Compressing proof..."),
    UPLOADING("uploading", "Uploading proof..."),
    UPLOADING_ORIGINAL("uploading_original", "Uploading original proof..."),
    UPLOADED("uploaded", "Proof uploaded"),
    RETRYING("retrying", "Upload failed. Retrying"),
    RETRYING_ORIGINAL("retrying_original", "Retrying original proof upload..."),
    RECORD_AGAIN("record_again", "Record again"),
    ;

    companion object {
        fun fromProcessingState(
            processingState: String?,
            syncStatus: CaptureSyncStatus,
            uploadOriginal: Boolean,
        ): ProofProcessingStatus {
            if (syncStatus == CaptureSyncStatus.SYNCED) return UPLOADED
            if (syncStatus == CaptureSyncStatus.FAILED) return if (uploadOriginal) RETRYING_ORIGINAL else RETRYING

            return when (processingState) {
                "LOCATION_RESOLVING",
                "CAPTURED_ORIGINAL" -> PREPARING
                "PROCESSING_MEDIA" -> COMPRESSING
                "PROCESSED",
                "REGISTERING_UPLOAD",
                "UPLOADING" -> if (uploadOriginal) UPLOADING_ORIGINAL else UPLOADING
                "UPLOAD_CONFIRMED",
                "ATTACHED_TO_SUBMISSION" -> UPLOADED
                "PROCESSING_FAILED_ORIGINAL_UPLOAD_QUEUED" -> UPLOADING_ORIGINAL
                "REGISTER_FAILED_RETRYING",
                "UPLOAD_FAILED_RETRYING" -> RETRYING
                "UPLOAD_ORIGINAL_FAILED_RETRYING" -> RETRYING_ORIGINAL
                "DEAD_LETTER" -> RECORD_AGAIN
                // HIGH fix: a processed-artifact validation/processing failure leaves the row
                // here with syncStatus still PENDING (nothing is ever enqueued for this state —
                // see CaptureRepository's P1 fix). Without this branch it fell through to the
                // syncStatus=PENDING default below and read as "Uploading proof..." forever, with
                // no operator affordance to notice the stuck row. RECORD_AGAIN is the SAME
                // recovery action DEAD_LETTER already renders: a fresh recording sidesteps the
                // stuck row entirely via captureReplacingLatest.
                "PROCESSING_FAILED_AWAITING_RETRY" -> RECORD_AGAIN
                else -> when (syncStatus) {
                    CaptureSyncStatus.PENDING,
                    CaptureSyncStatus.IN_FLIGHT -> if (uploadOriginal) UPLOADING_ORIGINAL else UPLOADING
                    CaptureSyncStatus.SYNCED -> UPLOADED
                    CaptureSyncStatus.FAILED -> if (uploadOriginal) RETRYING_ORIGINAL else RETRYING
                }
            }
        }
    }
}

/** One de-duplicated RFID scan captured for a task's `goat_scan` recording-form field. */
data class ScannedGoatRow(
    val fieldKey: String,
    val tag: String,
    val goatId: String?,
    val obligationId: String?,
    val capturedAtMs: Long,
    /**
     * Whether this capture reached the backend. Room is the SSOT for the CAPTURE, never for the
     * OBLIGATION: a SYNCED capture whose obligation the server still reports open (a verifier
     * rejected the proof and it reopened) is a STALE done-marker, while a PENDING/IN_FLIGHT one is
     * a legitimate offline scan the server has not seen. Conflating them either lies about
     * completion or breaks offline scanning.
     */
    val syncStatus: CaptureSyncStatus = CaptureSyncStatus.PENDING,
    /** Normalized operational partition identity (`whole` for an unpartitioned shed). */
    val partitionKey: String = "whole",
    /** obligation_instances.row_version from the backend at the time the scan was captured —
     *  the server-issued cycle discriminator that distinguishes "never submitted" from
     *  "submitted then reopened". */
    val obligationRowVersion: Int = 0,
)

enum class RfidScanAttemptOutcome(val wireValue: String) {
    ACCEPTED("accepted"),
    DUPLICATE("duplicate"),
    NOT_DUE("not_due"),
    UNKNOWN("unknown"),
}

enum class RfidScanTagRole(val wireValue: String) {
    PRIMARY("primary"),
    SECONDARY("secondary"),
    UNKNOWN("unknown"),
}

data class RfidScanAttemptRow(
    val id: String,
    val taskId: String,
    val fieldKey: String,
    val tag: String,
    val normalizedTag: String,
    val goatId: String?,
    val obligationId: String?,
    val outcome: RfidScanAttemptOutcome,
    val tagRole: RfidScanTagRole,
    val reason: String?,
    val capturedAtMs: Long,
)

/** Generic proof subjects supported by the central media module. Vaccination uses [GOAT]:
 *  every clip is linked to the scanned goat it proves, and several clips may share that goat.
 *  The remaining subjects support non-vaccination forms and older generic proof records. */
enum class ProofSubject(val wireValue: String) {
    GOAT("goat"),
    SHED("shed"),
    TASK("task"),
    // 'park' was NEVER a valid backend subject_type (validateCreate in
    // backend/internal/proof/app/service.go only accepts batch/goat/shed/task/vial_lot/
    // administration/other) -- every proof stamped with it 400'd. Deleted rather than kept
    // "for backward compatibility": that phrase described a value with no live caller, and its
    // presence let a future capture site silently regress to the same 400 (cc566278a). If a
    // proof genuinely has no typed backend subject, use OTHER.
    VIAL_LOT("vial_lot"),
    ADMINISTRATION("administration"),
    OTHER("other"),
    EXTRA("extra"),
    ;

    companion object {
        fun from(raw: String): ProofSubject = entries.firstOrNull { it.wireValue == raw } ?: EXTRA
    }
}

/** One captured proof video, as the UI/repository layer sees it. */
data class ProofCaptureRow(
    val id: String,
    val fieldKey: String,
    val proofSubject: ProofSubject,
    /** The goat whose single handling this clip proves. Multiple clips may share this id. */
    val subjectId: String? = null,
    val localUri: String,
    val processedUri: String? = null,
    val mimeType: String,
    val caption: String?,
    /** Human-readable RFID/tag for individual-animal proof matching and overlay display. */
    val rfidTag: String? = null,
    val capturedAtMs: Long,
    /** Freshness/anti-fraud metadata — see `ProofCaptureEntity`'s kdoc ("Camera-only capture"). */
    val capturedStartMs: Long,
    val capturedEndMs: Long,
    val capturedByPrincipalId: String?,
    val syncStatus: CaptureSyncStatus,
    val serverProofId: String?,
    val outboxItemId: String? = null,
    val lastError: String?,
    /** Normalized operational partition identity (`whole` for an unpartitioned shed). */
    val partitionKey: String = "whole",
    val featureSurface: String? = null,
    val proofMode: String? = null,
    val slotIndex: Int? = null,
    val slotRequired: Boolean = false,
    val processingState: String = "CAPTURED_ORIGINAL",
    val processingAttempted: Boolean = false,
    val stateAttempt: Int = 0,
    val uploadOriginal: Boolean = false,
    val originalBytes: Long? = null,
    val processedBytes: Long? = null,
    val lastErrorStage: String? = null,
    val lastErrorClass: String? = null,
) {
    val durationMs: Long get() = (capturedEndMs - capturedStartMs).coerceAtLeast(0)
    val processingStatus: ProofProcessingStatus
        get() = ProofProcessingStatus.fromProcessingState(processingState, syncStatus, uploadOriginal)
}
