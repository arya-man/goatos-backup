package sg.mesha.goatos.core.data

import androidx.paging.testing.asSnapshot
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import java.lang.reflect.Proxy
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.data.cache.PenReconciliationItemEntity
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.CountsPenReconciliationCardDto
import sg.mesha.goatos.core.network.dto.CountsPenReconciliationListResponseDto

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class PenReconciliationRepositoryCacheTest {
    @Test
    fun `same millisecond status refresh still makes detail lookup pick latest snapshot`() = runTest {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val database = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        try {
            val api = Proxy.newProxyInstance(AppApi::class.java.classLoader, arrayOf(AppApi::class.java)) { proxy, method, args ->
                when (method.name) {
                    "listCountsPenReconciliationCards" -> {
                        val status = args?.get(0) as String?
                        CountsPenReconciliationListResponseDto(
                            items = listOf(
                                CountsPenReconciliationCardDto(
                                    cardId = "card-1",
                                    status = status.orEmpty(),
                                    raisedAt = "2026-09-02T10:00:00Z",
                                ),
                            ),
                        )
                    }
                    "toString" -> "PenReconciliationAppApiTestProxy"
                    "hashCode" -> System.identityHashCode(proxy)
                    "equals" -> proxy === args?.firstOrNull()
                    else -> error("unexpected AppApi method ${method.name}")
                }
            } as AppApi
            val repository = DefaultPenReconciliationRepository(
                api = api,
                database = database,
                clock = { 100L },
            )

            repository.cards("rework").asSnapshot()
            repository.cards("completed").asSnapshot()

            assertEquals("completed", repository.findCached("card-1")?.status)
        } finally {
            database.close()
        }
    }

    @Test
    fun `tied cached rows prefer furthest workflow state over stale actionable copy`() = runTest {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val database = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        try {
            val dao = database.penReconciliationItemDao()
            dao.upsertAll(
                listOf(
                    PenReconciliationItemEntity(
                        queryKey = "rework",
                        cardId = "card-1",
                        sortIndex = 0,
                        raisedAt = "2026-09-02T10:00:00Z",
                        dtoJson = """{"card_id":"card-1","status":"rework","raised_at":"2026-09-02T10:00:00Z"}""",
                        updatedAt = 100L,
                        statusRank = 2,
                    ),
                    PenReconciliationItemEntity(
                        queryKey = "completed",
                        cardId = "card-1",
                        sortIndex = 0,
                        raisedAt = "2026-09-02T10:00:00Z",
                        dtoJson = """{"card_id":"card-1","status":"completed","raised_at":"2026-09-02T10:00:00Z"}""",
                        updatedAt = 100L,
                        statusRank = 4,
                    ),
                ),
            )

            assertEquals("completed", repositoryFrom(database).findCached("card-1")?.status)
        } finally {
            database.close()
        }
    }

    private fun repositoryFrom(database: GoatDatabase): DefaultPenReconciliationRepository {
        val api = Proxy.newProxyInstance(AppApi::class.java.classLoader, arrayOf(AppApi::class.java)) { proxy, method, args ->
            when (method.name) {
                "toString" -> "PenReconciliationAppApiTestProxy"
                "hashCode" -> System.identityHashCode(proxy)
                "equals" -> proxy === args?.firstOrNull()
                else -> error("unexpected AppApi method ${method.name}")
            }
        } as AppApi
        return DefaultPenReconciliationRepository(api = api, database = database, clock = { 100L })
    }
}
