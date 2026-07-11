package sg.mesha.goatos.core.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors

// Shared, stateless UI primitives for feature-* screens.

/** Colour intent for an [EmptyState] icon badge: a neutral zero state, a positive
 *  "all done / caught up" state, or a warning "nothing here but you may want to act" state. */
enum class EmptyTone { Neutral, Positive, Warn }

/**
 * The single shared empty / zero-data state for every screen (calendar, sheds, scan, alerts,
 * overdue, leadership, record, timetable). A calm, centered card — a soft icon badge, a title,
 * an optional one-line subtitle, and an optional action — NOT a bare line of grey text.
 *
 * "Empty" means an honest zero state (no drives due, all caught up, nothing overdue). It is
 * distinct from the offline/stale signal, which [SyncStatusIndicator] carries separately, and
 * from a hard load error — pass [EmptyTone.Warn] + a retry [action] for the "couldn't load,
 * no cache yet" case so the screen still reads as a real state, never a blank wall.
 */
@Composable
fun EmptyState(
    title: String,
    modifier: Modifier = Modifier,
    subtitle: String? = null,
    icon: ImageVector = MeshaIcons.Check,
    tone: EmptyTone = EmptyTone.Neutral,
    action: (@Composable () -> Unit)? = null,
) {
    val (badgeBg, iconTint) = when (tone) {
        EmptyTone.Neutral -> MeshaColors.Surf2 to MeshaColors.Muted
        EmptyTone.Positive -> MeshaColors.OkX to MeshaColors.Ok
        EmptyTone.Warn -> MeshaColors.WarnX to MeshaColors.Warn
    }
    Column(
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = 24.dp, vertical = 40.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Box(
            modifier = Modifier
                .size(64.dp)
                .clip(RoundedCornerShape(20.dp))
                .background(badgeBg),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = icon,
                contentDescription = null,
                tint = iconTint,
                modifier = Modifier.size(28.dp),
            )
        }
        Text(
            text = title,
            color = MeshaColors.Ink,
            fontSize = 15.sp,
            fontWeight = FontWeight.W700,
            textAlign = TextAlign.Center,
        )
        if (!subtitle.isNullOrBlank()) {
            Text(
                text = subtitle,
                color = MeshaColors.Muted,
                fontSize = 12.5.sp,
                fontWeight = FontWeight.W500,
                textAlign = TextAlign.Center,
            )
        }
        action?.let {
            Spacer(Modifier.size(4.dp))
            it()
        }
    }
}
