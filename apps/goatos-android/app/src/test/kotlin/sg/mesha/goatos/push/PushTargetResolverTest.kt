package sg.mesha.goatos.push

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.assertNotEquals
import org.junit.Test
import sg.mesha.goatos.ui.Routes

class PushTargetResolverTest {
    @Test
    fun `shed reminder opens vaccination landing instead of scan`() {
        val route = resolvePushRoute(
            mapOf(
                PushExtras.TYPE to "vaccination_reminder",
                PushExtras.SCREEN to "vaccination",
                PushExtras.SHED_ID to "shed-123",
                PushExtras.TASK_ID to "task-456",
            ),
        )

        assertEquals(Routes.VACCINATION, route)
    }

    @Test
    fun `explicit taskless scan target falls through instead of showing an unresumable execution`() {
        assertNull(resolvePushRoute(mapOf(PushExtras.TARGET to "scan/shed-123")))
    }

    @Test
    fun `explicit scan target preserves exact task identity`() {
        val route = resolvePushRoute(
            mapOf(PushExtras.TARGET to "scan/shed-123?task_id=task-456"),
        )

        assertTrue(route!!.startsWith("/scan?"))
        assertTrue(route.contains("shedId=shed-123"))
        assertTrue(route.contains("taskId=task-456"))
    }

    /**
     * The real payload the backend sends a verifier for a waiting proof video: `screen`, `type`
     * AND `target` are all present. The screen/type must never win over the target, or the tap
     * lands on a shed record or a module landing instead of the video under review.
     */
    @Test
    fun `verifier pending proof opens the exact video review even with screen and type present`() {
        val route = resolvePushRoute(
            mapOf(
                PushExtras.TYPE to "verification_pending",
                PushExtras.SCREEN to "verification",
                PushExtras.TARGET to "/verification/items/item-123",
                PushExtras.ITEM_ID to "item-123",
                PushExtras.CATEGORY to "weighing proof",
                PushExtras.SHED_ID to "shed-123",
            ),
        )

        assertEquals("/verify/item?itemId=item-123&actionMode=false&missed=false", route)
    }

    @Test
    fun `verification pending opens the exact verifier item from type-only payload`() {
        val route = resolvePushRoute(
            mapOf(
                PushExtras.TYPE to "verification_pending",
                PushExtras.ITEM_ID to "item-123",
                PushExtras.CATEGORY to "vaccination proof",
            ),
        )

        assertEquals("/verify/item?itemId=item-123&category=vaccination%20proof&actionMode=false&missed=false", route)
    }

    /** Who someone is comes from their own sign-in, never from a field on the message. */
    @Test
    fun `verifier item opens from the target no matter what role the payload claims`() {
        assertEquals(
            "/verify/item?itemId=item-123&actionMode=false&missed=false",
            resolvePushRoute(
                mapOf(
                    PushExtras.TYPE to "verification_pending",
                    PushExtras.TARGET to "/verification/items/item-123",
                    PushExtras.ROLE to "growth_director",
                ),
            ),
        )
    }

    @Test
    fun `weighing leadership pending proof opens weighing, never vaccination`() {
        assertEquals(
            Routes.WEIGHING,
            resolvePushRoute(
                mapOf(
                    PushExtras.TYPE to "verification_pending",
                    PushExtras.SCREEN to "weighing_overview",
                    PushExtras.TARGET to "/weighing",
                ),
            ),
        )
    }

    /**
     * The verifier and the module's director are sent the SAME waiting-proof event with the same
     * `type`; only the screen/target differ. The director must land on their own module, never in
     * the verifier's review queue.
     */
    @Test
    fun `feed leadership pending proof opens feed, not the review queue`() {
        assertEquals(
            Routes.FEED_DIRECTION,
            resolvePushRoute(
                mapOf(
                    PushExtras.TYPE to "verification_pending",
                    PushExtras.SCREEN to "feed_overview",
                    PushExtras.ITEM_ID to "item-123",
                ),
            ),
        )
    }

    @Test
    fun `counts leadership pending proof opens counts, not the review queue`() {
        assertEquals(
            // This tree has no /counts census root; Counts lands on its module-scoped birth route.
            Routes.COUNTS_BIRTH,
            resolvePushRoute(
                mapOf(
                    PushExtras.TYPE to "verification_pending",
                    PushExtras.SCREEN to "counts_overview",
                    PushExtras.ITEM_ID to "item-123",
                ),
            ),
        )
    }

    @Test
    fun `weighing task target keeps its own identity`() {
        assertEquals(
            "/weighing/task?campaignId=task-9",
            resolvePushRoute(mapOf(PushExtras.TARGET to "/weighing/task?campaignId=task-9")),
        )
    }

    @Test
    fun `weighing shed bucket target keeps its own identity`() {
        assertEquals(
            "/weighing/shed?campaignId=task-9&campaignShedId=bucket-2",
            resolvePushRoute(
                mapOf(PushExtras.TARGET to "/weighing/shed?campaignId=task-9&campaignShedId=bucket-2"),
            ),
        )
    }

    @Test
    fun `weighing videos target keeps its own identity`() {
        assertEquals(
            Routes.WEIGHING_VIDEOS,
            resolvePushRoute(mapOf(PushExtras.TARGET to "/weighing/videos")),
        )
    }

    /**
     * The load-bearing case: an unrecognised alert must NOT pick a module. Naming no destination
     * lands the person on their own home screen, so a weighing-only director is never dropped on
     * Vaccination.
     */
    @Test
    fun `unrecognised alert names no destination`() {
        assertNull(resolvePushRoute(mapOf(PushExtras.TYPE to "some_future_alert")))
        assertNull(resolvePushRoute(mapOf(PushExtras.SCREEN to "some_future_screen")))
        assertNull(resolvePushRoute(mapOf(PushExtras.TYPE to "verification_approved")))
        assertNull(resolvePushRoute(mapOf(PushExtras.SCREEN to "leadership_close")))
    }

    /** A link this build cannot open is not a reason to guess a module either. */
    @Test
    fun `unknown target names no destination`() {
        assertNull(resolvePushRoute(mapOf(PushExtras.TARGET to "/feed")))
        assertNull(resolvePushRoute(mapOf(PushExtras.TARGET to "/verification/items/")))
    }

    @Test
    fun `verification closed opens the originating shed record from type-only payload`() {
        assertEquals(
            "/record?shedId=shed-123",
            resolvePushRoute(
                mapOf(
                    PushExtras.TYPE to "verification_closed",
                    PushExtras.SHED_ID to "shed-123",
                    // The record screen is vaccination's, so the push must say which module it
                    // belongs to. Without the category a weighing rework opened the vaccination
                    // record for that shed, so an absent category now means "not vaccination".
                    PushExtras.CATEGORY to "vaccination",
                ),
            ),
        )
    }

    @Test
    fun `a weighing rework never opens the vaccination record`() {
        // The record route is VACCINATION's. The generic rework notice carries screen="record"
        // for every module, so a module-blind match sent a weighing rework into the vaccination
        // record for that shed -- the operator opens the wrong module's screen for the shed they
        // were asked to re-shoot.
        val route = resolvePushRoute(
            mapOf(
                PushExtras.TYPE to "rework",
                PushExtras.SHED_ID to "shed-123",
                PushExtras.CATEGORY to "weighing",
            ),
        )
        assertNotEquals("/record?shedId=shed-123", route)
    }

    @Test
    fun `a push with no category never assumes vaccination`() {
        // Absent category means unknown module, and the safe miss is the person's own landing
        // screen -- never another module's record.
        assertNotEquals(
            "/record?shedId=shed-123",
            resolvePushRoute(
                mapOf(
                    PushExtras.TYPE to "rework",
                    PushExtras.SHED_ID to "shed-123",
                ),
            ),
        )
    }

    @Test
    fun `rework opens the originating shed record`() {
        assertEquals(
            "/record?shedId=shed-123",
            resolvePushRoute(
                mapOf(
                    PushExtras.TYPE to "rework",
                    PushExtras.SHED_ID to "shed-123",
                    PushExtras.CATEGORY to "vaccination",
                ),
            ),
        )
    }

    @Test
    fun `shedless record payload names no destination instead of a stranger's shed`() {
        assertNull(resolvePushRoute(mapOf(PushExtras.TYPE to "rework")))
    }
}
