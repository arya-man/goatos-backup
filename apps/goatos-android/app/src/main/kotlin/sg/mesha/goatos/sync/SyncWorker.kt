package sg.mesha.goatos.sync

import android.content.Context
import androidx.hilt.work.HiltWorker
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import dagger.assisted.Assisted
import dagger.assisted.AssistedInject
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.CancellationException
import sg.mesha.goatos.core.data.sync.SyncEngine
import sg.mesha.goatos.core.data.sync.SyncRetryScheduler
import java.util.concurrent.TimeUnit
import javax.inject.Inject
import javax.inject.Singleton

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
            // drainOnce() returns false ONLY when the pass was aborted before attempting anything
            // (offline at start) — nothing was scheduled, so re-run under the CONNECTED constraint.
            // A completed pass returns true even if some rows failed transport: each failure has
            // already booked its own backoff via the unique, REPLACE-d retry work (SyncEngine ->
            // retryScheduler.scheduleAt). Returning success here means backed-off rows are driven
            // solely by that explicit retry work — never ALSO by a worker Result.retry(), which
            // would double-schedule the same row (retry-churn).
            if (syncEngine.drainOnce()) Result.success() else Result.retry()
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
@Singleton
class SyncWorkScheduler @Inject constructor(
    @ApplicationContext private val context: Context,
) : SyncRetryScheduler {
    fun schedule() {
        val request = PeriodicWorkRequestBuilder<SyncWorker>(PERIOD_MINUTES, TimeUnit.MINUTES)
            .setConstraints(syncConstraints())
            .build()
        WorkManager.getInstance(context)
            .enqueueUniquePeriodicWork(UNIQUE_WORK_NAME, ExistingPeriodicWorkPolicy.KEEP, request)
    }

    override fun scheduleAt(epochMillis: Long) {
        val delayMillis = (epochMillis - System.currentTimeMillis()).coerceAtLeast(0L)
        val request = OneTimeWorkRequestBuilder<SyncWorker>()
            .setInitialDelay(delayMillis, TimeUnit.MILLISECONDS)
            .setConstraints(syncConstraints())
            .build()
        // REPLACE (not plain enqueue): every failed row's backoff calls scheduleAt, so a
        // burst of failures must collapse to ONE pending retry job, not stack N identical
        // one-time workers that all fire and re-drain the same outbox.
        WorkManager.getInstance(context)
            .enqueueUniqueWork(UNIQUE_RETRY_WORK_NAME, ExistingWorkPolicy.REPLACE, request)
    }

    private fun syncConstraints(): Constraints =
        Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build()

    private companion object {
        const val UNIQUE_WORK_NAME = "goatos-outbox-sync"
        const val UNIQUE_RETRY_WORK_NAME = "goatos-outbox-retry"
        const val PERIOD_MINUTES = 15L // WorkManager's minimum periodic interval.
    }
}
