package sg.mesha.goatos.core.designsystem.theme

import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color

/**
 * The exact dark field-theme tokens from the source-of-truth mock
 * (`mock/vaccination-mobile-mock.html` :root). The app shell renders through these
 * directly — NOT Material's default color scheme — so chrome matches the mock.
 */
object MeshaColors {
    val PageBg = Color(0xFF0A0F0C)
    val Bg = Color(0xFF0B100D)
    val Surf = Color(0xFF131A15)
    val Surf2 = Color(0xFF1A241D)
    val Surf3 = Color(0xFF222E25)
    val Hair = Color(0xFF28352B)
    val Ink = Color(0xFFECF4EE)
    val Muted = Color(0xFF8FA497)
    val Faint = Color(0xFF5F7367)
    val Brand = Color(0xFF8AD457)
    val Brand2 = Color(0xFF5FB531)
    val BrandD = Color(0xFFB7EA8C)
    val OnBrand = Color(0xFF08130B)
    val Warn = Color(0xFFF0B54B)
    val Danger = Color(0xFFFB6F63)
    val Ok = Color(0xFF8AD457)

    val BrandGradient: Brush = Brush.linearGradient(listOf(Color(0xFF93DA5E), Color(0xFF5FB531)))
}
