package sg.mesha.goatos.core.designsystem.theme

import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color

/**
 * The single source of colour truth — the exact dark field tokens from the mock
 * (`mock/vaccination-mobile-mock.html` :root). Screens MUST read from here; no screen
 * may declare its own hex literals or a local `*Tokens` object. Dark-only by design.
 */
object MeshaColors {
    // Grounds / surfaces (mock --page-bg / --bg / --surf / --surf2 / --surf3 / --hair)
    val PageBg = Color(0xFF0A0F0C)
    val Bg = Color(0xFF0B100D)
    val Surf = Color(0xFF131A15)
    val Surf2 = Color(0xFF1A241D)
    val Surf3 = Color(0xFF222E25)
    val Hair = Color(0xFF28352B)
    val Line = Color(0xFF1C2620)

    // Ink (mock --ink / --muted / --faint)
    val Ink = Color(0xFFECF4EE)
    val Muted = Color(0xFF8FA497)
    val Faint = Color(0xFF5F7367)

    // Brand (mock --brand / --brand-2 / --brand-d, on-brand ink)
    val Brand = Color(0xFF8AD457)
    val Brand2 = Color(0xFF5FB531)
    val BrandD = Color(0xFFB7EA8C)
    val OnBrand = Color(0xFF08130B)
    val BrandTint = Color(0x248AD457) // rgba(138,212,87,.14) — logo/glyph tiles

    // Semantic (mock --ok/--warn/--danger/--teal/--purple + translucent "x" fills)
    val Ok = Color(0xFF8AD457)
    val OkX = Color(0x298AD457) // rgba(138,212,87,.16)
    val Warn = Color(0xFFF0B54B)
    val WarnX = Color(0x26F0B54B) // rgba(240,181,75,.15)
    val Danger = Color(0xFFFB6F63)
    val DangerX = Color(0x26FB6F63) // rgba(251,111,99,.15)
    val Teal = Color(0xFF57C9B0)
    val TealX = Color(0x2957C9B0) // rgba(87,201,176,.16)
    val Purple = Color(0xFFA78BF5)
    val PurpleX = Color(0x26A78BF5) // rgba(167,139,245,.15)

    // Gradients (mock --grad 135deg, --grad-soft)
    val BrandGradient: Brush = Brush.linearGradient(listOf(Color(0xFF93DA5E), Color(0xFF5FB531)))
    val BrandGradientSoft: Brush = Brush.linearGradient(listOf(Color(0x2993DA5E), Color(0x0D5FB531)))
}
