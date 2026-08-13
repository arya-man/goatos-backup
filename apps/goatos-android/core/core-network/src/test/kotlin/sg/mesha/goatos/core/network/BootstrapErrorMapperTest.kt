package sg.mesha.goatos.core.network

import okhttp3.MediaType.Companion.toMediaType
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Test
import retrofit2.HttpException
import retrofit2.Response
import java.io.IOException
import kotlin.test.assertIs
import kotlin.test.assertEquals

/**
 * Tests that network-layer HTTP errors are correctly mapped to domain-level bootstrap errors.
 */
class BootstrapErrorMapperTest {

    @Test
    fun `HTTP 401 maps to AuthSessionExpired`() {
        val httpError = httpError(401)
        val error = httpError.asBootstrapError()

        assertIs<BootstrapError.AuthSessionExpired>(error)
        assertEquals(401, error.statusCode)
    }

    @Test
    fun `HTTP 403 maps to AccessNotProvisioned, never a destructive sign-out`() {
        val httpError = httpError(403)
        val error = httpError.asBootstrapError()

        // A 403 means the token is VALID but access is not provisioned. It must NOT map to
        // AuthSessionExpired: that screen's only action is sign-out, which wipes the offline
        // outbox and would destroy an operator's unsynced scans to fix something sign-out
        // cannot fix.
        assertIs<BootstrapError.AccessNotProvisioned>(error)
        assertEquals(403, error.statusCode)
    }

    @Test
    fun `HTTP 5xx maps to ConnectivityFailure`() {
        listOf(500, 502, 503, 504).forEach { code ->
            val httpError = httpError(code)
            val error = httpError.asBootstrapError()

            assertIs<BootstrapError.ConnectivityFailure>(error)
        }
    }

    @Test
    fun `HTTP 404 maps to ConnectivityFailure`() {
        val httpError = httpError(404)
        val error = httpError.asBootstrapError()

        assertIs<BootstrapError.ConnectivityFailure>(error)
    }

    @Test
    fun `IOException maps to ConnectivityFailure`() {
        val ioError = IOException("Network unreachable")
        val error = ioError.asBootstrapError()

        assertIs<BootstrapError.ConnectivityFailure>(error)
    }

    @Test
    fun `Unknown exception maps to ConnectivityFailure`() {
        val unexpectedError = RuntimeException("Unexpected error")
        val error = unexpectedError.asBootstrapError()

        assertIs<BootstrapError.ConnectivityFailure>(error)
    }

    private fun httpError(code: Int): HttpException =
        HttpException(Response.error<Any>(code, "".toResponseBody("text/plain".toMediaType())))
}
