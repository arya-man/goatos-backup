package sg.mesha.goatos.push

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PushTargetResolverTest {
    @Test
    fun `shed reminder opens shed-wide scan route without inventing task id`() {
        val route = resolvePushRoute(
            mapOf(
                PushExtras.TYPE to "reminder",
                PushExtras.SCREEN to "scan",
                PushExtras.SHED_ID to "shed-123",
            ),
        )

        assertTrue(route.startsWith("/scan?"))
        assertTrue(route.contains("shedId=shed-123"))
        assertFalse(route.contains("taskId="))
    }

    @Test
    fun `explicit scan target opens shed-wide scan route without task id`() {
        val route = resolvePushRoute(mapOf(PushExtras.TARGET to "scan/shed-123"))

        assertTrue(route.startsWith("/scan?"))
        assertTrue(route.contains("shedId=shed-123"))
        assertFalse(route.contains("taskId="))
    }
}
