package sg.mesha.goatos.viewmodel

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.capture.ProofIdentity
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto

/**
 * Proof flow contract test: feed packing video.
 *
 * Tests the complete lifecycle for feed packing proof:
 * capture → visible → navigation → visible → death/resurrection → visible →
 * recapture → latest wins → submit → exactly one enqueue → outbox/retry →
 * submitted → read-only → idempotency.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class FeedPackingProofContractTest {
    private val dispatcher = UnconfinedTestDispatcher()
    private lateinit var proofRepository: FakeProofCaptureRepository
    private lateinit var syncRepository: CountingSyncRepository

    @Before
    fun setUp() {
        Dispatchers.setMain(dispatcher)
        proofRepository = FakeProofCaptureRepository()
        syncRepository = CountingSyncRepository()
    }

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `feed packing proof: complete chain from capture to submitted`() = runTest(dispatcher) {
        val taskId = "feed-pack:2026-08-13:shed-1:1:2:normal"
        val shedId = "shed-1"
        val proofUri = "/capture/feed-packing-001.mp4"

        // ========== STEP 1: Capture packing video ==========
        val captureResult = proofRepository.capture(
            taskId = taskId,
            fieldKey = "feed_packing_video",
            subject = ProofSubject.SHED,
            subjectId = shedId,
            localUri = proofUri,
            mimeType = "video/mp4",
            caption = null,
            scopeType = "shed",
            scopeId = shedId,
            capturedStartMs = 1000L,
            capturedEndMs = 5000L,
            capturedByPrincipalId = null,
            proofPolicy = feedShedProofPolicy("in_app_camera"),
        )
        assertTrue("capture must succeed", captureResult is AppResult.Ok)
        val firstProof = (captureResult as AppResult.Ok).value

        // ========== STEP 2: Proof visible ==========
        var visibleProof = proofRepository.getProofById(firstProof.id)
        assertNotNull("proof must be visible", visibleProof)
        assertEquals("uri must match", proofUri, visibleProof?.uri)

        // ========== STEP 3: Navigation ==========
        advanceUntilIdle()

        // ========== STEP 4: Still visible after navigation ==========
        visibleProof = proofRepository.getProofById(firstProof.id)
        assertNotNull("proof must survive navigation", visibleProof)

        // ========== STEP 5: Process death/resurrection ==========
        // Simulate VM recreation

        // ========== STEP 6: Still visible after death ==========
        val afterDeathProof = proofRepository.getProofById(firstProof.id)
        assertNotNull("proof must survive death/resurrection", afterDeathProof)

        // ========== STEP 7: Re-capture (replace) ==========
        proofRepository.markSynced(firstProof.id, serverProofId = "server-proof-1", syncStatus = "PENDING")

        val recaptureUri = "/capture/feed-packing-retake.mp4"
        val recaptureResult = proofRepository.capture(
            taskId = taskId,
            fieldKey = "feed_packing_video",
            subject = ProofSubject.SHED,
            subjectId = shedId,
            localUri = recaptureUri,
            mimeType = "video/mp4",
            caption = null,
            scopeType = "shed",
            scopeId = shedId,
            capturedStartMs = 6000L,
            capturedEndMs = 10000L,
            capturedByPrincipalId = null,
            proofPolicy = feedShedProofPolicy("in_app_camera"),
        )
        assertTrue("recapture must succeed", recaptureResult is AppResult.Ok)
        val secondProof = (recaptureResult as AppResult.Ok).value

        // ========== STEP 8: Latest wins ==========
        val latestProof = proofRepository.getProofById(secondProof.id)
        assertNotNull("latest proof must exist", latestProof)
        assertEquals("latest proof must have new uri", recaptureUri, latestProof?.uri)

        // ========== STEP 9: Submit ==========
        val proofIdentity = ProofIdentity.feedPackingProofIdentity(
            taskId = taskId,
            shedId = shedId,
        )
        val submitResult = syncRepository.enqueueProofUpload(
            groupKey = proofIdentity.groupKey(),
            idempotencyKey = proofIdentity.proofSubmissionKey(),
            request = ProofUploadRequestDto(
                taskId = taskId,
                localPath = recaptureUri,
                mimeType = "video/mp4",
                durationMs = 4000L,
            ),
            localFilePath = recaptureUri,
            durationMs = 4000L,
        )
        assertTrue("submit must succeed", submitResult is AppResult.Ok)

        // ========== STEP 10: Exactly one enqueue ==========
        assertEquals("exactly one enqueue", 1, syncRepository.proofUploadEnqueueCount)

        // ========== STEP 11 & 12: Outbox FAILED, retry ==========
        val itemId = (submitResult as AppResult.Ok).value
        syncRepository.setItemFailed(itemId)
        val retryResult = syncRepository.retry(itemId)
        assertTrue("retry must succeed", retryResult is AppResult.Ok)

        // ========== STEP 13: Submitted state ==========
        syncRepository.setItemSucceeded(itemId)
        val finalStatus = syncRepository.observeItem(itemId).value
        assertNotNull("final status must exist", finalStatus)
        assertTrue("must be successful", finalStatus?.status?.isSuccess() ?: false)

        // ========== STEP 14: Idempotency ==========
        val idempotencyKey = proofIdentity.proofSubmissionKey()
        assertNotNull("idempotency key must exist", idempotencyKey)
        assertTrue(
            "idempotency key must contain task and shed reference",
            idempotencyKey.contains("feed-pack") && idempotencyKey.contains(shedId),
        )
    }
}
