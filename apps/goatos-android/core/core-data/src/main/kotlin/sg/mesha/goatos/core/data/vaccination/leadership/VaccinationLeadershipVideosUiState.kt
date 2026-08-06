package sg.mesha.goatos.core.data.vaccination.leadership

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
)

/**
 * State for the vaccination leadership videos gallery.
 *
 * Full evidence trail (pending/approved/rejected/closed) rendered read-only with no approval/rejection controls.
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
)

/**
 * Events for the vaccination leadership videos screen.
 */
sealed class VaccinationLeadershipVideoEvent {
    data class Refresh(val reset: Boolean = true) : VaccinationLeadershipVideoEvent()
    data class ItemVisible(val index: Int) : VaccinationLeadershipVideoEvent()
    data class PlaybackEvent(val event: VaccinationLeadershipVideoPlaybackEvent) : VaccinationLeadershipVideoEvent()
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
