package sg.mesha.goatos.core.network

import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import retrofit2.HttpException
import retrofit2.Response
import java.io.IOException

/** Verifies [isTerminalAppApiError] so the outbox terminalizes definitive 4xx client errors
 *  immediately instead of burning the full backoff budget, while keeping auth/timeout/rate-limit
 *  and all transport/5xx failures retryable. */
class RetryClassificationTest {

    private fun httpError(code: Int): HttpException =
        HttpException(Response.error<Any>(code, "".toResponseBody(null)))

    @Test
    fun `definitive 4xx client errors are terminal`() {
        listOf(400, 404, 409, 422).forEach { code ->
            assertTrue("HTTP $code should be terminal", httpError(code).isTerminalAppApiError())
        }
    }

    @Test
    fun `auth, timeout and rate-limit 4xx stay retryable`() {
        listOf(401, 403, 408, 425, 429).forEach { code ->
            assertFalse("HTTP $code should be retryable", httpError(code).isTerminalAppApiError())
        }
    }

    @Test
    fun `5xx server errors stay retryable`() {
        listOf(500, 502, 503).forEach { code ->
            assertFalse("HTTP $code should be retryable", httpError(code).isTerminalAppApiError())
        }
    }

    @Test
    fun `non-http transport failures stay retryable`() {
        assertFalse(IOException("network down").isTerminalAppApiError())
        assertFalse(RuntimeException("boom").isTerminalAppApiError())
    }
}
