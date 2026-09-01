package sg.mesha.goatos.core.data

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.data.cache.HealthWorkItemDetailEntity
import sg.mesha.goatos.core.data.cache.HealthWorkItemEntity
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.HealthSummaryDto
import sg.mesha.goatos.core.network.dto.HealthWorkItemDetailDto
import sg.mesha.goatos.core.network.dto.HealthWorkItemDto
import sg.mesha.goatos.core.network.dto.HealthWorkItemPageDto

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class HealthRepositoryConvergenceTest {
    private val json = Json { ignoreUnknownKeys = true }

    @Test
    fun `case-open refresh replaces stale scope with canonical server list`() = runTest {
        withDatabase { database ->
            val filters = HealthFilters(ageBand = "adult", date = "2026-08-18")
            database.healthWorkItemDao().upsertAll(
                listOf(entity(filters.scopeKey, workItem("stale-session", "due"))),
            )
            val api = object : AppApi by FakeAppApi() {
                override suspend fun listHealthWorkItems(
                    ageBand: String,
                    date: String,
                    status: String?,
                    diseaseKey: String?,
                    parkId: String?,
                    shedId: String?,
                    session: String?,
                    cursor: String?,
                    limit: Int?,
                ): HealthWorkItemPageDto = HealthWorkItemPageDto(
                    items = listOf(workItem("canonical-session", "due")),
                    summary = HealthSummaryDto(total = 1, due = 1),
                )
            }
            val repository = DefaultHealthRepository(api, database, json, clock = { 100L })

            assertTrue(repository.refreshWorkItems(filters).isSuccess)

            assertTrue(database.healthWorkItemDao().findAll("stale-session").isEmpty())
            assertEquals(1, database.healthWorkItemDao().findAll("canonical-session").size)
            assertEquals(1, repository.observePageMeta(filters).first()?.page?.summary?.total)
        }
    }

    @Test
    fun `rejected treatment repairs optimistic completion in detail and every cached list scope`() = runTest {
        withDatabase { database ->
            val completedItem = workItem("health-session-1", "completed")
            database.healthWorkItemDao().upsertAll(
                listOf(
                    entity(HealthFilters("adult", "2026-08-18").scopeKey, completedItem),
                    entity(HealthFilters("adult", "2026-08-18", status = "due").scopeKey, completedItem),
                ),
            )
            database.healthWorkItemDetailDao().upsert(
                HealthWorkItemDetailEntity(
                    healthSessionId = "health-session-1",
                    dtoJson = json.encodeToString(detail("completed")),
                    updatedAt = 1L,
                ),
            )
            var detailCalls = 0
            val api = object : AppApi by FakeAppApi() {
                override suspend fun listHealthWorkItems(
                    ageBand: String,
                    date: String,
                    status: String?,
                    diseaseKey: String?,
                    parkId: String?,
                    shedId: String?,
                    session: String?,
                    cursor: String?,
                    limit: Int?,
                ): HealthWorkItemPageDto = HealthWorkItemPageDto(
                    items = if (status == null || status == "due") listOf(workItem("health-session-1", "due")) else emptyList(),
                    summary = HealthSummaryDto(total = 1, due = 1),
                )

                override suspend fun getHealthWorkItem(healthSessionId: String): HealthWorkItemDetailDto {
                    detailCalls += 1
                    return detail("due")
                }
            }
            val repository = DefaultHealthRepository(api, database, json, clock = { 200L })

            assertTrue(repository.reconcileRejectedTreatmentCompletion("health-session-1").isSuccess)

            assertEquals(1, detailCalls)
            val cachedStatuses = database.healthWorkItemDao().findAll("health-session-1")
                .map { json.decodeFromString<HealthWorkItemDto>(it.dtoJson).status }
            assertEquals(3, cachedStatuses.size)
            assertTrue(cachedStatuses.all { it == "due" })
            assertEquals("due", repository.observeDetail("health-session-1").first()?.status)
        }
    }

    private suspend fun withDatabase(block: suspend (GoatDatabase) -> Unit) {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val database = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        try {
            block(database)
        } finally {
            database.close()
        }
    }

    private fun entity(scopeKey: String, item: HealthWorkItemDto) = HealthWorkItemEntity(
        scopeKey = scopeKey,
        healthSessionId = item.healthSessionId,
        sortIndex = 0,
        dtoJson = json.encodeToString(item),
        updatedAt = 1L,
    )

    private fun workItem(id: String, status: String) = HealthWorkItemDto(
        healthSessionId = id,
        caseId = "case-1",
        goatId = "goat-1",
        goatDisplayId = "G-1",
        diseaseKey = "fever",
        diseaseName = "Fever",
        ageBand = "adult",
        dayNo = 1,
        durationDays = 3,
        businessDate = "2026-08-18",
        session = "morning",
        status = status,
    )

    private fun detail(status: String) = HealthWorkItemDetailDto(
        healthSessionId = "health-session-1",
        caseId = "case-1",
        goatId = "goat-1",
        goatDisplayId = "G-1",
        diseaseKey = "fever",
        diseaseName = "Fever",
        ageBand = "adult",
        dayNo = 1,
        durationDays = 3,
        businessDate = "2026-08-18",
        session = "morning",
        status = status,
    )
}
