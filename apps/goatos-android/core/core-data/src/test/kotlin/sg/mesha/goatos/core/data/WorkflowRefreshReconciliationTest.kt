package sg.mesha.goatos.core.data

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import java.lang.reflect.Proxy
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.data.cache.WorkflowDetailCacheEntity
import sg.mesha.goatos.core.data.cache.WorkflowCardEntity
import sg.mesha.goatos.core.data.sync.RoomOutboxStore
import sg.mesha.goatos.core.data.sync.WorkflowActionCompletePayload
import sg.mesha.goatos.core.database.outbox.OutboxDatabase
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.WorkflowActionDto
import sg.mesha.goatos.core.network.dto.WorkflowCardDto
import sg.mesha.goatos.core.network.dto.WorkflowDetailResponseDto
import sg.mesha.goatos.core.network.dto.WorkflowNextActionDto

/** Production-path regression for refresh racing the durable death-video outbox. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class WorkflowRefreshReconciliationTest {
    private lateinit var goatDatabase: GoatDatabase
    private lateinit var outboxDatabase: OutboxDatabase
    private val json = Json { ignoreUnknownKeys = true }

    @Before
    fun setUp() {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        goatDatabase = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        outboxDatabase = Room.inMemoryDatabaseBuilder(context, OutboxDatabase::class.java)
            .allowMainThreadQueries()
            .build()
    }

    @After
    fun tearDown() {
        goatDatabase.close()
        outboxDatabase.close()
    }

    @Test
    fun `death video stays a replaceable durable draft until submit`() = runTest {
        val workflowId = "death-workflow"
        val repository = DefaultWorkflowsRepository(
            api = apiReturning(deathDetail(workflowId, firstStatus = "pending", secondBlocked = true)),
            database = goatDatabase,
            outboxStore = RoomOutboxStore(outboxDatabase.outboxDao()),
            json = json,
            clock = { 3L },
        )
        val first = WorkflowVideoDraft(
            id = "draft-1",
            workflowId = workflowId,
            actionId = "death",
            subjectGoatId = "goat-1",
            localUri = "file:/first.mp4",
            mimeType = "video/mp4",
            startedAtMs = 10L,
            endedAtMs = 20L,
            captureSource = "in_app_camera",
        )
        val second = first.copy(id = "draft-2", localUri = "file:/second.mp4", startedAtMs = 30L, endedAtMs = 40L)

        assertEquals(null, repository.replaceVideoDraft(first))
        assertEquals(first, repository.replaceVideoDraft(second))

        val recreated = DefaultWorkflowsRepository(
            api = apiReturning(deathDetail(workflowId, firstStatus = "pending", secondBlocked = true)),
            database = goatDatabase,
            outboxStore = RoomOutboxStore(outboxDatabase.outboxDao()),
            json = json,
            clock = { 4L },
        )
        assertEquals(listOf(second), recreated.observeVideoDrafts(workflowId).first())
        assertTrue(outboxDatabase.outboxDao().observeActive().first().isEmpty())
    }

    @Test
    fun `refresh cannot roll queued death video back to pending`() = runTest {
        val workflowId = "death-workflow"
        val cached = deathDetail(workflowId, firstStatus = "in_review", secondBlocked = false)
        goatDatabase.workflowDetailCacheDao().upsert(
            WorkflowDetailCacheEntity(workflowId, json.encodeToString(cached), updatedAt = 1L),
        )
        outboxDatabase.outboxDao().insert(
            OutboxEntity(
                id = "completion-row",
                opType = OutboxOpType.WORKFLOW_ACTION_COMPLETE.name,
                groupKey = workflowId,
                idempotencyKey = "death-completion",
                payloadJson = json.encodeToString(
                    WorkflowActionCompletePayload(workflowId, "death", "proof-row"),
                ),
                status = OutboxStatus.QUEUED.name,
                createdAt = 2L,
                updatedAt = 2L,
            ),
        )
        val staleServer = deathDetail(workflowId, firstStatus = "pending", secondBlocked = true)
        val api = Proxy.newProxyInstance(
            AppApi::class.java.classLoader,
            arrayOf(AppApi::class.java),
        ) { proxy, method, args ->
            when (method.name) {
                "getWorkflow" -> staleServer
                "toString" -> "WorkflowRefreshApi"
                "hashCode" -> System.identityHashCode(proxy)
                "equals" -> proxy === args?.firstOrNull()
                else -> error("Unexpected AppApi call: ${method.name}")
            }
        } as AppApi
        val repository = DefaultWorkflowsRepository(
            api = api,
            database = goatDatabase,
            outboxStore = RoomOutboxStore(outboxDatabase.outboxDao()),
            json = json,
            clock = { 3L },
        )

        assertTrue(repository.refreshDetail(workflowId).isSuccess)
        val refreshed = repository.observeDetail(workflowId).first()!!

        assertEquals("in_review", refreshed.actions.single { it.actionId == "death" }.status)
        assertEquals(1, refreshed.actionsDone)
        assertFalse(refreshed.actions.single { it.actionId == "postmortem" }.blocked)
    }

    @Test
    fun `queuing second death video updates outer cached card to two of two`() = runTest {
        val workflowId = "death-workflow"
        val cached = deathDetail(workflowId, firstStatus = "in_review", secondBlocked = false)
        goatDatabase.workflowDetailCacheDao().upsert(
            WorkflowDetailCacheEntity(workflowId, json.encodeToString(cached), updatedAt = 1L),
        )
        goatDatabase.workflowCardDao().upsertAll(
            listOf(
                WorkflowCardEntity(
                    queryKey = "death|today|all",
                    workflowId = workflowId,
                    sortIndex = 0,
                    dtoJson = json.encodeToString(
                        WorkflowCardDto(
                            workflowId = workflowId,
                            module = "death",
                            actionsDone = 0,
                            actionsTotal = 2,
                            nextAction = WorkflowNextActionDto(
                                key = "death_video",
                                title = "Record death video",
                            ),
                            state = "open",
                        ),
                    ),
                    updatedAt = 1L,
                ),
            ),
        )
        val repository = DefaultWorkflowsRepository(
            api = apiReturning(cached),
            database = goatDatabase,
            outboxStore = RoomOutboxStore(outboxDatabase.outboxDao()),
            json = json,
            clock = { 3L },
        )

        repository.markActionCompleted(workflowId, "postmortem", inReview = true)

        val card = goatDatabase.workflowCardDao().findById(workflowId)!!
            .let { json.decodeFromString<WorkflowCardDto>(it.dtoJson) }
        assertEquals(2, card.actionsDone)
        assertEquals(2, card.actionsTotal)
        assertEquals(null, card.nextAction)
    }

    private fun apiReturning(detail: WorkflowDetailResponseDto): AppApi =
        Proxy.newProxyInstance(
            AppApi::class.java.classLoader,
            arrayOf(AppApi::class.java),
        ) { proxy, method, args ->
            when (method.name) {
                "getWorkflow" -> detail
                "toString" -> "WorkflowRefreshApi"
                "hashCode" -> System.identityHashCode(proxy)
                "equals" -> proxy === args?.firstOrNull()
                else -> error("Unexpected AppApi call: ${method.name}")
            }
        } as AppApi

    private fun deathDetail(
        workflowId: String,
        firstStatus: String,
        secondBlocked: Boolean,
    ) = WorkflowDetailResponseDto(
        workflowId = workflowId,
        module = "death",
        actionsDone = 0,
        actionsTotal = 2,
        actions = listOf(
            WorkflowActionDto(
                actionId = "death",
                actionKey = "death_video",
                seq = 1,
                actionType = "action",
                status = firstStatus,
            ),
            WorkflowActionDto(
                actionId = "postmortem",
                actionKey = "post_mortem_video",
                seq = 2,
                actionType = "action",
                status = "pending",
                blocked = secondBlocked,
            ),
        ),
    )
}
