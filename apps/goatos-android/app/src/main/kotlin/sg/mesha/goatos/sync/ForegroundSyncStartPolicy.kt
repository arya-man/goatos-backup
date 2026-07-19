package sg.mesha.goatos.sync

import android.app.ActivityManager.RunningAppProcessInfo

/**
 * Decides HOW an outbox drain should be kicked off: via the visible
 * [UploadForegroundService], or via the [SyncWorkScheduler] WorkManager fallback.
 *
 * ### Why this exists
 * Android 14+ (API 34) forbids starting a `dataSync`-typed foreground service from a
 * BOOT_COMPLETED context, and Android 12+ (API 31) forbids starting one from a general
 * background context. `GoatOsApplication.onCreate` calls
 * `ForegroundSyncController.ensureRunning()` on EVERY process start — including the process
 * starts the OS itself triggers after a reboot (WorkManager's own `RescheduleReceiver` is
 * registered for BOOT_COMPLETED via manifest merge, so the app process comes up in a
 * BOOT_COMPLETED context with no user involved). That unconditional FGS start is what threw
 * `ForegroundServiceStartNotAllowedException` out of `UploadForegroundService.onStartCommand`
 * and killed the process on every boot.
 *
 * ### Why an importance check rather than a "am I booting?" flag
 * There is no reliable app-side signal for "the OS considers this start attributable to
 * BOOT_COMPLETED" — the restriction is evaluated by ActivityManager against the process's
 * start reason, which the app cannot read. Process importance IS readable
 * (`ActivityManager.getMyMemoryState`) and answers the question that actually governs the
 * platform rule: *is there a user-visible reason for this app to be running right now?* A
 * foreground-service start is legitimate only when there is, so this gates on importance and
 * routes every other case to WorkManager.
 *
 * ### Correctness bias: the fallback never loses work
 * Both branches drain the SAME durable Room outbox via the same `SyncEngine.drainOnce`. The
 * only thing the FGS branch adds is the user-visible progress notification. So a
 * false-negative (choosing WorkManager when an FGS start would in fact have been permitted)
 * costs a notification; a false-positive (attempting an FGS start the OS refuses) costs a
 * process crash. This deliberately biases toward WorkManager — hence `IMPORTANCE_VISIBLE` and
 * `IMPORTANCE_PERCEPTIBLE` are NOT treated as permitting, even though the platform sometimes
 * allows a start from them.
 */
internal object ForegroundSyncStartPolicy {

    /** How the caller should start the drain. */
    enum class Decision {
        /** Safe to start [UploadForegroundService] — the app has a user-visible presence. */
        START_FOREGROUND_SERVICE,

        /**
         * No user-visible presence (boot, OS-scheduled job wake, cached process). Starting a
         * `dataSync` FGS here is refused by Android 12+/14+, so enqueue the WorkManager drain
         * instead. The queued writes still sync; only the progress notification is skipped.
         */
        ENQUEUE_BACKGROUND_WORK,
    }

    /**
     * @param processImportance a value from [RunningAppProcessInfo]'s `IMPORTANCE_*` scale, as
     *   returned via `ActivityManager.getMyMemoryState(...)`. LOWER means MORE important.
     */
    fun decide(processImportance: Int): Decision =
        // IMPORTANCE_FOREGROUND (100) = a visible activity is running.
        // IMPORTANCE_FOREGROUND_SERVICE (125) = an FGS is already running, so starting/
        // re-signalling one is inherently permitted. Anything higher (VISIBLE 200,
        // PERCEPTIBLE 230, SERVICE 300, CACHED 400, GONE 1000) has no user-visible presence
        // to justify an FGS -- and, critically, covers every post-boot process start.
        if (processImportance <= RunningAppProcessInfo.IMPORTANCE_FOREGROUND_SERVICE) {
            Decision.START_FOREGROUND_SERVICE
        } else {
            Decision.ENQUEUE_BACKGROUND_WORK
        }
}
