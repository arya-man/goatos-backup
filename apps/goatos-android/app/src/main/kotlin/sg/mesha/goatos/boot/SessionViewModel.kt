package sg.mesha.goatos.boot

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import sg.mesha.goatos.BuildConfig
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.datastore.SessionStore
import javax.inject.Inject

/**
 * Session gate: the shell renders only when a session token is present. Goat OS
 * sign-in is email-always — the real flow mints a verified token via the backend
 * after OTP/Firebase (gated). Here [signIn] sets a dev session token so the
 * gated shell + the network BearerAuthInterceptor are exercised end-to-end.
 */
@HiltViewModel
class SessionViewModel @Inject constructor(
    private val sessionStore: SessionStore,
) : ViewModel() {

    val isAuthed: StateFlow<Boolean> = sessionStore.bearerToken
        .map { !it.isNullOrBlank() }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), false)

    fun signIn(email: String) {
        viewModelScope.launch {
            // Dev: seed the injected HS256 dev token so the app authenticates against
            // the local backend; falls back to a stub if none is configured. Prod
            // replaces this with the Firebase-verified token (gated).
            val token = BuildConfig.DEV_BEARER_TOKEN.ifBlank { "dev-session:$email" }
            sessionStore.setBearerToken(token)
        }
    }

    fun signOut() {
        viewModelScope.launch {
            sessionStore.setBearerToken(null)
        }
    }
}
