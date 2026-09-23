package sg.mesha.goatos.analytics

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import sg.mesha.goatos.core.analytics.AnalyticsContext
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.data.sync.ConnectivitySource
import sg.mesha.goatos.core.data.sync.ConnectivitySyncTrigger
import sg.mesha.goatos.core.network.AppAnalyticsEventRequestDto
import sg.mesha.goatos.core.network.AppAnalyticsEventResponseDto
import sg.mesha.goatos.core.network.AppApi
import javax.inject.Provider

@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
class ConnectivityAnalyticsDrainTest {
    private class FakeSource : ConnectivitySource {
        private var listener: ((Boolean) -> Unit)? = null
        override fun start(onChange: (Boolean) -> Unit): AutoCloseable {
            listener = onChange
            return AutoCloseable { listener = null }
        }

        fun emit(online: Boolean) = listener?.invoke(online)
    }

    private class FakeApi(@Volatile var offline: Boolean) : AppApi by sg.mesha.goatos.core.network.FakeAppApi() {
        val received = mutableListOf<AppAnalyticsEventRequestDto>()
        override suspend fun recordAnalyticsEvent(request: AppAnalyticsEventRequestDto): AppAnalyticsEventResponseDto {
            if (offline) throw java.io.IOException("offline")
            received += request
            return AppAnalyticsEventResponseDto(accepted = true)
        }
    }

    @Test
    fun `offline critical event drains on reconnect`() = runTest {
        val context = ApplicationProvider.getApplicationContext<Context>()
        val queue = DurableAnalyticsQueue(context, ioDispatcher = UnconfinedTestDispatcher(testScheduler))
        val api = FakeApi(offline = true)
        val adapter = BackendAnalyticsAdapter(
            apiProvider = Provider { api },
            appScope = this,
            analyticsContext = AnalyticsContext(flavor = "dev"),
            queue = queue,
        )
        adapter.track(AnalyticsEvents.PROOF_PROCESSING_FAILED, mapOf("proof_stage" to "overlay"))
        advanceUntilIdle()
        assertEquals(1, queue.size())

        val source = FakeSource()
        val trigger = ConnectivitySyncTrigger(source) { online ->
            if (online) launch { adapter.drainQueue() }
        }
        trigger.start()
        api.offline = false
        source.emit(true)
        advanceUntilIdle()

        assertEquals(0, queue.size())
        assertEquals(listOf(AnalyticsEvents.PROOF_PROCESSING_FAILED), api.received.map { it.eventName })
        trigger.stop()
    }
}
