package sg.mesha.goatos.core.permissions

/**
 * Pure classification of [PermissionGrantState] — no Android calls, so it is
 * unit-testable off-device. The two Android-sourced inputs are read at the call site:
 *  - [isGranted]: `ContextCompat.checkSelfPermission(...) == PERMISSION_GRANTED`
 *    ([isPermissionGranted]).
 *  - [shouldShowRationale]: `ActivityCompat.shouldShowRequestPermissionRationale(...)`
 *    ([shouldShowRationale]).
 *
 * "Permanently denied" is only knowable AFTER a real request ([requestCount]): before any
 * request, `shouldShowRationale` is also `false`, but that means "never asked", not
 * "blocked" — Android has no direct API to tell the two apart up front, so the caller must
 * count how many times it has actually launched a request.
 *
 * `shouldShowRationale` alone is NOT trustworthy after a single denial. The flag an app can
 * read is a heuristic, not the OS's own "don't ask again" record (that lives in the package
 * manager's USER_FIXED flag, which apps cannot query). On stock Android the sequence is:
 * rationale=false before the first ask, true after one ordinary denial, false again after
 * "don't ask again". On Xiaomi/MIUI and HyperOS the flag commonly reports false straight
 * after ONE ordinary denial, and the OEM may auto-deny a request outright — so
 * `requestCount == 1 && !shouldShowRationale` is indistinguishable from a plain first "no".
 * Requiring [MIN_REQUESTS_BEFORE_BLOCKED] completed requests before believing "blocked" is
 * the portable reading of that flag: the worst case on a misbehaving OEM is one extra
 * prompt, whereas the old one-request rule dead-ended the operator on an open-settings
 * screen after a single tap of Deny.
 */
object PermissionGrantResolver {

    /**
     * Completed requests required before `!shouldShowRationale` may be believed as
     * "blocked". Two, because one denial must always be retryable by simply asking again.
     */
    const val MIN_REQUESTS_BEFORE_BLOCKED: Int = 2

    fun resolve(
        isGranted: Boolean,
        requestCount: Int,
        shouldShowRationale: Boolean,
    ): PermissionGrantState = when {
        isGranted -> PermissionGrantState.GRANTED
        requestCount >= MIN_REQUESTS_BEFORE_BLOCKED && !shouldShowRationale ->
            PermissionGrantState.PERMANENTLY_DENIED
        else -> PermissionGrantState.DENIED
    }
}
