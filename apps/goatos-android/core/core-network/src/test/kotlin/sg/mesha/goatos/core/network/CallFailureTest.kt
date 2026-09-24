package sg.mesha.goatos.core.network

import kotlinx.serialization.SerializationException
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import retrofit2.HttpException
import retrofit2.Response
import java.io.IOException
import java.net.UnknownHostException

class CallFailureTest {
    private val now = 1_750_000_000_000L // fixed clock
    private val cap = 120_000L

    private fun http(code: Int, retryAfter: String? = null): HttpException {
        val raw = okhttp3.Response.Builder()
            .request(Request.Builder().url("https://api.goatos.mesha.sg/x").build())
            .protocol(Protocol.HTTP_1_1)
            .code(code)
            .message("HTTP $code")
            .apply { if (retryAfter != null) header("Retry-After", retryAfter) }
            .build()
        return HttpException(Response.error<Any>("{}".toResponseBody(), raw))
    }

    @Test fun `delta seconds`() = assertEquals(20_000L, parseRetryAfterMs("20", now))

    @Test fun `http date in the future`() =
        assertEquals(30_000L, parseRetryAfterMs("Sun, 15 Jun 2025 14:50:30 GMT", 1_749_999_000_000L))

    @Test fun `http date in the past is zero`() = assertEquals(0L, parseRetryAfterMs("Sun, 15 Jun 2025 15:06:10 GMT", now))

    @Test fun `garbage and blank are ignored`() {
        assertNull(parseRetryAfterMs("soon", now))
        assertNull(parseRetryAfterMs("  ", now))
        assertNull(parseRetryAfterMs(null, now))
    }

    @Test fun `large delta is capped`() = assertEquals(cap, parseRetryAfterMs("600", now))

    @Test fun `overflowing delta is capped, not wrapped negative`() =
        assertEquals(cap, parseRetryAfterMs("9223372036854775807", now))

    @Test fun `far future date is capped`() = assertEquals(cap, parseRetryAfterMs("Fri, 31 Dec 9999 23:59:59 GMT", now))

    @Test fun `negative delta is zero`() = assertEquals(0L, parseRetryAfterMs("-5", now))

    @Test fun `classification`() {
        assertEquals(CallFailure.Denied(401), http(401).classifyCallFailure(now))
        assertEquals(CallFailure.Denied(403), http(403).classifyCallFailure(now))
        assertEquals(CallFailure.Transient(20_000L), http(503, "20").classifyCallFailure(now))
        assertEquals(CallFailure.Transient(null), http(500).classifyCallFailure(now))
        assertEquals(CallFailure.Transient(5_000L), http(429, "5").classifyCallFailure(now))
        for (code in listOf(400, 404, 409, 422)) assertEquals(CallFailure.Permanent, http(code).classifyCallFailure(now))
        assertEquals(CallFailure.Transient(null), UnknownHostException("dns").classifyCallFailure(now))
        assertEquals(CallFailure.Transient(null), RuntimeException("wrap", IOException("io")).classifyCallFailure(now))
        assertEquals(CallFailure.Permanent, SerializationException("bad").classifyCallFailure(now))
        assertEquals(CallFailure.Permanent, IllegalStateException("?").classifyCallFailure(now))
    }
}
