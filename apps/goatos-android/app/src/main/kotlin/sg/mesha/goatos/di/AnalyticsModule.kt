package sg.mesha.goatos.di

import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import sg.mesha.goatos.BuildConfig
import sg.mesha.goatos.core.analytics.AnalyticsContext
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.NoopAnalytics
import javax.inject.Singleton

/**
 * Wires the analytics seam. A [NoopAnalytics] is bound today — real event egress lands in a later,
 * Firebase-gated pass (see [AnalyticsPort]); binding it now lets call sites emit the taxonomy
 * without any behavior change or vendor coupling.
 *
 * [AnalyticsContext] is a singleton so the bootstrap-resolved role/park it holds are visible to
 * the (future) egress impl; its [AnalyticsContext.flavor] is build-fixed from [BuildConfig.FLAVOR].
 */
@Module
@InstallIn(SingletonComponent::class)
object AnalyticsModule {

    @Provides
    @Singleton
    fun provideAnalyticsPort(): AnalyticsPort = NoopAnalytics()

    @Provides
    @Singleton
    fun provideAnalyticsContext(): AnalyticsContext = AnalyticsContext(flavor = BuildConfig.FLAVOR)
}
