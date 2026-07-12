package sg.mesha.goatos

import android.app.Application
import androidx.hilt.work.HiltWorkerFactory
import androidx.work.Configuration
import dagger.hilt.android.HiltAndroidApp
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.data.sync.ConnectivitySyncTrigger
import sg.mesha.goatos.core.data.sync.ForegroundSyncController
import sg.mesha.goatos.sync.SyncWorkScheduler
import javax.inject.Inject

/** Application entry point + Hilt DI root. Kept thin (TRD §3).
 *
 * THREE complementary drain triggers, all converging on the same durable Room outbox +
 * [sg.mesha.goatos.core.data.sync.SyncEngine.drainOnce]:
 *  - [connectivitySyncTrigger] drains promptly WHILE the process is alive (reconnect → drain).
 *  - WorkManager ([SyncWorkScheduler]) is the OS-scheduled backstop that survives PROCESS DEATH:
 *    a killed app with queued writes still drains under a CONNECTED constraint. Its worker is
 *    Hilt-injected via [workerFactory] ([Configuration.Provider]) — the default WorkManager
 *    initializer is removed in the manifest so this on-demand config is the one that wins.
 *  - [foregroundSyncController] (MOB-002 §3, `docs/mobile/proof-capture-sync-and-e2e.md`) is the
 *    Drive/Photos-style VISIBLE background upload: `ensureRunning()` here at cold start is what
 *    resumes an upload the app was closed or killed mid-flight — a fresh
 *    `UploadForegroundService` instance attaches to the SAME durable Room queue and drains any
 *    still-PENDING row plus (via [sg.mesha.goatos.core.data.sync.OutboxStore.reclaimInFlight])
 *    any row stranded IN_FLIGHT by the earlier kill. A no-op (the service notices nothing is
 *    queued and stops itself immediately) when there is nothing to resume. */
@HiltAndroidApp
class GoatOsApplication : Application(), Configuration.Provider {
    @Inject lateinit var connectivitySyncTrigger: ConnectivitySyncTrigger
    @Inject lateinit var syncWorkScheduler: SyncWorkScheduler
    @Inject lateinit var foregroundSyncController: ForegroundSyncController
    @Inject lateinit var workerFactory: HiltWorkerFactory
    @Inject lateinit var appScope: CoroutineScope
    @Inject lateinit var analytics: AnalyticsPort

    override val workManagerConfiguration: Configuration
        get() = Configuration.Builder().setWorkerFactory(workerFactory).build()

    override fun onCreate() {
        super.onCreate()
        // Launch/session markers. Cheap (the port is a no-op today) and Hilt has already field-
        // injected [analytics] by the time super.onCreate() returns, so it is safe to call here.
        analytics.track(AnalyticsEvents.APP_OPEN)
        analytics.track(AnalyticsEvents.SESSION_START)
        // Off the main thread: enqueueUniquePeriodicWork does disk I/O on the calling thread, and
        // starting the connectivity trigger touches ConnectivityManager — neither is on the
        // critical path to first frame, so defer both to the app scope to keep cold start snappy.
        appScope.launch {
            connectivitySyncTrigger.start()
            syncWorkScheduler.schedule()
            // Resume-after-close/kill: see class KDoc. Deferred to the app scope for the same
            // cold-start-latency reason as the two calls above — starting a foreground service
            // touches the OS ActivityManager, not free work.
            foregroundSyncController.ensureRunning()
        }
    }
}
