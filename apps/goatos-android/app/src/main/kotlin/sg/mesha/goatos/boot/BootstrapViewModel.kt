package sg.mesha.goatos.boot

import android.util.Log
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsContext
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsSession
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.auth.AuthRepository
import sg.mesha.goatos.core.network.BootstrapError
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.sync.ConnectivityGate
import sg.mesha.goatos.core.datastore.DeviceStore
import sg.mesha.goatos.core.model.nav.NavChrome
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.push.PushTokenSync
import javax.inject.Inject

/**
 * Load status of the backend-driven bootstrap. A failure is surfaced as [Error]
 * rather than silently collapsing to an empty shell — an unreachable backend or a
 * rejected (401) token must NOT look like a signed-in principal with no modules.
 */
sealed interface BootstrapUiState {
    data object Loading : BootstrapUiState
    data class Ready(val navState: NavState) : BootstrapUiState
    data class Error(
        val errorType: BootstrapErrorType,
    ) : BootstrapUiState
}

/**
 * Loads the backend-driven nav state at boot (MVI: a single observable
 * [StateFlow]). Hilt injects the repository. Failures become [BootstrapUiState.Error]
 * so the shell can show a retryable error state instead of a blank/fake shell.
 */
@HiltViewModel
class BootstrapViewModel @Inject constructor(
    private val repo: BootstrapRepository,
    private val analytics: AnalyticsPort,
    private val analyticsContext: AnalyticsContext,
    private val deviceStore: DeviceStore,
    private val authRepository: AuthRepository,
    private val crashReporter: CrashReporter,
    private val pushTokenSync: PushTokenSync,
    private val connectivityGate: ConnectivityGate,
) : ViewModel() {
    private companion object {
        const val TAG = "GoatOSBootstrap"
    }

    private val _state = MutableStateFlow<BootstrapUiState>(BootstrapUiState.Loading)
    val state: StateFlow<BootstrapUiState> = _state.asStateFlow()

    // NO init { load() }.
    //
    // MainActivity already drives the load on the auth transition, and it MUST -- a bootstrap
    // fetched before the new session is active renders a stale shell (see the comment at that
    // call site). With both, a cold start ran load() twice and emitted two bootstrap_loaded
    // events, so every funnel counted one launch as two and the drop-off between "opened" and
    // "started work" read better than it was. One owner, one event.

    /**
     * Discards whatever nav state is currently held (logout clean-slate, C35-001). This
     * ViewModel is Activity-scoped (`by viewModels()` in `MainActivity`, not tied to the
     * session), so without this it would keep the departing user's [BootstrapUiState.Ready]
     * across a logout — the next principal signing in on the SAME device/Activity would
     * momentarily render the prior user's nav/identity until something happened to trigger
     * another [load]. Callers must follow a reset with [load] once a new session is
     * established (see `MainActivity`'s auth-state observer).
     */
    fun reset() {
        _state.value = BootstrapUiState.Loading
    }

    fun load() {
        viewModelScope.launch {
            _state.value = BootstrapUiState.Loading
            runCatching { repo.loadNavState() }
                .onSuccess { navState ->
                    _state.value = BootstrapUiState.Ready(navState)
                    val chrome = if (navState.chrome == NavChrome.EXPANDED) "expanded" else "minimal"
                    // Set analytics identity BEFORE any event is tracked (required: identity must be set
                    // before the first event of the session). This ensures setUserId() and user properties
                    // (email, device_id) are already applied when BOOTSTRAP_LOADED fires.
                    applyAnalyticsIdentity()
                    // Now that identity is set, retrieve and log the signed-in user's credentials.
                    val email = runCatching { authRepository.currentEmail() }.getOrNull()?.ifBlank { null }
                    val firebaseUid = runCatching { authRepository.currentFirebaseUid() }.getOrNull()?.ifBlank { null }
                    // Missing email/uid is EXPECTED, not a bug, for AuthMode.DEV_BEARER (see
                    // [identityGapIsExpectedForFlavor]) — the dev flavor's local backend authenticates
                    // via a baked HS256 bearer token, never touching FirebaseAuth, so
                    // `firebaseAuth.currentUser` is permanently null there and email/uid genuinely do
                    // not exist at the source. Only log the non-fatal when the gap is UNEXPECTED
                    // (stg/prod, Firebase-mode sign-in) so this breadcrumb stays a real signal instead
                    // of firing on every single dev-flavor bootstrap.
                    if ((email == null || firebaseUid == null) && !identityGapIsExpectedForFlavor()) {
                        crashReporter.log("bootstrap identity incomplete email=$email uid=$firebaseUid")
                    }
                    // Extends the existing BOOTSTRAP_LOADED event (never a duplicate second
                    // event) with WHO this bootstrap resolved for and WHAT it granted, plus
                    // whether the answer came from the network or the offline cache fallback
                    // (DefaultBootstrapRepository.loadNavState's ConnectivityFailure branch) —
                    // best-effort inferred from connectivity AT THIS MOMENT, since the
                    // repository itself does not report which path it took.
                    val role = runCatching { repo.operatorProfile() }.getOrNull()?.primaryRoleHint?.ifBlank { null }
                    val moduleKeys = navState.modules.map { it.key }.sorted().joinToString(",")
                    val offline = runCatching { !connectivityGate.isOnline() }.getOrDefault(false)
                    analytics.track(
                        AnalyticsEvents.BOOTSTRAP_LOADED,
                        buildMap {
                            put(AnalyticsEvents.Params.CHROME, chrome)
                            email?.let { put(AnalyticsEvents.Params.EMAIL, it) }
                            firebaseUid?.let { put(AnalyticsEvents.Params.FIREBASE_UID, it) }
                            role?.let { put(AnalyticsEventsSession.Params.ROLE, it) }
                            if (moduleKeys.isNotBlank()) put(AnalyticsEventsSession.Params.MODULE_KEYS, moduleKeys)
                            put(AnalyticsEventsSession.Params.OFFLINE, offline.toString())
                        },
                    )
                    logInfo("Bootstrap loaded email=${email.orEmpty()} uid=${firebaseUid.orEmpty()} chrome=$chrome role=${role.orEmpty()} offline=$offline")
                }
                .onFailure { throwable ->
                    // Set analytics identity even on bootstrap failure, so failure events have user context.
                    runCatching { applyAnalyticsIdentity() }
                    val email = runCatching { authRepository.currentEmail() }.getOrNull()?.ifBlank { null }
                    val firebaseUid = runCatching { authRepository.currentFirebaseUid() }.getOrNull()?.ifBlank { null }
                    logError("Bootstrap failed email=${email.orEmpty()} uid=${firebaseUid.orEmpty()}", throwable)

                    // Distinguish auth failures (401/expired token) from connectivity failures.
                    val errorType = when (throwable) {
                        is BootstrapError.AuthSessionExpired -> BootstrapErrorType.AUTH_SESSION_EXPIRED
                        is BootstrapError.AccessNotProvisioned -> BootstrapErrorType.ACCESS_NOT_PROVISIONED
                        is BootstrapError.ConnectivityFailure -> BootstrapErrorType.CONNECTIVITY_FAILURE
                        else -> {
                            // Fallback for unexpected errors (should not occur with the new mapping).
                            BootstrapErrorType.CONNECTIVITY_FAILURE
                        }
                    }

                    val offline = runCatching { !connectivityGate.isOnline() }.getOrDefault(false)
                    analytics.track(
                        AnalyticsEvents.BOOTSTRAP_FAILED,
                        buildMap {
                            put(AnalyticsEvents.Params.REASON, throwable::class.java.simpleName.ifBlank { "unknown" })
                            email?.let { put(AnalyticsEvents.Params.EMAIL, it) }
                            firebaseUid?.let { put(AnalyticsEvents.Params.FIREBASE_UID, it) }
                            put(AnalyticsEventsSession.Params.OFFLINE, offline.toString())
                        },
                    )
                    _state.value = BootstrapUiState.Error(errorType)
                }
        }
    }

    /**
     * Sets the analytics principal identity from the just-resolved bootstrap: [AnalyticsPort.setUserId]
     * (the stable, non-PII `operator_id`), the [AnalyticsContext] the egress impl reads, and the
     * durable user properties (role, park label + id, tenant, flavor, email, device id). Also
     * couples this device's FCM push token to the backend ([PushTokenSync]) — covers a cold start
     * with an already-valid session, not just a fresh sign-in, since `onNewToken` only fires once
     * per token mint/rotation. Best-effort: a profile/tenant read that fails (e.g. a leadership user
     * with no operator profile) must never fail or block bootstrap, so each read is wrapped and
     * its absence just leaves that piece of identity un-narrowed. Email is sent as a user property
     * by explicit business-owner decision (overrides the earlier ids/labels-only convention);
     * names/phone are still never sent.
     *
     * Email is genuinely ABSENT AT SOURCE (not merely late) for [sg.mesha.goatos.boot.AuthMode.
     * DEV_BEARER] sign-ins: that flavor's `SessionViewModel.signInWithDevToken` never calls
     * FirebaseAuth at all, so `authRepository.currentEmail()` returns null for the life of the
     * session, by construction. `GET /app/bootstrap`'s [sg.mesha.goatos.core.data.BootstrapRepository]
     * profile (`BootstrapOperatorProfileDto`) never carries an email either — deliberately, per the
     * "names/phone are still never sent" rule above. The correct STABLE human key in that case is
     * [memberId] (`profile.operatorId`), which is already sent via [AnalyticsPort.setUserId] whether
     * or not email resolves — that call is unconditional, a few lines below.
     */
    private suspend fun applyAnalyticsIdentity() {
        val profile = runCatching { repo.operatorProfile() }.getOrNull()
        val tenantId = runCatching { repo.actorTenantId() }.getOrNull()
        val role = profile?.primaryRoleHint?.ifBlank { null }
        val park = profile?.primaryLocation?.ifBlank { null }
        val parkId = profile?.primaryLocationId?.ifBlank { null }
        val memberId = profile?.operatorId?.ifBlank { null }
        // Signed-in user's email (business-owner decision: primary user identity dimension).
        val email = runCatching { authRepository.currentEmail() }.getOrNull()?.ifBlank { null }
        // Identity has ONE owner: the Application resolves it at start, before the first event.
        // This reads what is already there and only falls back to the store if that coroutine has
        // not landed yet. Re-deriving it here unconditionally made two concurrent resolvers of the
        // same ids, and the events already stamped with the first value would no longer match the
        // value that finally persisted -- splitting the very session the journey id exists to join.
        val deviceId = analyticsContext.deviceId
            ?: runCatching { deviceStore.appInstallId() }.getOrNull()?.ifBlank { null }
        val journeyId = analyticsContext.journeyId
            ?: runCatching { deviceStore.journeyId() }.getOrNull()?.ifBlank { null }

        analyticsContext.role = role
        analyticsContext.parkScope = park
        analyticsContext.deviceId = deviceId
        analyticsContext.journeyId = journeyId

        analytics.setUserId(memberId)
        analytics.setUserProperty(AnalyticsEvents.UserProps.ROLE, role)
        analytics.setUserProperty(AnalyticsEvents.UserProps.PRIMARY_PARK, park)
        analytics.setUserProperty(AnalyticsEvents.UserProps.PARK_ID, parkId)
        analytics.setUserProperty(AnalyticsEvents.UserProps.TENANT, tenantId)
        analytics.setUserProperty(AnalyticsEvents.UserProps.FLAVOR, analyticsContext.flavor)
        analytics.setUserProperty(AnalyticsEvents.UserProps.EMAIL, email)
        analytics.setUserProperty(AnalyticsEvents.UserProps.DEVICE_ID, deviceId)

        pushTokenSync.syncNow()
    }

    /** True when a missing email/Firebase-uid is an EXPECTED gap, not a defect: the dev flavor's
     *  [AuthMode.DEV_BEARER] path never establishes a Firebase session (see the KDoc on
     *  [applyAnalyticsIdentity]), so email/uid are unavailable at source for every dev-flavor
     *  bootstrap, by design. */
    private fun identityGapIsExpectedForFlavor(): Boolean =
        authModeForFlavor(analyticsContext.flavor) == AuthMode.DEV_BEARER

    private fun logInfo(message: String) {
        runCatching { Log.i(TAG, message) }
    }

    private fun logError(message: String, throwable: Throwable) {
        runCatching { Log.e(TAG, message, throwable) }
    }
}
