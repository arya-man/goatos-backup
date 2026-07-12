package sg.mesha.goatos.ui

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ExecutionRouteIdentityTest {
    @Test
    fun `scan and submit routes preserve selected execution identity`() {
        val routes = listOf(
            Routes.scanRoute("shed A", "drive-a", "batch-a", "task-a", "sop-a", 7),
            Routes.submitRoute("shed A", "drive-a", "batch-a", "task-a", "sop-a", 7),
        )

        routes.forEach { route ->
            assertTrue(route.contains("shedId=shed%20A"))
            assertTrue(route.contains("driveId=drive-a"))
            assertTrue(route.contains("batchId=batch-a"))
            assertTrue(route.contains("taskId=task-a"))
            assertTrue(route.contains("sopVersionId=sop-a"))
            assertTrue(route.contains("taskRowVersion=7"))
        }
    }

    @Test
    fun `route never invents absent task identity`() {
        val route = Routes.scanRoute("shed-a")
        assertFalse(route.contains("taskId="))
        assertFalse(route.contains("taskRowVersion="))
    }
}
