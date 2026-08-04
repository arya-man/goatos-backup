package sg.mesha.goatos.di

import androidx.media3.common.util.UnstableApi
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton
import sg.mesha.goatos.core.media.ProofMediaHttp
import sg.mesha.goatos.core.media.ProofPlayerFactory
import sg.mesha.goatos.core.media.TelemetryProofPlayerFactory
import sg.mesha.goatos.core.network.NetworkTelemetryReporter

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
    fun provideProofPlayerFactory(reporter: NetworkTelemetryReporter): ProofPlayerFactory =
        TelemetryProofPlayerFactory(ProofMediaHttp.proofMediaOkHttp(reporter = reporter))
}
