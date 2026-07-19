package sg.mesha.goatos.core.network

import okhttp3.Interceptor
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.internal.http.RealResponseBody
import org.junit.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

/**
 * Unit tests for [TelemetryInterceptor] — tests the pure cardinality/templating logic and
 * W3C traceparent format validation. Network calls are mocked.
 */
class TelemetryInterceptorTest {

    @Test
    fun `newTraceparent generates valid W3C format`() {
        val traceparent = TelemetryInterceptor.newTraceparent()

        // Format: 00-<32hex>-<16hex>-<2hex>
        val pattern = Regex("^00-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$")
        assertTrue(
            pattern.matches(traceparent),
            "traceparent must match W3C format, got: $traceparent"
        )
    }

    @Test
    fun `newTraceparent generates distinct values`() {
        val tp1 = TelemetryInterceptor.newTraceparent()
        val tp2 = TelemetryInterceptor.newTraceparent()
        assertTrue(tp1 != tp2, "traceparent values should be distinct")
    }

    @Test
    fun `routeTemplate replaces UUID path segments with id`() {
        val uuid = "550e8400-e29b-41d4-a716-446655440000"
        val path = "/app/tasks/$uuid/details"
        assertEquals(
            "/app/tasks/{id}/details",
            TelemetryInterceptor.routeTemplate(path),
            "UUID segments should be replaced with {id}"
        )
    }

    @Test
    fun `routeTemplate replaces numeric path segments with id`() {
        val path = "/app/sheds/42/animals/99/records"
        assertEquals(
            "/app/sheds/{id}/animals/{id}/records",
            TelemetryInterceptor.routeTemplate(path),
            "Numeric segments should be replaced with {id}"
        )
    }

    @Test
    fun `routeTemplate preserves non-id segments`() {
        val path = "/api/v1/bootstrap"
        assertEquals(
            "/api/v1/bootstrap",
            TelemetryInterceptor.routeTemplate(path),
            "Non-id segments should be preserved"
        )
    }

    @Test
    fun `routeTemplate handles mixed UUID and numeric segments`() {
        val path = "/drives/550e8400-e29b-41d4-a716-446655440000/tasks/123"
        assertEquals(
            "/drives/{id}/tasks/{id}",
            TelemetryInterceptor.routeTemplate(path),
            "Both UUID and numeric segments should be replaced"
        )
    }

    @Test
    fun `routeTemplate handles empty path segments`() {
        val path = "/api//v1///bootstrap"
        assertEquals(
            "/api//v1///bootstrap",
            TelemetryInterceptor.routeTemplate(path),
            "Empty segments should be preserved"
        )
    }

    @Test
    fun `routeTemplate handles trailing slash`() {
        val path = "/api/v1/bootstrap/"
        assertEquals(
            "/api/v1/bootstrap/",
            TelemetryInterceptor.routeTemplate(path),
            "Trailing slash should be preserved"
        )
    }

    @Test
    fun `routeTemplate handles root path`() {
        val path = "/"
        assertEquals("/", TelemetryInterceptor.routeTemplate(path))
    }

    @Test
    fun `intercept forwards traceparent header when disabled`() {
        val events = mutableListOf<NetworkTelemetryEvent>()
        val reporter = object : NetworkTelemetryReporter {
            override fun onNetworkCall(event: NetworkTelemetryEvent) {
                events.add(event)
            }
        }

        val interceptor = TelemetryInterceptor(enabled = false, reporter = reporter)
        val request = Request.Builder()
            .url("http://example.com/api/tasks")
            .get()
            .build()

        // Simulate a mock response via a chain
        val mockChain = MockInterceptorChain(request, Response.Builder()
            .request(request)
            .protocol(okhttp3.Protocol.HTTP_1_1)
            .code(200)
            .message("OK")
            .body(RealResponseBody("text/plain", 0, okio.Buffer()))
            .build()
        )

        val response = interceptor.intercept(mockChain)
        assertEquals(200, response.code)
        assertEquals(0, events.size, "Reporter should not be called when interceptor is disabled")
    }

    @Test
    fun `intercept reports event when enabled`() {
        val events = mutableListOf<NetworkTelemetryEvent>()
        val reporter = object : NetworkTelemetryReporter {
            override fun onNetworkCall(event: NetworkTelemetryEvent) {
                events.add(event)
            }
        }

        val interceptor = TelemetryInterceptor(enabled = true, reporter = reporter)
        val request = Request.Builder()
            .url("http://example.com/api/tasks/550e8400-e29b-41d4-a716-446655440000")
            .get()
            .build()

        val mockChain = MockInterceptorChain(request, Response.Builder()
            .request(request)
            .protocol(okhttp3.Protocol.HTTP_1_1)
            .code(200)
            .message("OK")
            .body(RealResponseBody("text/plain", 0, okio.Buffer()))
            .build()
        )

        val response = interceptor.intercept(mockChain)
        assertEquals(200, response.code)
        assertEquals(1, events.size, "Reporter should be called exactly once")

        val event = events[0]
        assertEquals("GET", event.method)
        assertEquals("/api/tasks/{id}", event.route, "Route should have UUID replaced")
        assertEquals(200, event.statusCode)
    }

    /**
     * Mock interceptor chain for testing — returns a pre-built response without network I/O.
     */
    private class MockInterceptorChain(
        private val request: Request,
        private val response: Response,
    ) : Interceptor.Chain {
        override fun request(): Request = request
        override fun proceed(request: Request): Response = response
        override fun connection(): okhttp3.Connection? = null
        override fun call(): okhttp3.Call = OkHttpClient().newCall(request)
        override fun connectTimeoutMillis(): Int = 0
        override fun withConnectTimeout(timeout: Int, unit: java.util.concurrent.TimeUnit): Interceptor.Chain = this
        override fun readTimeoutMillis(): Int = 0
        override fun withReadTimeout(timeout: Int, unit: java.util.concurrent.TimeUnit): Interceptor.Chain = this
        override fun writeTimeoutMillis(): Int = 0
        override fun withWriteTimeout(timeout: Int, unit: java.util.concurrent.TimeUnit): Interceptor.Chain = this
    }
}
