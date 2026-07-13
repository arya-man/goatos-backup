package sg.mesha.goatos.viewmodel

import app.cash.turbine.test
import kotlinx.coroutines.test.runTest
import org.junit.Test
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.VerificationRepository
import sg.mesha.goatos.core.network.dto.VerificationMediaItem
import sg.mesha.goatos.core.network.dto.VerificationQueueItem
import sg.mesha.goatos.core.network.dto.VerificationQueueResponseDto
import sg.mesha.goatos.core.network.dto.VerificationSourceRef
import sg.mesha.goatos.core.network.dto.VerificationStatus
import java.util.UUID
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

class FakeVerificationRepository : VerificationRepository {
    private var queueResponse: Resource<VerificationQueueResponseDto> =
        Resource(data = VerificationQueueResponseDto(items = emptyList()))

    fun setQueueResponse(response: Resource<VerificationQueueResponseDto>) {
        queueResponse = response
    }

    override fun observeQueue(category: String?, limit: Int) = kotlinx.coroutines.flow.flowOf(queueResponse)
    override suspend fun refreshQueue(category: String?, limit: Int): Result<Unit> = Result.success(Unit)
    override suspend fun appendQueue(cursor: String, category: String?, limit: Int): Result<Unit> = Result.success(Unit)
}

class FakeAnalyticsPort : AnalyticsPort {
    override fun track(event: String, properties: Map<String, Any>) {}
}

class VerifyQueueViewModelTest {

    @Test
    fun `renders labels instead of UUIDs for shed, park, and operator`() = runTest {
        val shedId = UUID.randomUUID().toString()
        val parkId = UUID.randomUUID().toString()
        val operatorId = UUID.randomUUID().toString()

        val item = VerificationQueueItem(
            itemId = UUID.randomUUID().toString(),
            vertical = "preventive_care",
            module = "vaccination",
            category = "vaccination_proof",
            status = VerificationStatus.PENDING,
            verdictReason = null,
            operatorId = operatorId,
            operatorName = "Ravi Operator",  // Label should be used
            shedId = shedId,
            shedLabel = "Shed A",  // Label should be used
            parkId = parkId,
            parkLabel = "Park 1",  // Label should be used
            capturedAt = "2026-07-13T10:00:00Z",
            verifiedBy = null,
            verifiedAt = null,
            rowVersion = 1,
            media = emptyList(),
            source = VerificationSourceRef(
                module = "vaccination",
                taskId = null,
                submissionId = null,
                refType = "sop_submission",
                refId = UUID.randomUUID().toString(),
            ),
        )

        val fakeRepo = FakeVerificationRepository()
        fakeRepo.setQueueResponse(
            Resource(
                data = VerificationQueueResponseDto(
                    items = listOf(item),
                    nextCursor = null,
                    traceId = "test-trace",
                ),
            ),
        )

        val viewModel = VerifyQueueViewModel(fakeRepo, FakeAnalyticsPort())

        viewModel.state.test {
            val state = awaitItem()

            // Verify that we have one row
            assertEquals(1, state.rows.size)

            val row = state.rows.first()

            // Verify title uses shed label, not shed UUID
            assertEquals("Shed A", row.title)
            assertTrue(
                !row.title.contains(shedId),
                "Title should not contain shed UUID, but it does: ${row.title}"
            )

            // Verify subtitle uses labels, not UUIDs
            assertTrue(
                row.subtitle.contains("Park 1"),
                "Subtitle should contain park label: ${row.subtitle}"
            )
            assertTrue(
                row.subtitle.contains("Ravi Operator"),
                "Subtitle should contain operator name: ${row.subtitle}"
            )

            // Verify that raw UUIDs are NOT in subtitle (regression test for STATUS-003)
            assertTrue(
                !row.subtitle.contains(parkId),
                "Subtitle should not contain park UUID: ${row.subtitle}"
            )
            assertTrue(
                !row.subtitle.contains(operatorId),
                "Subtitle should not contain operator UUID: ${row.subtitle}"
            )

            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `falls back to UUID when label is null`() = runTest {
        val shedId = UUID.randomUUID().toString()

        val item = VerificationQueueItem(
            itemId = UUID.randomUUID().toString(),
            vertical = "preventive_care",
            module = "vaccination",
            category = "vaccination_proof",
            status = VerificationStatus.PENDING,
            verdictReason = null,
            operatorId = null,
            operatorName = null,
            shedId = shedId,
            shedLabel = null,  // No label, should fall back to ID
            parkId = null,
            parkLabel = null,
            capturedAt = "2026-07-13T10:00:00Z",
            verifiedBy = null,
            verifiedAt = null,
            rowVersion = 1,
            media = emptyList(),
            source = VerificationSourceRef(
                module = "vaccination",
                taskId = null,
                submissionId = null,
                refType = "sop_submission",
                refId = UUID.randomUUID().toString(),
            ),
        )

        val fakeRepo = FakeVerificationRepository()
        fakeRepo.setQueueResponse(
            Resource(
                data = VerificationQueueResponseDto(
                    items = listOf(item),
                    nextCursor = null,
                    traceId = "test-trace",
                ),
            ),
        )

        val viewModel = VerifyQueueViewModel(fakeRepo, FakeAnalyticsPort())

        viewModel.state.test {
            val state = awaitItem()
            val row = state.rows.first()

            // When label is null, title should be the UUID
            assertEquals(shedId, row.title)

            cancelAndIgnoreRemainingEvents()
        }
    }
}
