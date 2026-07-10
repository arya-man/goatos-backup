package sg.mesha.goatos.boot

import android.content.Context
import androidx.credentials.exceptions.GetCredentialCancellationException
import androidx.credentials.exceptions.NoCredentialException
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.google.firebase.auth.FirebaseAuthInvalidCredentialsException
import com.google.firebase.auth.FirebaseAuthInvalidUserException
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.BuildConfig
import sg.mesha.goatos.auth.AuthRepository
import sg.mesha.goatos.core.datastore.SessionStore
import sg.mesha.goatos.feature.auth.LoginError
import java.io.IOException
import javax.inject.Inject

/** Which credential path [SessionViewModel] routes sign-in through for the running flavor. */
internal enum class AuthMode { DEV_BEARER, FIREBASE }

/** Pure: [BuildConfig.FLAVOR] carries no other meaning here, keeping this testable. */
internal fun authModeForFlavor(flavor: String): AuthMode =
    if (flavor == "dev") AuthMode.DEV_BEARER else AuthMode.FIREBASE

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
) : ViewModel() {

    val isAuthed: StateFlow<Boolean> = sessionStore.bearerToken
        .map { !it.isNullOrBlank() }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), false)

    private val _uiState = MutableStateFlow(LoginUiState())
    internal val uiState: StateFlow<LoginUiState> = _uiState.asStateFlow()

    private val authMode = authModeForFlavor(BuildConfig.FLAVOR)

    fun signInWithEmail(email: String, password: String) {
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
        if (authMode == AuthMode.DEV_BEARER) {
            _uiState.update { it.copy(errorReason = LoginError.NO_DEV_BACKEND, errorDetail = null) }
            return
        }
        viewModelScope.launch {
            _uiState.update { it.copy(isLoading = true, errorReason = null, errorDetail = null, resetEmailSent = null) }
            authRepository.sendPasswordReset(email)
                .onSuccess { _uiState.update { it.copy(isLoading = false, resetEmailSent = email) } }
                .onFailure { reportAuthFailure(it) }
        }
    }

    fun signOut() {
        viewModelScope.launch {
            authRepository.signOut()
            sessionStore.setBearerToken(null)
            _uiState.value = LoginUiState()
        }
    }

    /** Seeds the session bearer from the just-established Firebase user's ID token. */
    private suspend fun persistFirebaseSession() {
        val token = authRepository.currentIdToken()
        if (token.isNullOrBlank()) {
            _uiState.update {
                it.copy(
                    isLoading = false,
                    errorReason = LoginError.UNKNOWN,
                    errorDetail = "Signed in, but no Firebase session token was issued.",
                )
            }
            return
        }
        sessionStore.setBearerToken(token)
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
internal fun classifyAuthError(error: Throwable): Pair<LoginError, String?> = when (error) {
    is FirebaseAuthInvalidUserException,
    is FirebaseAuthInvalidCredentialsException,
    -> LoginError.INVALID_CREDENTIALS to null
    is GetCredentialCancellationException -> LoginError.GOOGLE_CANCELLED to null
    is NoCredentialException -> LoginError.NO_GOOGLE_ACCOUNT to null
    is IOException -> LoginError.NETWORK to null
    else -> {
        val message = error.message.orEmpty()
        when {
            message.contains("too-many-requests", ignoreCase = true) -> LoginError.TOO_MANY_REQUESTS to null
            message.contains("network", ignoreCase = true) -> LoginError.NETWORK to null
            else -> LoginError.UNKNOWN to error.message
        }
    }
}
