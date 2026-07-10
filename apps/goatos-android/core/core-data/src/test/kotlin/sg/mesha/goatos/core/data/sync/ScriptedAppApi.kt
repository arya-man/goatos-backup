package sg.mesha.goatos.core.data.sync

import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.ProofUploadResponseDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationResponseDto
import sg.mesha.goatos.core.network.dto.SubmissionResponseDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto

/**
 * Test double for [AppApi]: delegates to [FakeAppApi] by default (via Kotlin interface
 * delegation, `by delegate`), with per-call hooks a test can install to script
 * failures/successes and to capture the arguments a call was made with (in particular the
 * idempotency key, to assert it never changes across retries).
 */
class ScriptedAppApi(private val delegate: AppApi = FakeAppApi()) : AppApi by delegate {
    var submitAppTaskFn: (suspend (String, SubmitTaskRequestDto) -> SubmissionResponseDto)? = null
    var rescheduleObligationFn: (suspend (String, String, RescheduleObligationRequestDto) -> RescheduleObligationResponseDto)? = null
    var registerProofFn: (suspend (String, ProofUploadRequestDto) -> ProofUploadResponseDto)? = null

    /** (taskId, idempotencyKey) for every [submitAppTask] call, in call order. */
    val submitCalls = mutableListOf<Pair<String, String>>()

    override suspend fun submitAppTask(taskId: String, request: SubmitTaskRequestDto): SubmissionResponseDto {
        submitCalls += taskId to request.idempotencyKey
        return submitAppTaskFn?.invoke(taskId, request) ?: delegate.submitAppTask(taskId, request)
    }

    override suspend fun rescheduleObligation(
        obligationId: String,
        idempotencyKey: String,
        request: RescheduleObligationRequestDto,
    ): RescheduleObligationResponseDto =
        rescheduleObligationFn?.invoke(obligationId, idempotencyKey, request)
            ?: delegate.rescheduleObligation(obligationId, idempotencyKey, request)

    override suspend fun registerProof(idempotencyKey: String, request: ProofUploadRequestDto): ProofUploadResponseDto =
        registerProofFn?.invoke(idempotencyKey, request) ?: delegate.registerProof(idempotencyKey, request)
}
