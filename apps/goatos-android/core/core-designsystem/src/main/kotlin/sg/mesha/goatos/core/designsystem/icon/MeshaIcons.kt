package sg.mesha.goatos.core.designsystem.icon

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.PathParser
import androidx.compose.ui.unit.dp

/**
 * The mock's stroked SVG icon set (`mock/vaccination-mobile-mock.html` <symbol> defs),
 * ported 1:1 as Compose [ImageVector]s. All are 24x24, stroke-only (no fill), round
 * caps/joins — matching the mock's `.ic` rule. Tint at the call site via `Icon(tint=…)`;
 * the placeholder stroke color is recolored by the tint. Replaces the text-glyph
 * placeholders (`• 文 ◎ ◈`) that never matched the mock.
 */
private fun strokeIcon(name: String, vararg paths: String, strokeWidth: Float = 2f): ImageVector =
    ImageVector.Builder(
        name = name,
        defaultWidth = 24.dp,
        defaultHeight = 24.dp,
        viewportWidth = 24f,
        viewportHeight = 24f,
    ).apply {
        paths.forEach { d ->
            addPath(
                pathData = PathParser().parsePathString(d).toNodes(),
                stroke = SolidColor(Color.Black),
                strokeLineWidth = strokeWidth,
                strokeLineCap = StrokeCap.Round,
                strokeLineJoin = StrokeJoin.Round,
            )
        }
    }.build()

object MeshaIcons {
    // rect/circle from the mock rewritten as equivalent path data (PathParser is path-only).
    val Calendar: ImageVector = strokeIcon(
        "cal",
        "M6.5 5.5h11a3 3 0 0 1 3 3v9a3 3 0 0 1 -3 3h-11a3 3 0 0 1 -3 -3v-9a3 3 0 0 1 3 -3z",
        "M3.5 10h17M8.5 3v4M15.5 3v4",
    )
    val Syringe: ImageVector = strokeIcon(
        "syringe",
        "M13 4.5 19.5 11M17.5 5.5 18.5 6.5M15.5 9 8.5 16l-3.5 1 1-3.5 7-7zM5.5 15.5 8.5 18.5",
    )
    val Home: ImageVector = strokeIcon(
        "home",
        "M3 11.5 12 4l9 7.5M5.5 10.5V20h13v-9.5",
    )
    val Bell: ImageVector = strokeIcon(
        "bell",
        "M6 9a6 6 0 1 1 12 0c0 5 2 6 2 6H4s2-1 2-6M10 20a2 2 0 0 0 4 0",
    )
    val User: ImageVector = strokeIcon(
        "user",
        "M16 8.5a4 4 0 1 1 -8 0 4 4 0 0 1 8 0z",
        "M5 20c1-4 4.2-5.5 7-5.5s6 1.5 7 5.5",
    )
    val Menu: ImageVector = strokeIcon("menu", "M4 7h16M4 12h16M4 17h16")
    val Refresh: ImageVector = strokeIcon(
        "refresh",
        "M4 11a8 8 0 0 1 13.5-5L20 8M20 13a8 8 0 0 1 -13.5 5L4 16M20 4v4h-4M4 20v-4h4",
    )
    val Chevron: ImageVector = strokeIcon("chev", "M9 5l7 7-7 7")
    val Close: ImageVector = strokeIcon("x", "M6 6l12 12M18 6L6 18")
    val Warn: ImageVector = strokeIcon("warn", "M12 4l9 16H3zM12 10v4.5M12 18h.01")
    val Check: ImageVector = strokeIcon("check", "M4 12.5l5 5L20 6.5")
    val Bluetooth: ImageVector = strokeIcon("bt", "M7 7.5 17 17l-5 4V3l5 4L7 16.5")
    val Logout: ImageVector = strokeIcon("logout", "M15 4h4v16h-4M11 8l-4 4 4 4M7 12h9")
    val Globe: ImageVector = strokeIcon(
        "globe",
        "M21 12a9 9 0 1 1 -18 0 9 9 0 0 1 18 0z",
        "M3.5 12h17M12 3c3.4 3.5 3.4 14.5 0 18M12 3c-3.4 3.5-3.4 14.5 0 18",
    )
    val ChevronLeft: ImageVector = strokeIcon("chevl", "M15 5l-7 7 7 7")
    val Plus: ImageVector = strokeIcon("plus", "M12 5v14M5 12h14")
    val Video: ImageVector = strokeIcon(
        "video",
        "M4 7.5h9a1.5 1.5 0 0 1 1.5 1.5v6a1.5 1.5 0 0 1 -1.5 1.5h-9a1.5 1.5 0 0 1 -1.5 -1.5v-6a1.5 1.5 0 0 1 1.5 -1.5z",
        "M14.5 11 20.5 8v8l-6 -3",
    )
    val Vaccine: ImageVector = strokeIcon(
        "vial",
        "M9 3.5h6M10 3.5v6.5l-3 8a2 2 0 0 0 2 2.5h6a2 2 0 0 0 2 -2.5l-3 -8V3.5M8 13.5h8",
    )
    val Search: ImageVector = strokeIcon(
        "search",
        "M18 11a7 7 0 1 1 -14 0 7 7 0 0 1 14 0z",
        "M20 20l-4 -4",
    )
    val Clock: ImageVector = strokeIcon(
        "clock",
        "M21 12a9 9 0 1 1 -18 0 9 9 0 0 1 18 0z",
        "M12 7.5V12l3.5 2.2",
    )

    /** Maps a backend nav-item key to its mock icon. */
    fun forNavKey(key: String): ImageVector = when (key.lowercase()) {
        "calendar" -> Calendar
        "vaccination", "sheds", "pc.vaccination", "execution" -> Syringe
        "leadership", "home", "dhome", "overview" -> Home
        "alerts", "notifications" -> Bell
        "you", "profile", "settings" -> User
        else -> Syringe
    }
}
