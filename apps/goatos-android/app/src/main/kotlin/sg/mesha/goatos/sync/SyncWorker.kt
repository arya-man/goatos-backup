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
import androidx.work.workDataOf
import dagger.assisted.Assisted
import dagger.assisted.AssistedInject
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.CancellationException
import sg.mesha.goatos.core.data.sync.SyncEngine
import sg.mesha.goatos.core.data.sync.SyncJobsCanceller
import sg.mesha.goatos.core.data.sync.SyncJobsScheduler
import sg.mesha.goatos.core.data.sync.SyncRetryScheduler
import sg.mesha.goatos.core.data.sync.isLoopbackHttpBase
import sg.mesha.goatos.di.ApiBaseUrl
import java.util.concurrent.TimeUnit
import javax.inject.Inject
import javax.inject.Singleton

/**
 * OS-scheduled outbox drain. Unlike the in-process `ConnectivitySyncTrigger`, a WorkManager job
 * survives PROCESS DEATH: if the app is killed with queued writes, the OS still runs this under
 * [SyncWorkScheduler.syncConstraints] and drains the outbox. [doWork] delegates to the SAME
 * [SyncEngine.drainOnce] (which reclaims stranded IN_FLIGHT rows first), so no drain logic lives
 * here — this is the ~10-line `CoroutineWorker` shim the `SyncEngine` KDoc always anticipated.
 */
@HiltWorker
class SyncWorker @AssistedInject constructor(
    @Assisted appContext: Context,
    @Assisted params: WorkerParameters,
    private val syncEngine: SyncEngine,
    private val syncWorkScheduler: SyncWorkScheduler,
) : CoroutineWorker(appContext, params) {
    override suspend fun doWork(): Result =
        try {
            if (inputData.getBoolean(KEY_RETRY_WORK, false)) {
                syncWorkScheduler.clearScheduledRetry()
            }
            // drainOnce() returns false ONLY when the pass was aborted before attempting anything
            // (offline at start) — nothing was scheduled, so re-run under the network constraint.
            // A completed pass returns true even if some rows failed transport: each failure has
            // already contributed to the earliest explicit retry work (SyncEngine ->
            // retryScheduler.scheduleAt). Returning success here means backed-off rows are driven
            // by that explicit retry work — never ALSO by a worker Result.retry(), which
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
 * [ExistingPeriodicWorkPolicy.UPDATE] so app relaunches never stack duplicate work — and, unlike
 * KEEP, cannot leave an install pinned to a stale network constraint (see [SyncWorkScheduler.schedule]).
 */
@Singleton
class SyncWorkScheduler @Inject constructor(
    @ApplicationContext private val context: Context,
    @ApiBaseUrl private val apiBaseUrl: String,
) : SyncRetryScheduler, SyncJobsCanceller, SyncJobsScheduler {
    fun schedule() {
        val request = PeriodicWorkRequestBuilder<SyncWorker>(PERIOD_MINUTES, TimeUnit.MINUTES)
            .setConstraints(syncConstraints())
            .build()
        WorkManager.getInstance(context)
            // UPDATE, not KEEP: WorkManager captures constraints at ENQUEUE time and this periodic
            // work outlives app upgrades, so KEEP would pin an install forever to the constraint
            // the very first launch enqueued — a build that changes API_BASE_URL (remote -> local
            // proof run, or vice versa) would keep draining under the stale one. UPDATE re-applies
            // the current build's constraint in place, keeping the same work id and next-run
            // window, so the idempotence KEEP provided is preserved.
            .enqueueUniquePeriodicWork(UNIQUE_WORK_NAME, ExistingPeriodicWorkPolicy.UPDATE, request)
    }

    /** [SyncJobsScheduler] port — see its KDoc for why a fresh sign-in must call this. */
    override fun scheduleAll() = schedule()

    /**
     * One-shot, connectivity-gated drain — the **fallback for a foreground-service start the OS
     * refuses** (`AndroidForegroundSyncController.ensureRunning`, and the in-service
     * `ForegroundServiceStartNotAllowedException` handler in [UploadForegroundService]).
     *
     * Android 14+ forbids starting a `dataSync` foreground service from a BOOT_COMPLETED
     * context, so after a device reboot the FGS path is simply unavailable. The operator's
     * queued writes (births, deaths, shifting) are already DURABLE in the Room outbox — this
     * makes them drain PROMPTLY rather than waiting up to [PERIOD_MINUTES] for the periodic
     * worker's next tick. Without it the fallback would be silent and slow, which is how a
     * recorded birth appears "lost" after a reboot.
     *
     * [ExistingWorkPolicy.KEEP] so repeated `ensureRunning()` calls (every enqueue) collapse
     * onto one pending drain instead of stacking duplicates; the drain re-reads the outbox
     * fresh, so a row queued after this was enqueued is still picked up by the same pass.
     */
    fun syncNow() {
        val request = OneTimeWorkRequestBuilder<SyncWorker>()
            .setConstraints(syncConstraints())
            .build()
        WorkManager.getInstance(context)
            .enqueueUniqueWork(UNIQUE_SYNC_NOW_WORK_NAME, ExistingWorkPolicy.KEEP, request)
    }

    override fun scheduleAt(epochMillis: Long) {
        synchronized(retryScheduleLock) {
            val existingRetryAt = retryPrefs().getLong(KEY_NEXT_RETRY_AT, NO_RETRY_SCHEDULED)
            if (existingRetryAt != NO_RETRY_SCHEDULED && existingRetryAt <= epochMillis) {
                return
            }
            retryPrefs().edit().putLong(KEY_NEXT_RETRY_AT, epochMillis).commit()
            val delayMillis = (epochMillis - System.currentTimeMillis()).coerceAtLeast(0L)
            val request = OneTimeWorkRequestBuilder<SyncWorker>()
                .setInitialDelay(delayMillis, TimeUnit.MILLISECONDS)
                .setConstraints(syncConstraints())
                .setInputData(workDataOf(KEY_RETRY_WORK to true))
                .build()
            WorkManager.getInstance(context)
                .enqueueUniqueWork(UNIQUE_RETRY_WORK_NAME, ExistingWorkPolicy.REPLACE, request)
        }
    }

    fun clearScheduledRetry() {
        synchronized(retryScheduleLock) {
            retryPrefs().edit().remove(KEY_NEXT_RETRY_AT).commit()
        }
    }

    /** Logout clean-slate (C35-001): cancels the periodic drain, any pending one-time retry
     *  work, AND any pending [syncNow] foreground-service-fallback drain, then clears the
     *  persisted retry-schedule marker — so nothing tries to drain (or re-arm a retry for) an
     *  outbox that the logout wipe just deleted. */
    override fun cancelAll() {
        val workManager = WorkManager.getInstance(context)
        workManager.cancelUniqueWork(UNIQUE_WORK_NAME)
        workManager.cancelUniqueWork(UNIQUE_RETRY_WORK_NAME)
        workManager.cancelUniqueWork(UNIQUE_SYNC_NOW_WORK_NAME)
        clearScheduledRetry()
    }

    /**
     * The OS-level gate on whether this work may START — the layer ABOVE
     * [sg.mesha.goatos.core.data.sync.ConnectivityGate], which only gets consulted once a worker
     * is already running. The two must agree: `LocalBackendConnectivityGate` treats a loopback API
     * base as online (an `adb reverse` proof run, or any device with no validated internet but a
     * reachable localhost backend), so requiring a CONNECTED network here would veto the worker
     * before that gate is ever reached and strand the operator's queued writes indefinitely.
     *
     * Keyed on the ACTUAL base URL, never on build type: a dev build pointed at a remote host
     * still requires a network, and stg/prod are unchanged — real offline-first battery and retry
     * behaviour must not regress.
     */
    internal fun syncConstraints(): Constraints =
        Constraints.Builder()
            .setRequiredNetworkType(
                if (apiBaseUrl.isLoopbackHttpBase()) NetworkType.NOT_REQUIRED else NetworkType.CONNECTED,
            )
            .build()

    private fun retryPrefs() = context.getSharedPreferences(RETRY_PREFS_NAME, Context.MODE_PRIVATE)
}

private const val UNIQUE_WORK_NAME = "goatos-outbox-sync"
private const val UNIQUE_RETRY_WORK_NAME = "goatos-outbox-retry"
private const val UNIQUE_SYNC_NOW_WORK_NAME = "goatos-outbox-sync-now"
private const val PERIOD_MINUTES = 15L // WorkManager's minimum periodic interval.
private const val RETRY_PREFS_NAME = "goatos-outbox-retry-schedule"
private const val KEY_NEXT_RETRY_AT = "next_retry_at"
private const val KEY_RETRY_WORK = "retry_work"
private const val NO_RETRY_SCHEDULED = Long.MAX_VALUE
private val retryScheduleLock = Any()
