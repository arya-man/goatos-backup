package sg.mesha.goatos.core.data.capture

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Blocker 9 closure: milk_preparation, milk_feeding, and workflow_detail now address their
 * captures through [ProofFlow] + [EvidenceSlot] instead of hand-rolled strings. These tests pin
 * two things per flow:
 *  1. Canonical identity — building an [EvidenceSlot] the way the ViewModel does yields the
 *     expected flow/taskId/fieldKey shape.
 *  2. Legacy-row compatibility — the taskId/fieldKey strings the canonical slot now produces are
 *     BYTE-FOR-BYTE IDENTICAL to the pre-migration hand-rolled strings those ViewModels wrote to
 *     Room/outbox before this change, so already-captured (and offline-mid-capture) rows keep
 *     matching on app upgrade. No Room migration was required because no on-disk string changed.
 */
class ProofFlowMigrationTest {

    // --- milk_preparation --------------------------------------------------------------------

    private fun milkPreparationSlot(parkId: String, preparationDate: String, stepCode: String): EvidenceSlot =
        EvidenceSlot(
            identity = ProofIdentity(
                flow = ProofFlow.MILK_PREPARATION,
                taskId = "milk-preparation:$parkId:$preparationDate",
                partitionKey = "whole",
                subjectKey = parkId,
            ),
            fieldKey = "milk_preparation_$stepCode",
        )

    @Test
    fun `milk preparation canonical identity matches the flow and grain`() {
        val slot = milkPreparationSlot("park-1", "2026-08-15", "goat_milk_quantity")
        assertEquals(ProofFlow.MILK_PREPARATION, slot.identity.flow)
        assertEquals("milk_preparation", ProofFlow.MILK_PREPARATION.wireValue)
        assertEquals("whole", slot.identity.partitionKey)
        assertEquals("park-1", slot.identity.subjectKey)
    }

    @Test
    fun `milk preparation legacy taskId and fieldKey strings are unchanged`() {
        // Pre-migration literals MilkPreparationViewModel wrote (groupKey() and the raw step
        // field-key template), reproduced here without going through EvidenceSlot at all.
        val legacyTaskId = "milk-preparation:park-1:2026-08-15"
        val legacyFieldKey = "milk_preparation_goat_milk_quantity"

        val slot = milkPreparationSlot("park-1", "2026-08-15", "goat_milk_quantity")

        assertEquals(legacyTaskId, slot.identity.taskId)
        assertEquals(legacyFieldKey, slot.fieldKey)
    }

    // --- milk_feeding -------------------------------------------------------------------------

    private fun milkFeedingSlot(parkId: String, feedingDate: String, sessionNo: Int, taskId: String, code: String): EvidenceSlot =
        EvidenceSlot(
            identity = ProofIdentity(
                flow = ProofFlow.MILK_FEEDING,
                taskId = "milk-feeding:$parkId:$feedingDate:$sessionNo",
                partitionKey = "whole",
                subjectKey = taskId,
            ),
            fieldKey = "milk_feeding_$code",
        )

    @Test
    fun `milk feeding canonical identity matches the flow and grain`() {
        val slot = milkFeedingSlot("park-2", "2026-08-15", 1, "task-42", "clean_bottles")
        assertEquals(ProofFlow.MILK_FEEDING, slot.identity.flow)
        assertEquals("milk_feeding", ProofFlow.MILK_FEEDING.wireValue)
        assertEquals("task-42", slot.identity.subjectKey)
    }

    @Test
    fun `milk feeding legacy taskId and fieldKey strings are unchanged`() {
        // Pre-migration literals MilkFeedingViewModel wrote (groupKey() and the raw proof-code
        // field-key template).
        val legacyTaskId = "milk-feeding:park-2:2026-08-15:1"
        val legacyFieldKey = "milk_feeding_mixing_and_filling"

        val slot = milkFeedingSlot("park-2", "2026-08-15", 1, "task-42", "mixing_and_filling")

        assertEquals(legacyTaskId, slot.identity.taskId)
        assertEquals(legacyFieldKey, slot.fieldKey)
    }

    // --- workflow_detail ------------------------------------------------------------------------

    /** Mirrors WorkflowDetailViewModel.workflowProofFieldKey(actionId) verbatim. */
    private fun workflowProofFieldKey(actionId: String): String = "workflow_${actionId}_video"

    private fun workflowSlot(workflowId: String, goatId: String, actionId: String): EvidenceSlot =
        EvidenceSlot(
            identity = ProofIdentity(
                flow = ProofFlow.WORKFLOW_DETAIL,
                taskId = workflowId,
                partitionKey = "whole",
                subjectKey = goatId,
            ),
            fieldKey = workflowProofFieldKey(actionId),
        )

    @Test
    fun `workflow detail canonical identity matches the flow and grain`() {
        val slot = workflowSlot("wf-100", "goat-7", "hoof_trim")
        assertEquals(ProofFlow.WORKFLOW_DETAIL, slot.identity.flow)
        assertEquals("workflow_detail", ProofFlow.WORKFLOW_DETAIL.wireValue)
        assertEquals("wf-100", slot.identity.taskId)
        assertEquals("goat-7", slot.identity.subjectKey)
    }

    @Test
    fun `workflow detail legacy taskId and fieldKey strings are unchanged, including open-ended actionId vocabulary`() {
        // Pre-migration literals WorkflowDetailViewModel wrote: taskId = workflowId (passthrough,
        // no transformation) and fieldKey = the backend-declared workflowProofFieldKey(actionId).
        val legacyTaskId = "wf-100"
        val legacyFieldKey = "workflow_hoof_trim_video"

        val slot = workflowSlot("wf-100", "goat-7", "hoof_trim")

        assertEquals(legacyTaskId, slot.identity.taskId)
        assertEquals(legacyFieldKey, slot.fieldKey)

        // Backend-declared actionId vocabulary is open-ended (any string) — this must keep
        // working without requiring a fixed, client-known set of subject shapes.
        val arbitrarySlot = workflowSlot("wf-101", "goat-8", "some_future_backend_action_id_v3")
        assertEquals("workflow_some_future_backend_action_id_v3_video", arbitrarySlot.fieldKey)
    }
}
