package sg.mesha.goatos.core.network

import java.util.Locale
import java.util.concurrent.TimeUnit
import kotlinx.serialization.json.Json
import okhttp3.Interceptor
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Response
import retrofit2.HttpException
import retrofit2.Response as RetrofitResponse
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory
import retrofit2.create
import retrofit2.http.Body
import retrofit2.http.GET
import retrofit2.http.Header
import retrofit2.http.POST
import retrofit2.http.Path
import retrofit2.http.Query
import sg.mesha.goatos.core.network.dto.CalendarEventListResponseDto
import sg.mesha.goatos.core.network.dto.ControlTowerResponseDto
import sg.mesha.goatos.core.network.dto.CountsApprovalDecisionRequestDto
import sg.mesha.goatos.core.network.dto.CountsApprovalDecisionResponseDto
import sg.mesha.goatos.core.network.dto.CountsApprovalListResponseDto
import sg.mesha.goatos.core.network.dto.CountsBirthEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsBreakdownResponseDto
import sg.mesha.goatos.core.network.dto.GoatSearchResponseDto
import sg.mesha.goatos.core.network.dto.CountsDeathEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsGoatLifecycleResponseDto
import sg.mesha.goatos.core.network.dto.CountsShiftingDestinationsResponseDto
import sg.mesha.goatos.core.network.dto.CountsShiftingEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsShiftingEventResponseDto
import sg.mesha.goatos.core.network.dto.HerdRegisterSummaryResponseDto
import sg.mesha.goatos.core.network.dto.EnrichedPositionListResponseDto
import sg.mesha.goatos.core.network.dto.MyCoverageResponseDto
import sg.mesha.goatos.core.network.dto.ProofCompleteRequestDto
import sg.mesha.goatos.core.network.dto.ProofCompleteResponseDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.ProofUploadResponseDto
import sg.mesha.goatos.core.network.dto.forCreateUpload
import sg.mesha.goatos.core.network.dto.ProtocolAdherenceResponseDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationResponseDto
import sg.mesha.goatos.core.network.dto.ReviewTaskRequestDto
import sg.mesha.goatos.core.network.dto.ReviewTaskResponseDto
import sg.mesha.goatos.core.network.dto.ScanAttemptRequestDto
import sg.mesha.goatos.core.network.dto.ScanAttemptResponseDto
import sg.mesha.goatos.core.network.dto.ScanCaptureRequestDto
import sg.mesha.goatos.core.network.dto.ScanCaptureResponseDto
import sg.mesha.goatos.core.network.dto.ScanRosterResponseDto
import sg.mesha.goatos.core.network.dto.ShedCompletionSummaryDto
import sg.mesha.goatos.core.network.dto.SubmissionResponseDto
// Device DTOs live in the network package (AppApi.kt); no dto.* import needed.
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.TaskDetailResponseDto
import sg.mesha.goatos.core.network.dto.TaskListResponseDto
import sg.mesha.goatos.core.network.dto.TaskOptionValuesResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionShedDrilldownDto
import sg.mesha.goatos.core.network.dto.VaccinationGapsResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationCoverageResponseDto
import sg.mesha.goatos.core.network.dto.AppConfigResponseDto
import sg.mesha.goatos.core.network.dto.VerificationQueueResponseDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictRequestDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictResponseDto
import sg.mesha.goatos.core.network.dto.VerificationCloseRequestDto
import sg.mesha.goatos.core.network.dto.VerificationCloseSubmissionResponseDto

const val TENANT_CONTEXT_HEADER: String = "X-GoatOS-Tenant-ID"
const val LOCALE_CONTEXT_HEADER: String = "X-GoatOS-Locale"
const val ACCEPT_LANGUAGE_HEADER: String = "Accept-Language"
private const val DEFAULT_LOCALE_TAG: String = "en"
private val localeTagPattern = Regex("^[A-Za-z]{2,8}(-[A-Za-z0-9]{1,8})*$")

/**
 * Retrofit surface for the app API. One method per consumed endpoint. Paths are
 * relative to the base URL (which ends in `/`). Nullable @Query params are omitted
 * from the request when null.
 */
interface AppApiService {
    @POST("auth/session-events")
    suspend fun recordAuthSessionEvent(@Body request: AuthSessionEventRequestDto)

    @GET("app/bootstrap")
    suspend fun bootstrap(@Query("device_id") deviceId: String?): BootstrapDto

    @POST("app/devices/register")
    suspend fun registerDevice(@Body request: RegisterDeviceRequestDto): DeviceResponseDto

    @POST("app/devices/{device_id}/heartbeat")
    suspend fun heartbeatDevice(
        @Path("device_id") deviceId: String,
        @Body request: HeartbeatDeviceRequestDto,
    ): DeviceResponseDto

    @POST("app/devices/{device_id}/deregister")
    suspend fun deregisterDevice(@Path("device_id") deviceId: String): DeviceResponseDto

    @GET("app/vaccination/execution")
    suspend fun listVaccinationExecution(
        @Query("park_id") parkId: String?,
        @Query("work_state") workState: String?,
        @Query("as_of") asOf: String?,
        @Query("due_before") dueBefore: String?,
        @Query("open_only") openOnly: Boolean?,
        @Query("limit") limit: Int?,
        @Query("cursor") cursor: String?,
    ): VaccinationExecutionResponseDto

    @GET("app/vaccination/execution/sheds/{shed_id}")
    suspend fun getVaccinationExecutionShed(
        @Path("shed_id") shedId: String,
        @Query("as_of") asOf: String?,
        @Query("due_before") dueBefore: String?,
        @Query("limit") limit: Int?,
    ): VaccinationExecutionShedDrilldownDto

    @GET("calendar/vaccination/events")
    suspend fun listCalendarVaccinationEvents(
        @Query("park_id") parkId: String?,
        @Query("shed_id") shedId: String?,
        @Query("owner_key") ownerKey: String?,
        @Query("status") status: String?,
        @Query("date_from") dateFrom: String?,
        @Query("date_to") dateTo: String?,
        @Query("include_date_markers") includeDateMarkers: Boolean?,
        @Query("vaccine") vaccine: String?,
        @Query("include_filter_options") includeFilterOptions: Boolean?,
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int?,
    ): CalendarEventListResponseDto

    @GET("control-tower/vaccination")
    suspend fun getVaccinationControlTower(
        @Query("park_id") parkId: String?,
        @Query("shed_id") shedId: String?,
        @Query("work_state") workState: String?,
        @Query("severity") severity: String?,
        @Query("due_before") dueBefore: String?,
        @Query("as_of") asOf: String?,
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int?,
    ): ControlTowerResponseDto

    @GET("vaccination/adherence")
    suspend fun getVaccinationAdherence(
        @Query("park_id") parkId: String?,
        @Query("shed_id") shedId: String?,
        @Query("work_state") workState: String?,
        @Query("severity") severity: String?,
        @Query("due_before") dueBefore: String?,
        @Query("as_of") asOf: String?,
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int?,
    ): ProtocolAdherenceResponseDto

    @GET("app/tasks")
    suspend fun listAppTasks(
        @Query("state") state: String?,
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int,
    ): TaskListResponseDto

    @GET("app/tasks/{task_id}")
    suspend fun getAppTask(@Path("task_id") taskId: String): TaskDetailResponseDto

    @GET("app/vaccination/tasks/{task_id}/option-values")
    suspend fun getTaskOptionValues(@Path("task_id") taskId: String): TaskOptionValuesResponseDto

    @GET("app/tasks/{task_id}/shed-completion-summary")
    suspend fun getShedCompletionSummary(
        @Path("task_id") taskId: String,
        @Query("shed_id") shedId: String?,
    ): ShedCompletionSummaryDto

    @POST("app/tasks/{task_id}/submissions")
    suspend fun submitAppTask(
        @Path("task_id") taskId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: SubmitTaskRequestDto,
    ): SubmissionResponseDto

    @POST("app/tasks/{task_id}/scan-captures")
    suspend fun recordScanCapture(
        @Path("task_id") taskId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: ScanCaptureRequestDto,
    ): ScanCaptureResponseDto

    @POST("app/tasks/{task_id}/scan-attempts")
    suspend fun recordScanAttempt(
        @Path("task_id") taskId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: ScanAttemptRequestDto,
    ): ScanAttemptResponseDto

    @POST("admin/tasks/{task_id}/verify")
    suspend fun verifyAppTask(
        @Path("task_id") taskId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: ReviewTaskRequestDto,
    ): ReviewTaskResponseDto

    @POST("admin/tasks/{task_id}/rework")
    suspend fun reworkAppTask(
        @Path("task_id") taskId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: ReviewTaskRequestDto,
    ): ReviewTaskResponseDto

    @GET("app/vaccination/execution/sheds/{shed_id}/roster")
    suspend fun getScanRoster(
        @Path("shed_id") shedId: String,
        @Query("task_id") taskId: String?,
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int?,
    ): ScanRosterResponseDto

    @POST("app/vaccination/obligations/{obligation_id}/reschedule")
    suspend fun rescheduleObligation(
        @Path("obligation_id") obligationId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: RescheduleObligationRequestDto,
    ): RescheduleObligationResponseDto

    @GET("app/roster/timetable")
    suspend fun getOperatorTimetable(
        @Query("center_id") centerId: String,
        @Query("limit") limit: Int?,
    ): EnrichedPositionListResponseDto

    @GET("app/roster/my-coverage")
    suspend fun getMyCoverage(): MyCoverageResponseDto

    @POST("app/proofs/uploads")
    suspend fun registerProof(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: ProofUploadRequestDto,
    ): ProofUploadResponseDto

    @POST("app/proofs/{proof_id}/complete")
    suspend fun completeProofUpload(
        @Path("proof_id") proofId: String,
        @Body request: ProofCompleteRequestDto,
    ): ProofCompleteResponseDto

    @GET("app/vaccination/gaps")
    suspend fun getVaccinationGaps(
        @Query("park_id") parkId: String?,
        @Query("limit") limit: Int?,
        @Query("cursor") cursor: String?,
    ): VaccinationGapsResponseDto

    @GET("app/vaccination/coverage")
    suspend fun getVaccinationCoverage(
        @Query("park_id") parkId: String?,
        @Query("as_of") asOf: String?,
        @Query("due_before") dueBefore: String?,
        @Query("limit") limit: Int?,
    ): VaccinationCoverageResponseDto

    @GET("app/config")
    suspend fun getAppConfig(
        @Header("If-None-Match") eTag: String?,
    ): RetrofitResponse<AppConfigResponseDto>

    @GET("verification/queue")
    suspend fun listVerificationQueue(
        @Query("category") category: String?,
        @Query("park_id") parkId: String?,
        @Query("shed_id") shedId: String?,
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int?,
    ): VerificationQueueResponseDto

    @GET("verification/action-queue")
    suspend fun listVerificationActionQueue(
        @Query("category") category: String?,
        @Query("park_id") parkId: String?,
        @Query("shed_id") shedId: String?,
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int?,
    ): VerificationQueueResponseDto

    @POST("verification/items/{item_id}/verdict")
    suspend fun submitVerificationVerdict(
        @Path("item_id") itemId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: VerificationVerdictRequestDto,
    ): VerificationVerdictResponseDto

    @POST("verification/items/{item_id}/close")
    suspend fun closeVerificationItem(
        @Path("item_id") itemId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: VerificationCloseRequestDto,
    ): VerificationVerdictResponseDto

    @POST("verification/submissions/{submission_id}/close")
    suspend fun closeVerificationSubmission(
        @Path("submission_id") submissionId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
    ): VerificationCloseSubmissionResponseDto

    @POST("verification/vaccination-batches/{batch_id}/close")
    suspend fun closeVaccinationBatch(
        @Path("batch_id") batchId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
    ): VerificationCloseSubmissionResponseDto
    @GET("herd-register/summary")
    suspend fun getHerdRegisterSummary(
        @Query("lifecycle_status") lifecycleStatus: String?,
        @Query("park_id") parkId: String?,
        @Query("breed") breed: String?,
        @Query("sex") sex: String?,
    ): HerdRegisterSummaryResponseDto

    @GET("counts/breakdown")
    suspend fun getCountsBreakdown(
        @Query("park_id") parkId: String?,
        @Query("shed_id") shedId: String?,
        @Query("management_stage") managementStage: String?,
        @Query("breed") breed: String?,
        @Query("sex") sex: String?,
        @Query("lifecycle_status") lifecycleStatus: String?,
        @Query("limit") limit: Int?,
        @Query("offset") offset: Int?,
    ): CountsBreakdownResponseDto

    @POST("app/counts/shifting-events")
    suspend fun recordCountsShiftingEvent(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: CountsShiftingEventRequestDto,
    ): CountsShiftingEventResponseDto

    @GET("app/counts/shifting/destinations")
    suspend fun getCountsShiftingDestinations(): CountsShiftingDestinationsResponseDto

    @POST("app/counts/birth-events")
    suspend fun recordCountsBirthEvent(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: CountsBirthEventRequestDto,
    ): CountsGoatLifecycleResponseDto

    @POST("app/counts/death-events")
    suspend fun recordCountsDeathEvent(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: CountsDeathEventRequestDto,
    ): CountsGoatLifecycleResponseDto

    @GET("goats/search")
    suspend fun searchGoats(
        @Query("q") q: String?,
        @Query("park_id") parkId: String?,
        @Query("location_id") locationId: String?,
        @Query("status") status: String?,
        @Query("limit") limit: Int?,
        @Query("cursor") cursor: String?,
    ): GoatSearchResponseDto

    @GET("app/counts/approvals")
    suspend fun listCountsApprovals(
        @Query("status") status: String?,
        @Query("page_size") pageSize: Int?,
        @Query("cursor") cursor: String?,
    ): CountsApprovalListResponseDto

    @POST("app/counts/approvals/{request_id}/approve")
    suspend fun approveCountsApproval(
        @Path("request_id") requestId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: CountsApprovalDecisionRequestDto,
    ): CountsApprovalDecisionResponseDto

    @POST("app/counts/approvals/{request_id}/reject")
    suspend fun rejectCountsApproval(
        @Path("request_id") requestId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: CountsApprovalDecisionRequestDto,
    ): CountsApprovalDecisionResponseDto
}

/** Adapts the Retrofit service to the [AppApi] port so callers stay Retrofit-agnostic.
 *  [blobUploader] handles the binary PUT half of the proof-upload flow — a separate collaborator
 *  because it targets an arbitrary signed URL (often a third-party storage host), not this
 *  Retrofit service's fixed JSON base URL. */
class RetrofitAppApi(
    private val service: AppApiService,
    private val blobUploader: ProofBlobUploader,
) : AppApi {
    override suspend fun recordAuthSessionEvent(request: AuthSessionEventRequestDto) =
        service.recordAuthSessionEvent(request)

    override suspend fun bootstrap(deviceId: String?): BootstrapDto = service.bootstrap(deviceId)

    override suspend fun registerDevice(request: RegisterDeviceRequestDto): DeviceResponseDto =
        service.registerDevice(request)

    override suspend fun heartbeatDevice(deviceId: String, request: HeartbeatDeviceRequestDto): DeviceResponseDto =
        service.heartbeatDevice(deviceId, request)

    override suspend fun deregisterDevice(deviceId: String): DeviceResponseDto =
        service.deregisterDevice(deviceId)

    override suspend fun listVaccinationExecution(
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        openOnly: Boolean?,
        limit: Int?,
        cursor: String?,
    ): VaccinationExecutionResponseDto =
        service.listVaccinationExecution(parkId, workState, asOf, dueBefore, openOnly, limit, cursor)

    override suspend fun getVaccinationExecutionShed(
        shedId: String,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
    ): VaccinationExecutionShedDrilldownDto =
        service.getVaccinationExecutionShed(shedId, asOf, dueBefore, limit)

    override suspend fun listCalendarVaccinationEvents(
        parkId: String?,
        shedId: String?,
        ownerKey: String?,
        status: String?,
        dateFrom: String?,
        dateTo: String?,
        includeDateMarkers: Boolean,
        vaccine: String?,
        includeFilterOptions: Boolean,
        cursor: String?,
        limit: Int?,
    ): CalendarEventListResponseDto =
        service.listCalendarVaccinationEvents(
            parkId,
            shedId,
            ownerKey,
            status,
            dateFrom,
            dateTo,
            includeDateMarkers.takeIf { it },
            vaccine,
            includeFilterOptions.takeIf { it },
            cursor,
            limit,
        )

    override suspend fun getVaccinationControlTower(
        parkId: String?,
        shedId: String?,
        workState: String?,
        severity: String?,
        dueBefore: String?,
        asOf: String?,
        cursor: String?,
        limit: Int?,
    ): ControlTowerResponseDto =
        service.getVaccinationControlTower(parkId, shedId, workState, severity, dueBefore, asOf, cursor, limit)

    override suspend fun getVaccinationAdherence(
        parkId: String?,
        shedId: String?,
        workState: String?,
        severity: String?,
        dueBefore: String?,
        asOf: String?,
        cursor: String?,
        limit: Int?,
    ): ProtocolAdherenceResponseDto =
        service.getVaccinationAdherence(parkId, shedId, workState, severity, dueBefore, asOf, cursor, limit)

    override suspend fun listAppTasks(
        state: String?,
        cursor: String?,
        limit: Int,
    ): TaskListResponseDto = service.listAppTasks(state, cursor, limit)

    override suspend fun getAppTask(taskId: String): TaskDetailResponseDto = service.getAppTask(taskId)

    override suspend fun getTaskOptionValues(taskId: String): TaskOptionValuesResponseDto =
        service.getTaskOptionValues(taskId)

    override suspend fun getShedCompletionSummary(taskId: String, shedId: String?): ShedCompletionSummaryDto =
        service.getShedCompletionSummary(taskId, shedId)

    override suspend fun submitAppTask(
        taskId: String,
        idempotencyKey: String,
        request: SubmitTaskRequestDto,
    ): SubmissionResponseDto = service.submitAppTask(taskId, idempotencyKey, request)

    override suspend fun recordScanCapture(
        taskId: String,
        idempotencyKey: String,
        request: ScanCaptureRequestDto,
    ): ScanCaptureResponseDto = service.recordScanCapture(taskId, idempotencyKey, request)

    override suspend fun recordScanAttempt(
        taskId: String,
        idempotencyKey: String,
        request: ScanAttemptRequestDto,
    ): ScanAttemptResponseDto = service.recordScanAttempt(taskId, idempotencyKey, request)

    override suspend fun verifyAppTask(
        taskId: String,
        idempotencyKey: String,
        request: ReviewTaskRequestDto,
    ): ReviewTaskResponseDto = service.verifyAppTask(taskId, idempotencyKey, request)

    override suspend fun reworkAppTask(
        taskId: String,
        idempotencyKey: String,
        request: ReviewTaskRequestDto,
    ): ReviewTaskResponseDto = service.reworkAppTask(taskId, idempotencyKey, request)

    override suspend fun getScanRoster(
        shedId: String,
        taskId: String?,
        cursor: String?,
        limit: Int?,
    ): ScanRosterResponseDto = service.getScanRoster(shedId, taskId, cursor, limit)

    override suspend fun rescheduleObligation(
        obligationId: String,
        idempotencyKey: String,
        request: RescheduleObligationRequestDto,
    ): RescheduleObligationResponseDto =
        service.rescheduleObligation(obligationId, idempotencyKey, request)

    override suspend fun getOperatorTimetable(
        centerId: String,
        limit: Int?,
    ): EnrichedPositionListResponseDto = service.getOperatorTimetable(centerId, limit)

    override suspend fun getMyCoverage(): MyCoverageResponseDto = service.getMyCoverage()

    override suspend fun registerProof(idempotencyKey: String, request: ProofUploadRequestDto): ProofUploadResponseDto =
        service.registerProof(idempotencyKey, request.forCreateUpload())

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
        val result = blobUploader.putFile(
            uploadUrl = uploadUrl,
            uploadMethod = uploadMethod,
            uploadHeaders = uploadHeaders,
            uploadProtocol = uploadProtocol,
            chunkSizeBytes = chunkSizeBytes,
            mimeType = mimeType,
            filePath = filePath,
        )
        val request = when (result) {
            is ProofBlobPutResult.Uploaded -> ProofCompleteRequestDto(
                contentHash = result.contentHash,
                mimeType = mimeType,
                sizeBytes = result.sizeBytes,
                durationMs = durationMs,
            )
            // Bytes were already durable from a prior attempt — size/hash unknown to THIS call;
            // sizeBytes = 0 tells the backend to trust the stored object rather than compare.
            ProofBlobPutResult.AlreadyExists -> ProofCompleteRequestDto(
                mimeType = mimeType,
                sizeBytes = 0,
                durationMs = durationMs,
            )
        }
        return service.completeProofUpload(proofId, request)
    }

    override suspend fun getVaccinationGaps(
        parkId: String?,
        limit: Int?,
        cursor: String?,
    ): VaccinationGapsResponseDto = service.getVaccinationGaps(parkId, limit, cursor)

    override suspend fun getVaccinationCoverage(
        parkId: String?,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
    ): VaccinationCoverageResponseDto = service.getVaccinationCoverage(parkId, asOf, dueBefore, limit)

    override suspend fun getAppConfig(eTag: String?): AppConfigResponseDto? {
        val response = service.getAppConfig(eTag)
        // 304 Not Modified: config unchanged, no body — signal "keep cached" with null instead
        // of letting Retrofit surface the 3xx as an HttpException the way a bare-DTO return would.
        if (response.code() == 304) return null
        if (!response.isSuccessful) throw HttpException(response)
        return response.body()
    }

    override suspend fun listVerificationQueue(
        category: String?,
        parkId: String?,
        shedId: String?,
        cursor: String?,
        limit: Int?,
    ): VerificationQueueResponseDto = service.listVerificationQueue(category, parkId, shedId, cursor, limit)

    override suspend fun listVerificationActionQueue(
        category: String?,
        parkId: String?,
        shedId: String?,
        cursor: String?,
        limit: Int?,
    ): VerificationQueueResponseDto = service.listVerificationActionQueue(category, parkId, shedId, cursor, limit)

    override suspend fun submitVerificationVerdict(
        itemId: String,
        idempotencyKey: String,
        request: VerificationVerdictRequestDto,
    ): VerificationVerdictResponseDto = service.submitVerificationVerdict(itemId, idempotencyKey, request)

    override suspend fun closeVerificationItem(
        itemId: String,
        idempotencyKey: String,
        request: VerificationCloseRequestDto,
    ): VerificationVerdictResponseDto = service.closeVerificationItem(itemId, idempotencyKey, request)

    override suspend fun closeVerificationSubmission(
        submissionId: String,
        idempotencyKey: String,
    ): VerificationCloseSubmissionResponseDto =
        service.closeVerificationSubmission(submissionId, idempotencyKey)

    override suspend fun closeVaccinationBatch(
        batchId: String,
        idempotencyKey: String,
    ): VerificationCloseSubmissionResponseDto =
        service.closeVaccinationBatch(batchId, idempotencyKey)
    override suspend fun getHerdRegisterSummary(
        lifecycleStatus: String?,
        parkId: String?,
        breed: String?,
        sex: String?,
    ): HerdRegisterSummaryResponseDto =
        service.getHerdRegisterSummary(lifecycleStatus, parkId, breed, sex)

    override suspend fun getCountsBreakdown(
        parkId: String?,
        shedId: String?,
        managementStage: String?,
        breed: String?,
        sex: String?,
        lifecycleStatus: String?,
        limit: Int?,
        offset: Int?,
    ): CountsBreakdownResponseDto =
        service.getCountsBreakdown(parkId, shedId, managementStage, breed, sex, lifecycleStatus, limit, offset)

    override suspend fun getCountsShiftingDestinations(): CountsShiftingDestinationsResponseDto =
        service.getCountsShiftingDestinations()

    override suspend fun recordCountsShiftingEvent(
        idempotencyKey: String,
        request: CountsShiftingEventRequestDto,
    ): CountsShiftingEventResponseDto = service.recordCountsShiftingEvent(idempotencyKey, request)

    override suspend fun recordCountsBirthEvent(
        idempotencyKey: String,
        request: CountsBirthEventRequestDto,
    ): CountsGoatLifecycleResponseDto = service.recordCountsBirthEvent(idempotencyKey, request)

    override suspend fun recordCountsDeathEvent(
        idempotencyKey: String,
        request: CountsDeathEventRequestDto,
    ): CountsGoatLifecycleResponseDto = service.recordCountsDeathEvent(idempotencyKey, request)

    override suspend fun searchGoats(
        q: String?,
        parkId: String?,
        locationId: String?,
        status: String?,
        limit: Int?,
        cursor: String?,
    ): GoatSearchResponseDto = service.searchGoats(q, parkId, locationId, status, limit, cursor)

    override suspend fun listCountsApprovals(
        status: String?,
        pageSize: Int?,
        cursor: String?,
    ): CountsApprovalListResponseDto = service.listCountsApprovals(status, pageSize, cursor)

    override suspend fun approveCountsApproval(
        requestId: String,
        idempotencyKey: String,
        request: CountsApprovalDecisionRequestDto,
    ): CountsApprovalDecisionResponseDto = service.approveCountsApproval(requestId, idempotencyKey, request)

    override suspend fun rejectCountsApproval(
        requestId: String,
        idempotencyKey: String,
        request: CountsApprovalDecisionRequestDto,
    ): CountsApprovalDecisionResponseDto = service.rejectCountsApproval(requestId, idempotencyKey, request)
}

/**
 * Adds per-request auth, tenant, and locale headers from supplied providers. No token is
 * stored here — the provider reads the current session token (DataStore) each request, so
 * a refreshed token or changed app language is picked up without rebuilding the client.
 */
class BearerAuthInterceptor(
    private val tokenProvider: () -> String?,
    private val tenantIdProvider: () -> String? = { null },
    private val localeProvider: () -> String? = { null },
) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val token = tokenProvider()
        val tenantId = tenantIdProvider()
        val localeTag = normalizedLocaleTag(localeProvider())
        val builder = chain.request().newBuilder()
        if (!token.isNullOrBlank()) {
            builder.header("Authorization", "Bearer $token")
        }
        if (!tenantId.isNullOrBlank()) {
            builder.header(TENANT_CONTEXT_HEADER, tenantId)
        }
        builder.header(ACCEPT_LANGUAGE_HEADER, acceptLanguageValue(localeTag))
        builder.header(LOCALE_CONTEXT_HEADER, localeTag)
        return chain.proceed(builder.build())
    }
}

/** Builds the OkHttp/Retrofit stack. Hilt provides these in the DI pass. */
object NetworkFactory {
    val json: Json = Json {
        ignoreUnknownKeys = true
        explicitNulls = false
        encodeDefaults = true
    }

    fun okHttp(
        tokenProvider: () -> String?,
        tenantIdProvider: () -> String? = { null },
        localeProvider: () -> String? = { null },
        // Optional: traceparent stamping + method/route/status/duration reporting
        // (docs/observability/OBSERVABILITY_DESIGN.md §2.5). Null keeps the client identical to
        // before this was wired — every existing caller is unaffected until it opts in.
        telemetryInterceptor: okhttp3.Interceptor? = null,
    ): OkHttpClient =
        OkHttpClient.Builder()
            .addInterceptor(BearerAuthInterceptor(tokenProvider, tenantIdProvider, localeProvider))
            .apply { telemetryInterceptor?.let { addInterceptor(it) } }
            // Explicit bounds — never rely on the platform/OkHttp defaults (a stuck socket on a
            // field 2G link must fail and let the outbox back off, not hang the drain coroutine).
            .connectTimeout(15, TimeUnit.SECONDS)
            .readTimeout(30, TimeUnit.SECONDS)
            .writeTimeout(30, TimeUnit.SECONDS)
            .callTimeout(60, TimeUnit.SECONDS)
            .retryOnConnectionFailure(true)
            .build()

    /**
     * BARE client for the proof-blob PUT — deliberately built WITHOUT [BearerAuthInterceptor]:
     * a production upload target is a third-party storage host (GCS), and this app's bearer
     * token must never be attached to a request leaving its own API host
     * ([OkHttpProofBlobUploader] attaches it explicitly, only for a same-host relative URL).
     * Wider write/read/call timeouts than the JSON client — a multi-minute proof video over a
     * slow field connection must not be capped by the small-JSON-body budget; per-operation
     * (not total-elapsed) timeouts still bound a stalled socket.
     */
    fun bareOkHttp(): OkHttpClient =
        OkHttpClient.Builder()
            .connectTimeout(15, TimeUnit.SECONDS)
            .readTimeout(2, TimeUnit.MINUTES)
            .writeTimeout(2, TimeUnit.MINUTES)
            .retryOnConnectionFailure(true)
            .build()

    fun retrofit(baseUrl: String, client: OkHttpClient): Retrofit =
        Retrofit.Builder()
            .baseUrl(baseUrl)
            .client(client)
            .addConverterFactory(json.asConverterFactory("application/json".toMediaType()))
            .build()

    fun proofBlobUploader(baseUrl: String, tokenProvider: () -> String?): ProofBlobUploader =
        OkHttpProofBlobUploader(bareOkHttp(), baseUrl, tokenProvider)

    fun appApi(
        baseUrl: String,
        tokenProvider: () -> String?,
        tenantIdProvider: () -> String? = { null },
        localeProvider: () -> String? = { null },
        telemetryInterceptor: okhttp3.Interceptor? = null,
    ): AppApi =
        RetrofitAppApi(
            retrofit(baseUrl, okHttp(tokenProvider, tenantIdProvider, localeProvider, telemetryInterceptor)).create(),
            proofBlobUploader(baseUrl, tokenProvider),
        )
}

private fun normalizedLocaleTag(raw: String?): String {
    val tag = raw
        ?.trim()
        ?.replace('_', '-')
        ?.takeIf { it.isNotBlank() && localeTagPattern.matches(it) }
        ?: DEFAULT_LOCALE_TAG
    return tag.lowercase(Locale.ROOT)
}

private fun acceptLanguageValue(localeTag: String): String =
    if (localeTag == DEFAULT_LOCALE_TAG) DEFAULT_LOCALE_TAG else "$localeTag, $DEFAULT_LOCALE_TAG;q=0.8"
