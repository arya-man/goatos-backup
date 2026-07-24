package sg.mesha.goatos.boot

import android.content.Context
import androidx.credentials.exceptions.GetCredentialCancellationException
import androidx.credentials.exceptions.NoCredentialException
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.google.firebase.auth.FirebaseAuthInvalidCredentialsException
import com.google.firebase.auth.FirebaseAuthInvalidUserException
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import sg.mesha.goatos.BuildConfig
import sg.mesha.goatos.auth.AuthRepository
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.data.LogoutCoordinator
import sg.mesha.goatos.core.data.sync.SyncJobsScheduler
import sg.mesha.goatos.core.datastore.SessionStore
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.AuthSessionEventRequestDto
import sg.mesha.goatos.feature.auth.LoginError
import java.io.IOException
import javax.inject.Inject

/** Which credential path [SessionViewModel] routes sign-in through for the running flavor. */
internal enum class AuthMode { DEV_BEARER, FIREBASE }

/**
 * Firebase-mode session marker. In stg/prod the network layer authorizes every request with a
 * freshly-minted Firebase ID token ([sg.mesha.goatos.auth.currentFirebaseIdTokenBlocking], wired
 * in `AppModule`), so the persisted session value only has to signal "a user is signed in" for
 * [SessionViewModel.isAuthed]. Storing the short-lived ID token in plaintext DataStore would put a
 * live bearer on disk for no benefit — TRD requires no unencrypted credential at rest — so a
 * non-sensitive sentinel is persisted instead. (The dev flavor still persists its local HS256 dev
 * bearer, which the interceptor reads there and which is already baked into the APK.)
 */
internal const val FIREBASE_SESSION_MARKER = "firebase-session"

/** Pure: [BuildConfig.FLAVOR] carries no other meaning here, keeping this testable. */
internal fun authModeForFlavor(flavor: String): AuthMode =
    if (flavor == "dev") AuthMode.DEV_BEARER else AuthMode.FIREBASE

/** A dev APK can be reinstalled for a different local E2E principal. Its newly baked token is
 *  authoritative; retaining the previous APK's persisted bearer would silently run the wrong
 *  role until app data was cleared. Shared Firebase builds never use this replacement path. */
internal fun devSessionNeedsRefresh(mode: AuthMode, persisted: String?, baked: String): Boolean =
    mode == AuthMode.DEV_BEARER && persisted != baked.takeIf { it.isNotBlank() }

internal data class LoginUiState(
    val isLoading: Boolean = false,
    val errorReason: LoginError? = null,
    val errorDetail: String? = null,
    val resetEmailSent: String? = null,
)

/**
 * Session gate: the shell renders only when a session token is present.
 *
 * - dev flavor ([AuthMode.DEV_BEARER]): the local backend runs HS256/bearer auth, so a
 *   Firebase ID token would never validate there. Sign-in routes to the injected dev
 *   bearer token (`goatosDevBearerToken`), matching the emulator flow that existed before
 *   real auth landed.
 * - stg / prod flavor ([AuthMode.FIREBASE]): real Firebase Auth: email/password, Google
 *   SSO (Credential Manager -> GoogleIdTokenCredential -> Firebase), and password reset.
 *   On success the Firebase ID token is stored as the session bearer, while the network
 *   layer re-fetches a fresh token per request so expiry never stales a live session.
 */
@HiltViewModel
class SessionViewModel @Inject constructor(
    private val sessionStore: SessionStore,
    private val authRepository: AuthRepository,
    private val analytics: AnalyticsPort,
    private val logoutCoordinator: LogoutCoordinator,
    private val syncJobsScheduler: SyncJobsScheduler,
    private val appApi: AppApi,
    private val relauncher: SessionRelauncher,
) : ViewModel() {

    private val authMode = authModeForFlavor(BuildConfig.FLAVOR)
    private val devSessionReady = MutableStateFlow(authMode != AuthMode.DEV_BEARER)

    val isAuthed: StateFlow<Boolean> = combine(sessionStore.bearerToken, devSessionReady) { token, ready ->
        ready && !token.isNullOrBlank()
    }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), false)

    private val _uiState = MutableStateFlow(LoginUiState())
    internal val uiState: StateFlow<LoginUiState> = _uiState.asStateFlow()

    init {
        if (authMode == AuthMode.DEV_BEARER) {
            viewModelScope.launch {
                val baked = BuildConfig.DEV_BEARER_TOKEN
                val persisted = sessionStore.currentToken()
                if (devSessionNeedsRefresh(authMode, persisted, baked)) {
                    // A different baked token means a different local principal. Use the same
                    // authority-boundary wipe as an explicit logout: revoke the old device,
                    // remove its Room/outbox/cache state, cancel its jobs, and generate a fresh
                    // device identity before opening the new session. Replacing only the token
                    // caused bootstrap to send the previous operator's device id as a verifier.
                    logoutCoordinator.logout(signOutVendorAuth = authRepository::signOut)
                    if (baked.isNotBlank()) {
                        sessionStore.setBearerToken(baked)
                        withContext(Dispatchers.IO) { syncJobsScheduler.scheduleAll() }
                    }
                }
                // Do not let bootstrap/network requests race ahead with the previous APK's
                // persisted principal. A blank baked token deliberately leaves the login gate.
                devSessionReady.value = true
            }
        }
    }

    fun signInWithEmail(email: String, password: String) {
        analytics.track(AnalyticsEvents.LOGIN_ATTEMPT, mapOf(AnalyticsEvents.Params.METHOD to "email"))
        if (authMode == AuthMode.DEV_BEARER) {
            signInWithDevToken()
            return
        }
        viewModelScope.launch {
            _uiState.update { it.copy(isLoading = true, errorReason = null, errorDetail = null, resetEmailSent = null) }
            authRepository.signInWithEmailPassword(email, password)
                .onSuccess { persistFirebaseSession() }
                .onFailure { reportAuthFailure(it) }
        }
    }

    fun signInWithGoogle(activityContext: Context) {
        analytics.track(AnalyticsEvents.LOGIN_ATTEMPT, mapOf(AnalyticsEvents.Params.METHOD to "google"))
        if (authMode == AuthMode.DEV_BEARER) {
            signInWithDevToken()
            return
        }
        viewModelScope.launch {
            _uiState.update { it.copy(isLoading = true, errorReason = null, errorDetail = null, resetEmailSent = null) }
            authRepository.signInWithGoogle(activityContext)
                .onSuccess { persistFirebaseSession() }
                .onFailure { reportAuthFailure(it) }
        }
    }

    fun sendPasswordReset(email: String) {
        analytics.track(AnalyticsEvents.PASSWORD_RESET_REQUESTED)
        if (authMode == AuthMode.DEV_BEARER) {
            // The local dev backend has no Firebase user store to reset a password against.
            analytics.track(AnalyticsEvents.LOGIN_FAILURE, mapOf(AnalyticsEvents.Params.REASON to "no_dev_backend"))
            _uiState.update { it.copy(errorReason = LoginError.NO_DEV_BACKEND, errorDetail = null) }
            return
        }
        viewModelScope.launch {
            _uiState.update { it.copy(isLoading = true, errorReason = null, errorDetail = null, resetEmailSent = null) }
            authRepository.sendPasswordReset(email)
                .onSuccess {
                    analytics.track(AnalyticsEvents.PASSWORD_RESET_SENT)
                    _uiState.update { it.copy(isLoading = false, resetEmailSent = email) }
                }
                .onFailure { reportAuthFailure(it) }
        }
    }

    /**
     * Full clean-slate logout (C35-001): delegates to the shared [LogoutCoordinator] — the
     * SAME path [sg.mesha.goatos.viewmodel.ProfileViewModel.signOut] uses — so every
     * authority-sensitive local store (Room caches, outbox, device identity, session) is
     * wiped, not just the bearer token this ViewModel happens to hold.
     */
    fun signOut() {
        viewModelScope.launch {
            logoutCoordinator.logout(signOutVendorAuth = authRepository::signOut)
            analytics.track(AnalyticsEvents.SIGN_OUT)
            _uiState.value = LoginUiState()
            // Disk is now wiped; relaunch the process so no in-memory state (singleton repo
            // caches, retained ViewModels, Coil memory cache, AppLocaleState) from the departing
            // principal can bleed into the next account. See [SessionRelauncher].
            relauncher.relaunchToLogin()
        }
    }

    /**
     * Confirms the just-established Firebase user actually has an ID token (honest failure if
     * not), then opens the session. Only a non-sensitive presence marker is persisted — never
     * the ID token itself; the network layer re-fetches a live token per request. See
     * [FIREBASE_SESSION_MARKER].
     */
    private suspend fun persistFirebaseSession() {
        val token = authRepository.currentIdToken()
        if (token.isNullOrBlank()) {
            analytics.track(AnalyticsEvents.LOGIN_FAILURE, mapOf(AnalyticsEvents.Params.REASON to "no_token_issued"))
            _uiState.update {
                it.copy(
                    isLoading = false,
                    errorReason = LoginError.UNKNOWN,
                    errorDetail = "Signed in, but no Firebase session token was issued.",
                )
            }
            return
        }
        runCatching {
            appApi.recordAuthSessionEvent(
                AuthSessionEventRequestDto(
                    eventType = "auth.sign_in",
                    source = "android-${BuildConfig.FLAVOR}",
                ),
            )
        }.onFailure { t ->
            analytics.track(AnalyticsEvents.LOGIN_FAILURE, mapOf(AnalyticsEvents.Params.REASON to "session_event_failed"))
            _uiState.update {
                it.copy(
                    isLoading = false,
                    errorReason = LoginError.UNKNOWN,
                    errorDetail = t.message ?: "Signed in, but Goat OS could not open your workspace.",
                )
            }
            return
        }
        sessionStore.setBearerToken(FIREBASE_SESSION_MARKER)
        // A prior signOut() cancelled the periodic/retry WorkManager backstop
        // (LogoutCoordinator's clean-slate wipe) — re-arm it for this new session.
        // ExistingPeriodicWorkPolicy.KEEP makes this idempotent when it was never cancelled.
        // WorkManager's enqueue does disk I/O on the calling thread, so hop off Main.
        withContext(Dispatchers.IO) { syncJobsScheduler.scheduleAll() }
        analytics.track(AnalyticsEvents.LOGIN_SUCCESS)
        _uiState.update { it.copy(isLoading = false, errorReason = null, errorDetail = null) }
    }

    private fun signInWithDevToken() {
        val token = BuildConfig.DEV_BEARER_TOKEN
        if (token.isBlank()) {
            _uiState.update { it.copy(errorReason = LoginError.NO_DEV_BACKEND, errorDetail = null) }
            return
        }
        viewModelScope.launch {
            _uiState.update { it.copy(errorReason = null, errorDetail = null) }
            sessionStore.setBearerToken(token)
            withContext(Dispatchers.IO) { syncJobsScheduler.scheduleAll() }
        }
    }

    private fun reportAuthFailure(error: Throwable) {
        val (reason, detail) = classifyAuthError(error)
        _uiState.update { it.copy(isLoading = false, errorReason = reason, errorDetail = detail) }
    }
}

/**
 * Maps a sign-in failure to a [LoginError] the UI can localize. Pure, so it stays
 * unit-testable without a real FirebaseAuth or CredentialManager.
 */
internal fun classifyAuthError(error: Throwable): Pair<LoginError, String?> {
    // Tasks.await, Credential Manager, and coroutine bridges can wrap the real
    // Firebase error. Inspect the full cause chain and never expose raw provider
    // exception text to an operator.
    val chain = generateSequence(error) { it.cause }.toList()
    val signature = chain.joinToString(" | ") { "${it::class.java.name}: ${it.message.orEmpty()}" }

    return when {
        chain.any { it is FirebaseAuthInvalidUserException || it is FirebaseAuthInvalidCredentialsException } ->
            LoginError.INVALID_CREDENTIALS to null
        chain.any { it is GetCredentialCancellationException } -> LoginError.GOOGLE_CANCELLED to null
        chain.any { it is NoCredentialException } -> LoginError.NO_GOOGLE_ACCOUNT to null
        chain.any { it is IOException } -> LoginError.NETWORK to null
        signature.contains("too-many-requests", ignoreCase = true) ||
            signature.contains("too many", ignoreCase = true) ->
            LoginError.TOO_MANY_REQUESTS to null
        signature.contains("network", ignoreCase = true) ||
            signature.contains("timeout", ignoreCase = true) ->
            LoginError.NETWORK to null
        signature.contains("INVALID_LOGIN_CREDENTIALS", ignoreCase = true) ||
            signature.contains("auth credential is incorrect", ignoreCase = true) ||
            signature.contains("malformed or has expired", ignoreCase = true) ||
            signature.contains("password is invalid", ignoreCase = true) ||
            signature.contains("no user record", ignoreCase = true) ||
            signature.contains("badly formatted", ignoreCase = true) ||
            signature.contains("INVALID_EMAIL", ignoreCase = true) ->
            LoginError.INVALID_CREDENTIALS to null
        else -> LoginError.UNKNOWN to null
    }
}
