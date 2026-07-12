package sg.mesha.goatos.core.analytics

/**
 * Crash + non-fatal reporting seam (Firebase Crashlytics today). Kept separate from
 * [AnalyticsPort] because crash signal has different rules: only non-PII identity keys
 * (role/park/flavor/tenant) and throwables/breadcrumbs ever cross this seam — never a name,
 * email, or other free-text user content.
 *
 * Stable public API grepped by the telemetry CI guardrail (see `docs/TELEMETRY.md`) — do not
 * rename without updating that doc and the guard.
 */
interface CrashReporter {
    /** Records a non-fatal exception, with optional coarse, non-PII context. */
    fun recordException(throwable: Throwable, message: String? = null)

    /** Breadcrumb log line (non-PII only) attached to the next crash/non-fatal report. */
    fun log(message: String)

    /**
     * Sets a non-PII custom key visible on crash reports (role/primary_park/flavor/tenant — see
     * [AnalyticsEvents.UserProps]). Never pass a name, email, or other identifying free text.
     */
    fun setCustomKey(key: String, value: String)
}

/** Fallback for tests/local/dev — never touches a vendor SDK, never throws. */
class NoopCrashReporter : CrashReporter {
    override fun recordException(throwable: Throwable, message: String?) {}
    override fun log(message: String) {}
    override fun setCustomKey(key: String, value: String) {}
}
