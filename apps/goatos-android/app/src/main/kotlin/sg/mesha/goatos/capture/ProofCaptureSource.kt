package sg.mesha.goatos.capture

import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import dagger.hilt.EntryPoint
import dagger.hilt.InstallIn
import dagger.hilt.android.EntryPointAccessors
import dagger.hilt.components.SingletonComponent

/** One captured proof video, as the capture port sees it — a local, already-durable file the
 *  caller hands to Room (docs/mobile/proof-capture-sync-and-e2e.md §2/§3: "Room first").
 *  [startedAtMs]/[endedAtMs] are the device-clock record start/stop — anti-fraud freshness
 *  metadata ("Camera-only capture" business rule): every video is a fresh, timed, in-app
 *  recording, never an imported file (there is no picker path that could produce one). */
data class CapturedVideo(
    val localUri: String,
    val mimeType: String = "video/mp4",
    val startedAtMs: Long,
    val endedAtMs: Long,
)

/**
 * Port for the Submit recording-form's `video_proof` capture. Production: LIVE in-app CameraX
 * recording ONLY (`androidx.camera:camera-video` `Recorder`/`VideoCapture`, see
 * `InAppVideoRecorder.kt`) writing straight to this app's own private storage — no gallery
 * import, no `ACTION_GET_CONTENT`/`ACTION_PICK`, no chooser of any kind. This is a hard
 * anti-fraud business rule (docs/mobile/proof-capture-sync-and-e2e.md "Camera-only capture"):
 * a picker would let an operator submit an old or unrelated video as "proof", defeating the
 * entire point of the recording. Tests use [FakeProofCaptureSource].
 */
interface ProofCaptureSource {
    /** Suspends until a video has been captured (production: launches the camera intent and
     *  awaits its result), or returns null if the operator cancelled. */
    suspend fun captureVideo(): CapturedVideo?
}

/**
 * Production adapter. The live CameraX recorder must be bound from a Composable/Activity
 * that owns lifecycle, preview, and permission state — so this class is a thin, swappable
 * delegate: the hosting screen [bind]s a real CameraX-backed suspend function while it is
 * composed, and [unbind]s on dispose (lifecycle-aware — no leaked camera reference once the
 * capture surface leaves composition). Hilt provides ONE app-scoped instance; `SubmitViewModel`
 * depends only on the [ProofCaptureSource] interface and never knows a screen is (or isn't)
 * currently bound.
 */
class DelegatingProofCaptureSource : ProofCaptureSource {
    @Volatile
    private var delegate: (suspend () -> CapturedVideo?)? = null

    fun bind(launch: suspend () -> CapturedVideo?) {
        delegate = launch
    }

    fun unbind() {
        delegate = null
    }

    override suspend fun captureVideo(): CapturedVideo? = delegate?.invoke()
}

/** Test double: returns queued fixture results (or invokes [onCapture]) in call order. */
class FakeProofCaptureSource(
    private val results: MutableList<CapturedVideo?> = mutableListOf(),
) : ProofCaptureSource {
    var captureCount: Int = 0
        private set

    fun queue(video: CapturedVideo?) {
        results.add(video)
    }

    override suspend fun captureVideo(): CapturedVideo? {
        captureCount++
        return if (results.isNotEmpty()) results.removeAt(0) else null
    }
}

/** Fetches the Hilt-singleton [DelegatingProofCaptureSource] outside a ViewModel (Compose has
 *  no `hiltViewModel()`-style accessor for a plain `@Singleton` class) so the Submit screen's
 *  host composable can [BindVideoCaptureSource] to it right where it's rendered — the tightest
 *  scope for "release the camera the moment the capture surface leaves composition". */
@EntryPoint
@InstallIn(SingletonComponent::class)
interface ProofCaptureSourceEntryPoint {
    fun delegatingProofCaptureSource(): DelegatingProofCaptureSource
}

@Composable
fun rememberDelegatingProofCaptureSource(): DelegatingProofCaptureSource {
    val context = LocalContext.current
    return remember {
        EntryPointAccessors.fromApplication(context.applicationContext, ProofCaptureSourceEntryPoint::class.java)
            .delegatingProofCaptureSource()
    }
}
