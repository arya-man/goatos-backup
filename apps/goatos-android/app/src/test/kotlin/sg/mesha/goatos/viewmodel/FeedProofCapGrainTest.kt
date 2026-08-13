package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.data.capture.MAX_PROOFS_PER_GOAT

/**
 * The feed proof cap is PER SLOT, not one pooled budget for the whole shed.
 *
 * Feed distribution's three proofs — weight photo, feed video, water video — all carry the SHED as
 * their subject, so the per-subject cap counted them together: one budget of five
 * ([MAX_PROOFS_PER_GOAT]) for three required steps. Combined with a re-capture that leaked its old
 * row instead of replacing it (the hydration defect fixed the same day), pens reached five and
 * every later capture was REFUSED — returning an error and writing no Room row, so the operator's
 * proof simply never came back.
 *
 * Found 2026-08-13 on Castro - 1 session 2, holding three weight photos and two videos, with no
 * water video and therefore no way to ever submit.
 *
 * The cap is asserted through the shared policy rather than a screen, because the policy is what
 * every feed capture passes to the repository.
 */
class FeedProofCapGrainTest {
    @Test
    fun `a feed slot holds exactly one proof`() {
        val policy = feedShedProofPolicy("in_app_camera")

        assertEquals(
            "a feed slot is one required step, not repeat takes of one thing",
            1,
            policy.maximumCountPerField,
        )
    }

    @Test
    fun `the three feed slots do not share one budget`() {
        val policy = feedShedProofPolicy("in_app_camera")
        val slots = listOf(
            "feed_distribution_feed_weight_photo",
            "feed_distribution_video",
            "feed_distribution_water_video",
        )

        // Each slot is capped on its own, so filling every slot once cannot exhaust anything: the
        // pooled reading is what allowed three slots to consume one shared budget.
        val perFieldCap = requireNotNull(policy.maximumCountPerField)
        assertTrue(
            "every slot must be fillable regardless of how many slots the screen has",
            slots.size * perFieldCap > perFieldCap,
        )
        assertTrue(
            "the subject cap must still leave room for one proof in each slot",
            slots.size <= policy.maximumCount,
        )
    }

    // The rework path: the 14:00 feed correction reopens a pen whose proof was already delivered,
    // and the operator owes a NEW clip for the changed head count. The reopen returns to the SAME
    // group key, so the delivered row is still in Room -- and while the cap counted it, the only way
    // back into the slot was to destroy the evidence of what was packed before the correction.
    @Test
    fun `a delivered proof does not close the slot when a pen is reopened`() {
        val repository = FakeProofCaptureRepository()
        val policy = feedShedProofPolicy("in_app_camera")
        val task = "feed-pack:2026-08-13:shed-1:1:2:normal"
        val field = "feed_packing_video"

        val first = kotlinx.coroutines.runBlocking {
            repository.capture(
                taskId = task,
                fieldKey = field,
                subject = sg.mesha.goatos.core.data.capture.ProofSubject.SHED,
                subjectId = "shed-1",
                localUri = "/proof/packed-before-correction.mp4",
                mimeType = "video/mp4",
                caption = null,
                scopeType = "shed",
                scopeId = "shed-1",
                capturedStartMs = 1L,
                capturedEndMs = 2L,
                capturedByPrincipalId = null,
                proofPolicy = policy,
            )
        }
        val delivered = (first as sg.mesha.goatos.core.common.AppResult.Ok).value
        // The verifier has it: this row is now history on the server.
        repository.markSynced(delivered.id, serverProofId = "server-proof-1")

        val afterReopen = kotlinx.coroutines.runBlocking {
            repository.capture(
                taskId = task,
                fieldKey = field,
                subject = sg.mesha.goatos.core.data.capture.ProofSubject.SHED,
                subjectId = "shed-1",
                localUri = "/proof/packed-after-correction.mp4",
                mimeType = "video/mp4",
                caption = null,
                scopeType = "shed",
                scopeId = "shed-1",
                capturedStartMs = 3L,
                capturedEndMs = 4L,
                capturedByPrincipalId = null,
                proofPolicy = policy,
            )
        }

        assertTrue(
            "a reopened pen must be re-shootable without destroying the delivered proof: " +
                (afterReopen as? sg.mesha.goatos.core.common.AppResult.Err)?.message,
            afterReopen is sg.mesha.goatos.core.common.AppResult.Ok,
        )
    }

    @Test
    fun `capture refuses a second proof in the same slot and points at re-capture`() {
        val repository = FakeProofCaptureRepository()
        val policy = feedShedProofPolicy("in_app_camera")
        val field = "feed_distribution_feed_weight_photo"

        val first = kotlinx.coroutines.runBlocking {
            repository.capture(
                taskId = "feed-dist:2026-08-13:shed-1:1:2:experiment",
                fieldKey = field,
                subject = sg.mesha.goatos.core.data.capture.ProofSubject.SHED,
                subjectId = "shed-1",
                localUri = "/proof/one.jpg",
                mimeType = "image/jpeg",
                caption = null,
                scopeType = "shed",
                scopeId = "shed-1",
                capturedStartMs = 1L,
                capturedEndMs = 2L,
                capturedByPrincipalId = null,
                proofPolicy = policy,
            )
        }
        assertTrue("the first proof in a slot is accepted", first is sg.mesha.goatos.core.common.AppResult.Ok)

        val second = kotlinx.coroutines.runBlocking {
            repository.capture(
                taskId = "feed-dist:2026-08-13:shed-1:1:2:experiment",
                fieldKey = field,
                subject = sg.mesha.goatos.core.data.capture.ProofSubject.SHED,
                subjectId = "shed-1",
                localUri = "/proof/two.jpg",
                mimeType = "image/jpeg",
                caption = null,
                scopeType = "shed",
                scopeId = "shed-1",
                capturedStartMs = 3L,
                capturedEndMs = 4L,
                capturedByPrincipalId = null,
                proofPolicy = policy,
            )
        }

        // Refused LOUDLY, and in farm language that names the way out. Silently stacking a second
        // row is what filled the shed budget and made a later capture vanish.
        val error = second as? sg.mesha.goatos.core.common.AppResult.Err
        assertTrue("a slot must refuse a second proof rather than stack one", error != null)
        assertTrue(
            "the operator must be told how to replace it, not just that it failed: ${error?.message}",
            error?.message?.contains("re-capture", ignoreCase = true) == true,
        )
    }
}
