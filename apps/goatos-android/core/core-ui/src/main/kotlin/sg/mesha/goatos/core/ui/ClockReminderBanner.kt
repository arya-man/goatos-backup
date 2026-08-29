package sg.mesha.goatos.core.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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
import androidx.compose.ui.draw.clip
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/**
 * Shell-global "you haven't clocked in today" reminder (module clock, maintainer decision
 * 2026-08-27 — docs/features/clock-in-out/plan.md §4.3). The marriage of the two settled
 * patterns: OfflineBanner's shell-global placement, [CoverageBanner]'s backend-owned-copy dumb
 * renderer. [ClockReminderBannerUiState.text] is the ONLY content field — the backend's
 * `banner_text` from `GET /app/clock/status`, rendered VERBATIM (empty means no banner and the
 * ViewModel passes null). A tap navigates to the clock module; it is a reminder, never a lock.
 */
@Immutable
data class ClockReminderBannerUiState(val text: String)

@Composable
fun ClockReminderBanner(
    state: ClockReminderBannerUiState?,
    onTap: () -> Unit,
    modifier: Modifier = Modifier,
) {
    if (state == null || state.text.isBlank()) return
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = MeshaDimens.space4, vertical = MeshaDimens.space2)
            .clip(RoundedCornerShape(MeshaDimens.radiusCard))
            .background(MeshaColors.WarnX)
            .clickable(onClick = onTap)
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
