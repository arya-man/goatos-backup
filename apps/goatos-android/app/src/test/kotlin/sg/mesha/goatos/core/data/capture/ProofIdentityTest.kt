package sg.mesha.goatos.core.data.capture

import org.junit.Test
import org.junit.Assert.assertEquals

class ProofIdentityTest {
    @Test
    fun `storageKey vaccination with whole partition has double colon between task and subject`() {
        val identity = ProofIdentity(
            flow = ProofFlow.VACCINATION,
            taskId = "task-1",
            partitionKey = "whole",
            subjectKey = "goat-123",
        )
        // When partition is "whole", the intermediate colon is omitted but the surrounding colons remain,
        // creating the pattern task::subject
        assertEquals("vaccination:task-1::goat-123", identity.storageKey())
    }

    @Test
    fun `storageKey vaccination with partition produces key with partition segment`() {
        val identity = ProofIdentity(
            flow = ProofFlow.VACCINATION,
            taskId = "task-1",
            partitionKey = "shed-a",
            subjectKey = "goat-123",
        )
        assertEquals("vaccination:task-1::shed-a:goat-123", identity.storageKey())
    }

    @Test
    fun `submissionScopeKey omits partition when includePartition is false`() {
        val identity = ProofIdentity(
            flow = ProofFlow.GENERIC_SUBMIT,
            taskId = "task-1",
            scopeId = "shed-1",
            partitionKey = "whole",
            rowVersion = 1,
        )
        assertEquals("shed-submit:task-1:scope:shed-1:rv:1", identity.submissionScopeKey(includePartition = false))
    }

    @Test
    fun `submissionScopeKey with whole partition includes partition segment when includePartition true`() {
        val identity = ProofIdentity(
            flow = ProofFlow.GENERIC_SUBMIT,
            taskId = "task-1",
            scopeId = "shed-1",
            partitionKey = "whole",
            rowVersion = 1,
        )
        // Key fix: partition segment is now emitted unconditionally when includePartition=true,
        // even when partitionKey="whole". This matches the old wire format.
        assertEquals("shed-submit:task-1:scope:shed-1:partition:whole:rv:1", identity.submissionScopeKey(includePartition = true))
    }

    @Test
    fun `submissionScopeKey with named partition includes partition segment when includePartition true`() {
        val identity = ProofIdentity(
            flow = ProofFlow.GENERIC_SUBMIT,
            taskId = "task-1",
            scopeId = "shed-1",
            partitionKey = "shed-a",
            rowVersion = 1,
        )
        assertEquals("shed-submit:task-1:scope:shed-1:partition:shed-a:rv:1", identity.submissionScopeKey(includePartition = true))
    }
}
