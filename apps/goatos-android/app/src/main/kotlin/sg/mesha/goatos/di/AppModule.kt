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
import kotlinx.coroutines.CoroutineExceptionHandler
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
import sg.mesha.goatos.core.data.capture.FileSystemProofArtifactValidator
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureTelemetry
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
import sg.mesha.goatos.core.data.DefaultVaccinationAlertsRepository
import sg.mesha.goatos.core.data.DefaultWeighingAlertsRepository
import sg.mesha.goatos.core.data.VaccinationAlertsRepository
import sg.mesha.goatos.core.data.WeighingAlertsRepository
import sg.mesha.goatos.core.data.cache.VaccinationAlertsCacheDao
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
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.DefaultCaptureDraftRepository
import sg.mesha.goatos.core.data.DefaultShiftingPendingRepository
import sg.mesha.goatos.core.data.DefaultWorkflowsRepository
import sg.mesha.goatos.core.data.DefaultHealthRepository
import sg.mesha.goatos.core.data.HealthRepository
import sg.mesha.goatos.core.data.ShiftingPendingRepository
import sg.mesha.goatos.core.data.WorkflowsRepository
import sg.mesha.goatos.core.data.DefaultCountsRepository
import sg.mesha.goatos.core.data.DefaultMilkPreparationRepository
import sg.mesha.goatos.core.data.MilkPreparationRepository
import sg.mesha.goatos.core.data.DefaultMilkFeedingRepository
import sg.mesha.goatos.core.data.MilkFeedingRepository
import sg.mesha.goatos.core.data.DefaultFeedRepository
import sg.mesha.goatos.core.data.FeedTransportRepository
import sg.mesha.goatos.core.data.FeedRepository
import sg.mesha.goatos.core.data.GoatDatabase
import sg.mesha.goatos.core.data.cache.CountsBreakdownMetaCacheDao
import sg.mesha.goatos.core.data.cache.CountsShiftingDestinationsCacheDao
import sg.mesha.goatos.core.data.cache.FeedDirectionMetaCacheDao
import sg.mesha.goatos.core.data.cache.FeedPackingMetaCacheDao
import sg.mesha.goatos.core.data.cache.FeedWastageMetaCacheDao
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
import sg.mesha.goatos.core.data.sync.SubmittedGrainsSource
import sg.mesha.goatos.core.data.sync.AndroidConnectivityGate
import sg.mesha.goatos.core.data.sync.AndroidConnectivitySource
import sg.mesha.goatos.core.data.sync.ConnectivityGate
import sg.mesha.goatos.core.data.sync.ConnectivitySyncTrigger
import sg.mesha.goatos.core.data.sync.DefaultSyncRepository
import sg.mesha.goatos.core.data.sync.ForegroundSyncController
import sg.mesha.goatos.core.data.sync.LocalBackendConnectivityGate
import sg.mesha.goatos.core.data.sync.MediaStoreGalleryProofSaver
import sg.mesha.goatos.core.data.sync.OutboxStore
import sg.mesha.goatos.core.data.sync.OutboxWiper
import sg.mesha.goatos.core.data.sync.RoomOutboxStore
import sg.mesha.goatos.core.data.sync.SyncEngine
import sg.mesha.goatos.core.data.sync.milkFeedingSubmitRefreshHook
import sg.mesha.goatos.core.data.sync.milkPreparationSubmitRefreshHook
import sg.mesha.goatos.core.data.sync.countsShiftingRefreshHook
import sg.mesha.goatos.core.data.sync.countsBirthRefreshHook
import sg.mesha.goatos.core.data.sync.countsDeathRefreshHook
import sg.mesha.goatos.core.data.sync.countsApprovalApproveRefreshHook
import sg.mesha.goatos.core.data.sync.countsApprovalRejectRefreshHook
import sg.mesha.goatos.core.data.sync.shiftingCompleteRefreshHook
import sg.mesha.goatos.core.data.sync.shiftingCancelRefreshHook
import sg.mesha.goatos.core.data.sync.countsPromoteIdentifierRefreshHook
import sg.mesha.goatos.core.data.sync.healthCaseOpenRefreshHook
import sg.mesha.goatos.core.data.sync.healthTreatmentCompleteFailureHook
import sg.mesha.goatos.core.data.sync.healthTreatmentCompleteRefreshHook
import sg.mesha.goatos.core.data.sync.workflowActionAnswerFailureHook
import sg.mesha.goatos.core.data.sync.workflowActionCompleteFailureHook
import sg.mesha.goatos.core.database.outbox.OutboxOpType
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
import sg.mesha.goatos.capture.AppProofMediaProcessor
import sg.mesha.goatos.capture.AppProofLocationProvider
import sg.mesha.goatos.capture.PhotoCaptureSource
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.network.NetworkTelemetryReporter
import sg.mesha.goatos.core.network.RequestMetadata
import sg.mesha.goatos.core.network.TelemetryInterceptor
import sg.mesha.goatos.rfid.BtHidScanSource
import sg.mesha.goatos.rfid.DefaultRfidInputTransform
import sg.mesha.goatos.rfid.KeyboardWedgeRfidReader
import sg.mesha.goatos.rfid.RfidInputTransform
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.rfid.ScanSource
import sg.mesha.goatos.push.PushLogoutCleanup
import sg.mesha.goatos.sync.AndroidForegroundSyncController
import sg.mesha.goatos.analytics.BackendAnalyticsAdapter
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
    @Singleton
    fun provideWeighingExportFileWriter(
        @ApplicationContext context: Context,
        crashReporter: sg.mesha.goatos.core.analytics.CrashReporter,
    ): sg.mesha.goatos.export.WeighingExportFileWriter =
        sg.mesha.goatos.export.AndroidWeighingExportFileWriter(context, crashReporter)

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
    @Singleton
    fun provideVaccinationAlertsCacheDao(db: GoatDatabase): VaccinationAlertsCacheDao =
        db.vaccinationAlertsCacheDao()

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
    @Singleton
    fun provideFeedWastageMetaCacheDao(db: GoatDatabase): FeedWastageMetaCacheDao =
        db.feedWastageMetaCacheDao()

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
    fun provideAppApi(
        sessionStore: SessionStore,
        deviceStore: DeviceStore,
        networkTelemetryReporter: NetworkTelemetryReporter,
    ): AppApi =
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
    fun provideMilkPreparationRepository(
        api: AppApi,
        breakdownMetaDao: CountsBreakdownMetaCacheDao,
    ): MilkPreparationRepository = DefaultMilkPreparationRepository(api, breakdownMetaDao)

    @Provides
    @Singleton
    fun provideMilkFeedingRepository(
        api: AppApi,
        breakdownMetaDao: CountsBreakdownMetaCacheDao,
    ): MilkFeedingRepository = DefaultMilkFeedingRepository(api, breakdownMetaDao)

    @Provides
    @Singleton
    fun provideCountsApprovalRepository(
        api: AppApi,
        database: GoatDatabase,
    ): CountsApprovalRepository = DefaultCountsApprovalRepository(api, database)

    /**
     * The shared durable capture-draft store. Every capture screen writes its recorded proofs and
     * submit key here so Back + re-entry cannot lose them (see CaptureEvidenceDraftEntity).
     */
    @Provides
    @Singleton
    fun provideCaptureDraftRepository(
        database: GoatDatabase,
    ): CaptureDraftRepository = DefaultCaptureDraftRepository(database)

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
    fun provideHealthRepository(
        api: AppApi,
        database: GoatDatabase,
    ): HealthRepository = DefaultHealthRepository(api, database)

    @Provides
    @Singleton
    fun provideFeedRepository(
        wastageMetaDao: FeedWastageMetaCacheDao,
        api: AppApi,
        database: GoatDatabase,
        directionMetaDao: FeedDirectionMetaCacheDao,
        packingMetaDao: FeedPackingMetaCacheDao,
    ): FeedRepository =
        DefaultFeedRepository(api, database, directionMetaDao, packingMetaDao, wastageMetaDao)

    @Provides
    @Singleton
    fun providePcCareRepository(
        api: AppApi,
        database: GoatDatabase,
        syncRepository: sg.mesha.goatos.core.data.sync.SyncRepository,
    ): sg.mesha.goatos.core.data.PcCareRepository = sg.mesha.goatos.core.data.DefaultPcCareRepository(
        api = api,
        database = database,
        detailDao = database.pcCareTaskDetailCacheDao(),
        animalDao = database.pcCareAnimalRowDao(),
        syncRepository = syncRepository,
    )

    /**
     * Toxin module reads (module toxin, maintainer decision 2026-08-25). Room-backed and
     * offline-first; the WRITES ride the outbox, so unlike PC Care this repository takes no
     * SyncRepository and creates no Dagger cycle.
     */
    @Provides
    @Singleton
    fun provideToxinRepository(
        api: AppApi,
        database: GoatDatabase,
    ): sg.mesha.goatos.core.data.ToxinRepository =
        sg.mesha.goatos.core.data.DefaultToxinRepository(api = api, database = database)

    // App-scoped optimistic overlay for feed completions (offline-first badge ahead of the next
    // refresh). A process singleton, not persisted — the outbox is the durable command record.
    @Provides
    @Singleton
    fun provideFeedCompletionLocalStore(): FeedCompletionLocalStore = FeedCompletionLocalStore()

    @Provides @Singleton fun provideFeedTransportRepository(api: AppApi, database: GoatDatabase): FeedTransportRepository = FeedTransportRepository(api,database)

    /** The narrow live-status surface FeedTransportCaptureViewModel depends on — same singleton
     *  instance as [provideFeedTransportRepository], bound to its slimmer interface so tests can
     *  fake just that surface without a real [GoatDatabase]. */
    @Provides @Singleton fun provideFeedTransportStatusSource(repository: FeedTransportRepository): sg.mesha.goatos.core.data.FeedTransportStatusSource = repository

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
    fun provideVaccinationAlertsRepository(
        api: AppApi,
        dao: VaccinationAlertsCacheDao,
    ): VaccinationAlertsRepository = DefaultVaccinationAlertsRepository(api, dao)

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
        @ApplicationContext context: Context,
        dao: ProofCaptureDao,
        syncRepository: SyncRepository,
        appScope: CoroutineScope,
        analytics: AnalyticsPort,
        mediaProcessor: AppProofMediaProcessor,
        locationProvider: AppProofLocationProvider,
    ): ProofCaptureRepository = DefaultProofCaptureRepository(
        dao = dao,
        syncRepository = syncRepository,
        appScope = appScope,
        mediaProcessor = mediaProcessor,
        locationProvider = locationProvider,
        proofArtifactValidator = FileSystemProofArtifactValidator(),
        galleryProofSaver = MediaStoreGalleryProofSaver(context),
        telemetry = ProofCaptureTelemetry { event, props -> analytics.track(event, props) },
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
    fun provideAppScope(crashReporter: CrashReporter): CoroutineScope {
        val handler = CoroutineExceptionHandler { _, error ->
            crashReporter.recordException(error, "app-scope background job failed")
        }
        return CoroutineScope(SupervisorJob() + Dispatchers.Default + handler)
    }

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
    fun provideSubmittedGrainsSource(syncRepository: SyncRepository): SubmittedGrainsSource =
        // Bound to the OUTBOX-derived projection: a submit that succeeds or dies leaves the active
        // set by itself, so the badge retracts with nothing to clear.
        SubmittedGrainsSource { syncRepository.observeSubmittedForReviewGrains() }

    @Provides
    @Singleton
    fun provideSyncEngine(
        store: OutboxStore,
        api: AppApi,
        connectivityGate: ConnectivityGate,
        retryScheduler: SyncRetryScheduler,
        database: GoatDatabase,
        outboxTelemetry: OutboxTelemetryReporter,
        feedRepository: FeedRepository,
        feedTransportRepository: FeedTransportRepository,
        milkFeedingRepository: MilkFeedingRepository,
        milkPreparationRepository: MilkPreparationRepository,
        countsRepository: CountsRepository,
        countsApprovalRepository: CountsApprovalRepository,
        awaitingRfidRepository: AwaitingRfidRepository,
        shiftingPendingRepository: ShiftingPendingRepository,
        workflowsRepository: WorkflowsRepository,
        healthRepository: HealthRepository,
        // Provider, NOT the repository: PcCareRepository -> SyncRepository -> SyncEngine would
        // otherwise be a Dagger dependency cycle (PC Care is the one module whose repository
        // enqueues its own outbox writes). The handle defers provider.get() to CALL time, after
        // the graph is fully built, so construction never recurses.
        pcCareRepositoryProvider: javax.inject.Provider<sg.mesha.goatos.core.data.PcCareRepository>,
        // Without this, toxinRepository defaults to null in the constructor and the
        // TOXIN_STEP_COMPLETE/TOXIN_SUBMIT reconciliation silently no-ops in production: every
        // step write would land on the server while the phone kept rendering the PREVIOUS step
        // states until the next manual refresh. Same defect class as feedRepository above.
        toxinRepository: sg.mesha.goatos.core.data.ToxinRepository,
    ): SyncEngine {
        val pcCareRepository = DeferredPcCareRepository(pcCareRepositoryProvider)
        return SyncEngine(
        store = store,
        api = api,
        connectivityGate = connectivityGate,
        retryScheduler = retryScheduler,
        scannedGoatDao = database.scannedGoatDao(),
        weighingObservationDao = database.weighingObservationDao(),
        weighingShedObservationDao = database.weighingShedObservationDao(),
        weighingTransitionEpochDao = database.weighingTransitionEpochDao(),
        // Without this, feedRepository defaults to null in the constructor and
        // FEED_DISTRIBUTION_COMPLETE/FEED_PACKING_COMPLETE reconciliation silently no-ops in
        // production (feedRepository?.persist... does nothing) — the exact bug this wiring fixes.
        feedRepository = feedRepository,
        feedTransportRepository = feedTransportRepository,
        // Same rationale as feedRepository above: nullable constructor defaults silently no-op
        // PC_CARE_SCAN_ADD/PC_CARE_TASK_SUBMIT reconciliation in production without this wiring.
        pcCareAnimalRowDao = database.pcCareAnimalRowDao(),
        pcCareRepository = pcCareRepository,
        toxinRepository = toxinRepository,
        telemetry = outboxTelemetry,
        // Whole-page-blob reconcile: these opTypes affect cached lists/envelopes with no server-truth
        // row to write directly into. The reconcile is "refresh the page" or "forget the row",
        // never "write a result". Registered here (repo/DI layer), never in a ViewModel — see
        // PostSuccessRefreshHook. Counts-family operations (birth/death/shifting/approval) + Milk
        // operations (feeding/preparation) all follow this pattern.
        postSuccessRefreshHooks = mapOf(
            // Milk operations
            OutboxOpType.MILK_FEEDING_SUBMIT to milkFeedingSubmitRefreshHook(milkFeedingRepository),
            OutboxOpType.MILK_PREPARATION_SUBMIT to milkPreparationSubmitRefreshHook(milkPreparationRepository),
            // Counts family operations
            OutboxOpType.COUNTS_SHIFTING to countsShiftingRefreshHook(countsRepository),
            OutboxOpType.COUNTS_BIRTH to countsBirthRefreshHook(countsRepository),
            OutboxOpType.COUNTS_DEATH to countsDeathRefreshHook(countsRepository),
            OutboxOpType.COUNTS_APPROVAL_APPROVE to countsApprovalApproveRefreshHook(countsApprovalRepository),
            OutboxOpType.COUNTS_APPROVAL_REJECT to countsApprovalRejectRefreshHook(countsApprovalRepository),
            OutboxOpType.SHIFTING_COMPLETE to shiftingCompleteRefreshHook(shiftingPendingRepository),
            OutboxOpType.SHIFTING_CANCEL to shiftingCancelRefreshHook(shiftingPendingRepository),
            OutboxOpType.COUNTS_PROMOTE_IDENTIFIER to countsPromoteIdentifierRefreshHook(
                countsRepository,
                awaitingRfidRepository,
            ),
            // Health operations
            OutboxOpType.HEALTH_CASE_OPEN to healthCaseOpenRefreshHook(healthRepository),
            OutboxOpType.HEALTH_TREATMENT_COMPLETE to healthTreatmentCompleteRefreshHook(healthRepository),
            // PC Care: a successful slot registration re-polls the task's captures so the server's
            // per-slot truth (proof ref, attribution) lands back in the Room rows screens observe.
            OutboxOpType.PC_CARE_SLOT_REGISTER to sg.mesha.goatos.core.data.sync.pcCareSlotRegisterRefreshHook(pcCareRepository),
        ),
        preSuccessRefreshHooks = mapOf(
            OutboxOpType.HEALTH_CASE_OPEN to healthCaseOpenRefreshHook(healthRepository),
        ),
        postTerminalFailureHooks = mapOf(
            OutboxOpType.HEALTH_TREATMENT_COMPLETE to healthTreatmentCompleteFailureHook(healthRepository),
            OutboxOpType.WORKFLOW_ACTION_ANSWER to workflowActionAnswerFailureHook(workflowsRepository),
            OutboxOpType.WORKFLOW_ACTION_COMPLETE to workflowActionCompleteFailureHook(workflowsRepository),
        ),
        )
    }

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
        @ApplicationContext context: Context,
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
        connectivityGate: ConnectivityGate,
        backendAnalyticsAdapter: BackendAnalyticsAdapter,
    ): ConnectivitySyncTrigger {
        val repo = syncRepository as? DefaultSyncRepository
        return ConnectivitySyncTrigger(source = AndroidConnectivitySource(context)) { platformOnline ->
            // Reconcile the RAW platform signal with the gate the rest of sync already uses.
            // AndroidConnectivitySource reports the platform's VALIDATED-internet verdict, which
            // is false on a device whose only route to the API is an adb-reverse loopback (device
            // proof runs) or a network without validated internet. Pushing that straight through
            // raised "You're offline — records save on this phone and sync when you reconnect" on
            // a phone that was reaching the backend fine, on the same screen that had just
            // rendered freshly fetched data.
            val online = platformOnline || connectivityGate.isOnline()
            repo?.notifyConnectivityChanged(online)
            if (online) appScope.launch {
                engine.drainOnce()
                runCatching { backendAnalyticsAdapter.drainQueue() }
            }
        }
    }
}

/**
 * Call-time-deferred [sg.mesha.goatos.core.data.PcCareRepository] handle that breaks the ONE
 * legitimate loop in the sync graph: PC Care's repository enqueues its own outbox writes
 * (SyncRepository), SyncRepository wraps SyncEngine, and SyncEngine reconciles PC Care rows
 * back through the repository. Every method resolves the real singleton via [provider] at the
 * moment it is CALLED — never during construction — so Dagger sees no cycle and the first
 * engine callback still lands on the fully wired repository.
 */
private class DeferredPcCareRepository(
    private val provider: javax.inject.Provider<sg.mesha.goatos.core.data.PcCareRepository>,
) : sg.mesha.goatos.core.data.PcCareRepository {
    private val delegate: sg.mesha.goatos.core.data.PcCareRepository by lazy { provider.get() }

    override fun worklistRows(query: sg.mesha.goatos.core.data.PcCareWorklistQuery) = delegate.worklistRows(query)
    override suspend fun invalidateWorklist(query: sg.mesha.goatos.core.data.PcCareWorklistQuery) = delegate.invalidateWorklist(query)
    override fun observeTaskDetail(taskId: String) = delegate.observeTaskDetail(taskId)
    override fun observeTaskRowStatus(taskId: String) = delegate.observeTaskRowStatus(taskId)
    override suspend fun refreshTaskDetail(taskId: String) = delegate.refreshTaskDetail(taskId)
    override fun observeAnimals(taskId: String) = delegate.observeAnimals(taskId)
    override fun observeRoster(taskId: String) = delegate.observeRoster(taskId)
    override suspend fun refreshRoster(taskId: String) = delegate.refreshRoster(taskId)
    override suspend fun pollTaskOnce(taskId: String) = delegate.pollTaskOnce(taskId)
    override suspend fun recordScan(taskId: String, tagVerbatim: String) = delegate.recordScan(taskId, tagVerbatim)
    override suspend fun registerSlotProof(
        taskId: String,
        normalizedTag: String,
        slotFieldKey: String,
        proofOutboxItemId: String,
    ) = delegate.registerSlotProof(taskId, normalizedTag, slotFieldKey, proofOutboxItemId)
    override suspend fun submitTask(taskId: String, rowVersion: Int) = delegate.submitTask(taskId, rowVersion)
    override suspend fun persistTaskSubmitResult(taskId: String, status: String, rowVersion: Int, animalCount: Int) =
        delegate.persistTaskSubmitResult(taskId, status, rowVersion, animalCount)
    override suspend fun plannerCatalog() = delegate.plannerCatalog()
    override suspend fun plannerParkSheds(parkId: String, category: String, date: String, cursor: String?) =
        delegate.plannerParkSheds(parkId, category, date, cursor)
    override suspend fun createTask(
        idempotencyKey: String,
        request: sg.mesha.goatos.core.network.dto.PcCareCreateTaskRequestDto,
    ) = delegate.createTask(idempotencyKey, request)
    override suspend fun cancelTask(taskId: String) = delegate.cancelTask(taskId)
}
