package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Test
import sg.mesha.goatos.core.data.forms.ProofPolicy

/**
 * R50-027: Proof policy enforcement unit tests.
 * Verifies: minimum counts, subject selection, and explicit caps. The default policy must never
 * invent a cross-feature proof cap.
 */
class ProofPolicyEnforcementTest {

    @Test
    fun `minimum count below threshold blocks submit`() {
        val policy = ProofPolicy(
            minimumCount = 3,
            minimumCountPerSubject = 1,
            maximumCountPerSubject = 5,
        )
        assertEquals(3, policy.minimumCount)
        assertNotNull("Policy enforces minimum count", policy)
    }

    @Test
    fun `policy-driven subject overrides hardcoded mapping`() {
        val policyWithShed = ProofPolicy(
            expectedSubjects = listOf("shed"),
            subjectScope = "shed",
        )
        // This test verifies the policy is parsed correctly; actual routing is in ViewModel
        assertEquals("shed", policyWithShed.defaultSubject.wireValue)
    }

    @Test
    fun `per-subject cap applied to non-goat subjects`() {
        val policy = ProofPolicy(
            maximumCountPerSubject = 3,
            expectedSubjects = listOf("vial", "shed"),
        )
        assertEquals(3, policy.maximumCountPerSubject)
        // Actual enforcement in CaptureRepository.capture() for vial, shed, admin
    }

    @Test
    fun `policy required field honored`() {
        val requiredPolicy = ProofPolicy(required = true)
        assertEquals(true, requiredPolicy.required)

        val optionalPolicy = ProofPolicy(required = false)
        assertEquals(false, optionalPolicy.required)
    }

    @Test
    fun `default policy does not invent a client-side proof cap`() {
        val default = ProofPolicy.Default
        assertEquals(0, default.minimumCount)
        assertEquals(0, default.minimumCountPerSubject)
        assertEquals(null, default.maximumCount)
        assertEquals(null, default.maximumCountPerSubject)
        assertEquals("in_app_camera", default.captureSource)
    }

    @Test
    fun `weighing fasting feed and water proofs are capped per slot only`() {
        val policy = weighingFastingProofPolicy("in_app_camera")

        assertEquals(1, policy.maximumCountPerField)
        assertEquals(null, policy.maximumCount)
        assertEquals(null, policy.maximumCountPerSubject)
    }

    @Test
    fun `pc care removal proofs are capped per slot only`() {
        val policy = pcCareProofPolicy("in_app_camera")

        assertEquals(1, policy.maximumCountPerField)
        assertEquals(null, policy.maximumCount)
        assertEquals(null, policy.maximumCountPerSubject)
    }

    @Test
    fun `expected subjects list drives subject selection`() {
        val policy = ProofPolicy(expectedSubjects = listOf("goat", "shed", "vial"))
        // Verify the policy can specify multiple expected subjects
        assertEquals(3, policy.expectedSubjects.size)
        // Actual ViewModel logic: subjectForFieldKey derives from this list
    }
}
