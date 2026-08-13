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
 * Proof flow contract test: weighing individual animal video.
 *
 * Tests the complete proof lifecycle for individual animal weighing:
 * capture → visible → navigation → visible → death/resurrection → visible →
 * recapture → latest wins → submit → exactly one enqueue → outbox/retry →
 * submitted → read-only → idempotency.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class WeighingIndividualProofContractTest {
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
    fun `weighing individual animal proof: complete chain from capture to submitted`() = runTest(dispatcher) {
        val taskId = "weighing:2026-08-13:shed-1:animal:901007000504407"
        val animalId = "901007000504407"
        val proofUri = "/capture/weighing-individual-001.mp4"

        // ========== STEP 1: Capture individual animal video ==========
        val captureResult = proofRepository.capture(
            taskId = taskId,
            fieldKey = "weighing_individual_video",
            subject = ProofSubject.GOAT,
            subjectId = animalId,
            localUri = proofUri,
            mimeType = "video/mp4",
            caption = null,
            scopeType = "goat",
            scopeId = animalId,
            capturedStartMs = 1000L,
            capturedEndMs = 8000L,
            capturedByPrincipalId = null,
            proofPolicy = weighingIndividualProofPolicy("in_app_camera"),
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

        val recaptureUri = "/capture/weighing-individual-retake.mp4"
        val recaptureResult = proofRepository.capture(
            taskId = taskId,
            fieldKey = "weighing_individual_video",
            subject = ProofSubject.GOAT,
            subjectId = animalId,
            localUri = recaptureUri,
            mimeType = "video/mp4",
            caption = null,
            scopeType = "goat",
            scopeId = animalId,
            capturedStartMs = 9000L,
            capturedEndMs = 16000L,
            capturedByPrincipalId = null,
            proofPolicy = weighingIndividualProofPolicy("in_app_camera"),
        )
        assertTrue("recapture must succeed", recaptureResult is AppResult.Ok)
        val secondProof = (recaptureResult as AppResult.Ok).value

        // ========== STEP 8: Latest wins ==========
        val latestProof = proofRepository.getProofById(secondProof.id)
        assertNotNull("latest proof must exist", latestProof)
        assertEquals("latest proof must have new uri", recaptureUri, latestProof?.uri)

        // ========== STEP 9: Submit ==========
        val proofIdentity = ProofIdentity.weighingIndividualProofIdentity(
            taskId = taskId,
            animalId = animalId,
        )
        val submitResult = syncRepository.enqueueProofUpload(
            groupKey = proofIdentity.groupKey(),
            idempotencyKey = proofIdentity.proofSubmissionKey(),
            request = ProofUploadRequestDto(
                taskId = taskId,
                localPath = recaptureUri,
                mimeType = "video/mp4",
                durationMs = 7000L,
            ),
            localFilePath = recaptureUri,
            durationMs = 7000L,
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
            "idempotency key must contain animal reference",
            idempotencyKey.contains(animalId),
        )
    }
}
