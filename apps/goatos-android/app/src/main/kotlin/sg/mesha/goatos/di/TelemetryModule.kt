package sg.mesha.goatos.di

import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import sg.mesha.goatos.BuildConfig
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.analytics.FirebaseCrashReporter
import sg.mesha.goatos.core.analytics.FirebasePerfNetworkTelemetryReporter
import sg.mesha.goatos.core.analytics.FirebasePerformanceTracer
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.analytics.NoopPerformanceTracer
import sg.mesha.goatos.core.analytics.PerformanceTracer
import sg.mesha.goatos.core.network.NetworkTelemetryReporter
import sg.mesha.goatos.core.network.NoopNetworkTelemetryReporter
import javax.inject.Singleton

/**
 * Wires the Firebase-backed telemetry seams (see `docs/TELEMETRY.md`): [CrashReporter],
 * [PerformanceTracer], and [NetworkTelemetryReporter]. The real [sg.mesha.goatos.core.analytics.AnalyticsPort]
 * binding (which composes [CrashReporter] — see `FirebaseAnalyticsAdapter`) stays in
 * `AnalyticsModule` alongside [sg.mesha.goatos.core.analytics.AnalyticsContext] for locality.
 *
 * Every binding here gates on [BuildConfig.TELEMETRY_ENABLED] (a per-flavor build config field,
 * see `app/build.gradle.kts`) so a flavor without a confirmed Firebase project (today: `dev` and
 * `prod` — see `app/src/google-services-README.md`) never depends on a vendor SDK actually
 * working; it gets the zero-dependency `Noop*` fallback instead.
 */
@Module
@InstallIn(SingletonComponent::class)
object TelemetryModule {

    @Provides
    @Singleton
    fun provideCrashReporter(): CrashReporter =
        if (BuildConfig.TELEMETRY_ENABLED) FirebaseCrashReporter() else NoopCrashReporter()

    @Provides
    @Singleton
    fun providePerformanceTracer(): PerformanceTracer =
        if (BuildConfig.TELEMETRY_ENABLED) FirebasePerformanceTracer() else NoopPerformanceTracer()

    @Provides
    @Singleton
    fun provideNetworkTelemetryReporter(): NetworkTelemetryReporter =
        if (BuildConfig.TELEMETRY_ENABLED) FirebasePerfNetworkTelemetryReporter() else NoopNetworkTelemetryReporter
}
