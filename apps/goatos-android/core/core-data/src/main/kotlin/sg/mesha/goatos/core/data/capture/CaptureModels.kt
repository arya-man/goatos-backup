package sg.mesha.goatos.core.data.capture

/** Business cap (docs/mobile/proof-capture-sync-and-e2e.md §2, maintainer-confirmed): 3
 *  mandatory named videos (shed / vial_lot / administration) + up to 2 optional extras.
 *  Mirrors [sg.mesha.goatos.core.database.capture.ProofCaptureDao.MAX_PROOFS_PER_TASK] — kept
 *  here too so `:app`/ViewModel code never needs a `core-database` import for this constant. */
const val MAX_PROOFS_PER_TASK = 5

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

/** The proof subjects the vaccination SOP documents
 *  (docs/mobile/proof-capture-sync-and-e2e.md §2). [EXTRA] is any operator-added video beyond
 *  the named/required ones, up to the total cap — it carries a caption instead of a fixed label. */
enum class ProofSubject(val wireValue: String) {
    SHED("shed"),
    VIAL_LOT("vial_lot"),
    ADMINISTRATION("administration"),
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
