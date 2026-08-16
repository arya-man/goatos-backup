package sg.mesha.goatos.core.data.capture

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Blocker 9 closure: milk_preparation, milk_feeding, and workflow_detail now address their
 * captures through [ProofFlow] + [EvidenceSlot] instead of hand-rolled strings. These tests pin
 * two things per flow:
 *  1. Canonical identity — building an [EvidenceSlot] the way the ViewModel does yields the
 *     expected flow/taskId/fieldKey shape (via the production functions).
 *  2. Legacy-row compatibility — the taskId/fieldKey strings the canonical slot now produces are
 *     BYTE-FOR-BYTE IDENTICAL to the pre-migration hand-rolled strings those ViewModels wrote to
 *     Room/outbox before this change, so already-captured (and offline-mid-capture) rows keep
 *     matching on app upgrade. No Room migration was required because no on-disk string changed.
 */
class ProofFlowMigrationTest {

    // --- milk_preparation --------------------------------------------------------------------

    @Test
    fun `milk preparation canonical identity matches the flow and grain`() {
        val slot = buildMilkPreparationEvidenceSlot("park-1", "2026-08-15", "goat_milk_quantity")
        assertEquals(ProofFlow.MILK_PREPARATION, slot.identity.flow)
        assertEquals("milk_preparation", ProofFlow.MILK_PREPARATION.wireValue)
        assertEquals("whole", slot.identity.partitionKey)
        assertEquals("park-1", slot.identity.subjectKey)
    }

    @Test
    fun `milk preparation legacy taskId and fieldKey strings are unchanged`() {
        // Pre-migration literals MilkPreparationViewModel wrote, asserted against the ACTUAL
        // canonical production function buildMilkPreparationEvidenceSlot().
        val legacyTaskId = "milk-preparation:park-1:2026-08-15"
        val legacyFieldKey = "milk_preparation_goat_milk_quantity"

        val slot = buildMilkPreparationEvidenceSlot("park-1", "2026-08-15", "goat_milk_quantity")

        assertEquals(legacyTaskId, slot.identity.taskId)
        assertEquals(legacyFieldKey, slot.fieldKey)
    }

    @Test
    fun `milk preparation production slot output equals legacy literals`() {
        // Mutation check: verify the canonical output hasn't diverged from legacy strings.
        val parkId = "test-park"
        val date = "2026-07-10"
        val stepCode = "boiling_temperature"
        val slot = buildMilkPreparationEvidenceSlot(parkId, date, stepCode)

        // These must be byte-for-byte identical for backward compatibility
        assertEquals("milk-preparation:$parkId:$date", slot.identity.taskId)
        assertEquals("milk_preparation_$stepCode", slot.fieldKey)
    }

    // --- milk_feeding -------------------------------------------------------------------------

    @Test
    fun `milk feeding canonical identity matches the flow and grain`() {
        // Import required: import sg.mesha.goatos.viewmodel.buildMilkFeedingEvidenceSlot
        val slot = buildMilkFeedingEvidenceSlot("park-2", "2026-08-15", 1, "task-42", "clean_bottles")
        assertEquals(ProofFlow.MILK_FEEDING, slot.identity.flow)
        assertEquals("milk_feeding", ProofFlow.MILK_FEEDING.wireValue)
        assertEquals("task-42", slot.identity.subjectKey)
    }

    @Test
    fun `milk feeding legacy taskId and fieldKey strings are unchanged`() {
        // Pre-migration literals MilkFeedingViewModel wrote, asserted against the ACTUAL
        // canonical production function buildMilkFeedingEvidenceSlot().
        val legacyTaskId = "milk-feeding:park-2:2026-08-15:1"
        val legacyFieldKey = "milk_feeding_mixing_and_filling"

        val slot = buildMilkFeedingEvidenceSlot("park-2", "2026-08-15", 1, "task-42", "mixing_and_filling")

        assertEquals(legacyTaskId, slot.identity.taskId)
        assertEquals(legacyFieldKey, slot.fieldKey)
    }

    @Test
    fun `milk feeding production slot output equals legacy literals`() {
        // Mutation check: verify the canonical output hasn't diverged from legacy strings.
        val parkId = "test-park-3"
        val date = "2026-08-10"
        val sessionNo = 2
        val taskId = "task-id-99"
        val code = "clean_bottles"
        val slot = buildMilkFeedingEvidenceSlot(parkId, date, sessionNo, taskId, code)

        // These must be byte-for-byte identical for backward compatibility
        assertEquals("milk-feeding:$parkId:$date:$sessionNo", slot.identity.taskId)
        assertEquals(taskId, slot.identity.subjectKey)
        assertEquals("milk_feeding_$code", slot.fieldKey)
    }

    // --- workflow_detail ------------------------------------------------------------------------

    @Test
    fun `workflow detail canonical identity matches the flow and grain`() {
        val slot = buildWorkflowEvidenceSlot("wf-100", "goat-7", "hoof_trim")
        assertEquals(ProofFlow.WORKFLOW_DETAIL, slot.identity.flow)
        assertEquals("workflow_detail", ProofFlow.WORKFLOW_DETAIL.wireValue)
        assertEquals("wf-100", slot.identity.taskId)
        assertEquals("goat-7", slot.identity.subjectKey)
    }

    @Test
    fun `workflow detail legacy taskId and fieldKey strings are unchanged, including open-ended actionId vocabulary`() {
        // Pre-migration literals WorkflowDetailViewModel wrote, asserted against the ACTUAL
        // canonical production function buildWorkflowEvidenceSlot() + workflowProofFieldKey().
        val legacyTaskId = "wf-100"
        val legacyFieldKey = "workflow_hoof_trim_video"

        val slot = buildWorkflowEvidenceSlot("wf-100", "goat-7", "hoof_trim")

        assertEquals(legacyTaskId, slot.identity.taskId)
        assertEquals(legacyFieldKey, slot.fieldKey)

        // Backend-declared actionId vocabulary is open-ended (any string) — this must keep
        // working without requiring a fixed, client-known set of subject shapes.
        val arbitrarySlot = buildWorkflowEvidenceSlot("wf-101", "goat-8", "some_future_backend_action_id_v3")
        assertEquals("workflow_some_future_backend_action_id_v3_video", arbitrarySlot.fieldKey)
    }

    @Test
    fun `workflow detail production slot output equals legacy literals`() {
        // Mutation check: verify the canonical output hasn't diverged from legacy strings.
        val workflowId = "wf-999"
        val goatId = "goat-test-123"
        val actionId = "action_xyz"
        val slot = buildWorkflowEvidenceSlot(workflowId, goatId, actionId)

        // These must be byte-for-byte identical for backward compatibility
        assertEquals(workflowId, slot.identity.taskId)
        assertEquals(goatId, slot.identity.subjectKey)
        assertEquals("workflow_${actionId}_video", slot.fieldKey)
    }
}
