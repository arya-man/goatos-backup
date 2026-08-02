package sg.mesha.goatos.core.network

import retrofit2.HttpException
import java.io.IOException

/**
 * Maps network-layer errors to domain-level bootstrap errors:
 * - 401/403 HttpException → [BootstrapError.AuthSessionExpired]
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
        BootstrapError.AuthSessionExpired(statusCode = 403, cause = this)
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
