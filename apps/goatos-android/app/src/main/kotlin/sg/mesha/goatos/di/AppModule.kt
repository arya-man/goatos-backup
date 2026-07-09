package sg.mesha.goatos.di

import android.content.Context
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import sg.mesha.goatos.core.data.BootstrapCache
import sg.mesha.goatos.core.data.BootstrapCacheDao
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.DefaultBootstrapRepository
import sg.mesha.goatos.core.data.GoatDatabase
import sg.mesha.goatos.core.data.buildGoatDatabase
import sg.mesha.goatos.core.datastore.DataStoreSessionStore
import sg.mesha.goatos.core.datastore.SessionStore
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import javax.inject.Singleton

/**
 * App-level DI wiring. AppApi is the FakeAppApi until auth + a base URL are wired
 * (RetrofitAppApi + BearerAuthInterceptor are ready in core-network); swap the
 * provider here to flip the whole app to the live backend. Room + DataStore give
 * offline-first bootstrap + session persistence today.
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
    fun provideAppApi(): AppApi = FakeAppApi(chrome = "expanded")

    @Provides
    @Singleton
    fun provideBootstrapRepository(api: AppApi, cache: BootstrapCache): BootstrapRepository =
        DefaultBootstrapRepository(api, cache)
}
