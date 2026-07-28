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
 *  [startedAtMs]/[endedAtMs] are the device-clock capture/import start/stop metadata. The SOP
 *  decides whether this proof must be a live camera clip or may come from gallery. */
data class CapturedVideo(
    val localUri: String,
    val mimeType: String = "video/mp4",
    val startedAtMs: Long,
    val endedAtMs: Long,
    val captureSource: String = "in_app_camera",
)

/** Operator-facing copy for the shared recorder. The prompt belongs to each capture request,
 * not to the host route: one Death workflow contains both death and post-mortem recordings. */
enum class ProofCapturePrompt {
    VACCINATION,
    BIRTH,
    DEATH,
    POST_MORTEM,
    SHIFTING,
    FEED_DISTRIBUTION,
    WATER_DISTRIBUTION,
    FEED_PACKING,
}

/**
 * Port for the Submit recording-form's `video_proof` capture. Production camera capture uses
 * LIVE in-app CameraX (`androidx.camera:camera-video` `Recorder`/`VideoCapture`, see
 * `InAppVideoRecorder.kt`) writing straight to this app's own private storage. Gallery picker
 * is exposed only when backend SOP allows shed-level proof; per-goat proof remains camera-only.
 * Tests use [FakeProofCaptureSource].
 */
interface ProofCaptureSource {
    /** Suspends until a video has been captured (production: launches the camera intent and
     *  awaits its result), or returns null if the operator cancelled. */
    suspend fun captureVideo(): CapturedVideo?

    /** Captures with workflow-specific guidance. [taskTitle] is backend-owned workflow copy and
     * replaces the generic module recorder heading when supplied. Existing callers deliberately
     * omit it so their current copy and behavior remain unchanged. */
    suspend fun captureVideo(prompt: ProofCapturePrompt, taskTitle: String? = null): CapturedVideo? = captureVideo()

    /** Suspends until a video is selected from gallery and copied into app-private storage, or
     *  returns null if the operator cancelled. Only call when the backend SOP allows it. */
    suspend fun pickVideo(): CapturedVideo?
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
    private var delegate: (suspend (ProofCapturePrompt, String?) -> CapturedVideo?)? = null
    @Volatile
    private var pickerDelegate: (suspend () -> CapturedVideo?)? = null
    @Volatile
    private var generation: Int = 0

    @Synchronized
    fun bind(
        launch: suspend (ProofCapturePrompt, String?) -> CapturedVideo?,
        pick: suspend () -> CapturedVideo?,
    ): Int {
        generation += 1
        val token = generation
        delegate = launch
        pickerDelegate = pick
        return token
    }

    @Synchronized
    fun unbind(token: Int) {
        if (token != generation) {
            return
        }
        delegate = null
        pickerDelegate = null
    }

    override suspend fun captureVideo(): CapturedVideo? {
        return captureVideo(ProofCapturePrompt.VACCINATION)
    }

    override suspend fun captureVideo(prompt: ProofCapturePrompt, taskTitle: String?): CapturedVideo? {
        val launch = delegate
        return launch?.invoke(prompt, taskTitle)
    }

    override suspend fun pickVideo(): CapturedVideo? {
        val pick = pickerDelegate
        return pick?.invoke()
    }
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

    override suspend fun pickVideo(): CapturedVideo? = captureVideo()
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
