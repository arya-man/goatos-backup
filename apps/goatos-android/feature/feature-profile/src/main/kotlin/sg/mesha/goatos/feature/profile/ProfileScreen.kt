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
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors

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
enum class SettingKind { LANGUAGE, RFID, NOTIFICATIONS, SIGN_OUT }

/**
 * One backend-surfaced settings entry. Whether a row is present at all is the
 * backend's decision (dumb renderer): the app only renders the list it is given.
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
)

/**
 * Everything the You/Settings surface renders. Header labels come from bootstrap;
 * [rows] is the backend-surfaced settings list. A SIGN_OUT row (if present) renders
 * as the bottom sign-out button; all other rows render in the settings card.
 */
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
    data object SignOut : ProfileEvent
}

private fun eventFor(kind: SettingKind): ProfileEvent = when (kind) {
    SettingKind.LANGUAGE -> ProfileEvent.OpenLanguage
    SettingKind.RFID -> ProfileEvent.PairRfid
    SettingKind.NOTIFICATIONS -> ProfileEvent.ToggleNotifications
    SettingKind.SIGN_OUT -> ProfileEvent.SignOut
}

private fun iconFor(kind: SettingKind): ImageVector = when (kind) {
    SettingKind.LANGUAGE -> MeshaIcons.Globe
    SettingKind.RFID -> MeshaIcons.Bluetooth
    SettingKind.NOTIFICATIONS -> MeshaIcons.Bell
    SettingKind.SIGN_OUT -> MeshaIcons.Logout
}

@Composable
fun ProfileScreen(
    state: ProfileUiState,
    onEvent: (ProfileEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    val settingRows = state.rows.filter { it.kind != SettingKind.SIGN_OUT }
    val signOut = state.rows.firstOrNull { it.kind == SettingKind.SIGN_OUT }

    LazyColumn(
        modifier = modifier
            .fillMaxSize()
            .background(MeshaColors.Bg),
        contentPadding = PaddingValues(bottom = 24.dp),
    ) {
        item { ProfileHeader(state) }
        item {
            Text(
                text = state.settingsTitle.uppercase(),
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
    Column(modifier = Modifier.padding(top = 16.dp)) {
        Column(modifier = Modifier.padding(horizontal = 16.dp)) {
            Text(
                text = state.roleLabel.uppercase(),
                color = MeshaColors.Faint,
                fontSize = 10.5.sp,
                fontWeight = FontWeight.W700,
            )
            Text(
                text = state.screenTitle,
                color = MeshaColors.Ink,
                fontSize = 22.sp,
                fontWeight = FontWeight.W700,
            )
        }
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
                text = row.title,
                color = MeshaColors.Muted,
                fontSize = 13.sp,
            )
            row.subtitle?.let {
                Text(text = it, color = MeshaColors.Faint, fontSize = 11.sp)
            }
        }
        Spacer(Modifier.width(10.dp))
        SettingRowTrailing(row = row, onEvent = onEvent)
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
            text = row.title,
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
                        kind = SettingKind.NOTIFICATIONS,
                        title = "Notifications",
                        subtitle = "Drive reminders · overdue alerts",
                        toggleOn = true,
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
