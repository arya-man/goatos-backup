package sg.mesha.goatos.core.data.capture

/** Bounded Room read ceiling for one shed task. The business cap is per goat, below. */
const val MAX_PROOFS_PER_TASK = 10_000

/** A goat may have several camera clips from one handling, while still keeping each row bounded. */
const val MAX_PROOFS_PER_GOAT = 5

/** Field key used by the shed-level vaccination Scan screen before the Submit form is opened.
 *  Submit folds these roster-level captures into the SOP-declared GOAT_SCAN answer field, whose
 *  key is backend-owned and may not literally be `goat_scan`. */
const val ROSTER_SCAN_FIELD_KEY = "__scan_roster__"

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
    PARK("park"),
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
