package sg.mesha.goatos.core.network

import retrofit2.HttpException

/**
 * The HTTP status behind an app-api failure, or null when the call failed for a reason that never
 * reached the server (no connectivity, DNS, socket).
 *
 * Kept here, alongside [isTerminalAppApiError], so `retrofit2.HttpException` stays inside
 * `core-network` -- `core-data` asks this question without taking a dependency on Retrofit's
 * exception type.
 */
fun Throwable.appApiStatusCode(): Int? = (this as? HttpException)?.code()
