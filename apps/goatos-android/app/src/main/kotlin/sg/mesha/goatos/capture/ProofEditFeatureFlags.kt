package sg.mesha.goatos.capture

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import dagger.hilt.EntryPoint
import dagger.hilt.InstallIn
import dagger.hilt.android.EntryPointAccessors
import dagger.hilt.components.SingletonComponent
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.data.BootstrapRepository

/**
 * Reads the backend-owned bootstrap `feature_flags` for a capture host, and gives it a telemetry
 * sink, without either being threaded through every screen that renders a proof control.
 *
 * Uses the same Hilt entry-point pattern as [rememberDelegatingProofCaptureSource] in
 * `ProofCaptureSource.kt`: the capture surfaces are composed deep inside the nav graph, and
 * plumbing two more constructor dependencies through every one of them would touch a dozen
 * screens for a flag that only one surface reads today.
 */
@EntryPoint
@InstallIn(SingletonComponent::class)
internal interface ProofCaptureHostEntryPoint {
    fun bootstrapRepository(): BootstrapRepository
    fun analytics(): AnalyticsPort
}

/**
 * Bootstrap `feature_flags`, or an EMPTY map until they resolve — and permanently empty if the
 * read fails.
 *
 * Empty means the trim editor is not offered, so a device that cannot reach bootstrap gets today's
 * capture flow rather than a half-configured new one. That is the same fail-closed direction
 * [sg.mesha.goatos.core.proofedit.ProofEditGate] applies to an absent flag.
 */
@Composable
internal fun rememberProofCaptureFeatureFlags(): Map<String, Boolean> {
    val context = LocalContext.current
    val repository = remember(context) {
        EntryPointAccessors
            .fromApplication(context.applicationContext, ProofCaptureHostEntryPoint::class.java)
            .bootstrapRepository()
    }
    val flags by produceState(initialValue = emptyMap<String, Boolean>(), repository) {
        value = runCatching { repository.loadNavState().featureFlags }
            // exception:exempt a failed flag read must not break capture; falling back to an empty
            // map keeps the operator on the existing recording flow, which is the safe default.
            .getOrDefault(emptyMap())
    }
    return flags
}

/**
 * Step telemetry sink for a capture host. Events go to Firebase Analytics through the app's normal
 * [AnalyticsPort], which already carries the standard identity params and enforces the GA4 param
 * budget, so nothing here has to know about either.
 */
@Composable
internal fun rememberProofCaptureTelemetry(): (String, Map<String, String>) -> Unit {
    val context = LocalContext.current
    val analytics = remember(context) {
        EntryPointAccessors
            .fromApplication(context.applicationContext, ProofCaptureHostEntryPoint::class.java)
            .analytics()
    }
    return remember(analytics) { { event, props -> analytics.track(event, props) } }
}
