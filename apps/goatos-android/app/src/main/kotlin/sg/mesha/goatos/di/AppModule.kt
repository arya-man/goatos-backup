package sg.mesha.goatos.di

import android.content.Context
import android.os.Build
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import sg.mesha.goatos.core.permissions.areNotificationsEnabled
import dagger.hilt.components.SingletonComponent
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import sg.mesha.goatos.BuildConfig
import sg.mesha.goatos.auth.currentFirebaseIdTokenBlocking
import sg.mesha.goatos.core.data.BootstrapCache
import sg.mesha.goatos.core.data.BootstrapCacheDao
import sg.mesha.goatos.core.data.capture.DefaultProofCaptureRepository
import sg.mesha.goatos.core.data.capture.DefaultScanAttemptRepository
import sg.mesha.goatos.core.data.capture.DefaultScanCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ScanAttemptRepository
import sg.mesha.goatos.core.data.capture.ScanCaptureRepository
import sg.mesha.goatos.core.database.capture.ProofCaptureDao
import sg.mesha.goatos.core.database.capture.RfidScanAttemptDao
import sg.mesha.goatos.core.database.capture.ScannedGoatDao
import sg.mesha.goatos.core.data.AdherenceRepository
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.CalendarRepository
import sg.mesha.goatos.core.data.ControlTowerRepository
import sg.mesha.goatos.core.data.DefaultAdherenceRepository
import sg.mesha.goatos.core.data.DefaultBootstrapRepository
import sg.mesha.goatos.core.data.DefaultCalendarRepository
import sg.mesha.goatos.core.data.DefaultControlTowerRepository
import sg.mesha.goatos.core.data.DefaultWeighingAlertsRepository
import sg.mesha.goatos.core.data.WeighingAlertsRepository
import sg.mesha.goatos.core.data.cache.WeighingAlertsCacheDao
import sg.mesha.goatos.core.data.DefaultExecutionRepository
import sg.mesha.goatos.core.data.DefaultTasksRepository
import sg.mesha.goatos.core.data.DefaultVaccinationInsightsRepository
import sg.mesha.goatos.core.data.DefaultVerificationRepository
import sg.mesha.goatos.core.data.ExecutionRepository
import sg.mesha.goatos.core.data.CountsApprovalRepository
import sg.mesha.goatos.core.data.CountsRepository
import sg.mesha.goatos.core.data.DefaultCountsApprovalRepository
import sg.mesha.goatos.core.data.AwaitingRfidRepository
import sg.mesha.goatos.core.data.DefaultAwaitingRfidRepository
import sg.mesha.goatos.core.data.FeedCompletionLocalStore
import sg.mesha.goatos.core.data.DefaultShiftingPendingRepository
import sg.mesha.goatos.core.data.DefaultWorkflowsRepository
import sg.mesha.goatos.core.data.ShiftingPendingRepository
import sg.mesha.goatos.core.data.WorkflowsRepository
import sg.mesha.goatos.core.data.DefaultCountsRepository
import sg.mesha.goatos.core.data.DefaultFeedRepository
import sg.mesha.goatos.core.data.FeedTransportRepository
import sg.mesha.goatos.core.data.FeedRepository
import sg.mesha.goatos.core.data.GoatDatabase
import sg.mesha.goatos.core.data.cache.CountsBreakdownMetaCacheDao
import sg.mesha.goatos.core.data.cache.CountsShiftingDestinationsCacheDao
import sg.mesha.goatos.core.data.cache.FeedDirectionMetaCacheDao
import sg.mesha.goatos.core.data.cache.FeedPackingMetaCacheDao
import sg.mesha.goatos.core.data.cache.HerdSummaryCacheDao
import sg.mesha.goatos.core.data.LogoutCoordinator
import sg.mesha.goatos.core.data.DefaultRosterRepository
import sg.mesha.goatos.core.data.RoomScreenCacheStore
import sg.mesha.goatos.core.data.RosterRepository
import sg.mesha.goatos.core.data.ScreenCacheStore
import sg.mesha.goatos.core.data.TasksRepository
import sg.mesha.goatos.core.data.VaccinationInsightsRepository
import sg.mesha.goatos.core.data.VerificationRepository
import sg.mesha.goatos.core.data.buildGoatDatabase
import sg.mesha.goatos.core.data.cache.AdherenceCacheDao
import sg.mesha.goatos.core.data.cache.CalendarCacheDao
import sg.mesha.goatos.core.data.cache.ControlTowerCacheDao
import sg.mesha.goatos.core.data.cache.CacheVersionStore
import sg.mesha.goatos.core.data.cache.ExecutionCacheVersionGate
import sg.mesha.goatos.core.data.cache.ExecutionRowsCacheDao
import sg.mesha.goatos.core.data.cache.ExecutionShedCacheDao
import sg.mesha.goatos.cache.SharedPrefsCacheVersionStore
import sg.mesha.goatos.core.data.cache.InsightsCoverageCacheDao
import sg.mesha.goatos.core.data.cache.InsightsGapsCacheDao
import sg.mesha.goatos.core.data.cache.RosterCoverageCacheDao
import sg.mesha.goatos.core.data.cache.RosterTimetableCacheDao
import sg.mesha.goatos.core.data.cache.ScanRosterRowDao
import sg.mesha.goatos.core.data.cache.ShedCompletionSummaryCacheDao
import sg.mesha.goatos.core.data.cache.TaskDetailCacheDao
import sg.mesha.goatos.core.data.cache.VerificationQueueCacheDao
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.analytics.FailureReportingOutboxTelemetryReporter
import sg.mesha.goatos.core.common.OutboxTelemetryReporter
import sg.mesha.goatos.core.data.sync.AndroidConnectivityGate
import sg.mesha.goatos.core.data.sync.AndroidConnectivitySource
import sg.mesha.goatos.core.data.sync.ConnectivityGate
import sg.mesha.goatos.core.data.sync.ConnectivitySyncTrigger
import sg.mesha.goatos.core.data.sync.DefaultSyncRepository
import sg.mesha.goatos.core.data.sync.ForegroundSyncController
import sg.mesha.goatos.core.data.sync.LocalBackendConnectivityGate
import sg.mesha.goatos.core.data.sync.OutboxStore
import sg.mesha.goatos.core.data.sync.OutboxWiper
import sg.mesha.goatos.core.data.sync.RoomOutboxStore
import sg.mesha.goatos.core.data.sync.SyncEngine
import sg.mesha.goatos.core.data.sync.SyncJobsCanceller
import sg.mesha.goatos.core.data.sync.SyncJobsScheduler
import sg.mesha.goatos.core.data.sync.SyncRetryScheduler
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.weighing.DefaultWeighingRepository
import sg.mesha.goatos.core.data.weighing.WeighingRepository
import sg.mesha.goatos.core.database.outbox.OutboxDao
import sg.mesha.goatos.core.database.outbox.OutboxDatabase
import sg.mesha.goatos.core.database.outbox.buildOutboxDatabase
import sg.mesha.goatos.core.datastore.DataStoreDeviceStore
import sg.mesha.goatos.core.datastore.DataStoreSessionStore
import sg.mesha.goatos.core.datastore.DeviceStore
import sg.mesha.goatos.core.datastore.SessionStore
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.NetworkFactory
import sg.mesha.goatos.capture.DelegatingPhotoCaptureSource
import sg.mesha.goatos.capture.DelegatingProofCaptureSource
import sg.mesha.goatos.capture.PhotoCaptureSource
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.network.NetworkTelemetryReporter
import sg.mesha.goatos.core.network.TelemetryInterceptor
import sg.mesha.goatos.rfid.BtHidScanSource
import sg.mesha.goatos.rfid.DefaultRfidInputTransform
import sg.mesha.goatos.rfid.KeyboardWedgeRfidReader
import sg.mesha.goatos.rfid.RfidInputTransform
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.rfid.ScanSource
import sg.mesha.goatos.push.PushLogoutCleanup
import sg.mesha.goatos.sync.AndroidForegroundSyncController
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
    @Singleton
    fun provideWeighingAlertsCacheDao(db: GoatDatabase): WeighingAlertsCacheDao = db.weighingAlertsCacheDao()

    @Provides
    fun provideExecutionRowsCacheDao(db: GoatDatabase): ExecutionRowsCacheDao = db.executionRowsCacheDao()

    @Provides
    fun provideExecutionShedCacheDao(db: GoatDatabase): ExecutionShedCacheDao = db.executionShedCacheDao()

    @Provides
    @Singleton
    fun provideCacheVersionStore(@ApplicationContext context: Context): CacheVersionStore =
        SharedPrefsCacheVersionStore(context)

    @Provides
    fun provideExecutionCacheVersionGate(
        rowsDao: ExecutionRowsCacheDao,
        shedDao: ExecutionShedCacheDao,
        store: CacheVersionStore,
    ): ExecutionCacheVersionGate = ExecutionCacheVersionGate(rowsDao, shedDao, store)

    @Provides
    fun provideScanRosterRowDao(db: GoatDatabase): ScanRosterRowDao = db.scanRosterRowDao()

    @Provides
    fun provideAdherenceCacheDao(db: GoatDatabase): AdherenceCacheDao = db.adherenceCacheDao()

    @Provides
    fun provideInsightsGapsCacheDao(db: GoatDatabase): InsightsGapsCacheDao = db.insightsGapsCacheDao()

    @Provides
    fun provideInsightsCoverageCacheDao(db: GoatDatabase): InsightsCoverageCacheDao = db.insightsCoverageCacheDao()

    @Provides
    fun provideRosterTimetableCacheDao(db: GoatDatabase): RosterTimetableCacheDao = db.rosterTimetableCacheDao()

    // Counts read models. The breakdown's paged rows are read through GoatDatabase directly by
    // its RemoteMediator (it needs a transaction across the item + remote-key DAOs), so only the
    // two fixed-size rollup DAOs are injected here.
    @Provides
    fun provideHerdSummaryCacheDao(db: GoatDatabase): HerdSummaryCacheDao = db.herdSummaryCacheDao()

    @Provides
    fun provideCountsBreakdownMetaCacheDao(db: GoatDatabase): CountsBreakdownMetaCacheDao =
        db.countsBreakdownMetaCacheDao()

    @Provides
    fun provideCountsShiftingDestinationsCacheDao(db: GoatDatabase): CountsShiftingDestinationsCacheDao =
        db.countsShiftingDestinationsCacheDao()

    // Feed read models. Like the Counts breakdown, each screen's paged rows are read through
    // GoatDatabase directly by its RemoteMediator (it needs a transaction across the item +
    // remote-key DAOs), so only the two fixed-size summary DAOs are injected here.
    @Provides
    fun provideFeedDirectionMetaCacheDao(db: GoatDatabase): FeedDirectionMetaCacheDao =
        db.feedDirectionMetaCacheDao()

    @Provides
    fun provideFeedPackingMetaCacheDao(db: GoatDatabase): FeedPackingMetaCacheDao =
        db.feedPackingMetaCacheDao()

    @Provides
    fun provideRosterCoverageCacheDao(db: GoatDatabase): RosterCoverageCacheDao = db.rosterCoverageCacheDao()

    @Provides
    fun provideTaskDetailCacheDao(db: GoatDatabase): TaskDetailCacheDao = db.taskDetailCacheDao()

    @Provides
    fun provideShedCompletionSummaryCacheDao(db: GoatDatabase): ShedCompletionSummaryCacheDao =
        db.shedCompletionSummaryCacheDao()

    @Provides
    fun provideScannedGoatDao(db: GoatDatabase): ScannedGoatDao = db.scannedGoatDao()

    @Provides
    fun provideRfidScanAttemptDao(db: GoatDatabase): RfidScanAttemptDao = db.rfidScanAttemptDao()

    @Provides
    fun provideProofCaptureDao(db: GoatDatabase): ProofCaptureDao = db.proofCaptureDao()

    @Provides
    fun provideVerificationQueueCacheDao(db: GoatDatabase): VerificationQueueCacheDao = db.verificationQueueCacheDao()

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
    fun provideRfidInputTransform(transform: DefaultRfidInputTransform): RfidInputTransform = transform

    @Provides
    @Singleton
    fun provideAppApi(sessionStore: SessionStore, networkTelemetryReporter: NetworkTelemetryReporter): AppApi =
        NetworkFactory.appApi(
            baseUrl = BuildConfig.API_BASE_URL,
            tokenProvider = {
                // OkHttp interceptors are synchronous: read the session-warmed snapshot rather
                // than blocking an interceptor thread on DataStore for every request.
                if (BuildConfig.FLAVOR == "dev") {
                    // Dev/local builds authenticate with the Gradle-injected HS256 bearer. After a
                    // clean reinstall or pm clear, the session gate can observe the DataStore token
                    // before this synchronous interceptor's in-memory snapshot is warm; falling back
                    // to the baked token prevents the first bootstrap from racing out unauthenticated.
                    sessionStore.cachedToken() ?: BuildConfig.DEV_BEARER_TOKEN.takeIf { it.isNotBlank() }
                } else {
                    currentFirebaseIdTokenBlocking()
                }
            },
            tenantIdProvider = { BuildConfig.TENANT_ID },
            localeProvider = { sessionStore.cachedLanguage() },
            // traceparent stamping + method/route/status/duration reporting (docs/TELEMETRY.md).
            // Always ENABLED. The comment here previously described exactly this intent — "a
            // flavor without a confirmed Firebase project still gets traceparent propagation
            // for backend correlation, only the Firebase Perf reporting half is gated" — while
            // the code passed TELEMETRY_ENABLED and so switched the WHOLE interceptor off,
            // taking traceparent correlation and every API-failure report with it. Gating now
            // lives where the comment always said it did: on the reporter's Firebase Perf
            // delegate, inside TelemetryModule.
            telemetryInterceptor = TelemetryInterceptor(enabled = true, reporter = networkTelemetryReporter),
        )

    @Provides
    @Singleton
    fun provideBootstrapRepository(
        api: AppApi,
        cache: BootstrapCache,
        deviceStore: DeviceStore,
        @ApplicationContext context: Context,
    ): BootstrapRepository =
        DefaultBootstrapRepository(
            api = api,
            cache = cache,
            deviceStore = deviceStore,
            appVersion = BuildConfig.VERSION_NAME,
            osVersion = Build.VERSION.RELEASE.orEmpty(),
            // Read at report time, not captured once: someone can switch notifications off in
            // system settings long after this repository was constructed, and the heartbeat that
            // follows must carry the CURRENT answer.
            notificationsEnabled = { areNotificationsEnabled(context) },
        )

    @Provides
    @Singleton
    fun provideExecutionRepository(
        api: AppApi,
        rowsDao: ExecutionRowsCacheDao,
        shedDao: ExecutionShedCacheDao,
        scanRosterRowDao: ScanRosterRowDao,
        database: GoatDatabase,
    ): ExecutionRepository = DefaultExecutionRepository(api, rowsDao, shedDao, scanRosterRowDao, database)

    @Provides
    @Singleton
    fun provideCalendarRepository(api: AppApi, dao: CalendarCacheDao, database: GoatDatabase): CalendarRepository =
        DefaultCalendarRepository(api, dao, database)

    @Provides
    @Singleton
    fun provideCountsRepository(
        api: AppApi,
        database: GoatDatabase,
        summaryDao: HerdSummaryCacheDao,
        breakdownMetaDao: CountsBreakdownMetaCacheDao,
        shiftingDestinationsDao: CountsShiftingDestinationsCacheDao,
    ): CountsRepository =
        DefaultCountsRepository(api, database, summaryDao, breakdownMetaDao, shiftingDestinationsDao)

    @Provides
    @Singleton
    fun provideCountsApprovalRepository(
        api: AppApi,
        database: GoatDatabase,
    ): CountsApprovalRepository = DefaultCountsApprovalRepository(api, database)

    @Provides
    @Singleton
    fun provideShiftingPendingRepository(
        api: AppApi,
        database: GoatDatabase,
    ): ShiftingPendingRepository = DefaultShiftingPendingRepository(api, database)

    @Provides
    @Singleton
    fun provideAwaitingRfidRepository(
        api: AppApi,
        database: GoatDatabase,
    ): AwaitingRfidRepository = DefaultAwaitingRfidRepository(api, database)

    @Provides
    @Singleton
    fun provideWorkflowsRepository(
        api: AppApi,
        database: GoatDatabase,
        outboxStore: OutboxStore,
    ): WorkflowsRepository = DefaultWorkflowsRepository(api, database, outboxStore)

    @Provides
    @Singleton
    fun provideFeedRepository(
        api: AppApi,
        database: GoatDatabase,
        directionMetaDao: FeedDirectionMetaCacheDao,
        packingMetaDao: FeedPackingMetaCacheDao,
    ): FeedRepository =
        DefaultFeedRepository(api, database, directionMetaDao, packingMetaDao)

    // App-scoped optimistic overlay for feed completions (offline-first badge ahead of the next
    // refresh). A process singleton, not persisted — the outbox is the durable command record.
    @Provides
    @Singleton
    fun provideFeedCompletionLocalStore(): FeedCompletionLocalStore = FeedCompletionLocalStore()

    @Provides @Singleton fun provideFeedTransportRepository(api: AppApi, database: GoatDatabase): FeedTransportRepository = FeedTransportRepository(api,database)

    @Provides
    @Singleton
    fun provideControlTowerRepository(api: AppApi, dao: ControlTowerCacheDao): ControlTowerRepository =
        DefaultControlTowerRepository(api, dao)

    @Provides
    @Singleton
    fun provideWeighingAlertsRepository(api: AppApi, dao: WeighingAlertsCacheDao): WeighingAlertsRepository =
        DefaultWeighingAlertsRepository(api, dao)

    @Provides
    @Singleton
    fun provideTasksRepository(
        api: AppApi,
        dao: TaskDetailCacheDao,
        shedCompletionSummaryDao: ShedCompletionSummaryCacheDao,
    ): TasksRepository =
        DefaultTasksRepository(api, dao, shedCompletionSummaryDao)

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

    @Provides
    @Singleton
    fun provideVerificationRepository(
        api: AppApi,
        dao: VerificationQueueCacheDao,
    ): VerificationRepository = DefaultVerificationRepository(api, dao)

    @Provides
    @Singleton
    fun provideWeighingRepository(
        api: AppApi,
        database: GoatDatabase,
        syncRepository: SyncRepository,
        appScope: CoroutineScope,
    ): WeighingRepository = DefaultWeighingRepository(
        api = api,
        tenantId = BuildConfig.TENANT_ID,
        rosterDao = database.weighingRosterDao(),
        observationDao = database.weighingObservationDao(),
        shedObservationDao = database.weighingShedObservationDao(),
        database = database,
        syncRepository = syncRepository,
        appScope = appScope,
    )

    // --- MOB-002 capture (docs/mobile/proof-capture-sync-and-e2e.md) -------------------
    // Room-first SSOT behind Submit's `goat_scan`/`video_proof` recording-form controls.
    // BtHidScanSource wraps the SAME RfidReaderPort singleton the shed-roster Scan screen
    // uses — Android owns one BT-HID connection; only one screen enables capture at a time.

    @Provides
    @Singleton
    fun provideScanSource(reader: RfidReaderPort): ScanSource = BtHidScanSource(reader)

    @Provides
    @Singleton
    fun provideDelegatingProofCaptureSource(): DelegatingProofCaptureSource = DelegatingProofCaptureSource()

    @Provides
    @Singleton
    fun provideProofCaptureSource(delegate: DelegatingProofCaptureSource): ProofCaptureSource = delegate

    @Provides
    @Singleton
    fun provideDelegatingPhotoCaptureSource(): DelegatingPhotoCaptureSource = DelegatingPhotoCaptureSource()

    @Provides
    @Singleton
    fun providePhotoCaptureSource(delegate: DelegatingPhotoCaptureSource): PhotoCaptureSource = delegate

    @Provides
    @Singleton
    fun provideScanCaptureRepository(
        dao: ScannedGoatDao,
        syncRepository: SyncRepository,
    ): ScanCaptureRepository = DefaultScanCaptureRepository(dao, syncRepository)

    @Provides
    @Singleton
    fun provideScanAttemptRepository(
        dao: RfidScanAttemptDao,
        syncRepository: SyncRepository,
        appScope: CoroutineScope,
    ): ScanAttemptRepository = DefaultScanAttemptRepository(dao, syncRepository, appScope)

    @Provides
    @Singleton
    fun provideProofCaptureRepository(
        dao: ProofCaptureDao,
        syncRepository: SyncRepository,
        appScope: CoroutineScope,
    ): ProofCaptureRepository = DefaultProofCaptureRepository(
        dao = dao,
        syncRepository = syncRepository,
        appScope = appScope,
    )

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

    /** Single binding for the active flavor's API base URL — see [ApiBaseUrl]. */
    @Provides
    @Singleton
    @ApiBaseUrl
    fun provideApiBaseUrl(): String = BuildConfig.API_BASE_URL

    @Provides
    @Singleton
    fun provideConnectivityGate(
        @ApplicationContext context: Context,
        @ApiBaseUrl apiBaseUrl: String,
    ): ConnectivityGate =
        LocalBackendConnectivityGate(
            delegate = AndroidConnectivityGate(context),
            apiBaseUrl = apiBaseUrl,
        )

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
    fun provideSessionRelauncher(
        impl: sg.mesha.goatos.boot.ProcessSessionRelauncher,
    ): sg.mesha.goatos.boot.SessionRelauncher = impl

    @Provides
    @Singleton
    fun provideLogoutCoordinator(
        api: AppApi,
        deviceStore: DeviceStore,
        sessionStore: SessionStore,
        screenCacheStore: ScreenCacheStore,
        outboxWiper: OutboxWiper,
        syncJobsCanceller: SyncJobsCanceller,
        pushLogoutCleanup: PushLogoutCleanup,
        feedCompletionLocalStore: FeedCompletionLocalStore,
    ): LogoutCoordinator = LogoutCoordinator(
        api = api,
        deviceStore = deviceStore,
        sessionStore = sessionStore,
        screenCacheStore = screenCacheStore,
        outboxWiper = outboxWiper,
        syncJobsCanceller = syncJobsCanceller,
        clearPushAndAnalyticsIdentity = pushLogoutCleanup::clear,
        feedCompletionLocalStore = feedCompletionLocalStore,
    )

    @Provides
    @Singleton
    fun provideSyncEngine(
        store: OutboxStore,
        api: AppApi,
        connectivityGate: ConnectivityGate,
        retryScheduler: SyncRetryScheduler,
        database: GoatDatabase,
        outboxTelemetry: OutboxTelemetryReporter,
    ): SyncEngine = SyncEngine(
        store = store,
        api = api,
        connectivityGate = connectivityGate,
        retryScheduler = retryScheduler,
        scannedGoatDao = database.scannedGoatDao(),
        weighingObservationDao = database.weighingObservationDao(),
        weighingShedObservationDao = database.weighingShedObservationDao(),
        telemetry = outboxTelemetry,
    )

    /**
     * Queue-lifecycle visibility (W-23). Bound unconditionally — unlike the network reporter
     * there is no vendor-gated variant to choose between: the logcat half must work on EVERY
     * flavor (that is the half whose absence made a stuck upload undiagnosable on-device), and
     * the Crashlytics/Analytics halves already degrade to no-ops when their seams are the
     * Noop implementations.
     */
    @Provides
    @Singleton
    fun provideOutboxTelemetryReporter(
        crashReporter: CrashReporter,
        analytics: AnalyticsPort,
    ): OutboxTelemetryReporter = FailureReportingOutboxTelemetryReporter(
        crashReporter = crashReporter,
        analytics = analytics,
    )

    // Drive/Photos-style background upload foreground service (MOB-002 §3,
    // sg.mesha.goatos.sync.UploadForegroundService). The port lives in :core:core-data
    // (framework-free); this is its ONLY Android-touching implementation, mirroring how
    // SyncWorkScheduler is the sole implementation of the SyncRetryScheduler/SyncJobs* ports.

    @Provides
    @Singleton
    fun provideForegroundSyncController(
        @ApplicationContext context: Context,
        syncWorkScheduler: SyncWorkScheduler,
        analytics: AnalyticsPort,
    ): ForegroundSyncController =
        AndroidForegroundSyncController(context, syncWorkScheduler, analytics)

    @Provides
    @Singleton
    fun provideSyncRepository(
        store: OutboxStore,
        engine: SyncEngine,
        connectivityGate: ConnectivityGate,
        appScope: CoroutineScope,
        foregroundSyncController: ForegroundSyncController,
        outboxTelemetry: OutboxTelemetryReporter,
    ): SyncRepository = DefaultSyncRepository(
        store = store,
        engine = engine,
        connectivityGate = connectivityGate,
        appScope = appScope,
        foregroundSyncController = foregroundSyncController,
        telemetry = outboxTelemetry,
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
