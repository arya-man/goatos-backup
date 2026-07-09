package sg.mesha.goatos.boot

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import sg.mesha.goatos.BuildConfig
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.datastore.SessionStore
import javax.inject.Inject

/**
 * Session gate: the shell renders only when a session token is present. Goat OS
 * sign-in is email-always — the real flow mints a verified token via the backend
 * after OTP/Firebase (gated).
 *
 * Until real auth lands, [signIn] can only establish a session when a real HS256
 * dev bearer token has been injected at build time (`goatosDevBearerToken`). It
 * NEVER fabricates a placeholder token: a bogus token would flip [isAuthed] true
 * yet every protected API would 401, dropping the user into an empty/fake shell.
 * When no token is configured, sign-in fails loudly via [signInError] instead.
 */
@HiltViewModel
class SessionViewModel @Inject constructor(
    private val sessionStore: SessionStore,
) : ViewModel() {

    val isAuthed: StateFlow<Boolean> = sessionStore.bearerToken
        .map { !it.isNullOrBlank() }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), false)

    private val _signInError = MutableStateFlow<String?>(null)
    val signInError: StateFlow<String?> = _signInError.asStateFlow()

    fun signIn(email: String) {
        val token = BuildConfig.DEV_BEARER_TOKEN
        if (token.isBlank()) {
            // No real backend token in this build — do NOT create a fake session.
            _signInError.value =
                "This build has no backend session configured. Install a dev build with " +
                "goatosDevBearerToken set, or a Firebase-verified build, to sign in."
            return
        }
        viewModelScope.launch {
            _signInError.value = null
            sessionStore.setBearerToken(token)
        }
    }

    fun signOut() {
        viewModelScope.launch {
            _signInError.value = null
            sessionStore.setBearerToken(null)
        }
    }
}
