package sg.mesha.goatos.core.network

import retrofit2.HttpException
import java.io.IOException
import java.time.Duration
import java.time.ZonedDateTime
import java.time.format.DateTimeFormatter

/**
 * What a failed best-effort call should do next. Retrofit stays behind this module, so callers
 * outside core-network classify here instead of inspecting [HttpException] themselves.
 */
sealed interface CallFailure {
    /** Network/IO failure, HTTP 5xx or 429: try again, no sooner than [retryAfterMs] if given. */
    data class Transient(val retryAfterMs: Long?) : CallFailure

    /** HTTP 401/403: the server's definitive answer about this principal. */
    data class Denied(val statusCode: Int) : CallFailure

    /** Any other 4xx, a serialization error, or anything unexpected: retrying cannot help. */
    data object Permanent : CallFailure
}

fun Throwable.classifyCallFailure(nowEpochMs: Long = System.currentTimeMillis()): CallFailure {
    val chain = generateSequence(this) { it.cause }.toList()
    val http = chain.filterIsInstance<HttpException>().firstOrNull()
    if (http != null) {
        val code = http.code()
        return when {
            code == 401 || code == 403 -> CallFailure.Denied(code)
            code == 429 || code >= 500 ->
                CallFailure.Transient(parseRetryAfterMs(http.response()?.headers()?.get("Retry-After"), nowEpochMs))
            else -> CallFailure.Permanent
        }
    }
    return if (chain.any { it is IOException }) CallFailure.Transient(null) else CallFailure.Permanent
}

/** The longest a server-requested Retry-After is honoured; longer asks are clamped to this. */
const val MAX_RETRY_AFTER_MS: Long = 120_000L

/**
 * Retry-After is either delta-seconds or an HTTP date (RFC 9110 §10.2.3). Clamped to
 * [0, MAX_RETRY_AFTER_MS] BEFORE converting seconds to millis so a huge value cannot overflow.
 * Returns null when absent or unparseable (the caller then uses its own backoff).
 */
internal fun parseRetryAfterMs(value: String?, nowEpochMs: Long): Long? {
    val raw = value?.trim()?.takeIf { it.isNotEmpty() } ?: return null
    raw.toLongOrNull()?.let { seconds -> return seconds.coerceIn(0L, MAX_RETRY_AFTER_MS / 1_000) * 1_000 }
    // exception:exempt an unparseable Retry-After header is not an error: it means "no server hint",
    // which null encodes, and the caller falls back to its own backoff schedule.
    return runCatching {
        val at = ZonedDateTime.parse(raw, DateTimeFormatter.RFC_1123_DATE_TIME).toInstant().toEpochMilli()
        Duration.ofMillis(at - nowEpochMs).toMillis().coerceIn(0L, MAX_RETRY_AFTER_MS)
    }.getOrNull()
}
