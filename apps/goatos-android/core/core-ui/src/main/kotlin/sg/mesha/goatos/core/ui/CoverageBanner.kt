package sg.mesha.goatos.core.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/**
 * HRMS coverage banner (docs/hr/roster-rbac-design.md S4.6/S4.8) for a landing screen
 * (today: Calendar; future: Drives). Shown when the principal currently holds a
 * temporary coverage window — an ad-hoc-leave replacement or a week-off backup fill-in
 * for another position's due work.
 *
 * Per TRD §14 dumb-renderer, [CoverageBannerUiState.text] is the ONLY field: a
 * ViewModel decides who is covering, until when, and the exact wording from the
 * backend's `/admin/roster/vaccination-owner` + `/admin/roster/leave` reads — this
 * composable only renders it, the same way a StatusPill renders a tone. A null [state]
 * (or blank text) renders nothing, so callers can pass it unconditionally.
 */
@Immutable
data class CoverageBannerUiState(val text: String)

@Composable
fun CoverageBanner(state: CoverageBannerUiState?, modifier: Modifier = Modifier) {
    if (state == null || state.text.isBlank()) return
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = modifier
            .fillMaxWidth()
            .background(MeshaColors.WarnX, shape = RoundedCornerShape(MeshaDimens.radiusCard))
            .padding(horizontal = MeshaDimens.space4, vertical = MeshaDimens.space3),
    ) {
        Icon(
            imageVector = MeshaIcons.Clock,
            contentDescription = null,
            tint = MeshaColors.Warn,
            modifier = Modifier.size(MeshaDimens.iconSm),
        )
        Spacer(Modifier.width(MeshaDimens.space2))
        Text(text = state.text, style = MeshaType.cardSubtitle, color = MeshaColors.Warn)
    }
}
