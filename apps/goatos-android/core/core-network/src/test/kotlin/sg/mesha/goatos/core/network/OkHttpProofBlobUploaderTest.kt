package sg.mesha.goatos.core.network

import java.io.File
import java.io.FileNotFoundException
import kotlinx.coroutines.test.runTest
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Before
import org.junit.Test

/**
 * Exercises [OkHttpProofBlobUploader] against a real HTTP server standing in for the signed
 * storage target (docs/mobile/proof-capture-sync-and-e2e.md §3 — the "actual binary PUT" step).
 * [MockWebServer] is the "fake object store" for this layer: it captures the exact bytes/headers
 * a PUT sent, so this proves the uploader streams the real file content (not a placeholder) to
 * the real signed URL — [SyncEngineTest] then covers the orchestration around it (register ->
 * upload -> complete -> Room SYNCED, plus the resumable-retry contract) with a scripted fake.
 */
class OkHttpProofBlobUploaderTest {

    private lateinit var server: MockWebServer
    private lateinit var tempFile: File

    @Before
    fun setUp() {
        server = MockWebServer()
        server.start()
        tempFile = File.createTempFile("proof-capture", ".mp4").apply {
            writeBytes(ByteArray(200_000) { (it % 256).toByte() }) // large enough to span several stream chunks
            deleteOnExit()
        }
    }

    @After
    fun tearDown() {
        server.shutdown()
        tempFile.delete()
    }

    private fun uploader(baseUrl: String = "https://api.goatos.example/") =
        OkHttpProofBlobUploader(OkHttpClient(), baseUrl, bearerTokenProvider = { "app-bearer-token" })

    @Test
    fun `streams the exact file bytes to the signed URL with the response headers attached`() = runTest {
        server.enqueue(MockResponse().setResponseCode(201))
        val signedUrl = server.url("/bucket/proofs/proof-1").toString()

        val result = uploader().putFile(
            uploadUrl = signedUrl,
            uploadMethod = "PUT",
            uploadHeaders = mapOf("x-goog-if-generation-match" to "0"),
            mimeType = "video/mp4",
            filePath = tempFile.absolutePath,
        )

        val request = server.takeRequest()
        assertEquals("PUT", request.method)
        assertEquals("/bucket/proofs/proof-1", request.path)
        assertEquals("0", request.getHeader("x-goog-if-generation-match"))
        assertArrayEquals(tempFile.readBytes(), request.body.readByteArray())

        // A GCS-style signed URL host differs from the app's own API host — the app's bearer
        // token must never leak to a third-party storage host.
        assertNull("app bearer token must not be sent to an external storage host", request.getHeader("Authorization"))

        require(result is ProofBlobPutResult.Uploaded)
        assertEquals(tempFile.length(), result.sizeBytes)
        assertTrue(result.contentHash.startsWith("sha256:"))
    }

    @Test
    fun `attaches the app bearer token only for a same-host (local-dev storage) upload URL`() = runTest {
        server.enqueue(MockResponse().setResponseCode(200))
        val baseUrl = server.url("/").toString()

        uploader(baseUrl = baseUrl).putFile(
            uploadUrl = "/app/proofs/proof-1/upload?expires=1&sig=abc",
            uploadMethod = "PUT",
            uploadHeaders = mapOf("Content-Type" to "video/mp4"),
            mimeType = "video/mp4",
            filePath = tempFile.absolutePath,
        )

        val request = server.takeRequest()
        assertEquals("Bearer app-bearer-token", request.getHeader("Authorization"))
    }

    @Test
    fun `a 412 (object already written by a prior attempt) is treated as AlreadyExists, not a failure`() = runTest {
        server.enqueue(MockResponse().setResponseCode(412))

        val result = uploader().putFile(
            uploadUrl = server.url("/bucket/proofs/proof-1").toString(),
            uploadMethod = "PUT",
            uploadHeaders = emptyMap(),
            mimeType = "video/mp4",
            filePath = tempFile.absolutePath,
        )

        assertEquals(ProofBlobPutResult.AlreadyExists, result)
    }

    @Test
    fun `a real server error is a retryable ProofBlobUploadException`() = runTest {
        server.enqueue(MockResponse().setResponseCode(503))

        try {
            uploader().putFile(
                uploadUrl = server.url("/bucket/proofs/proof-1").toString(),
                uploadMethod = "PUT",
                uploadHeaders = emptyMap(),
                mimeType = "video/mp4",
                filePath = tempFile.absolutePath,
            )
            fail("expected ProofBlobUploadException")
        } catch (e: ProofBlobUploadException) {
            assertTrue(e.message.orEmpty().contains("503"))
        }
    }

    @Test
    fun `a missing capture file fails fast without touching the network`() = runTest {
        try {
            uploader().putFile(
                uploadUrl = server.url("/bucket/proofs/proof-1").toString(),
                uploadMethod = "PUT",
                uploadHeaders = emptyMap(),
                mimeType = "video/mp4",
                filePath = "${tempFile.absolutePath}.does-not-exist",
            )
            fail("expected FileNotFoundException")
        } catch (e: FileNotFoundException) {
            // expected
        }
        assertEquals(0, server.requestCount)
    }
}
