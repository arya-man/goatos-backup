package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test
import sg.mesha.goatos.core.network.dto.VaccinationExecutionRowDto

class ShedsExecutionIdentityTest {

    @Test
    fun `one park task spanning two sheds produces distinct stable card keys`() {
        val gandhiOne = executionCardId("shed-gandhi-1", "task-park", "batch-park", "drive-park")
        val gandhiTwo = executionCardId("shed-gandhi-2", "task-park", "batch-park", "drive-park")

        assertEquals("shed:shed-gandhi-1|task:task-park", gandhiOne)
        assertEquals(gandhiOne, executionCardId("shed-gandhi-1", "task-park", "batch-park", "drive-park"))
        assertNotEquals(gandhiOne, gandhiTwo)
    }

    @Test
    fun `shed key falls back through batch drive and shed identity`() {
        assertEquals("shed:shed-1|batch:batch-1", executionCardId("shed-1", null, "batch-1", "drive-1"))
        assertEquals("shed:shed-1|drive:drive-1", executionCardId("shed-1", null, null, "drive-1"))
        assertEquals("shed:shed-1", executionCardId("shed-1", null, null, null))
    }

    @Test
    fun `shed totals use backend animal counts instead of aggregated row count`() {
        val counts = executionCounts(
            listOf(
                VaccinationExecutionRowDto(targetCount = 2, openCount = 2, doneCount = 0),
                VaccinationExecutionRowDto(targetCount = 3, openCount = 1, doneCount = 2),
            ),
        )

        assertEquals(ExecutionCounts(target = 5, open = 3, done = 2), counts)
    }
}
