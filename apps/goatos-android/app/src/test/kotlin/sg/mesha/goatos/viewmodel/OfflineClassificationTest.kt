package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import retrofit2.HttpException
import retrofit2.Response
import sg.mesha.goatos.core.network.isConnectivityFailure
import java.io.IOException

/**
 * Verify that ViewModels correctly classify failures as offline or not.
 *
 * Offline (isOffline=true): network unreachable (IOException) or server unusable (5xx)
 * Not offline (isOffline=false): client error (4xx), null, other exceptions
 *
 * Reference: ConnectivityFailure.kt isConnectivityFailure()
 */
class OfflineClassificationTest {

    @Test
    fun `IOException is classified as connectivity failure (offline)`() {
        val error = IOException("Network unreachable")
        assertTrue("IOException should be offline", error.isConnectivityFailure())
    }

    @Test
    fun `5xx HttpException is classified as connectivity failure (offline)`() {
        val response = Response.error<Unit>(500, okhttp3.ResponseBody.create(null, ""))
        val error = HttpException(response)
        assertTrue("5xx should be offline", error.isConnectivityFailure())
    }

    @Test
    fun `503 HttpException is classified as connectivity failure (offline)`() {
        val response = Response.error<Unit>(503, okhttp3.ResponseBody.create(null, ""))
        val error = HttpException(response)
        assertTrue("503 should be offline", error.isConnectivityFailure())
    }

    @Test
    fun `400 HttpException is NOT classified as connectivity failure (not offline)`() {
        val response = Response.error<Unit>(400, okhttp3.ResponseBody.create(null, ""))
        val error = HttpException(response)
        assertFalse("4xx should not be offline", error.isConnectivityFailure())
    }

    @Test
    fun `403 HttpException is NOT classified as connectivity failure (not offline)`() {
        val response = Response.error<Unit>(403, okhttp3.ResponseBody.create(null, ""))
        val error = HttpException(response)
        assertFalse("403 should not be offline", error.isConnectivityFailure())
    }

    @Test
    fun `404 HttpException is NOT classified as connectivity failure (not offline)`() {
        val response = Response.error<Unit>(404, okhttp3.ResponseBody.create(null, ""))
        val error = HttpException(response)
        assertFalse("404 should not be offline", error.isConnectivityFailure())
    }

    @Test
    fun `409 HttpException is NOT classified as connectivity failure (not offline)`() {
        val response = Response.error<Unit>(409, okhttp3.ResponseBody.create(null, ""))
        val error = HttpException(response)
        assertFalse("409 should not be offline", error.isConnectivityFailure())
    }

    @Test
    fun `null exception is NOT classified as connectivity failure`() {
        val error: Throwable? = null
        assertFalse("null should not be offline", error.isConnectivityFailure())
    }

    @Test
    fun `generic exception is NOT classified as connectivity failure`() {
        val error = IllegalArgumentException("some error")
        assertFalse("generic exception should not be offline", error.isConnectivityFailure())
    }

    @Test
    fun `NullPointerException is NOT classified as connectivity failure`() {
        val error = NullPointerException("NPE")
        assertFalse("NPE should not be offline", error.isConnectivityFailure())
    }

    @Test
    fun `ClassCastException is NOT classified as connectivity failure`() {
        val error = ClassCastException("cast error")
        assertFalse("ClassCastException should not be offline", error.isConnectivityFailure())
    }
}
