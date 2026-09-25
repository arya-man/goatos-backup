package sg.mesha.goatos.push

import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.ui.Routes

/**
 * The Feed Director's sale pushes (maintainer decisions 2026-09-07 and 2026-09-25): the reduce
 * notice, its 07:00 reminder, and the failed-sale return. Each carries `screen: feed_stock` and
 * `href: /feed/analytics` -- a web page with no phone screen -- so without an explicit mapping the
 * tap fell through to the person's generic landing. They open Feed, the module they are about, and
 * the words shown are the backend's, verbatim.
 */
class FeedSalePushMappingTest {
    private fun payload(type: String) = mapOf(
        PushExtras.TYPE to type,
        PushExtras.SCREEN to "feed_stock",
        PushExtras.HREF to "/feed/analytics",
    )

    @Test
    fun `every sale feed notice opens Feed`() {
        for (type in listOf("feed_sale_reduce", "feed_sale_reduce_reminder", "feed_sale_failed_return")) {
            assertEquals(type, Routes.FEED_DIRECTION, resolvePushRoute(payload(type)))
        }
    }

    @Test
    fun `the failed sale return is matched by its type even without a screen`() {
        assertEquals(Routes.FEED_DIRECTION, resolvePushRoute(mapOf(PushExtras.TYPE to "feed_sale_failed_return")))
    }

    @Test
    fun `the failed sale return shows the backend's words verbatim`() {
        val body = "The sale to Ramesh Traders failed on 24/09/2026. 12 animals are back in CPT Castro 1, CPT Castro 2. " +
            "Feed these pens as before from 25/09/2026."
        val display = pushDisplayText(
            data = payload("feed_sale_failed_return") + mapOf(PushExtras.TITLE to "Sale failed: feed as before", PushExtras.BODY to body),
            notificationTitle = "Mesha",
            notificationBody = "Open the app for details.",
            defaultTitle = "Mesha",
        )
        assertEquals("Sale failed: feed as before", display.title)
        assertEquals(body, display.body)
    }
}
