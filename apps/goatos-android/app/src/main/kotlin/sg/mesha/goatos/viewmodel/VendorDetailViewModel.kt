package sg.mesha.goatos.viewmodel

import android.content.Context
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.exoplayer.ExoPlayer
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsVendors
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.data.VendorsRepository
import sg.mesha.goatos.core.media.ProofPlayerFactory
import sg.mesha.goatos.core.network.dto.VendorDto
import sg.mesha.goatos.feature.vendors.VendorDetailEvent
import sg.mesha.goatos.feature.vendors.VendorDetailUiState
import sg.mesha.goatos.feature.vendors.VendorsDetailRowUi
import sg.mesha.goatos.feature.vendors.VendorsDetailSectionUi
import sg.mesha.goatos.feature.vendors.VoiceNotePlayback
import sg.mesha.goatos.ui.Routes
import javax.inject.Inject

/** One vendor's detail (L1), Room-first, with the voice note's on-demand playback. */
@HiltViewModel
class VendorDetailViewModel @Inject constructor(
    @param:ApplicationContext private val appContext: Context,
    savedStateHandle: SavedStateHandle,
    private val repository: VendorsRepository,
    private val proofPlayerFactory: ProofPlayerFactory,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private val vendorId: String = savedStateHandle.get<String>(Routes.VENDOR_ID_ARG).orEmpty()

    private data class Local(
        val isRefreshing: Boolean = false,
        val loaded: Boolean = false,
        val playback: VoiceNotePlayback = VoiceNotePlayback.NONE,
        val url: String = "",
        val message: String? = null,
    )

    private val local = MutableStateFlow(Local())
    private var player: ExoPlayer? = null
    private var playbackRequestId = 0
    private var activeVoiceNoteProofRef: String = ""
    private var activeVoiceNotePlayTracked = false

    init {
        refresh()
    }

    val state: StateFlow<VendorDetailUiState> = combine(repository.observeVendor(vendorId), local) { vendor, l ->
        if (vendor == null) {
            VendorDetailUiState(isRefreshing = l.isRefreshing, isLoading = !l.loaded, message = l.message)
        } else {
            VendorDetailUiState(
                title = vendor.displayName.ifBlank { vendor.businessName },
                subtitle = vendor.typeLine(),
                statusLabel = vendor.statusLabel.ifBlank { vendor.status },
                statusTone = vendorStatusTone(vendor.status),
                sections = vendor.sections(),
                voiceNote = if (vendor.voiceNoteProofRef.isNullOrBlank()) VoiceNotePlayback.NONE else if (l.playback == VoiceNotePlayback.NONE) VoiceNotePlayback.IDLE else l.playback,
                voiceNoteUrl = l.url,
                isRefreshing = l.isRefreshing,
                isLoading = false,
                message = l.message,
            )
        }
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), VendorDetailUiState())

    fun onEvent(event: VendorDetailEvent) {
        when (event) {
            VendorDetailEvent.Refresh -> refresh()
            VendorDetailEvent.PlayVoiceNote -> play()
            VendorDetailEvent.StopVoiceNote -> stop()
            VendorDetailEvent.AppStopped -> stop()
            VendorDetailEvent.DismissMessage -> local.update { it.copy(message = null) }
            VendorDetailEvent.Back -> Unit
            // Navigation-owned: the host routes to the edit form.
            VendorDetailEvent.Edit -> Unit
        }
    }

    private fun refresh() {
        viewModelScope.launch {
            local.update { it.copy(isRefreshing = true) }
            try {
                repository.refreshVendor(vendorId)
            } finally {
                local.update { it.copy(isRefreshing = false, loaded = true) }
            }
        }
    }

    private fun play() {
        val requestId = ++playbackRequestId
        viewModelScope.launch {
            local.update { it.copy(playback = VoiceNotePlayback.LOADING, message = null) }
            val ref = repository.observeVendor(vendorId).first()?.voiceNoteProofRef.orEmpty()
            val url = repository.resolveVoiceNoteUrl(ref)?.let(::vendorsAbsoluteProofUrl)
            if (requestId != playbackRequestId) {
                resetStaleLoading()
                return@launch
            }
            if (url.isNullOrBlank()) {
                local.update { it.copy(playback = VoiceNotePlayback.FAILED) }
                analytics.track(AnalyticsEventsVendors.VENDORS_FAILURE, mapOf(AnalyticsEvents.Params.REASON to "voice_note_url"))
                return@launch
            }
            // proof-media-egress:ignore Voice-note playback is explicit tap action and startPlayer uses ProofMediaHttp telemetry/auth.
            startPlayer(url, ref)
        }
    }

    private fun resetStaleLoading() {
        local.update { if (it.playback == VoiceNotePlayback.LOADING) it.copy(playback = VoiceNotePlayback.IDLE) else it }
    }

    // proof-media-egress:ignore Voice-note playback helper is explicit tap action and uses ProofMediaHttp telemetry/auth.
    private fun startPlayer(url: String, proofRef: String) {
        stop()
        activeVoiceNoteProofRef = proofRef.trim()
        activeVoiceNotePlayTracked = false
        try {
            val mediaPlayer = proofPlayerFactory.create(appContext)
            mediaPlayer.addListener(object : Player.Listener {
                override fun onPlaybackStateChanged(playbackState: Int) {
                    if (playbackState == Player.STATE_READY && mediaPlayer.playWhenReady) {
                        local.update { l -> l.copy(playback = VoiceNotePlayback.PLAYING, url = url) }
                        if (!activeVoiceNotePlayTracked) {
                            activeVoiceNotePlayTracked = true
                            trackVoiceNotePlayback("play")
                        }
                    }
                    if (playbackState == Player.STATE_ENDED) {
                        trackVoiceNotePlayback("complete")
                        activeVoiceNoteProofRef = ""
                        activeVoiceNotePlayTracked = false
                        local.update { l -> l.copy(playback = VoiceNotePlayback.IDLE) }
                    }
                }

                override fun onPlayerError(error: PlaybackException) {
                    crashReporter.recordException(error, "vendor voice note playback failed")
                    trackVoiceNotePlayback("failure")
                    activeVoiceNoteProofRef = ""
                    activeVoiceNotePlayTracked = false
                    local.update { l -> l.copy(playback = VoiceNotePlayback.FAILED) }
                }
            })
            // proof-media-egress:ignore Voice-note playback is tap-started and goes through ProofMediaHttp auth/telemetry before release on stop/background.
            mediaPlayer.setMediaItem(MediaItem.fromUri(url))
            mediaPlayer.playWhenReady = true
            mediaPlayer.prepare()
            player = mediaPlayer
        } catch (e: Exception) {
            crashReporter.recordException(e, "vendor voice note playback failed")
            trackVoiceNotePlayback("failure")
            activeVoiceNoteProofRef = ""
            activeVoiceNotePlayTracked = false
            local.update { it.copy(playback = VoiceNotePlayback.FAILED) }
        }
    }

    private fun stop() {
        playbackRequestId += 1
        // exception:exempt a player that never prepared throws on stop(); release regardless.
        runCatching { player?.stop() }
        runCatching { player?.release() }
        player = null
        if (activeVoiceNoteProofRef.isNotBlank()) {
            trackVoiceNotePlayback("stop")
            activeVoiceNoteProofRef = ""
        }
        activeVoiceNotePlayTracked = false
        local.update { if (it.playback == VoiceNotePlayback.PLAYING) it.copy(playback = VoiceNotePlayback.IDLE) else it }
    }

    private fun trackVoiceNotePlayback(action: String) {
        analytics.track(
            AnalyticsEventsVendors.VENDORS_VOICE_NOTE_PLAYBACK,
            mapOf(
                "vendor_id" to vendorId.take(80),
                "proof_ref" to activeVoiceNoteProofRef.take(80),
                "action" to action,
            ),
        )
    }

    override fun onCleared() {
        stop()
        super.onCleared()
    }
}

/** The detail's sections: identity, supply, notes. Backend words verbatim; blanks are dropped. */
internal fun VendorDto.sections(): List<VendorsDetailSectionUi> {
    fun rows(vararg pairs: Pair<String, String?>) = pairs.mapNotNull { (label, value) ->
        value?.takeIf { it.isNotBlank() }?.let { VendorsDetailRowUi(label, it) }
    }
    val identity = rows(
        "Vendor type" to recordType,
        "Contact person" to contactPersonName,
        "Phone number" to phoneNumber,
        "Location" to locationDisplay,
    )
    val supply = rows(
        "Capacity" to capacityDisplay,
        "Feed" to feed,
        "Breed" to breed,
        "Price per goat" to pricePerGoat?.let { "₹$it" },
        "Average animal weight" to averageAnimalWeightDisplay,
        "Lead time" to etaAfterOrderDays?.let { "$it days" },
        "Filtered stock" to filteredStock?.toString(),
        "Ready to filtered" to readyToFiltered,
    )
    val notes = rows("Details" to details, "Note" to comments)
    // VENDOR FORM IS AUTHORED (2026-09-19): the questions the published form added beyond the
    // register's columns, labelled by the backend from the version they were answered on. The
    // single-vendor read carries them; a list-cached row (no rows yet) falls back to the raw ids.
    val more = answerRows.map { VendorsDetailRowUi(it.label, it.value) }.ifEmpty {
        answers.filterKeys { !it.endsWith("_other") }.filterValues { it.isNotBlank() }.map { (k, v) -> VendorsDetailRowUi(k, v) }
    }
    return listOfNotNull(
        VendorsDetailSectionUi("Who they are", identity).takeIf { identity.isNotEmpty() },
        VendorsDetailSectionUi("What they supply", supply).takeIf { supply.isNotEmpty() },
        VendorsDetailSectionUi("Notes", notes).takeIf { notes.isNotEmpty() },
        VendorsDetailSectionUi("More", more).takeIf { more.isNotEmpty() },
    )
}
