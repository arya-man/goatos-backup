package sg.mesha.goatos.sync

import android.app.ActivityManager
import android.app.ActivityManager.RunningAppProcessInfo
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.IBinder
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import dagger.hilt.android.AndroidEntryPoint
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import sg.mesha.goatos.MainActivity
import sg.mesha.goatos.R
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.data.sync.ForegroundSyncController
import sg.mesha.goatos.core.data.sync.OutboxStore
import sg.mesha.goatos.core.data.sync.SyncEngine
import sg.mesha.goatos.core.data.sync.UploadSyncCoordinator
import sg.mesha.goatos.core.designsystem.R as DesignSystemR
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Drive/Photos-style **background upload foreground service** (MOB-002,
 * `docs/mobile/proof-capture-sync-and-e2e.md` §3 "Background sync survives app close"): shows an
 * ongoing progress notification while the outbox's upload-relevant rows
 * ([UploadSyncCoordinator.RELEVANT_OP_TYPES] — proof-video registrations + shed submissions)
 * drain, so closing the app (or Android killing the process under memory pressure) never loses
 * or silently pauses an in-flight vaccination drive.
 *
 * This class is deliberately a THIN Android shim — like `SyncWorker` (`:app`'s WorkManager
 * backstop) — around [UploadSyncCoordinator], which is the actual framework-free "run the drain
 * loop, report progress" body (`:core:core-data`, unit-tested there). No dispatch/drain logic is
 * duplicated here.
 *
 * ### Resume after app close / process kill
 * The outbox (Room) is the durable source of truth, not this service's in-memory state:
 *  - [ForegroundSyncController.ensureRunning] — the only way anything starts this service — is
 *    called both on every relevant enqueue (`DefaultSyncRepository.enqueue`) AND once at app
 *    startup (`GoatOsApplication.onCreate`). The startup call is what "resumes" a drive whose
 *    upload was interrupted by the app being closed or killed: a fresh service instance attaches
 *    to the SAME Room queue, and [SyncEngine.drainOnce] (via [UploadSyncCoordinator]) first
 *    reclaims any row stranded IN_FLIGHT by the earlier kill before draining PENDING rows —
 *    see `OutboxProcessRestartResumeTest` (`:core:core-data`) for the durable, Room-backed proof.
 *  - The WorkManager `SyncWorker` backstop (`:app`) is a second, OS-scheduled trigger that keeps
 *    draining even if this foreground service itself never got a chance to (re)start.
 *  - No double-upload: every write carries a stable idempotency key end to end
 *    ([SyncEngine]'s KDoc) — a resumed pass either completes an interrupted upload or safely
 *    no-ops on an exact replay, never a duplicate.
 *
 * `android:foregroundServiceType="dataSync"` (manifest) + `FOREGROUND_SERVICE_DATA_SYNC`
 * (Android 14+) classify this correctly to the OS; `startForeground` is called synchronously in
 * [onStartCommand], before any suspending work, to satisfy the platform's 5-second window.
 */
@AndroidEntryPoint
class UploadForegroundService : Service() {

    @Inject lateinit var syncEngine: SyncEngine
    @Inject lateinit var outboxStore: OutboxStore
    @Inject lateinit var notifications: UploadSyncNotifications

    /** Fallback drain when the platform refuses this service's foreground start (see
     *  [onStartCommand]) — the queued operator writes must still sync. */
    @Inject lateinit var syncWorkScheduler: SyncWorkScheduler
    @Inject lateinit var crashReporter: CrashReporter
    @Inject lateinit var analytics: AnalyticsPort

    private val serviceJob = SupervisorJob()
    private val serviceScope = CoroutineScope(serviceJob + Dispatchers.Default)

    // Guards against stacking a second concurrent drain loop when onStartCommand fires again
    // while one is already running (e.g. a second capture finishes and calls ensureRunning()
    // again) — the NEW row is still picked up by the ALREADY-running coordinator's next pass
    // (it re-reads eligibility fresh, same contract as SyncEngine.drainMutex), so a duplicate
    // loop would only waste a coroutine, never help.
    private var drainJob: Job? = null

    override fun onCreate() {
        super.onCreate()
        notifications.ensureChannel()
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        // minSdk 29 (Q) already IS the version that introduced FOREGROUND_SERVICE_TYPE_DATA_SYNC
        // and the 3-arg startForeground(id, notification, type) overload, so no SDK_INT branch
        // is needed here — ServiceCompat still centralizes the call for readability/parity with
        // the rest of the codebase's androidx.core usage.
        //
        // This call is the one that used to CRASH THE PROCESS ON EVERY DEVICE REBOOT: Android
        // 14+ rejects a `dataSync` FGS started from a BOOT_COMPLETED context, and the rejection
        // surfaces HERE (as a ForegroundServiceStartNotAllowedException thrown out of
        // onStartCommand) rather than at the caller's startForegroundService() — so the
        // controller's own runCatching cannot see it. AndroidForegroundSyncController now avoids
        // attempting the start at all in that situation; this handler is the second line of
        // defence for the race it cannot eliminate (the platform decides against the process
        // START REASON, which the app cannot read, so an allowed-then-refused window remains).
        try {
            ServiceCompat.startForeground(
                this,
                NOTIFICATION_ID,
                notifications.buildProgress(done = 0, total = 0),
                ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC,
            )
        } catch (notAllowed: IllegalStateException) {
            // Caught as IllegalStateException, NOT as ForegroundServiceStartNotAllowedException:
            // that class only exists from API 31 and this module's minSdk is 29, so naming it in
            // a catch clause would put an unresolvable type in the handler on older devices. It
            // (and the API 34 foreground-service-type refusals) all extend IllegalStateException,
            // and in onStartCommand an ISE out of startForeground means "the platform refused
            // this start" — for which the WorkManager fallback is the correct response either way.
            // NOT swallowed: the outbox drain is handed to WorkManager, which is not subject to
            // the foreground-service start restriction, so the operator's queued writes still
            // sync. We must NOT run the drain loop in-process here — without a successful
            // startForeground this service has no foreground privilege and the OS will kill it
            // mid-drain (and ANR-timeout the start), which is precisely the "silently dropped
            // work" failure mode this handler exists to prevent.
            crashReporter.recordException(
                notAllowed,
                "dataSync FGS start refused; falling back to WorkManager outbox drain",
            )
            analytics.track(
                AnalyticsEvents.SYNC_FOREGROUND_START_BLOCKED,
                mapOf(AnalyticsEvents.Params.REASON to "start_foreground_refused"),
            )
            syncWorkScheduler.syncNow()
            stopSelf(startId)
            return START_NOT_STICKY
        }
        if (drainJob?.isActive != true) {
            drainJob = serviceScope.launch { runDrainLoop() }
        }
        // Room already holds the durable state; nothing to redeliver if the system kills THIS
        // service — the next ensureRunning() (enqueue, reconnect, or app relaunch) or the
        // WorkManager backstop starts a fresh instance that resumes from Room.
        return START_NOT_STICKY
    }

    private suspend fun runDrainLoop() {
        val coordinator = UploadSyncCoordinator(syncEngine, outboxStore)
        val outcome = coordinator.run { done, total ->
            notifications.notify(NOTIFICATION_ID, notifications.buildProgress(done, total))
        }
        when (outcome) {
            is UploadSyncCoordinator.Outcome.Idle -> notifications.notifySuccess()
            is UploadSyncCoordinator.Outcome.Waiting -> notifications.notifyWaiting(outcome.remaining)
            is UploadSyncCoordinator.Outcome.InProgress ->
                notifications.notify(NOTIFICATION_ID, notifications.buildProgress(outcome.total - outcome.remaining, outcome.total))
        }
        ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    override fun onDestroy() {
        // No leak: cancel every coroutine this service started. The durable queue itself is
        // untouched — Room, not this Job, is the source of truth for what still needs to drain.
        serviceJob.cancel()
        super.onDestroy()
    }

    private companion object {
        const val NOTIFICATION_ID = 4210
    }
}

/** Builds + posts the service's notifications. A thin Android-only helper — deliberately NOT
 *  unit-tested (mirrors [android.app.Notification] builders elsewhere in this codebase); the
 *  behavior worth testing (progress math, when to stop, resume-after-restart) lives in
 *  [UploadSyncCoordinator] and is covered there. */
@Singleton
class UploadSyncNotifications @Inject constructor(
    @ApplicationContext private val context: Context,
) {
    private val manager = context.getSystemService(NotificationManager::class.java)

    fun ensureChannel() {
        val channel = NotificationChannel(
            CHANNEL_ID,
            context.getString(DesignSystemR.string.sync_notification_channel_name),
            NotificationManager.IMPORTANCE_LOW, // progress updates — no sound/heads-up, mirrors Drive/Photos.
        ).apply {
            description = context.getString(DesignSystemR.string.sync_notification_channel_description)
        }
        manager?.createNotificationChannel(channel)
    }

    fun buildProgress(done: Int, total: Int): Notification =
        baseBuilder()
            .setContentTitle(context.getString(DesignSystemR.string.sync_notification_uploading_title))
            .setContentText(
                if (total > 0) context.getString(DesignSystemR.string.sync_notification_uploading_text, done, total) else null,
            )
            .setOngoing(true)
            .setProgress(total.coerceAtLeast(1), done.coerceIn(0, total.coerceAtLeast(1)), total <= 0)
            .build()

    fun notifySuccess() {
        val notification = baseBuilder()
            .setContentTitle(context.getString(DesignSystemR.string.sync_notification_success_title))
            .setContentText(context.getString(DesignSystemR.string.sync_notification_success_text))
            .setOngoing(false)
            .setAutoCancel(true)
            .build()
        notify(SUCCESS_NOTIFICATION_ID, notification)
    }

    fun notifyWaiting(remaining: Int) {
        val notification = baseBuilder()
            .setContentTitle(context.getString(DesignSystemR.string.sync_notification_waiting_title))
            .setContentText(context.getString(DesignSystemR.string.sync_notification_waiting_text, remaining))
            .setOngoing(false)
            .setAutoCancel(true)
            .build()
        notify(WAITING_NOTIFICATION_ID, notification)
    }

    fun notify(id: Int, notification: Notification) {
        runCatching { manager?.notify(id, notification) }
    }

    private fun baseBuilder(): NotificationCompat.Builder {
        // Tapping opens MainActivity — there is no per-drive deep link yet (REMAINING: route
        // straight to the specific drive/shed once a mobile deep-link contract exists); opening
        // the app is a safe, always-correct seam in the meantime.
        val contentIntent = PendingIntent.getActivity(
            context,
            0,
            Intent(context, MainActivity::class.java).apply {
                flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP
            },
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        return NotificationCompat.Builder(context, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_notification_upload)
            .setContentIntent(contentIntent)
            .setOnlyAlertOnce(true)
            .setPriority(NotificationCompat.PRIORITY_LOW)
    }

    private companion object {
        const val CHANNEL_ID = "goatos-background-sync"
        const val SUCCESS_NOTIFICATION_ID = 4211
        const val WAITING_NOTIFICATION_ID = 4212
    }
}

/** [ForegroundSyncController] Android implementation: starts [UploadForegroundService] when the
 *  app has a user-visible presence, and otherwise falls back to the [SyncWorkScheduler]
 *  WorkManager drain. Kept in `:app` (not `:core:core-data`, which stays framework-free) —
 *  mirrors [SyncWorkScheduler] wrapping [sg.mesha.goatos.sync.SyncWorker].
 *
 *  The fallback is NOT an optimisation — see [ForegroundSyncStartPolicy]: `GoatOsApplication`
 *  calls [ensureRunning] on every process start, including OS-triggered post-reboot starts
 *  where Android 14+ refuses a `dataSync` foreground service outright. */
class AndroidForegroundSyncController(
    private val context: Context,
    private val syncWorkScheduler: SyncWorkScheduler,
    private val analytics: AnalyticsPort,
) : ForegroundSyncController {
    override fun ensureRunning() {
        if (foregroundStartDecision() == ForegroundSyncStartPolicy.Decision.ENQUEUE_BACKGROUND_WORK) {
            // Boot / OS-scheduled wake / cached process: don't even ATTEMPT the FGS start. The
            // durable outbox still drains, just without the progress notification.
            fallBackToBackgroundWork(reason = "no_foreground_presence")
            return
        }
        val intent = Intent(context, UploadForegroundService::class.java)
        runCatching {
            ContextCompat.startForegroundService(context, intent)
        }.onFailure {
            // Background-start restrictions (Android 12+) can still reject this on OEM/timing
            // edge cases the importance check above cannot see (the platform evaluates the
            // process START REASON, which the app cannot read). Never crash the caller (an
            // enqueue) over it — but never silently drop the work either: fall back to the same
            // WorkManager drain so a queued birth/death/shifting still syncs promptly rather
            // than waiting for the 15-minute periodic tick.
            fallBackToBackgroundWork(reason = "start_rejected")
        }
    }

    /** Current process importance. Defaults to a NON-foreground value so that an unavailable
     *  ActivityManager degrades to the safe branch (WorkManager) rather than a crash-prone
     *  FGS attempt. */
    private fun foregroundStartDecision(): ForegroundSyncStartPolicy.Decision {
        val state = RunningAppProcessInfo()
        return runCatching {
            ActivityManager.getMyMemoryState(state)
            ForegroundSyncStartPolicy.decide(state.importance)
        }.getOrDefault(ForegroundSyncStartPolicy.Decision.ENQUEUE_BACKGROUND_WORK)
    }

    private fun fallBackToBackgroundWork(reason: String) {
        // Visible in analytics because a silent fallback is exactly how "my recorded birth
        // never synced" becomes unexplainable: the work is safe either way, but we want to
        // know how often operators lose the progress notification and why.
        analytics.track(
            AnalyticsEvents.SYNC_FOREGROUND_START_BLOCKED,
            mapOf(AnalyticsEvents.Params.REASON to reason),
        )
        runCatching { syncWorkScheduler.syncNow() }
    }
}
