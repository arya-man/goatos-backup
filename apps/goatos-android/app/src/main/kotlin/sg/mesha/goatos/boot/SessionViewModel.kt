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
 * sign-in is email-always — the real flow mints a verified token via Firebase/backend
 * token exchange (gated).
 *
 * Until real auth lands, [signIn] can only establish a session on the dev
 * flavor when a real HS256 dev bearer token has been injected at build time
 * (`goatosDevBearerToken`). It NEVER fabricates a placeholder token: a bogus
 * token would flip [isAuthed] true yet every protected API would 401, dropping
 * the user into an empty/fake shell. Staging/prod builds fail loudly via
 * [signInError] until Firebase/backend exchange is wired.
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
        val normalizedEmail = email.trim().lowercase()
        if (!normalizedEmail.endsWith("@mesha.sg")) {
            _signInError.value = "Use your Mesha work email to sign in."
            return
        }
        val token = BuildConfig.DEV_BEARER_TOKEN.takeIf { BuildConfig.FLAVOR == "dev" }.orEmpty()
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
