package sg.mesha.goatos.viewmodel

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import java.time.LocalDate
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertFalse
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.boot.RecordingAnalytics
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.data.FeedTransportRepository
import sg.mesha.goatos.core.data.GoatDatabase
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.FeedTransportFilterOptionDto
import sg.mesha.goatos.core.network.dto.FeedTransportFilterOptionsDto
import sg.mesha.goatos.core.network.dto.FeedTransportTaskDto
import sg.mesha.goatos.core.network.dto.FeedTransportTaskPageDto
import sg.mesha.goatos.feature.feed.FeedTransportCaptureUiState
import sg.mesha.goatos.feature.feed.FeedTransportEvent
import sg.mesha.goatos.feature.feed.FeedTransportResultUi
import sg.mesha.goatos.feature.feed.FeedTransportSubmitStatus

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
@OptIn(ExperimentalCoroutinesApi::class)
class FeedTransportSequenceTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `submit follows distribution proof gating`() {
        assertFalse(FeedTransportCaptureUiState().submitEnabled)
        assertFalse(FeedTransportCaptureUiState(isCapturing = true, videoCaptured = true).submitEnabled)
        assertTrue(FeedTransportCaptureUiState(videoCaptured = true).submitEnabled)
        assertFalse(
            FeedTransportCaptureUiState(
                videoCaptured = true,
                result = FeedTransportResultUi(FeedTransportSubmitStatus.QUEUED, "Submitted"),
            ).submitEnabled,
        )
    }

    @Test
    fun `duplicate transport filter selections do not emit duplicate analytics`() = runTest(dispatcher) {
        val database = Room.inMemoryDatabaseBuilder(
            ApplicationProvider.getApplicationContext(),
            GoatDatabase::class.java,
        ).allowMainThreadQueries().build()
        try {
            val api = object : AppApi by FakeAppApi() {
                override suspend fun getFeedTransportTasks(
                    businessDate: String,
                    parkId: String?,
                    shedId: String?,
                    partitionLabel: String?,
                    status: String?,
                    cursor: String?,
                    limit: Int?,
                ): FeedTransportTaskPageDto = FeedTransportTaskPageDto(
                    items = listOf(
                        FeedTransportTaskDto(
                            taskId = "task-${parkId.orEmpty()}-${shedId.orEmpty()}-${partitionLabel.orEmpty()}-${status.orEmpty()}",
                            parkId = parkId ?: "park-1",
                            parkLabel = "Farm 1",
                            shedId = shedId ?: "shed-1",
                            shedLabel = "Shed 1",
                            partitionLabel = partitionLabel,
                            operationalLocationDisplay = listOfNotNull("Shed 1", partitionLabel).joinToString(" - "),
                            businessDate = businessDate,
                            status = status ?: "due",
                            scheduledAt = "2026-07-29T15:30:00+05:30",
                        ),
                    ),
                    filters = FeedTransportFilterOptionsDto(
                        parks = listOf(FeedTransportFilterOptionDto("park-1", "Farm 1")),
                        sheds = listOf(FeedTransportFilterOptionDto("shed-1", "Shed 1 - Part 3", "Part 3")),
                    ),
                )
            }
            val analytics = RecordingAnalytics()
            val viewModel = FeedTransportViewModel(
                repo = FeedTransportRepository(api, database),
                analytics = analytics,
                crashReporter = NoopCrashReporter(),
            )
            advanceUntilIdle()

            viewModel.onEvent(FeedTransportEvent.SelectShed("shed-1\u001fPart 3"))
            viewModel.onEvent(FeedTransportEvent.SelectShed("shed-1\u001fPart 3"))
            viewModel.onEvent(FeedTransportEvent.SelectStatus("completed"))
            viewModel.onEvent(FeedTransportEvent.SelectStatus("completed"))
            viewModel.onEvent(FeedTransportEvent.SelectPark("park-1"))
            viewModel.onEvent(FeedTransportEvent.SelectPark("park-1"))
            viewModel.onEvent(FeedTransportEvent.SelectDate(LocalDate.now().minusDays(1)))
            viewModel.onEvent(FeedTransportEvent.SelectDate(LocalDate.now().minusDays(1)))
            viewModel.onEvent(FeedTransportEvent.ClearFilters)
            viewModel.onEvent(FeedTransportEvent.ClearFilters)
            advanceUntilIdle()

            assertEquals(
                "re-selecting the already-active transport filter must not pollute the funnel",
                5,
                analytics.events.count { it.name == AnalyticsEvents.FEED_FILTER_APPLIED },
            )
        } finally {
            database.close()
        }
    }
}
