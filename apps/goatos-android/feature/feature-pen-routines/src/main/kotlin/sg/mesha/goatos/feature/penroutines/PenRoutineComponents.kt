package sg.mesha.goatos.feature.penroutines

// telemetry:exempt pure stateless renderers; the @HiltViewModels in :app own the
// pen_routine_* AnalyticsEventsPenRoutines + CrashReporter wiring.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/** Backend-composed state chip, rendered VERBATIM. Blank copy renders nothing at all. */
@Composable
internal fun PenRoutineStateChip(label: String, tone: PenRoutineTone, modifier: Modifier = Modifier) {
    if (label.isBlank()) return
    val accent = penRoutineToneAccent(tone)
    Box(
        modifier = modifier
            .clip(RoundedCornerShape(8.dp))
            .background(accent.copy(alpha = 0.16f))
            .padding(horizontal = 8.dp, vertical = 3.dp),
    ) {
        Text(text = label, color = accent, style = MeshaType.pillStrong)
    }
}

/** Card surface shared by the list rows and the detail sections — the Leadership Tasks card. */
@Composable
internal fun penRoutineCardModifier(enabled: Boolean = true, onClick: (() -> Unit)? = null): Modifier {
    var base = Modifier
        .fillMaxWidth()
        .padding(horizontal = 16.dp)
        .clip(RoundedCornerShape(16.dp))
        .background(MeshaColors.Surf)
        .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
    if (onClick != null) {
        base = base.clickable(enabled = enabled, onClick = onClick)
    }
    return base.padding(16.dp)
}

/** Primary (filled) action. Disabled renders dead, never hidden. */
@Composable
internal fun PenRoutinePrimaryButton(
    label: String,
    enabled: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    icon: ImageVector? = null,
) {
    Row(
        modifier = modifier
            .heightIn(min = 48.dp)
            .clip(RoundedCornerShape(14.dp))
            .background(if (enabled) MeshaColors.Brand else MeshaColors.Surf3)
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .padding(horizontal = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.CenterHorizontally),
    ) {
        val tint = if (enabled) MeshaColors.OnBrand else MeshaColors.Faint
        if (icon != null) {
            Icon(imageVector = icon, contentDescription = null, tint = tint, modifier = Modifier.size(18.dp))
        }
        Text(text = label, color = tint, style = MeshaType.button, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** Secondary (ghost) action, used for the re-record after a failure. */
@Composable
internal fun PenRoutineGhostButton(
    label: String,
    enabled: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    icon: ImageVector? = null,
) {
    val tint = if (enabled) MeshaColors.Ink else MeshaColors.Faint
    Row(
        modifier = modifier
            .heightIn(min = 48.dp)
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf2)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(14.dp))
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .padding(horizontal = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.CenterHorizontally),
    ) {
        if (icon != null) {
            Icon(imageVector = icon, contentDescription = null, tint = tint, modifier = Modifier.size(18.dp))
        }
        Text(text = label, color = tint, style = MeshaType.button, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** A centered small spinner beside an in-flight line. */
@Composable
internal fun PenRoutineInlineSpinner(modifier: Modifier = Modifier) {
    CircularProgressIndicator(color = MeshaColors.BrandD, strokeWidth = 2.dp, modifier = modifier.size(18.dp))
}

internal fun penRoutineToneAccent(tone: PenRoutineTone): Color = when (tone) {
    PenRoutineTone.INFO -> MeshaColors.Info
    PenRoutineTone.REVIEW -> MeshaColors.Warn
    PenRoutineTone.DANGER -> MeshaColors.Danger
    PenRoutineTone.SUCCESS -> MeshaColors.Ok
    PenRoutineTone.MUTED -> MeshaColors.Muted
}
