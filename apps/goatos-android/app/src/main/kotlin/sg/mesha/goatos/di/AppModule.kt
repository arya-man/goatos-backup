package sg.mesha.goatos.di

import android.content.Context
import android.os.Build
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import sg.mesha.goatos.BuildConfig
import sg.mesha.goatos.auth.currentFirebaseIdTokenBlocking
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
import sg.mesha.goatos.core.data.DefaultVaccinationInsightsRepository
import sg.mesha.goatos.core.data.ExecutionRepository
import sg.mesha.goatos.core.data.GoatDatabase
import sg.mesha.goatos.core.data.DefaultRosterRepository
import sg.mesha.goatos.core.data.RosterRepository
import sg.mesha.goatos.core.data.TasksRepository
import sg.mesha.goatos.core.data.VaccinationInsightsRepository
import sg.mesha.goatos.core.data.buildGoatDatabase
import sg.mesha.goatos.core.data.sync.AndroidConnectivityGate
import sg.mesha.goatos.core.data.sync.AndroidConnectivitySource
import sg.mesha.goatos.core.data.sync.ConnectivityGate
import sg.mesha.goatos.core.data.sync.ConnectivitySyncTrigger
import sg.mesha.goatos.core.data.sync.DefaultSyncRepository
import sg.mesha.goatos.core.data.sync.OutboxStore
import sg.mesha.goatos.core.data.sync.RoomOutboxStore
import sg.mesha.goatos.core.data.sync.SyncEngine
import sg.mesha.goatos.core.data.sync.SyncRetryScheduler
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.database.outbox.OutboxDao
import sg.mesha.goatos.core.database.outbox.OutboxDatabase
import sg.mesha.goatos.core.database.outbox.buildOutboxDatabase
import sg.mesha.goatos.core.datastore.DataStoreDeviceStore
import sg.mesha.goatos.core.datastore.DataStoreSessionStore
import sg.mesha.goatos.core.datastore.DeviceStore
import sg.mesha.goatos.core.datastore.SessionStore
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.NetworkFactory
import sg.mesha.goatos.rfid.KeyboardWedgeRfidReader
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.sync.SyncWorkScheduler
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
    fun provideDeviceStore(@ApplicationContext context: Context): DeviceStore =
        DataStoreDeviceStore(context)

    @Provides
    @Singleton
    fun provideRfidReaderPort(@ApplicationContext context: Context): RfidReaderPort =
        KeyboardWedgeRfidReader(context)

    @Provides
    @Singleton
    fun provideAppApi(sessionStore: SessionStore): AppApi =
        NetworkFactory.appApi(
            baseUrl = BuildConfig.API_BASE_URL,
            tokenProvider = {
                // Interceptor runs off the main thread; a blocking token read is safe here.
                if (BuildConfig.FLAVOR == "dev") {
                    runBlocking { sessionStore.currentToken() }
                } else {
                    currentFirebaseIdTokenBlocking()
                }
            },
            tenantIdProvider = { BuildConfig.TENANT_ID },
            localeProvider = { runBlocking { sessionStore.currentLanguage() } },
        )

    @Provides
    @Singleton
    fun provideBootstrapRepository(
        api: AppApi,
        cache: BootstrapCache,
        deviceStore: DeviceStore,
    ): BootstrapRepository =
        DefaultBootstrapRepository(
            api = api,
            cache = cache,
            deviceStore = deviceStore,
            appVersion = BuildConfig.VERSION_NAME,
            osVersion = Build.VERSION.RELEASE.orEmpty(),
        )

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

    @Provides
    @Singleton
    fun provideVaccinationInsightsRepository(api: AppApi): VaccinationInsightsRepository =
        DefaultVaccinationInsightsRepository(api)

    @Provides
    @Singleton
    fun provideRosterRepository(api: AppApi): RosterRepository = DefaultRosterRepository(api)

    // --- Offline sync engine (outbox) --------------------------------------------------
    // The engine runs on this Hilt-provided, app-lifetime CoroutineScope (a Singleton, never
    // GlobalScope), triggered on enqueue and on reconnect. A WorkManager SyncWorker (wired in
    // GoatOsApplication) is the process-death backstop that calls the SAME drainOnce().

    @Provides
    @Singleton
    fun provideOutboxDatabase(@ApplicationContext context: Context): OutboxDatabase =
        buildOutboxDatabase(context)

    @Provides
    fun provideOutboxDao(db: OutboxDatabase): OutboxDao = db.outboxDao()

    @Provides
    @Singleton
    fun provideOutboxStore(dao: OutboxDao): OutboxStore = RoomOutboxStore(dao)

    @Provides
    @Singleton
    fun provideAppScope(): CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.Default)

    @Provides
    @Singleton
    fun provideConnectivityGate(@ApplicationContext context: Context): ConnectivityGate =
        AndroidConnectivityGate(context)

    @Provides
    @Singleton
    fun provideSyncRetryScheduler(scheduler: SyncWorkScheduler): SyncRetryScheduler = scheduler

    @Provides
    @Singleton
    fun provideSyncEngine(
        store: OutboxStore,
        api: AppApi,
        connectivityGate: ConnectivityGate,
        retryScheduler: SyncRetryScheduler,
    ): SyncEngine = SyncEngine(
        store = store,
        api = api,
        connectivityGate = connectivityGate,
        retryScheduler = retryScheduler,
    )

    @Provides
    @Singleton
    fun provideSyncRepository(
        store: OutboxStore,
        engine: SyncEngine,
        connectivityGate: ConnectivityGate,
        appScope: CoroutineScope,
    ): SyncRepository = DefaultSyncRepository(
        store = store,
        engine = engine,
        connectivityGate = connectivityGate,
        appScope = appScope,
    )

    // Reads back the concrete DefaultSyncRepository (same @Singleton instance returned
    // above) purely to forward connectivity changes into its display flag — this cast
    // never leaks past DI wiring; SyncRepository callers only ever see the port.
    @Provides
    @Singleton
    fun provideConnectivitySyncTrigger(
        @ApplicationContext context: Context,
        appScope: CoroutineScope,
        engine: SyncEngine,
        syncRepository: SyncRepository,
    ): ConnectivitySyncTrigger {
        val repo = syncRepository as? DefaultSyncRepository
        return ConnectivitySyncTrigger(source = AndroidConnectivitySource(context)) { online ->
            repo?.notifyConnectivityChanged(online)
            if (online) appScope.launch { engine.drainOnce() }
        }
    }
}
