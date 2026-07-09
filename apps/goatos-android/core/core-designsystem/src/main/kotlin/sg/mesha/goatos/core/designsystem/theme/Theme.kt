package sg.mesha.goatos.core.designsystem.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

// Tokens seeded from docs/mobile/design-system.md; refine against the mock in the
// design-system pass. Dark is the DEFAULT theme on both surfaces (ADR + design-
// system.md), so GoatOsTheme defaults to dark regardless of the system setting.
private val Brand = Color(0xFF3DA35D)
private val BrandDark = Color(0xFF2E7D46)

private val DarkColors = darkColorScheme(
    primary = Brand,
    onPrimary = Color(0xFF06210F),
    background = Color(0xFF0A0F0C),
    onBackground = Color(0xFFEAF3EE),
    surface = Color(0xFF18211A),
    onSurface = Color(0xFFEAF3EE),
)

private val LightColors = lightColorScheme(
    primary = BrandDark,
    onPrimary = Color.White,
    background = Color(0xFFF3F6F1),
    onBackground = Color(0xFF13201A),
    surface = Color(0xFFECF1E8),
    onSurface = Color(0xFF13201A),
)

@Composable
fun GoatOsTheme(
    // Dark-default: only flips to light when explicitly asked (or a future stored
    // preference). Never auto-follow system to light unless the user chose it.
    useDarkTheme: Boolean = true,
    respectSystem: Boolean = false,
    content: @Composable () -> Unit,
) {
    val dark = if (respectSystem) isSystemInDarkTheme() else useDarkTheme
    MaterialTheme(
        colorScheme = if (dark) DarkColors else LightColors,
        content = content,
    )
}
