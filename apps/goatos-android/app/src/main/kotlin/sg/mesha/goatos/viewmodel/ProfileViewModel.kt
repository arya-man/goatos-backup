package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.datastore.SessionStore
import sg.mesha.goatos.feature.profile.ProfileUiState
import sg.mesha.goatos.feature.profile.SettingKind
import sg.mesha.goatos.ui.sampleProfileState
import javax.inject.Inject

/**
 * You / Settings state holder. Seeds the interim [sampleProfileState] fixture and owns the
 * session-backed actions: [signOut] clears the bearer token in [SessionStore] (MainActivity's
 * gate then flips to the login screen), and [setLanguage] / [cycleLanguage] persist the chosen
 * language and reflect it on the Language row so the change feels live. RFID / notifications
 * rows are navigation, routed by the host.
 *
 * TODO: replace the fake seed with the bootstrap-surfaced settings list via AppApi.
 */
@HiltViewModel
class ProfileViewModel @Inject constructor(
    private val sessionStore: SessionStore,
) : ViewModel() {

    private val _state = MutableStateFlow(sampleProfileState())
    val state: StateFlow<ProfileUiState> = _state.asStateFlow()

    /** Clears the session token — the login gate in MainActivity reacts to this. */
    fun signOut() {
        viewModelScope.launch { sessionStore.setBearerToken(null) }
    }

    /** Persists [code] and updates the Language row's displayed value. */
    fun setLanguage(code: String) {
        val label = LANGUAGES[code] ?: return
        viewModelScope.launch { sessionStore.setLanguage(code) }
        _state.update { current ->
            current.copy(
                rows = current.rows.map {
                    if (it.kind == SettingKind.LANGUAGE) it.copy(value = label) else it
                },
            )
        }
    }

    /** Cycles en → hi → kn → te → en (interim, until a real language picker sheet exists). */
    fun cycleLanguage() {
        val current = _state.value.rows
            .firstOrNull { it.kind == SettingKind.LANGUAGE }?.value
        val currentCode = LANGUAGES.entries.firstOrNull { it.value == current }?.key ?: "en"
        val order = LANGUAGES.keys.toList()
        val next = order[(order.indexOf(currentCode) + 1) % order.size]
        setLanguage(next)
    }

    private companion object {
        val LANGUAGES = linkedMapOf(
            "en" to "English",
            "hi" to "हिन्दी",
            "kn" to "ಕನ್ನಡ",
            "te" to "తెలుగు",
        )
    }
}
