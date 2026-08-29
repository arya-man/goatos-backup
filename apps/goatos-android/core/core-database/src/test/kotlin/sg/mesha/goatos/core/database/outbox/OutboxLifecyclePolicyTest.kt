package sg.mesha.goatos.core.database.outbox

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Test

class OutboxLifecyclePolicyTest {
    @Test
    fun `every outbox operation resolves a complete lifecycle policy`() {
        val policies = OutboxOpType.entries.associateWith { it.lifecyclePolicy }

        assertEquals(OutboxOpType.entries.toSet(), policies.keys)
        policies.forEach { (opType, policy) ->
            assertNotNull("$opType immediate UI policy", policy.immediate)
            assertNotNull("$opType success policy", policy.success)
            assertNotNull("$opType terminal-failure policy", policy.terminalFailure)
            assertNotNull("$opType process-death policy", policy.processDeath)
        }
    }

    @Test
    fun `background writes delegate visible state instead of inventing their own`() {
        val backgroundPolicies = OutboxOpType.entries
            .associateWith { it.lifecyclePolicy }
            .filterValues { it.userImpact == OutboxUserImpact.BACKGROUND_SUPPORT_WRITE }

        assertEquals(
            setOf(OutboxOpType.SCAN_ATTEMPT, OutboxOpType.PROOF_UPLOAD, OutboxOpType.VERIFICATION_REVIEW_EVENTS),
            backgroundPolicies.keys,
        )
        assertEquals(
            OutboxImmediateUiPolicy.NO_USER_VISIBLE_STATE,
            backgroundPolicies.getValue(OutboxOpType.SCAN_ATTEMPT).immediate,
        )
        assertEquals(
            OutboxImmediateUiPolicy.PARENT_OPERATION_STATUS,
            backgroundPolicies.getValue(OutboxOpType.PROOF_UPLOAD).immediate,
        )
        assertEquals(
            OutboxImmediateUiPolicy.NO_USER_VISIBLE_STATE,
            backgroundPolicies.getValue(OutboxOpType.VERIFICATION_REVIEW_EVENTS).immediate,
        )
    }
}
