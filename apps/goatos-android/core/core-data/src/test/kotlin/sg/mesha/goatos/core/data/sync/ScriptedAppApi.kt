package sg.mesha.goatos.core.data.sync

import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.ProofCompleteResponseDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.ProofUploadResponseDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationResponseDto
import sg.mesha.goatos.core.network.dto.ScanAttemptRequestDto
import sg.mesha.goatos.core.network.dto.ScanAttemptResponseDto
import sg.mesha.goatos.core.network.dto.ScanCaptureRequestDto
import sg.mesha.goatos.core.network.dto.ScanCaptureResponseDto
import sg.mesha.goatos.core.network.dto.SubmissionResponseDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictRequestDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictResponseDto
import sg.mesha.goatos.core.network.dto.VerificationCloseSubmissionResponseDto
import sg.mesha.goatos.core.network.dto.VerificationReviewEventBatchRequestDto
import sg.mesha.goatos.core.network.dto.VerificationReviewEventBatchResponseDto
import sg.mesha.goatos.core.network.dto.HealthCompleteRequestDto
import sg.mesha.goatos.core.network.dto.HealthCompleteResponseDto
import sg.mesha.goatos.core.network.dto.HealthOpenCaseRequestDto
import sg.mesha.goatos.core.network.dto.HealthOpenCaseResponseDto

/**
 * Test double for [AppApi]: delegates to [FakeAppApi] by default (via Kotlin interface
 * delegation, `by delegate`), with per-call hooks a test can install to script
 * failures/successes and to capture the arguments a call was made with (in particular the
 * idempotency key, to assert it never changes across retries).
 */
class ScriptedAppApi(private val delegate: AppApi = FakeAppApi()) : AppApi by delegate {
    var submitAppTaskFn: (suspend (String, String, SubmitTaskRequestDto) -> SubmissionResponseDto)? = null
    var recordScanCaptureFn: (suspend (String, String, ScanCaptureRequestDto) -> ScanCaptureResponseDto)? = null
    var recordScanAttemptFn: (suspend (String, String, ScanAttemptRequestDto) -> ScanAttemptResponseDto)? = null
    var rescheduleObligationFn: (suspend (String, String, RescheduleObligationRequestDto) -> RescheduleObligationResponseDto)? = null
    var registerProofFn: (suspend (String, ProofUploadRequestDto) -> ProofUploadResponseDto)? = null
    var submitVerificationVerdictFn: (suspend (String, String, VerificationVerdictRequestDto) -> VerificationVerdictResponseDto)? = null
    var recordVerificationReviewEventsFn: (suspend (VerificationReviewEventBatchRequestDto) -> VerificationReviewEventBatchResponseDto)? = null
    var closeVerificationSubmissionFn: (suspend (String, String) -> VerificationCloseSubmissionResponseDto)? = null
    var closeVaccinationBatchFn: (suspend (String, String) -> VerificationCloseSubmissionResponseDto)? = null
    var completeHealthWorkItemFn: (suspend (String, String, HealthCompleteRequestDto) -> HealthCompleteResponseDto)? = null
    var openHealthCaseFn: (suspend (String, HealthOpenCaseRequestDto) -> HealthOpenCaseResponseDto)? = null
    val healthOpenCalls: MutableList<Pair<String, HealthOpenCaseRequestDto>> =
        java.util.concurrent.CopyOnWriteArrayList<Pair<String, HealthOpenCaseRequestDto>>()
    val healthCompleteCalls: MutableList<Pair<String, String>> =
        java.util.concurrent.CopyOnWriteArrayList<Pair<String, String>>()

    /** (itemId, header Idempotency-Key) for every [submitVerificationVerdict] call — same
     *  same-key-on-retry assertion shape as [submitCalls]. */
    val verdictCalls: MutableList<Pair<String, String>> =
        java.util.concurrent.CopyOnWriteArrayList<Pair<String, String>>()
    val closeSubmissionCalls: MutableList<Pair<String, String>> =
        java.util.concurrent.CopyOnWriteArrayList<Pair<String, String>>()
    val closeBatchCalls: MutableList<Pair<String, String>> =
        java.util.concurrent.CopyOnWriteArrayList<Pair<String, String>>()
    val reviewEventCalls: MutableList<VerificationReviewEventBatchRequestDto> =
        java.util.concurrent.CopyOnWriteArrayList<VerificationReviewEventBatchRequestDto>()

    /** Scripts the binary-PUT + complete step ([AppApi.uploadProofBlob]) — the hook a test
     *  installs to act as a fake object store: assert the (proofId, uploadUrl, filePath) it was
     *  called with, capture "uploaded" bytes, or throw to exercise the resumable-retry path. */
    var uploadProofBlobFn: (suspend (String, String, String, Map<String, String>, String, Long?, String, String, Long?) -> ProofCompleteResponseDto)? = null

    /** Every [uploadProofBlob] call, in order — lets a test assert how many times bytes were
     *  (re-)streamed across a failure + retry. */
    val uploadProofBlobCalls: MutableList<String> =
        java.util.concurrent.CopyOnWriteArrayList<String>()

    /** (taskId, header Idempotency-Key) for every [submitAppTask] call — asserts the outbox sends
     *  the SAME key on every retry (and actually sends one at all). Thread-safe: the drain fans out
     *  group coroutines across real IO threads, so concurrent submits append here in parallel; a
     *  plain ArrayList raced and intermittently dropped a call (250-row drain read 249). */
    val submitCalls: MutableList<Pair<String, String>> =
        java.util.concurrent.CopyOnWriteArrayList<Pair<String, String>>()

    override suspend fun submitAppTask(
        taskId: String,
        idempotencyKey: String,
        request: SubmitTaskRequestDto,
    ): SubmissionResponseDto {
        submitCalls += taskId to idempotencyKey
        return submitAppTaskFn?.invoke(taskId, idempotencyKey, request)
            ?: delegate.submitAppTask(taskId, idempotencyKey, request)
    }

    override suspend fun recordScanCapture(
        taskId: String,
        idempotencyKey: String,
        request: ScanCaptureRequestDto,
    ): ScanCaptureResponseDto =
        recordScanCaptureFn?.invoke(taskId, idempotencyKey, request)
            ?: delegate.recordScanCapture(taskId, idempotencyKey, request)

    override suspend fun recordScanAttempt(
        taskId: String,
        idempotencyKey: String,
        request: ScanAttemptRequestDto,
    ): ScanAttemptResponseDto =
        recordScanAttemptFn?.invoke(taskId, idempotencyKey, request)
            ?: delegate.recordScanAttempt(taskId, idempotencyKey, request)

    override suspend fun rescheduleObligation(
        obligationId: String,
        idempotencyKey: String,
        request: RescheduleObligationRequestDto,
    ): RescheduleObligationResponseDto =
        rescheduleObligationFn?.invoke(obligationId, idempotencyKey, request)
            ?: delegate.rescheduleObligation(obligationId, idempotencyKey, request)

    override suspend fun registerProof(idempotencyKey: String, request: ProofUploadRequestDto): ProofUploadResponseDto =
        registerProofFn?.invoke(idempotencyKey, request) ?: delegate.registerProof(idempotencyKey, request)

    override suspend fun submitVerificationVerdict(
        itemId: String,
        idempotencyKey: String,
        request: VerificationVerdictRequestDto,
    ): VerificationVerdictResponseDto {
        verdictCalls += itemId to idempotencyKey
        return submitVerificationVerdictFn?.invoke(itemId, idempotencyKey, request)
            ?: delegate.submitVerificationVerdict(itemId, idempotencyKey, request)
    }

    override suspend fun closeVerificationSubmission(
        submissionId: String,
        idempotencyKey: String,
    ): VerificationCloseSubmissionResponseDto {
        closeSubmissionCalls += submissionId to idempotencyKey
        return closeVerificationSubmissionFn?.invoke(submissionId, idempotencyKey)
            ?: delegate.closeVerificationSubmission(submissionId, idempotencyKey)
    }

    override suspend fun closeVaccinationBatch(
        batchId: String,
        idempotencyKey: String,
    ): VerificationCloseSubmissionResponseDto {
        closeBatchCalls += batchId to idempotencyKey
        return closeVaccinationBatchFn?.invoke(batchId, idempotencyKey)
            ?: delegate.closeVaccinationBatch(batchId, idempotencyKey)
    }

    override suspend fun recordVerificationReviewEvents(
        request: VerificationReviewEventBatchRequestDto,
    ): VerificationReviewEventBatchResponseDto {
        reviewEventCalls += request
        return recordVerificationReviewEventsFn?.invoke(request)
            ?: delegate.recordVerificationReviewEvents(request)
    }

    override suspend fun completeHealthWorkItem(
        healthSessionId: String,
        idempotencyKey: String,
        request: HealthCompleteRequestDto,
    ): HealthCompleteResponseDto {
        healthCompleteCalls += healthSessionId to idempotencyKey
        return completeHealthWorkItemFn?.invoke(healthSessionId, idempotencyKey, request)
            ?: delegate.completeHealthWorkItem(healthSessionId, idempotencyKey, request)
    }

    override suspend fun openHealthCase(
        idempotencyKey: String,
        request: HealthOpenCaseRequestDto,
    ): HealthOpenCaseResponseDto {
        healthOpenCalls += idempotencyKey to request
        return openHealthCaseFn?.invoke(idempotencyKey, request)
            ?: delegate.openHealthCase(idempotencyKey, request)
    }

    override suspend fun uploadProofBlob(
        proofId: String,
        uploadUrl: String,
        uploadMethod: String,
        uploadHeaders: Map<String, String>,
        uploadProtocol: String,
        chunkSizeBytes: Long?,
        mimeType: String,
        filePath: String,
        durationMs: Long?,
    ): ProofCompleteResponseDto {
        uploadProofBlobCalls += proofId
        return uploadProofBlobFn?.invoke(
            proofId,
            uploadUrl,
            uploadMethod,
            uploadHeaders,
            uploadProtocol,
            chunkSizeBytes,
            mimeType,
            filePath,
            durationMs,
        )
            ?: delegate.uploadProofBlob(
                proofId,
                uploadUrl,
                uploadMethod,
                uploadHeaders,
                uploadProtocol,
                chunkSizeBytes,
                mimeType,
                filePath,
                durationMs,
            )
    }
}
