package sg.mesha.goatos.viewmodel

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.feature.pccare.PcCareSlotState

/**
 * PC Care slots are PARALLEL by contract: each slot chip's UI state derives ONLY from its own
 * capture rows plus the task lifecycle lock — capturing, removing, or syncing one slot must
 * never change a sibling slot's enabled state, and any capture order is accepted.
 */
class PcCareSlotParallelismTest {
    private val json = Json { ignoreUnknownKeys = true }
    private val detail = pcCareTaskDtoFixture()

    private fun proofRow(
        fieldKey: String,
        syncStatus: CaptureSyncStatus = CaptureSyncStatus.PENDING,
        serverProofId: String? = null,
    ): ProofCaptureRow = ProofCaptureRow(
        id = "proof-$fieldKey",
        fieldKey = fieldKey,
        proofSubject = ProofSubject.OTHER,
        subjectId = fieldKey.substringBefore(':'),
        localUri = "file:///$fieldKey.mp4",
        mimeType = "video/mp4",
        caption = null,
        capturedAtMs = 1L,
        capturedStartMs = 0L,
        capturedEndMs = 1_000L,
        capturedByPrincipalId = null,
        syncStatus = syncStatus,
        serverProofId = serverProofId,
        lastError = null,
    )

    @Test
    fun `capturing one slot leaves sibling slots recordable`() {
        val animals = listOf(pcCareAnimalEntity(tag = "t1"))
        // The DURING slot's camera is open right now.
        val uis = pcCareBuildAnimalUis(
            expectedSlots = detail.expectedSlots,
            animals = animals,
            proofs = emptyList(),
            capturingSlotKey = pcCareSlotProofFieldKey("t1", "during_video"),
            json = json,
        )
        val slots = uis.single().slots.associateBy { it.fieldKey }
        assertEquals(PcCareSlotState.WORKING, slots.getValue("during_video").state)
        assertFalse(slots.getValue("during_video").canRecord)
        // Siblings are untouched: still empty AND still recordable.
        assertEquals(PcCareSlotState.EMPTY, slots.getValue("before_video").state)
        assertTrue(slots.getValue("before_video").canRecord)
        assertEquals(PcCareSlotState.EMPTY, slots.getValue("after_video").state)
        assertTrue(slots.getValue("after_video").canRecord)
    }

    @Test
    fun `one slot's synced upload never changes a sibling's state`() {
        val animals = listOf(pcCareAnimalEntity(tag = "t1"))
        val withoutAfter = pcCareBuildAnimalUis(
            detail.expectedSlots,
            animals,
            proofs = listOf(
                proofRow(pcCareSlotProofFieldKey("t1", "during_video"), CaptureSyncStatus.SYNCED, "sp-1"),
            ),
            capturingSlotKey = null,
            json = json,
        ).single().slots.associateBy { it.fieldKey }
        val withAfter = pcCareBuildAnimalUis(
            detail.expectedSlots,
            animals,
            proofs = listOf(
                proofRow(pcCareSlotProofFieldKey("t1", "during_video"), CaptureSyncStatus.SYNCED, "sp-1"),
                proofRow(pcCareSlotProofFieldKey("t1", "after_video"), CaptureSyncStatus.PENDING),
            ),
            capturingSlotKey = null,
            json = json,
        ).single().slots.associateBy { it.fieldKey }

        // The DURING slot reads synced in both worlds.
        assertEquals(PcCareSlotState.SYNCED, withoutAfter.getValue("during_video").state)
        assertEquals(PcCareSlotState.SYNCED, withAfter.getValue("during_video").state)
        // Adding/removing the AFTER slot's capture changes ONLY the AFTER chip.
        assertEquals(PcCareSlotState.EMPTY, withoutAfter.getValue("after_video").state)
        assertEquals(PcCareSlotState.WORKING, withAfter.getValue("after_video").state)
        assertEquals(
            withoutAfter.getValue("before_video"),
            withAfter.getValue("before_video"),
        )
    }

    @Test
    fun `any capture order is accepted — after then before then during submits the same`() {
        val animals = listOf(pcCareAnimalEntity(tag = "t1"))
        val orderings = listOf(
            listOf("before_video", "during_video", "after_video"),
            listOf("after_video", "before_video", "during_video"),
            listOf("during_video", "after_video", "before_video"),
        )
        orderings.forEach { order ->
            val proofs = order.map { slot ->
                proofRow(pcCareSlotProofFieldKey("t1", slot), CaptureSyncStatus.SYNCED, "sp-$slot")
            }
            val evaluation = pcCareEvaluateSubmit(detail.expectedSlots, animals, proofs, json)
            assertTrue("order $order should be submittable", evaluation.ready)
        }
    }

    @Test
    fun `during slot's duration hint is guidance text and never gates the chip`() {
        val animals = listOf(pcCareAnimalEntity(tag = "t1"))
        val slots = pcCareBuildAnimalUis(detail.expectedSlots, animals, emptyList(), null, json)
            .single().slots.associateBy { it.fieldKey }
        assertEquals("Record at least 10 seconds", slots.getValue("during_video").hintLabel)
        assertTrue(slots.getValue("during_video").canRecord)
        assertEquals("", slots.getValue("before_video").hintLabel)
    }
}
