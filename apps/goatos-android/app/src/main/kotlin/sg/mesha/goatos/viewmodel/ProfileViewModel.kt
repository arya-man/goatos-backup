package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.datastore.SessionStore
import sg.mesha.goatos.core.designsystem.locale.AppLocaleState
import sg.mesha.goatos.feature.profile.ProfileUiState
import sg.mesha.goatos.feature.profile.SettingKind
import sg.mesha.goatos.feature.profile.SettingRow
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.rfid.RfidReaderStatus
import javax.inject.Inject

/**
 * You / Settings state holder. Renders the REAL signed-in principal from the bootstrap
 * operator profile (not a sample), the live RFID reader readiness on the RFID row, and the
 * persisted language. [signOut] clears the session; [setLanguage] persists + reflects the
 * chosen language. RFID / notifications rows are navigation, routed by the host.
 */
@HiltViewModel
class ProfileViewModel @Inject constructor(
    private val bootstrap: BootstrapRepository,
    private val sessionStore: SessionStore,
    private val reader: RfidReaderPort,
) : ViewModel() {

    private val _state = MutableStateFlow(placeholder())
    val state: StateFlow<ProfileUiState> = _state.asStateFlow()

    init {
        load()
        viewModelScope.launch {
            reader.status.collect { status -> _state.update { it.copy(rows = it.rows.withRfid(status)) } }
        }
    }

    private fun load() = viewModelScope.launch {
        val profile = runCatching { bootstrap.operatorProfile() }.getOrNull()
        // AppLocaleState is the single in-memory source of truth for the active app-wide
        // locale (MainActivity seeds it from SessionStore at launch and persists every
        // change back). Reading it here — instead of SessionStore again — guarantees this
        // row always agrees with whatever the app is actually rendering in, including a
        // language picked on Login before the user ever opens You/Settings.
        val langCode = AppLocaleState.tag
        val name = profile?.displayName?.ifBlank { profile.displayCode }?.ifBlank { null } ?: "Signed in"
        val role = profile?.primaryRoleHint?.ifBlank { "" } ?: ""
        val location = profile?.primaryLocation?.ifBlank { "" } ?: ""
        _state.value = ProfileUiState(
            name = name,
            roleLabel = role,
            // Mock subtitle is "role · location" (e.g. "Health Asst Mgr · CBE").
            scopeLabel = listOf(role, location).filter { it.isNotBlank() }.joinToString(" · "),
            initials = initialsOf(name),
            rows = baseRows(langCode).withRfid(reader.status.value),
        )
    }

    /** Clears the session token — the login gate in MainActivity reacts to this. */
    fun signOut() {
        viewModelScope.launch { sessionStore.setBearerToken(null) }
    }

    /**
     * Switches the app-wide locale via [AppLocaleState] — the same call LoginScreen makes —
     * so the whole tree recomposes in the new language, not just this row's label.
     * MainActivity observes [AppLocaleState.tag] and persists it to [SessionStore] itself
     * (see MainActivity's `LaunchedEffect(AppLocaleState.tag)`), so this no longer writes
     * SessionStore directly: one source of truth, one place that persists it.
     */
    fun setLanguage(code: String) {
        val label = LANGUAGES[code] ?: return
        AppLocaleState.set(code)
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
        val current = _state.value.rows.firstOrNull { it.kind == SettingKind.LANGUAGE }?.value
        val currentCode = LANGUAGES.entries.firstOrNull { it.value == current }?.key ?: "en"
        val order = LANGUAGES.keys.toList()
        setLanguage(order[(order.indexOf(currentCode) + 1) % order.size])
    }

    private fun placeholder(): ProfileUiState = ProfileUiState(
        name = "…",
        roleLabel = "",
        scopeLabel = "",
        initials = "",
        rows = baseRows("en"),
    )

    private fun baseRows(langCode: String): List<SettingRow> = listOf(
        SettingRow(SettingKind.LANGUAGE, "Language", value = LANGUAGES[langCode] ?: "English"),
        SettingRow(SettingKind.RFID, "RFID reader"),
        // Mock renders Notifications as a chevron row (opens notification settings), not a
        // toggle switch — keep the row a plain navigable entry.
        SettingRow(SettingKind.NOTIFICATIONS, "Notifications"),
        // Read-only HRMS shift roster mirror (docs/hr/roster-rbac-design.md) — the route +
        // handler already existed (AppNavHost Routes.TIMETABLE); this row was missing so the
        // screen was unreachable from You/Settings (maintainer review finding).
        SettingRow(SettingKind.TIMETABLE, "Timetable", subtitle = "Shift roster (read-only)"),
        SettingRow(SettingKind.SIGN_OUT, "Sign out"),
    )

    /** Reflect live reader readiness on the RFID settings row. */
    private fun List<SettingRow>.withRfid(status: RfidReaderStatus): List<SettingRow> = map { row ->
        if (row.kind != SettingKind.RFID) return@map row
        val (value, subtitle, emphasis) = when (status) {
            RfidReaderStatus.READY -> Triple("Ready", "Reader connected", true)
            RfidReaderStatus.PAIRED_NOT_READY -> Triple("Reconnect", "Paired, not connected", false)
            RfidReaderStatus.NOT_PAIRED -> Triple("Pair", "Not paired", false)
            RfidReaderStatus.PERMISSION_NEEDED -> Triple("Allow", "Nearby devices permission needed", false)
            RfidReaderStatus.BLUETOOTH_OFF -> Triple("Bluetooth off", "Turn on Bluetooth", false)
        }
        row.copy(subtitle = subtitle, value = value, valueEmphasis = emphasis)
    }

    private fun initialsOf(name: String): String =
        name.split(' ', '·').filter { it.isNotBlank() }.take(2)
            .joinToString("") { it.first().uppercase() }
            .ifBlank { name.take(1).uppercase() }

    private companion object {
        val LANGUAGES = linkedMapOf(
            "en" to "English",
            "hi" to "हिन्दी",
            "kn" to "ಕನ್ನಡ",
            "te" to "తెలుగు",
        )
    }
}
