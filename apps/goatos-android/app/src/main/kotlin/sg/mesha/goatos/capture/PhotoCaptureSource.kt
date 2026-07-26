package sg.mesha.goatos.capture

import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import dagger.hilt.EntryPoint
import dagger.hilt.InstallIn
import dagger.hilt.android.EntryPointAccessors
import dagger.hilt.components.SingletonComponent

/**
 * One captured proof PHOTO, as the capture port sees it — a local, already-durable file in this
 * app's OWN private storage the caller hands to the outbox (same "Room first" contract as
 * [CapturedVideo]). Used by the feed-distribution flow's water-distribution proof, which the
 * operator may satisfy with a photo OR a video; the video option reuses [ProofCaptureSource].
 */
data class CapturedPhoto(
    val localUri: String,
    val mimeType: String = "image/jpeg",
    val capturedAtMs: Long,
    val captureSource: String = "in_app_camera",
)

/**
 * Port for a LIVE in-app photo capture (CameraX `ImageCapture`), the photo sibling of
 * [ProofCaptureSource]. Production capture uses the back camera writing straight to app-private
 * storage — no FileProvider, MediaStore, or gallery surface. Tests use [FakePhotoCaptureSource].
 */
interface PhotoCaptureSource {
    /** Suspends until a photo has been captured (production: launches the in-app camera and awaits
     *  its result), or returns null if the operator cancelled. */
    suspend fun capturePhoto(): CapturedPhoto?
}

/**
 * Production adapter — a thin, swappable delegate mirroring [DelegatingProofCaptureSource]: the
 * hosting screen [bind]s a real CameraX-backed suspend function while it is composed and [unbind]s
 * on dispose (lifecycle-aware — no leaked camera reference once the capture surface leaves
 * composition). Hilt provides ONE app-scoped instance.
 */
class DelegatingPhotoCaptureSource : PhotoCaptureSource {
    @Volatile
    private var delegate: (suspend () -> CapturedPhoto?)? = null
    @Volatile
    private var generation: Int = 0

    @Synchronized
    fun bind(capture: suspend () -> CapturedPhoto?): Int {
        generation += 1
        val token = generation
        delegate = capture
        return token
    }

    @Synchronized
    fun unbind(token: Int) {
        if (token != generation) {
            return
        }
        delegate = null
    }

    override suspend fun capturePhoto(): CapturedPhoto? = delegate?.invoke()
}

/** Test double: returns queued fixture results in call order. */
class FakePhotoCaptureSource(
    private val results: MutableList<CapturedPhoto?> = mutableListOf(),
) : PhotoCaptureSource {
    var captureCount: Int = 0
        private set

    fun queue(photo: CapturedPhoto?) {
        results.add(photo)
    }

    override suspend fun capturePhoto(): CapturedPhoto? {
        captureCount++
        return if (results.isNotEmpty()) results.removeAt(0) else null
    }
}

/** Fetches the Hilt-singleton [DelegatingPhotoCaptureSource] outside a ViewModel so the hosting
 *  composable can [BindPhotoCaptureSource] to it right where it's rendered (tightest scope for
 *  "release the camera the moment the capture surface leaves composition"). */
@EntryPoint
@InstallIn(SingletonComponent::class)
interface PhotoCaptureSourceEntryPoint {
    fun delegatingPhotoCaptureSource(): DelegatingPhotoCaptureSource
}

@Composable
fun rememberDelegatingPhotoCaptureSource(): DelegatingPhotoCaptureSource {
    val context = LocalContext.current
    return remember {
        EntryPointAccessors.fromApplication(context.applicationContext, PhotoCaptureSourceEntryPoint::class.java)
            .delegatingPhotoCaptureSource()
    }
}
