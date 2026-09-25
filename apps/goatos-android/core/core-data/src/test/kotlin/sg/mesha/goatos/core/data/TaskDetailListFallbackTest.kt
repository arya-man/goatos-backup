package sg.mesha.goatos.core.data

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import java.io.IOException
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.data.cache.LeadershipTaskItemEntity
import sg.mesha.goatos.core.data.cache.PenVisitItemEntity
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.LeadershipTaskDetailDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskDto
import sg.mesha.goatos.core.network.dto.PenVisitDetailDto
import sg.mesha.goatos.core.network.dto.PenVisitDto

/**
 * Found on the phone 2026-09-25 with the server stopped: a task or a pen visit whose LIST row was
 * already in Room opened to an endless spinner, because its detail read only the detail cache and
 * the failed refresh was swallowed. The list row IS the same DTO the detail renders, so the detail
 * must render from it while there is no detail cache — and a refresh must say whether it reached
 * the server, so a screen with neither can stop spinning and offer Try again.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class TaskDetailListFallbackTest {
    private val json = Json { ignoreUnknownKeys = true }

    private fun database() = Room.inMemoryDatabaseBuilder(
        ApplicationProvider.getApplicationContext(),
        GoatDatabase::class.java,
    ).allowMainThreadQueries().build()

    private class DownApi(delegate: AppApi = FakeAppApi()) : AppApi by delegate {
        override suspend fun getPenVisit(taskId: String): PenVisitDetailDto = throw IOException("server unreachable")
        override suspend fun getLeadershipTask(taskId: String): LeadershipTaskDetailDto = throw IOException("server unreachable")
    }

    @Test
    fun `an uncached pen visit renders from its cached list row while the server is down`() = runTest {
        val db = database()
        val visit = PenVisitDto(taskId = "visit-1", title = "Visit Castro 2 · Coimbatore", canSubmit = true, rowVersion = 3)
        db.penVisitItemDao().upsertAll(listOf(PenVisitItemEntity(queryKey = "q", grainKey = "visit-1", sortIndex = 0, dtoJson = json.encodeToString(visit), updatedAt = 1L)))
        val repository = DefaultPenVisitsRepository(DownApi(), db, json)

        assertEquals("the refresh reports it never reached the server", false, repository.refreshVisit("visit-1"))
        assertEquals(visit, repository.observeVisit("visit-1").first())
        assertNull("no row, no invention", repository.observeVisit("visit-2").first())
        db.close()
    }

    @Test
    fun `an uncached leadership task renders from its cached list row while the server is down`() = runTest {
        val db = database()
        val task = LeadershipTaskDto(taskId = "task-1", title = "Fix the CPT water line", rowVersion = 5)
        db.leadershipTaskItemDao().upsertAll(listOf(LeadershipTaskItemEntity(queryKey = "q", grainKey = "task-1", sortIndex = 0, dtoJson = json.encodeToString(task), updatedAt = 1L)))
        val bootstrap = object : BootstrapRepository {
            override suspend fun loadNavState() = error("unused")
            override suspend fun operatorProfile() = null
        }
        val repository = DefaultLeadershipTasksRepository(
            api = DownApi(),
            database = db,
            bootstrapRepository = bootstrap,
            cacheRoot = ApplicationProvider.getApplicationContext<android.content.Context>().cacheDir,
            json = json,
        )

        assertEquals(false, repository.refreshTaskDetail("task-1"))
        assertEquals(task, repository.observeTaskDetail("task-1").first())
        assertNull(repository.observeTaskDetail("task-2").first())
        db.close()
    }
}
