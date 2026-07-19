package sg.mesha.goatos.core.network

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.encodeToString
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.network.dto.TaskListResponseDto
import sg.mesha.goatos.core.network.dto.TaskSummaryDto

class AppTaskPaginationContractTest {

    @Test
    fun taskPageUsesTwentyRowBoundaryAndCarriesCompletenessMetadata() {
        val server = MockWebServer()
        server.start()
        try {
            val expected = TaskListResponseDto(
                items = (1..APP_TASK_PAGE_SIZE).map { TaskSummaryDto(taskId = "task-$it") },
                total = 21,
                hasMore = true,
                nextCursor = "cursor-20",
                traceId = "trace-1",
            )
            server.enqueue(
                MockResponse()
                    .setResponseCode(200)
                    .setHeader("Content-Type", "application/json")
                    .setBody(NetworkFactory.json.encodeToString(expected)),
            )

            val actual = runBlocking {
                NetworkFactory.appApi(server.url("/").toString(), tokenProvider = { "token" })
                    .listAppTasks(cursor = "cursor-previous")
            }
            val request = server.takeRequest()

            assertEquals(20, APP_TASK_PAGE_SIZE)
            assertEquals("20", request.requestUrl?.queryParameter("limit"))
            assertEquals("cursor-previous", request.requestUrl?.queryParameter("cursor"))
            assertEquals(20, actual.items.size)
            assertEquals(21L, actual.total)
            assertTrue(actual.hasMore)
            assertEquals("cursor-20", actual.nextCursor)
        } finally {
            server.shutdown()
        }
    }
}
