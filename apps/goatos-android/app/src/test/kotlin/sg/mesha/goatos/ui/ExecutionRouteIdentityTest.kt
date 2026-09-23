package sg.mesha.goatos.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.feature.sheds.ShedRow
import sg.mesha.goatos.feature.sheds.ShedStatus
import java.nio.file.Path
import kotlin.io.path.readText

class ExecutionRouteIdentityTest {
    @Test
    fun `scan and submit routes preserve selected execution identity`() {
        val routes = listOf(
            Routes.scanRoute("shed A", "drive-a", "assignment-a", "batch-a", "task-a", "sop-a", 7, "Gandhi 1 - Part 3", "Part 3"),
            Routes.submitRoute("shed A", "drive-a", "assignment-a", "batch-a", "task-a", "sop-a", 7, "Gandhi 1 - Part 3", "Part 3"),
        )

        routes.forEach { route ->
            assertTrue(route.contains("shedId=shed%20A"))
            assertTrue(route.contains("driveId=drive-a"))
            assertTrue(route.contains("assignmentId=assignment-a"))
            assertTrue(route.contains("batchId=batch-a"))
            assertTrue(route.contains("taskId=task-a"))
            assertTrue(route.contains("sopVersionId=sop-a"))
            assertTrue(route.contains("taskRowVersion=7"))
            assertTrue(route.contains("scanTitle=Gandhi%201%20-%20Part%203"))
            assertTrue(route.contains("partitionLabel=Part%203"))
        }
    }

    @Test
    fun `scan to submit navigation falls back to scan state partition identity`() {
        val navHost = Path.of("src/main/kotlin/sg/mesha/goatos/ui/AppNavHost.kt").readText()
        val scanRoute = navHost.substringAfter("route = executionRoutePattern(Routes.SCAN)")
            .substringBefore("ScanEvent.Back -> navController.popBackStack()")

        assertTrue(scanRoute.contains("?: state.partitionLabel"))
    }

    @Test
    fun `route never invents absent task identity`() {
        val route = Routes.scanRoute("shed-a")
        assertFalse(route.contains("taskId="))
        assertFalse(route.contains("taskRowVersion="))
    }

    @Test
    fun `mixed batch assignment card tap carries assignment scope`() {
        val row = executableRow(taskId = "task-from-old-batch")

        val route = shedExecutionRoute(row, Routes.VACCINATION)

        assertTrue(route.startsWith(Routes.SCAN))
        assertTrue(route.contains("assignmentId=assignment-current"))
        assertTrue(route.contains("taskId=task-from-old-batch"))
        assertTrue(route.contains("batchId=batch-old"))
    }

    @Test
    fun `open assignment without task fails closed to record`() {
        val route = shedExecutionRoute(executableRow(taskId = null), Routes.VACCINATION)

        assertTrue(route.startsWith(Routes.RECORD))
        assertFalse(route.contains("assignmentId=assignment-current"))
        assertFalse(route.startsWith(Routes.SCAN))
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

    private fun executableRow(taskId: String?) = ShedRow(
        id = "shed:shed-a|partition:3",
        name = "Yashoda 3",
        animalStage = "kid",
        status = ShedStatus.PENDING,
        statusLabel = "In progress",
        vaccineGroups = emptyList(),
        inShed = "2",
        due = "2",
        done = "0",
        progressLabel = "0%",
        progressFraction = 0f,
        shedId = "shed-a",
        assignmentId = "assignment-current",
        batchId = "batch-old",
        taskId = taskId,
        partitionLabel = "Part 3",
    )
}
