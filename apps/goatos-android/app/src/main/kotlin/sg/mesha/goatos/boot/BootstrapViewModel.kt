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
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.auth.AuthRepository
import sg.mesha.goatos.core.network.BootstrapError
import sg.mesha.goatos.core.data.BootstrapRepository
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
    private val pushTokenSync: PushTokenSync,
) : ViewModel() {
    private companion object {
        const val TAG = "GoatOSBootstrap"
    }

    private val _state = MutableStateFlow<BootstrapUiState>(BootstrapUiState.Loading)
    val state: StateFlow<BootstrapUiState> = _state.asStateFlow()

    init {
        load()
    }

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
                    val email = runCatching { authRepository.currentEmail() }.getOrNull()?.ifBlank { null }
                    val firebaseUid = runCatching { authRepository.currentFirebaseUid() }.getOrNull()?.ifBlank { null }
                    analytics.track(
                        AnalyticsEvents.BOOTSTRAP_LOADED,
                        buildMap {
                            put(AnalyticsEvents.Params.CHROME, chrome)
                            email?.let { put(AnalyticsEvents.Params.EMAIL, it) }
                            firebaseUid?.let { put(AnalyticsEvents.Params.FIREBASE_UID, it) }
                        },
                    )
                    logInfo("Bootstrap loaded email=${email.orEmpty()} uid=${firebaseUid.orEmpty()} chrome=$chrome")
                    applyAnalyticsIdentity()
                }
                .onFailure { throwable ->
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

                    analytics.track(
                        AnalyticsEvents.BOOTSTRAP_FAILED,
                        buildMap {
                            put(AnalyticsEvents.Params.REASON, throwable::class.java.simpleName.ifBlank { "unknown" })
                            email?.let { put(AnalyticsEvents.Params.EMAIL, it) }
                            firebaseUid?.let { put(AnalyticsEvents.Params.FIREBASE_UID, it) }
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
        // Stable per-install device id — same login on two phones is distinguishable.
        val deviceId = runCatching { deviceStore.appInstallId() }.getOrNull()?.ifBlank { null }

        analyticsContext.role = role
        analyticsContext.parkScope = park
        analyticsContext.deviceId = deviceId

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

    private fun logInfo(message: String) {
        runCatching { Log.i(TAG, message) }
    }

    private fun logError(message: String, throwable: Throwable) {
        runCatching { Log.e(TAG, message, throwable) }
    }
}
