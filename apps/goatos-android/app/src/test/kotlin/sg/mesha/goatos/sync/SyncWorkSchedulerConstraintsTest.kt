package sg.mesha.goatos.sync

import android.content.Context
import androidx.work.Configuration
import androidx.work.ListenableWorker
import androidx.work.NetworkType
import androidx.work.WorkManager
import androidx.work.WorkerFactory
import androidx.work.WorkerParameters
import androidx.test.core.app.ApplicationProvider
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import java.util.concurrent.Executor
import java.util.concurrent.Executors

/**
 * A queued operator submission MUST still be dispatched when the phone has NO validated internet
 * but the API base is loopback (`adb reverse` device proof runs). [sg.mesha.goatos.core.data.sync.
 * LocalBackendConnectivityGate] already knows that, but WorkManager's own `NetworkType.CONNECTED`
 * constraint blocks the worker from ever STARTING, so the loopback-aware gate is never consulted.
 * These tests pin the constraint to agree with the gate — and keep the real remote flavors on
 * `CONNECTED` so production offline-first battery/retry behaviour does not regress.
 */
@RunWith(RobolectricTestRunner::class)
class SyncWorkSchedulerConstraintsTest {

    private lateinit var context: Context

    @Before
    fun setUp() {
        context = ApplicationProvider.getApplicationContext()
        // initialize() throws if a previous test in this class already did it — Robolectric keeps
        // WorkManager's singleton across tests in a class, so tolerate the second call.
        runCatching {
            WorkManager.initialize(
                context,
                Configuration.Builder()
                    .setExecutor(Executor { it.run() })
                    // WorkManager's own Room database must not be touched from the main thread.
                    .setTaskExecutor(Executors.newSingleThreadExecutor())
                    .setWorkerFactory(NoopWorkerFactory)
                    .build(),
            )
        }
        clearWork()
    }

    @After
    fun tearDown() = clearWork()

    private fun clearWork() {
        val workManager = WorkManager.getInstance(context)
        workManager.cancelAllWork().result.get()
        // Cancelled work stays in the database as a finished row; prune it so the next test's
        // "exactly one request" assertion sees only what that test enqueued.
        workManager.pruneWork().result.get()
    }

    @Test
    fun `loopback api base does not require a connected network`() {
        val scheduler = SyncWorkScheduler(context, apiBaseUrl = "http://localhost:8080/")

        scheduler.syncNow()

        assertEquals(NetworkType.NOT_REQUIRED, requiredNetworkTypeFor("goatos-outbox-sync-now"))
    }

    @Test
    fun `remote api base still requires a connected network`() {
        val scheduler = SyncWorkScheduler(context, apiBaseUrl = "https://stg-api.dashboard.mesha.sg/")

        scheduler.syncNow()

        assertEquals(NetworkType.CONNECTED, requiredNetworkTypeFor("goatos-outbox-sync-now"))
    }

    /**
     * The end-to-end shape of the failure: the operator submits a shed, the foreground-service
     * start is refused, and the fallback WorkManager drain is enqueued on a phone with no
     * validated network. Every drain trigger (periodic backstop, retry work, one-shot fallback)
     * must still be startable, or the submission sits in the outbox forever behind an
     * "Upload paused" notification even though the loopback API is reachable.
     */
    @Test
    fun `every loopback drain trigger is startable with no network`() {
        val scheduler = SyncWorkScheduler(context, apiBaseUrl = "http://127.0.0.1:8080/")

        scheduler.schedule()
        scheduler.syncNow()
        scheduler.scheduleAt(System.currentTimeMillis() + 60_000)

        listOf("goatos-outbox-sync", "goatos-outbox-sync-now", "goatos-outbox-retry").forEach { name ->
            assertEquals(
                "unique work $name must not require a validated network on a loopback base",
                NetworkType.NOT_REQUIRED,
                requiredNetworkTypeFor(name),
            )
        }
    }

    private fun requiredNetworkTypeFor(uniqueWorkName: String): NetworkType {
        val infos = WorkManager.getInstance(context)
            .getWorkInfosForUniqueWork(uniqueWorkName)
            .get()
        assertEquals("expected exactly one enqueued request for $uniqueWorkName", 1, infos.size)
        return infos.single().constraints.requiredNetworkType
    }

    private object NoopWorkerFactory : WorkerFactory() {
        override fun createWorker(
            appContext: Context,
            workerClassName: String,
            workerParameters: WorkerParameters,
        ): ListenableWorker? = null
    }
}
