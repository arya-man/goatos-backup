package sg.mesha.goatos.push

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
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
                PushExtras.ROLE to "operator",
            ),
        )

        assertEquals(Routes.VACCINATION, route)
    }

    @Test
    fun `leadership vaccination reminder opens vaccination landing`() {
        assertEquals(
            Routes.VACCINATION,
            resolvePushRoute(
                mapOf(
                    PushExtras.TYPE to "vaccination_reminder",
                    PushExtras.SCREEN to "vaccination",
                    PushExtras.SHED_ID to "shed-123",
                    PushExtras.ROLE to "pc_director",
                ),
            ),
        )
    }

    @Test
    fun `explicit taskless scan target falls back instead of showing an unresumable execution`() {
        val route = resolvePushRoute(mapOf(PushExtras.TARGET to "scan/shed-123"))

        assertEquals(Routes.calendarDriveRoute(null), route)
    }

    @Test
    fun `explicit scan target preserves exact task identity`() {
        val route = resolvePushRoute(
            mapOf(PushExtras.TARGET to "scan/shed-123?task_id=task-456"),
        )

        assertTrue(route.startsWith("/scan?"))
        assertTrue(route.contains("shedId=shed-123"))
        assertTrue(route.contains("taskId=task-456"))
    }

    @Test
    fun `verification pending opens the exact verifier item from type-only payload`() {
        val route = resolvePushRoute(
            mapOf(
                PushExtras.TYPE to "verification_pending",
                PushExtras.ITEM_ID to "item-123",
                PushExtras.CATEGORY to "vaccination proof",
                PushExtras.ROLE to "verifier",
            ),
        )

        assertEquals("/verify/item?itemId=item-123&category=vaccination%20proof", route)
    }

    @Test
    fun `verification pending opens vaccination for director and ceo recipients`() {
        assertEquals(
            Routes.VACCINATION,
            resolvePushRoute(
                mapOf(
                    PushExtras.TYPE to "verification_pending",
                    PushExtras.ITEM_ID to "item-123",
                    PushExtras.ROLE to "ceo_internal",
                ),
            ),
        )
    }

    @Test
    fun `verification pending opens vaccination for park head recipients`() {
        assertEquals(
            Routes.VACCINATION,
            resolvePushRoute(
                mapOf(
                    PushExtras.TYPE to "verification_pending",
                    PushExtras.ITEM_ID to "item-123",
                    PushExtras.ROLE to "park_head",
                ),
            ),
        )
    }

    @Test
    fun `verification approved opens vaccination after leadership screen removal`() {
        assertEquals(
            Routes.VACCINATION,
            resolvePushRoute(mapOf(PushExtras.TYPE to "verification_approved")),
        )
    }

    @Test
    fun `legacy leadership close payload opens vaccination after leadership screen removal`() {
        assertEquals(
            Routes.VACCINATION,
            resolvePushRoute(mapOf(PushExtras.SCREEN to "leadership_close")),
        )
    }

    @Test
    fun `verification closed opens the originating shed record from type-only payload`() {
        assertEquals(
            "/record?shedId=shed-123",
            resolvePushRoute(
                mapOf(
                    PushExtras.TYPE to "verification_closed",
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
                ),
            ),
        )
    }
}
