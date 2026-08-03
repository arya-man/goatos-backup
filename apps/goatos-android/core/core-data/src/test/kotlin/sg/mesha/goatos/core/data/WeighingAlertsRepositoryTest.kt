package sg.mesha.goatos.core.data

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
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
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.WeighingAlertDto
import sg.mesha.goatos.core.network.dto.WeighingAlertPageResponseDto

/**
 * Offline-first proof for the weighing alerts feed.
 *
 * The screen this backs is opened in a shed, where the network is worst, and it carries the
 * messages that told an operator what to do — work assigned to them, a proof sent back for
 * rework. A refresh that fails there must NOT blank the list.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class WeighingAlertsRepositoryTest {

    private fun database() = Room.inMemoryDatabaseBuilder(
        ApplicationProvider.getApplicationContext(),
        GoatDatabase::class.java,
    ).allowMainThreadQueries().build()

    private val page = WeighingAlertPageResponseDto(
        items = listOf(
            WeighingAlertDto(
                alertId = "a1",
                kind = "weighing_campaign_published",
                direction = "downstream",
                title = "New weighing work",
                body = "Weighing work is assigned to you: Shed 1.",
                severity = "normal",
                target = "/weighing",
                occurredAt = "2026-08-03T04:00:00Z",
            ),
        ),
        title = "Weighing alerts",
        emptyMessage = "No weighing updates yet.",
    )

    /** Delegates every other endpoint to [FakeAppApi]; only the alerts read is scripted. */
    private class StubApi(
        private val response: WeighingAlertPageResponseDto? = null,
        private val failure: Throwable? = null,
        delegate: AppApi = FakeAppApi(),
    ) : AppApi by delegate {
        var calls = 0
        override suspend fun listWeighingAlerts(cursor: String?, limit: Int): WeighingAlertPageResponseDto {
            calls++
            failure?.let { throw it }
            return response ?: WeighingAlertPageResponseDto()
        }
    }

    @Test
    fun `cold cache emits no data, then a successful refresh makes Room the source of truth`() = runTest {
        val db = database()
        val api = StubApi(response = page)
        val repo = DefaultWeighingAlertsRepository(api, db.weighingAlertsCacheDao())

        assertNull("cold cache must emit null data, not a fabricated row", repo.observeAlerts().first().data)

        assertTrue(repo.refreshAlerts().isSuccess)

        val cached = repo.observeAlerts().first().data
        assertEquals("Weighing alerts", cached?.title)
        assertEquals(1, cached?.items?.size)
        assertEquals("New weighing work", cached?.items?.first()?.title)
        // The row survives a brand-new repository instance over the same Room file: the cache is
        // the source of truth, not in-memory state that dies with the ViewModel.
        val reopened = DefaultWeighingAlertsRepository(StubApi(), db.weighingAlertsCacheDao())
        assertEquals(1, reopened.observeAlerts().first().data?.items?.size)
        db.close()
    }

    @Test
    fun `a failed refresh keeps the cached alerts on screen instead of blanking them`() = runTest {
        val db = database()
        val dao = db.weighingAlertsCacheDao()
        assertTrue(DefaultWeighingAlertsRepository(StubApi(response = page), dao).refreshAlerts().isSuccess)

        val offline = DefaultWeighingAlertsRepository(StubApi(failure = java.io.IOException("no network")), dao)
        val result = offline.refreshAlerts()

        assertTrue("the failure must be reported so the caller can flag offline", result.isFailure)
        val stillThere = offline.observeAlerts().first().data
        assertEquals(
            "a failed refresh wiped the cache; the operator loses the message that told them what to do",
            1,
            stillThere?.items?.size,
        )
        db.close()
    }

    @Test
    fun `the feed asks the backend for the caller's own alerts with no identity parameter`() = runTest {
        // There is deliberately no operator/member/park argument on this API: the audience is
        // derived server-side from the session. A client-supplied identity would be a way to ask
        // for somebody else's alerts.
        val db = database()
        val api = StubApi(response = page)
        val repo = DefaultWeighingAlertsRepository(api, db.weighingAlertsCacheDao())
        repo.refreshAlerts()
        assertEquals(1, api.calls)
        db.close()
    }
}
