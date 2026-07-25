package sg.mesha.goatos.core.ui

import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LocalContentColor
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors

/**
 * Shared sync/refresh icon button for all read screens.
 *
 * When [isSyncing] is true:
 * - The icon continuously rotates (360° animation loop)
 * - The button is disabled, so taps are ignored (no duplicate refresh)
 *
 * When [isSyncing] is false:
 * - The icon is static
 * - The button is clickable and calls [onSync]
 *
 * This ensures consistent refresh UX across Calendar, Sheds, Counts, Approval,
 * Verify, Timetable, Leadership, and Alerts screens.
 * Part of the offline-first pattern (docs/decisions/android-offline-first.md):
 * the screen shows cached data, refresh runs in background with in-flight indication.
 */
@Composable
fun SyncIconButton(
    isSyncing: Boolean,
    onSync: () -> Unit,
    modifier: Modifier = Modifier,
    contentDescription: String? = "Refresh",
) {
    IconButton(
        onClick = onSync,
        enabled = !isSyncing,
        modifier = modifier,
    ) {
        if (isSyncing) {
            SpinningRefreshIcon(contentDescription = contentDescription)
        } else {
            Icon(
                imageVector = MeshaIcons.Refresh,
                contentDescription = contentDescription,
                tint = MeshaColors.Muted,
            )
        }
    }
}

/**
 * Spinning refresh icon shown when a sync is in flight.
 * Rotates continuously from 0° to 360° and repeats indefinitely.
 */
@Composable
private fun SpinningRefreshIcon(contentDescription: String?) {
    val infiniteTransition = rememberInfiniteTransition(label = "refresh_spin")
    val rotation by infiniteTransition.animateFloat(
        initialValue = 0f,
        targetValue = 360f,
        animationSpec = infiniteRepeatable(
            animation = tween(durationMillis = 1000, delayMillis = 0),
        ),
        label = "rotation",
    )

    Icon(
        imageVector = MeshaIcons.Refresh,
        contentDescription = contentDescription,
        tint = MeshaColors.Muted,
        modifier = Modifier.rotate(rotation),
    )
}
