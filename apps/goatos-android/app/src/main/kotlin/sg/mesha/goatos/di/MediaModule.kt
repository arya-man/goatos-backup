package sg.mesha.goatos.di

import androidx.media3.common.util.UnstableApi
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton
import sg.mesha.goatos.core.media.ProofMediaHttp
import sg.mesha.goatos.core.media.ProofPlayerFactory
import sg.mesha.goatos.core.media.ProofRemoteImageLoader
import sg.mesha.goatos.core.media.OkHttpProofRemoteImageLoader
import sg.mesha.goatos.core.media.TelemetryProofPlayerFactory
import sg.mesha.goatos.core.datastore.DeviceStore
import sg.mesha.goatos.core.datastore.SessionStore
import sg.mesha.goatos.core.network.NetworkTelemetryReporter
import sg.mesha.goatos.core.network.RequestMetadata
import sg.mesha.goatos.BuildConfig
import android.os.Build
import sg.mesha.goatos.auth.currentFirebaseIdTokenBlocking

/**
 * Gives proof-video playback the SAME failure telemetry every API call gets (W-22).
 *
 * The reporter injected here is the one `TelemetryModule` builds — the
 * `FailureReportingNetworkTelemetryReporter`, with its single copy of the non-fatal throttle and
 * redaction rules. That is the whole point of routing media3 through OkHttp rather than hanging
 * an `AnalyticsListener` off each player: one policy, one implementation.
 *
 * Singleton because an OkHttp client owns a connection pool and dispatcher; one player per open
 * video, but one client for all of them.
 */
@Module
@InstallIn(SingletonComponent::class)
object MediaModule {

    @Provides
    @Singleton
    @UnstableApi
    fun provideProofMediaClient(
        reporter: NetworkTelemetryReporter,
        sessionStore: SessionStore,
        deviceStore: DeviceStore,
    ): okhttp3.OkHttpClient =
        ProofMediaHttp.proofMediaOkHttp(
            reporter = reporter,
            apiBaseUrl = BuildConfig.API_BASE_URL,
            tokenProvider = {
                if (BuildConfig.FLAVOR == "dev") {
                    sessionStore.cachedToken() ?: BuildConfig.DEV_BEARER_TOKEN.takeIf { it.isNotBlank() }
                } else {
                    currentFirebaseIdTokenBlocking()
                }
            },
            tenantIdProvider = { BuildConfig.TENANT_ID },
            localeProvider = { sessionStore.cachedLanguage() },
            requestMetadataProvider = {
                RequestMetadata(
                    appVersion = BuildConfig.VERSION_NAME,
                    appVersionCode = BuildConfig.VERSION_CODE.toString(),
                    buildType = BuildConfig.FLAVOR + if (BuildConfig.DEBUG) "Debug" else "Release",
                    deviceId = deviceStore.appInstallIdSync(),
                    platform = "android",
                    osVersion = "Android ${Build.VERSION.RELEASE.orEmpty()}",
                    sdkVersion = Build.VERSION.SDK_INT.toString(),
                    deviceModel = listOf(Build.MANUFACTURER, Build.MODEL)
                        .map { it.trim() }
                        .filter { it.isNotBlank() }
                        .distinct()
                        .joinToString(" "),
                )
            },
        )

    @Provides
    @Singleton
    @UnstableApi
    fun provideProofPlayerFactory(client: okhttp3.OkHttpClient): ProofPlayerFactory =
        TelemetryProofPlayerFactory(client)

    @Provides
    @Singleton
    fun provideProofRemoteImageLoader(client: okhttp3.OkHttpClient): ProofRemoteImageLoader =
        OkHttpProofRemoteImageLoader(client)
}
