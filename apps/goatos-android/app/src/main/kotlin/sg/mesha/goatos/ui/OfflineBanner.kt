package sg.mesha.goatos.ui

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.expandVertically
import androidx.compose.animation.shrinkVertically
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.res.stringResource
import sg.mesha.goatos.core.designsystem.R as DesignSystemR
import sg.mesha.goatos.core.designsystem.theme.MeshaColors

/**
 * Passive, non-blocking offline notice pinned above the screen content. It appears (animated) only
 * after connectivity has been lost for a grace period (debounced in `SyncStatusViewModel`), auto-
 * hides the instant the device is back online, and can be dismissed for the current offline stretch.
 * Tapping the bar opens the full sync sheet.
 *
 * It NEVER blocks input — capture/submit keep working offline; the outbox queues durably and drains
 * on reconnect. Soft amber tone (not error red): being offline is expected in the field, not a fault.
 */
@Composable
fun OfflineBanner(
    visible: Boolean,
    onOpenDetails: () -> Unit,
    modifier: Modifier = Modifier,
) {
    var dismissed by remember { mutableStateOf(false) }
    // Each time connectivity returns, clear the dismissal so the NEXT offline stretch shows again.
    LaunchedEffect(visible) { if (!visible) dismissed = false }

    AnimatedVisibility(
        visible = visible && !dismissed,
        enter = expandVertically(expandFrom = Alignment.Top) + fadeIn(),
        exit = shrinkVertically(shrinkTowards = Alignment.Top) + fadeOut(),
        modifier = modifier,
    ) {
        Row(
            Modifier
                .fillMaxWidth()
                .padding(horizontal = 12.dp, vertical = 6.dp)
                .clip(RoundedCornerShape(12.dp))
                .background(MeshaColors.WarnX)
                .clickable(onClick = onOpenDetails)
                .padding(horizontal = 12.dp, vertical = 9.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Box(Modifier.size(8.dp).clip(CircleShape).background(MeshaColors.Warn))
            Column(Modifier.weight(1f)) {
                Text(stringResource(DesignSystemR.string.offline_title), color = MeshaColors.Warn, fontSize = 12.5.sp, fontWeight = FontWeight.W700)
                Text(
                    stringResource(DesignSystemR.string.offline_subtitle),
                    color = MeshaColors.Muted,
                    fontSize = 11.sp,
                    lineHeight = 15.sp,
                )
            }
            Text(stringResource(DesignSystemR.string.offline_details), color = MeshaColors.BrandD, fontSize = 11.5.sp, fontWeight = FontWeight.W700)
            Text(
                "✕",
                color = MeshaColors.Faint,
                fontSize = 13.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier
                    .clip(CircleShape)
                    .clickable { dismissed = true }
                    .padding(horizontal = 6.dp, vertical = 2.dp),
            )
        }
    }
}
