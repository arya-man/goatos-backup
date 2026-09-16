package sg.mesha.goatos.core.data.sync

import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import sg.mesha.goatos.core.network.dto.PenRoutineIntegrityDto
import sg.mesha.goatos.core.network.dto.PenRoutineLocationDto

/**
 * Pins the pen-routine outbox key formats (PenRoutinePayloads.kt owns every one of them — a
 * second call site rebuilding a key is the banned shape, see SubmittedGrainKeys.kt's header)
 * and the list-badge grain the projection and the ViewModels both derive from.
 */
class PenRoutineKeyTest {

    @Test
    fun `submit and presence keys are task plus row version and stable`() {
        assertEquals("pen-routine:submit:task-9:3", penRoutineSubmitIdempotencyKey("task-9", 3))
        assertEquals(penRoutineSubmitIdempotencyKey("task-9", 3), penRoutineSubmitIdempotencyKey("task-9", 3))
        // A write after the task moved (bumped row version) is a genuinely new key.
        assertEquals("pen-routine:submit:task-9:4", penRoutineSubmitIdempotencyKey("task-9", 4))
        assertEquals("pen-routine:presence:task-9:enter:3", penRoutinePresenceIdempotencyKey("task-9", "enter", 3))
    }

    @Test
    fun `group key is one lane per task and slot keys are one per slot`() {
        assertEquals("pen-routine:task:task-9", penRoutineTaskGroupKey("task-9"))
        assertEquals("routine-photo-1", penRoutinePhotoFieldKey(1))
        assertEquals("routine-video-2", penRoutineVideoFieldKey(2))
    }

    @Test
    fun `the list badge grain is projected from the payload the row was dispatched with`() {
        val payload = syncJson.encodeToString(
            PenRoutineSubmitPayload(
                taskId = "task-9",
                rowVersion = 3,
                answers = JsonObject(mapOf("cleaned" to JsonPrimitive("yes"))),
                proofs = listOf(PenRoutineSubmitProof(proofOutboxItemId = "proof-row-1", kind = "photo")),
            ),
        )
        assertEquals(penRoutineGrainKey("task-9", 3), submittedGrainKeyOf("PEN_ROUTINE_SUBMIT", payload, syncJson))
        // A presence punch carries no list badge: the card is not "sending" while checking in.
        val presence = syncJson.encodeToString(
            PenRoutinePresencePayload(
                taskId = "task-9",
                rowVersion = 3,
                eventType = "enter",
                capturedAt = "2026-09-16T01:30:00Z",
                location = PenRoutineLocationDto(status = "captured"),
                integrity = PenRoutineIntegrityDto(),
            ),
        )
        assertNull(submittedGrainKeyOf("PEN_ROUTINE_PRESENCE", presence, syncJson))
    }
}
