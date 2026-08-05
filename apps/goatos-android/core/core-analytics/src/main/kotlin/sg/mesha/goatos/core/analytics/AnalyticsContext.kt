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
    var role: String? = null

    @Volatile
    var parkScope: String? = null

    /**
     * INSTALL-lifetime identity: minted once on this Android install's very first launch
     * (`DeviceStore.appInstallId`/`appInstallIdSync`), persisted, and stamped onto every event by
     * [FirebaseAnalyticsAdapter.track] for as long as the app remains installed. Deliberately
     * survives logout (`PushLogoutCleanup` never nulls it, `DeviceStore.clear()` never wipes its
     * backing store) — it identifies the PHYSICAL DEVICE, not the signed-in principal, which is
     * exactly what lets the same user's sessions on two different phones (or two different users'
     * sessions on one shared field phone) be told apart. Only cleared by an app uninstall/data
     * wipe, never by a logout/re-login cycle.
     */
    @Volatile
    var deviceId: String? = null

    /**
     * SESSION-lifetime identity: this work session's journey id, stamped onto every event by
     * [FirebaseAnalyticsAdapter.track] exactly the way [deviceId] is. Populated from
     * `DeviceStore.journeyId()` on the same bootstrap path that populates [deviceId] (see
     * `BootstrapViewModel.applyAnalyticsIdentity`), so a whole login-to-logout work session
     * (hours-long, and possibly spanning a process death) is reconstructible from one id instead
     * of fragmenting across Firebase's 30-minute auto-sessions. UNLIKE [deviceId], this IS cleared
     * on logout (`PushLogoutCleanup`, `DeviceStore.clear()`) — a new sign-in, by the same or a
     * different operator, starts a new work session and must get its own journey id.
     */
    @Volatile
    var journeyId: String? = null
}
