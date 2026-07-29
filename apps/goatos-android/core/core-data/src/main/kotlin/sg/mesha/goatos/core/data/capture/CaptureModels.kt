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

/** One de-duplicated RFID scan captured for a task's `goat_scan` recording-form field. */
data class ScannedGoatRow(
    val fieldKey: String,
    val tag: String,
    val goatId: String?,
    val obligationId: String?,
    val capturedAtMs: Long,
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
    val mimeType: String,
    val caption: String?,
    val capturedAtMs: Long,
    /** Freshness/anti-fraud metadata — see `ProofCaptureEntity`'s kdoc ("Camera-only capture"). */
    val capturedStartMs: Long,
    val capturedEndMs: Long,
    val capturedByPrincipalId: String?,
    val syncStatus: CaptureSyncStatus,
    val serverProofId: String?,
    val lastError: String?,
) {
    val durationMs: Long get() = (capturedEndMs - capturedStartMs).coerceAtLeast(0)
}
