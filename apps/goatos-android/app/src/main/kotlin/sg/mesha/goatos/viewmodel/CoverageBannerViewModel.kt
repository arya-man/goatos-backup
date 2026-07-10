package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.data.RosterRepository
import sg.mesha.goatos.core.ui.CoverageBannerUiState
import java.time.LocalDate
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.TextStyle
import java.util.Locale
import javax.inject.Inject

private val KOLKATA: ZoneId = ZoneId.of("Asia/Kolkata")

/**
 * HRMS coverage banner (docs/hr/roster-rbac-design.md S4.6/S4.8) for a landing screen
 * (today: Calendar). Shows "Covering Vaccination until <date>" ONLY when the resolved
 * `GET /admin/roster/vaccination-owner` owner for the principal's HR-center scope on
 * today's date IS this principal (an id-equality check, not a client-side decision —
 * TRD §14 dumb-renderer), via an ad-hoc-leave replacement or week-off backup fill-in.
 * The "until" date comes from the matching approved `GET /admin/roster/leave` row's
 * `ends_at`, never fabricated.
 *
 * OPEN QUESTION (handoff): `GET /app/bootstrap` does not expose the signed-in
 * principal's HR `workforce_member_id`, nor an HR-center `scope_id` for the roster
 * reads above — `BootstrapActorDto`/`BootstrapOperatorProfileDto` carry `actor_id` /
 * `operator_id` / `primary_location`, which the roster design doc treats as a distinct
 * identity/scope space from `workforce_members` (docs/hr/roster-rbac-design.md §1: HR
 * roster identity vs. the existing `user_scope_grants`/device-operator identity are
 * separate tables with no documented 1:1 bridge). Guessing that bridge would risk
 * showing the banner to the wrong person, so [load] is only callable with an explicit
 * [principalWorkforceMemberId] + roster [scopeId] — until the backend adds that bridge
 * (or ships a dedicated `/app/roster/my-coverage` endpoint returning the ready banner
 * text server-side, which would be the more TRD-§14-faithful design), no caller in this
 * app has a safe value to pass, so the banner stays hidden (never wrong, never guessed).
 */
@HiltViewModel
class CoverageBannerViewModel @Inject constructor(
    private val repo: RosterRepository,
) : ViewModel() {

    private val _state = MutableStateFlow<CoverageBannerUiState?>(null)
    val state: StateFlow<CoverageBannerUiState?> = _state.asStateFlow()

    /**
     * Resolves the banner for [scopeType]/[scopeId] on [date] (default: today, India
     * business calendar) — shows it only when the resolved owner is
     * [principalWorkforceMemberId]. See the class KDoc for why no current call site in
     * this app has real values to pass yet.
     */
    fun load(
        scopeType: String,
        scopeId: String,
        principalWorkforceMemberId: String,
        date: LocalDate = LocalDate.now(KOLKATA),
    ) = viewModelScope.launch {
        val owner = runCatching { repo.vaccinationOwner(scopeType, scopeId, date.toString()).owner }.getOrNull()
        val isCoveringMe = owner != null &&
            owner.ownerWorkforceMemberId == principalWorkforceMemberId &&
            (owner.ownerSource == "replacement" || owner.ownerSource == "week_off_backup")
        if (!isCoveringMe) {
            _state.value = null
            return@launch
        }
        val untilLabel = resolveCoverageEndLabel(scopeType, scopeId, principalWorkforceMemberId)
        _state.value = CoverageBannerUiState(
            text = if (untilLabel != null) "Covering Vaccination until $untilLabel" else "Covering Vaccination today",
        )
    }

    private suspend fun resolveCoverageEndLabel(
        scopeType: String,
        scopeId: String,
        principalWorkforceMemberId: String,
    ): String? {
        val leaves = runCatching {
            repo.leave(scopeType = scopeType, scopeId = scopeId, status = "approved").items
        }.getOrElse { emptyList() }
        val today = LocalDate.now(KOLKATA)
        val ends = leaves
            .filter { it.replacementMemberId == principalWorkforceMemberId }
            .mapNotNull { parseLocalDate(it.endsAt) }
            .filter { !it.isBefore(today) }
            .minOrNull()
            ?: return null
        return "${ends.dayOfWeek.getDisplayName(TextStyle.SHORT, Locale.ENGLISH)} " +
            "${ends.dayOfMonth} ${ends.month.getDisplayName(TextStyle.SHORT, Locale.ENGLISH)}"
    }
}

private fun parseLocalDate(value: String): LocalDate? =
    runCatching { OffsetDateTime.parse(value).atZoneSameInstant(KOLKATA).toLocalDate() }.getOrNull()
