package sg.mesha.goatos.capture

import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import dagger.hilt.EntryPoint
import dagger.hilt.InstallIn
import dagger.hilt.android.EntryPointAccessors
import dagger.hilt.components.SingletonComponent

/**
 * One captured AUDIO note (maintainer decision 2026-09-03: the vendor voice note), as the capture
 * port sees it — a local, already-durable file in this app's OWN private storage the caller hands
 * to the outbox (same "Room first" contract as [CapturedVideo] and [CapturedPhoto]).
 *
 * `captureSource` is the microphone, and the server holds an audio upload to that: there is no
 * gallery path for a voice note, so a note is always spoken at the desk, never picked from a file.
 */
data class CapturedAudio(
    val localUri: String,
    val mimeType: String = "audio/mp4",
    val startedAtMs: Long,
    val endedAtMs: Long,
    val captureSource: String = "in_app_microphone",
)

data class AudioCaptureContext(
    val title: String = "",
    val instruction: String = "",
)

/** Port for a LIVE in-app microphone recording; the audio sibling of [PhotoCaptureSource]. */
interface AudioCaptureSource {
    /** Suspends until a note has been recorded, or returns null if the operator cancelled. */
    suspend fun captureAudio(context: AudioCaptureContext = AudioCaptureContext()): CapturedAudio?
}

/**
 * Production adapter — the same swappable delegate as [DelegatingPhotoCaptureSource]: the hosting
 * screen [bind]s a real MediaRecorder-backed suspend function while it is composed and [unbind]s
 * on dispose, so no recorder outlives the screen that asked for it.
 */
class DelegatingAudioCaptureSource : AudioCaptureSource {
    @Volatile
    private var delegate: (suspend (AudioCaptureContext) -> CapturedAudio?)? = null

    @Volatile
    private var generation: Int = 0

    @Synchronized
    fun bind(capture: suspend (AudioCaptureContext) -> CapturedAudio?): Int {
        generation += 1
        val token = generation
        delegate = capture
        return token
    }

    @Synchronized
    fun unbind(token: Int) {
        if (token != generation) return
        delegate = null
    }

    override suspend fun captureAudio(context: AudioCaptureContext): CapturedAudio? = delegate?.invoke(context)
}

/** Test double: returns queued fixture results in call order. */
class FakeAudioCaptureSource(
    private val results: MutableList<CapturedAudio?> = mutableListOf(),
) : AudioCaptureSource {
    var captureCount: Int = 0
        private set

    fun queue(audio: CapturedAudio?) {
        results.add(audio)
    }

    override suspend fun captureAudio(context: AudioCaptureContext): CapturedAudio? {
        captureCount++
        return if (results.isNotEmpty()) results.removeAt(0) else null
    }
}

@EntryPoint
@InstallIn(SingletonComponent::class)
interface AudioCaptureSourceEntryPoint {
    fun delegatingAudioCaptureSource(): DelegatingAudioCaptureSource
}

@Composable
fun rememberDelegatingAudioCaptureSource(): DelegatingAudioCaptureSource {
    val context = LocalContext.current
    return remember {
        EntryPointAccessors.fromApplication(context.applicationContext, AudioCaptureSourceEntryPoint::class.java)
            .delegatingAudioCaptureSource()
    }
}
