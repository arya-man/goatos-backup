package sg.mesha.goatos.core.data.vaccination.leadership

/**
 * One playable proof clip on a leadership evidence item. [url] is a short-lived signed URL
 * streamed directly (never proxied/downloaded whole).
 */
data class VaccinationLeadershipMediaUi(
    val proofId: String,
    val url: String,
    val mimeType: String,
)

/**
 * UI model for one verification item in the leadership gallery.
 *
 * Shows the proof status, metadata, and video(s) without any verdict controls.
 */
data class VaccinationLeadershipItemUi(
    val id: String,
    val title: String,
    val status: String,
    val statusLabel: String,
    val statusTone: String, // "neutral", "success", "error", "warn"
    val timestamp: String,
    val proofCount: Int,
    val videoUrls: List<String>,
    val summary: String, // Human-readable summary without technical vocabulary
    // Context fields (backend-composed labels — TRD dumb-renderer rule, never client-derived).
    val shedLabel: String = "",
    val parkLabel: String = "",
    val operatorLabel: String = "",
    val media: List<VaccinationLeadershipMediaUi> = emptyList(),
)

/** One backend-supplied park/shed filter option. Null [id] means "all". */
data class VaccinationLeadershipLocationOptionUi(val id: String?, val label: String)

/**
 * One drive ready for leadership CLOSE. This is `verification.act` authority (rule: leadership
 * keeps act, never verdict) — approve/reject remain verifier-only and never appear here.
 */
data class VaccinationLeadershipDriveClosureUi(
    val batchId: String,
    val driveLabel: String,
    val batchLabel: String,
    val totalCount: Int,
    val shedCount: Int,
    val videoCount: Int,
    val approvedVideos: Int,
    val rejectedVideos: Int,
    val pendingVideos: Int,
    val ready: Boolean,
)

/**
 * State for the vaccination leadership videos gallery.
 *
 * Full evidence trail (pending/approved/rejected/closed) rendered read-only with no
 * approval/rejection controls. Park/shed filters and drive-closure cards are leadership's own
 * `verification.act` surface, independent of the verifier's action queue.
 */
data class VaccinationLeadershipVideosUiState(
    // Screen title. Backend-owned (the queue contract's module label), never composed on-device --
    // clients render backend copy verbatim.
    val title: String = "",
    val loading: Boolean = false,
    val loadingMore: Boolean = false,
    val items: List<VaccinationLeadershipItemUi> = emptyList(),
    val error: String? = null,
    val staleNotice: String = "",
    val parkOptions: List<VaccinationLeadershipLocationOptionUi> = emptyList(),
    val selectedParkId: String? = null,
    val shedOptions: List<VaccinationLeadershipLocationOptionUi> = emptyList(),
    val selectedShedId: String? = null,
    val driveClosures: List<VaccinationLeadershipDriveClosureUi> = emptyList(),
    val closingBatchId: String? = null,
    val closeErrorBatchId: String? = null,
    val closeErrorMessage: String? = null,
    /** Item currently open in the read-only detail overlay. Null = gallery view. */
    val selectedItemId: String? = null,
)

/**
 * Events for the vaccination leadership videos screen.
 */
sealed class VaccinationLeadershipVideoEvent {
    data class Refresh(val reset: Boolean = true) : VaccinationLeadershipVideoEvent()
    data class ItemVisible(val index: Int) : VaccinationLeadershipVideoEvent()
    data class PlaybackEvent(val event: VaccinationLeadershipVideoPlaybackEvent) : VaccinationLeadershipVideoEvent()
    data class SelectPark(val parkId: String?) : VaccinationLeadershipVideoEvent()
    data class SelectShed(val shedId: String?) : VaccinationLeadershipVideoEvent()
    data class OpenItem(val itemId: String) : VaccinationLeadershipVideoEvent()
    data object CloseItem : VaccinationLeadershipVideoEvent()
    data class CloseDrive(val batchId: String) : VaccinationLeadershipVideoEvent()
}

/**
 * Video playback actions for telemetry.
 */
enum class VaccinationLeadershipVideoPlaybackAction {
    PLAY_STARTED,
    WATCH_SUMMARY,
    PLAYBACK_ERROR,
}

/**
 * Video playback event carrying telemetry details.
 */
data class VaccinationLeadershipVideoPlaybackEvent(
    val action: VaccinationLeadershipVideoPlaybackAction,
    val proofId: String,
    val mimeType: String = "",
    val durationMs: Long = 0,
    val watchTimeMs: Long = 0,
    val positionMs: Long = 0,
    val percentWatched: Float = 0f,
    val seekCount: Int = 0,
    val replayCount: Int = 0,
    val bufferingTimeMs: Long = 0,
    val reason: String? = null,
)
