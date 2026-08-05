package sg.mesha.goatos.core.ui

import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.ui.Alignment
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.theme.MeshaColors

/** Shared cold-load treatment for every mobile surface. Cached rows remain visible during refresh;
 * this shimmer is only for the first Room/cache load, so refresh never replaces useful data. */
@Composable
fun LoadingSkeletonList(
    modifier: Modifier = Modifier,
    rows: Int = 3,
    shape: SkeletonShape = SkeletonShape.BLOCK,
    contentPadding: PaddingValues = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
) {
    val transition = rememberInfiniteTransition(label = "skeleton")
    val travel by transition.animateFloat(
        initialValue = -500f,
        targetValue = 1_200f,
        animationSpec = infiniteRepeatable(
            animation = tween(durationMillis = 1_100),
            repeatMode = RepeatMode.Restart,
        ),
        label = "skeleton-travel",
    )
    val brush = Brush.linearGradient(
        colors = listOf(MeshaColors.Surf3, MeshaColors.Hair, MeshaColors.Surf3),
        start = Offset(travel, 0f),
        end = Offset(travel + 420f, 220f),
    )
    Column(
        modifier = modifier
            .fillMaxWidth()
            .padding(contentPadding),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        repeat(rows.coerceIn(1, 6)) {
            when (shape) {
                SkeletonShape.BLOCK -> Box(
                    Modifier
                        .fillMaxWidth()
                        .height(82.dp)
                        .background(brush, RoundedCornerShape(16.dp)),
                )
                // A card that ACTUALLY looks like the card replacing it: leading media square,
                // a long title line, a shorter meta line, and a trailing status pill. A row of
                // identical featureless blocks tells the reader nothing about what is coming and
                // makes every screen's cold load look the same.
                SkeletonShape.MEDIA_CARD -> Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .background(MeshaColors.Surf, RoundedCornerShape(16.dp))
                        .padding(12.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    Box(
                        Modifier
                            .size(44.dp)
                            .background(brush, RoundedCornerShape(12.dp)),
                    )
                    Column(
                        modifier = Modifier.weight(1f),
                        verticalArrangement = Arrangement.spacedBy(7.dp),
                    ) {
                        Box(
                            Modifier
                                .fillMaxWidth(0.72f)
                                .height(13.dp)
                                .background(brush, RoundedCornerShape(6.dp)),
                        )
                        Box(
                            Modifier
                                .fillMaxWidth(0.45f)
                                .height(11.dp)
                                .background(brush, RoundedCornerShape(6.dp)),
                        )
                    }
                    Box(
                        Modifier
                            .width(58.dp)
                            .height(22.dp)
                            .background(brush, RoundedCornerShape(999.dp)),
                    )
                }
            }
        }
    }
}

/** What the cold-load placeholder should be shaped like, so it previews the real content. */
enum class SkeletonShape {
    /** Featureless block. For surfaces whose content is not a card list. */
    BLOCK,

    /** Media square + title + meta + status pill: the queue/shed card shape. */
    MEDIA_CARD,
}
