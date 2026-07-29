package sg.mesha.goatos.core.designsystem.theme

import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ProvideTextStyle
import androidx.compose.material3.Shapes
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp

// ---------------------------------------------------------------------------
// Material 3 (Expressive-styled) theme mapped to the mock's dark field palette
// (mock/vaccination-mobile-mock.html :root). This is the keystone the whole
// redesign builds on: screens consume MaterialTheme.colorScheme / .shapes rather
// than hand-rolled per-file tokens, so the app is one coherent M3 surface in the
// mock's colours. Dark-only by design (the mock is dark-first).
// ---------------------------------------------------------------------------

private val MeshaDarkScheme = darkColorScheme(
    primary = Color(0xFF8AD457),
    onPrimary = Color(0xFF08130B),
    primaryContainer = Color(0xFF23361B),
    onPrimaryContainer = Color(0xFFB7EA8C),
    secondary = Color(0xFF57C9B0),
    onSecondary = Color(0xFF04231C),
    secondaryContainer = Color(0xFF123029),
    onSecondaryContainer = Color(0xFF8FE6D3),
    tertiary = Color(0xFFA78BF5),
    onTertiary = Color(0xFF1E1235),
    tertiaryContainer = Color(0xFF2A2145),
    onTertiaryContainer = Color(0xFFCDBBFF),
    error = Color(0xFFFB6F63),
    onError = Color(0xFF2A0B08),
    errorContainer = Color(0xFF3D1512),
    onErrorContainer = Color(0xFFFFB4AC),
    background = Color(0xFF0A0F0C),
    onBackground = Color(0xFFECF4EE),
    surface = Color(0xFF0B100D),
    onSurface = Color(0xFFECF4EE),
    surfaceVariant = Color(0xFF1A241D),
    onSurfaceVariant = Color(0xFF8FA497),
    surfaceContainerLowest = Color(0xFF080C09),
    surfaceContainerLow = Color(0xFF131A15),
    surfaceContainer = Color(0xFF1A241D),
    surfaceContainerHigh = Color(0xFF222E25),
    surfaceContainerHighest = Color(0xFF28352B),
    outline = Color(0xFF3A493F),
    outlineVariant = Color(0xFF28352B),
    inverseSurface = Color(0xFFECF4EE),
    inverseOnSurface = Color(0xFF0A0F0C),
    scrim = Color(0xFF000000),
)

// Expressive shapes: generous, varied corner radii (M3 Expressive leans rounder).
private val MeshaShapes = Shapes(
    extraSmall = RoundedCornerShape(8.dp),
    small = RoundedCornerShape(12.dp),
    medium = RoundedCornerShape(18.dp),
    large = RoundedCornerShape(24.dp),
    extraLarge = RoundedCornerShape(30.dp),
)

private val MeshaTypography = Typography(
    displayLarge = MeshaType.screenTitle,
    displayMedium = MeshaType.screenTitle,
    displaySmall = MeshaType.screenTitle,
    headlineLarge = MeshaType.screenTitle,
    headlineMedium = MeshaType.screenTitle,
    headlineSmall = MeshaType.headerTitle,
    titleLarge = MeshaType.headerTitle,
    titleMedium = MeshaType.cardTitle,
    titleSmall = MeshaType.listTitle,
    bodyLarge = MeshaType.body,
    bodyMedium = MeshaType.body,
    bodySmall = MeshaType.cardSubtitle,
    labelLarge = MeshaType.button,
    labelMedium = MeshaType.pill,
    labelSmall = MeshaType.caption,
)

@Composable
fun GoatOsTheme(
    // Retained for source compatibility; the app is dark-first to match the mock.
    useDarkTheme: Boolean = true,
    respectSystem: Boolean = false,
    content: @Composable () -> Unit,
) {
    MaterialTheme(
        colorScheme = MeshaDarkScheme,
        shapes = MeshaShapes,
        typography = MeshaTypography,
    ) {
        ProvideTextStyle(MeshaTypography.bodyMedium, content)
    }
}
