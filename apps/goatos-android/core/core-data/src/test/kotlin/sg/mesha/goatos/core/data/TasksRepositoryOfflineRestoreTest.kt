package sg.mesha.goatos.core.data

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import java.io.IOException
import java.lang.reflect.Proxy
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlinx.serialization.json.putJsonArray
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
import sg.mesha.goatos.core.network.dto.TaskOptionSourceDto
import sg.mesha.goatos.core.network.dto.TaskOptionValueDto
import sg.mesha.goatos.core.network.dto.TaskOptionValuesResponseDto

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
    fun `a vaccination form with inline options does not depend on the option-values endpoint`() = runTest {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val database = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        try {
            var optionCalls = 0
            val formDsl = buildJsonObject {
                putJsonArray("fields") {
                    add(buildJsonObject {
                        put("key", "vaccine_lot_id")
                        put("label", "Vaccine lot")
                        put("type", "vaccine_batch_picker")
                        putJsonArray("options") {
                            add(buildJsonObject {
                                put("value", "lot-inline")
                                put("label", "INLINE-LOT")
                            })
                        }
                    })
                }
            }.entries.associate { it.key to it.value }
            val api = Proxy.newProxyInstance(AppApi::class.java.classLoader, arrayOf(AppApi::class.java)) { proxy, method, args ->
                when (method.name) {
                    "getAppTask" -> TaskDetailResponseDto(
                        task = TaskSummaryDto(taskId = "task-inline", taskType = "vaccination"),
                        sopVersion = SopVersionDto(sopVersionId = "sop-inline", formDsl = formDsl),
                    )
                    "getTaskOptionValues" -> {
                        optionCalls++
                        error("inline options must not require the option-values endpoint")
                    }
                    "toString" -> "InlineOptionsAppApiTestProxy"
                    "hashCode" -> System.identityHashCode(proxy)
                    "equals" -> proxy === args?.firstOrNull()
                    else -> error("unexpected AppApi method ${method.name}")
                }
            } as AppApi

            val detail = DefaultTasksRepository(api, database.taskDetailCacheDao(), database.shedCompletionSummaryCacheDao()).taskDetail("task-inline")

            assertEquals("INLINE-LOT", detail.form.fields.single().options.single().label)
            assertEquals(0, optionCalls)
        } finally {
            database.close()
        }
    }

    @Test
    fun `a cached task detail survives repository recreation with the network offline`() = runTest {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val database = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        try {
            val taskId = "task-shed-record-1"
            var getCalls = 0
            var optionCalls = 0
            var online = true
            val formDsl = buildJsonObject {
                put("schema_version", "goatos.sop-form.v1")
                putJsonArray("fields") {
                    add(buildJsonObject {
                        put("key", "vaccine_lot_id")
                        put("label", "Vaccine lot")
                        put("type", "vaccine_batch_picker")
                        put("option_source", "vaccine_lots")
                    })
                }
            }.entries.associate { it.key to it.value }
            val fetchedDetail = TaskDetailResponseDto(
                task = TaskSummaryDto(
                    taskId = taskId,
                    sopVersionId = "sop-1",
                    scopeId = "shed-1",
                    title = "Gandhi 1",
                    taskType = "vaccination",
                    rowVersion = 3,
                ),
                sopVersion = SopVersionDto(sopVersionId = "sop-1", formDsl = formDsl),
            )
            val api = Proxy.newProxyInstance(AppApi::class.java.classLoader, arrayOf(AppApi::class.java)) { proxy, method, args ->
                when (method.name) {
                    "getAppTask" -> {
                        getCalls++
                        if (!online) throw IOException("offline")
                        fetchedDetail
                    }
                    "getTaskOptionValues" -> {
                        optionCalls++
                        if (!online) throw IOException("offline")
                        TaskOptionValuesResponseDto(
                            taskId = taskId,
                            sources = listOf(
                                TaskOptionSourceDto(
                                    source = "vaccine_lots",
                                    options = listOf(TaskOptionValueDto(value = "lot-1", label = "ETTT-LOT-001")),
                                ),
                            ),
                        )
                    }
                    "toString" -> "TasksAppApiTestProxy"
                    "hashCode" -> System.identityHashCode(proxy)
                    "equals" -> proxy === args?.firstOrNull()
                    else -> error("unexpected AppApi method ${method.name}")
                }
            } as AppApi

            // Session 1 (online): first open — populates Room via refreshTaskDetail.
            val sessionOne = DefaultTasksRepository(api, database.taskDetailCacheDao(), database.shedCompletionSummaryCacheDao(), clock = { 10L })
            assertNull("cold cache must start empty", sessionOne.observeTaskDetail(taskId).first().data)
            sessionOne.refreshTaskDetail(taskId).getOrThrow()
            val cached = sessionOne.observeTaskDetail(taskId).first().data
            assertEquals(taskId, cached?.task?.taskId)
            assertEquals("ETTT-LOT-001", cached?.form?.fields?.single()?.options?.single()?.label)
            assertEquals(1, getCalls)
            assertEquals(1, optionCalls)

            // Simulate process death: a brand-new repository instance over the SAME Room
            // database, network now offline.
            online = false
            val sessionTwo = DefaultTasksRepository(api, database.taskDetailCacheDao(), database.shedCompletionSummaryCacheDao(), clock = { 20L })
            val restored = sessionTwo.observeTaskDetail(taskId).first().data
            assertEquals(
                "a recreated repository must render the cached task with ZERO network calls",
                taskId,
                restored?.task?.taskId,
            )
            assertEquals("Gandhi 1", restored?.task?.title)
            assertEquals("observeTaskDetail must never call the network", 1, getCalls)
            assertEquals("cached option values must not call the network", 1, optionCalls)

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
