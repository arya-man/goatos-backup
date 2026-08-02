package sg.mesha.goatos.core.network

/**
 * Sealed hierarchy for bootstrap errors that require different UX responses:
 * - [AuthSessionExpired] (401/invalid_bearer_token) → sign in again
 * - [ConnectivityFailure] (no network, timeout, DNS, 5xx) → check connection and retry
 *
 * Domain-level exceptions that are mapped from network-layer errors. Lives in core-network because that is where the transport error types (Retrofit
 * HttpException, IOException) are visible; core-data depends on core-network, so the
 * repository layer can still surface these as domain errors.
 */
sealed class BootstrapError(message: String, cause: Throwable? = null) : Exception(message, cause) {
    /**
     * Session ended (401 / invalid or expired bearer token). The app must take the
     * operator to sign-in, not show a retryable network error.
     */
    class AuthSessionExpired(
        val statusCode: Int,
        message: String = "Session ended. Please sign in again.",
        cause: Throwable? = null,
    ) : BootstrapError(message, cause)

    /**
     * Network connectivity or server (5xx) failure. The app should show a retryable
     * "check your connection" message.
     */
    class ConnectivityFailure(
        message: String = "Couldn't reach the server. Check your connection and try again.",
        cause: Throwable? = null,
    ) : BootstrapError(message, cause)
}
