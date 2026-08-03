package sg.mesha.goatos.core.media

import android.net.Uri
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DataSpec
import androidx.media3.datasource.DefaultHttpDataSource
import kotlin.test.assertEquals
import kotlin.test.assertTrue
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.network.NetworkTelemetryEvent
import sg.mesha.goatos.core.network.NetworkTelemetryReporter

/**
 * W-22. Proof-video playback used to be INVISIBLE to telemetry: both players were built with
 * `ExoPlayer.Builder(context).build()`, so media3 fetched over its own `DefaultHttpDataSource`
 * and never touched the app's OkHttp client — the one place `TelemetryInterceptor` lives. A
 * verifier with 17 pending items would see a dead player on an expired signed URL while the
 * device produced zero logcat output.
 *
 * [defaultMedia3StackReportsNothing] pins the blind spot so it cannot silently return;
 * [okHttpBackedMediaLoadFailureReachesTelemetrySeam] is the fix's proof.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
@UnstableApi
class ProofMediaTelemetryTest {

    private lateinit var server: MockWebServer
    private val reported = mutableListOf<NetworkTelemetryEvent>()
    private val reporter = NetworkTelemetryReporter { reported += it }

    @Before
    fun setUp() {
        server = MockWebServer()
        server.start()
    }

    @After
    fun tearDown() {
        server.shutdown()
    }

    /** The signed-URL shape the verifier actually plays: opaque object path + signature query. */
    private fun signedProofUrl(): String =
        server.url(
            "/goatos-proofs/tenant-a/weighing/2026/08/03/a7f3c1d9e2b04c6f8a15.mp4" +
                "?X-Goog-Algorithm=GOOG4-RSA-SHA256&X-Goog-Expires=900" +
                "&X-Goog-Signature=deadbeefcafef00d1234567890abcdef",
        ).toString()

    private fun openExpectingFailure(open: () -> Unit) {
        runCatching(open).also {
            assertTrue(it.isFailure, "expected the media load to fail on the mocked HTTP error")
        }
    }

    @Test
    fun `okHttp-backed media load failure reaches telemetry seam`() {
        server.enqueue(MockResponse().setResponseCode(403))
        val client = ProofMediaHttp.proofMediaOkHttp(reporter = reporter)
        val source = ProofMediaHttp.dataSourceFactory(client).createDataSource()

        openExpectingFailure { source.open(DataSpec(Uri.parse(signedProofUrl()))) }

        assertEquals(1, reported.size, "a failed proof-video fetch must produce exactly one event")
        val event = reported.single()
        assertEquals("GET", event.method)
        assertEquals(403, event.statusCode)
        assertEquals(ProofMediaHttp.PROOF_MEDIA_ROUTE, event.route)
    }

    /**
     * The signature lives in the query string and the object key in the path; neither may appear
     * in anything the seam emits. Goat RFIDs/tags WOULD be loggable — a signed-URL signature is
     * not.
     */
    @Test
    fun `reported event carries no signed-URL signature or object key`() {
        server.enqueue(MockResponse().setResponseCode(500))
        val client = ProofMediaHttp.proofMediaOkHttp(reporter = reporter)
        val source = ProofMediaHttp.dataSourceFactory(client).createDataSource()

        openExpectingFailure { source.open(DataSpec(Uri.parse(signedProofUrl()))) }

        val rendered = reported.single().toString()
        assertTrue("X-Goog-Signature" !in rendered, "signature must never cross the seam: $rendered")
        assertTrue("deadbeef" !in rendered, "signature value must never cross the seam: $rendered")
        assertTrue("a7f3c1d9e2b04c6f8a15" !in rendered, "object key must not be reported: $rendered")
    }

    /** Route stays a CONSTANT, so the non-fatal throttle map cannot grow per video watched. */
    @Test
    fun `distinct videos collapse to one route label`() {
        repeat(2) { server.enqueue(MockResponse().setResponseCode(404)) }
        val client = ProofMediaHttp.proofMediaOkHttp(reporter = reporter)
        val factory = ProofMediaHttp.dataSourceFactory(client)

        openExpectingFailure {
            factory.createDataSource().open(DataSpec(Uri.parse(server.url("/proofs/one.mp4").toString())))
        }
        openExpectingFailure {
            factory.createDataSource().open(DataSpec(Uri.parse(server.url("/proofs/two.mp4").toString())))
        }

        assertEquals(2, reported.size)
        assertEquals(setOf(ProofMediaHttp.PROOF_MEDIA_ROUTE), reported.map { it.route }.toSet())
    }

    /**
     * The blind spot itself, pinned. media3's stock data source — what
     * `ExoPlayer.Builder(context).build()` uses — fetches the SAME failing URL and the seam stays
     * empty. Before the fix, this was the behaviour of BOTH proof players.
     */
    @Test
    fun `default media3 stack reports nothing`() {
        server.enqueue(MockResponse().setResponseCode(403))
        val source = DefaultHttpDataSource.Factory().createDataSource()

        openExpectingFailure { source.open(DataSpec(Uri.parse(signedProofUrl()))) }

        assertTrue(reported.isEmpty(), "media3's own HTTP stack cannot reach the OkHttp seam")
    }

    /** Playback must carry NO bearer: a pre-signed URL is often on a third-party storage host. */
    @Test
    fun `media client attaches no authorization header`() {
        server.enqueue(MockResponse().setResponseCode(403))
        val client = ProofMediaHttp.proofMediaOkHttp(reporter = reporter)

        openExpectingFailure {
            ProofMediaHttp.dataSourceFactory(client)
                .createDataSource()
                .open(DataSpec(Uri.parse(signedProofUrl())))
        }

        val recorded = server.takeRequest()
        assertEquals(null, recorded.getHeader("Authorization"))
        assertTrue(
            recorded.getHeader("traceparent")?.startsWith("00-") == true,
            "the telemetry interceptor must still stamp trace context",
        )
    }
}
