package sg.mesha.goatos.core.data.sync

import kotlinx.serialization.encodeToString
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Pins the pen-visit outbox key formats (PenVisitPayloads.kt owns every one of them — a second
 * call site rebuilding a key is the banned shape, see SubmittedGrainKeys.kt's header) and the
 * list-badge grain the projection and the list ViewModel both derive from.
 */
class PenVisitSubmitKeyTest {

    @Test
    fun `submit idempotency key is task plus row version and stable`() {
        assertEquals("pen-visit:submit:task-9:3", penVisitSubmitIdempotencyKey("task-9", 3))
        assertEquals(penVisitSubmitIdempotencyKey("task-9", 3), penVisitSubmitIdempotencyKey("task-9", 3))
        // A submit after the task moved (bumped row version) is a genuinely new key.
        assertEquals("pen-visit:submit:task-9:4", penVisitSubmitIdempotencyKey("task-9", 4))
    }

    @Test
    fun `group key is one lane per visit`() {
        assertEquals("pen-visit:task:task-9", penVisitTaskGroupKey("task-9"))
    }

    @Test
    fun `the list badge grain is projected from the payload the row was dispatched with`() {
        val payload = syncJson.encodeToString(
            PenVisitSubmitPayload(taskId = "task-9", rowVersion = 3, proofOutboxItemId = "proof-row-1"),
        )
        assertEquals(penVisitGrainKey("task-9"), submittedGrainKeyOf("PEN_VISIT_SUBMIT", payload, syncJson))
    }
}
