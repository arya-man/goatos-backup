package sg.mesha.goatos.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.nio.file.Path
import kotlin.io.path.readText

class ExecutionRouteIdentityTest {
    @Test
    fun `scan and submit routes preserve selected execution identity`() {
        val routes = listOf(
            Routes.scanRoute("shed A", "drive-a", "batch-a", "task-a", "sop-a", 7, "Gandhi 1 - Part 3", "Part 3"),
            Routes.submitRoute("shed A", "drive-a", "batch-a", "task-a", "sop-a", 7, "Gandhi 1 - Part 3", "Part 3"),
        )

        routes.forEach { route ->
            assertTrue(route.contains("shedId=shed%20A"))
            assertTrue(route.contains("driveId=drive-a"))
            assertTrue(route.contains("batchId=batch-a"))
            assertTrue(route.contains("taskId=task-a"))
            assertTrue(route.contains("sopVersionId=sop-a"))
            assertTrue(route.contains("taskRowVersion=7"))
            assertTrue(route.contains("scanTitle=Gandhi%201%20-%20Part%203"))
            assertTrue(route.contains("partitionLabel=Part%203"))
        }
    }

    @Test
    fun `route never invents absent task identity`() {
        val route = Routes.scanRoute("shed-a")
        assertFalse(route.contains("taskId="))
        assertFalse(route.contains("taskRowVersion="))
    }

    @Test
    fun `record route preserves selected partition`() {
        val route = Routes.recordRoute("shed-a", "Part 2")

        assertTrue(route.contains("shedId=shed-a"))
        assertTrue(route.contains("partitionLabel=Part%202"))
    }

    @Test
    fun `notification rework targets preserve sibling partitions`() {
        val partOne = pushTargetRoute("/vaccination/record/shed-a?partition_label=Part+1")
        val partTwo = pushTargetRoute("/vaccination/record/shed-a?partition_label=Part+2")

        assertEquals(Routes.recordRoute("shed-a", "Part 1"), partOne)
        assertEquals(Routes.recordRoute("shed-a", "Part 2"), partTwo)
        assertNotEquals(partOne, partTwo)
    }

    @Test
    fun `vaccination route remains shed execution gated by execute flag`() {
        val navHost = Path.of("src/main/kotlin/sg/mesha/goatos/ui/AppNavHost.kt").readText()
        val vaccinationRoute = navHost.substringAfter("composable(Routes.VACCINATION)")
            .substringBefore("ShedsEvent.Back -> Unit")

        assertTrue(vaccinationRoute.contains("ShedsScreen("))
        assertTrue(vaccinationRoute.contains("if (!canExecuteVaccination)"))
        assertTrue(vaccinationRoute.contains("shedExecutionRoute(selected, Routes.VACCINATION)"))
        assertFalse(vaccinationRoute.contains("Leadership"))
    }

    @Test
    fun `vaccination stock route opens inventory category worklist`() {
        assertEquals("/vaccination/stock", Routes.VACCINATION_STOCK)
        assertEquals("/pc/inventory-vaccine", Routes.PC_INVENTORY_VACCINE)

        val navHost = Path.of("src/main/kotlin/sg/mesha/goatos/ui/AppNavHost.kt").readText()
        val stockBinding =
            """pcCareCategoryComposable\(\s*Routes\.VACCINATION_STOCK,\s*"inventory_vaccine",\s*"Stock"""".toRegex()
        val binding = """pcCareCategoryComposable\(Routes\.PC_INVENTORY_VACCINE,\s*"inventory_vaccine",\s*"Vaccine Stock"""".toRegex()

        assertTrue(
            "The backend bootstrap href /vaccination/stock must stay wired to the inventory_vaccine task category.",
            stockBinding.containsMatchIn(navHost),
        )
        assertTrue(
            "The legacy /pc/inventory-vaccine route stays wired as a compatibility route.",
            binding.containsMatchIn(navHost),
        )
    }
}
