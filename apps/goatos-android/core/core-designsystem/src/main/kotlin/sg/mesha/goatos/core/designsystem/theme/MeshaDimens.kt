package sg.mesha.goatos.core.designsystem.theme

import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.ui.unit.dp

/**
 * The single source of spacing, radii, and sizing — ported from the mock's layout metrics.
 * Screens MUST use these named tokens instead of raw `dp` literals so the whole app shares
 * one spacing rhythm and corner language.
 */
object MeshaDimens {
    // Screen gutter (mock `.pad{padding:0 16px}`) + first-content top rhythm.
    val gutter = 16.dp
    val screenTop = 16.dp

    // Vertical rhythm scale (mock spacing steps).
    val space1 = 4.dp
    val space2 = 8.dp
    val space3 = 12.dp
    val space4 = 14.dp
    val space5 = 16.dp
    val space6 = 22.dp
    val space7 = 26.dp
    val space8 = 30.dp

    // Corner radii (mock component radii).
    val radiusSmall = 8.dp     // seg buttons
    val radiusSeg = 11.dp      // seg container
    val radiusIcon = 12.dp     // icon buttons, small chips
    val radiusInput = 14.dp    // .inp, .langbtn
    val radiusButton = 15.dp   // .btn
    val radiusCard = 18.dp     // .card
    val radiusSheet = 20.dp    // bottom sheets
    val radiusHero = 24.dp     // hero tiles

    // Sizes.
    val hairline = 1.dp
    val iconButton = 38.dp     // .vhead .ib/.bk, .avatar
    val glyphTile = 26.dp      // .langbtn .gl, .conn .bt
    val avatarLarge = 48.dp    // You header avatar
    val minTapRow = 50.dp      // .inp / row min-height
    val minTapButton = 52.dp   // .btn min-height
    val iconSm = 15.dp
    val iconMd = 18.dp

    // Pill shape helper.
    val pill = RoundedCornerShape(999.dp)
}
