package sg.mesha.goatos.ui

import android.net.Uri
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.nio.file.Path
import kotlin.io.path.readText

/**
 * The Work Board routes (maintainer decision 2026-09-10): the L0 list is exactly `/work` (the
 * backend nav item's href, verbatim) and one row's drill carries the backend `row_key` —
 * `module|source_type|source_id`, pipes included — URL-encoded so Navigation hands it back intact.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class WorkBoardRouteIdentityTest {
    @Test
    fun `the list route is the backend href verbatim and the drill pattern names the row key arg`() {
        assertEquals("/work", Routes.WORK)
        assertEquals("rowKey", Routes.WORK_ITEM_ARG)
        assertEquals("/work/item/{rowKey}", Routes.WORK_ITEM)
    }

    @Test
    fun `a row key with pipe characters round-trips through the item route`() {
        val rowKey = "feed|feed_packing_completion|2d1a0f3c-0b1e-4d8f-9a2b-7c6e5d4f3a21"

        val route = Routes.workItemRoute(rowKey)

        // The pipes are encoded on the way in, so the route is one clean path segment ...
        assertTrue(route.startsWith("/work/item/"))
        assertFalse("pipes must be encoded in the route", route.substringAfter("/work/item/").contains('|'))
        assertTrue(route.contains("%7C"))
        // ... and decode back to the exact backend row_key on the way out.
        assertEquals(rowKey, Uri.decode(route.removePrefix("/work/item/")))
        // Never a slash inside the segment, or Navigation would read it as a deeper path.
        assertFalse(route.removePrefix("/work/item/").contains('/'))
    }

    @Test
    fun `the work board routes are registered as real navigation destinations`() {
        val navHost = Path.of("src/main/kotlin/sg/mesha/goatos/ui/AppNavHost.kt").readText()

        assertTrue(navHost.contains("composable(Routes.WORK)"))
        assertTrue(navHost.contains("route = Routes.WORK_ITEM,"))
        assertTrue(navHost.contains("navArgument(Routes.WORK_ITEM_ARG)"))
        assertTrue(navHost.contains("WorkBoardScreen("))
        assertTrue(navHost.contains("WorkBoardDetailScreen("))
        // The detail's Open button resolves the backend href through the SAME resolver pushes use,
        // never a hand-rolled route guess.
        assertTrue(navHost.contains("state.row?.href?.takeIf { it.isNotBlank() }?.let { pushTargetRoute(it) }"))
    }
}
