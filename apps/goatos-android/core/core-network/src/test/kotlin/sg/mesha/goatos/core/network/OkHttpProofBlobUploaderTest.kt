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
            writeBytes(ByteArray(400_000) { (it % 256).toByte() }) // large enough to span several stream chunks
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
            uploadProtocol = "simple_put",
            chunkSizeBytes = null,
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
            uploadProtocol = "simple_put",
            chunkSizeBytes = null,
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
            uploadProtocol = "simple_put",
            chunkSizeBytes = null,
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
                uploadProtocol = "simple_put",
                chunkSizeBytes = null,
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
                uploadProtocol = "simple_put",
                chunkSizeBytes = null,
                mimeType = "video/mp4",
                filePath = "${tempFile.absolutePath}.does-not-exist",
            )
            fail("expected FileNotFoundException")
        } catch (e: FileNotFoundException) {
            // expected
        }
        assertEquals(0, server.requestCount)
    }

    @Test
    fun `resumable protocol starts a session and uploads content-range chunks`() = runTest {
        val sessionUrl = server.url("/upload/session-1").toString()
        server.enqueue(MockResponse().setResponseCode(201).setHeader("Location", sessionUrl))
        server.enqueue(MockResponse().setResponseCode(308).setHeader("Range", "bytes=0-262143"))
        server.enqueue(MockResponse().setResponseCode(200))

        val result = uploader().putFile(
            uploadUrl = server.url("/bucket/proofs/proof-1?X-Goog-Signature=abc").toString(),
            uploadMethod = "POST",
            uploadHeaders = mapOf(
                "Content-Type" to "video/mp4",
                "x-goog-resumable" to "start",
                "x-goog-if-generation-match" to "0",
            ),
            uploadProtocol = "gcs_resumable_v1",
            chunkSizeBytes = 262_144,
            mimeType = "video/mp4",
            filePath = tempFile.absolutePath,
        )

        val init = server.takeRequest()
        assertEquals("POST", init.method)
        assertEquals("/bucket/proofs/proof-1?X-Goog-Signature=abc", init.path)
        assertEquals("start", init.getHeader("x-goog-resumable"))
        assertEquals("0", init.getHeader("x-goog-if-generation-match"))
        assertEquals(0L, init.bodySize)

        val firstChunk = server.takeRequest()
        assertEquals("PUT", firstChunk.method)
        assertEquals("/upload/session-1", firstChunk.path)
        assertEquals("bytes 0-262143/${tempFile.length()}", firstChunk.getHeader("Content-Range"))
        assertArrayEquals(tempFile.readBytes().copyOfRange(0, 262_144), firstChunk.body.readByteArray())

        val secondChunk = server.takeRequest()
        assertEquals("PUT", secondChunk.method)
        assertEquals("bytes 262144-399999/${tempFile.length()}", secondChunk.getHeader("Content-Range"))
        assertArrayEquals(tempFile.readBytes().copyOfRange(262_144, 400_000), secondChunk.body.readByteArray())

        require(result is ProofBlobPutResult.Uploaded)
        assertEquals(tempFile.length(), result.sizeBytes)
    }

    @Test
    fun `resumable protocol resumes from the server acknowledged Range`() = runTest {
        server.enqueue(MockResponse().setResponseCode(201).setHeader("Location", server.url("/upload/session-range").toString()))
        server.enqueue(MockResponse().setResponseCode(308).setHeader("Range", "bytes=0-131071"))
        server.enqueue(MockResponse().setResponseCode(308).setHeader("Range", "bytes=0-393215"))
        server.enqueue(MockResponse().setResponseCode(200))

        uploader().putFile(
            uploadUrl = server.url("/bucket/proofs/proof-range").toString(),
            uploadMethod = "POST",
            uploadHeaders = mapOf("x-goog-resumable" to "start"),
            uploadProtocol = "gcs_resumable_v1",
            chunkSizeBytes = 262_144,
            mimeType = "video/mp4",
            filePath = tempFile.absolutePath,
        )

        server.takeRequest() // session initiation
        val firstChunk = server.takeRequest()
        assertEquals("bytes 0-262143/${tempFile.length()}", firstChunk.getHeader("Content-Range"))

        val resumedChunk = server.takeRequest()
        assertEquals("bytes 131072-393215/${tempFile.length()}", resumedChunk.getHeader("Content-Range"))
        assertArrayEquals(tempFile.readBytes().copyOfRange(131_072, 393_216), resumedChunk.body.readByteArray())

        val finalChunk = server.takeRequest()
        assertEquals("bytes 393216-399999/${tempFile.length()}", finalChunk.getHeader("Content-Range"))
        assertArrayEquals(tempFile.readBytes().copyOfRange(393_216, 400_000), finalChunk.body.readByteArray())
    }

    @Test
    fun `local resumable protocol resolves relative session URLs and keeps app auth on API host`() = runTest {
        val baseUrl = server.url("/").toString()
        server.enqueue(MockResponse().setResponseCode(201).setHeader("Location", "/app/proofs/proof-1/resumable/session"))
        server.enqueue(MockResponse().setResponseCode(200))

        uploader(baseUrl = baseUrl).putFile(
            uploadUrl = "/app/proofs/proof-1/upload?expires=1&sig=abc",
            uploadMethod = "POST",
            uploadHeaders = emptyMap(),
            uploadProtocol = "gcs_resumable_v1",
            chunkSizeBytes = 1_000_000,
            mimeType = "video/mp4",
            filePath = tempFile.absolutePath,
        )

        val init = server.takeRequest()
        assertEquals("POST", init.method)
        assertEquals("Bearer app-bearer-token", init.getHeader("Authorization"))
        assertEquals("start", init.getHeader("x-goog-resumable"))

        val chunk = server.takeRequest()
        assertEquals("PUT", chunk.method)
        assertEquals("/app/proofs/proof-1/resumable/session", chunk.path)
        assertEquals("bytes 0-399999/${tempFile.length()}", chunk.getHeader("Content-Range"))
    }
}
