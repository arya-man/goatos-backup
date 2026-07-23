package sg.mesha.goatos.push

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.ui.Routes

class PushTargetResolverTest {
    @Test
    fun `shed reminder opens exact task scan route`() {
        val route = resolvePushRoute(
            mapOf(
                PushExtras.TYPE to "reminder",
                PushExtras.SCREEN to "scan",
                PushExtras.SHED_ID to "shed-123",
                PushExtras.TASK_ID to "task-456",
            ),
        )

        assertTrue(route.startsWith("/scan?"))
        assertTrue(route.contains("shedId=shed-123"))
        assertTrue(route.contains("taskId=task-456"))
    }

    @Test
    fun `taskless scan reminder falls back instead of showing an unresumable execution`() {
        assertEquals(
            Routes.VACCINATION,
            resolvePushRoute(
                mapOf(
                    PushExtras.TYPE to "reminder",
                    PushExtras.SCREEN to "scan",
                    PushExtras.SHED_ID to "shed-123",
                ),
            ),
        )
    }

    @Test
    fun `explicit taskless scan target falls back instead of showing an unresumable execution`() {
        val route = resolvePushRoute(mapOf(PushExtras.TARGET to "scan/shed-123"))

        assertEquals(Routes.CALENDAR_DRIVE, route)
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
            ),
        )

        assertEquals("/verify/item?itemId=item-123&category=vaccination%20proof", route)
    }

    @Test
    fun `verification approved opens leadership closure from type-only payload`() {
        assertEquals(
            Routes.LEADERSHIP,
            resolvePushRoute(mapOf(PushExtras.TYPE to "verification_approved")),
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
