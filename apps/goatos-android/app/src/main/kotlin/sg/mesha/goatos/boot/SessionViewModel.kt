package sg.mesha.goatos.boot

import android.content.Context
import android.util.Log
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
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import sg.mesha.goatos.BuildConfig
import sg.mesha.goatos.auth.AuthRepository
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsSession
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.data.LogoutCoordinator
import sg.mesha.goatos.core.data.sync.SyncJobsScheduler
import sg.mesha.goatos.core.datastore.SessionStore
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.AuthSessionEventRequestDto
import sg.mesha.goatos.feature.auth.LoginError
import java.io.IOException
import java.util.Base64
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

internal fun devSessionNeedsWipe(mode: AuthMode, persisted: String?, baked: String): Boolean =
    mode == AuthMode.DEV_BEARER && when {
        persisted.isNullOrBlank() -> false
        baked.isBlank() -> true
        persisted == baked -> false
        else -> {
            val persistedPrincipal = devBearerPrincipalKey(persisted)
            val bakedPrincipal = devBearerPrincipalKey(baked)
            persistedPrincipal == null || bakedPrincipal == null || persistedPrincipal != bakedPrincipal
        }
    }

internal fun devBearerPrincipalKey(token: String): String? = runCatching {
    val payload = token.split('.').getOrNull(1)?.takeIf { it.isNotBlank() } ?: return@runCatching null
    val json = String(Base64.getUrlDecoder().decode(payload), Charsets.UTF_8)
    val obj = Json.parseToJsonElement(json).jsonObject
    val subject = obj["sub"]?.jsonPrimitive?.content?.trim().orEmpty()
    val tenant = obj["tenant_id"]?.jsonPrimitive?.content?.trim().orEmpty()
    if (subject.isBlank() || tenant.isBlank()) null else "$tenant:$subject"
}.onFailure {
    android.util.Log.w("SessionViewModel", "dev_bearer_principal_parse_failed")
}.getOrNull()

internal fun sessionIsAuthedForMode(mode: AuthMode, persisted: String?): Boolean =
    when (mode) {
        AuthMode.DEV_BEARER -> !persisted.isNullOrBlank()
        AuthMode.FIREBASE -> persisted == FIREBASE_SESSION_MARKER
    }

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
 *   On success only a Firebase-session marker is stored; the network layer re-fetches a
 *   fresh ID token per request so expiry never stales a live session.
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
    private companion object {
        const val TAG = "GoatOSSession"
    }

    private val authMode = authModeForFlavor(BuildConfig.FLAVOR)
    private val devSessionReady = MutableStateFlow(authMode != AuthMode.DEV_BEARER)

    /**
     * null until the stored session has actually been READ. Seeding this false meant the login
     * card was drawn for the first frames of every cold start, then replaced the moment the token
     * arrived -- a sign-in screen flashed at an already-signed-in operator. Callers must treat
     * null as "not known yet" and render neither the app nor the login gate.
     */
    val isAuthed: StateFlow<Boolean?> = combine(sessionStore.bearerToken, devSessionReady) { token, ready ->
        // Not-ready is UNKNOWN, not "signed out". Returning false here published a confident
        // "show the login card" before the dev session had been read, so a dev build flashed a
        // sign-in screen at an already-signed-in operator -- the same defect one layer down.
        if (!ready) null else sessionIsAuthedForMode(authMode, token)
    }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)

    private val _uiState = MutableStateFlow(LoginUiState())
    internal val uiState: StateFlow<LoginUiState> = _uiState.asStateFlow()

    init {
        if (authMode == AuthMode.DEV_BEARER) {
            viewModelScope.launch {
                // The whole dev-session bring-up is wrapped so devSessionReady ALWAYS resolves.
                // A throw in here (logout, token store, scheduler) would otherwise leave isAuthed
                // null forever: a permanent loading screen with no way to reach the login gate.
                try {
                val baked = BuildConfig.DEV_BEARER_TOKEN
                val persisted = sessionStore.currentToken()
                if (devSessionNeedsRefresh(authMode, persisted, baked)) {
                    if (devSessionNeedsWipe(authMode, persisted, baked)) {
                        // A different baked principal is a real authority boundary. Use the same
                        // wipe as explicit logout before opening the new local E2E actor.
                        logoutCoordinator.logout(signOutVendorAuth = authRepository::signOut)
                    }
                    if (baked.isNotBlank()) {
                        sessionStore.setBearerToken(baked)
                        withContext(Dispatchers.IO) { syncJobsScheduler.scheduleAll() }
                    }
                }
                // Do not let bootstrap/network requests race ahead with the previous APK's
                // persisted principal. A blank baked token deliberately leaves the login gate.
                devSessionReady.value = true
                // A non-blank token surviving this process's cold start (whether it was already
                // there or just replaced above) opened the session WITHOUT a fresh sign-in
                // attempt this run — the token-restore path, distinct from LOGIN_SUCCESS which
                // only ever follows an explicit signIn* call.
                if (!sessionStore.currentToken().isNullOrBlank()) {
                    analytics.track(AnalyticsEventsSession.SESSION_RESTORED)
                }
                } catch (t: Throwable) {
                    // A cancelled scope is not a failure. Catching Throwable without letting
                    // CancellationException through breaks structured concurrency: rotating the
                    // screen or navigating away would be reported as an error and would publish
                    // state after the scope had already been cancelled.
                    if (t is kotlinx.coroutines.CancellationException) throw t
                    // FAIL CLOSED. The wipe above revokes the previous principal's device and
                    // clears its state; if it threw partway, the OLD bearer may still be on disk.
                    // Simply opening the gate here would let bootstrap reopen that principal --
                    // a cross-principal session, which is worse than the locked-loading screen
                    // this catch was added to prevent. Drop the token first, so the gate opens on
                    // a signed-OUT app the operator can sign into.
                    runCatching { sessionStore.setBearerToken(null) }
                    // Do NOT rethrow. This runs in viewModelScope.launch, so an uncaught throw
                    // here takes the app down on cold start instead of showing the signed-out
                    // gate this catch exists to reach. Record it and let the finally publish the
                    // resolved (signed-out) state.
                    // exception:exempt startup-path diagnostic; the signed-out state IS the handling
                    android.util.Log.e(TAG, "dev session bring-up failed; signing out", t)
                } finally {
                    // Resolve either way: unknown-forever is a locked-out app. By here the token
                    // is either the new principal's or gone.
                    devSessionReady.value = true
                }
            }
        } else {
            viewModelScope.launch {
                val persisted = sessionStore.currentToken()
                if (!persisted.isNullOrBlank() && persisted != FIREBASE_SESSION_MARKER) {
                    logWarning("Clearing stale non-Firebase session marker for flavor=${BuildConfig.FLAVOR}")
                    logoutCoordinator.logout(signOutVendorAuth = authRepository::signOut)
                } else if (persisted == FIREBASE_SESSION_MARKER) {
                    // A still-valid Firebase-session marker from a previous run: the session
                    // gate opens on this cached marker, not a fresh sign-in this process.
                    analytics.track(AnalyticsEventsSession.SESSION_RESTORED)
                }
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
            logWarning("Firebase sign-in returned no ID token email=${authRepository.currentEmail().orEmpty()} uid=${authRepository.currentFirebaseUid().orEmpty()}")
            _uiState.update {
                it.copy(
                    isLoading = false,
                    errorReason = LoginError.UNKNOWN,
                    errorDetail = "Signed in, but no Firebase session token was issued.",
                )
            }
            return
        }
        val email = authRepository.currentEmail()?.ifBlank { null }
        val firebaseUid = authRepository.currentFirebaseUid()?.ifBlank { null }
        val identityProps = buildMap {
            email?.let { put(AnalyticsEvents.Params.EMAIL, it) }
            firebaseUid?.let { put(AnalyticsEvents.Params.FIREBASE_UID, it) }
        }
        analytics.track(AnalyticsEvents.LOGIN_SESSION_READY, identityProps)
        analytics.setUserProperty(AnalyticsEvents.UserProps.EMAIL, email)
        logInfo("Firebase session ready email=${email.orEmpty()} uid=${firebaseUid.orEmpty()} flavor=${BuildConfig.FLAVOR}")
        runCatching {
            appApi.recordAuthSessionEvent(
                AuthSessionEventRequestDto(
                    eventType = "auth.sign_in",
                    source = "android-${BuildConfig.FLAVOR}",
                ),
            )
        }.onFailure { t ->
            analytics.track(
                AnalyticsEvents.LOGIN_FAILURE,
                identityProps + mapOf(AnalyticsEvents.Params.REASON to "session_event_failed"),
            )
            logWarning("Goat OS session event failed email=${email.orEmpty()} uid=${firebaseUid.orEmpty()}", t)
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
        analytics.track(AnalyticsEvents.LOGIN_SUCCESS, identityProps)
        logInfo("Goat OS login session opened email=${email.orEmpty()} uid=${firebaseUid.orEmpty()} flavor=${BuildConfig.FLAVOR}")
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
        val email = authRepository.currentEmail()?.ifBlank { null }
        val firebaseUid = authRepository.currentFirebaseUid()?.ifBlank { null }
        analytics.track(
            AnalyticsEvents.LOGIN_FAILURE,
            buildMap {
                put(AnalyticsEvents.Params.REASON, reason.name.lowercase())
                email?.let { put(AnalyticsEvents.Params.EMAIL, it) }
                firebaseUid?.let { put(AnalyticsEvents.Params.FIREBASE_UID, it) }
            },
        )
        logWarning("Firebase login failed reason=${reason.name} email=${email.orEmpty()} uid=${firebaseUid.orEmpty()}", error)
        _uiState.update { it.copy(isLoading = false, errorReason = reason, errorDetail = detail) }
    }

    private fun logInfo(message: String) {
        runCatching { Log.i(TAG, message) }
    }

    private fun logWarning(message: String, throwable: Throwable? = null) {
        runCatching {
            if (throwable == null) {
                Log.w(TAG, message)
            } else {
                Log.w(TAG, message, throwable)
            }
        }
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
