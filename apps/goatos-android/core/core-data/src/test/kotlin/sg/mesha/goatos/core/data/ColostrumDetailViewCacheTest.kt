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
import org.junit.Assert.assertNotNull
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.data.cache.WorkflowCardEntity
import sg.mesha.goatos.core.data.cache.WorkflowDetailCacheEntity
import sg.mesha.goatos.core.data.sync.RoomOutboxStore
import sg.mesha.goatos.core.database.outbox.OutboxDatabase
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.WorkflowActionDto
import sg.mesha.goatos.core.network.dto.WorkflowCardDto
import sg.mesha.goatos.core.network.dto.WorkflowDetailResponseDto
import sg.mesha.goatos.core.network.dto.WorkflowNextActionDto

/**
 * One kid legitimately has TWO cached details at once: the Birth detail (every task) and the
 * Colostrum detail for a date (that day's feeds, day-grain counters). They are different documents
 * about the same animal, and the cache has to hold both without either corrupting the other
 * (docs/decisions/colostrum-milk-module.md).
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class ColostrumDetailViewCacheTest {
    private lateinit var goatDatabase: GoatDatabase
    private lateinit var outboxDatabase: OutboxDatabase
    private val json = Json { ignoreUnknownKeys = true }

    private val workflowId = "kid-workflow"
    private val date = "2026-08-06"

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

    /**
     * The clobbering bug this cache key exists to prevent: with a shared key, opening the kid in
     * Colostrum would overwrite its Birth detail, and the Birth screen would then show that day's
     * feeds where the kid's whole task list belongs.
     */
    @Test
    fun `the colostrum view and the birth view of one kid are cached separately`() = runTest {
        val repository = repositoryReturning(colostrumDetail())
        goatDatabase.workflowDetailCacheDao().upsert(
            WorkflowDetailCacheEntity(workflowId, json.encodeToString(birthDetail()), updatedAt = 1L),
        )

        assertEquals(
            true,
            repository.refreshDetail(workflowId, lens = WORKFLOW_MODULE_COLOSTRUM, date = date).isSuccess,
        )

        val colostrum = repository.observeDetail(workflowId, WORKFLOW_MODULE_COLOSTRUM, date).first()
        assertNotNull("the colostrum view must be cached", colostrum)
        assertEquals(2, colostrum!!.actions.size)
        assertEquals(2, colostrum.actionsTotal)

        val birth = repository.observeDetail(workflowId).first()
        assertNotNull("the birth view must survive a colostrum refresh", birth)
        assertEquals(
            "the birth detail still holds every task, not the colostrum subset",
            4,
            birth!!.actions.size,
        )
        assertEquals(4, birth.actionsTotal)
    }

    /**
     * A completion belongs to both views, because it is one row. The operator who feeds from
     * Colostrum must not find the same feed still pending in Birth.
     */
    @Test
    fun `an optimistic completion reaches every cached view of the workflow`() = runTest {
        val repository = repositoryReturning(colostrumDetail())
        val detailDao = goatDatabase.workflowDetailCacheDao()
        detailDao.upsert(
            WorkflowDetailCacheEntity(workflowId, json.encodeToString(birthDetail()), updatedAt = 1L),
        )
        detailDao.upsert(
            WorkflowDetailCacheEntity(
                detailCacheKey(workflowId, WORKFLOW_MODULE_COLOSTRUM, date),
                json.encodeToString(colostrumDetail()),
                updatedAt = 1L,
            ),
        )

        repository.markActionCompleted(workflowId, "feed-1100", inReview = false)

        val colostrum = repository.observeDetail(workflowId, WORKFLOW_MODULE_COLOSTRUM, date).first()!!
        val birth = repository.observeDetail(workflowId).first()!!
        assertEquals(
            "completed",
            colostrum.actions.single { it.actionId == "feed-1100" }.status,
        )
        assertEquals(
            "the same feed is the same row in Birth — one state, two entry points",
            "completed",
            birth.actions.single { it.actionId == "feed-1100" }.status,
        )
    }

    /**
     * THE CROSS-GRAIN TEST. A colostrum card counts one day's feeds; a birth card counts every
     * operator action. Updating a birth card from the colostrum view would publish the day's
     * numbers as the kid's overall progress — the cross-surface count-parity defect.
     */
    @Test
    fun `each card is updated from a detail of its own grain`() = runTest {
        val repository = repositoryReturning(colostrumDetail())
        val detailDao = goatDatabase.workflowDetailCacheDao()
        detailDao.upsert(
            WorkflowDetailCacheEntity(workflowId, json.encodeToString(birthDetail()), updatedAt = 1L),
        )
        detailDao.upsert(
            WorkflowDetailCacheEntity(
                detailCacheKey(workflowId, WORKFLOW_MODULE_COLOSTRUM, date),
                json.encodeToString(colostrumDetail()),
                updatedAt = 1L,
            ),
        )
        goatDatabase.workflowCardDao().upsertAll(
            listOf(
                cardEntity(queryKey = "workflows|birth|$date|all", module = "birth", done = 2, total = 4),
                cardEntity(queryKey = "workflows|colostrum|$date|all", module = "colostrum", done = 1, total = 2),
            ),
        )

        repository.markActionCompleted(workflowId, "feed-1100", inReview = false)

        val cards = goatDatabase.workflowCardDao().findAllById(workflowId, 10)
            .associate { entity ->
                val card = json.decodeFromString<WorkflowCardDto>(entity.dtoJson)
                entity.queryKey to card
            }
        val birthCard = cards.getValue("workflows|birth|$date|all")
        val colostrumCard = cards.getValue("workflows|colostrum|$date|all")

        // Each card's denominator is the size of ITS OWN view's action list, which is exactly why
        // pairing a card with the wrong view is detectable: had the birth card been rewritten from
        // the colostrum view, its total would read 2 (that day's feeds) instead of 4.
        assertEquals(
            "the birth card must NOT be rewritten from the colostrum view — its total would collapse " +
                "to that day's feed count",
            4,
            birthCard.actionsTotal,
        )
        assertEquals(
            "the colostrum card keeps its day denominator",
            2,
            colostrumCard.actionsTotal,
        )
        assertEquals(
            "the colostrum card counts the feed just completed",
            2,
            colostrumCard.actionsDone,
        )
        assertEquals(
            "the birth card counts the same completion inside its own larger grain",
            3,
            birthCard.actionsDone,
        )
    }

    @Test
    fun `a card scope maps to the detail view of its own grain`() {
        assertEquals(
            detailCacheKey(workflowId, WORKFLOW_MODULE_COLOSTRUM, date),
            detailViewKeyForCard(workflowId, "workflows|colostrum|$date|all"),
        )
        assertEquals(workflowId, detailViewKeyForCard(workflowId, "workflows|birth|$date|all"))
        assertEquals(workflowId, detailViewKeyForCard(workflowId, "workflows|death|$date|overdue"))
    }

    // -----------------------------------------------------------------------

    private fun repositoryReturning(detail: WorkflowDetailResponseDto): DefaultWorkflowsRepository {
        val api = Proxy.newProxyInstance(
            AppApi::class.java.classLoader,
            arrayOf(AppApi::class.java),
        ) { proxy, method, args ->
            when (method.name) {
                "getWorkflow" -> detail
                "toString" -> "ColostrumCacheApi"
                "hashCode" -> System.identityHashCode(proxy)
                "equals" -> proxy === args?.firstOrNull()
                else -> error("Unexpected AppApi call: ${method.name}")
            }
        } as AppApi
        return DefaultWorkflowsRepository(
            api = api,
            database = goatDatabase,
            outboxStore = RoomOutboxStore(outboxDatabase.outboxDao()),
            json = json,
            clock = { 3L },
        )
    }

    private fun cardEntity(queryKey: String, module: String, done: Int, total: Int) = WorkflowCardEntity(
        queryKey = queryKey,
        workflowId = workflowId,
        sortIndex = 0,
        dtoJson = json.encodeToString(
            WorkflowCardDto(
                workflowId = workflowId,
                module = module,
                actionsDone = done,
                actionsTotal = total,
                nextAction = WorkflowNextActionDto(key = "colostrum_day_2_1100", title = "6th Colostrum"),
                state = "open",
            ),
        ),
        updatedAt = 1L,
    )

    /** The kid's whole task list as the Birth screen sees it (trimmed to four representative rows). */
    private fun birthDetail() = WorkflowDetailResponseDto(
        workflowId = workflowId,
        module = "birth",
        actionsDone = 2,
        actionsTotal = 4,
        actions = listOf(
            WorkflowActionDto(actionId = "clean", actionKey = "kid_clean", seq = 1, actionType = "question", status = "completed"),
            WorkflowActionDto(actionId = "first", actionKey = "first_colostrum", seq = 5, actionType = "action", status = "completed"),
            WorkflowActionDto(actionId = "feed-1100", actionKey = "colostrum_day_2_1100", seq = 10, actionType = "action", status = "pending"),
            WorkflowActionDto(actionId = "tag", actionKey = "tag_the_kid", seq = 14, actionType = "action", status = "pending"),
        ),
    )

    /** The same kid on 2026-08-06 as the Colostrum screen sees it: that day's feeds, day counters. */
    private fun colostrumDetail() = WorkflowDetailResponseDto(
        workflowId = workflowId,
        module = "colostrum",
        actionsDone = 1,
        actionsTotal = 2,
        actions = listOf(
            WorkflowActionDto(actionId = "feed-0700", actionKey = "colostrum_day_2_0700", seq = 9, actionType = "action", status = "completed"),
            WorkflowActionDto(actionId = "feed-1100", actionKey = "colostrum_day_2_1100", seq = 10, actionType = "action", status = "pending"),
        ),
    )
}
