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
    val Download: ImageVector = strokeIcon(
        "download",
        "M12 4v10M8 11l4 4 4-4M5 19h14",
    )
    val Chevron: ImageVector = strokeIcon("chev", "M9 5l7 7-7 7")
    val Close: ImageVector = strokeIcon("x", "M6 6l12 12M18 6L6 18")
    val Warn: ImageVector = strokeIcon("warn", "M12 4l9 16H3zM12 10v4.5M12 18h.01")
    val Check: ImageVector = strokeIcon("check", "M4 12.5l5 5L20 6.5")

    /** Approval queue: a decision to be made — a tick inside a boundary, not a bare tick
     *  (the bare [Check] already means "this is the active/selected thing"). */
    val CheckCircle: ImageVector = strokeIcon(
        "checkcircle",
        "M12 3.5a8.5 8.5 0 1 0 0 17 8.5 8.5 0 0 0 0-17z",
        "M8 12.2l2.8 2.8L16 9.8",
    )
    val Bluetooth: ImageVector = strokeIcon("bt", "M7 7.5 17 17l-5 4V3l5 4L7 16.5")
    val Logout: ImageVector = strokeIcon("logout", "M15 4h4v16h-4M11 8l-4 4 4 4M7 12h9")
    val Globe: ImageVector = strokeIcon(
        "globe",
        "M21 12a9 9 0 1 1 -18 0 9 9 0 0 1 18 0z",
        "M3.5 12h17M12 3c3.4 3.5 3.4 14.5 0 18M12 3c-3.4 3.5-3.4 14.5 0 18",
    )
    val ChevronLeft: ImageVector = strokeIcon("chevl", "M15 5l-7 7 7 7")

    /** Downward chevron — the affordance on a closed single-choice dropdown. */
    val ChevronDown: ImageVector = strokeIcon("chevd", "M5 9l7 7 7-7")
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
    val Eye: ImageVector = strokeIcon(
        "eye",
        "M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12z",
        "M12 9.5a2.5 2.5 0 1 0 0 5 2.5 2.5 0 0 0 0-5z",
    )
    val EyeOff: ImageVector = strokeIcon(
        "eyeoff",
        "M2.5 12S6 5.5 12 5.5c2 0 3.7.6 5.1 1.4M21.5 12S18 18.5 12 18.5c-2 0-3.7-.6-5.1-1.4",
        "M9.5 9.5a2.5 2.5 0 0 0 3.5 3.5",
        "M4 4l16 16",
    )
    val Feed: ImageVector = strokeIcon(
        "feed",
        "M3.5 11h17a8.5 8.5 0 0 1 -17 0zM8 11c0-2 1-2.5 0-4.5M12 11c0-2 1-2.5 0-4.5M16 11c0-2 1-2.5 0-4.5",
    )
    val Goat: ImageVector = strokeIcon(
        "goat",
        "M5 8c-1-3 1-4 2-2M19 8c1-3-1-4-2-2M7 6c0 6 2 9 5 9s5-3 5-9M9 15v3M15 15v3M10 11h.01M14 11h.01",
    )

    /**
     * Counts vertical (`bar-chart-3`, the same glyph admin-web uses for Counts). Deliberately
     * NOT the syringe: per AGENTS.md the syringe/injection icon belongs to the Vaccination
     * MODULE alone and must never stand in for another vertical.
     */
    val BarChart: ImageVector = strokeIcon(
        "barchart",
        "M3.5 3.5v17h17",
        "M8 17.5v-4M13 17.5v-8M18 17.5v-11",
    )

    /** Birth/Death: one lifecycle event up, one down. */
    val ArrowUpDown: ImageVector = strokeIcon(
        "arrowupdown",
        "M7 20.5V4M3.5 7.5 7 4l3.5 3.5",
        "M17 3.5V20M13.5 16.5 17 20l3.5-3.5",
    )

    /** Shifting: an animal moving between sheds/parks. */
    val Transfer: ImageVector = strokeIcon(
        "transfer",
        "M4 8.5h16M16.5 5 20 8.5 16.5 12",
        "M20 15.5H4M7.5 12 4 15.5 7.5 19",
    )

    /** Neutral fallback for an unmapped backend key (a generic module tile). */
    val Module: ImageVector = strokeIcon(
        "module",
        "M4 4.5h6.5v6.5H4zM13.5 4.5H20v6.5h-6.5zM4 13.5h6.5V20H4zM13.5 13.5H20V20h-6.5z",
    )

    /**
     * Maps a backend nav-item OR module key to its mock icon. Keys are the stable backend
     * identifiers from `moduleNavRegistry` (bootstrap_copy.go); an unknown key falls back to
     * the neutral [Module] tile rather than borrowing another vertical's glyph.
     */
    fun forNavKey(key: String): ImageVector = when (key.lowercase()) {
        "calendar" -> Calendar
        // Vaccination MODULE + its own destinations. The syringe is scoped to this module.
        "vaccination", "sheds", "pc.vaccination", "execution" -> Syringe
        "home", "dhome", "overview" -> Home
        "alerts", "notifications" -> Bell
        "you", "profile", "settings" -> User
        // Counts vertical and its field-event destinations.
        "counts" -> BarChart
        // The nav destination, plus the two individual request types an approval row carries.
        "birth_death", "birth", "death" -> ArrowUpDown
        "shifting" -> Transfer
        // The approver's queue: a decision to be made, not a record to be captured.
        "approval", "approvals" -> CheckCircle
        // Declared-but-unbuilt modules the backend advertises as "soon".
        "feed_direction" -> Feed
        "breeding" -> Goat
        // Standalone Verifier section (context/architecture/verifier-app-and-flow.md) — its
        // one job is a video-verification queue, so the Video glyph is its nav icon.
        "verify", "verification", "video_verification" -> Video
        else -> Module
    }
}
