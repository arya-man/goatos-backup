package sg.mesha.goatos.feature.toxin

// telemetry:exempt pure stateless renderers; the @HiltViewModels in :app own the toxin_*
// AnalyticsEventsToxin + CrashReporter wiring.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/** Backend-composed status chip, rendered VERBATIM. Blank copy renders nothing at all. */
@Composable
internal fun ToxinStatusChip(label: String, modifier: Modifier = Modifier) {
    if (label.isBlank()) return
    Box(
        modifier = modifier
            .clip(RoundedCornerShape(8.dp))
            .background(MeshaColors.Surf2)
            .padding(horizontal = 8.dp, vertical = 3.dp),
    ) {
        Text(text = label, color = MeshaColors.Muted, style = MeshaType.pillStrong)
    }
}

/** Card surface shared by the toxin list rows and the guided step rows. */
@Composable
internal fun toxinCardModifier(enabled: Boolean, onClick: (() -> Unit)?): Modifier {
    var base = Modifier
        .fillMaxWidth()
        .padding(horizontal = 16.dp)
        .clip(RoundedCornerShape(16.dp))
        .background(MeshaColors.Surf)
        .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
    if (onClick != null) {
        base = base.clickable(enabled = enabled, onClick = onClick)
    }
    return base.padding(14.dp)
}

/** Primary action. A disabled action renders as a dead button, never hidden — an operator must be
 *  able to see that the step exists and is simply not open yet. */
@Composable
internal fun ToxinPrimaryButton(
    label: String,
    enabled: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Box(
        modifier = modifier
            .heightIn(min = 48.dp)
            .clip(RoundedCornerShape(14.dp))
            .background(if (enabled) MeshaColors.Brand else MeshaColors.Surf3)
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .padding(horizontal = 16.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = label,
            color = if (enabled) MeshaColors.OnBrand else MeshaColors.Faint,
            style = MeshaType.button,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

/** Secondary (ghost) action, matching the other module surfaces. */
@Composable
internal fun ToxinGhostButton(
    label: String,
    enabled: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Box(
        modifier = modifier
            .heightIn(min = 48.dp)
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf2)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(14.dp))
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .padding(horizontal = 12.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = label,
            color = if (enabled) MeshaColors.Ink else MeshaColors.Faint,
            style = MeshaType.button,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

/**
 * The remaining wait on a `waiting` step, as bare clock digits (`h:mm:ss` / `mm:ss`).
 *
 * DISPLAY ONLY. It is derived from the SERVER's `available_at` against [nowMs] purely so the
 * operator can see how long is left; the step's action stays disabled until the SERVER says
 * `available`, and the server re-checks on every write. A device clock that runs fast therefore
 * shortens this text and unlocks nothing.
 *
 * Returns blank when there is no server instant, or once the countdown has run out — at that
 * point the honest thing to show is nothing, because only the next refresh can say whether the
 * server agrees the step is open.
 */
internal fun toxinCountdownDigits(availableAtEpochMs: Long, nowMs: Long): String {
    if (availableAtEpochMs <= 0L) return ""
    val remainingSeconds = (availableAtEpochMs - nowMs) / 1000L
    if (remainingSeconds <= 0L) return ""
    val hours = remainingSeconds / 3600L
    val minutes = (remainingSeconds % 3600L) / 60L
    val seconds = remainingSeconds % 60L
    return if (hours > 0L) {
        "%d:%02d:%02d".format(hours, minutes, seconds)
    } else {
        "%d:%02d".format(minutes, seconds)
    }
}
