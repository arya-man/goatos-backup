package sg.mesha.goatos.core.designsystem.icon

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Test

/**
 * A web-authored phone tab (maintainer instruction 2026-10-01) names its bar icon from a CLOSED set.
 *
 * [BACKEND_TAB_ICONS] MIRRORS `TabIcons` in `backend/internal/penroutines/domain/tab.go` byte for
 * byte, in the same order. When the backend adds a key, add it here AND give it a glyph in
 * [MeshaIcons.forTabIcon]: the first test goes red until both are done, so a new icon can never
 * reach a phone as the neutral fallback tile.
 */
class MeshaTabIconTest {

    @Test
    fun `every backend tab icon key has a glyph`() {
        BACKEND_TAB_ICONS.forEach { key ->
            assertNotNull("tab icon '$key' has no glyph", MeshaIcons.forTabIcon(key))
        }
        assertEquals("the closed set has no duplicate keys", BACKEND_TAB_ICONS.size, BACKEND_TAB_ICONS.toSet().size)
    }

    @Test
    fun `each tab icon key maps to its agreed glyph`() {
        val expected = mapOf(
            "routine" to MeshaIcons.Routine,
            "clipboard_check" to MeshaIcons.ClipboardCheck,
            "check_circle" to MeshaIcons.CheckCircle,
            "camera" to MeshaIcons.Camera,
            "video" to MeshaIcons.Video,
            "photo" to MeshaIcons.Photo,
            "calendar" to MeshaIcons.Calendar,
            "clock" to MeshaIcons.Clock,
            "bell" to MeshaIcons.Bell,
            "eye" to MeshaIcons.Eye,
            "home" to MeshaIcons.Home,
            "pen_visit" to MeshaIcons.PenVisit,
            "goat" to MeshaIcons.Goat,
            "health" to MeshaIcons.Health,
            "pc_care" to MeshaIcons.PcCare,
            "syringe" to MeshaIcons.Syringe,
            "vaccine" to MeshaIcons.Vaccine,
            "deworming" to MeshaIcons.Deworming,
            "anti_protozoan" to MeshaIcons.AntiProtozoan,
            "tick" to MeshaIcons.Tick,
            "hoof_trimming" to MeshaIcons.HoofTrimming,
            "hair_trimming" to MeshaIcons.HairTrimming,
            "fumigation" to MeshaIcons.Fumigation,
            "feed" to MeshaIcons.Feed,
            "water" to MeshaIcons.Water,
            "package" to MeshaIcons.Package,
            "truck" to MeshaIcons.Truck,
            "store" to MeshaIcons.Store,
            "milk" to MeshaIcons.MilkPreparation,
            "breeding" to MeshaIcons.Breeding,
            "birth" to MeshaIcons.Birth,
            "bar_chart" to MeshaIcons.BarChart,
            "document" to MeshaIcons.Document,
            "tasks" to MeshaIcons.Tasks,
            "warn" to MeshaIcons.Warn,
        )
        assertEquals(BACKEND_TAB_ICONS, expected.keys.toList())
        expected.forEach { (key, glyph) -> assertSame(key, glyph, MeshaIcons.forTabIcon(key)) }
    }

    @Test
    fun `an item with no or an unknown icon keeps the glyph for its key`() {
        assertNull(MeshaIcons.forTabIcon(""))
        assertNull(MeshaIcons.forTabIcon(null))
        assertNull(MeshaIcons.forTabIcon("rocket"))
        assertSame(MeshaIcons.forNavKey("pc_deworming"), MeshaIcons.forNavItem("pc_deworming", ""))
        assertSame(MeshaIcons.forNavKey("routine_tab_x"), MeshaIcons.forNavItem("routine_tab_x", "rocket"))
        assertSame(MeshaIcons.Fumigation, MeshaIcons.forNavItem("routine_tab_fumigation", "fumigation"))
    }

    private companion object {
        /** Mirrors backend/internal/penroutines/domain/tab.go `TabIcons`, in picker order. */
        val BACKEND_TAB_ICONS = listOf(
            "routine", "clipboard_check", "check_circle", "camera", "video", "photo", "calendar",
            "clock", "bell", "eye", "home", "pen_visit", "goat", "health", "pc_care", "syringe",
            "vaccine", "deworming", "anti_protozoan", "tick", "hoof_trimming", "hair_trimming",
            "fumigation", "feed", "water", "package", "truck", "store", "milk", "breeding", "birth",
            "bar_chart", "document", "tasks", "warn",
        )
    }
}
