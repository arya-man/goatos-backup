package sg.mesha.goatos

import android.app.Application
import dagger.hilt.android.HiltAndroidApp
import sg.mesha.goatos.core.data.sync.ConnectivitySyncTrigger
import javax.inject.Inject

/** Application entry point + Hilt DI root. Kept thin (TRD §3).
 *
 * Starts [connectivitySyncTrigger] once at boot so the offline sync engine drains the
 * outbox as soon as the network is available, without a foreground screen having to ask
 * for it first (see SyncEngine's KDoc — this is the app's substitute for WorkManager's
 * connectivity `Constraints`, since `androidx.work` is not wired in this build). */
@HiltAndroidApp
class GoatOsApplication : Application() {
    @Inject lateinit var connectivitySyncTrigger: ConnectivitySyncTrigger

    override fun onCreate() {
        super.onCreate()
        connectivitySyncTrigger.start()
    }
}
