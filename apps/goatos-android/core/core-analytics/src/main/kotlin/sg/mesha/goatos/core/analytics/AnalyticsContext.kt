package sg.mesha.goatos.core.analytics

/**
 * Process-scoped analytics identity applied to every event's standard params by the egress impl
 * (the [NoopAnalytics] ignores it). [flavor] is build-fixed; [role] and [parkScope] are populated
 * once bootstrap resolves the operator profile, and re-populated on every re-bootstrap.
 *
 * Written by the bootstrap path on the main thread and read by the (future) egress impl on its own
 * thread, so the mutable fields are `@Volatile` for safe publication. Held as a DI singleton.
 */
class AnalyticsContext(val flavor: String) {
    @Volatile
    var appVersionName: String? = null

    @Volatile
    var appVersionCode: String? = null

    @Volatile
    var role: String? = null

    @Volatile
    var parkScope: String? = null

    /** Stable per-install device id, stamped onto every event by [FirebaseAnalyticsAdapter.track]. */
    @Volatile
    var deviceId: String? = null

    /** Process/session marker used to correlate launch, bootstrap, scan, proof, and sync events. */
    @Volatile
    var journeyId: String? = null

    @Volatile
    var tenantId: String? = null

    @Volatile
    var actorId: String? = null

    @Volatile
    var email: String? = null

    @Volatile
    var parkId: String? = null
}
