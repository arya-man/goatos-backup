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

    /**
     * Token is valid but access is not provisioned (403: no roster profile / no grant).
     * Primary action: "Retry" -- never an automatic sign-out, which would wipe unsynced offline
     * writes. Secondary action: "Sign in with another account", the operator's explicit choice for
     * a wrong account; it confirms first, showing how many unsynced items would be lost, whenever
     * anything is still waiting to upload.
     */
    ACCESS_NOT_PROVISIONED,
}
