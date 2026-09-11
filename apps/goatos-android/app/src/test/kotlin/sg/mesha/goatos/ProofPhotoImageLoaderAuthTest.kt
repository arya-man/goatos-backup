package sg.mesha.goatos

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import coil.ImageLoader
import coil.request.ImageRequest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import kotlinx.coroutines.runBlocking
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.media.ProofMediaHttp
import sg.mesha.goatos.core.network.TENANT_CONTEXT_HEADER

/**
 * The verifier's feed-weight PHOTO rendered as an empty card while the two videos beside it
 * played (maintainer report 2026-09-11). A proof's `download_url` is `/app/proofs/{id}/download`
 * on the API, which requires the bearer. ExoPlayer streams it over the proof-media OkHttp client,
 * whose interceptor attaches the bearer on same-origin requests; `AsyncImage` resolved Coil's
 * DEFAULT loader -- a bare client -- and the photo request 401'd (STG server log: 338-byte
 * request -> 401 for the photo, 2030-byte request -> 307 for each video).
 *
 * The fix is one seam: [GoatOsApplication.newImageLoader] hands Coil that same client through
 * [proofImageLoader]. This builds that exact loader against a MockWebServer and reads the headers
 * the server received.
 * [defaultCoilLoaderCarriesNoBearer] pins the defect so it cannot silently return.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class ProofPhotoImageLoaderAuthTest {

    private lateinit var server: MockWebServer
    private val context: Context get() = ApplicationProvider.getApplicationContext()

    @Before
    fun setUp() {
        server = MockWebServer()
        server.start()
    }

    @After
    fun tearDown() {
        server.shutdown()
    }

    private fun proofMediaClientFor(apiOrigin: String) =
        ProofMediaHttp.proofMediaOkHttp(
            telemetryEnabled = false,
            apiBaseUrl = apiOrigin,
            tokenProvider = { "firebase-id-token" },
            tenantIdProvider = { "00000000-0000-4000-8000-000000000001" },
        )

    private fun photoDownloadUrl(): String =
        server.url("/app/proofs/313d8b88-65c9-4f3f-93b1-1e020b595374/download").toString()

    private fun fetchThrough(loader: ImageLoader) {
        server.enqueue(MockResponse().setResponseCode(401).setBody("""{"code":"missing_bearer_token"}"""))
        runBlocking {
            loader.execute(
                ImageRequest.Builder(context)
                    .data(photoDownloadUrl())
                    .memoryCachePolicy(coil.request.CachePolicy.DISABLED)
                    .diskCachePolicy(coil.request.CachePolicy.DISABLED)
                    .build(),
            )
        }
    }

    @Test
    fun appImageLoaderSendsTheBearerAndTenantToTheProofDownloadRoute() {
        // The client MediaModule binds: same-origin auth interceptor, no token for other hosts.
        val proofMediaClient = proofMediaClientFor(server.url("/").toString())

        fetchThrough(proofImageLoader(context, proofMediaClient))

        val received = server.takeRequest()
        assertEquals("/app/proofs/313d8b88-65c9-4f3f-93b1-1e020b595374/download", received.path)
        assertEquals("Bearer firebase-id-token", received.getHeader("Authorization"))
        assertEquals("00000000-0000-4000-8000-000000000001", received.getHeader(TENANT_CONTEXT_HEADER))
    }

    /** The defect: Coil's own loader knows nothing about the app's session, so the API refuses it. */
    @Test
    fun defaultCoilLoaderCarriesNoBearer() {
        fetchThrough(ImageLoader.Builder(context).build())

        val received = server.takeRequest()
        assertNull(received.getHeader("Authorization"))
        assertNull(received.getHeader(TENANT_CONTEXT_HEADER))
    }
}
