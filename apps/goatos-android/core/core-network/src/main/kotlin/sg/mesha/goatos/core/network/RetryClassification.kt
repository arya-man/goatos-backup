package sg.mesha.goatos.core.network

import retrofit2.HttpException

/**
 * Retryable-vs-terminal classification for app-api failures, kept in `core-network` so the
 * `retrofit2.HttpException` type never leaks into `core-data`'s sync engine (it only calls
 * [isTerminalAppApiError]).
 *
 * A definitive 4xx client error will NOT change by re-sending the SAME payload, so the outbox
 * should terminalize it immediately instead of burning the full exponential-backoff budget
 * (~8 attempts, up to ~15+ min) on a request that can never succeed as-is.
 *
 * Exceptions kept RETRYABLE even though they are 4xx:
 *  - 401 / 403 — a token may be refreshed / a grant may land; a later attempt can succeed.
 *  - 408 Request Timeout, 425 Too Early, 429 Too Many Requests — explicitly transient.
 * Everything else in 400..499 (400/404/409/422/…) is terminal.
 */
fun Throwable.isTerminalAppApiError(): Boolean {
    val status = (this as? HttpException)?.code() ?: return false
    if (status !in 400..499) return false
    return status !in RETRYABLE_CLIENT_STATUSES
}

private val RETRYABLE_CLIENT_STATUSES = setOf(401, 403, 408, 425, 429)
