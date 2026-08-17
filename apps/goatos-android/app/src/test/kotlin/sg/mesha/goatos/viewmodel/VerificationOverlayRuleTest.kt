package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.data.sync.shedSessionKey
import sg.mesha.goatos.core.data.sync.taskGrainKey

/**
 * ONE rule now serves every verifier-gated flow, so this is the only place its precedence is
 * asserted. It replaces four near-identical test files that each restated the same `when {}` for
 * their own flow — the duplication that let Feed Packing and Feed Direction drift apart.
 *
 * Grain scoping is asserted against the PRODUCTION key builders, so a row can only be flipped by a
 * submit for that exact grain.
 */
class VerificationOverlayRuleTest {

    private val submitted = setOf(
        shedSessionKey("2026-08-17", "shed-castro", "2", 1, "experiment"),
        taskGrainKey("feed-transport", "task-1"),
    )

    private fun render(
        backendStatus: String,
        key: String,
        reworkReason: String? = null,
        inReviewToken: String = IN_REVIEW_PENDING_VERIFICATION,
    ) = overlayVerificationStatus(
        backendStatus = backendStatus,
        reworkReason = reworkReason,
        isLocallySubmitted = submitted.contains(key),
        inReviewToken = inReviewToken,
    )

    private fun packing(partition: String?, session: Int, workflow: String = "experiment", date: String = "2026-08-17") =
        shedSessionKey(date, "shed-castro", partition, session, workflow)

    @Test
    fun `REGRESSION - a queued submit renders in review while the backend still says pending`() {
        assertEquals("pending_verification", render("pending", packing("2", 1)))
    }

    @Test
    fun `COMPLETED wins - server acceptance is never downgraded by a local hint`() {
        assertEquals("completed", render("completed", packing("2", 1)))
    }

    @Test
    fun `REWORK wins - a rejection is never masked by a local hint`() {
        assertEquals(
            "a reworked line comes back as pending WITH a reason; overlaying it hides the rework",
            "pending",
            render("pending", packing("2", 1), reworkReason = "Bag count short"),
        )
    }

    @Test
    fun `each flow keeps its own in-review vocabulary`() {
        assertEquals(
            "verification_due",
            render("due", taskGrainKey("feed-transport", "task-1"), inReviewToken = IN_REVIEW_VERIFICATION_DUE),
        )
    }

    @Test
    fun `GRAIN_SCOPE - a submit only flips its exact grain`() {
        assertEquals("pending_verification", render("pending", packing("2", 1)))
        assertEquals("different partition", "pending", render("pending", packing("1", 1)))
        assertEquals("different session", "pending", render("pending", packing("2", 2)))
        assertEquals("different workflow", "pending", render("pending", packing("2", 1, workflow = "normal")))
        assertEquals("different day", "pending", render("pending", packing("2", 1, date = "2026-08-18")))
        assertEquals(
            "another task id",
            "due",
            render("due", taskGrainKey("feed-transport", "task-2"), inReviewToken = IN_REVIEW_VERIFICATION_DUE),
        )
    }

    @Test
    fun `NAMESPACE - the same id in a different flow is a different grain`() {
        assertEquals("not_submitted", render("not_submitted", taskGrainKey("milk-feeding", "task-1")))
    }

    @Test
    fun `an unsubmitted row is passed through untouched`() {
        assertEquals("pending", render("pending", packing("9", 9)))
    }
}
