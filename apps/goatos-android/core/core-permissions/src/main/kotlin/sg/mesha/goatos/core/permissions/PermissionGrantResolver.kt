package sg.mesha.goatos.core.permissions

/**
 * Pure classification of [PermissionGrantState] — no Android calls, so it is
 * unit-testable off-device. The two Android-sourced inputs are read at the call site:
 *  - [isGranted]: `ContextCompat.checkSelfPermission(...) == PERMISSION_GRANTED`
 *    ([isPermissionGranted]).
 *  - [shouldShowRationale]: `ActivityCompat.shouldShowRequestPermissionRationale(...)`
 *    ([shouldShowRationale]).
 *
 * "Permanently denied" is only knowable AFTER at least one real request
 * ([hasRequestedOnce]): before any request, `shouldShowRationale` is also `false`, but
 * that means "never asked", not "blocked" — Android has no direct API to tell the two
 * apart up front, so the caller must track whether it has actually launched a request
 * this session.
 */
object PermissionGrantResolver {
    fun resolve(
        isGranted: Boolean,
        hasRequestedOnce: Boolean,
        shouldShowRationale: Boolean,
    ): PermissionGrantState = when {
        isGranted -> PermissionGrantState.GRANTED
        hasRequestedOnce && !shouldShowRationale -> PermissionGrantState.PERMANENTLY_DENIED
        else -> PermissionGrantState.DENIED
    }
}
