package sg.mesha.goatos.core.network

import retrofit2.HttpException
import java.io.IOException

/**
 * True only for failures that actually mean "this phone could not reach the server".
 *
 * `isOffline = result.isFailure` treated EVERY failure as offline, so a server that answered
 * perfectly well and refused the request -- a 403 from the verification module gate, a 409, a
 * validation 400 -- raised "You're offline. Records save on this phone and sync when you
 * reconnect" on a phone that was online. That sends the reader to check their signal for a
 * problem no amount of signal fixes, and hides the real refusal.
 *
 * A 5xx is deliberately included: the server is unusable from here and the honest advice is the
 * same as for a dead network. Only 4xx -- the server understood and declined -- is excluded.
 */
fun Throwable?.isConnectivityFailure(): Boolean = when (this) {
    null -> false
    is IOException -> true
    is HttpException -> code() >= 500
    else -> true
}
