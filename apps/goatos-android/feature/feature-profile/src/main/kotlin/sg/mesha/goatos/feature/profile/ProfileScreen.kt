package sg.mesha.goatos.feature.profile

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.feature.profile.R

// ---------------------------------------------------------------------------
// You / Settings surface (screens.md: v-you, v-rfid, v-alerts).
//
// Per TRD §14 dumb-renderer: this screen RENDERS what bootstrap surfaced. It does
// NOT check role, does NOT decide which settings a principal may see, and does NOT
// know that "leadership gets profile only" — the backend decides that by choosing
// which SettingRow entries to include in ProfileUiState.rows. Every visible label,
// value, and action is a field here, never a client-side literal.
//
// For single-feature (bottom-bar-only) principals this same screen is the home for
// the affordances a nav drawer would otherwise hold — language, RFID reader
// pairing, notifications/alerts, and sign out — because no sidebar/drawer is shown
// for them (screens.md "You / Settings"; nav chrome is backend-driven).
// ---------------------------------------------------------------------------


/** The kind drives both the leading glyph and which ProfileEvent a row emits. */
enum class SettingKind { LANGUAGE, RFID, NOTIFICATIONS, TIMETABLE, SIGN_OUT }

/**
 * RFID reader state as this screen needs it. Module-local (mirrors the app-module
 * `RfidReaderStatus`) so feature-profile does not depend on the app module — the
 * ProfileViewModel maps the real reader status onto this before handing it to the row.
 */
enum class RfidRowStatus { READY, PAIRED_NOT_READY, NOT_PAIRED, PERMISSION_NEEDED, BLUETOOTH_OFF }

/**
 * One backend-surfaced settings entry. Whether a row is present at all is the
 * backend's decision (dumb renderer): the app only renders the list it is given.
 * For RFID rows, [rfidStatus] tracks the reader state for proper localization of
 * status values and subtitles; the Composable uses it to render localized strings.
 */
data class SettingRow(
    val kind: SettingKind,
    val title: String,
    val subtitle: String? = null,
    /** Trailing value text, e.g. "English" or "Chainway R3". */
    val value: String? = null,
    /** Tint the value with the brand accent (e.g. a paired reader). */
    val valueEmphasis: Boolean = false,
    /** When non-null the row renders a Switch (notifications/alerts) instead of a chevron. */
    val toggleOn: Boolean? = null,
    /** RFID reader status (used to generate localized status strings in the Composable). */
    val rfidStatus: RfidRowStatus? = null,
)

/**
 * Everything the You/Settings surface renders. Header labels come from bootstrap;
 * [rows] is the backend-surfaced settings list. A SIGN_OUT row (if present) renders
 * as the bottom sign-out button; all other rows render in the settings card.
 */
// @Immutable: rows: List<SettingRow> otherwise marks this unstable (item 6, perf/stability pass).
@Immutable
data class ProfileUiState(
    val name: String,
    val roleLabel: String,
    val scopeLabel: String,
    val initials: String,
    val screenTitle: String = "You",
    val eyebrow: String = "Mesha",
    val settingsTitle: String = "Settings",
    val rows: List<SettingRow> = emptyList(),
)

sealed interface ProfileEvent {
    data object OpenLanguage : ProfileEvent
    data object PairRfid : ProfileEvent
    data object ToggleNotifications : ProfileEvent
    /** Opens the read-only HRMS Timetable (shift roster) mirror — screens.md/docs/mobile. */
    data object OpenTimetable : ProfileEvent
    data object SignOut : ProfileEvent
}

private fun eventFor(kind: SettingKind): ProfileEvent = when (kind) {
    SettingKind.LANGUAGE -> ProfileEvent.OpenLanguage
    SettingKind.RFID -> ProfileEvent.PairRfid
    SettingKind.NOTIFICATIONS -> ProfileEvent.ToggleNotifications
    SettingKind.TIMETABLE -> ProfileEvent.OpenTimetable
    SettingKind.SIGN_OUT -> ProfileEvent.SignOut
}

private fun iconFor(kind: SettingKind): ImageVector = when (kind) {
    SettingKind.LANGUAGE -> MeshaIcons.Globe
    SettingKind.RFID -> MeshaIcons.Bluetooth
    SettingKind.NOTIFICATIONS -> MeshaIcons.Bell
    SettingKind.TIMETABLE -> MeshaIcons.Clock
    SettingKind.SIGN_OUT -> MeshaIcons.Logout
}

/** Localized title for a setting kind. */
@Composable
private fun titleFor(kind: SettingKind): String = when (kind) {
    SettingKind.LANGUAGE -> stringResource(R.string.profile_setting_language)
    SettingKind.RFID -> stringResource(R.string.profile_setting_rfid)
    SettingKind.NOTIFICATIONS -> stringResource(R.string.profile_setting_rfid)  // Not visible in current design
    SettingKind.TIMETABLE -> stringResource(R.string.profile_setting_timetable)
    SettingKind.SIGN_OUT -> stringResource(R.string.profile_setting_sign_out)
}

/** Localized subtitle for a setting kind (may return null). */
@Composable
private fun subtitleFor(kind: SettingKind): String? = when (kind) {
    SettingKind.LANGUAGE -> null
    SettingKind.RFID -> null
    SettingKind.NOTIFICATIONS -> null
    SettingKind.TIMETABLE -> stringResource(R.string.profile_setting_timetable_subtitle)
    SettingKind.SIGN_OUT -> null
}

/** Localized value and subtitle for RFID reader status. */
@Composable
private fun statusFor(status: RfidRowStatus): Pair<String, String> = when (status) {
    RfidRowStatus.READY ->
        stringResource(R.string.profile_rfid_status_ready) to stringResource(R.string.profile_rfid_status_ready_subtitle)
    RfidRowStatus.PAIRED_NOT_READY ->
        stringResource(R.string.profile_rfid_status_reconnect) to stringResource(R.string.profile_rfid_status_reconnect_subtitle)
    RfidRowStatus.NOT_PAIRED ->
        stringResource(R.string.profile_rfid_status_pair) to stringResource(R.string.profile_rfid_status_pair_subtitle)
    RfidRowStatus.PERMISSION_NEEDED ->
        stringResource(R.string.profile_rfid_status_allow) to stringResource(R.string.profile_rfid_status_allow_subtitle)
    RfidRowStatus.BLUETOOTH_OFF ->
        stringResource(R.string.profile_rfid_status_bluetooth_off) to stringResource(R.string.profile_rfid_status_bluetooth_off_subtitle)
}

@Composable
fun ProfileScreen(
    state: ProfileUiState,
    onEvent: (ProfileEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    val settingRows = state.rows.filter { it.kind != SettingKind.SIGN_OUT }
    val signOut = state.rows.firstOrNull { it.kind == SettingKind.SIGN_OUT }
    val localizedSettingsTitle = stringResource(R.string.profile_settings_label)

    LazyColumn(
        modifier = modifier
            .fillMaxSize()
            .background(MeshaColors.Bg),
        contentPadding = PaddingValues(bottom = 24.dp),
    ) {
        item { ProfileHeader(state) }
        item {
            Text(
                text = localizedSettingsTitle.uppercase(),
                color = MeshaColors.Faint,
                fontSize = 11.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 14.dp, bottom = 6.dp),
            )
        }
        if (settingRows.isNotEmpty()) {
            item { SettingsCard(rows = settingRows, onEvent = onEvent) }
        }
        signOut?.let { row ->
            item { SignOutButton(row = row, onEvent = onEvent) }
        }
    }
}

@Composable
private fun ProfileHeader(state: ProfileUiState) {
    val localizedScreenTitle = stringResource(R.string.profile_screen_title)
    Column(modifier = Modifier.padding(top = 16.dp)) {
        // "You" is a global L0 tab present in every module's bar, so it carries the drawer too —
        // resolved by the shell, not asserted here.
        MeshaScreenHeader(
            title = localizedScreenTitle,
            eyebrow = state.roleLabel.uppercase(),
            contentPadding = PaddingValues(horizontal = 16.dp),
        )
        Row(
            verticalAlignment = Alignment.CenterVertically,
            modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 14.dp, bottom = 4.dp),
        ) {
            Box(
                modifier = Modifier
                    .size(48.dp)
                    .background(MeshaColors.BrandGradient, shape = androidx.compose.foundation.shape.RoundedCornerShape(15.dp)),
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    text = state.initials,
                    color = MeshaColors.OnBrand,
                    fontSize = 18.sp,
                    fontWeight = FontWeight.W800,
                )
            }
            Spacer(Modifier.width(12.dp))
            Column {
                Text(
                    text = state.name,
                    color = MeshaColors.Ink,
                    fontSize = 16.sp,
                    fontWeight = FontWeight.W700,
                )
                Text(
                    text = state.scopeLabel,
                    color = MeshaColors.Muted,
                    fontSize = 12.sp,
                )
            }
        }
    }
}

@Composable
private fun SettingsCard(rows: List<SettingRow>, onEvent: (ProfileEvent) -> Unit) {
    Column(
        modifier = Modifier
            .padding(horizontal = 16.dp)
            .fillMaxWidth()
            .background(MeshaColors.Surf, shape = androidx.compose.foundation.shape.RoundedCornerShape(16.dp))
            .border(1.dp, MeshaColors.Hair, shape = androidx.compose.foundation.shape.RoundedCornerShape(16.dp))
            .padding(horizontal = 15.dp),
    ) {
        rows.forEachIndexed { index, row ->
            SettingRowItem(row = row, onEvent = onEvent)
            if (index != rows.lastIndex) {
                HorizontalDivider(thickness = 1.dp, color = MeshaColors.Surf2)
            }
        }
    }
}

@Composable
private fun SettingRowItem(row: SettingRow, onEvent: (ProfileEvent) -> Unit) {
    val localizedTitle = titleFor(row.kind)
    val localizedSubtitle = subtitleFor(row.kind) ?: row.subtitle

    // For RFID rows with a status enum, derive localized strings from it
    val displayValue: String?
    val displaySubtitle: String?
    if (row.kind == SettingKind.RFID && row.rfidStatus != null) {
        val (value, subtitle) = statusFor(row.rfidStatus)
        displayValue = value
        displaySubtitle = subtitle
    } else {
        displayValue = row.value
        displaySubtitle = localizedSubtitle
    }

    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(min = 48.dp)
            .clickable { onEvent(eventFor(row.kind)) }
            .padding(vertical = 11.dp),
    ) {
        Icon(
            imageVector = iconFor(row.kind),
            contentDescription = null,
            tint = MeshaColors.Muted,
            modifier = Modifier.size(18.dp),
        )
        Spacer(Modifier.width(9.dp))
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = localizedTitle,
                color = MeshaColors.Muted,
                fontSize = 13.sp,
            )
            displaySubtitle?.let {
                Text(text = it, color = MeshaColors.Faint, fontSize = 11.sp)
            }
        }
        Spacer(Modifier.width(10.dp))
        SettingRowTrailing(row = row.copy(value = displayValue), onEvent = onEvent)
    }
}

@Composable
private fun SettingRowTrailing(row: SettingRow, onEvent: (ProfileEvent) -> Unit) {
    if (row.toggleOn != null) {
        Switch(
            checked = row.toggleOn,
            onCheckedChange = { onEvent(eventFor(row.kind)) },
            colors = SwitchDefaults.colors(
                checkedThumbColor = MeshaColors.OnBrand,
                checkedTrackColor = MeshaColors.Brand,
                uncheckedThumbColor = MeshaColors.Muted,
                uncheckedTrackColor = MeshaColors.Surf2,
                uncheckedBorderColor = MeshaColors.Hair,
            ),
        )
        return
    }
    Row(verticalAlignment = Alignment.CenterVertically) {
        row.value?.let {
            Text(
                text = it,
                color = if (row.valueEmphasis) MeshaColors.BrandD else MeshaColors.Muted,
                fontSize = 13.sp,
                fontWeight = FontWeight.W700,
            )
            Spacer(Modifier.width(6.dp))
        }
        Text(text = "›", color = MeshaColors.Faint, fontSize = 16.sp)
    }
}

@Composable
private fun SignOutButton(row: SettingRow, onEvent: (ProfileEvent) -> Unit) {
    val localizedTitle = titleFor(row.kind)
    Row(
        horizontalArrangement = Arrangement.Center,
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .padding(start = 16.dp, end = 16.dp, top = 16.dp)
            .fillMaxWidth()
            .heightIn(min = 52.dp)
            .background(MeshaColors.Surf2, shape = androidx.compose.foundation.shape.RoundedCornerShape(15.dp))
            .border(1.dp, MeshaColors.Hair, shape = androidx.compose.foundation.shape.RoundedCornerShape(15.dp))
            .clickable { onEvent(ProfileEvent.SignOut) }
            .padding(15.dp),
    ) {
        Icon(
            imageVector = iconFor(SettingKind.SIGN_OUT),
            contentDescription = null,
            tint = MeshaColors.Danger,
            modifier = Modifier.size(18.dp),
        )
        Spacer(Modifier.width(9.dp))
        Text(
            text = localizedTitle,
            color = MeshaColors.Danger,
            fontSize = 15.sp,
            fontWeight = FontWeight.W700,
            textAlign = TextAlign.Center,
            fontFamily = FontFamily.Default,
        )
    }
}

@Preview(backgroundColor = 0xFF0B100D, showBackground = true)
@Composable
private fun ProfileScreenPreview() {
    GoatOsTheme {
        ProfileScreen(
            state = ProfileUiState(
                name = "Arun Kumar",
                roleLabel = "Health Asst Mgr",
                scopeLabel = "Health Asst Mgr · CBE",
                initials = "AK",
                rows = listOf(
                    SettingRow(
                        kind = SettingKind.LANGUAGE,
                        title = "Language",
                        value = "English",
                    ),
                    SettingRow(
                        kind = SettingKind.RFID,
                        title = "RFID reader",
                        value = "Chainway R3",
                        valueEmphasis = true,
                    ),
                    SettingRow(
                        kind = SettingKind.SIGN_OUT,
                        title = "Sign out",
                    ),
                ),
            ),
        )
    }
}
