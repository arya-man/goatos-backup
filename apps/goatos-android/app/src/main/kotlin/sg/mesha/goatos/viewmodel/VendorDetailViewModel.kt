package sg.mesha.goatos.viewmodel

import android.media.MediaPlayer
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
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
    savedStateHandle: SavedStateHandle,
    private val repository: VendorsRepository,
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
    private var player: MediaPlayer? = null

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
            VendorDetailEvent.DismissMessage -> local.update { it.copy(message = null) }
            VendorDetailEvent.Back -> Unit
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
        viewModelScope.launch {
            local.update { it.copy(playback = VoiceNotePlayback.LOADING, message = null) }
            val ref = repository.observeVendor(vendorId).first()?.voiceNoteProofRef.orEmpty()
            val url = repository.resolveVoiceNoteUrl(ref)?.let(::vendorsAbsoluteProofUrl)
            if (url.isNullOrBlank()) {
                local.update { it.copy(playback = VoiceNotePlayback.FAILED) }
                analytics.track(AnalyticsEventsVendors.VENDORS_FAILURE, mapOf(AnalyticsEvents.Params.REASON to "voice_note_url"))
                return@launch
            }
            startPlayer(url)
        }
    }

    private fun startPlayer(url: String) {
        stop()
        try {
            val mediaPlayer = MediaPlayer()
            mediaPlayer.setDataSource(url)
            mediaPlayer.setOnPreparedListener {
                it.start()
                local.update { l -> l.copy(playback = VoiceNotePlayback.PLAYING, url = url) }
            }
            mediaPlayer.setOnCompletionListener { local.update { l -> l.copy(playback = VoiceNotePlayback.IDLE) } }
            mediaPlayer.setOnErrorListener { _, _, _ ->
                local.update { l -> l.copy(playback = VoiceNotePlayback.FAILED) }
                true
            }
            mediaPlayer.prepareAsync()
            player = mediaPlayer
        } catch (e: Exception) {
            crashReporter.recordException(e, "vendor voice note playback failed")
            local.update { it.copy(playback = VoiceNotePlayback.FAILED) }
        }
    }

    private fun stop() {
        // exception:exempt a player that never prepared throws on stop(); release regardless.
        runCatching { player?.stop() }
        runCatching { player?.release() }
        player = null
        local.update { if (it.playback == VoiceNotePlayback.PLAYING) it.copy(playback = VoiceNotePlayback.IDLE) else it }
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
        "Lead time" to etaAfterOrderDays?.let { "$it days" },
        "Filtered stock" to filteredStock?.toString(),
        "Ready to filtered" to readyToFiltered,
    )
    val notes = rows("Details" to details, "Note" to comments)
    return listOfNotNull(
        VendorsDetailSectionUi("Who they are", identity).takeIf { identity.isNotEmpty() },
        VendorsDetailSectionUi("What they supply", supply).takeIf { supply.isNotEmpty() },
        VendorsDetailSectionUi("Notes", notes).takeIf { notes.isNotEmpty() },
    )
}
