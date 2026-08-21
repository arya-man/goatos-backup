package sg.mesha.goatos.core.data.sync

import kotlinx.serialization.encodeToString
import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.data.normalizePcCareTag
import sg.mesha.goatos.core.network.dto.PcCareTaskDto

/**
 * Pins the PC Care outbox key formats (PcCarePayloads.kt owns every one of them — a second call
 * site rebuilding a key is the banned shape, see SubmittedGrainKeys.kt's header).
 */
class PcCareSubmitKeyTest {

    @Test
    fun `submit idempotency key is task plus row version and stable`() {
        assertEquals("pc-care:submit:task-9:rv:3", pcCareSubmitIdempotencyKey("task-9", 3))
        // Stable: the same inputs always produce the same key (a retry replays for free) …
        assertEquals(
            pcCareSubmitIdempotencyKey("task-9", 3),
            pcCareSubmitIdempotencyKey("task-9", 3),
        )
        // … while a post-rework re-submit (bumped row version) is a genuinely new key.
        assertEquals("pc-care:submit:task-9:rv:4", pcCareSubmitIdempotencyKey("task-9", 4))
    }

    @Test
    fun `scan idempotency key is stable per normalized tag`() {
        val first = pcCareScanIdempotencyKey("task-1", normalizePcCareTag("  RF-042 "))
        val again = pcCareScanIdempotencyKey("task-1", normalizePcCareTag("rf-042"))
        assertEquals("pc-care:scan:task-1:rf-042", first)
        // The same physical tag scanned twice — whatever the whitespace/case the reader emitted —
        // maps to ONE key, so a double-tap can never create two server rows.
        assertEquals(first, again)
    }

    @Test
    fun `slot registration key names task tag slot and proof row`() {
        assertEquals(
            "pc-care:slot:task-1:rf-042:before_video:proof-row-7",
            pcCareSlotIdempotencyKey("task-1", "rf-042", "before_video", "proof-row-7"),
        )
    }

    @Test
    fun `group keys keep scans off the video lane but submits on it`() {
        assertEquals("pc-care:task:task-1", pcCareTaskGroupKey("task-1"))
        assertEquals("pc-care:scan:task-1", pcCareScanGroupKey("task-1"))
    }

    @Test
    fun `submitted grain key projects identically from the payload and from the row`() {
        // Both sides of the "In review" badge must derive the SAME key: the outbox projection
        // (submittedGrainKeyOf over the queued payload) and the list row's own key.
        val payloadJson = syncJson.encodeToString(PcCareTaskSubmitPayload(taskId = "task-1", rowVersion = 2))
        val projected = submittedGrainKeyOf("PC_CARE_TASK_SUBMIT", payloadJson, syncJson)
        val row = PcCareTaskDto(
            taskId = "task-1",
            category = "deworming",
            parkId = "park-1",
            parkLabel = "CPT",
            shedId = "shed-1",
            shedLabel = "Castro",
            plannedBusinessDate = "2026-08-21",
            dueBusinessDate = "2026-08-21",
            workState = "scheduled",
            status = "open",
            rowVersion = 2,
        )
        assertEquals(row.submittedGrainKey(), projected)
        assertEquals("task|pc-care|task-1", projected)
    }
}
