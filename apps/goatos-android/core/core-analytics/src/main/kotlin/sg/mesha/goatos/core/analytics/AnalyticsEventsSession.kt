package sg.mesha.goatos.core.analytics

/**
 * Session-boundary, bootstrap-gate, navigation, and login-time-permission event names — the
 * "can this journey be reconstructed start to finish" instrumentation pass.
 *
 * Kept in a SEPARATE object from [AnalyticsEvents] / [AnalyticsFunnels] on purpose: those files
 * are a hand-off boundary with a parallel session instrumenting feature-screen (weighing/verify)
 * events, and editing them concurrently risked a lost or conflicting change. New call sites in
 * `boot` (all files), `MainActivity`, `AppNavHost`, and `feature-auth` use THIS object; everything already
 * wired (login, bootstrap, module-switch, sign-out) keeps using [AnalyticsEvents] unchanged.
 *
 * Same snake_case + Firebase reserved-name discipline as [AnalyticsEvents] — see
 * [AnalyticsEvents.SESSION_START]'s KDoc for why that check matters. Every name below is pinned
 * in [AnalyticsContractTest] alongside the [AnalyticsEvents] contract.
 */
object AnalyticsEventsSession {
    /**
     * The app process left the foreground (`MainActivity.onStop`). Pairs with
     * [AnalyticsEvents.APP_OPEN] / [AnalyticsEvents.SESSION_START] to bound how long a session
     * was actually visible, and is the other half of "who signed in, what they opened, and how
     * the session ended" for a session that ends by backgrounding rather than sign-out.
     */
    const val APP_BACKGROUNDED = "app_backgrounded"

    /**
     * A previously-persisted session token authenticated this cold start WITHOUT a fresh
     * sign-in attempt this process — the token-restore path. Distinct from
     * [AnalyticsEvents.LOGIN_SUCCESS], which only ever fires from an explicit
     * [sg.mesha.goatos.boot.SessionViewModel.signInWithEmail] /
     * `signInWithGoogle` / dev-token call; without this event a returning operator's entire
     * session looked like it started at `bootstrap_loaded` with no visible sign-in at all.
     */
    const val SESSION_RESTORED = "session_restored"

    /**
     * A persisted session was rejected by the backend (401 / revoked / expired) and the
     * operator was forced back to the sign-in screen. Fired at the moment the forced-re-auth
     * screen is actually shown — [AnalyticsEvents.BOOTSTRAP_FAILED]
     * (reason=`AuthSessionExpired`) already records the underlying failure, but this is the
     * user-visible boundary: the point a still-open app becomes a login gate again.
     */
    const val SESSION_TOKEN_EXPIRED = "session_token_expired"

    /**
     * The non-dismissible force-update gate rendered because this build is below the
     * server-declared floor. The gate sits ABOVE auth and bootstrap
     * ([sg.mesha.goatos.MainActivity]'s `UpdateGateUiState.Blocked` branch), so until this
     * event existed nobody could see it firing at all — an out-of-date fleet looked identical
     * to one that simply never opened the app.
     */
    const val FORCE_UPDATE_GATE_SHOWN = "force_update_gate_shown"

    /** The operator tapped the force-update gate's CTA to open the install link. */
    const val FORCE_UPDATE_TAPPED = "force_update_tapped"

    /**
     * The gate re-confirmed a block on a later check (e.g. the app came back to the
     * foreground and [sg.mesha.goatos.update.UpdateGateViewModel.refresh] re-ran) — distinct
     * from [FORCE_UPDATE_GATE_SHOWN] so a dashboard can tell "first blocked this launch" from
     * "still blocked, operator keeps returning without updating".
     */
    const val FORCE_UPDATE_GATE_BLOCKING = "force_update_gate_blocking"

    /**
     * A navigation destination became current: a drawer module switch, a bottom-bar tab tap,
     * or a top-level route entered any other way. [Params.NAV_ROUTE] is the bounded-cardinality
     * route template (a `Routes.*` constant), never a raw id-bearing path. Fired from the single
     * NavHost-level seam in [sg.mesha.goatos.ui.AppNavHost] so no per-screen call site can forget
     * it and no screen can double-count it from recomposition.
     */
    const val ROUTE_ENTERED = "route_entered"

    /**
     * The back stack popped to a previous destination — covers the system back gesture/button
     * exactly as it covers an in-app Back affordance, since Compose Navigation routes both
     * through the same controller pop. [Params.NAV_ROUTE] is the route being LEFT.
     */
    const val ROUTE_EXITED_VIA_BACK = "route_exited_via_back"

    /**
     * The login-time optional device-permission readiness card
     * ([sg.mesha.goatos.feature.auth.PermissionGateCard]) rendered with at least one required
     * permission still missing. [Params.PERMISSION] is the manifest permission string.
     */
    const val PERMISSION_GATE_SHOWN = "permission_gate_shown"

    /**
     * A permission prompt from the login-time gate was answered.
     * [Params.PERMISSION] is the manifest permission; [AnalyticsEvents.Params.REASON] is
     * `granted`/`denied` — mirrors [AnalyticsEvents.NOTIFICATION_PERMISSION_RESULT]'s existing
     * convention rather than forking a new one. A denied camera permission here is exactly why
     * an operator "can't record" later, and today nothing at all surfaces that.
     */
    const val PERMISSION_GATE_RESULT = "permission_gate_result"

    object Params {
        /** Manifest permission string (`android.permission.CAMERA`, …) — bounded cardinality by
         *  construction (it is one of [sg.mesha.goatos.core.permissions.AppPermission]'s known
         *  values), never a user-entered value. */
        const val PERMISSION = "permission"

        /**
         * Bounded-cardinality nav route TEMPLATE (e.g. `Routes.CALENDAR`, `Routes.VACCINATION`),
         * never a raw id-bearing path. A separate key from [AnalyticsEvents.Params.ROUTE] (the
         * API-call route template) so a navigation funnel and an API-failure funnel never
         * collide on the same param name in the same event stream.
         */
        const val NAV_ROUTE = "nav_route"

        /** Resolved principal role at bootstrap (`operator`, `director`, …) — same value
         *  [sg.mesha.goatos.boot.BootstrapViewModel] stamps as the [AnalyticsEvents.UserProps.ROLE]
         *  user property, carried here as a per-event param on [AnalyticsEvents.BOOTSTRAP_LOADED]
         *  too so a single event answers "who, with what modules". */
        const val ROLE = "role"

        /** Comma-joined, sorted backend module keys the bootstrap granted — bounded cardinality
         *  (module keys are a small, backend-owned enum-like set; never a raw id). */
        const val MODULE_KEYS = "module_keys"

        /** `"true"`/`"false"` — whether the device was offline (no validated internet) at the
         *  moment this bootstrap resolved. A `bootstrap_loaded` with `offline=true` is the
         *  cache-fallback path succeeding; distinguishing it from an online load is the entire
         *  point of instrumenting it. */
        const val OFFLINE = "offline"
    }
}
