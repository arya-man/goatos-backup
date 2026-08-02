package sg.mesha.goatos.boot

/**
 * Bootstrap error categories that map to localized UI messages and actions.
 */
enum class BootstrapErrorType {
    /**
     * Session ended (401 / invalid or expired bearer token).
     * Action: "Sign in again" → sign out and return to login screen.
     */
    AUTH_SESSION_EXPIRED,

    /**
     * Network connectivity or server error (no network, timeout, DNS, 5xx).
     * Action: "Retry" → retry loading the bootstrap.
     */
    CONNECTIVITY_FAILURE,
}
