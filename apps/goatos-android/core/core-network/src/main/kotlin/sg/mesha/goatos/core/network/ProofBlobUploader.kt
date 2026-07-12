package sg.mesha.goatos.core.network

import java.io.File
import java.io.FileNotFoundException
import java.io.IOException
import java.net.URI
import java.security.MessageDigest
import java.util.Locale
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.MediaType.Companion.toMediaTypeOrNull
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.asRequestBody

/** GCS's V4 signed PUT is `x-goog-if-generation-match: 0` (create-only) — a retried PUT of an
 *  object a PRIOR attempt already fully wrote answers 412. That is not a failure: the bytes are
 *  already durable, so the caller should proceed straight to the complete call. */
private const val HTTP_PRECONDITION_FAILED = 412

/**
 * Streams a captured proof video's bytes to the signed URL a `registerProof` call returned
 * (docs/mobile/proof-capture-sync-and-e2e.md §3 Room-first/outbox pipeline — this is the
 * "actual binary PUT" step `CaptureRepository`'s kdoc used to call out as not-yet-built).
 *
 * The file is read from this app's OWN private storage ([android.content.Context.filesDir] —
 * see `InAppVideoRecorder`) and streamed via a file-backed [okhttp3.RequestBody]
 * ([File.asRequestBody]) — OkHttp/Okio copies it to the socket in fixed-size chunks, the whole
 * file is never materialized in the JVM heap. A separate first pass computes a streaming
 * SHA-256 (same fixed-buffer discipline) so the complete call can prove what was actually sent.
 */
interface ProofBlobUploader {
    /**
     * PUTs [filePath]'s bytes to [uploadUrl] (absolute, e.g. a GCS signed URL, OR relative to
     * this app's own API base URL, e.g. the local-dev storage adapter's signed path) using
     * [uploadMethod] and [uploadHeaders] verbatim (as returned by `registerProof` — a V4 signed
     * URL's signature only covers the headers it named). The app's own bearer token is attached
     * ONLY when [uploadUrl] resolves to the app's own API host — an external signed-URL host
     * (GCS) never sees this app's Authorization header.
     */
    suspend fun putFile(
        uploadUrl: String,
        uploadMethod: String,
        uploadHeaders: Map<String, String>,
        mimeType: String,
        filePath: String,
    ): ProofBlobPutResult
}

sealed interface ProofBlobPutResult {
    /** The bytes were PUT (or were already durably stored from a prior attempt whose PUT
     *  succeeded but whose complete-call never landed) — safe to call the complete endpoint. */
    data class Uploaded(val contentHash: String, val sizeBytes: Long) : ProofBlobPutResult

    /** GCS 412: a PRIOR attempt already wrote this exact object (create-only signed URL). No
     *  bytes were sent this call, but the object is durable — complete with size/hash unknown
     *  (0/blank) so the backend derives them from the stored object itself. */
    data object AlreadyExists : ProofBlobPutResult
}

/** Thrown for any non-2xx/non-412 response or a locally-unreadable file. Always treated as
 *  RETRYABLE by [sg.mesha.goatos.core.data.sync.SyncEngine] (it is a plain [IOException], not an
 *  [retrofit2.HttpException]) — a retry re-runs `registerProof` first, which mints a FRESH signed
 *  URL, so even an expired-signature 403 self-heals on the next attempt. */
class ProofBlobUploadException(message: String) : IOException(message)

/**
 * [client] MUST be a BARE OkHttp client with no [BearerAuthInterceptor] — the target host for a
 * production upload is a third-party storage host (`storage.googleapis.com`), and this app's
 * bearer token must never be attached to a request leaving its own API host. Timeouts are wider
 * than the JSON-API client's: a multi-minute proof video over a slow field connection must not
 * be capped by the same 30s/60s budget used for small JSON bodies.
 */
class OkHttpProofBlobUploader(
    private val client: OkHttpClient,
    private val baseUrl: String,
    private val bearerTokenProvider: () -> String?,
) : ProofBlobUploader {

    override suspend fun putFile(
        uploadUrl: String,
        uploadMethod: String,
        uploadHeaders: Map<String, String>,
        mimeType: String,
        filePath: String,
    ): ProofBlobPutResult = withContext(Dispatchers.IO) {
        val file = resolveLocalFile(filePath)
        if (!file.isFile || !file.canRead()) {
            throw FileNotFoundException("Captured proof file is missing or unreadable: $filePath")
        }
        val contentHash = "sha256:" + streamingSha256(file)
        val resolvedUrl = resolveUploadUrl(uploadUrl)
        val mediaType = mimeType.ifBlank { DEFAULT_MEDIA_TYPE }.toMediaTypeOrNull()
        val body = file.asRequestBody(mediaType)

        val requestBuilder = Request.Builder().url(resolvedUrl)
        uploadHeaders.forEach { (name, value) -> requestBuilder.header(name, value) }
        if (uploadHeaders.keys.none { it.equals("Content-Type", ignoreCase = true) }) {
            requestBuilder.header("Content-Type", mediaType?.toString() ?: DEFAULT_MEDIA_TYPE)
        }
        if (isSameApiHost(resolvedUrl)) {
            bearerTokenProvider()?.takeIf { it.isNotBlank() }?.let {
                requestBuilder.header("Authorization", "Bearer $it")
            }
        }
        when (uploadMethod.uppercase(Locale.ROOT).ifBlank { "PUT" }) {
            "PUT" -> requestBuilder.put(body)
            else -> throw ProofBlobUploadException("Unsupported proof upload method: $uploadMethod")
        }

        client.newCall(requestBuilder.build()).execute().use { response ->
            when {
                response.isSuccessful ->
                    ProofBlobPutResult.Uploaded(contentHash = contentHash, sizeBytes = file.length())
                response.code == HTTP_PRECONDITION_FAILED -> ProofBlobPutResult.AlreadyExists
                else -> throw ProofBlobUploadException(
                    "Proof blob upload failed (HTTP ${response.code}): ${response.message}",
                )
            }
        }
    }

    private fun resolveUploadUrl(uploadUrl: String): HttpUrl {
        uploadUrl.toHttpUrlOrNull()?.let { return it }
        val base = baseUrl.toHttpUrlOrNull()
            ?: throw ProofBlobUploadException("Invalid app base URL: $baseUrl")
        return base.resolve(uploadUrl.removePrefix("/"))
            ?: throw ProofBlobUploadException("Invalid proof upload URL: $uploadUrl")
    }

    private fun isSameApiHost(url: HttpUrl): Boolean {
        val base = baseUrl.toHttpUrlOrNull() ?: return false
        return url.host.equals(base.host, ignoreCase = true)
    }

    private companion object {
        const val DEFAULT_MEDIA_TYPE = "application/octet-stream"
        const val STREAM_BUFFER_BYTES = 64 * 1024
    }

    private fun streamingSha256(file: File): String {
        val digest = MessageDigest.getInstance("SHA-256")
        file.inputStream().use { input ->
            val buffer = ByteArray(STREAM_BUFFER_BYTES)
            while (true) {
                val read = input.read(buffer)
                if (read == -1) break
                digest.update(buffer, 0, read)
            }
        }
        return digest.digest().joinToString(separator = "") { "%02x".format(it) }
    }
}

/** [InAppVideoRecorder] persists `localUri` as `File.toURI().toString()` (a `file:` URI), not a
 *  raw path — accept either form so a legacy/raw-path row still resolves correctly. */
private fun resolveLocalFile(filePath: String): File =
    if (filePath.startsWith("file:", ignoreCase = true)) {
        runCatching { File(URI(filePath)) }.getOrElse { File(filePath) }
    } else {
        File(filePath)
    }
