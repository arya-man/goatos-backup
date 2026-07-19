package sg.mesha.goatos.sync

import android.app.ActivityManager.RunningAppProcessInfo
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Regression cover for the boot-path crash: the app died on EVERY device reboot because
 * `GoatOsApplication.onCreate` unconditionally asked for the `dataSync`
 * [UploadForegroundService], which Android 14+ refuses to start from a BOOT_COMPLETED context
 * (`ForegroundServiceStartNotAllowedException` out of `onStartCommand`).
 *
 * These assert the DECISION, which is the part that is genuinely testable without a device:
 * the platform's own refusal cannot be provoked in a JVM unit test, so the contract worth
 * pinning is "a process with no user-visible presence must never attempt the foreground-service
 * start, and must route the drain to WorkManager instead".
 */
class ForegroundSyncStartPolicyTest {

    @Test
    fun `post-boot background process enqueues work instead of starting the foreground service`() {
        // The exact shape of the crash: after a reboot the OS starts the app process to deliver
        // BOOT_COMPLETED to WorkManager's RescheduleReceiver. Importance is RECEIVER/SERVICE --
        // never FOREGROUND -- so an FGS start must not be attempted.
        assertEquals(
            ForegroundSyncStartPolicy.Decision.ENQUEUE_BACKGROUND_WORK,
            ForegroundSyncStartPolicy.decide(RunningAppProcessInfo.IMPORTANCE_SERVICE),
        )
    }

    @Test
    fun `cached and gone processes enqueue work`() {
        // An OS-scheduled job waking a cached process is the other everyday non-visible start.
        for (importance in listOf(
            RunningAppProcessInfo.IMPORTANCE_CACHED,
            RunningAppProcessInfo.IMPORTANCE_GONE,
        )) {
            assertEquals(
                "importance=$importance must not attempt a foreground-service start",
                ForegroundSyncStartPolicy.Decision.ENQUEUE_BACKGROUND_WORK,
                ForegroundSyncStartPolicy.decide(importance),
            )
        }
    }

    @Test
    fun `visible-but-not-foreground states still enqueue work`() {
        // Deliberate conservative bias (see ForegroundSyncStartPolicy KDoc): the platform
        // SOMETIMES permits a start from these, but guessing wrong crashes the process whereas
        // falling back only costs the progress notification. Pinned so a future "optimisation"
        // that widens this has to justify itself against the crash it reintroduces.
        for (importance in listOf(
            RunningAppProcessInfo.IMPORTANCE_VISIBLE,
            RunningAppProcessInfo.IMPORTANCE_PERCEPTIBLE,
        )) {
            assertEquals(
                "importance=$importance must take the safe fallback",
                ForegroundSyncStartPolicy.Decision.ENQUEUE_BACKGROUND_WORK,
                ForegroundSyncStartPolicy.decide(importance),
            )
        }
    }

    @Test
    fun `app in the foreground still uses the visible foreground service`() {
        // The fix must NOT regress the normal operator flow: with the app open, an upload still
        // gets the Drive/Photos-style ongoing progress notification (MOB-002 §3).
        assertEquals(
            ForegroundSyncStartPolicy.Decision.START_FOREGROUND_SERVICE,
            ForegroundSyncStartPolicy.decide(RunningAppProcessInfo.IMPORTANCE_FOREGROUND),
        )
    }

    @Test
    fun `an already-running foreground service may be re-signalled`() {
        // ensureRunning() is called on every enqueue; while a drain is already in flight the
        // process sits at IMPORTANCE_FOREGROUND_SERVICE and re-signalling is always permitted.
        assertEquals(
            ForegroundSyncStartPolicy.Decision.START_FOREGROUND_SERVICE,
            ForegroundSyncStartPolicy.decide(RunningAppProcessInfo.IMPORTANCE_FOREGROUND_SERVICE),
        )
    }

    @Test
    fun `decision boundary sits exactly at foreground-service importance`() {
        // Guards the comparison operator itself (<= vs <), which is the one-character mistake
        // that would silently reintroduce either the crash or a lost notification.
        assertEquals(
            ForegroundSyncStartPolicy.Decision.START_FOREGROUND_SERVICE,
            ForegroundSyncStartPolicy.decide(RunningAppProcessInfo.IMPORTANCE_FOREGROUND_SERVICE),
        )
        assertEquals(
            ForegroundSyncStartPolicy.Decision.ENQUEUE_BACKGROUND_WORK,
            ForegroundSyncStartPolicy.decide(RunningAppProcessInfo.IMPORTANCE_FOREGROUND_SERVICE + 1),
        )
    }
}
