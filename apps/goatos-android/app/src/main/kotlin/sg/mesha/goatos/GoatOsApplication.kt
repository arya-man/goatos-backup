package sg.mesha.goatos

import android.app.Application
import androidx.hilt.work.HiltWorkerFactory
import androidx.work.Configuration
import dagger.hilt.android.HiltAndroidApp
import sg.mesha.goatos.core.data.sync.ConnectivitySyncTrigger
import sg.mesha.goatos.sync.SyncWorkScheduler
import javax.inject.Inject

/** Application entry point + Hilt DI root. Kept thin (TRD §3).
 *
 * Two complementary drain triggers:
 *  - [connectivitySyncTrigger] drains promptly WHILE the process is alive (reconnect → drain).
 *  - WorkManager ([SyncWorkScheduler]) is the OS-scheduled backstop that survives PROCESS DEATH:
 *    a killed app with queued writes still drains under a CONNECTED constraint. Its worker is
 *    Hilt-injected via [workerFactory] ([Configuration.Provider]) — the default WorkManager
 *    initializer is removed in the manifest so this on-demand config is the one that wins. */
@HiltAndroidApp
class GoatOsApplication : Application(), Configuration.Provider {
    @Inject lateinit var connectivitySyncTrigger: ConnectivitySyncTrigger
    @Inject lateinit var syncWorkScheduler: SyncWorkScheduler
    @Inject lateinit var workerFactory: HiltWorkerFactory

    override val workManagerConfiguration: Configuration
        get() = Configuration.Builder().setWorkerFactory(workerFactory).build()

    override fun onCreate() {
        super.onCreate()
        connectivitySyncTrigger.start()
        syncWorkScheduler.schedule()
    }
}
