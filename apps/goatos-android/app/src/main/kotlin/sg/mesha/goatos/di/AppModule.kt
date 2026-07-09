package sg.mesha.goatos.di

import android.content.Context
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import kotlinx.coroutines.runBlocking
import sg.mesha.goatos.BuildConfig
import sg.mesha.goatos.core.data.BootstrapCache
import sg.mesha.goatos.core.data.BootstrapCacheDao
import sg.mesha.goatos.core.data.AdherenceRepository
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.CalendarRepository
import sg.mesha.goatos.core.data.ControlTowerRepository
import sg.mesha.goatos.core.data.DefaultAdherenceRepository
import sg.mesha.goatos.core.data.DefaultBootstrapRepository
import sg.mesha.goatos.core.data.DefaultCalendarRepository
import sg.mesha.goatos.core.data.DefaultControlTowerRepository
import sg.mesha.goatos.core.data.DefaultExecutionRepository
import sg.mesha.goatos.core.data.DefaultTasksRepository
import sg.mesha.goatos.core.data.ExecutionRepository
import sg.mesha.goatos.core.data.GoatDatabase
import sg.mesha.goatos.core.data.TasksRepository
import sg.mesha.goatos.core.data.buildGoatDatabase
import sg.mesha.goatos.core.datastore.DataStoreSessionStore
import sg.mesha.goatos.core.datastore.SessionStore
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.NetworkFactory
import javax.inject.Singleton

/**
 * App-level DI wiring. The real Retrofit-backed [AppApi] hits the backend at
 * BuildConfig.API_BASE_URL, authorized with the current session bearer token
 * (read per request via the interceptor). Room + DataStore give offline-first
 * bootstrap + session persistence. Screens' ViewModels consume the repositories.
 */
@Module
@InstallIn(SingletonComponent::class)
object AppModule {

    @Provides
    @Singleton
    fun provideDatabase(@ApplicationContext context: Context): GoatDatabase =
        buildGoatDatabase(context)

    @Provides
    fun provideBootstrapCacheDao(db: GoatDatabase): BootstrapCacheDao = db.bootstrapCacheDao()

    @Provides
    @Singleton
    fun provideBootstrapCache(dao: BootstrapCacheDao): BootstrapCache = BootstrapCache(dao)

    @Provides
    @Singleton
    fun provideSessionStore(@ApplicationContext context: Context): SessionStore =
        DataStoreSessionStore(context)

    @Provides
    @Singleton
    fun provideAppApi(sessionStore: SessionStore): AppApi =
        NetworkFactory.appApi(BuildConfig.API_BASE_URL) {
            // Interceptor runs off the main thread; a blocking token read is safe here.
            runBlocking { sessionStore.currentToken() }
        }

    @Provides
    @Singleton
    fun provideBootstrapRepository(api: AppApi, cache: BootstrapCache): BootstrapRepository =
        DefaultBootstrapRepository(api, cache)

    @Provides
    @Singleton
    fun provideExecutionRepository(api: AppApi): ExecutionRepository = DefaultExecutionRepository(api)

    @Provides
    @Singleton
    fun provideCalendarRepository(api: AppApi): CalendarRepository = DefaultCalendarRepository(api)

    @Provides
    @Singleton
    fun provideControlTowerRepository(api: AppApi): ControlTowerRepository = DefaultControlTowerRepository(api)

    @Provides
    @Singleton
    fun provideTasksRepository(api: AppApi): TasksRepository = DefaultTasksRepository(api)

    @Provides
    @Singleton
    fun provideAdherenceRepository(api: AppApi): AdherenceRepository = DefaultAdherenceRepository(api)
}
