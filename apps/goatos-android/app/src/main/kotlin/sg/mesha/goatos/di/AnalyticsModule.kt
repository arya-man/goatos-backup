package sg.mesha.goatos.di

import android.content.Context
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import sg.mesha.goatos.BuildConfig
import sg.mesha.goatos.core.analytics.AnalyticsContext
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.analytics.FirebaseAnalyticsAdapter
import sg.mesha.goatos.core.analytics.NoopAnalytics
import javax.inject.Singleton

/**
 * Wires the analytics seam (see `docs/TELEMETRY.md`). [FirebaseAnalyticsAdapter] is bound for
 * flavors with `BuildConfig.TELEMETRY_ENABLED = true` (today: `stg`, a confirmed real Firebase
 * project — see `app/src/google-services-README.md`); [NoopAnalytics] otherwise (`dev`/`prod`
 * until their Firebase projects are confirmed, and always in tests). This is also the FCM push
 * identity seam: [AnalyticsPort.setUserId] / [AnalyticsPort.setUserProperty] are what
 * `BootstrapViewModel.applyAnalyticsIdentity` and `PushLogoutCleanup` call to couple/decouple the
 * operator identity Firebase Messaging keys push delivery on — there is no separate push-only
 * analytics port.
 *
 * [AnalyticsContext] is a singleton so the bootstrap-resolved role/park it holds are visible to
 * the egress impl; its [AnalyticsContext.flavor] is build-fixed from [BuildConfig.FLAVOR].
 */
@Module
@InstallIn(SingletonComponent::class)
object AnalyticsModule {

    @Provides
    @Singleton
    fun provideAnalyticsPort(
        @ApplicationContext context: Context,
        crashReporter: CrashReporter,
    ): AnalyticsPort =
        if (BuildConfig.TELEMETRY_ENABLED) {
            FirebaseAnalyticsAdapter(context, crashReporter)
        } else {
            NoopAnalytics()
        }

    @Provides
    @Singleton
    fun provideAnalyticsContext(): AnalyticsContext = AnalyticsContext(flavor = BuildConfig.FLAVOR)
}
