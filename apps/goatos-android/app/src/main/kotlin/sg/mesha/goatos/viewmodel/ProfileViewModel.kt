package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.auth.AuthRepository
import sg.mesha.goatos.boot.SessionRelauncher
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.LogoutCoordinator
import sg.mesha.goatos.core.designsystem.locale.AppLocaleState
import sg.mesha.goatos.feature.profile.ProfileUiState
import sg.mesha.goatos.feature.profile.RfidRowStatus
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
    private val authRepository: AuthRepository,
    private val reader: RfidReaderPort,
    private val logoutCoordinator: LogoutCoordinator,
    private val relauncher: SessionRelauncher,
) : ViewModel() {

    // Imperatively-updated profile/settings data (load/signOut/setLanguage/cycleLanguage). The
    // RFID row's live status is NOT folded in here — it is derived once, below, in [state] — so
    // there is a single application point for [reader.status] instead of two.
    private val _profileState = MutableStateFlow(placeholder())

    /**
     * Combines [_profileState] with the live RFID hardware status (MOB-010). This combine — and
     * therefore the [RfidReaderPort.status] collection — is COLD: it only runs
     * while [state] itself has an active subscriber, via the single WhileSubscribed(5_000)
     * below (same reference pattern as [AlertsViewModel.state]). The previous approach launched
     * a permanent forever-`collect` inside [init] around an inner `stateIn(WhileSubscribed)`,
     * which defeated it — that inner flow never saw zero subscribers, so the RFID hardware
     * stream was collected forever, never released when the screen was backgrounded.
     */
    val state: StateFlow<ProfileUiState> = combine(_profileState, reader.status) { profile, rfidStatus ->
        profile.copy(rows = profile.rows.withRfid(rfidStatus))
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), placeholder())

    init {
        load()
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
        _profileState.value = ProfileUiState(
            name = name,
            roleLabel = role,
            // Mock subtitle is "role · location" (e.g. "Health Asst Mgr · CBE").
            scopeLabel = listOf(role, location).filter { it.isNotBlank() }.joinToString(" · "),
            initials = initialsOf(name),
            rows = baseRows(langCode, showRfid = role == OPERATOR_ROLE),
        )
    }

    /**
     * Full clean-slate logout (C35-001): delegates to the shared [LogoutCoordinator] — the
     * SAME path [sg.mesha.goatos.boot.SessionViewModel.signOut] uses — so this entry point
     * (You/Settings) wipes every authority-sensitive local store exactly like the session
     * gate's sign-out does, instead of only dropping the bearer token. `authRepository.signOut`
     * is a harmless no-op on the dev/bearer flavor; the coordinator runs it at the one correct
     * point in the sequence (after the backend device-deregister attempt, before local wipes).
     */
    fun signOut() {
        viewModelScope.launch {
            logoutCoordinator.logout(signOutVendorAuth = authRepository::signOut)
            // Disk is now wiped; relaunch the process so no in-memory state (singleton repo
            // caches, retained ViewModels, Coil memory cache, AppLocaleState) from the departing
            // principal can bleed into the next account. See [SessionRelauncher].
            relauncher.relaunchToLogin()
        }
    }

    /**
     * Switches the app-wide locale via [AppLocaleState] — the same call LoginScreen makes —
     * so the whole tree recomposes in the new language, not just this row's label.
     * MainActivity observes [AppLocaleState.tag] and persists it to `SessionStore` itself
     * (see MainActivity's `LaunchedEffect(AppLocaleState.tag)`), so this no longer writes
     * SessionStore directly: one source of truth, one place that persists it.
     */
    fun setLanguage(code: String) {
        val label = LANGUAGES[code] ?: return
        AppLocaleState.set(code)
        _profileState.update { current ->
            current.copy(
                rows = current.rows.map {
                    if (it.kind == SettingKind.LANGUAGE) it.copy(value = label) else it
                },
            )
        }
    }

    /** Cycles en → hi → kn → te → en (interim, until a real language picker sheet exists). */
    fun cycleLanguage() {
        val current = _profileState.value.rows.firstOrNull { it.kind == SettingKind.LANGUAGE }?.value
        val currentCode = LANGUAGES.entries.firstOrNull { it.value == current }?.key ?: "en"
        val order = LANGUAGES.keys.toList()
        setLanguage(order[(order.indexOf(currentCode) + 1) % order.size])
    }

    private fun placeholder(): ProfileUiState = ProfileUiState(
        name = "…",
        roleLabel = "",
        scopeLabel = "",
        initials = "",
        rows = baseRows("en", showRfid = false),
    )

    private fun baseRows(langCode: String, showRfid: Boolean): List<SettingRow> =
        buildList {
            add(SettingRow(SettingKind.LANGUAGE, "", value = LANGUAGES[langCode] ?: "English"))
            if (showRfid) add(SettingRow(SettingKind.RFID, ""))
            add(SettingRow(SettingKind.SIGN_OUT, ""))
        }

    /** Reflect live reader readiness on the RFID settings row. */
    private fun List<SettingRow>.withRfid(status: RfidReaderStatus): List<SettingRow> = map { row ->
        if (row.kind != SettingKind.RFID) return@map row
        // Hand the Composable a module-local status enum so it can render localized strings;
        // feature-profile must not depend on the app-module RfidReaderStatus. Emphasis on READY.
        row.copy(rfidStatus = status.toRowStatus(), valueEmphasis = status == RfidReaderStatus.READY)
    }

    private fun RfidReaderStatus.toRowStatus(): RfidRowStatus = when (this) {
        RfidReaderStatus.READY -> RfidRowStatus.READY
        RfidReaderStatus.PAIRED_NOT_READY -> RfidRowStatus.PAIRED_NOT_READY
        RfidReaderStatus.NOT_PAIRED -> RfidRowStatus.NOT_PAIRED
        RfidReaderStatus.PERMISSION_NEEDED -> RfidRowStatus.PERMISSION_NEEDED
        RfidReaderStatus.BLUETOOTH_OFF -> RfidRowStatus.BLUETOOTH_OFF
    }

    private fun initialsOf(name: String): String =
        name.split(' ', '·').filter { it.isNotBlank() }.take(2)
            .joinToString("") { it.first().uppercase() }
            .ifBlank { name.take(1).uppercase() }

    private companion object {
        const val OPERATOR_ROLE = "operator"

        val LANGUAGES = linkedMapOf(
            "en" to "English",
            "hi" to "हिन्दी",
            "kn" to "ಕನ್ನಡ",
            "te" to "తెలుగు",
        )
    }
}
