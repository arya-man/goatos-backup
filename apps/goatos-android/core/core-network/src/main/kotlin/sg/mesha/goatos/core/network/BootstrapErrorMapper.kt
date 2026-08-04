package sg.mesha.goatos.core.network

import retrofit2.HttpException
import java.io.IOException

/**
 * Maps network-layer errors to domain-level bootstrap errors:
 * - 401 HttpException → [BootstrapError.AuthSessionExpired]
 * - 403 HttpException → [BootstrapError.AccessNotProvisioned] (NOT a dead session: the token is
 *   valid, the person just has no profile/grant. Sign-out wipes the offline outbox, so routing
 *   403 to sign-out would destroy unsynced operator writes to fix nothing.)
 * - IOException (network failure) → [BootstrapError.ConnectivityFailure]
 * - Other HttpException (5xx, etc.) → [BootstrapError.ConnectivityFailure]
 *
 * Called from [DefaultBootstrapRepository] at the boundary between network and domain layers.
 */
fun Throwable.asBootstrapError(): BootstrapError = when {
    this is HttpException && code() == 401 -> {
        BootstrapError.AuthSessionExpired(statusCode = 401, cause = this)
    }
    this is HttpException && code() == 403 -> {
        BootstrapError.AccessNotProvisioned(statusCode = 403, cause = this)
    }
    this is IOException -> {
        BootstrapError.ConnectivityFailure(cause = this)
    }
    this is HttpException -> {
        // Other HTTP errors (5xx, etc.) are connectivity-like: retry with caution.
        BootstrapError.ConnectivityFailure(cause = this)
    }
    else -> {
        // Unexpected error type: treat as connectivity failure.
        BootstrapError.ConnectivityFailure(cause = this)
    }
}
