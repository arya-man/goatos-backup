package sg.mesha.goatos.sync

import android.content.Context
import androidx.hilt.work.HiltWorker
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.NetworkType
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import dagger.assisted.Assisted
import dagger.assisted.AssistedInject
import kotlinx.coroutines.CancellationException
import sg.mesha.goatos.core.data.sync.SyncEngine
import java.util.concurrent.TimeUnit

/**
 * OS-scheduled outbox drain. Unlike the in-process `ConnectivitySyncTrigger`, a WorkManager job
 * survives PROCESS DEATH: if the app is killed with queued writes, the OS still runs this under a
 * CONNECTED constraint and drains the outbox. [doWork] delegates to the SAME
 * [SyncEngine.drainOnce] (which reclaims stranded IN_FLIGHT rows first), so no drain logic lives
 * here — this is the ~10-line `CoroutineWorker` shim the `SyncEngine` KDoc always anticipated.
 */
@HiltWorker
class SyncWorker @AssistedInject constructor(
    @Assisted appContext: Context,
    @Assisted params: WorkerParameters,
    private val syncEngine: SyncEngine,
) : CoroutineWorker(appContext, params) {
    override suspend fun doWork(): Result =
        try {
            syncEngine.drainOnce()
            Result.success()
        } catch (cancellation: CancellationException) {
            throw cancellation // honour WorkManager's own cancellation — never swallow it.
        } catch (_: Exception) {
            // Durable rows survive + are reclaimed next pass; let WorkManager reschedule.
            Result.retry()
        }
}

/**
 * Registers the periodic, connectivity-gated outbox drain. Idempotent via
 * [ExistingPeriodicWorkPolicy.KEEP] so app relaunches never stack duplicate work.
 */
class SyncWorkScheduler(private val context: Context) {
    fun schedule() {
        val request = PeriodicWorkRequestBuilder<SyncWorker>(PERIOD_MINUTES, TimeUnit.MINUTES)
            .setConstraints(
                Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build(),
            )
            .build()
        WorkManager.getInstance(context)
            .enqueueUniquePeriodicWork(UNIQUE_WORK_NAME, ExistingPeriodicWorkPolicy.KEEP, request)
    }

    private companion object {
        const val UNIQUE_WORK_NAME = "goatos-outbox-sync"
        const val PERIOD_MINUTES = 15L // WorkManager's minimum periodic interval.
    }
}
