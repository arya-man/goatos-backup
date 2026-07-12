package sg.mesha.goatos.core.data

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import java.io.IOException
import java.lang.reflect.Proxy
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.SopVersionDto
import sg.mesha.goatos.core.network.dto.TaskDetailResponseDto
import sg.mesha.goatos.core.network.dto.TaskSummaryDto

/**
 * MOB-001 guardrail: the operator Scan -> Submit task read must be offline-first
 * (docs/decisions/android-offline-first.md), not the banned network-only `api.xxx()`
 * pass-through. A ViewModel recreated after process death gets a brand-new
 * [DefaultTasksRepository] from Hilt (the Room database survives, the repository instance does
 * not) — this test proves that exact contract: a task fetched in one repository "session" is
 * still rendered by a freshly-constructed repository over the SAME Room database with the
 * network offline, and with ZERO additional network calls.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class TasksRepositoryOfflineRestoreTest {

    @Test
    fun `a cached task detail survives repository recreation with the network offline`() = runTest {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val database = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        try {
            val taskId = "task-shed-record-1"
            var getCalls = 0
            var online = true
            val fetchedDetail = TaskDetailResponseDto(
                task = TaskSummaryDto(
                    taskId = taskId,
                    sopVersionId = "sop-1",
                    scopeId = "shed-1",
                    title = "Gandhi 1",
                    rowVersion = 3,
                ),
                sopVersion = SopVersionDto(sopVersionId = "sop-1"),
            )
            val api = Proxy.newProxyInstance(AppApi::class.java.classLoader, arrayOf(AppApi::class.java)) { proxy, method, args ->
                when (method.name) {
                    "getAppTask" -> {
                        getCalls++
                        if (!online) throw IOException("offline")
                        fetchedDetail
                    }
                    "toString" -> "TasksAppApiTestProxy"
                    "hashCode" -> System.identityHashCode(proxy)
                    "equals" -> proxy === args?.firstOrNull()
                    else -> error("unexpected AppApi method ${method.name}")
                }
            } as AppApi

            // Session 1 (online): first open — populates Room via refreshTaskDetail.
            val sessionOne = DefaultTasksRepository(api, database.taskDetailCacheDao(), clock = { 10L })
            assertNull("cold cache must start empty", sessionOne.observeTaskDetail(taskId).first().data)
            sessionOne.refreshTaskDetail(taskId).getOrThrow()
            val cached = sessionOne.observeTaskDetail(taskId).first().data
            assertEquals(taskId, cached?.task?.taskId)
            assertEquals(1, getCalls)

            // Simulate process death: a brand-new repository instance over the SAME Room
            // database, network now offline.
            online = false
            val sessionTwo = DefaultTasksRepository(api, database.taskDetailCacheDao(), clock = { 20L })
            val restored = sessionTwo.observeTaskDetail(taskId).first().data
            assertEquals(
                "a recreated repository must render the cached task with ZERO network calls",
                taskId,
                restored?.task?.taskId,
            )
            assertEquals("Gandhi 1", restored?.task?.title)
            assertEquals("observeTaskDetail must never call the network", 1, getCalls)

            // A refresh attempt while offline must fail cleanly and leave the cache intact —
            // never blank the screen just because the background refresh failed.
            assertTrue(sessionTwo.refreshTaskDetail(taskId).isFailure)
            val stillCached = sessionTwo.observeTaskDetail(taskId).first().data
            assertEquals(taskId, stillCached?.task?.taskId)
        } finally {
            database.close()
        }
    }
}
