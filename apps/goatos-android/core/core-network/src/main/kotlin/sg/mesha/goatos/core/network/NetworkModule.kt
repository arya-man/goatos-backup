package sg.mesha.goatos.core.network

import java.util.Locale
import java.util.concurrent.TimeUnit
import kotlinx.serialization.json.Json
import okhttp3.Interceptor
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Response
import okhttp3.ResponseBody
import retrofit2.HttpException
import retrofit2.Response as RetrofitResponse
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory
import retrofit2.create
import retrofit2.http.Body
import retrofit2.http.DELETE
import retrofit2.http.GET
import retrofit2.http.Header
import retrofit2.http.POST
import retrofit2.http.Path
import retrofit2.http.PUT
import retrofit2.http.Query
import retrofit2.http.Streaming
import sg.mesha.goatos.core.network.dto.CalendarEventListResponseDto
import sg.mesha.goatos.core.network.dto.ClockPersonDayResponseDto
import sg.mesha.goatos.core.network.dto.ClockPresenceResponseDto
import sg.mesha.goatos.core.network.dto.ClockPunchRequestDto
import sg.mesha.goatos.core.network.dto.ClockPunchResponseDto
import sg.mesha.goatos.core.network.dto.ClockStatusResponseDto
import sg.mesha.goatos.core.network.dto.ControlTowerResponseDto
import sg.mesha.goatos.core.network.dto.ConfirmHealthDiagnosisRequestDto
import sg.mesha.goatos.core.network.dto.ConfirmHealthDiagnosisResponseDto
import sg.mesha.goatos.core.network.dto.HealthDiagnosisProposalResponseDto
import sg.mesha.goatos.core.network.dto.HealthDiagnosisQueuePageDto
import sg.mesha.goatos.core.network.dto.HealthDiagnosisRunDto
import sg.mesha.goatos.core.network.dto.SubmitHealthObservationRequestDto
import sg.mesha.goatos.core.network.dto.HealthCloseCaseRequestDto
import sg.mesha.goatos.core.network.dto.HealthCloseCaseResponseDto
import sg.mesha.goatos.core.network.dto.HealthCompleteRequestDto
import sg.mesha.goatos.core.network.dto.HealthCompleteResponseDto
import sg.mesha.goatos.core.network.dto.HealthOpenCaseRequestDto
import sg.mesha.goatos.core.network.dto.HealthOpenCaseResponseDto
import sg.mesha.goatos.core.network.dto.HealthWorkItemDetailDto
import sg.mesha.goatos.core.network.dto.HealthWorkItemPageDto
import sg.mesha.goatos.core.network.dto.FeedDirectionCompleteRequestDto
import sg.mesha.goatos.core.network.dto.FeedDirectionCompleteResponseDto
import sg.mesha.goatos.core.network.dto.FeedDistributionCompleteRequestDto
import sg.mesha.goatos.core.network.dto.FeedDistributionCompleteResponseDto
import sg.mesha.goatos.core.network.dto.FeedPackingCompleteRequestDto
import sg.mesha.goatos.core.network.dto.MilkPreparationSubmissionRequestDto
import sg.mesha.goatos.core.network.dto.MilkPreparationSubmissionResponseDto
import sg.mesha.goatos.core.network.dto.MilkPreparationPageDto
import sg.mesha.goatos.core.network.dto.MilkFeedingPageDto
import sg.mesha.goatos.core.network.dto.MilkFeedingSubmitRequestDto
import sg.mesha.goatos.core.network.dto.MilkFeedingSubmitResponseDto
import sg.mesha.goatos.core.network.dto.FeedPackingCompleteResponseDto
import sg.mesha.goatos.core.network.dto.FeedDirectionPreviewPageDto
import sg.mesha.goatos.core.network.dto.FeedDistributionCapturesDto
import sg.mesha.goatos.core.network.dto.FeedPackingWorklistPageDto
import sg.mesha.goatos.core.network.dto.FeedWastageCompleteRequestDto
import sg.mesha.goatos.core.network.dto.FeedWastageCompleteResponseDto
import sg.mesha.goatos.core.network.dto.FeedWastageMeasurementRequestDto
import sg.mesha.goatos.core.network.dto.FeedWastageMeasurementResponseDto
import sg.mesha.goatos.core.network.dto.FeedWastageWorklistPageDto
import sg.mesha.goatos.core.network.dto.PcCareCapturesDto
import sg.mesha.goatos.core.network.dto.PcCareCreateTaskRequestDto
import sg.mesha.goatos.core.network.dto.PcCareCloseRequestDto
import sg.mesha.goatos.core.network.dto.PcCareCreateRoundRequestDto
import sg.mesha.goatos.core.network.dto.PcCareRoundCardPageDto
import sg.mesha.goatos.core.network.dto.PcCareRoundDto
import sg.mesha.goatos.core.network.dto.PcCareRoundPenDto
import sg.mesha.goatos.core.network.dto.PcCareRemovalPenListDto
import sg.mesha.goatos.core.network.dto.PcCareRemovalPenProofRequestDto
import sg.mesha.goatos.core.network.dto.PcCarePlannerCatalogDto
import sg.mesha.goatos.core.network.dto.PcCarePlannerShedsDto
import sg.mesha.goatos.core.network.dto.PcCareScanRequestDto
import sg.mesha.goatos.core.network.dto.PcCareScanResponseDto
import sg.mesha.goatos.core.network.dto.PcCareSlotProofRequestDto
import sg.mesha.goatos.core.network.dto.PcCareStockVerdictRequestDto
import sg.mesha.goatos.core.network.dto.PcCareSubmitResponseDto
import sg.mesha.goatos.core.network.dto.PcCareTaskDto
import sg.mesha.goatos.core.network.dto.PcCareTaskPageDto
import sg.mesha.goatos.core.network.dto.PcCareTaskRosterDto
import sg.mesha.goatos.core.network.dto.LeadershipAssigneeListDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskDetailDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskEditRequestDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskPageDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskRaiseRequestDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskStatusRequestDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskCommentRequestDto
import sg.mesha.goatos.core.network.dto.ToxinStepCompleteRequestDto
import sg.mesha.goatos.core.network.dto.ToxinSubmitRequestDto
import sg.mesha.goatos.core.network.dto.ToxinTaskDetailDto
import sg.mesha.goatos.core.network.dto.ToxinTaskPageDto
import sg.mesha.goatos.core.network.dto.VendorCatalogDto
import sg.mesha.goatos.core.network.dto.VendorDto
import sg.mesha.goatos.core.network.dto.VendorPageDto
import sg.mesha.goatos.core.network.dto.VendorWriteDto
import sg.mesha.goatos.core.network.dto.FeedPurchaseDeliveryWriteDto
import sg.mesha.goatos.core.network.dto.FeedPurchaseDto
import sg.mesha.goatos.core.network.dto.FeedPurchaseEditDto
import sg.mesha.goatos.core.network.dto.FeedPurchasePaymentWriteDto
import sg.mesha.goatos.core.network.dto.FeedPurchaseStatusWriteDto
import sg.mesha.goatos.core.network.dto.FeedPurchaseOptionsDto
import sg.mesha.goatos.core.network.dto.FeedPurchasePageDto
import sg.mesha.goatos.core.network.dto.FeedPurchaseWriteDto
import sg.mesha.goatos.core.network.dto.SaleAllocationDto
import sg.mesha.goatos.core.network.dto.SaleAllocationRequestDto
import sg.mesha.goatos.core.network.dto.SaleCandidatePageDto
import sg.mesha.goatos.core.network.dto.SaleLocationsDto
import sg.mesha.goatos.core.network.dto.SalePreviewDto
import sg.mesha.goatos.core.network.dto.SalesBenchmarkWriteDto
import sg.mesha.goatos.core.network.dto.SalesBuyerLeadDto
import sg.mesha.goatos.core.network.dto.SalesBuyerLeadPageDto
import sg.mesha.goatos.core.network.dto.SalesBuyerLeadWriteDto
import sg.mesha.goatos.core.network.dto.SalesDealDto
import sg.mesha.goatos.core.network.dto.SalesDealPaymentWriteDto
import sg.mesha.goatos.core.network.dto.SalesDealStatusWriteDto
import sg.mesha.goatos.core.network.dto.SalesFpoLeadDto
import sg.mesha.goatos.core.network.dto.SalesFpoLeadPageDto
import sg.mesha.goatos.core.network.dto.SalesFpoLeadWriteDto
import sg.mesha.goatos.core.network.dto.SalesLeadStatusWriteDto
import sg.mesha.goatos.core.network.dto.SalesRecordedDto
import sg.mesha.goatos.core.network.dto.SalesSoldTagsResultDto
import sg.mesha.goatos.core.network.dto.SalesSoldTagsWriteDto
import sg.mesha.goatos.core.network.dto.SalesWeightCheckWriteDto
import sg.mesha.goatos.core.network.dto.SalesDealPageDto
import sg.mesha.goatos.core.network.dto.SalesDealWriteDto
import sg.mesha.goatos.core.network.dto.SalesOptionsDto
import sg.mesha.goatos.core.network.dto.VendorOptionsDto
import sg.mesha.goatos.core.network.dto.FeedTransportTaskPageDto
import sg.mesha.goatos.core.network.dto.FeedTransportSubmitRequestDto
import sg.mesha.goatos.core.network.dto.FeedTransportSubmitResponseDto
import sg.mesha.goatos.core.network.dto.CountsApprovalDecisionRequestDto
import sg.mesha.goatos.core.network.dto.CountsApprovalDecisionResponseDto
import sg.mesha.goatos.core.network.dto.CountsApprovalListResponseDto
import sg.mesha.goatos.core.network.dto.CountsBirthEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsBreakdownResponseDto
import sg.mesha.goatos.core.network.dto.CountsBreedsResponseDto
import sg.mesha.goatos.core.network.dto.GoatSearchResponseDto
import sg.mesha.goatos.core.network.dto.CountsDeathEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsApprovalSubmitResponseDto
import sg.mesha.goatos.core.network.dto.CountsPenReconciliationCompleteRequestDto
import sg.mesha.goatos.core.network.dto.CountsPenReconciliationCompleteResponseDto
import sg.mesha.goatos.core.network.dto.CountsPenReconciliationListResponseDto
import sg.mesha.goatos.core.network.dto.CountsShiftingCancelRequestDto
import sg.mesha.goatos.core.network.dto.CountsPromoteIdentifierRequestDto
import sg.mesha.goatos.core.network.dto.CountsPromoteIdentifierResponseDto
import sg.mesha.goatos.core.network.dto.CountsShiftingCompleteRequestDto
import sg.mesha.goatos.core.network.dto.CountsShiftingDestinationsResponseDto
import sg.mesha.goatos.core.network.dto.CountsShiftingEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsShiftingEventResponseDto
import sg.mesha.goatos.core.network.dto.CountsShiftingExecutionResponseDto
import sg.mesha.goatos.core.network.dto.CountsShiftingPendingExecutionResponseDto
import sg.mesha.goatos.core.network.dto.HerdRegisterSummaryResponseDto
import sg.mesha.goatos.core.network.dto.EnrichedPositionListResponseDto
import sg.mesha.goatos.core.network.dto.MyCoverageResponseDto
import sg.mesha.goatos.core.network.dto.ProofCompleteRequestDto
import sg.mesha.goatos.core.network.dto.ProofDownloadUrlResponseDto
import sg.mesha.goatos.core.network.dto.ProofCompleteResponseDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.ProofUploadResponseDto
import sg.mesha.goatos.core.network.dto.UploadedProofListResponseDto
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
import sg.mesha.goatos.core.network.dto.TemporaryTaggedGoatsResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionShedDrilldownDto
import sg.mesha.goatos.core.network.dto.VaccinationGapsResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationCoverageResponseDto
import sg.mesha.goatos.core.network.dto.AppConfigResponseDto
import sg.mesha.goatos.core.network.dto.VerificationQueueResponseDto
import sg.mesha.goatos.core.network.dto.VerificationReviewEventBatchRequestDto
import sg.mesha.goatos.core.network.dto.VerificationReviewEventBatchResponseDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictRequestDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictResponseDto
import sg.mesha.goatos.core.network.dto.WeighingWeightCorrectionRequestDto
import sg.mesha.goatos.core.network.dto.WeighingWeightCorrectionResponseDto
import sg.mesha.goatos.core.network.dto.VerificationCloseRequestDto
import sg.mesha.goatos.core.network.dto.VerificationCloseSubmissionResponseDto
import sg.mesha.goatos.core.network.dto.WorkflowActionAnswerRequestDto
import sg.mesha.goatos.core.network.dto.WorkflowActionCompleteRequestDto
import sg.mesha.goatos.core.network.dto.WorkflowActionWriteResponseDto
import sg.mesha.goatos.core.network.dto.WorkflowDetailResponseDto
import sg.mesha.goatos.core.network.dto.WorkflowListResponseDto
import sg.mesha.goatos.core.network.dto.WeighingFastingShedCardListResponseDto
import sg.mesha.goatos.core.network.dto.WeighingFastingShedCardResponseDto
import sg.mesha.goatos.core.network.dto.SubmitWeighingFastingShedRequestDto
import sg.mesha.goatos.core.network.dto.WeighingAnimalObservationRequestDto
import sg.mesha.goatos.core.network.dto.WeighingCampaignResponseDto
import sg.mesha.goatos.core.network.dto.WeighingCampaignDetailResponseDto
import sg.mesha.goatos.core.network.dto.WeighingCampaignListResponseDto
import sg.mesha.goatos.core.network.dto.WeighingParkListResponseDto
import sg.mesha.goatos.core.network.dto.WeighingCampaignShedPageResponseDto
import sg.mesha.goatos.core.network.dto.WeighingCreateCampaignRequestDto
import sg.mesha.goatos.core.network.dto.WeighingObservationResponseDto
import sg.mesha.goatos.core.network.dto.WeighingPlannerCatalogResponseDto
import sg.mesha.goatos.core.network.dto.WeighingPlannerParkBucketsResponseDto
import sg.mesha.goatos.core.network.dto.WeighingRosterResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationAlertPageResponseDto
import sg.mesha.goatos.core.network.dto.WeighingAlertPageResponseDto
import sg.mesha.goatos.core.network.dto.WeighingLeadershipShedVideosResponseDto
import sg.mesha.goatos.core.network.dto.WeighingShedObservationRequestDto
import sg.mesha.goatos.core.network.dto.WeighingScopeCloseRequestDto
import sg.mesha.goatos.core.network.dto.WeighingScopeReopenRequestDto
import sg.mesha.goatos.core.network.dto.WeighingScopeSubmitRequestDto

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

    @POST("app/analytics/events")
    suspend fun recordAnalyticsEvent(@Body request: AppAnalyticsEventRequestDto): AppAnalyticsEventResponseDto

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
        @Query("include_filter_options") includeFilterOptions: Boolean?,
        @Query("include_card_summaries") includeCardSummaries: Boolean?,
    ): VaccinationExecutionResponseDto

    @GET("app/vaccination/execution/sheds/{shed_id}")
    suspend fun getVaccinationExecutionShed(
        @Path("shed_id") shedId: String,
        @Query("as_of") asOf: String?,
        @Query("due_before") dueBefore: String?,
        @Query("limit") limit: Int?,
        @Query("partition_label") partitionLabel: String?,
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
        @Query("include_drive_summary") includeDriveSummary: Boolean?,
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
        @Query("partition_label") partitionLabel: String?,
    ): ShedCompletionSummaryDto

    /**
     * Lists weighing campaigns for ONE weighing surface.
     *
     * [scope] names the surface the caller is rendering rather than letting the server infer it
     * from the actor's roles: "mine" is the caller's own assigned sheds (the only executable
     * list), "all" is the planner's flat all-tasks list, and "operators" is read-only oversight
     * of other people's work. Omitting it means "mine".
     */
    @GET("app/weighing/campaigns")
    suspend fun listWeighingCampaigns(
        @Query("scope") scope: String? = null,
        @Query("cursor") cursor: String? = null,
        @Query("limit") limit: Int = WEIGHING_PAGE_SIZE,
        @Query("park_id") parkId: String? = null,
    ): WeighingCampaignListResponseDto

    // Declared before the {campaign_id} pattern so the literal "parks" segment reads as what it
    // is -- a sibling route, not a campaign id. Retrofit matches on the annotation, not order.
    @GET("app/weighing/parks")
    suspend fun listWeighingParks(): WeighingParkListResponseDto

    // Declared before the {campaign_id} pattern for the same readability reason as "parks" above:
    // the literal "fasting" segment is a sibling route, never a campaign id.
    @GET("app/weighing/fasting")
    suspend fun listWeighingFastingShedCards(
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int,
    ): WeighingFastingShedCardListResponseDto

    @POST("app/weighing/fasting/{fasting_task_id}/sheds/{campaign_shed_id}/submit")
    suspend fun submitWeighingFastingShed(
        @Path("fasting_task_id") fastingTaskId: String,
        @Path("campaign_shed_id") campaignShedId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: SubmitWeighingFastingShedRequestDto,
    ): WeighingFastingShedCardResponseDto

    @GET("app/weighing/campaigns/{campaign_id}")
    suspend fun getWeighingCampaign(
        @Path("campaign_id") campaignId: String,
    ): WeighingCampaignDetailResponseDto

    @GET("app/weighing/campaigns/{campaign_id}/sheds")
    suspend fun listWeighingCampaignSheds(
        @Path("campaign_id") campaignId: String,
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int,
    ): WeighingCampaignShedPageResponseDto

    @GET("app/weighing/planner/catalog")
    suspend fun getWeighingPlannerCatalog(
        @Query("period_start_date") periodStartDate: String,
    ): WeighingPlannerCatalogResponseDto

    @GET("app/weighing/planner/parks/{park_id}/buckets")
    suspend fun getWeighingPlannerParkBuckets(
        @Path("park_id") parkId: String,
        @Query("period_start_date") periodStartDate: String,
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int,
        @Query("exclude_campaign_id") excludeCampaignId: String? = null,
    ): WeighingPlannerParkBucketsResponseDto

    @POST("weighing/campaigns")
    suspend fun createWeighingCampaign(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: WeighingCreateCampaignRequestDto,
    ): WeighingCampaignResponseDto

    @PUT("weighing/campaigns/{campaign_id}")
    suspend fun updateWeighingCampaign(
        @Path("campaign_id") campaignId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: WeighingCreateCampaignRequestDto,
    ): WeighingCampaignResponseDto

    @POST("weighing/campaigns/{campaign_id}/publish")
    suspend fun publishWeighingCampaign(
        @Path("campaign_id") campaignId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
    ): WeighingCampaignResponseDto

    @GET("app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/roster")
    suspend fun getWeighingRoster(
        @Path("campaign_id") campaignId: String,
        @Path("campaign_shed_id") campaignShedId: String,
        @Query("observations_cursor") observationsCursor: String?,
        @Query("limit") limit: Int,
    ): WeighingRosterResponseDto

    @GET("app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/videos")
    suspend fun getWeighingLeadershipShedVideos(
        @Path("campaign_id") campaignId: String,
        @Path("campaign_shed_id") campaignShedId: String,
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int,
    ): WeighingLeadershipShedVideosResponseDto

    @GET("app/vaccination/alerts")
    suspend fun listVaccinationAlerts(
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int,
    ): VaccinationAlertPageResponseDto

    @GET("app/weighing/alerts")
    suspend fun listWeighingAlerts(
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int,
    ): WeighingAlertPageResponseDto

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

    @POST("app/weighing/campaigns/{campaign_id}/animal-observations")
    suspend fun recordWeighingAnimalObservation(
        @Path("campaign_id") campaignId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: WeighingAnimalObservationRequestDto,
    ): WeighingObservationResponseDto

    @POST("app/weighing/campaigns/{campaign_id}/shed-observations")
    suspend fun recordWeighingShedObservation(
        @Path("campaign_id") campaignId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: WeighingShedObservationRequestDto,
    ): WeighingObservationResponseDto

    @POST("app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/submit")
    suspend fun submitWeighingScope(
        @Path("campaign_id") campaignId: String,
        @Path("campaign_shed_id") campaignShedId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: WeighingScopeSubmitRequestDto,
    )

    @POST("app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/reopen")
    suspend fun reopenWeighingScope(
        @Path("campaign_id") campaignId: String,
        @Path("campaign_shed_id") campaignShedId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: WeighingScopeReopenRequestDto,
    )

    @POST("app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/close")
    suspend fun closeShedWeighingCampaign(
        @Path("campaign_id") campaignId: String,
        @Path("campaign_shed_id") campaignShedId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: WeighingScopeCloseRequestDto,
    )

    @POST("app/weighing/campaigns/{campaign_id}/close")
    suspend fun closeWeighingCampaign(
        @Path("campaign_id") campaignId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: WeighingScopeCloseRequestDto,
    )

    // @Streaming: this is a FILE download (text/csv), not a JSON body -- without it Retrofit
    // would buffer the whole response into memory before handing back the ResponseBody, which
    // defeats the point of streaming the body straight through without a second in-memory copy.
    // Path is the PLANNER route -- NOT under `/app` like the rest of this interface -- registered
    // in backend/internal/permissions/routes.go as exportWeighingCampaignCSV.
    @Streaming
    @GET("weighing/campaigns/{campaign_id}/export")
    suspend fun exportWeighingCampaignCsv(@Path("campaign_id") campaignId: String): ResponseBody

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
        @Query("partition_label") partitionLabel: String?,
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

    @GET("app/proofs/uploads")
    suspend fun listUploadedProofs(
        @Query("scope_type") scopeType: String,
        @Query("scope_id") scopeId: String,
        @Query("client_task_key") clientTaskKey: String?,
        @Query("field_key") fieldKey: String?,
        @Query("limit") limit: Int?,
    ): UploadedProofListResponseDto

    @GET("app/proofs/{proof_id}/download")
    suspend fun getProofDownloadUrl(
        @Path("proof_id") proofId: String,
    ): ProofDownloadUrlResponseDto

    @DELETE("app/proofs/{proof_id}")
    suspend fun deleteProof(@Path("proof_id") proofId: String)

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
        @Query("status") status: String?,
        @Query("business_date") businessDate: String?,
        @Query("missed") missed: Boolean?,
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

    /**
     * THE VERIFIER'S WEIGHT CORRECTION (maintainer decision 2026-08-17). Weighing owns the route --
     * the correction writes a weighing record -- while the verification item tells the app WHICH
     * record to address, via measurement_correction. Same endpoint admin-web calls: one act, one
     * rule, one route.
     */
    @POST("app/weighing/observations/{observation_id}/weight-correction")
    suspend fun correctWeighingObservationWeight(
        @Path("observation_id") observationId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: WeighingWeightCorrectionRequestDto,
    ): WeighingWeightCorrectionResponseDto

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

    @POST("verification/review-events")
    suspend fun recordVerificationReviewEvents(
        @Body request: VerificationReviewEventBatchRequestDto,
    ): VerificationReviewEventBatchResponseDto

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

    @GET("app/counts/breeds")
    suspend fun getAppCountsBreeds(): CountsBreedsResponseDto

    @GET("app/counts/shifting-events/pending-execution")
    suspend fun listCountsShiftingPendingExecution(
        @Query("date") date: String?,
        @Query("status") status: String?,
        @Query("page_size") pageSize: Int?,
        @Query("cursor") cursor: String?,
    ): CountsShiftingPendingExecutionResponseDto

    @GET("app/counts/goats/temporary-tagged")
    suspend fun listCountsTemporaryTaggedGoats(
        @Query("page_size") pageSize: Int?,
        @Query("cursor") cursor: String?,
        @Query("park_id") parkId: String?,
        @Query("shed_id") shedId: String?,
    ): TemporaryTaggedGoatsResponseDto

    @POST("app/counts/goats/{goat_id}/promote-identifier")
    suspend fun promoteCountsIdentifier(
        @Path("goat_id") goatId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: CountsPromoteIdentifierRequestDto,
    ): CountsPromoteIdentifierResponseDto

    @POST("app/counts/shifting-events/{shifting_event_id}/complete")
    suspend fun completeCountsShiftingEvent(
        @Path("shifting_event_id") shiftingEventId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: CountsShiftingCompleteRequestDto,
    ): CountsShiftingExecutionResponseDto

    @POST("app/counts/shifting-events/{shifting_event_id}/cancel")
    suspend fun cancelCountsShiftingEvent(
        @Path("shifting_event_id") shiftingEventId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: CountsShiftingCancelRequestDto,
    ): CountsShiftingExecutionResponseDto

    @GET("app/counts/pen-reconciliation/cards")
    suspend fun listCountsPenReconciliationCards(
        @Query("status") status: String?,
        @Query("page_size") pageSize: Int?,
        @Query("cursor") cursor: String?,
    ): CountsPenReconciliationListResponseDto

    @POST("app/counts/pen-reconciliation/cards/{card_id}/complete")
    suspend fun completeCountsPenReconciliationCard(
        @Path("card_id") cardId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: CountsPenReconciliationCompleteRequestDto,
    ): CountsPenReconciliationCompleteResponseDto

    @GET("feed-direction/preview")
    suspend fun getFeedDirectionPreview(
        @Query("park_id") parkId: String,
        @Query("target_date") targetDate: String,
        @Query("shed_id") shedId: String?,
        @Query("partition_label") partitionLabel: String?,
        @Query("session") session: Int?,
        @Query("workflow") workflow: String?,
        @Query("status") status: String?,
        @Query("limit") limit: Int?,
        @Query("offset") offset: Int?,
    ): FeedDirectionPreviewPageDto

    @GET("feed-packing/worklist")
    suspend fun getFeedPackingWorklist(
        @Query("park_id") parkId: String,
        @Query("target_date") targetDate: String,
        @Query("shed_id") shedId: String?,
        @Query("partition_label") partitionLabel: String?,
        @Query("session") session: Int?,
        @Query("workflow") workflow: String?,
        @Query("status") status: String?,
        @Query("limit") limit: Int?,
        @Query("offset") offset: Int?,
    ): FeedPackingWorklistPageDto

    @GET("feed-wastage/worklist")
    suspend fun getFeedWastageWorklist(
        @Query("park_id") parkId: String,
        @Query("target_date") targetDate: String,
        @Query("shed_id") shedId: String?,
        @Query("partition_label") partitionLabel: String?,
        @Query("status") status: String?,
        @Query("limit") limit: Int?,
        @Query("offset") offset: Int?,
    ): FeedWastageWorklistPageDto

    @POST("feed-direction/wastage/complete")
    suspend fun completeFeedWastage(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: FeedWastageCompleteRequestDto,
    ): FeedWastageCompleteResponseDto

    /**
     * THE VERIFIER'S WASTAGE MEASUREMENT (maintainer decision 2026-08-18). Feed owns the route —
     * the measurement writes a feed-wastage record — while the verification item tells the app
     * WHICH record to address, via measurement_correction. Same endpoint admin-web calls.
     */
    @POST("feed-direction/wastage/{completion_id}/measurement")
    suspend fun recordFeedWastageMeasurement(
        @Path("completion_id") completionId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: FeedWastageMeasurementRequestDto,
    ): FeedWastageMeasurementResponseDto


    // ------------------------------------------------------------------
    // PC Care (module pc_care, maintainer decision 2026-08-21)
    // ------------------------------------------------------------------

    @GET("app/pc-care/worklist")
	    suspend fun getPcCareWorklist(
	        @Query("category") category: String,
	        @Query("date") date: String,
	        @Query("limit") limit: Int?,
	        @Query("cursor") cursor: String?,
	    ): PcCareTaskPageDto

    @GET("app/pc-care/tasks")
    suspend fun getPcCareTasks(
        @Query("date") date: String,
	        @Query("park_id") parkId: String?,
	        @Query("category") category: String?,
	        @Query("limit") limit: Int?,
	        @Query("cursor") cursor: String?,
	    ): PcCareTaskPageDto

    @GET("app/pc-care/tasks/{task_id}")
    suspend fun getPcCareTask(
        @Path("task_id") taskId: String,
    ): PcCareTaskDto

    @GET("app/pc-care/tasks/{task_id}/captures")
    suspend fun getPcCareTaskCaptures(
        @Path("task_id") taskId: String,
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int?,
    ): PcCareCapturesDto

    @GET("app/pc-care/tasks/{task_id}/roster")
    suspend fun getPcCareTaskRoster(
        @Path("task_id") taskId: String,
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int?,
    ): PcCareTaskRosterDto

    @POST("app/pc-care/tasks/{task_id}/animals")
    suspend fun scanPcCareAnimal(
        @Path("task_id") taskId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: PcCareScanRequestDto,
    ): PcCareScanResponseDto

    @PUT("app/pc-care/tasks/{task_id}/animals/{animal_row_id}/proofs/{slot}")
    suspend fun registerPcCareSlotProof(
        @Path("task_id") taskId: String,
        @Path("animal_row_id") animalRowId: String,
        @Path("slot") slot: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: PcCareSlotProofRequestDto,
    ): Unit

    @PUT("app/pc-care/tasks/{task_id}/proofs/{slot}")
    suspend fun registerPcCareTaskProof(
        @Path("task_id") taskId: String,
        @Path("slot") slot: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: PcCareSlotProofRequestDto,
    ): Unit

    @POST("app/pc-care/tasks/{task_id}/submit")
    suspend fun submitPcCareTask(
        @Path("task_id") taskId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
    ): PcCareSubmitResponseDto

    @GET("app/pc-care/planner/catalog")
    suspend fun getPcCarePlannerCatalog(): PcCarePlannerCatalogDto

    @GET("app/pc-care/planner/parks/{park_id}/sheds")
    suspend fun getPcCarePlannerParkSheds(
        @Path("park_id") parkId: String,
        @Query("category") category: String,
        @Query("date") date: String,
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int?,
    ): PcCarePlannerShedsDto

    @POST("app/pc-care/tasks")
    suspend fun createPcCareTask(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: PcCareCreateTaskRequestDto,
    ): PcCareTaskDto

    @POST("app/pc-care/rounds")
    suspend fun createPcCareRound(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: PcCareCreateRoundRequestDto,
    ): PcCareRoundDto

    @GET("app/pc-care/rounds")
    suspend fun getPcCareRoundCards(
        @Query("date") date: String?,
        @Query("category") category: String?,
        @Query("park_id") parkId: String?,
        @Query("filter") filter: String?,
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int?,
    ): PcCareRoundCardPageDto

    @GET("app/pc-care/rounds/{round_id}")
    suspend fun getPcCareRound(
        @Path("round_id") roundId: String,
    ): PcCareRoundDto

    @GET("app/pc-care/tasks/{task_id}/removal-pens")
    suspend fun getPcCareRemovalPens(
        @Path("task_id") taskId: String,
    ): PcCareRemovalPenListDto

    @PUT("app/pc-care/tasks/{task_id}/removal-pens/proofs/{slot}")
    suspend fun putPcCareRemovalPenProof(
        @Path("task_id") taskId: String,
        @Path("slot") slot: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: PcCareRemovalPenProofRequestDto,
    )

    @POST("app/pc-care/tasks/{task_id}/close")
    suspend fun closePcCareTask(
        @Path("task_id") taskId: String,
        @Body request: PcCareCloseRequestDto,
    ): Unit

    @POST("app/pc-care/tasks/{task_id}/reopen")
    suspend fun reopenPcCareTask(
        @Path("task_id") taskId: String,
    ): Unit

    @POST("app/pc-care/rounds/{round_id}/close")
    suspend fun closePcCareRound(
        @Path("round_id") roundId: String,
        @Body request: PcCareCloseRequestDto,
    ): Unit

    // The PC Director's approve/reject on a submitted vaccine-stock task (maintainer decision
    // 2026-09-02): gated server-side on pc_care.stock_approve — never the verifier's route.
    @POST("app/pc-care/tasks/{task_id}/stock-verdict")
    suspend fun recordPcCareStockVerdict(
        @Path("task_id") taskId: String,
        @Body request: PcCareStockVerdictRequestDto,
    ): PcCareTaskDto

    // ------------------------------------------------------------------
    // Toxin (aflatoxin strip test, maintainer decision 2026-08-25)
    // ------------------------------------------------------------------

    @GET("app/toxin/tasks")
    suspend fun getToxinTasks(
        @Query("filter") filter: String?,
        @Query("limit") limit: Int?,
        @Query("cursor") cursor: String?,
    ): ToxinTaskPageDto

    @GET("app/toxin/tasks/{task_id}")
    suspend fun getToxinTask(
        @Path("task_id") taskId: String,
    ): ToxinTaskDetailDto

    @POST("app/toxin/tasks/{task_id}/steps/{step_no}/complete")
    suspend fun completeToxinStep(
        @Path("task_id") taskId: String,
        @Path("step_no") stepNo: Int,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: ToxinStepCompleteRequestDto,
    ): ToxinTaskDetailDto

    @POST("app/toxin/tasks/{task_id}/submit")
    suspend fun submitToxinReading(
        @Path("task_id") taskId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: ToxinSubmitRequestDto,
    ): ToxinTaskDetailDto

    // Vendors (maintainer decision 2026-09-03): the same procurement routes the web uses.
    @GET("procurement/vendors")
    suspend fun getProcurementVendors(
        @Query("search") search: String?,
        @Query("status") status: String?,
        @Query("limit") limit: Int?,
        @Query("offset") offset: Int?,
    ): VendorPageDto

    @GET("procurement/vendors/{vendor_id}")
    suspend fun getProcurementVendor(@Path("vendor_id") vendorId: String): VendorDto

    @GET("procurement/vendor-catalog")
    suspend fun getProcurementVendorCatalog(): VendorCatalogDto

    @POST("procurement/vendors")
    suspend fun createProcurementVendor(@Body request: VendorWriteDto): VendorDto

    @GET("procurement/feed-purchases")
    suspend fun getFeedPurchases(
        @Query("farm") farm: String?,
        @Query("delivery") delivery: String?,
        @Query("limit") limit: Int?,
        @Query("offset") offset: Int?,
    ): FeedPurchasePageDto

    @GET("procurement/feed-purchase-options")
    suspend fun getFeedPurchaseOptions(): FeedPurchaseOptionsDto

    @POST("procurement/feed-purchases")
    suspend fun createFeedPurchase(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: FeedPurchaseWriteDto,
    ): FeedPurchaseDto

    // Sales (maintainer instruction 2026-09-04): the same routes the web's /sales/config uses.
    @GET("sales/deals")
    suspend fun getSalesDeals(
        @Query("farm") farm: String?,
        @Query("limit") limit: Int?,
        @Query("offset") offset: Int?,
    ): SalesDealPageDto

    @GET("sales/options")
    suspend fun getSalesOptions(): SalesOptionsDto

    @GET("procurement/vendor-options")
    suspend fun getVendorOptions(): VendorOptionsDto


    // Changing a recorded load (maintainer instruction 2026-09-04). Each returns the WHOLE load.
    @POST("procurement/feed-purchases/{purchase_id}/payments")
    suspend fun createFeedPurchasePayment(
        @Path("purchase_id") purchaseId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: FeedPurchasePaymentWriteDto,
    ): FeedPurchaseDto

    @PUT("procurement/feed-purchases/{purchase_id}/payment-status")
    suspend fun setFeedPurchasePaymentStatus(
        @Path("purchase_id") purchaseId: String,
        @Body request: FeedPurchaseStatusWriteDto,
    ): FeedPurchaseDto

    @PUT("procurement/feed-purchases/{purchase_id}")
    suspend fun editFeedPurchase(
        @Path("purchase_id") purchaseId: String,
        @Body request: FeedPurchaseEditDto,
    ): FeedPurchaseDto

    @PUT("procurement/feed-purchases/{purchase_id}/delivery")
    suspend fun recordFeedPurchaseDelivery(
        @Path("purchase_id") purchaseId: String,
        @Body request: FeedPurchaseDeliveryWriteDto,
    ): FeedPurchaseDto

    @POST("sales/deals")
    suspend fun createSalesDeal(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: SalesDealWriteDto,
    ): SalesDealDto

    @GET("admin/goats/sale-locations")
    suspend fun getSaleLocations(): SaleLocationsDto

    @GET("admin/goats/sale-candidates")
    suspend fun getSaleCandidates(
        @Query("park_id") parkId: String,
        @Query("shed_id") shedId: String?,
        @Query("partition_label") partitionLabels: List<String>,
        @Query("q") query: String?,
        @Query("limit") limit: Int?,
        @Query("cursor") cursor: String?,
    ): SaleCandidatePageDto

    @GET("admin/goats/sale-allocations/{sales_deal_id}")
    suspend fun getSaleAllocation(@Path("sales_deal_id") salesDealId: String): SaleAllocationDto

    @POST("admin/goats/sale-allocations/preview")
    suspend fun previewSaleAllocation(@Body request: SaleAllocationRequestDto): SalePreviewDto

    @POST("admin/goats/sale-allocations/confirm")
    suspend fun confirmSaleAllocation(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: SaleAllocationRequestDto,
    ): SaleAllocationDto

    // Editing a recorded sale (maintainer instruction 2026-09-04). Every one of these returns the
    // WHOLE updated deal, so the phone persists the server's row rather than patching its own.
    @POST("sales/deals/{deal_id}/payments")
    suspend fun createSalesDealPayment(
        @Path("deal_id") dealId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: SalesDealPaymentWriteDto,
    ): SalesDealDto

    @PUT("sales/deals/{deal_id}/payments/{payment_id}")
    suspend fun updateSalesDealPayment(
        @Path("deal_id") dealId: String,
        @Path("payment_id") paymentId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: SalesDealPaymentWriteDto,
    ): SalesDealDto

    @DELETE("sales/deals/{deal_id}/payments/{payment_id}")
    suspend fun deleteSalesDealPayment(
        @Path("deal_id") dealId: String,
        @Path("payment_id") paymentId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
    ): SalesDealDto

    @POST("sales/deals/{deal_id}/status")
    suspend fun setSalesDealStatus(
        @Path("deal_id") dealId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: SalesDealStatusWriteDto,
    ): SalesDealDto

    // Pipeline and evidence: the five panels the retired Sales DB sheet carried.
    @GET("sales/buyer-leads")
    suspend fun getSalesBuyerLeads(
        @Query("limit") limit: Int?,
        @Query("offset") offset: Int?,
    ): SalesBuyerLeadPageDto

    @POST("sales/buyer-leads")
    suspend fun createSalesBuyerLead(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: SalesBuyerLeadWriteDto,
    ): SalesBuyerLeadDto

    @POST("sales/buyer-leads/{lead_id}/status")
    suspend fun setSalesBuyerLeadStatus(
        @Path("lead_id") leadId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: SalesLeadStatusWriteDto,
    ): SalesBuyerLeadDto

    @GET("sales/fpo-leads")
    suspend fun getSalesFpoLeads(
        @Query("limit") limit: Int?,
        @Query("offset") offset: Int?,
    ): SalesFpoLeadPageDto

    @POST("sales/fpo-leads")
    suspend fun createSalesFpoLead(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: SalesFpoLeadWriteDto,
    ): SalesFpoLeadDto

    @POST("sales/fpo-leads/{lead_id}/status")
    suspend fun setSalesFpoLeadStatus(
        @Path("lead_id") leadId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: SalesLeadStatusWriteDto,
    ): SalesFpoLeadDto

    @POST("sales/market-benchmarks")
    suspend fun createSalesMarketBenchmark(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: SalesBenchmarkWriteDto,
    ): SalesRecordedDto

    @POST("sales/sold-tags")
    suspend fun createSalesSoldTags(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: SalesSoldTagsWriteDto,
    ): SalesSoldTagsResultDto

    @POST("sales/weight-checks")
    suspend fun createSalesWeightCheck(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: SalesWeightCheckWriteDto,
    ): SalesRecordedDto
    // Leadership Tasks (maintainer request 2026-09-04)
    @GET("app/leadership-tasks")
    suspend fun getLeadershipTasks(
        @Query("filter") filter: String?,
        @Query("limit") limit: Int?,
        @Query("cursor") cursor: String?,
    ): LeadershipTaskPageDto

    @GET("app/leadership-tasks/assignees")
    suspend fun getLeadershipTaskAssignees(): LeadershipAssigneeListDto

    @GET("app/leadership-tasks/{task_id}")
    suspend fun getLeadershipTask(
        @Path("task_id") taskId: String,
    ): LeadershipTaskDetailDto

    @POST("app/leadership-tasks")
    suspend fun raiseLeadershipTask(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: LeadershipTaskRaiseRequestDto,
    ): LeadershipTaskDetailDto

    @POST("app/leadership-tasks/{task_id}/edit")
    suspend fun editLeadershipTask(
        @Path("task_id") taskId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: LeadershipTaskEditRequestDto,
    ): LeadershipTaskDetailDto

    @POST("app/leadership-tasks/{task_id}/status")
    suspend fun changeLeadershipTaskStatus(
        @Path("task_id") taskId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: LeadershipTaskStatusRequestDto,
    ): LeadershipTaskDetailDto

    @POST("app/leadership-tasks/{task_id}/seen")
    suspend fun markLeadershipTaskSeen(
        @Path("task_id") taskId: String,
    ): LeadershipTaskDetailDto

    @POST("app/leadership-tasks/{task_id}/comment")
    suspend fun setLeadershipTaskComment(
        @Path("task_id") taskId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: LeadershipTaskCommentRequestDto,
    ): LeadershipTaskDetailDto

    @GET("app/leadership-tasks/{task_id}/attachments/{proof_id}/download")
    suspend fun getLeadershipTaskAttachmentDownloadUrl(
        @Path("task_id") taskId: String,
        @Path("proof_id") proofId: String,
    ): ProofDownloadUrlResponseDto

    @GET("feed-direction/distribution/captures")
    suspend fun getFeedDistributionCaptures(
        @Query("park_id") parkId: String?,
        @Query("shed_id") shedId: String,
        @Query("partition_label") partitionLabel: String?,
        @Query("session_no") sessionNo: Int,
        @Query("target_date") targetDate: String,
        @Query("workflow") workflow: String,
    ): FeedDistributionCapturesDto

    @POST("feed-direction/complete")
    suspend fun completeFeedDirectionSession(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: FeedDirectionCompleteRequestDto,
    ): FeedDirectionCompleteResponseDto

    @POST("feed-direction/distribution/complete")
    suspend fun completeFeedDistribution(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: FeedDistributionCompleteRequestDto,
    ): FeedDistributionCompleteResponseDto

    @POST("feed-direction/packing/complete")
    suspend fun completeFeedPacking(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: FeedPackingCompleteRequestDto,
    ): FeedPackingCompleteResponseDto

    @POST("app/counts/milk-preparation/submit")
    suspend fun submitMilkPreparation(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: MilkPreparationSubmissionRequestDto,
    ): MilkPreparationSubmissionResponseDto

    @GET("app/counts/milk-preparation")
    suspend fun getMilkPreparation(
        @Query("preparation_date") preparationDate: String?,
        @Query("park_id") parkId: String?,
        @Query("limit") limit: Int,
        @Query("offset") offset: Int,
    ): MilkPreparationPageDto

    @GET("app/counts/milk-feeding/tasks")
    suspend fun getMilkFeedingTasks(@Query("feeding_date") feedingDate: String, @Query("park_id") parkId: String?, @Query("session_no") sessionNo: Int?, @Query("limit") limit: Int, @Query("offset") offset: Int): MilkFeedingPageDto

    @POST("app/counts/milk-feeding/tasks/{task_id}/submit")
    suspend fun submitMilkFeedingTask(@Path("task_id") taskId: String, @Header("Idempotency-Key") idempotencyKey: String, @Body request: MilkFeedingSubmitRequestDto): MilkFeedingSubmitResponseDto

    // No partition filter: transport is one task per physical shed, so shed_id is the finest
    // location this list narrows to.
    @GET("feed-transport/tasks")
    suspend fun getFeedTransportTasks(
        @Query("business_date") businessDate: String,
        @Query("park_id") parkId: String?,
        @Query("shed_id") shedId: String?,
        @Query("status") status: String?,
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int?,
    ): FeedTransportTaskPageDto

    @POST("feed-transport/tasks/{task_id}/submit")
    suspend fun submitFeedTransport(@Path("task_id") taskId: String, @Header("Idempotency-Key") idempotencyKey: String, @Body request: FeedTransportSubmitRequestDto): FeedTransportSubmitResponseDto

    @POST("app/counts/birth-events")
    suspend fun recordCountsBirthEvent(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: CountsBirthEventRequestDto,
    ): CountsApprovalSubmitResponseDto

    @POST("app/counts/death-events")
    suspend fun recordCountsDeathEvent(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: CountsDeathEventRequestDto,
    ): CountsApprovalSubmitResponseDto

    @GET("app/workflows")
    suspend fun listWorkflows(
        @Query("module") module: String,
        @Query("date") date: String?,
        @Query("filter") filter: String?,
        @Query("page_size") pageSize: Int?,
        @Query("cursor") cursor: String?,
    ): WorkflowListResponseDto

    @GET("app/workflows/{workflow_id}")
    suspend fun getWorkflow(
        @Path("workflow_id") workflowId: String,
        @Query("lens") lens: String? = null,
        @Query("date") date: String? = null,
    ): WorkflowDetailResponseDto

    @POST("app/workflows/{workflow_id}/actions/{action_id}/answer")
    suspend fun answerWorkflowAction(
        @Path("workflow_id") workflowId: String,
        @Path("action_id") actionId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: WorkflowActionAnswerRequestDto,
    ): WorkflowActionWriteResponseDto

    @POST("app/workflows/{workflow_id}/actions/{action_id}/complete")
    suspend fun completeWorkflowAction(
        @Path("workflow_id") workflowId: String,
        @Path("action_id") actionId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: WorkflowActionCompleteRequestDto,
    ): WorkflowActionWriteResponseDto

    @GET("app/health/work-items")
    suspend fun listHealthWorkItems(
        @Query("age_band") ageBand: String,
        @Query("date") date: String,
        @Query("status") status: String?,
        @Query("disease_key") diseaseKey: String?,
        @Query("park_id") parkId: String?,
        @Query("shed_id") shedId: String?,
        @Query("session") session: String?,
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int?,
    ): HealthWorkItemPageDto

    @POST("app/health/cases")
    suspend fun openHealthCase(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: HealthOpenCaseRequestDto,
    ): HealthOpenCaseResponseDto

    @GET("app/health/work-items/{health_session_id}")
    suspend fun getHealthWorkItem(
        @Path("health_session_id") healthSessionId: String,
    ): HealthWorkItemDetailDto

    @POST("app/health/work-items/{health_session_id}/complete")
    suspend fun completeHealthWorkItem(
        @Path("health_session_id") healthSessionId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: HealthCompleteRequestDto,
    ): HealthCompleteResponseDto

    // The diagnosis engine. Submit PROPOSES; only confirm opens a treatment course,
    // and the two carry different permissions server-side.
    @POST("app/health/observations")
    suspend fun submitHealthObservation(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: SubmitHealthObservationRequestDto,
    ): HealthDiagnosisProposalResponseDto

    @GET("app/health/observations")
    suspend fun listHealthObservations(
        @Query("status") status: String?,
        @Query("goat_id") goatId: String?,
        @Query("cursor") cursor: String?,
        @Query("limit") limit: Int?,
    ): HealthDiagnosisQueuePageDto

    @GET("app/health/observations/{health_diagnosis_run_id}")
    suspend fun getHealthObservation(
        @Path("health_diagnosis_run_id") diagnosisRunId: String,
    ): HealthDiagnosisRunDto

    @POST("app/health/observations/{health_diagnosis_run_id}/confirm")
    suspend fun confirmHealthDiagnosis(
        @Path("health_diagnosis_run_id") diagnosisRunId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: ConfirmHealthDiagnosisRequestDto,
    ): ConfirmHealthDiagnosisResponseDto
    @POST("app/health/cases/{health_case_id}/close")
    suspend fun closeHealthCase(
        @Path("health_case_id") healthCaseId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: HealthCloseCaseRequestDto,
    ): HealthCloseCaseResponseDto

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

    // --- Clock In / Clock Out (docs/features/clock-in-out/plan.md) --------------------------
    // The Idempotency-Key header mirrors the body's idempotency_key (the contract accepts both;
    // body wins server-side) so the punch shares the same header convention every other outbox
    // write here uses.

    @POST("app/clock/in")
    suspend fun recordClockIn(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: ClockPunchRequestDto,
    ): ClockPunchResponseDto

    @POST("app/clock/out")
    suspend fun recordClockOut(
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: ClockPunchRequestDto,
    ): ClockPunchResponseDto

    @GET("app/clock/status")
    suspend fun getClockStatus(): ClockStatusResponseDto

    @GET("app/clock/presence")
    suspend fun listClockPresence(
        @Query("date") date: String?,
        @Query("park_id") parkId: String?,
        @Query("designation") designation: String?,
        @Query("bucket") bucket: String?,
        @Query("q") q: String?,
        @Query("limit") limit: Int?,
        @Query("cursor") cursor: String?,
    ): ClockPresenceResponseDto

    @GET("app/clock/presence/{workforce_member_id}")
    suspend fun getClockPresencePerson(
        @Path("workforce_member_id") workforceMemberId: String,
        @Query("date") date: String?,
    ): ClockPersonDayResponseDto
}

/** Adapts the Retrofit service to the [AppApi] port so callers stay Retrofit-agnostic.
 *  [blobUploader] handles the binary PUT half of the proof-upload flow — a separate collaborator
 *  because it targets an arbitrary signed URL (often a third-party storage host), not this
 *  Retrofit service's fixed JSON base URL. */
class RetrofitAppApi(
    private val service: AppApiService,
    private val blobUploader: ProofBlobUploader,
    private val baseUrl: String = "",
) : AppApi {
    override suspend fun recordAuthSessionEvent(request: AuthSessionEventRequestDto) =
        service.recordAuthSessionEvent(request)

    override suspend fun recordAnalyticsEvent(request: AppAnalyticsEventRequestDto): AppAnalyticsEventResponseDto =
        service.recordAnalyticsEvent(request)

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
        includeFilterOptions: Boolean,
        includeCardSummaries: Boolean,
    ): VaccinationExecutionResponseDto =
        service.listVaccinationExecution(parkId, workState, asOf, dueBefore, openOnly, limit, cursor, includeFilterOptions, includeCardSummaries)

    override suspend fun getVaccinationExecutionShed(
        shedId: String,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
        partitionLabel: String?,
    ): VaccinationExecutionShedDrilldownDto =
        service.getVaccinationExecutionShed(shedId, asOf, dueBefore, limit, partitionLabel)

    override suspend fun listCalendarVaccinationEvents(
        parkId: String?,
        shedId: String?,
        ownerKey: String?,
        status: String?,
        dateFrom: String?,
        dateTo: String?,
        includeDateMarkers: Boolean,
        includeDriveSummary: Boolean,
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
            includeDriveSummary.takeIf { it },
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

    override suspend fun getShedCompletionSummary(taskId: String, shedId: String?, partitionLabel: String?): ShedCompletionSummaryDto =
        service.getShedCompletionSummary(taskId, shedId, partitionLabel)

    override suspend fun listWeighingCampaigns(
        scope: String?,
        cursor: String?,
        limit: Int,
        parkId: String?,
    ): WeighingCampaignListResponseDto =
        service.listWeighingCampaigns(scope = scope, cursor = cursor, limit = limit, parkId = parkId)

    override suspend fun getWeighingCampaign(campaignId: String): WeighingCampaignDetailResponseDto =
        service.getWeighingCampaign(campaignId)

    override suspend fun listWeighingParks(): WeighingParkListResponseDto = service.listWeighingParks()

    override suspend fun listWeighingFastingShedCards(
        cursor: String?,
        limit: Int,
    ): WeighingFastingShedCardListResponseDto = service.listWeighingFastingShedCards(cursor, limit)

    override suspend fun submitWeighingFastingShed(
        fastingTaskId: String,
        campaignShedId: String,
        idempotencyKey: String,
        request: SubmitWeighingFastingShedRequestDto,
    ): WeighingFastingShedCardResponseDto =
        service.submitWeighingFastingShed(fastingTaskId, campaignShedId, idempotencyKey, request)

    override suspend fun listWeighingCampaignSheds(
        campaignId: String,
        cursor: String?,
        limit: Int,
    ): WeighingCampaignShedPageResponseDto = service.listWeighingCampaignSheds(campaignId, cursor, limit)

    override suspend fun getWeighingPlannerCatalog(
        periodStartDate: String,
    ): WeighingPlannerCatalogResponseDto = service.getWeighingPlannerCatalog(periodStartDate)

    override suspend fun getWeighingPlannerParkBuckets(
        parkId: String,
        periodStartDate: String,
        cursor: String?,
        limit: Int,
        excludeCampaignId: String?,
    ): WeighingPlannerParkBucketsResponseDto =
        service.getWeighingPlannerParkBuckets(parkId, periodStartDate, cursor, limit, excludeCampaignId)

    override suspend fun createWeighingCampaign(
        idempotencyKey: String,
        request: WeighingCreateCampaignRequestDto,
    ): WeighingCampaignResponseDto = service.createWeighingCampaign(idempotencyKey, request)

    override suspend fun updateWeighingCampaign(
        campaignId: String,
        idempotencyKey: String,
        request: WeighingCreateCampaignRequestDto,
    ): WeighingCampaignResponseDto = service.updateWeighingCampaign(campaignId, idempotencyKey, request)

    override suspend fun publishWeighingCampaign(
        campaignId: String,
        idempotencyKey: String,
    ): WeighingCampaignResponseDto = service.publishWeighingCampaign(campaignId, idempotencyKey)

    override suspend fun getWeighingRoster(
        campaignId: String,
        campaignShedId: String,
        observationsCursor: String?,
        limit: Int,
    ): WeighingRosterResponseDto = service.getWeighingRoster(campaignId, campaignShedId, observationsCursor, limit)

    override suspend fun getWeighingLeadershipShedVideos(
        campaignId: String,
        campaignShedId: String,
        cursor: String?,
        limit: Int,
    ): WeighingLeadershipShedVideosResponseDto =
        service.getWeighingLeadershipShedVideos(campaignId, campaignShedId, cursor, limit)

    override suspend fun listVaccinationAlerts(
        cursor: String?,
        limit: Int,
    ): VaccinationAlertPageResponseDto = service.listVaccinationAlerts(cursor, limit)

    override suspend fun listWeighingAlerts(
        cursor: String?,
        limit: Int,
    ): WeighingAlertPageResponseDto = service.listWeighingAlerts(cursor, limit)

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

    override suspend fun recordWeighingAnimalObservation(
        campaignId: String,
        idempotencyKey: String,
        request: WeighingAnimalObservationRequestDto,
    ): WeighingObservationResponseDto = service.recordWeighingAnimalObservation(campaignId, idempotencyKey, request)

    override suspend fun recordWeighingShedObservation(
        campaignId: String,
        idempotencyKey: String,
        request: WeighingShedObservationRequestDto,
    ): WeighingObservationResponseDto = service.recordWeighingShedObservation(campaignId, idempotencyKey, request)

    override suspend fun submitWeighingScope(
        campaignId: String,
        campaignShedId: String,
        idempotencyKey: String,
        request: WeighingScopeSubmitRequestDto,
    ) = service.submitWeighingScope(campaignId, campaignShedId, idempotencyKey, request)

    override suspend fun reopenWeighingScope(
        campaignId: String,
        campaignShedId: String,
        idempotencyKey: String,
        request: WeighingScopeReopenRequestDto,
    ) = service.reopenWeighingScope(campaignId, campaignShedId, idempotencyKey, request)

    override suspend fun closeShedWeighingCampaign(
        campaignId: String,
        campaignShedId: String,
        idempotencyKey: String,
        request: WeighingScopeCloseRequestDto,
    ) = service.closeShedWeighingCampaign(campaignId, campaignShedId, idempotencyKey, request)

    override suspend fun closeWeighingCampaign(
        campaignId: String,
        idempotencyKey: String,
        request: WeighingScopeCloseRequestDto,
    ) = service.closeWeighingCampaign(campaignId, idempotencyKey, request)

    // .use { } closes the response body's underlying source once read, so the connection is
    // released even if `.bytes()` throws -- same discipline as every other network read here,
    // just with a raw byte body instead of a decoded DTO.
    override suspend fun exportWeighingCampaignCsv(campaignId: String): ByteArray =
        service.exportWeighingCampaignCsv(campaignId).use { it.bytes() }

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
        partitionLabel: String?,
    ): ScanRosterResponseDto = service.getScanRoster(shedId, taskId, cursor, limit, partitionLabel)

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

    override suspend fun listUploadedProofs(
        scopeType: String,
        scopeId: String,
        clientTaskKey: String?,
        fieldKey: String?,
        limit: Int?,
    ): UploadedProofListResponseDto = service.listUploadedProofs(scopeType, scopeId, clientTaskKey, fieldKey, limit)

    override suspend fun getProofDownloadUrl(proofId: String): String {
        val url = service.getProofDownloadUrl(proofId).downloadUrl
        // Local storage signs a RELATIVE path; a raw URL loader needs it absolute or the
        // teammate thumbnail silently never renders (device finding 2026-08-15).
        return if (url.startsWith("/")) baseUrl.trimEnd('/') + url else url
    }

    override suspend fun deleteProof(proofId: String) = service.deleteProof(proofId)

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
        status: String?,
        businessDate: String?,
        missed: Boolean?,
        parkId: String?,
        shedId: String?,
        cursor: String?,
        limit: Int?,
    ): VerificationQueueResponseDto = service.listVerificationQueue(category, status, businessDate, missed, parkId, shedId, cursor, limit)

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

    override suspend fun correctWeighingObservationWeight(
        observationId: String,
        idempotencyKey: String,
        request: WeighingWeightCorrectionRequestDto,
    ): WeighingWeightCorrectionResponseDto = service.correctWeighingObservationWeight(observationId, idempotencyKey, request)

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

    override suspend fun recordVerificationReviewEvents(
        request: VerificationReviewEventBatchRequestDto,
    ): VerificationReviewEventBatchResponseDto =
        service.recordVerificationReviewEvents(request)

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

    override suspend fun getAppCountsBreeds(): CountsBreedsResponseDto =
        service.getAppCountsBreeds()

    override suspend fun listCountsShiftingPendingExecution(
        date: String?,
        status: String?,
        pageSize: Int?,
        cursor: String?,
    ): CountsShiftingPendingExecutionResponseDto =
        service.listCountsShiftingPendingExecution(date, status, pageSize, cursor)

    override suspend fun listCountsTemporaryTaggedGoats(
        pageSize: Int?,
        cursor: String?,
        parkId: String?,
        shedId: String?,
    ): TemporaryTaggedGoatsResponseDto =
        service.listCountsTemporaryTaggedGoats(pageSize, cursor, parkId, shedId)

    override suspend fun promoteCountsIdentifier(
        goatId: String,
        idempotencyKey: String,
        permanentIdentifier: String,
        rowVersion: Int,
        secondaryIdentifier: String?,
    ): CountsPromoteIdentifierResponseDto =
        service.promoteCountsIdentifier(
            goatId,
            idempotencyKey,
            CountsPromoteIdentifierRequestDto(
                permanentIdentifier = permanentIdentifier,
                animalIdentifier2 = secondaryIdentifier?.trim()?.ifBlank { null },
                rowVersion = rowVersion,
            ),
        )

    override suspend fun completeCountsShiftingEvent(
        shiftingEventId: String,
        idempotencyKey: String,
        destinationTag: String?,
        proofRef: String,
        feedPackingProofRef: String?,
        feedGivenProofRef: String?,
        feedConfigFingerprint: String?,
    ): CountsShiftingExecutionResponseDto =
        service.completeCountsShiftingEvent(
            shiftingEventId,
            idempotencyKey,
            CountsShiftingCompleteRequestDto(
                proofRef = proofRef,
                feedPackingProofRef = feedPackingProofRef,
                feedGivenProofRef = feedGivenProofRef,
                feedConfigFingerprint = feedConfigFingerprint,
                destinationTag = destinationTag,
            ),
        )

    override suspend fun cancelCountsShiftingEvent(
        shiftingEventId: String,
        idempotencyKey: String,
        reason: String,
    ): CountsShiftingExecutionResponseDto =
        service.cancelCountsShiftingEvent(
            shiftingEventId,
            idempotencyKey,
            CountsShiftingCancelRequestDto(reason = reason),
        )

    override suspend fun listCountsPenReconciliationCards(
        status: String?,
        pageSize: Int?,
        cursor: String?,
    ): CountsPenReconciliationListResponseDto =
        service.listCountsPenReconciliationCards(status, pageSize, cursor)

    override suspend fun completeCountsPenReconciliationCard(
        cardId: String,
        idempotencyKey: String,
        proofRef: String,
    ): CountsPenReconciliationCompleteResponseDto =
        service.completeCountsPenReconciliationCard(
            cardId,
            idempotencyKey,
            CountsPenReconciliationCompleteRequestDto(proofRef = proofRef),
        )

    override suspend fun getFeedDirectionPreview(
        parkId: String,
        targetDate: String,
        shedId: String?,
        partitionLabel: String?,
        session: Int?,
        workflow: String?,
        status: String?,
        limit: Int?,
        offset: Int?,
    ): FeedDirectionPreviewPageDto =
        service.getFeedDirectionPreview(parkId, targetDate, shedId, partitionLabel, session, workflow, status, limit, offset)

    override suspend fun getFeedPackingWorklist(
        parkId: String,
        targetDate: String,
        shedId: String?,
        partitionLabel: String?,
        session: Int?,
        workflow: String?,
        status: String?,
        limit: Int?,
        offset: Int?,
    ): FeedPackingWorklistPageDto =
        service.getFeedPackingWorklist(parkId, targetDate, shedId, partitionLabel, session, workflow, status, limit, offset)


	    override suspend fun getPcCareWorklist(
	        category: String,
	        date: String,
	        limit: Int?,
	        cursor: String?,
	    ): PcCareTaskPageDto = service.getPcCareWorklist(category, date, limit, cursor)

    override suspend fun getPcCareTasks(
        date: String,
	        parkId: String?,
	        category: String?,
	        limit: Int?,
	        cursor: String?,
	    ): PcCareTaskPageDto = service.getPcCareTasks(date, parkId, category, limit, cursor)

    override suspend fun getPcCareTask(taskId: String): PcCareTaskDto = service.getPcCareTask(taskId)

    override suspend fun getPcCareTaskCaptures(
        taskId: String,
        cursor: String?,
        limit: Int?,
    ): PcCareCapturesDto = service.getPcCareTaskCaptures(taskId, cursor, limit)

    override suspend fun getPcCareTaskRoster(
        taskId: String,
        cursor: String?,
        limit: Int?,
    ): PcCareTaskRosterDto = service.getPcCareTaskRoster(taskId, cursor, limit)

    override suspend fun scanPcCareAnimal(
        taskId: String,
        idempotencyKey: String,
        request: PcCareScanRequestDto,
    ): PcCareScanResponseDto = service.scanPcCareAnimal(taskId, idempotencyKey, request)

    override suspend fun registerPcCareSlotProof(
        taskId: String,
        animalRowId: String,
        slot: String,
        idempotencyKey: String,
        request: PcCareSlotProofRequestDto,
    ) = service.registerPcCareSlotProof(taskId, animalRowId, slot, idempotencyKey, request)

    override suspend fun registerPcCareTaskProof(
        taskId: String,
        slot: String,
        idempotencyKey: String,
        request: PcCareSlotProofRequestDto,
    ) = service.registerPcCareTaskProof(taskId, slot, idempotencyKey, request)

    override suspend fun submitPcCareTask(
        taskId: String,
        idempotencyKey: String,
    ): PcCareSubmitResponseDto = service.submitPcCareTask(taskId, idempotencyKey)

    override suspend fun getPcCarePlannerCatalog(): PcCarePlannerCatalogDto =
        service.getPcCarePlannerCatalog()

    override suspend fun getPcCarePlannerParkSheds(
        parkId: String,
        category: String,
        date: String,
        cursor: String?,
        limit: Int?,
    ): PcCarePlannerShedsDto = service.getPcCarePlannerParkSheds(parkId, category, date, cursor, limit)

    override suspend fun createPcCareTask(
        idempotencyKey: String,
        request: PcCareCreateTaskRequestDto,
    ): PcCareTaskDto = service.createPcCareTask(idempotencyKey, request)

    override suspend fun createPcCareRound(
        idempotencyKey: String,
        request: PcCareCreateRoundRequestDto,
    ): PcCareRoundDto = service.createPcCareRound(idempotencyKey, request)

    override suspend fun getPcCareRoundCards(
        date: String?,
        category: String?,
        parkId: String?,
        filter: String?,
        cursor: String?,
        limit: Int?,
    ): PcCareRoundCardPageDto = service.getPcCareRoundCards(date, category, parkId, filter, cursor, limit)

    override suspend fun getPcCareRound(roundId: String): PcCareRoundDto = service.getPcCareRound(roundId)

    override suspend fun getPcCareRemovalPens(taskId: String): PcCareRemovalPenListDto =
        service.getPcCareRemovalPens(taskId)

    override suspend fun putPcCareRemovalPenProof(
        taskId: String,
        slot: String,
        idempotencyKey: String,
        request: PcCareRemovalPenProofRequestDto,
    ) = service.putPcCareRemovalPenProof(taskId, slot, idempotencyKey, request)

    override suspend fun closePcCareTask(taskId: String, request: PcCareCloseRequestDto) =
        service.closePcCareTask(taskId, request)

    override suspend fun reopenPcCareTask(taskId: String) = service.reopenPcCareTask(taskId)

    override suspend fun closePcCareRound(roundId: String, request: PcCareCloseRequestDto) =
        service.closePcCareRound(roundId, request)

    override suspend fun recordPcCareStockVerdict(
        taskId: String,
        request: PcCareStockVerdictRequestDto,
    ): PcCareTaskDto = service.recordPcCareStockVerdict(taskId, request)

    override suspend fun getToxinTasks(
        filter: String?,
        limit: Int?,
        cursor: String?,
    ): ToxinTaskPageDto = service.getToxinTasks(filter, limit, cursor)

    override suspend fun getToxinTask(taskId: String): ToxinTaskDetailDto = service.getToxinTask(taskId)

    override suspend fun completeToxinStep(
        taskId: String,
        stepNo: Int,
        idempotencyKey: String,
        request: ToxinStepCompleteRequestDto,
    ): ToxinTaskDetailDto = service.completeToxinStep(taskId, stepNo, idempotencyKey, request)

    override suspend fun submitToxinReading(
        taskId: String,
        idempotencyKey: String,
        request: ToxinSubmitRequestDto,
    ): ToxinTaskDetailDto = service.submitToxinReading(taskId, idempotencyKey, request)

    override suspend fun getProcurementVendors(
        search: String?,
        status: String?,
        limit: Int?,
        offset: Int?,
    ): VendorPageDto = service.getProcurementVendors(search, status, limit, offset)

    override suspend fun getProcurementVendor(vendorId: String): VendorDto = service.getProcurementVendor(vendorId)

    override suspend fun getProcurementVendorCatalog(): VendorCatalogDto = service.getProcurementVendorCatalog()

    override suspend fun createProcurementVendor(request: VendorWriteDto): VendorDto =
        service.createProcurementVendor(request)

    override suspend fun getFeedPurchases(
        farm: String?,
        delivery: String?,
        limit: Int?,
        offset: Int?,
    ): FeedPurchasePageDto = service.getFeedPurchases(farm, delivery, limit, offset)

    override suspend fun getFeedPurchaseOptions(): FeedPurchaseOptionsDto = service.getFeedPurchaseOptions()

    override suspend fun createFeedPurchase(idempotencyKey: String, request: FeedPurchaseWriteDto): FeedPurchaseDto =
        service.createFeedPurchase(idempotencyKey, request)

    override suspend fun getSalesDeals(farm: String?, limit: Int?, offset: Int?): SalesDealPageDto =
        service.getSalesDeals(farm, limit, offset)

    override suspend fun getSalesOptions(): SalesOptionsDto = service.getSalesOptions()

    override suspend fun getVendorOptions(): VendorOptionsDto = service.getVendorOptions()

    override suspend fun createSalesDeal(idempotencyKey: String, request: SalesDealWriteDto): SalesDealDto =
        service.createSalesDeal(idempotencyKey, request)

    override suspend fun createFeedPurchasePayment(purchaseId: String, idempotencyKey: String, request: FeedPurchasePaymentWriteDto): FeedPurchaseDto =
        service.createFeedPurchasePayment(purchaseId, idempotencyKey, request)

    override suspend fun setFeedPurchasePaymentStatus(purchaseId: String, request: FeedPurchaseStatusWriteDto): FeedPurchaseDto =
        service.setFeedPurchasePaymentStatus(purchaseId, request)

    override suspend fun editFeedPurchase(purchaseId: String, request: FeedPurchaseEditDto): FeedPurchaseDto =
        service.editFeedPurchase(purchaseId, request)

    override suspend fun recordFeedPurchaseDelivery(purchaseId: String, request: FeedPurchaseDeliveryWriteDto): FeedPurchaseDto =
        service.recordFeedPurchaseDelivery(purchaseId, request)


    override suspend fun getSaleLocations(): SaleLocationsDto = service.getSaleLocations()

    override suspend fun getSaleCandidates(parkId: String, shedId: String?, partitionLabels: List<String>, query: String?, limit: Int?, cursor: String?): SaleCandidatePageDto =
        service.getSaleCandidates(parkId, shedId, partitionLabels, query, limit, cursor)

    override suspend fun getSaleAllocation(salesDealId: String): SaleAllocationDto = service.getSaleAllocation(salesDealId)

    override suspend fun previewSaleAllocation(request: SaleAllocationRequestDto): SalePreviewDto = service.previewSaleAllocation(request)

    override suspend fun confirmSaleAllocation(idempotencyKey: String, request: SaleAllocationRequestDto): SaleAllocationDto =
        service.confirmSaleAllocation(idempotencyKey, request)

    override suspend fun createSalesDealPayment(dealId: String, idempotencyKey: String, request: SalesDealPaymentWriteDto): SalesDealDto =
        service.createSalesDealPayment(dealId, idempotencyKey, request)

    override suspend fun updateSalesDealPayment(dealId: String, paymentId: String, idempotencyKey: String, request: SalesDealPaymentWriteDto): SalesDealDto =
        service.updateSalesDealPayment(dealId, paymentId, idempotencyKey, request)

    override suspend fun deleteSalesDealPayment(dealId: String, paymentId: String, idempotencyKey: String): SalesDealDto =
        service.deleteSalesDealPayment(dealId, paymentId, idempotencyKey)

    override suspend fun setSalesDealStatus(dealId: String, idempotencyKey: String, request: SalesDealStatusWriteDto): SalesDealDto =
        service.setSalesDealStatus(dealId, idempotencyKey, request)

    override suspend fun getSalesBuyerLeads(limit: Int?, offset: Int?): SalesBuyerLeadPageDto =
        service.getSalesBuyerLeads(limit, offset)

    override suspend fun createSalesBuyerLead(idempotencyKey: String, request: SalesBuyerLeadWriteDto): SalesBuyerLeadDto =
        service.createSalesBuyerLead(idempotencyKey, request)

    override suspend fun setSalesBuyerLeadStatus(leadId: String, idempotencyKey: String, request: SalesLeadStatusWriteDto): SalesBuyerLeadDto =
        service.setSalesBuyerLeadStatus(leadId, idempotencyKey, request)

    override suspend fun getSalesFpoLeads(limit: Int?, offset: Int?): SalesFpoLeadPageDto =
        service.getSalesFpoLeads(limit, offset)

    override suspend fun createSalesFpoLead(idempotencyKey: String, request: SalesFpoLeadWriteDto): SalesFpoLeadDto =
        service.createSalesFpoLead(idempotencyKey, request)

    override suspend fun setSalesFpoLeadStatus(leadId: String, idempotencyKey: String, request: SalesLeadStatusWriteDto): SalesFpoLeadDto =
        service.setSalesFpoLeadStatus(leadId, idempotencyKey, request)

    override suspend fun createSalesMarketBenchmark(idempotencyKey: String, request: SalesBenchmarkWriteDto): SalesRecordedDto =
        service.createSalesMarketBenchmark(idempotencyKey, request)

    override suspend fun createSalesSoldTags(idempotencyKey: String, request: SalesSoldTagsWriteDto): SalesSoldTagsResultDto =
        service.createSalesSoldTags(idempotencyKey, request)

    override suspend fun createSalesWeightCheck(idempotencyKey: String, request: SalesWeightCheckWriteDto): SalesRecordedDto =
        service.createSalesWeightCheck(idempotencyKey, request)
    override suspend fun getLeadershipTasks(
        filter: String?,
        limit: Int?,
        cursor: String?,
    ): LeadershipTaskPageDto = service.getLeadershipTasks(filter, limit, cursor)

    override suspend fun getLeadershipTaskAssignees(): LeadershipAssigneeListDto =
        service.getLeadershipTaskAssignees()

    override suspend fun getLeadershipTask(taskId: String): LeadershipTaskDetailDto =
        service.getLeadershipTask(taskId)

    override suspend fun raiseLeadershipTask(
        idempotencyKey: String,
        request: LeadershipTaskRaiseRequestDto,
    ): LeadershipTaskDetailDto = service.raiseLeadershipTask(idempotencyKey, request)

    override suspend fun editLeadershipTask(
        taskId: String,
        idempotencyKey: String,
        request: LeadershipTaskEditRequestDto,
    ): LeadershipTaskDetailDto = service.editLeadershipTask(taskId, idempotencyKey, request)

    override suspend fun changeLeadershipTaskStatus(
        taskId: String,
        idempotencyKey: String,
        request: LeadershipTaskStatusRequestDto,
    ): LeadershipTaskDetailDto = service.changeLeadershipTaskStatus(taskId, idempotencyKey, request)

    override suspend fun markLeadershipTaskSeen(taskId: String): LeadershipTaskDetailDto =
        service.markLeadershipTaskSeen(taskId)

    override suspend fun setLeadershipTaskComment(
        taskId: String,
        idempotencyKey: String,
        request: LeadershipTaskCommentRequestDto,
    ): LeadershipTaskDetailDto = service.setLeadershipTaskComment(taskId, idempotencyKey, request)

    override suspend fun getLeadershipTaskAttachmentDownloadUrl(taskId: String, proofId: String): String {
        val url = service.getLeadershipTaskAttachmentDownloadUrl(taskId, proofId).downloadUrl
        return if (url.startsWith("/")) baseUrl.trimEnd('/') + url else url
    }

    override suspend fun getFeedDistributionCaptures(
        parkId: String?,
        shedId: String,
        partitionLabel: String?,
        sessionNo: Int,
        targetDate: String,
        workflow: String,
    ): FeedDistributionCapturesDto =
        service.getFeedDistributionCaptures(parkId, shedId, partitionLabel, sessionNo, targetDate, workflow)

    override suspend fun completeFeedDirectionSession(
        idempotencyKey: String,
        request: FeedDirectionCompleteRequestDto,
    ): FeedDirectionCompleteResponseDto = service.completeFeedDirectionSession(idempotencyKey, request)

    override suspend fun completeFeedDistribution(
        idempotencyKey: String,
        request: FeedDistributionCompleteRequestDto,
    ): FeedDistributionCompleteResponseDto = service.completeFeedDistribution(idempotencyKey, request)

    override suspend fun completeFeedPacking(
        idempotencyKey: String,
        request: FeedPackingCompleteRequestDto,
    ): FeedPackingCompleteResponseDto = service.completeFeedPacking(idempotencyKey, request)

    override suspend fun getFeedWastageWorklist(
        parkId: String,
        targetDate: String,
        shedId: String?,
        partitionLabel: String?,
        status: String?,
        limit: Int?,
        offset: Int?,
    ): FeedWastageWorklistPageDto =
        service.getFeedWastageWorklist(parkId, targetDate, shedId, partitionLabel, status, limit, offset)

    override suspend fun completeFeedWastage(
        idempotencyKey: String,
        request: FeedWastageCompleteRequestDto,
    ): FeedWastageCompleteResponseDto = service.completeFeedWastage(idempotencyKey, request)

    override suspend fun recordFeedWastageMeasurement(
        completionId: String,
        idempotencyKey: String,
        request: FeedWastageMeasurementRequestDto,
    ): FeedWastageMeasurementResponseDto =
        service.recordFeedWastageMeasurement(completionId, idempotencyKey, request)

    override suspend fun submitMilkPreparation(
        idempotencyKey: String,
        request: MilkPreparationSubmissionRequestDto,
    ): MilkPreparationSubmissionResponseDto = service.submitMilkPreparation(idempotencyKey, request)

    override suspend fun getMilkFeedingTasks(feedingDate: String, parkId: String?, sessionNo: Int?, limit: Int, offset: Int): MilkFeedingPageDto = service.getMilkFeedingTasks(feedingDate, parkId, sessionNo, limit, offset)
    override suspend fun submitMilkFeedingTask(taskId: String, idempotencyKey: String, request: MilkFeedingSubmitRequestDto): MilkFeedingSubmitResponseDto = service.submitMilkFeedingTask(taskId, idempotencyKey, request)

    override suspend fun getMilkPreparation(preparationDate: String?, parkId: String?, limit: Int, offset: Int): MilkPreparationPageDto =
        service.getMilkPreparation(preparationDate, parkId, limit, offset)

    override suspend fun getFeedTransportTasks(
        businessDate: String,
        parkId: String?,
        shedId: String?,
        status: String?,
        cursor: String?,
        limit: Int?,
    ): FeedTransportTaskPageDto = service.getFeedTransportTasks(businessDate, parkId, shedId, status, cursor, limit)
    override suspend fun submitFeedTransport(taskId: String, idempotencyKey: String, request: FeedTransportSubmitRequestDto): FeedTransportSubmitResponseDto = service.submitFeedTransport(taskId, idempotencyKey, request)

    override suspend fun recordCountsShiftingEvent(
        idempotencyKey: String,
        request: CountsShiftingEventRequestDto,
    ): CountsShiftingEventResponseDto = service.recordCountsShiftingEvent(idempotencyKey, request)

    override suspend fun recordCountsBirthEvent(
        idempotencyKey: String,
        request: CountsBirthEventRequestDto,
    ): CountsApprovalSubmitResponseDto = service.recordCountsBirthEvent(idempotencyKey, request)

    override suspend fun recordCountsDeathEvent(
        idempotencyKey: String,
        request: CountsDeathEventRequestDto,
    ): CountsApprovalSubmitResponseDto = service.recordCountsDeathEvent(idempotencyKey, request)

    override suspend fun listWorkflows(
        module: String,
        date: String?,
        filter: String?,
        pageSize: Int?,
        cursor: String?,
    ): WorkflowListResponseDto = service.listWorkflows(module, date, filter, pageSize, cursor)

    override suspend fun getWorkflow(workflowId: String, lens: String?, date: String?): WorkflowDetailResponseDto =
        service.getWorkflow(workflowId, lens, date)

    override suspend fun answerWorkflowAction(
        workflowId: String,
        actionId: String,
        idempotencyKey: String,
        request: WorkflowActionAnswerRequestDto,
    ): WorkflowActionWriteResponseDto =
        service.answerWorkflowAction(workflowId, actionId, idempotencyKey, request)

    override suspend fun completeWorkflowAction(
        workflowId: String,
        actionId: String,
        idempotencyKey: String,
        request: WorkflowActionCompleteRequestDto,
    ): WorkflowActionWriteResponseDto =
        service.completeWorkflowAction(workflowId, actionId, idempotencyKey, request)

    override suspend fun listHealthWorkItems(
        ageBand: String,
        date: String,
        status: String?,
        diseaseKey: String?,
        parkId: String?,
        shedId: String?,
        session: String?,
        cursor: String?,
        limit: Int?,
    ): HealthWorkItemPageDto = service.listHealthWorkItems(
        ageBand, date, status, diseaseKey, parkId, shedId, session, cursor, limit,
    )

    override suspend fun openHealthCase(
        idempotencyKey: String,
        request: HealthOpenCaseRequestDto,
    ): HealthOpenCaseResponseDto = service.openHealthCase(idempotencyKey, request)

    override suspend fun getHealthWorkItem(healthSessionId: String): HealthWorkItemDetailDto =
        service.getHealthWorkItem(healthSessionId)

    override suspend fun submitHealthObservation(
        idempotencyKey: String,
        request: SubmitHealthObservationRequestDto,
    ): HealthDiagnosisProposalResponseDto = service.submitHealthObservation(idempotencyKey, request)

    override suspend fun listHealthObservations(
        status: String?,
        goatId: String?,
        cursor: String?,
        limit: Int?,
    ): HealthDiagnosisQueuePageDto = service.listHealthObservations(status, goatId, cursor, limit)

    override suspend fun getHealthObservation(diagnosisRunId: String): HealthDiagnosisRunDto =
        service.getHealthObservation(diagnosisRunId)

    override suspend fun confirmHealthDiagnosis(
        diagnosisRunId: String,
        idempotencyKey: String,
        request: ConfirmHealthDiagnosisRequestDto,
    ): ConfirmHealthDiagnosisResponseDto =
        service.confirmHealthDiagnosis(diagnosisRunId, idempotencyKey, request)

    override suspend fun completeHealthWorkItem(
        healthSessionId: String,
        idempotencyKey: String,
        request: HealthCompleteRequestDto,
    ): HealthCompleteResponseDto =
        service.completeHealthWorkItem(healthSessionId, idempotencyKey, request)

    override suspend fun closeHealthCase(
        healthCaseId: String,
        idempotencyKey: String,
        request: HealthCloseCaseRequestDto,
    ): HealthCloseCaseResponseDto =
        service.closeHealthCase(healthCaseId, idempotencyKey, request)

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

    override suspend fun recordClockIn(
        idempotencyKey: String,
        request: ClockPunchRequestDto,
    ): ClockPunchResponseDto = service.recordClockIn(idempotencyKey, request)

    override suspend fun recordClockOut(
        idempotencyKey: String,
        request: ClockPunchRequestDto,
    ): ClockPunchResponseDto = service.recordClockOut(idempotencyKey, request)

    override suspend fun getClockStatus(): ClockStatusResponseDto = service.getClockStatus()

    override suspend fun listClockPresence(
        date: String?,
        parkId: String?,
        designation: String?,
        bucket: String?,
        q: String?,
        limit: Int?,
        cursor: String?,
    ): ClockPresenceResponseDto =
        service.listClockPresence(date, parkId, designation, bucket, q, limit, cursor)

    override suspend fun getClockPresencePerson(
        workforceMemberId: String,
        date: String?,
    ): ClockPersonDayResponseDto = service.getClockPresencePerson(workforceMemberId, date)
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
    private val requestMetadataProvider: () -> RequestMetadata = { RequestMetadata() },
) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val token = tokenProvider()
        val tenantId = tenantIdProvider()
        val localeTag = normalizedLocaleTag(localeProvider())
        val requestMetadata = requestMetadataProvider()
        val builder = chain.request().newBuilder()
        if (!token.isNullOrBlank()) {
            builder.header("Authorization", "Bearer $token")
        }
        if (!tenantId.isNullOrBlank()) {
            builder.header(TENANT_CONTEXT_HEADER, tenantId)
        }
        builder.header(ACCEPT_LANGUAGE_HEADER, acceptLanguageValue(localeTag))
        builder.header(LOCALE_CONTEXT_HEADER, localeTag)
        requestMetadata.headers().forEach { (name, value) -> builder.header(name, value) }
        return chain.proceed(builder.build())
    }
}

data class RequestMetadata(
    val appVersion: String = "",
    val appVersionCode: String = "",
    val buildType: String = "",
    val deviceId: String = "",
    val platform: String = "",
    val osVersion: String = "",
    val sdkVersion: String = "",
    val deviceModel: String = "",
) {
    fun headers(): List<Pair<String, String>> = listOfNotNull(
        header("X-GoatOS-App-Version", appVersion),
        header("X-GoatOS-App-Version-Code", appVersionCode),
        header("X-GoatOS-Build-Type", buildType),
        header("X-GoatOS-Device-Id", deviceId),
        header("X-Device-Id", deviceId),
        header("X-GoatOS-Platform", platform),
        header("X-GoatOS-OS-Version", osVersion),
        header("X-GoatOS-SDK-Version", sdkVersion),
        header("X-GoatOS-Device-Model", deviceModel),
    )

    private fun header(name: String, rawValue: String): Pair<String, String>? {
        val value = rawValue.trim().filterNot { it.code < 0x20 || it.code == 0x7f }.take(128)
        return value.takeIf { it.isNotBlank() }?.let { name to it }
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
        requestMetadataProvider: () -> RequestMetadata = { RequestMetadata() },
        // Optional: traceparent stamping + method/route/status/duration reporting
        // (docs/observability/OBSERVABILITY_DESIGN.md §2.5). Null keeps the client identical to
        // before this was wired — every existing caller is unaffected until it opts in.
        telemetryInterceptor: okhttp3.Interceptor? = null,
    ): OkHttpClient =
        OkHttpClient.Builder()
            .addInterceptor(BearerAuthInterceptor(tokenProvider, tenantIdProvider, localeProvider, requestMetadataProvider))
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
        requestMetadataProvider: () -> RequestMetadata = { RequestMetadata() },
        telemetryInterceptor: okhttp3.Interceptor? = null,
    ): AppApi =
        RetrofitAppApi(
            retrofit(baseUrl, okHttp(tokenProvider, tenantIdProvider, localeProvider, requestMetadataProvider, telemetryInterceptor)).create(),
            proofBlobUploader(baseUrl, tokenProvider),
            baseUrl,
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
