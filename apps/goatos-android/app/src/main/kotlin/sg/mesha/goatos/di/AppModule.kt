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
import sg.mesha.goatos.core.data.LogoutCoordinator
import sg.mesha.goatos.core.data.DefaultRosterRepository
import sg.mesha.goatos.core.data.RoomScreenCacheStore
import sg.mesha.goatos.core.data.RosterRepository
import sg.mesha.goatos.core.data.ScreenCacheStore
import sg.mesha.goatos.core.data.TasksRepository
import sg.mesha.goatos.core.data.VaccinationInsightsRepository
import sg.mesha.goatos.core.data.buildGoatDatabase
import sg.mesha.goatos.core.data.cache.AdherenceCacheDao
import sg.mesha.goatos.core.data.cache.CalendarCacheDao
import sg.mesha.goatos.core.data.cache.ControlTowerCacheDao
import sg.mesha.goatos.core.data.cache.ExecutionRowsCacheDao
import sg.mesha.goatos.core.data.cache.ExecutionShedCacheDao
import sg.mesha.goatos.core.data.cache.InsightsCoverageCacheDao
import sg.mesha.goatos.core.data.cache.InsightsGapsCacheDao
import sg.mesha.goatos.core.data.cache.RosterCoverageCacheDao
import sg.mesha.goatos.core.data.cache.RosterTimetableCacheDao
import sg.mesha.goatos.core.data.cache.ScanRosterCacheDao
import sg.mesha.goatos.core.data.cache.TaskDetailCacheDao
import sg.mesha.goatos.core.data.sync.AndroidConnectivityGate
import sg.mesha.goatos.core.data.sync.AndroidConnectivitySource
import sg.mesha.goatos.core.data.sync.ConnectivityGate
import sg.mesha.goatos.core.data.sync.ConnectivitySyncTrigger
import sg.mesha.goatos.core.data.sync.DefaultSyncRepository
import sg.mesha.goatos.core.data.sync.OutboxStore
import sg.mesha.goatos.core.data.sync.OutboxWiper
import sg.mesha.goatos.core.data.sync.RoomOutboxStore
import sg.mesha.goatos.core.data.sync.SyncEngine
import sg.mesha.goatos.core.data.sync.SyncJobsCanceller
import sg.mesha.goatos.core.data.sync.SyncJobsScheduler
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

    // --- Offline-first read-screen caches (docs/decisions/android-offline-first.md) -----
    // One JSON-blob-by-scope cache table per screen-facing read model; each DAO is handed
    // straight to its Default*Repository alongside the shared AppApi.

    @Provides
    fun provideCalendarCacheDao(db: GoatDatabase): CalendarCacheDao = db.calendarCacheDao()

    @Provides
    fun provideControlTowerCacheDao(db: GoatDatabase): ControlTowerCacheDao = db.controlTowerCacheDao()

    @Provides
    fun provideExecutionRowsCacheDao(db: GoatDatabase): ExecutionRowsCacheDao = db.executionRowsCacheDao()

    @Provides
    fun provideExecutionShedCacheDao(db: GoatDatabase): ExecutionShedCacheDao = db.executionShedCacheDao()

    @Provides
    fun provideScanRosterCacheDao(db: GoatDatabase): ScanRosterCacheDao = db.scanRosterCacheDao()

    @Provides
    fun provideAdherenceCacheDao(db: GoatDatabase): AdherenceCacheDao = db.adherenceCacheDao()

    @Provides
    fun provideInsightsGapsCacheDao(db: GoatDatabase): InsightsGapsCacheDao = db.insightsGapsCacheDao()

    @Provides
    fun provideInsightsCoverageCacheDao(db: GoatDatabase): InsightsCoverageCacheDao = db.insightsCoverageCacheDao()

    @Provides
    fun provideRosterTimetableCacheDao(db: GoatDatabase): RosterTimetableCacheDao = db.rosterTimetableCacheDao()

    @Provides
    fun provideRosterCoverageCacheDao(db: GoatDatabase): RosterCoverageCacheDao = db.rosterCoverageCacheDao()

    @Provides
    fun provideTaskDetailCacheDao(db: GoatDatabase): TaskDetailCacheDao = db.taskDetailCacheDao()

    @Provides
    @Singleton
    fun provideScreenCacheStore(db: GoatDatabase): ScreenCacheStore = RoomScreenCacheStore(db)

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
    fun provideExecutionRepository(
        api: AppApi,
        rowsDao: ExecutionRowsCacheDao,
        shedDao: ExecutionShedCacheDao,
        scanRosterDao: ScanRosterCacheDao,
    ): ExecutionRepository = DefaultExecutionRepository(api, rowsDao, shedDao, scanRosterDao)

    @Provides
    @Singleton
    fun provideCalendarRepository(api: AppApi, dao: CalendarCacheDao): CalendarRepository =
        DefaultCalendarRepository(api, dao)

    @Provides
    @Singleton
    fun provideControlTowerRepository(api: AppApi, dao: ControlTowerCacheDao): ControlTowerRepository =
        DefaultControlTowerRepository(api, dao)

    @Provides
    @Singleton
    fun provideTasksRepository(api: AppApi, dao: TaskDetailCacheDao): TasksRepository =
        DefaultTasksRepository(api, dao)

    @Provides
    @Singleton
    fun provideAdherenceRepository(api: AppApi, dao: AdherenceCacheDao): AdherenceRepository =
        DefaultAdherenceRepository(api, dao)

    @Provides
    @Singleton
    fun provideVaccinationInsightsRepository(
        api: AppApi,
        gapsDao: InsightsGapsCacheDao,
        coverageDao: InsightsCoverageCacheDao,
    ): VaccinationInsightsRepository =
        DefaultVaccinationInsightsRepository(api, gapsDao, coverageDao)

    @Provides
    @Singleton
    fun provideRosterRepository(
        api: AppApi,
        timetableDao: RosterTimetableCacheDao,
        coverageDao: RosterCoverageCacheDao,
    ): RosterRepository = DefaultRosterRepository(api, timetableDao, coverageDao)

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
    fun provideOutboxWiper(dao: OutboxDao): OutboxWiper = OutboxWiper { dao.clearAll() }

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
    fun provideSyncJobsCanceller(scheduler: SyncWorkScheduler): SyncJobsCanceller = scheduler

    @Provides
    @Singleton
    fun provideSyncJobsScheduler(scheduler: SyncWorkScheduler): SyncJobsScheduler = scheduler

    @Provides
    @Singleton
    fun provideLogoutCoordinator(
        api: AppApi,
        deviceStore: DeviceStore,
        sessionStore: SessionStore,
        screenCacheStore: ScreenCacheStore,
        outboxWiper: OutboxWiper,
        syncJobsCanceller: SyncJobsCanceller,
    ): LogoutCoordinator = LogoutCoordinator(
        api = api,
        deviceStore = deviceStore,
        sessionStore = sessionStore,
        screenCacheStore = screenCacheStore,
        outboxWiper = outboxWiper,
        syncJobsCanceller = syncJobsCanceller,
    )

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
