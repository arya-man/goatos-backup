package sg.mesha.goatos.capture

import android.content.Context
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import dagger.hilt.EntryPoint
import dagger.hilt.InstallIn
import dagger.hilt.android.EntryPointAccessors
import dagger.hilt.components.SingletonComponent
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.NoopAnalytics

internal const val PROOF_CAMERA_KIND_PHOTO = "photo"
internal const val PROOF_CAMERA_KIND_VIDEO = "video"

@EntryPoint
@InstallIn(SingletonComponent::class)
internal interface ProofCameraAnalyticsEntryPoint {
    fun analyticsPort(): AnalyticsPort
}

@Composable
internal fun rememberProofCameraAnalytics(): AnalyticsPort {
    val context = LocalContext.current.applicationContext
    return remember(context) { context.proofCameraAnalytics() }
}

internal fun Context.proofCameraAnalytics(): AnalyticsPort =
    runCatching {
        EntryPointAccessors.fromApplication(
            applicationContext,
            ProofCameraAnalyticsEntryPoint::class.java,
        ).analyticsPort()
    }.getOrDefault(NoopAnalytics())

internal fun AnalyticsPort.trackProofCamera(
    event: String,
    kind: String,
    source: String = "generic",
    props: Map<String, String> = emptyMap(),
) {
    track(
        event,
        buildMap {
            put(AnalyticsEvents.Params.KIND, kind)
            put(AnalyticsEvents.Params.SOURCE, source)
            putAll(props)
        },
    )
}

internal fun proofCameraSource(prompt: ProofCapturePrompt?): String =
    prompt?.name?.lowercase() ?: "generic"

internal fun proofCameraStatus(torchEnabled: Boolean): String =
    if (torchEnabled) "torch_on" else "torch_off"

internal fun ProofTorchMode.analyticsValue(): String = name.lowercase()
