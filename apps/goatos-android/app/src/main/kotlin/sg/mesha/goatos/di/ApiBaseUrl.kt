package sg.mesha.goatos.di

import javax.inject.Qualifier

/**
 * The app's API base URL (`BuildConfig.API_BASE_URL` for the active flavor). Injected rather than
 * read from `BuildConfig` at each use site so connectivity decisions that MUST agree — the
 * drain-time `ConnectivityGate` and the enqueue-time WorkManager `Constraints` — are driven by one
 * binding and are testable with an arbitrary base URL.
 */
@Qualifier
@Retention(AnnotationRetention.BINARY)
annotation class ApiBaseUrl
