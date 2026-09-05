package sg.mesha.goatos.core.network

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import sg.mesha.goatos.core.model.nav.NavChrome
import sg.mesha.goatos.core.model.nav.NavItem
import sg.mesha.goatos.core.model.nav.NavModule
import sg.mesha.goatos.core.model.nav.NavModuleStatus
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.dto.CalendarEventListResponseDto
import sg.mesha.goatos.core.network.dto.ClockPersonDayResponseDto
import sg.mesha.goatos.core.network.dto.ClockPresenceResponseDto
import sg.mesha.goatos.core.network.dto.ClockPunchRequestDto
import sg.mesha.goatos.core.network.dto.ClockPunchResponseDto
import sg.mesha.goatos.core.network.dto.ClockStatusResponseDto
import sg.mesha.goatos.core.network.dto.ClockEntryDto
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
import sg.mesha.goatos.core.network.dto.ToxinStepDto
import sg.mesha.goatos.core.network.dto.ToxinSubmitRequestDto
import sg.mesha.goatos.core.network.dto.ToxinTaskDetailDto
import sg.mesha.goatos.core.network.dto.ToxinTaskPageDto
import sg.mesha.goatos.core.network.dto.VendorCatalogDto
import sg.mesha.goatos.core.network.dto.VendorCatalogEntryDto
import sg.mesha.goatos.core.network.dto.VendorDto
import sg.mesha.goatos.core.network.dto.VendorPageDto
import sg.mesha.goatos.core.network.dto.VendorWriteDto
import sg.mesha.goatos.core.network.dto.FeedItemOptionDto
import sg.mesha.goatos.core.network.dto.DeliveryStatusOptionDto
import sg.mesha.goatos.core.network.dto.FeedPurchaseDeliveryWriteDto
import sg.mesha.goatos.core.network.dto.FeedPurchaseDto
import sg.mesha.goatos.core.network.dto.FeedPurchaseEditDto
import sg.mesha.goatos.core.network.dto.FeedPurchasePaymentDto
import sg.mesha.goatos.core.network.dto.FeedPurchasePaymentWriteDto
import sg.mesha.goatos.core.network.dto.FeedPurchaseStatusWriteDto
import sg.mesha.goatos.core.network.dto.FeedPurchaseOptionsDto
import sg.mesha.goatos.core.network.dto.FeedPurchasePageDto
import sg.mesha.goatos.core.network.dto.FeedPurchaseWriteDto
import sg.mesha.goatos.core.network.dto.SaleShedGroupDto
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
import sg.mesha.goatos.core.network.dto.SalesDealPaymentDto
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
import sg.mesha.goatos.core.network.dto.SalesStatusOptionDto
import sg.mesha.goatos.core.network.dto.SaleLocationParkDto
import sg.mesha.goatos.core.network.dto.SaleLocationEntryDto
import sg.mesha.goatos.core.network.dto.SaleCandidateDto
import sg.mesha.goatos.core.network.dto.VendorOptionDto
import sg.mesha.goatos.core.network.dto.VendorOptionsDto
import sg.mesha.goatos.core.network.dto.FeedDirectionPreviewPageDto
import sg.mesha.goatos.core.network.dto.FeedDistributionCapturesDto
import sg.mesha.goatos.core.network.dto.FeedPackingWorklistPageDto
import sg.mesha.goatos.core.network.dto.FeedTransportTaskPageDto
import sg.mesha.goatos.core.network.dto.FeedTransportSubmitRequestDto
import sg.mesha.goatos.core.network.dto.FeedTransportSubmitResponseDto
import sg.mesha.goatos.core.network.dto.CountsApprovalDecisionRequestDto
import sg.mesha.goatos.core.network.dto.CountsApprovalDecisionResponseDto
import sg.mesha.goatos.core.network.dto.CountsApprovalListResponseDto
import sg.mesha.goatos.core.network.dto.CountsBirthEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsBreakdownResponseDto
import sg.mesha.goatos.core.network.dto.CountsBreedsResponseDto
import sg.mesha.goatos.core.network.dto.CountsDeathEventRequestDto
import sg.mesha.goatos.core.network.dto.DeathCauseCatalogDto
import sg.mesha.goatos.core.network.dto.CountsApprovalSubmitResponseDto
import sg.mesha.goatos.core.network.dto.CountsPenReconciliationCompleteResponseDto
import sg.mesha.goatos.core.network.dto.CountsPenReconciliationListResponseDto
import sg.mesha.goatos.core.network.dto.CountsShiftingCancelRequestDto
import sg.mesha.goatos.core.network.dto.CountsPromoteIdentifierResponseDto
import sg.mesha.goatos.core.network.dto.CountsShiftingCompleteRequestDto
import sg.mesha.goatos.core.network.dto.CountsShiftingDestinationsResponseDto
import sg.mesha.goatos.core.network.dto.CountsShiftingEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsShiftingEventResponseDto
import sg.mesha.goatos.core.network.dto.CountsShiftingExecutionResponseDto
import sg.mesha.goatos.core.network.dto.CountsShiftingPendingExecutionResponseDto
import sg.mesha.goatos.core.network.dto.GoatSearchResponseDto
import sg.mesha.goatos.core.network.dto.HerdRegisterSummaryResponseDto
import sg.mesha.goatos.core.network.dto.EnrichedPositionListResponseDto
import sg.mesha.goatos.core.network.dto.MyCoverageResponseDto
import sg.mesha.goatos.core.network.dto.ProofArtifactDto
import sg.mesha.goatos.core.network.dto.ProofCompleteResponseDto
import sg.mesha.goatos.core.network.dto.ProofDownloadUrlResponseDto
import sg.mesha.goatos.core.network.dto.ProofReferenceDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.ProofUploadResponseDto
import sg.mesha.goatos.core.network.dto.UploadedProofListResponseDto
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
import sg.mesha.goatos.core.network.dto.WeighingFastingShedCardDto
import sg.mesha.goatos.core.network.dto.WeighingFastingShedCardListResponseDto
import sg.mesha.goatos.core.network.dto.WeighingFastingShedCardResponseDto
import sg.mesha.goatos.core.network.dto.SubmitWeighingFastingShedRequestDto
import sg.mesha.goatos.core.network.dto.WeighingLeadershipShedVideosResponseDto
import sg.mesha.goatos.core.network.dto.WeighingShedObservationRequestDto
import sg.mesha.goatos.core.network.dto.WeighingScopeReopenRequestDto
import sg.mesha.goatos.core.network.dto.WeighingScopeCloseRequestDto
import sg.mesha.goatos.core.network.dto.WeighingScopeSubmitRequestDto

/**
 * One phone-screen page of weighing roster rows. A viewport holds ~7-10 rows, so the
 * network page and the observed Room window are both this size and grow only by
 * viewport-triggered continuation (see docs/decisions/mobile-data-fetch-anti-patterns.md).
 */
const val WEIGHING_PAGE_SIZE = 20

/**
 * One phone-screen page of weighing ALERTS. Same ~20-rows-per-screen budget as every other
 * mobile list; the backend clamps anything larger, so this is the client's half of one contract
 * rather than an independent guess.
 */
const val WEIGHING_ALERTS_PAGE_SIZE = 20

/** One phone-viewport page of vaccination alerts. Matches the backend's AlertPageSize. */
const val VACCINATION_ALERTS_PAGE_SIZE = 20

/**
 * The three weighing surfaces. Each is a separate destination with its own authority, so the
 * client names the surface it is rendering instead of the server inferring it from the viewer's
 * roles. Values match the backend `scope` query parameter.
 */
const val WEIGHING_SCOPE_MINE = "mine"

/** The planner's flat all-tasks list across parks. Read-only; requires weighing.plan. */
const val WEIGHING_SCOPE_ALL = "all"

/** Read-only oversight of other people's work. Requires weighing.oversee_operators. */
const val WEIGHING_SCOPE_OPERATORS = "operators"

/** Hard ceiling for a caller-requested Room window (e.g., observeScope). */
const val MAX_OBSERVED_WINDOW = WEIGHING_PAGE_SIZE * 2  // 40

/** Hard ceiling for background scope hydration (e.g., refreshScope). */
const val MAX_SCOPE_HYDRATION_ROWS = WEIGHING_PAGE_SIZE * 10  // 200

/** Canonical task-page boundary shared by Retrofit, Room PagingSource and RemoteMediator. */
const val APP_TASK_PAGE_SIZE = 20

// Wire DTOs for the nav slice of GET /app/bootstrap. The response (BootstrapResponse)
// carries many more fields; with ignoreUnknownKeys the client only binds the ones it
// renders. Defaults keep deserialization lenient. These hand-mapped DTOs are replaced
// 1:1 by the OpenAPI-generated client next.
@Serializable
data class BootstrapDto(
    @SerialName("nav_chrome") val navChrome: String = "minimal",
    @SerialName("visible_navigation") val visibleNavigation: List<NavItemDto> = emptyList(),
    // Drawer modules, each carrying its OWN bottom bar (BootstrapModule). Selecting a module
    // in the drawer swaps the bar to its nav_items — no second network call. Defaulted to
    // empty so a cached pre-`modules` bootstrap still decodes and falls back to
    // visible_navigation. See docs/decisions/role-module-nav-composition.md.
    @SerialName("modules") val modules: List<BootstrapModuleDto> = emptyList(),
    // Extended bootstrap slice used by the shell beyond nav: identity, feature gates,
    // and version/time. All optional with defaults so the cached-bootstrap decode and
    // the original nav-only contract still succeed.
    @SerialName("actor") val actor: BootstrapActorDto? = null,
    @SerialName("operator_profile") val operatorProfile: BootstrapOperatorProfileDto? = null,
    @SerialName("device_state") val deviceState: BootstrapDeviceStateDto = BootstrapDeviceStateDto(),
    @SerialName("feature_flags") val featureFlags: Map<String, Boolean> = emptyMap(),
    @SerialName("app_min_supported_version") val appMinSupportedVersion: String = "",
    @SerialName("server_time") val serverTime: String = "",
    @SerialName("trace_id") val traceId: String = "",
)

/**
 * A drawer module (BootstrapModule): identity plus the module-scoped bottom bar it owns.
 * [status] is `available` (built, tappable) or `soon` (declared roadmap, disabled row).
 * [label] is already localized by the backend — render it verbatim.
 */
@Serializable
data class BootstrapModuleDto(
    @SerialName("key") val key: String = "",
    @SerialName("label") val label: String = "",
    @SerialName("href") val href: String = "",
    @SerialName("status") val status: String = "soon",
    @SerialName("nav_items") val navItems: List<NavItemDto> = emptyList(),
    /**
     * A numeric attention count the backend composed for this module (0 when nothing): for
     * `leadership_tasks` it is the caller's unseen assigned-task count. Rendered as a badge on the
     * drawer row and on the module's bottom-bar item when > 0; the client never derives it.
     */
    @SerialName("badge_count") val badgeCount: Int = 0,
)

/**
 * Android device registration state the backend returns on bootstrap
 * (BootstrapDeviceState). When [required] is true and no [device] is registered
 * (status typically `not_registered`), the client registers the device.
 */
@Serializable
data class BootstrapDeviceStateDto(
    @SerialName("required") val required: Boolean = false,
    @SerialName("device") val device: DeviceSummaryDto? = null,
    @SerialName("status") val status: String = "",
    @SerialName("reason") val reason: String? = null,
)

/** Device record (DeviceSummary). Only the mobile-consumed fields are modeled. */
@Serializable
data class DeviceSummaryDto(
    @SerialName("device_id") val deviceId: String = "",
    @SerialName("operator_id") val operatorId: String = "",
    @SerialName("app_install_id") val appInstallId: String = "",
    @SerialName("status") val status: String = "",
)

/** Request body for POST /app/devices/register (RegisterDeviceRequest).
 *  [fcmToken]: the RAW FCM registration token for this install — see
 *  [sg.mesha.goatos.core.data.push.DefaultNotificationsPort]'s KDoc for the wire-field history.
 *  [pushTokenHash] is kept for backward compat with the identity/dedup hash the backend keys
 *  device-binding uniqueness on; mobile does not compute a hash of the token, so it is always
 *  left `null` here — device identity/dedup is keyed on [appInstallId], not this field. */
@Serializable
data class RegisterDeviceRequestDto(
    @SerialName("app_install_id") val appInstallId: String,
    @SerialName("app_version") val appVersion: String,
    @SerialName("os_version") val osVersion: String = "",
    @SerialName("push_token_hash") val pushTokenHash: String? = null,
    @SerialName("fcm_token") val fcmToken: String? = null,
    /** Whether this phone will actually SHOW what we send it
     *  (`NotificationManagerCompat.areNotificationsEnabled()`), so the backend can mark the device
     *  push-muted and stop counting a dropped push as delivered. `null` = not reported. */
    @SerialName("notifications_enabled") val notificationsEnabled: Boolean? = null,
)

/** Request body for POST /app/devices/{device_id}/heartbeat (HeartbeatDeviceRequest).
 *  [fcmToken] / [pushTokenHash]: same wire-field note as [RegisterDeviceRequestDto]. */
@Serializable
data class HeartbeatDeviceRequestDto(
    @SerialName("app_version") val appVersion: String = "",
    @SerialName("os_version") val osVersion: String = "",
    @SerialName("push_token_hash") val pushTokenHash: String? = null,
    @SerialName("fcm_token") val fcmToken: String? = null,
    /** Re-reported on every heartbeat: someone who switches notifications off (or back on) in
     *  system settings after registering is picked up on the next bootstrap. */
    @SerialName("notifications_enabled") val notificationsEnabled: Boolean? = null,
)

/** Response for register/heartbeat (DeviceResponse). */
@Serializable
data class DeviceResponseDto(
    @SerialName("device") val device: DeviceSummaryDto = DeviceSummaryDto(),
    @SerialName("trace_id") val traceId: String = "",
)

/** Request body for POST /auth/session-events.
 *
 * Firebase ID tokens do not carry Goat OS tenant/role grants. The backend uses
 * this audited session event to claim a pending email grant and attach the
 * Firebase principal to its Goat OS workforce actor before /app/bootstrap runs.
 */
@Serializable
data class AuthSessionEventRequestDto(
    @SerialName("event_type") val eventType: String,
    @SerialName("source") val source: String,
)

@Serializable
data class AppAnalyticsEventRequestDto(
    @SerialName("event_name") val eventName: String,
    @SerialName("properties") val properties: Map<String, String> = emptyMap(),
    @SerialName("client_event_time_ms") val clientEventTimeMs: Long,
    @SerialName("flavor") val flavor: String,
    @SerialName("app_version_name") val appVersionName: String,
    @SerialName("app_version_code") val appVersionCode: Int,
    /** Client-minted operation id — the backend's idempotency key (UNIQUE per tenant); a resend
     *  after a lost response must not double-count. Top-level, not a property, so the server can
     *  dedupe without parsing the properties map. */
    @SerialName("client_event_id") val clientEventId: String? = null,
)

@Serializable
data class AppAnalyticsEventResponseDto(
    @SerialName("accepted") val accepted: Boolean = true,
)

@Serializable
data class NavItemDto(
    val key: String = "",
    val label: String = "",
    val href: String = "",
)

/** Identity of the bootstrapped principal (BootstrapActor). */
@Serializable
data class BootstrapActorDto(
    @SerialName("actor_id") val actorId: String = "",
    @SerialName("tenant_id") val tenantId: String = "",
)

/** Operator profile slice the shell shows (OperatorProfile). Fields the UI needs only.
 *  [primaryLocationId] is the operator's HR center scope id — the `center_id` the
 *  Timetable screen passes to `GET /app/roster/timetable` (distinct from
 *  [primaryLocation], the display label). */
@Serializable
data class BootstrapOperatorProfileDto(
    @SerialName("operator_id") val operatorId: String = "",
    @SerialName("display_code") val displayCode: String = "",
    @SerialName("display_name") val displayName: String = "",
    @SerialName("status") val status: String = "",
    @SerialName("primary_role_hint") val primaryRoleHint: String = "",
    @SerialName("primary_location_id") val primaryLocationId: String? = null,
    @SerialName("primary_location") val primaryLocation: String? = null,
)

/**
 * App API port. The real Retrofit adapter lands behind this interface so callers stay
 * Retrofit-agnostic. One suspend method per consumed endpoint; optional query filters
 * default to null (omitted from the request). Mapping DTO -> feature UiState happens in
 * the :app layer, not here.
 */
interface AppApi {
    /** POST /auth/session-events — audited sign-in/session-refresh bridge for Firebase auth. */
    suspend fun recordAuthSessionEvent(request: AuthSessionEventRequestDto) = Unit

    /** POST /app/analytics/events — backend mirror for Firebase product analytics. */
    suspend fun recordAnalyticsEvent(request: AppAnalyticsEventRequestDto): AppAnalyticsEventResponseDto =
        AppAnalyticsEventResponseDto()

    /** GET /app/bootstrap — nav + identity + device state. [deviceId] identifies a
     *  previously-registered device so the backend can return its device_state. */
    suspend fun bootstrap(deviceId: String? = null): BootstrapDto

    /** POST /app/devices/register — register this Android install as a device. */
    suspend fun registerDevice(request: RegisterDeviceRequestDto): DeviceResponseDto

    /** POST /app/devices/{device_id}/heartbeat — refresh device liveness + app version. */
    suspend fun heartbeatDevice(deviceId: String, request: HeartbeatDeviceRequestDto): DeviceResponseDto

    /** POST /app/devices/{device_id}/deregister — logout clean-slate (C35-001): clears the
     *  stored FCM push-token binding and revokes THIS device (self-service, scoped to the
     *  caller's own registration). Idempotent for an already-revoked device the actor owns. */
    suspend fun deregisterDevice(deviceId: String): DeviceResponseDto

    /** GET /app/vaccination/execution — mobile execution rows grouped by park + shed. */
    suspend fun listVaccinationExecution(
        parkId: String? = null,
        workState: String? = null,
        asOf: String? = null,
        dueBefore: String? = null,
        openOnly: Boolean? = null,
        limit: Int? = null,
        cursor: String? = null,
        includeFilterOptions: Boolean = false,
        includeCardSummaries: Boolean = true,
    ): VaccinationExecutionResponseDto

    /** GET /app/vaccination/execution/sheds/{shed_id} — one mobile shed execution context. */
    suspend fun getVaccinationExecutionShed(
        shedId: String,
        asOf: String? = null,
        dueBefore: String? = null,
        limit: Int? = null,
        partitionLabel: String? = null,
    ): VaccinationExecutionShedDrilldownDto

    /** GET /calendar/vaccination/events — presentation + bounded events. */
    suspend fun listCalendarVaccinationEvents(
        parkId: String? = null,
        shedId: String? = null,
        ownerKey: String? = null,
        status: String? = null,
        dateFrom: String? = null,
        dateTo: String? = null,
        includeDateMarkers: Boolean = false,
        includeDriveSummary: Boolean = false,
        vaccine: String? = null,
        includeFilterOptions: Boolean = false,
        cursor: String? = null,
        limit: Int? = null,
    ): CalendarEventListResponseDto

    /** GET /control-tower/vaccination — summary + at-risk/broken alerts. */
    suspend fun getVaccinationControlTower(
        parkId: String? = null,
        shedId: String? = null,
        workState: String? = null,
        severity: String? = null,
        dueBefore: String? = null,
        asOf: String? = null,
        cursor: String? = null,
        limit: Int? = null,
    ): ControlTowerResponseDto

    /** GET /vaccination/adherence — expected-vs-actual protocol adherence rows. */
    suspend fun getVaccinationAdherence(
        parkId: String? = null,
        shedId: String? = null,
        workState: String? = null,
        severity: String? = null,
        dueBefore: String? = null,
        asOf: String? = null,
        cursor: String? = null,
        limit: Int? = null,
    ): ProtocolAdherenceResponseDto

    /** GET /app/tasks — assigned operator tasks. */
    suspend fun listAppTasks(
        state: String? = null,
        cursor: String? = null,
        limit: Int = APP_TASK_PAGE_SIZE,
    ): TaskListResponseDto

    /** GET /app/tasks/{task_id} — the task PLUS its SOP version (`form_dsl`) so the operator
     *  form runner can render the drive form. The list endpoint omits the form. */
    suspend fun getAppTask(taskId: String): TaskDetailResponseDto

    /** GET /app/vaccination/tasks/{task_id}/option-values — backend-pinned FEFO/source options
     *  for the exact task row. Labels and disabled reasons are already locale-aware. */
    suspend fun getTaskOptionValues(taskId: String): TaskOptionValuesResponseDto

    /** GET /app/tasks/{task_id}/shed-completion-summary — vaccination shed completion summary
     *  (read-only acknowledgement contract: shed name, drive name, animal counts, vaccine breakdown). */
    suspend fun getShedCompletionSummary(taskId: String, shedId: String? = null, partitionLabel: String? = null): ShedCompletionSummaryDto

    /** GET /app/weighing/campaigns — operator-visible Weighing campaigns (keyset paginated). */
    suspend fun listWeighingCampaigns(
        scope: String? = null,
        cursor: String? = null,
        limit: Int = WEIGHING_PAGE_SIZE,
        parkId: String? = null,
    ): WeighingCampaignListResponseDto

    /**
     * GET /app/weighing/campaigns/{campaign_id} — ONE task resolved by id.
     *
     * The read behind a notification deep link. The task list is a keyset page with no id filter,
     * so a cold tap on a task further down the keyset could only be answered by walking pages;
     * this answers it in one call. A 404 means "not yours or not there" and the two are
     * deliberately indistinguishable -- the client must not report which.
     */
    suspend fun getWeighingCampaign(campaignId: String): WeighingCampaignDetailResponseDto

    /**
     * GET /app/weighing/parks — the parks whose weighing this caller may look at.
     *
     * Identity-only park VOCABULARY, already capability-scoped by the backend and unpaged. It
     * exists because the only other park list is the planner catalog, which is gated on the
     * planning permission a Growth Director does not hold.
     */
    suspend fun listWeighingParks(): WeighingParkListResponseDto

    /**
     * GET /app/weighing/campaigns/{campaign_id}/sheds — ONE task's shed buckets, keyset-paged on
     * (display_name, campaign_shed_id). The task-detail read; the task LIST is not a substitute,
     * because a park holds 76+ sheds and a 20-task page would carry over a thousand bucket rows.
     */
    suspend fun listWeighingCampaignSheds(
        campaignId: String,
        cursor: String? = null,
        limit: Int = WEIGHING_PAGE_SIZE,
    ): WeighingCampaignShedPageResponseDto

    /**
     * GET /app/weighing/planner/catalog — the PARK-grain planner vocabulary for one weigh date.
     *
     * Every park the planner may use, each with its own shed COUNT, plus the operator picker. No
     * cursor and no limit: the park step must offer them ALL. The many side pages separately
     * through [getWeighingPlannerParkBuckets].
     */
    suspend fun getWeighingPlannerCatalog(
        periodStartDate: String,
    ): WeighingPlannerCatalogResponseDto

    /**
     * GET /app/weighing/planner/parks/{park_id}/buckets — ONE keyset page of ONE park's sheds,
     * carrying the date-scoped availability the bucket step renders.
     */
    suspend fun getWeighingPlannerParkBuckets(
        parkId: String,
        periodStartDate: String,
        cursor: String? = null,
        limit: Int = WEIGHING_PAGE_SIZE,
        // The task being EDITED, so its own sheds never read back as "already scheduled" against
        // themselves. Null on the create wizard, where there is no source task to exclude.
        excludeCampaignId: String? = null,
    ): WeighingPlannerParkBucketsResponseDto

    suspend fun createWeighingCampaign(
        idempotencyKey: String,
        request: WeighingCreateCampaignRequestDto,
    ): WeighingCampaignResponseDto

    suspend fun updateWeighingCampaign(
        campaignId: String,
        idempotencyKey: String,
        request: WeighingCreateCampaignRequestDto,
    ): WeighingCampaignResponseDto

    suspend fun publishWeighingCampaign(
        campaignId: String,
        idempotencyKey: String,
    ): WeighingCampaignResponseDto

    /**
     * GET .../roster — the active scope's SCAN HISTORY. Free-flow weighing has no expected-animal
     * roster (`weighing_expected_animals` dropped by 000079), so the response's `items` array is
     * permanently empty and the roster cursor/`include_roster` gate are gone with it: this read
     * pages `observations` only.
     */
    suspend fun getWeighingRoster(
        campaignId: String,
        campaignShedId: String,
        observationsCursor: String? = null,
        limit: Int = WEIGHING_PAGE_SIZE,
    ): WeighingRosterResponseDto

    /**
     * GET .../videos — the shed bucket as leadership reads it. `individual` is a keyset page on
     * (accepted_at, observation_id); the lump-sum row is a single latest read and is not paged.
     */
    suspend fun getWeighingLeadershipShedVideos(
        campaignId: String,
        campaignShedId: String,
        cursor: String? = null,
        limit: Int = WEIGHING_PAGE_SIZE,
    ): WeighingLeadershipShedVideosResponseDto



    /**
     * GET /app/weighing/fasting — the caller's own feed & water removal cards, ONE CARD PER SHED
     * (maintainer correction #2, 2026-09-03). Keyset-paged, newest window first. The 20:00 IST
     * serve window and the midnight roll-forward are SERVER-side: the client never derives the
     * window from its own clock — it simply re-reads and renders what comes back.
     */
    suspend fun listWeighingFastingShedCards(
        cursor: String? = null,
        limit: Int = WEIGHING_PAGE_SIZE,
    ): WeighingFastingShedCardListResponseDto

    /**
     * POST /app/weighing/fasting/{fasting_task_id}/sheds/{campaign_shed_id}/submit — ONE shed's
     * two removal videos. The offline sync engine drains this with a stable [idempotencyKey]
     * (same key on every retry), so a server-committed-but-client-unrecorded replay returns the
     * original result.
     */
    suspend fun submitWeighingFastingShed(
        fastingTaskId: String,
        campaignShedId: String,
        idempotencyKey: String,
        request: SubmitWeighingFastingShedRequestDto,
    ): WeighingFastingShedCardResponseDto

    /**
     * GET /app/weighing/alerts — the weighing module's OWN lifecycle feed: work assigned, shed
     * submitted for verification, proof sent back for rework, shed reopened, work closed, each
     * routed to whoever owns the next action.
     *
     * NOT the vaccination process-integrity feed. The backend scopes the rows to the caller and
     * authors every visible string (title/body plus the page's title and empty-state sentence).
     */
    suspend fun listWeighingAlerts(
        cursor: String? = null,
        limit: Int = WEIGHING_ALERTS_PAGE_SIZE,
    ): WeighingAlertPageResponseDto

    /**
     * GET /app/vaccination/alerts — the vaccination module's OWN lifecycle feed: a proof
     * approved, a proof sent back for rework, a record closed.
     *
     * NOT the control-tower gap summary, which is what this tab used to render and is why
     * lifecycle notifications were invisible on the phone. The backend scopes rows to the caller
     * and authors every visible string (title/body plus the page title and empty-state sentence).
     */
    suspend fun listVaccinationAlerts(
        cursor: String? = null,
        limit: Int = VACCINATION_ALERTS_PAGE_SIZE,
    ): VaccinationAlertPageResponseDto

    /** POST /app/tasks/{task_id}/submissions — idempotent SOP task submission. The offline
     *  sync engine's outbox drains this with a stable [idempotencyKey] (same key on every
     *  retry) so a server-committed-but-client-unrecorded replay never duplicates the write. */
    suspend fun submitAppTask(
        taskId: String,
        idempotencyKey: String,
        request: SubmitTaskRequestDto,
    ): SubmissionResponseDto

    /** POST /app/tasks/{task_id}/scan-captures — idempotent server-side draft RFID capture.
     *  Each scan is still written to local Room first; this endpoint protects work before the
     *  final Submit by syncing the draft capture through the durable outbox. */
    suspend fun recordScanCapture(
        taskId: String,
        idempotencyKey: String,
        request: ScanCaptureRequestDto,
    ): ScanCaptureResponseDto

    /** POST /app/tasks/{task_id}/scan-attempts — append-only RFID audit event.
     *  This records every physical reader hit, including duplicate alias, not-due, and unknown
     *  reads. It never drives Submit counters. */
    suspend fun recordScanAttempt(
        taskId: String,
        idempotencyKey: String,
        request: ScanAttemptRequestDto,
    ): ScanAttemptResponseDto

    suspend fun recordWeighingAnimalObservation(
        campaignId: String,
        idempotencyKey: String,
        request: WeighingAnimalObservationRequestDto,
    ): WeighingObservationResponseDto

    suspend fun recordWeighingShedObservation(
        campaignId: String,
        idempotencyKey: String,
        request: WeighingShedObservationRequestDto,
    ): WeighingObservationResponseDto

    suspend fun submitWeighingScope(
        campaignId: String,
        campaignShedId: String,
        idempotencyKey: String,
        request: WeighingScopeSubmitRequestDto,
    )

    suspend fun reopenWeighingScope(
        campaignId: String,
        campaignShedId: String,
        idempotencyKey: String,
        request: WeighingScopeReopenRequestDto,
    )

    /** POST /app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/close — leadership close
     *  action for a weighing shed scope. Requires permission weighing.monitor. */
    suspend fun closeShedWeighingCampaign(
        campaignId: String,
        campaignShedId: String,
        idempotencyKey: String,
        request: WeighingScopeCloseRequestDto,
    )

    /** POST /app/weighing/campaigns/{campaign_id}/close — leadership close action for an entire
     *  weighing campaign. Requires permission weighing.monitor. */
    suspend fun closeWeighingCampaign(
        campaignId: String,
        idempotencyKey: String,
        request: WeighingScopeCloseRequestDto,
    )

    /**
     * GET /weighing/campaigns/{campaign_id}/export (the PLANNER route, NOT under `/app`) — the
     * task's full CSV export (every shed, including ones with nothing captured). Requires
     * permission weighing.monitor, park-scope checked. Returns the raw `text/csv` bytes: this is a
     * FILE download, not a decoded DTO, and okhttp3.ResponseBody stays confined to core-network --
     * the implementation reads and closes it here so nothing above this module depends on OkHttp
     * types for what is otherwise just "give me the bytes of a CSV".
     */
    suspend fun exportWeighingCampaignCsv(campaignId: String): ByteArray



    /** POST /admin/tasks/{task_id}/verify — leadership verify action on a record task (C35-011).
     *  Idempotent via [idempotencyKey]. The outbox drains this like submitAppTask. */
    suspend fun verifyAppTask(
        taskId: String,
        idempotencyKey: String,
        request: ReviewTaskRequestDto,
    ): ReviewTaskResponseDto

    /** POST /admin/tasks/{task_id}/rework — leadership rework action on a record task (C35-011).
     *  Idempotent via [idempotencyKey]. The outbox drains this like submitAppTask. */
    suspend fun reworkAppTask(
        taskId: String,
        idempotencyKey: String,
        request: ReviewTaskRequestDto,
    ): ReviewTaskResponseDto

    /** GET /app/vaccination/execution/sheds/{shed_id}/roster — per-animal scan roster with RFID tags. */
    suspend fun getScanRoster(
        shedId: String,
        taskId: String? = null,
        cursor: String? = null,
        limit: Int? = null,
        partitionLabel: String? = null,
    ): ScanRosterResponseDto

    /** POST /app/vaccination/obligations/{obligation_id}/reschedule — reschedule obligation to new date. */
    suspend fun rescheduleObligation(
        obligationId: String,
        idempotencyKey: String,
        request: RescheduleObligationRequestDto,
    ): RescheduleObligationResponseDto

    /** GET /app/roster/timetable — the operator's center's enriched position list
     *  (Timetable screen; design doc docs/hr/roster-rbac-design.md). Operator-scoped —
     *  mobile never reads the admin `/admin/roster` surface (RosterRead-gated, 403s
     *  for operators). Mobile is READ-ONLY for HRMS: no create/reassign call is exposed
     *  here — all roster CRUD stays web-only (TRD §14). */
    suspend fun getOperatorTimetable(
        centerId: String,
        limit: Int? = null,
    ): EnrichedPositionListResponseDto

    /** GET /app/roster/my-coverage — the authenticated principal's own coverage status
     *  (design doc S4.6/S4.8). Drives the coverage banner: the app renders the
     *  server-composed `banner_text` verbatim, it never derives its own wording or
     *  identity bridge (TRD §14 dumb-renderer). */
    suspend fun getMyCoverage(): MyCoverageResponseDto

    /** POST /app/proofs/uploads — registers a captured proof (signed-upload metadata step;
     *  see [ProofUploadRequestDto]). The offline sync engine's outbox drains this with an
     *  idempotency key exactly like [submitAppTask] / [rescheduleObligation]. */
    suspend fun registerProof(idempotencyKey: String, request: ProofUploadRequestDto): ProofUploadResponseDto

    suspend fun listUploadedProofs(
        scopeType: String,
        scopeId: String,
        clientTaskKey: String?,
        fieldKey: String?,
        limit: Int? = 20,
    ): UploadedProofListResponseDto

    /** GET /app/proofs/{proof_id}/download — fetches the signed download URL for a proof
     *  so its media can be previewed. The URL is short-lived, so clients fetch on-demand
     *  rather than caching. */
    suspend fun getProofDownloadUrl(proofId: String): String

    /**
     * The binary-PUT + completion pass that follows a successful [registerProof]
     * (docs/mobile/proof-capture-sync-and-e2e.md §3): streams [filePath]'s bytes (this app's own
     * private storage, chunked, never buffered whole-file) to [uploadUrl]/[uploadMethod]/
     * [uploadHeaders] exactly as `registerProof` returned them, then calls
     * `POST /app/proofs/{proof_id}/complete`. Idempotent: a retry re-derives a fresh signed URL
     * via a fresh [registerProof] call first (never reuses a stale/expired one), and a PUT whose
     * object a prior attempt already wrote (GCS 412) short-circuits straight to the complete
     * call — see [ProofBlobUploader].
     */
    suspend fun uploadProofBlob(
        proofId: String,
        uploadUrl: String,
        uploadMethod: String,
        uploadHeaders: Map<String, String>,
        uploadProtocol: String,
        chunkSizeBytes: Long?,
        mimeType: String,
        filePath: String,
        durationMs: Long?,
    ): ProofCompleteResponseDto

    /** DELETE /app/proofs/{proof_id} — removes a synced proof only while it is still unattached
     *  to any submitted record. Used by pre-submit X/remove so the local UI cannot hide a backend
     *  video that would still be eligible for submission. */
    suspend fun deleteProof(proofId: String)

    /** GET /app/vaccination/gaps — animals excluded from vaccination coverage with reasons.
     *  Backs the mobile "Data gaps" overlay. */
    suspend fun getVaccinationGaps(
        parkId: String? = null,
        limit: Int? = null,
        cursor: String? = null,
    ): VaccinationGapsResponseDto

    /** GET /app/vaccination/coverage — per-vaccine given-dose count + coverage % rollup.
     *  Backs the mobile "Doses given" overlay. */
    suspend fun getVaccinationCoverage(
        parkId: String? = null,
        asOf: String? = null,
        dueBefore: String? = null,
        limit: Int? = null,
    ): VaccinationCoverageResponseDto

    /** GET /app/config — mobile live-config bundle (nav labels, flags, tunables, kill-switch).
     *  Supports conditional GET via If-None-Match header. Returns `null` when the server
     *  answers `304 Not Modified` (config unchanged — the caller keeps its cached copy);
     *  a non-null value is a fresh config bundle. */
    suspend fun getAppConfig(
        eTag: String? = null,
    ): AppConfigResponseDto?

    /** GET /verification/queue — the standalone Verifier section's media queue
     *  (context/architecture/verifier-app-and-flow.md), keyset-paginated (~20/page) and
     *  filtered to [category] (`vaccine`/`feed_direction`/`diagnosis`/`death_post_mortem`/
     *  `breeding`/…, `null` = every category this verifier is assigned). Per contracts/openapi/app-api.yaml. */
    suspend fun listVerificationQueue(
        category: String? = null,
        status: String? = null,
        businessDate: String? = null,
        missed: Boolean? = null,
        parkId: String? = null,
        shedId: String? = null,
        cursor: String? = null,
        limit: Int? = null,
    ): VerificationQueueResponseDto

    /** GET /verification/action-queue — verifier-approved items within the leadership grant scope. */
    suspend fun listVerificationActionQueue(
        category: String? = null,
        parkId: String? = null,
        shedId: String? = null,
        cursor: String? = null,
        limit: Int? = null,
    ): VerificationQueueResponseDto

    /** POST /verification/items/{item_id}/verdict — the verifier's approve/reject + reason
     *  action (`verification.review` permission). Drained through the offline-sync outbox
     *  with a stable [idempotencyKey] exactly like [verifyAppTask]/[reworkAppTask], so a
     *  server-committed-but-client-unrecorded replay never double-submits a verdict. */
    suspend fun submitVerificationVerdict(
        itemId: String,
        idempotencyKey: String,
        request: VerificationVerdictRequestDto,
    ): VerificationVerdictResponseDto

    /**
     * POST /app/weighing/observations/{observation_id}/weight-correction -- THE VERIFIER'S WEIGHT
     * CORRECTION (maintainer decision 2026-08-17). She replaces the weight the operator typed while
     * she watches the proof video; the corrected value REPLACES the recorded one.
     *
     * Weighing owns the route because the correction writes a weighing record; the verification item
     * only tells the screen WHICH record to address. Drained through the offline-sync outbox with a
     * stable [idempotencyKey] like every other write, so a server-committed-but-client-unrecorded
     * replay returns the original correction instead of writing a second one.
     */
    suspend fun correctWeighingObservationWeight(
        observationId: String,
        idempotencyKey: String,
        request: WeighingWeightCorrectionRequestDto,
    ): WeighingWeightCorrectionResponseDto

    suspend fun closeVerificationItem(
        itemId: String,
        idempotencyKey: String,
        request: VerificationCloseRequestDto,
    ): VerificationVerdictResponseDto

    /** POST /verification/submissions/{submission_id}/close — one atomic leadership action for
     *  the complete verifier-approved vaccination drive submission. */
    suspend fun closeVerificationSubmission(
        submissionId: String,
        idempotencyKey: String,
    ): VerificationCloseSubmissionResponseDto

    suspend fun closeVaccinationBatch(
        batchId: String,
        idempotencyKey: String,
    ): VerificationCloseSubmissionResponseDto

    /** POST /verification/review-events -- raw verifier journey audit rows. */
    suspend fun recordVerificationReviewEvents(
        request: VerificationReviewEventBatchRequestDto,
    ): VerificationReviewEventBatchResponseDto

    /** GET /herd-register/summary — exact scoped census counts from canonical goats. The
     *  response is a small fixed-size rollup (one row per scope grain), not a growable list,
     *  so it is fetched whole rather than paged. */
    suspend fun getHerdRegisterSummary(
        lifecycleStatus: String? = null,
        parkId: String? = null,
        breed: String? = null,
        sex: String? = null,
    ): HerdRegisterSummaryResponseDto

    /** GET /counts/breakdown — census head counts grouped by farm x stage x breed x sex x shed.
     *  Only `items` is a page ([limit]/[offset]); `total_*`, `charts`, and `facets` are rolled
     *  up over the FULL filtered set by the backend and must never be re-derived from the
     *  fetched page. [limit] is capped at one screen-page of rows by the caller
     *  (COUNTS_BREAKDOWN_PAGE_SIZE), never a whole-cohort pull. */
    suspend fun getCountsBreakdown(
        parkId: String? = null,
        shedId: String? = null,
        managementStage: String? = null,
        breed: String? = null,
        sex: String? = null,
        lifecycleStatus: String? = null,
        limit: Int? = null,
        offset: Int? = null,
    ): CountsBreakdownResponseDto

    /**
     * GET /app/counts/shifting/destinations — every park the caller may move animals INTO, each
     * carrying its own sheds. Backs the shifting screen's two cascading dropdowns.
     *
     * Fetched whole rather than paged, and that is NOT a mobile over-fetch exemption by
     * convenience: this is a bounded picker VOCABULARY (order-of two parks, ~154 sheds), not a
     * screen list that grows with the herd. It changes when the farm's physical layout changes,
     * which is approximately never, so it is cached in Room and re-served offline.
     */
    suspend fun getCountsShiftingDestinations(): CountsShiftingDestinationsResponseDto

    /**
     * GET /app/counts/breeds — the breeds present on the live herd, backing the birth form's breed
     * picker. Served on the operator (CountsWrite) surface, unlike the Counts Breakdown breed facet
     * which is CountsRead: a field operator holds CountsWrite (to record births) but not CountsRead,
     * so the picker must source its vocabulary from here, never from `/counts/breakdown`.
     *
     * A bounded picker VOCABULARY (a handful of breeds), not a screen list that grows with the herd,
     * so it is fetched whole and cached — the same contract as the shifting destinations catalog.
     */
    suspend fun getAppCountsBreeds(): CountsBreedsResponseDto

    /**
     * GET /app/counts/shifting-events/pending-execution — the operator's Actions queue: newly raised,
     * authorized, and applied movements needing evidence rework.
     * Keyset-paginated and server-capped at 20 rows. [date] is the Asia/Kolkata raised business day
     * and [status] is a disjoint backend-owned Actions bucket.
     */
    suspend fun listCountsShiftingPendingExecution(
        date: String? = null,
        status: String? = null,
        pageSize: Int? = null,
        cursor: String? = null,
    ): CountsShiftingPendingExecutionResponseDto

    /**
     * POST /app/counts/shifting-events/{id}/complete — records the operator's mandatory live-camera
     * video and completion gate. It can run before or after Park Head approval. If approval already
     * exists, this transaction relocates the animals and returns `applied`; otherwise it returns
     * `pending`, and the later approval transaction performs the move. Evidence verification is a
     * post-task review and cannot roll the herd or census back. Drained through the offline outbox
     * with a stable [idempotencyKey], so a server-committed-but-client-unrecorded retry cannot apply
     * twice. The mobile flow sends an empty [destinationTag] and lets the server derive the
     * destination cohort.
     */
    suspend fun completeCountsShiftingEvent(
        shiftingEventId: String,
        idempotencyKey: String,
        destinationTag: String? = null,
        proofRef: String,
        feedPackingProofRef: String? = null,
        feedGivenProofRef: String? = null,
        feedConfigFingerprint: String? = null,
    ): CountsShiftingExecutionResponseDto

    /**
     * GET /app/counts/goats/temporary-tagged — the operator's "Awaiting RFID" list: goats that still
     * carry an active temporary tag and are waiting to be promoted to a permanent RFID. Keyset-
     * paginated and server-capped at 20 rows. The app renders what arrives; each row carries the
     * goat's row_version for the promote call.
     */
    suspend fun listCountsTemporaryTaggedGoats(
        pageSize: Int? = null,
        cursor: String? = null,
        // Optional location filter (park -> shed cascade). Null = unfiltered on that dimension.
        parkId: String? = null,
        shedId: String? = null,
    ): TemporaryTaggedGoatsResponseDto

    /**
     * POST /app/counts/goats/{goat_id}/promote-identifier — assigns a permanent RFID to a
     * temporary-tagged goat, atomically retiring the temp. Drained through the offline outbox with a
     * stable [idempotencyKey]: a server-committed-but-client-unrecorded retry returns the ORIGINAL
     * promotion (idempotent_replay=true) instead of retagging twice. The temp tag to retire is found
     * server-side; the caller sends only the [permanentIdentifier] and the goat's [rowVersion].
     */
    suspend fun promoteCountsIdentifier(
        goatId: String,
        idempotencyKey: String,
        permanentIdentifier: String,
        rowVersion: Int,
        secondaryIdentifier: String? = null,
    ): CountsPromoteIdentifierResponseDto

    /**
     * POST /app/counts/shifting-events/{id}/cancel — retires an authorized movement that will never
     * be walked. Moves NOTHING. A [reason] is REQUIRED server-side. Same stable-key replay contract
     * as complete.
     */
    suspend fun cancelCountsShiftingEvent(
        shiftingEventId: String,
        idempotencyKey: String,
        reason: String,
    ): CountsShiftingExecutionResponseDto

    /**
     * GET /app/counts/pen-reconciliation/cards — the Herd Operations Reconcile tab: "wrong pen"
     * cards raised by weighing submits (maintainer decision 2026-09-02). The herd register is
     * truth; each card names where the animal was FOUND and where it BELONGS, and the operator
     * physically returns it. Keyset-paginated and server-capped at 20 rows. [status] is a
     * disjoint backend-owned bucket (all/open/pending_verification/rework/completed);
     * `status_counts` are whole-filter truth, never page-local.
     */
    suspend fun listCountsPenReconciliationCards(
        status: String? = null,
        pageSize: Int? = null,
        cursor: String? = null,
    ): CountsPenReconciliationListResponseDto

    /**
     * POST /app/counts/pen-reconciliation/cards/{card_id}/complete — submits the MANDATORY
     * live-camera video proving the animal was returned to its registered pen. The register is
     * truth and this request never rewrites it — no goat location changes here. The card flips to
     * pending_verification; there is deliberately NO approver step (unlike shifting). Drained
     * through the offline outbox with a stable [idempotencyKey]: a
     * server-committed-but-client-unrecorded retry returns the ORIGINAL result
     * (idempotent_replay=true) instead of queueing a second verification.
     */
    suspend fun completeCountsPenReconciliationCard(
        cardId: String,
        idempotencyKey: String,
        proofRef: String,
    ): CountsPenReconciliationCompleteResponseDto

    /**
     * GET /feed-direction/preview — one park's generated feed sheet for one Asia/Kolkata business
     * day (projected head count x authored grams/head x shed factor, split across sessions). Only
     * `items` is a page ([limit]/[offset], paged by SHED); `summary` rolls up the FULL filtered
     * scope and must never be re-derived from the fetched page. [targetDate] is `YYYY-MM-DD`;
     * [parkId] is REQUIRED by the backend (the ration grid, session split, and dispatch clock are
     * all park-scoped). [workflow] narrows to `normal`/`experiment`; null/blank means both.
     */
    suspend fun getFeedDirectionPreview(
        parkId: String,
        targetDate: String,
        shedId: String? = null,
        partitionLabel: String? = null,
        session: Int? = null,
        workflow: String? = null,
        // Optional verification-lifecycle filter: pending | pending_verification | completed.
        // null/blank means every status. Backend-owned semantics; the backend filters the whole scope
        // before paging so the page and its summary stay consistent.
        status: String? = null,
        limit: Int? = null,
        offset: Int? = null,
    ): FeedDirectionPreviewPageDto

    /**
     * GET /feed-packing/worklist — one park's per-shed bag worklist for one business day. Same
     * paging + whole-scope-summary contract as [getFeedDirectionPreview]; the row grain here is the
     * packing line (shed x session), not the ration grain.
     */
    /**
     * POST /feed-direction/complete — record that one shed-session's feed direction was carried out,
     * with OPTIONAL video proof. Idempotent on [idempotencyKey]: a replay returns the original
     * result, and the shed-session natural key makes a second completion a no-op (`applied=false`).
     */
    suspend fun completeFeedDirectionSession(
        idempotencyKey: String,
        request: FeedDirectionCompleteRequestDto,
    ): FeedDirectionCompleteResponseDto

    /**
     * POST /feed-direction/distribution/complete — the verifier-GATED feed-distribution completion
     * (docs/decisions/feed-distribution-verification.md). Carries a MANDATORY feed-distribution
     * video ref plus a MANDATORY water-distribution proof ref; flips the shed-session to
     * `pending_verification` and enqueues a verification item — NOTHING is completed until a verifier
     * approves. A blank either proof is rejected `422 proof_required`. Idempotent on [idempotencyKey]:
     * a replay re-enqueues the SAME verification item and completes nobody twice. This is separate
     * from [completeFeedDirectionSession] (the untouched packing path).
     */
    suspend fun completeFeedDistribution(
        idempotencyKey: String,
        request: FeedDistributionCompleteRequestDto,
    ): FeedDistributionCompleteResponseDto

    /**
     * POST /feed-direction/packing/complete — the verifier-GATED feed-PACKING completion. Carries a
     * SINGLE MANDATORY packing video ref; flips the shed-session to `pending_verification` and
     * enqueues a verification item — NOTHING is completed until a verifier approves. A blank proof
     * is rejected `422 proof_required`. Idempotent on [idempotencyKey]: a replay re-enqueues the SAME
     * verification item and completes nobody twice. This is separate from both
     * [completeFeedDirectionSession] (the untouched instant packing path) and
     * [completeFeedDistribution] (the two-proof distribution path).
     */
    suspend fun completeFeedPacking(
        idempotencyKey: String,
        request: FeedPackingCompleteRequestDto,
    ): FeedPackingCompleteResponseDto

    /** All applicable step videos are submitted together; only verifier approval completes it. */
    suspend fun submitMilkPreparation(
        idempotencyKey: String,
        request: MilkPreparationSubmissionRequestDto,
    ): MilkPreparationSubmissionResponseDto

    suspend fun getMilkPreparation(
        preparationDate: String? = null,
        parkId: String? = null,
        limit: Int = 20,
        offset: Int = 0,
    ): MilkPreparationPageDto

    suspend fun getMilkFeedingTasks(feedingDate: String, parkId: String? = null, sessionNo: Int? = null, limit: Int = 20, offset: Int = 0): MilkFeedingPageDto
    suspend fun submitMilkFeedingTask(taskId: String, idempotencyKey: String, request: MilkFeedingSubmitRequestDto): MilkFeedingSubmitResponseDto

    /**
     * One task per PHYSICAL SHED per day -- there is no pen filter, because a shed's whole load
     * leaves on one trip. Pen grain belongs to packing and distribution.
     */
    suspend fun getFeedTransportTasks(
        businessDate: String,
        parkId: String? = null,
        shedId: String? = null,
        status: String? = null,
        cursor: String? = null,
        limit: Int? = null,
    ): FeedTransportTaskPageDto
    suspend fun submitFeedTransport(taskId: String, idempotencyKey: String, request: FeedTransportSubmitRequestDto): FeedTransportSubmitResponseDto

    suspend fun getFeedPackingWorklist(
        parkId: String,
        targetDate: String,
        shedId: String? = null,
        partitionLabel: String? = null,
        // Optional session filter (session_no; null = every session). Mirrors the preview.
        session: Int? = null,
        workflow: String? = null,
        // Optional verification-lifecycle filter, same contract as the preview.
        status: String? = null,
        limit: Int? = null,
        offset: Int? = null,
    ): FeedPackingWorklistPageDto

    /**
     * GET /feed-wastage/worklist — one park's per-PEN wastage worklist for one feed day
     * (maintainer decision 2026-08-18). EXPERIMENT pens only; the grain is the PEN-DAY (no
     * session). Same paging + whole-scope-summary contract as [getFeedPackingWorklist].
     */
    suspend fun getFeedWastageWorklist(
        parkId: String,
        targetDate: String,
        shedId: String? = null,
        partitionLabel: String? = null,
        // Optional verification-lifecycle filter: pending | pending_verification | completed.
        status: String? = null,
        limit: Int? = null,
        offset: Int? = null,
    ): FeedWastageWorklistPageDto

    /**
     * POST /feed-direction/wastage/complete — the verifier-GATED feed-WASTAGE completion
     * (maintainer decision 2026-08-18). Carries ONE MANDATORY leftover-feed video ref; flips the
     * PEN-DAY to `pending_verification` and enqueues a verification item — NOTHING is completed
     * until a verifier approves. A blank proof is `422 proof_required`; a pen off that day's
     * experiment sheet is `422 not_experiment_pen`; a DIFFERENT video for a pen-day that already
     * holds one is a `409` the caller must surface as terminal. Idempotent on [idempotencyKey].
     */
    suspend fun completeFeedWastage(
        idempotencyKey: String,
        request: FeedWastageCompleteRequestDto,
    ): FeedWastageCompleteResponseDto

    /**
     * POST /feed-direction/wastage/{completion_id}/measurement — THE VERIFIER'S WASTAGE
     * MEASUREMENT (maintainer decision 2026-08-18). She records the leftover weight she reads off
     * the wastage video, in kg; ZERO IS VALID (an empty trough). The [completionId] comes from the
     * verification item's own `measurement_correction.observation_id`; the client never composes
     * that address itself. Drained through the offline-sync outbox with a stable value-bearing
     * [idempotencyKey], like the weighing weight correction.
     */
    suspend fun recordFeedWastageMeasurement(
        completionId: String,
        idempotencyKey: String,
        request: FeedWastageMeasurementRequestDto,
    ): FeedWastageMeasurementResponseDto

    /**
     * GET /feed-direction/distribution/captures — which of ONE pen-session's proof slots are ALREADY
     * recorded, by ANY operator, each with its SERVER proof id.
     *
     * Three operators may split a pen-session's three proofs. This is how a phone learns a slot it
     * did not shoot is done, and how whoever submits names proofs they do not hold locally.
     *
     * [partitionLabel] is part of the IDENTITY: omitting it on a partitioned shed answers for the
     * shed as a whole and would claim another pen's work.
     */

    /**
     * PC Care (module pc_care, maintainer decision 2026-08-21). The worklist is the operator's
     * ASSIGNED tasks for one category tab and one business date; the task detail carries the
     * backend-owned `expected_slots` contract the capture screen iterates verbatim.
     */
	    suspend fun getPcCareWorklist(
	        category: String,
	        date: String,
	        limit: Int? = null,
	        cursor: String? = null,
	    ): PcCareTaskPageDto

    /** GET /app/pc-care/tasks — the plan/monitor flat list (CEO planner surface). */
	    suspend fun getPcCareTasks(
	        date: String,
	        parkId: String? = null,
	        category: String? = null,
	        limit: Int? = null,
	        cursor: String? = null,
	    ): PcCareTaskPageDto

    suspend fun getPcCareTask(taskId: String): PcCareTaskDto

    /**
     * GET /app/pc-care/tasks/{task_id}/captures — the peer-visibility poll: which animals are
     * scanned and which slots each holds, by ANY assignee, with "Captured by X" attribution.
     * Read-only; it is what lets several assigned phones split one task's videos.
     */
    suspend fun getPcCareTaskCaptures(
        taskId: String,
        cursor: String? = null,
        limit: Int? = null,
    ): PcCareCapturesDto

    /**
     * GET /app/pc-care/tasks/{task_id}/roster — the roster_pick tap list: the active RFIDs of
     * alive animals currently in the task's pen. Read-only; tapping one records a normal
     * free-flow scan, so this list never gates what a scan may store.
     */
    suspend fun getPcCareTaskRoster(
        taskId: String,
        cursor: String? = null,
        limit: Int? = null,
    ): PcCareTaskRosterDto

    /**
     * POST /app/pc-care/tasks/{task_id}/animals — scan one RFID into the task, VERBATIM. A tag
     * already in the task is `409 duplicate_scan` (terminal — surface "Already scanned", never
     * re-enqueue under a new key); a locked task is `409 task_locked`.
     */
    suspend fun scanPcCareAnimal(
        taskId: String,
        idempotencyKey: String,
        request: PcCareScanRequestDto,
    ): PcCareScanResponseDto

    /**
     * PUT /app/pc-care/tasks/{task_id}/animals/{animal_row_id}/proofs/{slot} — attach one slot's
     * live-camera video (server proof id from the /app/proofs pipeline) to one scanned animal.
     */
    suspend fun registerPcCareSlotProof(
        taskId: String,
        animalRowId: String,
        slot: String,
        idempotencyKey: String,
        request: PcCareSlotProofRequestDto,
    )

    suspend fun registerPcCareTaskProof(
        taskId: String,
        slot: String,
        idempotencyKey: String,
        request: PcCareSlotProofRequestDto,
    )

    /**
     * POST /app/pc-care/tasks/{task_id}/submit — submit the WHOLE task (any assignee). Refused
     * until every scanned animal carries its full slot set (`422 proof_incomplete`) or while no
     * animal is scanned (`422 no_animals`). Idempotent on [idempotencyKey].
     */
    suspend fun submitPcCareTask(
        taskId: String,
        idempotencyKey: String,
    ): PcCareSubmitResponseDto

    suspend fun getPcCarePlannerCatalog(): PcCarePlannerCatalogDto

    suspend fun getPcCarePlannerParkSheds(
        parkId: String,
        category: String,
        date: String,
        cursor: String? = null,
        limit: Int? = null,
    ): PcCarePlannerShedsDto

    suspend fun createPcCareTask(
        idempotencyKey: String,
        request: PcCareCreateTaskRequestDto,
    ): PcCareTaskDto

    /**
     * POST /app/pc-care/rounds — plan a ROUND covering one or more pens in ONE write
     * (maintainer decision 2026-09-05). Replaces the create-per-pen loop: the planner ticks
     * the pens once, and either the whole round lands or none of it does.
     */
    suspend fun createPcCareRound(
        idempotencyKey: String,
        request: PcCareCreateRoundRequestDto,
    ): PcCareRoundDto

    /**
     * GET /app/pc-care/rounds — the PLANNER's list at ROUND grain: one card per round, and one
     * per round-less legacy task. The operator worklist stays pen-grained; an operator works a
     * pen, a planner plans a round.
     */
    suspend fun getPcCareRoundCards(
        date: String?,
        category: String?,
        parkId: String?,
        filter: String?,
        cursor: String?,
        limit: Int?,
    ): PcCareRoundCardPageDto

    /** GET /app/pc-care/rounds/{round_id} — one round with its pen buckets. */
    suspend fun getPcCareRound(roundId: String): PcCareRoundDto

    /** GET /app/pc-care/tasks/{task_id}/removal-pens — a removal card's pen-by-pen slot list. */
    suspend fun getPcCareRemovalPens(taskId: String): PcCareRemovalPenListDto

    /** PUT /app/pc-care/tasks/{task_id}/removal-pens/proofs/{slot} — one pen's feed or water video. */
    suspend fun putPcCareRemovalPenProof(
        taskId: String,
        slot: String,
        idempotencyKey: String,
        request: PcCareRemovalPenProofRequestDto,
    )

    /**
     * POST /app/pc-care/tasks/{task_id}/close — end a task's work with a reason (maintainer
     * decision 2026-09-05, RETIRING cancel). REFUSED (409) while the task's evidence is
     * awaiting a verdict; the way out is to finish the review, never to force the close.
     */
    suspend fun closePcCareTask(taskId: String, request: PcCareCloseRequestDto)

    /** POST /app/pc-care/tasks/{task_id}/reopen — undo a close. */
    suspend fun reopenPcCareTask(taskId: String)

    /** POST /app/pc-care/rounds/{round_id}/close — end a whole round with a reason. */
    suspend fun closePcCareRound(roundId: String, request: PcCareCloseRequestDto)

    /**
     * POST /app/pc-care/tasks/{task_id}/stock-verdict — the PC Director's approve/reject on a
     * submitted vaccine-stock task (maintainer decision 2026-09-02). Server-gated on
     * pc_care.stock_approve; never the verifier's route.
     */
    suspend fun recordPcCareStockVerdict(
        taskId: String,
        request: PcCareStockVerdictRequestDto,
    ): PcCareTaskDto

    // ------------------------------------------------------------------
    // Toxin (aflatoxin strip test, maintainer decision 2026-08-25)
    // ------------------------------------------------------------------

    /**
     * GET /app/toxin/tasks — the tester's task list, one row per test round, keyset-paged.
     * [filter] is a backend filter KEY (`all` | `pending` | `completed`); blank means `all`.
     * The client never composes a status list — the backend owns what each key means.
     */
    suspend fun getToxinTasks(
        filter: String? = null,
        limit: Int? = null,
        cursor: String? = null,
    ): ToxinTaskPageDto

    /**
     * GET /app/toxin/tasks/{task_id} — the guided 7-step flow with LIVE server-composed step
     * states. The phone renders [ToxinStepDto.state] verbatim and never derives gate logic from
     * its own clock.
     */
    suspend fun getToxinTask(taskId: String): ToxinTaskDetailDto

    /**
     * POST /app/toxin/tasks/{task_id}/steps/{step_no}/complete — records one step's proof.
     * `422 wait_not_elapsed` (server clock gate) and `409 step_already_done` carry backend farm
     * copy to surface verbatim; both mean "refresh and re-render server state".
     */
    suspend fun completeToxinStep(
        taskId: String,
        stepNo: Int,
        idempotencyKey: String,
        request: ToxinStepCompleteRequestDto,
    ): ToxinTaskDetailDto

    /** POST /app/toxin/tasks/{task_id}/submit — step 7's strip photo + reading. */
    suspend fun submitToxinReading(
        taskId: String,
        idempotencyKey: String,
        request: ToxinSubmitRequestDto,
    ): ToxinTaskDetailDto

    // ------------------------------------------------------------------
    // Vendors (vendor register + feed purchases on the phone, maintainer decision 2026-09-03)
    // ------------------------------------------------------------------

    /**
     * GET /procurement/vendors — one bounded page of the register, newest first, optionally
     * narrowed by a search term (backend trigram search) and a status. Offset-paged on the server
     * (a few hundred rows, bounded), consumed here ~20 at a time.
     */
    suspend fun getProcurementVendors(
        search: String? = null,
        status: String? = null,
        limit: Int? = null,
        offset: Int? = null,
    ): VendorPageDto

    /** GET /procurement/vendors/{vendor_id} — one register row. */
    suspend fun getProcurementVendor(vendorId: String): VendorDto

    /** GET /procurement/vendor-catalog — the business-managed vocabularies the add form renders. */
    suspend fun getProcurementVendorCatalog(): VendorCatalogDto

    /**
     * POST /procurement/vendors — records a vendor. No idempotency header on this route: the
     * register's natural key (business, record type, state, phone) refuses a duplicate with
     * `409 vendor_duplicate`, which a replay after a lost response reads as "already there".
     */
    suspend fun createProcurementVendor(request: VendorWriteDto): VendorDto

    /** GET /procurement/feed-purchases — one bounded page of the ledger, newest first. */
    suspend fun getFeedPurchases(
        farm: String? = null,
        delivery: String? = null,
        limit: Int? = null,
        offset: Int? = null,
    ): FeedPurchasePageDto

    /** GET /procurement/feed-purchase-options — farms, the ACTIVE feed catalog, payment words, vendors seen. */
    suspend fun getFeedPurchaseOptions(): FeedPurchaseOptionsDto

    /** POST /procurement/feed-purchases — records a load. The `Idempotency-Key` is REQUIRED. */
    suspend fun createFeedPurchase(
        idempotencyKey: String,
        request: FeedPurchaseWriteDto,
    ): FeedPurchaseDto

    // Sales (maintainer instruction 2026-09-04): the same routes the web's /sales/config uses.

    // Changing a recorded feed load (maintainer instruction 2026-09-04). Each returns the WHOLE
    // updated load, so the caller persists the server's row rather than patching its own.

    /** POST /procurement/feed-purchases/{id}/payments — one instalment. Idempotency-Key REQUIRED. */
    suspend fun createFeedPurchasePayment(
        purchaseId: String,
        idempotencyKey: String,
        request: FeedPurchasePaymentWriteDto,
    ): FeedPurchaseDto

    /** PUT /procurement/feed-purchases/{id}/payment-status — Paid or Pending. */
    suspend fun setFeedPurchasePaymentStatus(purchaseId: String, request: FeedPurchaseStatusWriteDto): FeedPurchaseDto

    /** PUT /procurement/feed-purchases/{id} — corrects a recorded load's values. */
    suspend fun editFeedPurchase(purchaseId: String, request: FeedPurchaseEditDto): FeedPurchaseDto

    /** PUT /procurement/feed-purchases/{id}/delivery — marks the load reached. */
    suspend fun recordFeedPurchaseDelivery(purchaseId: String, request: FeedPurchaseDeliveryWriteDto): FeedPurchaseDto

    /** GET /sales/deals — one bounded page of the ledger, newest sale first. Offset-paged. */
    suspend fun getSalesDeals(
        farm: String? = null,
        limit: Int? = null,
        offset: Int? = null,
    ): SalesDealPageDto

    /** GET /sales/options — farms, products, breeds per product, statuses with tones. */
    suspend fun getSalesOptions(): SalesOptionsDto

    /** GET /procurement/vendor-options — the ACTIVE register as the buyer picklist. */
    suspend fun getVendorOptions(): VendorOptionsDto

    /** POST /sales/deals — records a sale. The `Idempotency-Key` is REQUIRED. */
    suspend fun createSalesDeal(
        idempotencyKey: String,
        request: SalesDealWriteDto,
    ): SalesDealDto

    /** GET /admin/goats/sale-locations — the park/pen catalog the tag-animals picker offers. */
    suspend fun getSaleLocations(): SaleLocationsDto

    /** GET /admin/goats/sale-candidates — a cursor page of animals that could be sold. */
    suspend fun getSaleCandidates(
        parkId: String,
        shedId: String? = null,
        partitionLabels: List<String> = emptyList(),
        query: String? = null,
        limit: Int? = null,
        cursor: String? = null,
    ): SaleCandidatePageDto

    /** GET /admin/goats/sale-allocations/{deal} — what is already tagged to this sale. */
    suspend fun getSaleAllocation(salesDealId: String): SaleAllocationDto

    /** POST /admin/goats/sale-allocations/preview — the review step; mutates nothing. */
    suspend fun previewSaleAllocation(request: SaleAllocationRequestDto): SalePreviewDto

    /** POST /admin/goats/sale-allocations/confirm — marks the picked animals sold. Idempotency-Key. */
    suspend fun confirmSaleAllocation(
        idempotencyKey: String,
        request: SaleAllocationRequestDto,
    ): SaleAllocationDto

    // Editing a recorded sale (maintainer instruction 2026-09-04). Each returns the WHOLE updated
    // deal, so the caller persists the server's row and computes no money figure of its own.

    /** POST /sales/deals/{deal_id}/payments — records a buyer receipt. Idempotency-Key. */
    suspend fun createSalesDealPayment(
        dealId: String,
        idempotencyKey: String,
        request: SalesDealPaymentWriteDto,
    ): SalesDealDto

    /** PUT /sales/deals/{deal_id}/payments/{payment_id} — corrects one receipt. */
    suspend fun updateSalesDealPayment(
        dealId: String,
        paymentId: String,
        idempotencyKey: String,
        request: SalesDealPaymentWriteDto,
    ): SalesDealDto

    /** DELETE /sales/deals/{deal_id}/payments/{payment_id} — removes one receipt. */
    suspend fun deleteSalesDealPayment(
        dealId: String,
        paymentId: String,
        idempotencyKey: String,
    ): SalesDealDto

    /** POST /sales/deals/{deal_id}/status — moves the deal's status word. */
    suspend fun setSalesDealStatus(
        dealId: String,
        idempotencyKey: String,
        request: SalesDealStatusWriteDto,
    ): SalesDealDto

    // Pipeline and evidence: the five panels the retired Sales DB sheet carried.

    /** GET /sales/buyer-leads — the buyer demand board, newest first. */
    suspend fun getSalesBuyerLeads(limit: Int? = null, offset: Int? = null): SalesBuyerLeadPageDto

    /** POST /sales/buyer-leads — records one buyer enquiry. */
    suspend fun createSalesBuyerLead(idempotencyKey: String, request: SalesBuyerLeadWriteDto): SalesBuyerLeadDto

    /** POST /sales/buyer-leads/{lead_id}/status — moves where that conversation stands. */
    suspend fun setSalesBuyerLeadStatus(leadId: String, idempotencyKey: String, request: SalesLeadStatusWriteDto): SalesBuyerLeadDto

    /** GET /sales/fpo-leads — the farmer-group board. */
    suspend fun getSalesFpoLeads(limit: Int? = null, offset: Int? = null): SalesFpoLeadPageDto

    /** POST /sales/fpo-leads — records one farmer group. */
    suspend fun createSalesFpoLead(idempotencyKey: String, request: SalesFpoLeadWriteDto): SalesFpoLeadDto

    /** POST /sales/fpo-leads/{lead_id}/status. */
    suspend fun setSalesFpoLeadStatus(leadId: String, idempotencyKey: String, request: SalesLeadStatusWriteDto): SalesFpoLeadDto

    /** POST /sales/market-benchmarks — what a market is paying, beside our landed cost. */
    suspend fun createSalesMarketBenchmark(idempotencyKey: String, request: SalesBenchmarkWriteDto): SalesRecordedDto

    /** POST /sales/sold-tags — the tag list of a sold lot. */
    suspend fun createSalesSoldTags(idempotencyKey: String, request: SalesSoldTagsWriteDto): SalesSoldTagsResultDto

    /** POST /sales/weight-checks — the book weight beside the weight read off the video. */
    suspend fun createSalesWeightCheck(idempotencyKey: String, request: SalesWeightCheckWriteDto): SalesRecordedDto
    // Leadership Tasks (maintainer request 2026-09-04)
    // ------------------------------------------------------------------

    /**
     * GET /app/leadership-tasks — the caller's tasks (a director sees what they raised, a CXO
     * what is assigned to them), keyset-paged. [filter] is a backend filter KEY
     * (`all` | `open` | `in_progress` | `done`); blank means the backend default.
     */
    suspend fun getLeadershipTasks(
        filter: String? = null,
        limit: Int? = null,
        cursor: String? = null,
    ): LeadershipTaskPageDto

    /** GET /app/leadership-tasks/assignees — the CXOs a director may raise a task for. */
    suspend fun getLeadershipTaskAssignees(): LeadershipAssigneeListDto

    /** GET /app/leadership-tasks/{task_id}. */
    suspend fun getLeadershipTask(taskId: String): LeadershipTaskDetailDto

    /** POST /app/leadership-tasks — raise a task. Idempotent on [idempotencyKey]. */
    suspend fun raiseLeadershipTask(
        idempotencyKey: String,
        request: LeadershipTaskRaiseRequestDto,
    ): LeadershipTaskDetailDto

    /** POST /app/leadership-tasks/{task_id}/edit — title/body/attachments, fenced on row_version. */
    suspend fun editLeadershipTask(
        taskId: String,
        idempotencyKey: String,
        request: LeadershipTaskEditRequestDto,
    ): LeadershipTaskDetailDto

    /** POST /app/leadership-tasks/{task_id}/status — move the task, fenced on row_version. */
    suspend fun changeLeadershipTaskStatus(
        taskId: String,
        idempotencyKey: String,
        request: LeadershipTaskStatusRequestDto,
    ): LeadershipTaskDetailDto

    /** POST /app/leadership-tasks/{task_id}/seen — the assignee opened it (no body, no key). */
    suspend fun markLeadershipTaskSeen(taskId: String): LeadershipTaskDetailDto

    /** The assignee's note back on the task (one field, overwritten). */
    suspend fun setLeadershipTaskComment(
        taskId: String,
        idempotencyKey: String,
        request: LeadershipTaskCommentRequestDto,
    ): LeadershipTaskDetailDto

    /** GET /app/leadership-tasks/{task_id}/attachments/{proof_id}/download. */
    suspend fun getLeadershipTaskAttachmentDownloadUrl(taskId: String, proofId: String): String

    suspend fun getFeedDistributionCaptures(
        parkId: String?,
        shedId: String,
        partitionLabel: String?,
        sessionNo: Int,
        targetDate: String,
        workflow: String,
    ): FeedDistributionCapturesDto

    /** POST /app/counts/shifting-events — an operator-reported movement between sheds. Drained
     *  through the offline-sync outbox with a stable [idempotencyKey]: the backend derives the
     *  movement's logical key from that key, so an exact retry collapses onto the SAME row
     *  instead of recording a second movement. */
    suspend fun recordCountsShiftingEvent(
        idempotencyKey: String,
        request: CountsShiftingEventRequestDto,
    ): CountsShiftingEventResponseDto

    /** POST /app/counts/birth-events — creates every child and its birth work immediately. The
     *  accompanying web approval controls only whether those children join herd counts. */
    suspend fun recordCountsBirthEvent(
        idempotencyKey: String,
        request: CountsBirthEventRequestDto,
    ): CountsApprovalSubmitResponseDto

    /** POST /app/counts/death-events — raises a pending death approval request. The animal exits
     *  only when the request is approved. */
    suspend fun recordCountsDeathEvent(
        idempotencyKey: String,
        request: CountsDeathEventRequestDto,
    ): CountsApprovalSubmitResponseDto

    /**
     * GET /goats/search — scope-filtered animal lookup. Backs the shifting screen's animal
     * picker: the operator scans or types a tag, and this resolves it to the `goat_id` the
     * shifting write must carry.
     *
     * [limit] is bounded by the caller to one screen-page (never a whole-cohort pull), and
     * [cursor] is the backend's own keyset continuation.
     */
    suspend fun searchGoats(
        q: String? = null,
        parkId: String? = null,
        locationId: String? = null,
        status: String? = null,
        limit: Int? = null,
        cursor: String? = null,
    ): GoatSearchResponseDto

    /** GET /app/counts/approvals — the approver's queue, keyset-paginated and server-capped at
     *  20 rows. Returns ONLY the request types this caller may decide; the app renders what
     *  arrives and never re-derives that authority locally. */
    suspend fun listCountsApprovals(
        status: String? = null,
        pageSize: Int? = null,
        cursor: String? = null,
    ): CountsApprovalListResponseDto

    /** POST /app/counts/approvals/{request_id}/approve — applies the request (activates birth
     *  count eligibility, exits the animal, or authorizes movement), atomically with
     *  the status flip. Drained through the offline outbox with a stable [idempotencyKey] so a
     *  server-committed-but-client-unrecorded retry returns the original decision instead of
     *  applying the effect twice. */
    suspend fun approveCountsApproval(
        requestId: String,
        idempotencyKey: String,
        request: CountsApprovalDecisionRequestDto,
    ): CountsApprovalDecisionResponseDto

    /** POST /app/counts/approvals/{request_id}/reject — rejects the request, applying nothing.
     *  A reason is REQUIRED server-side. Same stable-key replay contract as approve. */
    suspend fun rejectCountsApproval(
        requestId: String,
        idempotencyKey: String,
        request: CountsApprovalDecisionRequestDto,
    ): CountsApprovalDecisionResponseDto

    /**
     * GET /app/workflows — the Birth/Death follow-up work list
     * (docs/decisions/birth-death-workflows.md): one card per (template, subject goat) workflow
     * opened when the event APPLIED. Scoped to ONE [module] (`birth`|`death`) and one Asia/Kolkata
     * business [date] (`YYYY-MM-DD`, default today IST); [filter] is the backend bucket
     * (`all|overdue|due|completed|awaiting_video`). Keyset-paginated and server-capped at 20; the
     * response also carries the day's chip counts computed over the same key set the page reads.
     */
    suspend fun listWorkflows(
        module: String,
        date: String? = null,
        filter: String? = null,
        pageSize: Int? = null,
        cursor: String? = null,
    ): WorkflowListResponseDto

    /**
     * GET /app/workflows/{workflow_id} — one workflow's card header, context facts, and full
     * bounded action list (≤18 rows).
     *
     * [lens] = `colostrum` narrows the rows to the colostrum feeds due on [date] and re-counts the
     * card header at that day's grain, so the Colostrum detail matches the card that opened it
     * (docs/decisions/colostrum-milk-module.md). Blocked state still comes from the kid's complete
     * action set, so a feed may legitimately arrive `blocked` with a reason naming work this
     * screen does not show.
     */
    suspend fun getWorkflow(
        workflowId: String,
        lens: String? = null,
        date: String? = null,
    ): WorkflowDetailResponseDto

    /**
     * POST /app/workflows/{workflow_id}/actions/{action_id}/answer — answers a question /
     * question_select action and completes it. Drained through the offline outbox with a stable
     * [idempotencyKey]: an exact replay returns the original result with `idempotent_replay=true`.
     */
    suspend fun answerWorkflowAction(
        workflowId: String,
        actionId: String,
        idempotencyKey: String,
        request: WorkflowActionAnswerRequestDto,
    ): WorkflowActionWriteResponseDto

    /**
     * POST /app/workflows/{workflow_id}/actions/{action_id}/complete — completes an `action`-type
     * step. `proof_ref` is MANDATORY when the action `requires_video` (missing → 422
     * `proof_required`). Same stable-key replay contract as answer.
     */
    suspend fun completeWorkflowAction(
        workflowId: String,
        actionId: String,
        idempotencyKey: String,
        request: WorkflowActionCompleteRequestDto,
    ): WorkflowActionWriteResponseDto

    suspend fun listHealthWorkItems(
        ageBand: String,
        date: String,
        status: String? = null,
        diseaseKey: String? = null,
        parkId: String? = null,
        shedId: String? = null,
        session: String? = null,
        cursor: String? = null,
        limit: Int? = null,
    ): HealthWorkItemPageDto

    suspend fun openHealthCase(
        idempotencyKey: String,
        request: HealthOpenCaseRequestDto,
    ): HealthOpenCaseResponseDto

    suspend fun getHealthWorkItem(healthSessionId: String): HealthWorkItemDetailDto

    /** POST /app/health/observations — record one observation and receive a PROPOSAL. Opens nothing. */
    suspend fun submitHealthObservation(
        idempotencyKey: String,
        request: SubmitHealthObservationRequestDto,
    ): HealthDiagnosisProposalResponseDto

    /**
     * GET /app/health/observations — the Director's queue, newest first.
     *
     * A KEYSET page. `cursor` comes from the previous page's `next_cursor`; there is
     * no offset, because new observations land at the head of the list and an offset
     * would re-show or skip rows as work arrives mid-scroll.
     */
    suspend fun listHealthObservations(
        status: String?,
        goatId: String?,
        cursor: String?,
        limit: Int?,
    ): HealthDiagnosisQueuePageDto

    /** GET /app/health/observations/{id} — read one diagnosis run and its stored proposal. */
    suspend fun getHealthObservation(diagnosisRunId: String): HealthDiagnosisRunDto

    /**
     * GET /app/health/death-causes — every disease a death may be recorded as.
     *
     * ONE READ, NO PAGING, cached: the whole vocabulary is a few dozen diseases and it is
     * static for the life of the server process, so the death form searches it locally rather
     * than round-tripping per keystroke.
     */
    suspend fun listDeathCauses(): DeathCauseCatalogDto

    /**
     * POST /app/health/observations/{id}/confirm — the Director's decision, and the
     * only path that opens a treatment course. An empty list declines the whole
     * proposal, which is a legitimate override.
     */
    suspend fun confirmHealthDiagnosis(
        diagnosisRunId: String,
        idempotencyKey: String,
        request: ConfirmHealthDiagnosisRequestDto,
    ): ConfirmHealthDiagnosisResponseDto

    suspend fun completeHealthWorkItem(
        healthSessionId: String,
        idempotencyKey: String,
        request: HealthCompleteRequestDto,
    ): HealthCompleteResponseDto

    /** POST /app/health/cases/{health_case_id}/close — the clinical outcome (health.diagnose). */
    suspend fun closeHealthCase(
        healthCaseId: String,
        idempotencyKey: String,
        request: HealthCloseCaseRequestDto,
    ): HealthCloseCaseResponseDto
    /**
     * POST /app/clock/in — the day's clock-in punch (docs/features/clock-in-out/plan.md).
     * Drained through the offline-sync outbox with the STABLE day-scoped [idempotencyKey]
     * (`clock:<business_date>:in`), so a server-committed-but-client-unrecorded retry replays
     * the original entry instead of punching twice. A payload admitting a mock-provided fix or
     * an installed mock-location app is refused 422 `mock_location_detected`; a second clock-in
     * on the same IST business day is refused 409 `already_clocked_in`.
     */
    suspend fun recordClockIn(
        idempotencyKey: String,
        request: ClockPunchRequestDto,
    ): ClockPunchResponseDto

    /** POST /app/clock/out — closes today's open entry; the backend stamps `worked_minutes`.
     *  409 `not_clocked_in` / `already_clocked_out`; same mock gate as clock-in. */
    suspend fun recordClockOut(
        idempotencyKey: String,
        request: ClockPunchRequestDto,
    ): ClockPunchResponseDto

    /** GET /app/clock/status — today's state, recent days, ALL module copy, and the shell
     *  reminder `banner_text` (empty = no banner). */
    suspend fun getClockStatus(): ClockStatusResponseDto

    /** GET /app/clock/presence — the leadership presence board: one row per ACTIVE workforce
     *  member for the date, keyset-paginated (~20). Summary tiles are whole-filter aggregates. */
    suspend fun listClockPresence(
        date: String? = null,
        parkId: String? = null,
        designation: String? = null,
        bucket: String? = null,
        q: String? = null,
        limit: Int? = null,
        cursor: String? = null,
    ): ClockPresenceResponseDto

    /** GET /app/clock/presence/{workforce_member_id} — one person's day in full. */
    suspend fun getClockPresencePerson(
        workforceMemberId: String,
        date: String? = null,
    ): ClockPersonDayResponseDto
}

/**
 * Canned bootstrap so the shell renders the backend-driven nav end-to-end before
 * auth/network are wired. `chrome` lets previews exercise both drawer + bottom-bar
 * states. This is test/dev scaffolding, NOT product truth. Data methods return empty
 * responses so previews/tests compile without a live backend.
 */
class FakeAppApi(private val chrome: String = "expanded") : AppApi {
    override suspend fun recordAnalyticsEvent(request: AppAnalyticsEventRequestDto): AppAnalyticsEventResponseDto =
        AppAnalyticsEventResponseDto()

    override suspend fun bootstrap(deviceId: String?): BootstrapDto = BootstrapDto(
        navChrome = chrome,
        visibleNavigation = listOf(
            NavItemDto(key = "vaccination", label = "Drives", href = "/vaccination"),
            NavItemDto(key = "calendar", label = "Calendar", href = "/calendar"),
            NavItemDto(key = "alerts", label = "Alerts", href = "/vaccination/alerts"),
        ),
        // Mirrors the backend moduleNavRegistry shape (available + soon) so previews and
        // screenshot tests render the real backend-composed drawer, not a client stub.
        modules = listOf(
            BootstrapModuleDto(
                key = "vaccination",
                label = "Vaccination",
                href = "/vaccination",
                status = "available",
                navItems = listOf(
                    NavItemDto(key = "vaccination", label = "Drives", href = "/vaccination"),
                    NavItemDto(key = "calendar", label = "Calendar", href = "/calendar"),
                    NavItemDto(key = "alerts", label = "Alerts", href = "/vaccination/alerts"),
                ),
            ),
            BootstrapModuleDto(
                key = "feed_direction",
                label = "Feed",
                href = "/feed/direction",
                status = "available",
                navItems = listOf(
                    NavItemDto(key = "feed_direction", label = "Feed Direction", href = "/feed/direction"),
                    NavItemDto(key = "feed_packing", label = "Feed Packing", href = "/feed/packing"),
                    NavItemDto(key = "feed_transport", label = "Feed Transport", href = "/feed/transport"),
                ),
            ),
            BootstrapModuleDto(key = "breeding", label = "Breeding", status = "soon"),
        ),
        featureFlags = mapOf("tasks" to true, "sop_runner" to true),
        appMinSupportedVersion = "0.1.0",
    )

    override suspend fun registerDevice(request: RegisterDeviceRequestDto): DeviceResponseDto =
        DeviceResponseDto(device = DeviceSummaryDto(deviceId = "fake-device", status = "active"))

    override suspend fun heartbeatDevice(deviceId: String, request: HeartbeatDeviceRequestDto): DeviceResponseDto =
        DeviceResponseDto(device = DeviceSummaryDto(deviceId = deviceId, status = "active"))

    override suspend fun deregisterDevice(deviceId: String): DeviceResponseDto =
        DeviceResponseDto(device = DeviceSummaryDto(deviceId = deviceId, status = "revoked"))

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
    ): VaccinationExecutionResponseDto = VaccinationExecutionResponseDto()

    override suspend fun getVaccinationExecutionShed(
        shedId: String,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
        partitionLabel: String?,
    ): VaccinationExecutionShedDrilldownDto = VaccinationExecutionShedDrilldownDto(shedId = shedId)

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
    ): CalendarEventListResponseDto = CalendarEventListResponseDto()

    override suspend fun getVaccinationControlTower(
        parkId: String?,
        shedId: String?,
        workState: String?,
        severity: String?,
        dueBefore: String?,
        asOf: String?,
        cursor: String?,
        limit: Int?,
    ): ControlTowerResponseDto = ControlTowerResponseDto()

    override suspend fun getVaccinationAdherence(
        parkId: String?,
        shedId: String?,
        workState: String?,
        severity: String?,
        dueBefore: String?,
        asOf: String?,
        cursor: String?,
        limit: Int?,
    ): ProtocolAdherenceResponseDto = ProtocolAdherenceResponseDto()

    override suspend fun listAppTasks(
        state: String?,
        cursor: String?,
        limit: Int,
    ): TaskListResponseDto = TaskListResponseDto()

    override suspend fun getAppTask(taskId: String): TaskDetailResponseDto = TaskDetailResponseDto()

    override suspend fun getTaskOptionValues(taskId: String): TaskOptionValuesResponseDto =
        TaskOptionValuesResponseDto(taskId = taskId)

    override suspend fun getShedCompletionSummary(taskId: String, shedId: String?, partitionLabel: String?): ShedCompletionSummaryDto =
        ShedCompletionSummaryDto(
            taskId = taskId,
            shedName = "Shed A — Weaners",
            driveName = "Vaccination · July 2026",
            expectedCount = 50,
            handledCount = 50,
            proofReadyCount = 50,
            vaccineBreakdown = emptyList(),
            submitEnabled = true,
            blockingReason = null,
            submitState = "draft",
        )

    override suspend fun listWeighingCampaigns(
        scope: String?,
        cursor: String?,
        limit: Int,
        parkId: String?,
    ): WeighingCampaignListResponseDto = WeighingCampaignListResponseDto()

    override suspend fun getWeighingCampaign(campaignId: String): WeighingCampaignDetailResponseDto =
        WeighingCampaignDetailResponseDto()

    override suspend fun listWeighingParks(): WeighingParkListResponseDto = WeighingParkListResponseDto()

    override suspend fun listWeighingFastingShedCards(
        cursor: String?,
        limit: Int,
    ): WeighingFastingShedCardListResponseDto = WeighingFastingShedCardListResponseDto()

    override suspend fun submitWeighingFastingShed(
        fastingTaskId: String,
        campaignShedId: String,
        idempotencyKey: String,
        request: SubmitWeighingFastingShedRequestDto,
    ): WeighingFastingShedCardResponseDto = WeighingFastingShedCardResponseDto(
        fastingShedCard = WeighingFastingShedCardDto(
            fastingTaskId = fastingTaskId,
            campaignShedId = campaignShedId,
            feedProofRef = request.feedProofRef,
            waterProofRef = request.waterProofRef,
            status = "pending_verification",
        ),
    )

    override suspend fun listWeighingCampaignSheds(
        campaignId: String,
        cursor: String?,
        limit: Int,
    ): WeighingCampaignShedPageResponseDto = WeighingCampaignShedPageResponseDto()

    override suspend fun getWeighingPlannerCatalog(
        periodStartDate: String,
    ): WeighingPlannerCatalogResponseDto = WeighingPlannerCatalogResponseDto()

    override suspend fun getWeighingPlannerParkBuckets(
        parkId: String,
        periodStartDate: String,
        cursor: String?,
        limit: Int,
        excludeCampaignId: String?,
    ): WeighingPlannerParkBucketsResponseDto = WeighingPlannerParkBucketsResponseDto()

    override suspend fun createWeighingCampaign(
        idempotencyKey: String,
        request: WeighingCreateCampaignRequestDto,
    ): WeighingCampaignResponseDto = WeighingCampaignResponseDto()

    override suspend fun updateWeighingCampaign(
        campaignId: String,
        idempotencyKey: String,
        request: WeighingCreateCampaignRequestDto,
    ): WeighingCampaignResponseDto = WeighingCampaignResponseDto()

    override suspend fun publishWeighingCampaign(
        campaignId: String,
        idempotencyKey: String,
    ): WeighingCampaignResponseDto = WeighingCampaignResponseDto()

    override suspend fun getWeighingRoster(
        campaignId: String,
        campaignShedId: String,
        observationsCursor: String?,
        limit: Int,
    ): WeighingRosterResponseDto = WeighingRosterResponseDto()

    override suspend fun getWeighingLeadershipShedVideos(
        campaignId: String,
        campaignShedId: String,
        cursor: String?,
        limit: Int,
    ): WeighingLeadershipShedVideosResponseDto = WeighingLeadershipShedVideosResponseDto()

    override suspend fun listWeighingAlerts(
        cursor: String?,
        limit: Int,
    ): WeighingAlertPageResponseDto = WeighingAlertPageResponseDto()

    override suspend fun listVaccinationAlerts(
        cursor: String?,
        limit: Int,
    ): VaccinationAlertPageResponseDto = VaccinationAlertPageResponseDto()

    override suspend fun submitAppTask(
        taskId: String,
        idempotencyKey: String,
        request: SubmitTaskRequestDto,
    ): SubmissionResponseDto = SubmissionResponseDto()

    override suspend fun recordScanCapture(
        taskId: String,
        idempotencyKey: String,
        request: ScanCaptureRequestDto,
    ): ScanCaptureResponseDto = ScanCaptureResponseDto()

    override suspend fun recordScanAttempt(
        taskId: String,
        idempotencyKey: String,
        request: ScanAttemptRequestDto,
    ): ScanAttemptResponseDto = ScanAttemptResponseDto()

    override suspend fun recordWeighingAnimalObservation(
        campaignId: String,
        idempotencyKey: String,
        request: WeighingAnimalObservationRequestDto,
    ): WeighingObservationResponseDto = WeighingObservationResponseDto()

    override suspend fun recordWeighingShedObservation(
        campaignId: String,
        idempotencyKey: String,
        request: WeighingShedObservationRequestDto,
    ): WeighingObservationResponseDto = WeighingObservationResponseDto()

    override suspend fun submitWeighingScope(
        campaignId: String,
        campaignShedId: String,
        idempotencyKey: String,
        request: WeighingScopeSubmitRequestDto,
    ) = Unit

    override suspend fun reopenWeighingScope(
        campaignId: String,
        campaignShedId: String,
        idempotencyKey: String,
        request: WeighingScopeReopenRequestDto,
    ) = Unit

    override suspend fun closeShedWeighingCampaign(
        campaignId: String,
        campaignShedId: String,
        idempotencyKey: String,
        request: WeighingScopeCloseRequestDto,
    ) = Unit

    override suspend fun closeWeighingCampaign(
        campaignId: String,
        idempotencyKey: String,
        request: WeighingScopeCloseRequestDto,
    ) = Unit

    override suspend fun exportWeighingCampaignCsv(campaignId: String): ByteArray = ByteArray(0)

    override suspend fun verifyAppTask(
        taskId: String,
        idempotencyKey: String,
        request: ReviewTaskRequestDto,
    ): ReviewTaskResponseDto = ReviewTaskResponseDto()

    override suspend fun reworkAppTask(
        taskId: String,
        idempotencyKey: String,
        request: ReviewTaskRequestDto,
    ): ReviewTaskResponseDto = ReviewTaskResponseDto()

    override suspend fun getScanRoster(
        shedId: String,
        taskId: String?,
        cursor: String?,
        limit: Int?,
        partitionLabel: String?,
    ): ScanRosterResponseDto = ScanRosterResponseDto(source = "fake", rows = emptyList())

    override suspend fun rescheduleObligation(
        obligationId: String,
        idempotencyKey: String,
        request: RescheduleObligationRequestDto,
    ): RescheduleObligationResponseDto = RescheduleObligationResponseDto(obligationId = obligationId, idempotentReplay = false)

    override suspend fun getOperatorTimetable(
        centerId: String,
        limit: Int?,
    ): EnrichedPositionListResponseDto = EnrichedPositionListResponseDto()

    override suspend fun getMyCoverage(): MyCoverageResponseDto = MyCoverageResponseDto()

    override suspend fun registerProof(idempotencyKey: String, request: ProofUploadRequestDto): ProofUploadResponseDto =
        ProofUploadResponseDto(
            proof = ProofReferenceDto(
                proofId = "fake-proof-$idempotencyKey",
                proofType = request.proofType,
                subjectType = request.subjectType,
                subjectId = request.subjectId,
                uploadState = "pending",
            ),
            uploadUrl = "https://fake.local/proofs/upload",
            uploadMethod = "PUT",
        )

    override suspend fun listUploadedProofs(
        scopeType: String,
        scopeId: String,
        clientTaskKey: String?,
        fieldKey: String?,
        limit: Int?,
    ): UploadedProofListResponseDto = UploadedProofListResponseDto()

    override suspend fun getProofDownloadUrl(proofId: String): String =
        "https://fake.local/proofs/$proofId/download"

    // Test/dev scaffolding — does not touch the filesystem or network; a proof is simply marked
    // completed under the id `registerProof` handed back, so previews/unit tests that don't care
    // about the real byte-streaming path (see OkHttpProofBlobUploader) compile and pass.
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
    ): ProofCompleteResponseDto = ProofCompleteResponseDto(
        proof = ProofArtifactDto(proofId = proofId, uploadState = "completed", mimeType = mimeType, durationMs = durationMs),
    )

    override suspend fun deleteProof(proofId: String) = Unit

    override suspend fun getVaccinationGaps(
        parkId: String?,
        limit: Int?,
        cursor: String?,
    ): VaccinationGapsResponseDto = VaccinationGapsResponseDto()

    override suspend fun getVaccinationCoverage(
        parkId: String?,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
    ): VaccinationCoverageResponseDto = VaccinationCoverageResponseDto()

    override suspend fun getAppConfig(eTag: String?): AppConfigResponseDto? = AppConfigResponseDto()

    override suspend fun listVerificationQueue(
        category: String?,
        status: String?,
        businessDate: String?,
        missed: Boolean?,
        parkId: String?,
        shedId: String?,
        cursor: String?,
        limit: Int?,
    ): VerificationQueueResponseDto = VerificationQueueResponseDto()

    override suspend fun listVerificationActionQueue(
        category: String?,
        parkId: String?,
        shedId: String?,
        cursor: String?,
        limit: Int?,
    ): VerificationQueueResponseDto = VerificationQueueResponseDto()

    override suspend fun submitVerificationVerdict(
        itemId: String,
        idempotencyKey: String,
        request: VerificationVerdictRequestDto,
    ): VerificationVerdictResponseDto = VerificationVerdictResponseDto()

    override suspend fun correctWeighingObservationWeight(
        observationId: String,
        idempotencyKey: String,
        request: WeighingWeightCorrectionRequestDto,
    ): WeighingWeightCorrectionResponseDto = WeighingWeightCorrectionResponseDto()

    override suspend fun closeVerificationItem(
        itemId: String,
        idempotencyKey: String,
        request: VerificationCloseRequestDto,
    ): VerificationVerdictResponseDto = VerificationVerdictResponseDto()

    override suspend fun closeVerificationSubmission(
        submissionId: String,
        idempotencyKey: String,
    ): VerificationCloseSubmissionResponseDto = VerificationCloseSubmissionResponseDto()

    override suspend fun closeVaccinationBatch(
        batchId: String,
        idempotencyKey: String,
    ): VerificationCloseSubmissionResponseDto = VerificationCloseSubmissionResponseDto()

    override suspend fun recordVerificationReviewEvents(
        request: VerificationReviewEventBatchRequestDto,
    ): VerificationReviewEventBatchResponseDto = VerificationReviewEventBatchResponseDto()

    override suspend fun getHerdRegisterSummary(
        lifecycleStatus: String?,
        parkId: String?,
        breed: String?,
        sex: String?,
    ): HerdRegisterSummaryResponseDto = HerdRegisterSummaryResponseDto()

    override suspend fun getCountsBreakdown(
        parkId: String?,
        shedId: String?,
        managementStage: String?,
        breed: String?,
        sex: String?,
        lifecycleStatus: String?,
        limit: Int?,
        offset: Int?,
    ): CountsBreakdownResponseDto = CountsBreakdownResponseDto()

    override suspend fun getCountsShiftingDestinations(): CountsShiftingDestinationsResponseDto =
        CountsShiftingDestinationsResponseDto()

    override suspend fun getAppCountsBreeds(): CountsBreedsResponseDto = CountsBreedsResponseDto()

    override suspend fun listCountsShiftingPendingExecution(
        date: String?,
        status: String?,
        pageSize: Int?,
        cursor: String?,
    ): CountsShiftingPendingExecutionResponseDto = CountsShiftingPendingExecutionResponseDto()

    override suspend fun listCountsTemporaryTaggedGoats(
        pageSize: Int?,
        cursor: String?,
        parkId: String?,
        shedId: String?,
    ): TemporaryTaggedGoatsResponseDto = TemporaryTaggedGoatsResponseDto()

    override suspend fun promoteCountsIdentifier(
        goatId: String,
        idempotencyKey: String,
        permanentIdentifier: String,
        rowVersion: Int,
        secondaryIdentifier: String?,
    ): CountsPromoteIdentifierResponseDto = CountsPromoteIdentifierResponseDto(goatId = goatId)

    override suspend fun completeCountsShiftingEvent(
        shiftingEventId: String,
        idempotencyKey: String,
        destinationTag: String?,
        proofRef: String,
        feedPackingProofRef: String?,
        feedGivenProofRef: String?,
        feedConfigFingerprint: String?,
    ): CountsShiftingExecutionResponseDto = CountsShiftingExecutionResponseDto(
        shiftingEventId = shiftingEventId,
        eventStatus = "pending_verification",
    )

    override suspend fun cancelCountsShiftingEvent(
        shiftingEventId: String,
        idempotencyKey: String,
        reason: String,
    ): CountsShiftingExecutionResponseDto = CountsShiftingExecutionResponseDto(
        shiftingEventId = shiftingEventId,
        eventStatus = "canceled",
    )

    override suspend fun listCountsPenReconciliationCards(
        status: String?,
        pageSize: Int?,
        cursor: String?,
    ): CountsPenReconciliationListResponseDto = CountsPenReconciliationListResponseDto()

    override suspend fun completeCountsPenReconciliationCard(
        cardId: String,
        idempotencyKey: String,
        proofRef: String,
    ): CountsPenReconciliationCompleteResponseDto = CountsPenReconciliationCompleteResponseDto(
        cardId = cardId,
        status = "pending_verification",
    )

    override suspend fun completeFeedDirectionSession(
        idempotencyKey: String,
        request: FeedDirectionCompleteRequestDto,
    ): FeedDirectionCompleteResponseDto =
        FeedDirectionCompleteResponseDto(completionId = "fake-completion", status = "completed", applied = true)

    override suspend fun completeFeedDistribution(
        idempotencyKey: String,
        request: FeedDistributionCompleteRequestDto,
    ): FeedDistributionCompleteResponseDto =
        FeedDistributionCompleteResponseDto(
            completionId = "fake-distribution-completion",
            status = "pending_verification",
            newlyPending = true,
        )

    override suspend fun completeFeedPacking(
        idempotencyKey: String,
        request: FeedPackingCompleteRequestDto,
    ): FeedPackingCompleteResponseDto =
        FeedPackingCompleteResponseDto(
            completionId = "fake-packing-completion",
            status = "pending_verification",
            newlyPending = true,
        )

    override suspend fun submitMilkPreparation(
        idempotencyKey: String,
        request: MilkPreparationSubmissionRequestDto,
    ): MilkPreparationSubmissionResponseDto = MilkPreparationSubmissionResponseDto(
        completionId = "fake-milk-preparation", status = "pending_verification", attemptNo = 1, rowVersion = 1,
    )

    override suspend fun getMilkPreparation(preparationDate: String?, parkId: String?, limit: Int, offset: Int): MilkPreparationPageDto =
        MilkPreparationPageDto()

    override suspend fun getMilkFeedingTasks(feedingDate: String, parkId: String?, sessionNo: Int?, limit: Int, offset: Int): MilkFeedingPageDto = MilkFeedingPageDto(feedingDate = feedingDate)
    override suspend fun submitMilkFeedingTask(taskId: String, idempotencyKey: String, request: MilkFeedingSubmitRequestDto): MilkFeedingSubmitResponseDto = MilkFeedingSubmitResponseDto(completionId = taskId, status = "pending_verification", attemptNo = 1, rowVersion = 1)

    override suspend fun getFeedTransportTasks(
        businessDate: String,
        parkId: String?,
        shedId: String?,
        status: String?,
        cursor: String?,
        limit: Int?,
    ): FeedTransportTaskPageDto = FeedTransportTaskPageDto()
    override suspend fun submitFeedTransport(taskId: String, idempotencyKey: String, request: FeedTransportSubmitRequestDto): FeedTransportSubmitResponseDto = FeedTransportSubmitResponseDto("fake-attempt", "verification_due", 1, true)

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
    ): FeedDirectionPreviewPageDto = FeedDirectionPreviewPageDto(targetDate = targetDate)

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
    ): FeedPackingWorklistPageDto = FeedPackingWorklistPageDto(targetDate = targetDate)

    override suspend fun getFeedWastageWorklist(
        parkId: String,
        targetDate: String,
        shedId: String?,
        partitionLabel: String?,
        status: String?,
        limit: Int?,
        offset: Int?,
    ): FeedWastageWorklistPageDto = FeedWastageWorklistPageDto(targetDate = targetDate)

    override suspend fun completeFeedWastage(
        idempotencyKey: String,
        request: FeedWastageCompleteRequestDto,
    ): FeedWastageCompleteResponseDto =
        FeedWastageCompleteResponseDto(
            completionId = "fake-wastage-completion",
            status = "pending_verification",
            newlyPending = true,
        )

    override suspend fun recordFeedWastageMeasurement(
        completionId: String,
        idempotencyKey: String,
        request: FeedWastageMeasurementRequestDto,
    ): FeedWastageMeasurementResponseDto = FeedWastageMeasurementResponseDto()

    // Nothing recorded by anyone else: the fake keeps the single-phone behaviour tests assert.
    override suspend fun getFeedDistributionCaptures(
        parkId: String?,
        shedId: String,
        partitionLabel: String?,
        sessionNo: Int,
        targetDate: String,
        workflow: String,
    ): FeedDistributionCapturesDto = FeedDistributionCapturesDto()

    override suspend fun recordCountsShiftingEvent(
        idempotencyKey: String,
        request: CountsShiftingEventRequestDto,
    ): CountsShiftingEventResponseDto =
        CountsShiftingEventResponseDto(shiftingEventId = "fake-shifting-$idempotencyKey")

    override suspend fun recordCountsBirthEvent(
        idempotencyKey: String,
        request: CountsBirthEventRequestDto,
    ): CountsApprovalSubmitResponseDto = CountsApprovalSubmitResponseDto(
        approvalRequestId = "fake-birth-approval-$idempotencyKey",
        requestType = "birth",
        status = "pending",
        raisedAt = "2026-07-28T00:00:00Z",
        idempotentReplay = false,
    )

    override suspend fun recordCountsDeathEvent(
        idempotencyKey: String,
        request: CountsDeathEventRequestDto,
    ): CountsApprovalSubmitResponseDto = CountsApprovalSubmitResponseDto(
        approvalRequestId = "fake-death-approval-$idempotencyKey",
        requestType = "death",
        status = "pending",
        raisedAt = "2026-07-28T00:00:00Z",
        idempotentReplay = false,
    )

    override suspend fun searchGoats(
        q: String?,
        parkId: String?,
        locationId: String?,
        status: String?,
        limit: Int?,
        cursor: String?,
    ): GoatSearchResponseDto = GoatSearchResponseDto()

    override suspend fun listCountsApprovals(
        status: String?,
        pageSize: Int?,
        cursor: String?,
    ): CountsApprovalListResponseDto = CountsApprovalListResponseDto()

    override suspend fun approveCountsApproval(
        requestId: String,
        idempotencyKey: String,
        request: CountsApprovalDecisionRequestDto,
    ): CountsApprovalDecisionResponseDto =
        CountsApprovalDecisionResponseDto(approvalRequestId = requestId, status = "approved")

    override suspend fun rejectCountsApproval(
        requestId: String,
        idempotencyKey: String,
        request: CountsApprovalDecisionRequestDto,
    ): CountsApprovalDecisionResponseDto =
        CountsApprovalDecisionResponseDto(approvalRequestId = requestId, status = "rejected")

    override suspend fun listWorkflows(
        module: String,
        date: String?,
        filter: String?,
        pageSize: Int?,
        cursor: String?,
    ): WorkflowListResponseDto = WorkflowListResponseDto()

    override suspend fun getWorkflow(
        workflowId: String,
        lens: String?,
        date: String?,
    ): WorkflowDetailResponseDto = WorkflowDetailResponseDto(workflowId = workflowId)

    override suspend fun answerWorkflowAction(
        workflowId: String,
        actionId: String,
        idempotencyKey: String,
        request: WorkflowActionAnswerRequestDto,
    ): WorkflowActionWriteResponseDto =
        WorkflowActionWriteResponseDto(workflowId = workflowId, actionId = actionId, status = "completed")

    override suspend fun completeWorkflowAction(
        workflowId: String,
        actionId: String,
        idempotencyKey: String,
        request: WorkflowActionCompleteRequestDto,
    ): WorkflowActionWriteResponseDto =
        WorkflowActionWriteResponseDto(workflowId = workflowId, actionId = actionId, status = "completed")

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
    ): HealthWorkItemPageDto = HealthWorkItemPageDto()

    override suspend fun getHealthWorkItem(healthSessionId: String): HealthWorkItemDetailDto =
        HealthWorkItemDetailDto(healthSessionId = healthSessionId)

    override suspend fun submitHealthObservation(
        idempotencyKey: String,
        request: SubmitHealthObservationRequestDto,
    ): HealthDiagnosisProposalResponseDto = HealthDiagnosisProposalResponseDto()

    override suspend fun listHealthObservations(
        status: String?,
        goatId: String?,
        cursor: String?,
        limit: Int?,
    ): HealthDiagnosisQueuePageDto = HealthDiagnosisQueuePageDto()

    override suspend fun getHealthObservation(diagnosisRunId: String): HealthDiagnosisRunDto =
        HealthDiagnosisRunDto(diagnosisRunId = diagnosisRunId)

    override suspend fun listDeathCauses(): DeathCauseCatalogDto = DeathCauseCatalogDto()

    override suspend fun confirmHealthDiagnosis(
        diagnosisRunId: String,
        idempotencyKey: String,
        request: ConfirmHealthDiagnosisRequestDto,
    ): ConfirmHealthDiagnosisResponseDto =
        ConfirmHealthDiagnosisResponseDto(diagnosisRunId = diagnosisRunId)

    override suspend fun openHealthCase(
        idempotencyKey: String,
        request: HealthOpenCaseRequestDto,
    ): HealthOpenCaseResponseDto = HealthOpenCaseResponseDto(
        caseId = "case-${request.goatId}",
        firstSessionId = "health-${request.goatId}",
    )

    override suspend fun completeHealthWorkItem(
        healthSessionId: String,
        idempotencyKey: String,
        request: HealthCompleteRequestDto,
    ): HealthCompleteResponseDto = HealthCompleteResponseDto(
        healthSessionId = healthSessionId,
        status = "completed",
    )

    override suspend fun closeHealthCase(
        healthCaseId: String,
        idempotencyKey: String,
        request: HealthCloseCaseRequestDto,
    ): HealthCloseCaseResponseDto = HealthCloseCaseResponseDto(
        caseId = healthCaseId,
        status = request.outcome,
    )
	    override suspend fun getPcCareWorklist(
	        category: String,
	        date: String,
	        limit: Int?,
	        cursor: String?,
	    ): PcCareTaskPageDto = PcCareTaskPageDto()

    override suspend fun getPcCareTasks(
        date: String,
	        parkId: String?,
	        category: String?,
	        limit: Int?,
	        cursor: String?,
	    ): PcCareTaskPageDto = PcCareTaskPageDto()

    override suspend fun getPcCareTask(taskId: String): PcCareTaskDto = PcCareTaskDto(
        taskId = taskId,
        category = "deworming",
        parkId = "park-1",
        parkLabel = "CPT",
        shedId = "shed-1",
        shedLabel = "Castro",
        plannedBusinessDate = "2026-08-21",
        dueBusinessDate = "2026-08-21",
        workState = "scheduled",
        status = "open",
        rowVersion = 1,
    )

    override suspend fun getPcCareTaskCaptures(
        taskId: String,
        cursor: String?,
        limit: Int?,
    ): PcCareCapturesDto = PcCareCapturesDto()

    override suspend fun getPcCareTaskRoster(
        taskId: String,
        cursor: String?,
        limit: Int?,
    ): PcCareTaskRosterDto = PcCareTaskRosterDto()

    override suspend fun scanPcCareAnimal(
        taskId: String,
        idempotencyKey: String,
        request: PcCareScanRequestDto,
    ): PcCareScanResponseDto = PcCareScanResponseDto(animalRowId = "row-${request.scannedIdentifier}")

    override suspend fun registerPcCareSlotProof(
        taskId: String,
        animalRowId: String,
        slot: String,
        idempotencyKey: String,
        request: PcCareSlotProofRequestDto,
    ) = Unit

    override suspend fun registerPcCareTaskProof(
        taskId: String,
        slot: String,
        idempotencyKey: String,
        request: PcCareSlotProofRequestDto,
    ) = Unit

    override suspend fun submitPcCareTask(
        taskId: String,
        idempotencyKey: String,
    ): PcCareSubmitResponseDto = PcCareSubmitResponseDto(
        taskId = taskId,
        status = "pending_verification",
        rowVersion = 2,
    )

    override suspend fun getPcCarePlannerCatalog(): PcCarePlannerCatalogDto = PcCarePlannerCatalogDto()

    override suspend fun getPcCarePlannerParkSheds(
        parkId: String,
        category: String,
        date: String,
        cursor: String?,
        limit: Int?,
    ): PcCarePlannerShedsDto = PcCarePlannerShedsDto()

    override suspend fun createPcCareTask(
        idempotencyKey: String,
        request: PcCareCreateTaskRequestDto,
    ): PcCareTaskDto = PcCareTaskDto(
        taskId = "pc-care-task-1",
        category = request.category,
        parkId = request.parkId,
        parkLabel = request.parkId,
        shedId = request.shedId,
        shedLabel = request.shedId,
        partitionLabel = request.partitionLabel,
        plannedBusinessDate = request.plannedBusinessDate,
        dueBusinessDate = request.plannedBusinessDate,
        workState = "scheduled",
        status = "open",
        rowVersion = 1,
        assigneeUserIds = request.assigneeUserIds,
    )

    override suspend fun createPcCareRound(
        idempotencyKey: String,
        request: PcCareCreateRoundRequestDto,
    ): PcCareRoundDto = PcCareRoundDto(
        roundId = "pc-care-round-1",
        category = request.category,
        categoryLabel = request.category,
        parkId = request.parkId,
        parkName = request.parkId,
        plannedBusinessDate = request.plannedBusinessDate,
        status = "open",
        penCount = request.pens.size,
    )

    override suspend fun getPcCareRoundCards(
        date: String?,
        category: String?,
        parkId: String?,
        filter: String?,
        cursor: String?,
        limit: Int?,
    ): PcCareRoundCardPageDto = PcCareRoundCardPageDto()

    override suspend fun getPcCareRound(roundId: String): PcCareRoundDto = PcCareRoundDto()

    override suspend fun getPcCareRemovalPens(taskId: String): PcCareRemovalPenListDto =
        PcCareRemovalPenListDto()

    override suspend fun putPcCareRemovalPenProof(
        taskId: String,
        slot: String,
        idempotencyKey: String,
        request: PcCareRemovalPenProofRequestDto,
    ) = Unit

    override suspend fun closePcCareTask(taskId: String, request: PcCareCloseRequestDto) = Unit

    override suspend fun reopenPcCareTask(taskId: String) = Unit

    override suspend fun closePcCareRound(roundId: String, request: PcCareCloseRequestDto) = Unit

    override suspend fun recordPcCareStockVerdict(
        taskId: String,
        request: PcCareStockVerdictRequestDto,
    ): PcCareTaskDto = PcCareTaskDto(
        taskId = taskId,
        category = "inventory_vaccine",
        parkId = "park-1",
        parkLabel = "CPT",
        shedId = "",
        shedLabel = "",
        plannedBusinessDate = "2026-09-02",
        dueBusinessDate = "2026-09-02",
        workState = if (request.verdict == "approve") "completed" else "scheduled",
        status = if (request.verdict == "approve") "completed" else "rework",
        rowVersion = 2,
    )

    override suspend fun getToxinTasks(
        filter: String?,
        limit: Int?,
        cursor: String?,
    ): ToxinTaskPageDto = ToxinTaskPageDto()

    override suspend fun getToxinTask(taskId: String): ToxinTaskDetailDto = ToxinTaskDetailDto(
        taskId = taskId,
        status = "in_progress",
        statusChip = "Test in progress",
        stepsTotal = 6,
        steps = listOf(
            ToxinStepDto(stepNo = 1, kind = "video", title = "Weigh the sample", instruction = "", state = "available"),
        ),
    )

    override suspend fun completeToxinStep(
        taskId: String,
        stepNo: Int,
        idempotencyKey: String,
        request: ToxinStepCompleteRequestDto,
    ): ToxinTaskDetailDto = getToxinTask(taskId)

    override suspend fun submitToxinReading(
        taskId: String,
        idempotencyKey: String,
        request: ToxinSubmitRequestDto,
    ): ToxinTaskDetailDto = getToxinTask(taskId).copy(status = "pending_review", statusChip = "Sent for review")

    override suspend fun getProcurementVendors(
        search: String?,
        status: String?,
        limit: Int?,
        offset: Int?,
    ): VendorPageDto = VendorPageDto(vendors = listOf(fakeVendor()), total = 1, limit = limit ?: 20, offset = offset ?: 0)

    override suspend fun getProcurementVendor(vendorId: String): VendorDto = fakeVendor().copy(vendorId = vendorId)

    override suspend fun getProcurementVendorCatalog(): VendorCatalogDto = VendorCatalogDto(
        recordTypes = listOf(VendorCatalogEntryDto("Feed Agent", "Feed Agent"), VendorCatalogEntryDto("Sheep Agent", "Sheep Agent")),
        states = listOf(VendorCatalogEntryDto("KA", "Karnataka"), VendorCatalogEntryDto("TN", "Tamil Nadu")),
        statuses = listOf(VendorCatalogEntryDto("active", "Active"), VendorCatalogEntryDto("negotiating", "Negotiating")),
        capacityUnits = listOf(VendorCatalogEntryDto("kg", "kg"), VendorCatalogEntryDto("animals", "animals")),
        supplyFrequencies = listOf(VendorCatalogEntryDto("per_week", "Every week"), VendorCatalogEntryDto("one_time", "One time")),
    )

    override suspend fun createProcurementVendor(request: VendorWriteDto): VendorDto =
        fakeVendor().copy(vendorId = "vendor-new", businessName = request.businessName, displayName = request.businessName)

    override suspend fun getFeedPurchases(
        farm: String?,
        delivery: String?,
        limit: Int?,
        offset: Int?,
    ): FeedPurchasePageDto = FeedPurchasePageDto(purchases = listOf(fakeFeedPurchase()), total = 1, quantityKg = 1000.0, spendRupees = 23000.0, limit = limit ?: 20, offset = offset ?: 0)

    override suspend fun getFeedPurchaseOptions(): FeedPurchaseOptionsDto = FeedPurchaseOptionsDto(
        farms = listOf("CBE", "CPT"),
        feedItems = listOf(FeedItemOptionDto("concentrate", "Concentrate")),
        paymentStatuses = listOf("Paid", "Pending"),
        deliveryStatuses = listOf(DeliveryStatusOptionDto("purchased", "On the road"), DeliveryStatusOptionDto("reached", "Reached")),
        vendors = listOf("QA Vendor"),
    )

    override suspend fun createFeedPurchase(idempotencyKey: String, request: FeedPurchaseWriteDto): FeedPurchaseDto =
        fakeFeedPurchase().copy(feedPurchaseId = "purchase-new", feedItem = request.feedItem, quantityKg = request.quantityKg)

    override suspend fun getSalesDeals(farm: String?, limit: Int?, offset: Int?): SalesDealPageDto =
        SalesDealPageDto(deals = listOf(fakeSalesDeal()), total = 1, limit = limit ?: 20, offset = offset ?: 0)

    override suspend fun getSalesOptions(): SalesOptionsDto = SalesOptionsDto(
        farms = listOf("CBE", "CPT"),
        productTypes = listOf("Sheep", "Goat", "Manure"),
        breeds = mapOf("Sheep" to listOf("Anantapur"), "Goat" to listOf("Malai", "Sirohi"), "Manure" to listOf("Manure")),
        statuses = listOf(SalesStatusOptionDto("Deal Closed", "Deal Closed", "ok"), SalesStatusOptionDto("Advance Paid", "Advance Paid", "warn")),
        defaultStatus = "Deal Closed",
    )

    override suspend fun getVendorOptions(): VendorOptionsDto =
        VendorOptionsDto(vendors = listOf(VendorOptionDto("vendor-1", "Kumar Traders", "Sheep Agent", "Hosur", "TN")))

    override suspend fun createSalesDeal(idempotencyKey: String, request: SalesDealWriteDto): SalesDealDto =
        fakeSalesDeal().copy(dealId = "deal-new", buyerName = request.buyerName, salesValue = request.salesValue, paymentBalance = request.salesValue)

    override suspend fun createFeedPurchasePayment(purchaseId: String, idempotencyKey: String, request: FeedPurchasePaymentWriteDto): FeedPurchaseDto =
        fakeFeedPurchase().copy(
            feedPurchaseId = purchaseId,
            paymentReleased = request.amountRupees,
            paymentBalance = (23000.0 - request.amountRupees).coerceAtLeast(0.0),
            payments = listOf(FeedPurchasePaymentDto("fp-payment-1", request.paidOn, request.amountRupees, request.note)),
        )

    override suspend fun setFeedPurchasePaymentStatus(purchaseId: String, request: FeedPurchaseStatusWriteDto): FeedPurchaseDto =
        fakeFeedPurchase().copy(feedPurchaseId = purchaseId, paymentStatus = request.paymentStatus)

    override suspend fun editFeedPurchase(purchaseId: String, request: FeedPurchaseEditDto): FeedPurchaseDto =
        fakeFeedPurchase().copy(
            feedPurchaseId = purchaseId, purchaseDate = request.purchaseDate,
            quantityKg = request.quantityKg, vendor = request.vendor, totalCost = request.totalCost,
        )

    override suspend fun recordFeedPurchaseDelivery(purchaseId: String, request: FeedPurchaseDeliveryWriteDto): FeedPurchaseDto =
        fakeFeedPurchase().copy(
            feedPurchaseId = purchaseId, deliveryStatus = "reached",
            reachedOn = request.reachedOn, reachedWeightKg = request.reachedWeightKg,
            stockKg = request.reachedWeightKg ?: 1000.0,
        )


    override suspend fun getSaleLocations(): SaleLocationsDto = SaleLocationsDto(
        parks = listOf(SaleLocationParkDto("park-1", "CBE")),
        locations = listOf(SaleLocationEntryDto("shed-1", "park-1", "1", "Castro 1")),
    )

    override suspend fun getSaleCandidates(parkId: String, shedId: String?, partitionLabels: List<String>, query: String?, limit: Int?, cursor: String?): SaleCandidatePageDto =
        SaleCandidatePageDto(candidates = listOf(fakeSaleCandidate()))

    override suspend fun getSaleAllocation(salesDealId: String): SaleAllocationDto = SaleAllocationDto(salesDealId = salesDealId)

    override suspend fun previewSaleAllocation(request: SaleAllocationRequestDto): SalePreviewDto =
        SalePreviewDto(salesDealId = request.salesDealId, sellable = request.goatIds.size, shedGroups = listOf(SaleShedGroupDto(parkName = "CBE", operationalLocationDisplay = "Castro 1", animals = request.goatIds.size, tagNumbers = listOf("155"))))

    override suspend fun confirmSaleAllocation(idempotencyKey: String, request: SaleAllocationRequestDto): SaleAllocationDto =
        SaleAllocationDto(salesDealId = request.salesDealId, allocated = request.goatIds.size)

    override suspend fun createSalesDealPayment(dealId: String, idempotencyKey: String, request: SalesDealPaymentWriteDto): SalesDealDto =
        fakeSalesDeal().copy(
            dealId = dealId,
            paymentReceived = 50000.0 + request.amountRupees,
            paymentBalance = (150000.0 - 50000.0 - request.amountRupees).coerceAtLeast(0.0),
            payments = listOf(SalesDealPaymentDto("payment-1", request.receivedOn, request.amountRupees, request.note)),
        )

    override suspend fun updateSalesDealPayment(dealId: String, paymentId: String, idempotencyKey: String, request: SalesDealPaymentWriteDto): SalesDealDto =
        fakeSalesDeal().copy(
            dealId = dealId,
            payments = listOf(SalesDealPaymentDto(paymentId, request.receivedOn, request.amountRupees, request.note)),
        )

    override suspend fun deleteSalesDealPayment(dealId: String, paymentId: String, idempotencyKey: String): SalesDealDto =
        fakeSalesDeal().copy(dealId = dealId, payments = emptyList(), paymentReceived = 0.0, paymentBalance = 150000.0)

    override suspend fun setSalesDealStatus(dealId: String, idempotencyKey: String, request: SalesDealStatusWriteDto): SalesDealDto =
        fakeSalesDeal().copy(dealId = dealId, status = request.status)

    override suspend fun getSalesBuyerLeads(limit: Int?, offset: Int?): SalesBuyerLeadPageDto = SalesBuyerLeadPageDto(
        leads = listOf(SalesBuyerLeadDto("lead-1", "2026-09-01", "CBE", "Kumar Traders", "Hosur", "Goat", "Malai", "Interested")),
        total = 1,
        statusOptions = listOf("Interested", "Not Interested", "Call Later"),
    )

    override suspend fun createSalesBuyerLead(idempotencyKey: String, request: SalesBuyerLeadWriteDto): SalesBuyerLeadDto =
        SalesBuyerLeadDto("lead-new", request.recordedDate, request.farm, request.buyerName, request.buyerPlace, request.animalType, request.breed, request.callStatus)

    override suspend fun setSalesBuyerLeadStatus(leadId: String, idempotencyKey: String, request: SalesLeadStatusWriteDto): SalesBuyerLeadDto =
        SalesBuyerLeadDto(leadId, buyerName = "Kumar Traders", callStatus = request.callStatus)

    override suspend fun getSalesFpoLeads(limit: Int?, offset: Int?): SalesFpoLeadPageDto = SalesFpoLeadPageDto(
        leads = listOf(SalesFpoLeadDto("fpo-1", "Erode Farmer Group", "Maize", "Erode", "Bhavani", "TN", "Interested")),
        total = 1,
        statusOptions = listOf("Interested", "Not Interested", "Call Later"),
    )

    override suspend fun createSalesFpoLead(idempotencyKey: String, request: SalesFpoLeadWriteDto): SalesFpoLeadDto =
        SalesFpoLeadDto("fpo-new", request.fpoName, request.crops, request.district, request.taluk, request.state, request.callStatus)

    override suspend fun setSalesFpoLeadStatus(leadId: String, idempotencyKey: String, request: SalesLeadStatusWriteDto): SalesFpoLeadDto =
        SalesFpoLeadDto(leadId, fpoName = "Erode Farmer Group", callStatus = request.callStatus)

    override suspend fun createSalesMarketBenchmark(idempotencyKey: String, request: SalesBenchmarkWriteDto): SalesRecordedDto = SalesRecordedDto(recorded = true)

    override suspend fun createSalesSoldTags(idempotencyKey: String, request: SalesSoldTagsWriteDto): SalesSoldTagsResultDto = SalesSoldTagsResultDto(recorded = request.rows.size)

    override suspend fun createSalesWeightCheck(idempotencyKey: String, request: SalesWeightCheckWriteDto): SalesRecordedDto = SalesRecordedDto(recorded = true)

    private fun fakeSalesDeal(): SalesDealDto = SalesDealDto(
        dealId = "deal-1", saleDate = "2026-09-01", farm = "CBE", buyerName = "Kumar Traders", buyerPlace = "Hosur",
        buyerVendorId = "vendor-1", productType = "Goat", breed = "Malai", animalCount = 12.0, totalWeightKg = 300.0,
        salesValue = 150000.0, paymentReceived = 50000.0, paymentBalance = 100000.0, status = "Advance Paid",
    )

    private fun fakeSaleCandidate(): SaleCandidateDto = SaleCandidateDto(
        goatId = "goat-1", displayId = "G-1", tagNumber = "155", parkId = "park-1", parkName = "CBE",
        shedId = "shed-1", shedName = "Castro", partitionLabel = "1", operationalLocationDisplay = "Castro 1",
        breed = "Malai", sex = "male",
    )

    private fun fakeVendor(): VendorDto = VendorDto(
        vendorId = "vendor-1", recordType = "Feed Agent", businessName = "Kumar Traders",
        displayName = "Kumar Traders - Kumar", contactPersonName = "Kumar", phoneNumber = "9800000000",
        status = "active", statusLabel = "Active", state = "KA", city = "Mysuru", locationDisplay = "Mysuru, KA",
        capacityQuantity = "5000", capacityUnit = "kg", supplyFrequency = "per_2_weeks",
        capacityDisplay = "5,000 kg · Every 2 weeks",
    )

    private fun fakeFeedPurchase(): FeedPurchaseDto = FeedPurchaseDto(
        feedPurchaseId = "purchase-1", purchaseDate = "2026-09-01", farm = "CBE", feedItem = "Concentrate",
        batchNo = 12, quantityKg = 1000.0, totalCost = 23000.0, perKgCost = 23.0, vendor = "QA Vendor",
        paymentStatus = "Pending", paymentBalance = 23000.0, deliveryStatus = "purchased", entrySource = "app",
    )
    override suspend fun getLeadershipTasks(
        filter: String?,
        limit: Int?,
        cursor: String?,
    ): LeadershipTaskPageDto = LeadershipTaskPageDto(title = "Tasks")

    override suspend fun getLeadershipTaskAssignees(): LeadershipAssigneeListDto = LeadershipAssigneeListDto()

    override suspend fun getLeadershipTask(taskId: String): LeadershipTaskDetailDto =
        LeadershipTaskDetailDto(task = sg.mesha.goatos.core.network.dto.LeadershipTaskDto(taskId = taskId))

    override suspend fun raiseLeadershipTask(
        idempotencyKey: String,
        request: LeadershipTaskRaiseRequestDto,
    ): LeadershipTaskDetailDto = LeadershipTaskDetailDto(
        task = sg.mesha.goatos.core.network.dto.LeadershipTaskDto(taskId = idempotencyKey, title = request.title, body = request.body),
    )

    override suspend fun editLeadershipTask(
        taskId: String,
        idempotencyKey: String,
        request: LeadershipTaskEditRequestDto,
    ): LeadershipTaskDetailDto = getLeadershipTask(taskId)

    override suspend fun changeLeadershipTaskStatus(
        taskId: String,
        idempotencyKey: String,
        request: LeadershipTaskStatusRequestDto,
    ): LeadershipTaskDetailDto = getLeadershipTask(taskId)

    override suspend fun markLeadershipTaskSeen(taskId: String): LeadershipTaskDetailDto = getLeadershipTask(taskId)

    override suspend fun setLeadershipTaskComment(
        taskId: String,
        idempotencyKey: String,
        request: LeadershipTaskCommentRequestDto,
    ): LeadershipTaskDetailDto = getLeadershipTask(taskId)

    override suspend fun getLeadershipTaskAttachmentDownloadUrl(taskId: String, proofId: String): String =
        getProofDownloadUrl(proofId)

    override suspend fun recordClockIn(
        idempotencyKey: String,
        request: ClockPunchRequestDto,
    ): ClockPunchResponseDto = ClockPunchResponseDto(entry = fakeClockEntry(status = "open"))

    override suspend fun recordClockOut(
        idempotencyKey: String,
        request: ClockPunchRequestDto,
    ): ClockPunchResponseDto = ClockPunchResponseDto(entry = fakeClockEntry(status = "closed"))

    override suspend fun getClockStatus(): ClockStatusResponseDto = ClockStatusResponseDto(
        businessDate = "2026-08-28",
        state = "not_clocked_in",
    )

    override suspend fun listClockPresence(
        date: String?,
        parkId: String?,
        designation: String?,
        bucket: String?,
        q: String?,
        limit: Int?,
        cursor: String?,
    ): ClockPresenceResponseDto = ClockPresenceResponseDto(businessDate = date ?: "2026-08-28")

    override suspend fun getClockPresencePerson(
        workforceMemberId: String,
        date: String?,
    ): ClockPersonDayResponseDto = ClockPersonDayResponseDto(
        personName = "Fake Person",
        businessDate = date ?: "2026-08-28",
    )

    private fun fakeClockEntry(status: String): ClockEntryDto = ClockEntryDto(
        clockEntryId = "fake-entry",
        workforceMemberId = "fake-member",
        personName = "Fake Person",
        businessDate = "2026-08-28",
        status = status,
        clockInAt = "2026-08-28T08:12:00+05:30",
        clockInLabel = "08:12",
    )
}

/**
 * DTO -> domain. The backend already computed the chrome, the drawer modules, and each
 * module's bar; the client only parses them (TRD §14 dumb-renderer). Labels pass through
 * verbatim — they are localized backend-side in `bootstrap_copy.go`.
 */
fun BootstrapDto.toNavState(): NavState {
    val enabledModules = modules
        .map { module ->
        NavModule(
            key = module.key,
            label = module.label,
            href = module.href,
            status = NavModuleStatus.from(module.status),
            navItems = module.navItems
                .map { it.toNavItem() },
            badgeCount = module.badgeCount.coerceAtLeast(0),
        )
    }
    val enabledItems = visibleNavigation
        .map { it.toNavItem() }
        .ifEmpty { enabledModules.firstOrNull()?.navItems.orEmpty() }
    return NavState(
        chrome = if (navChrome.equals("expanded", ignoreCase = true)) NavChrome.EXPANDED else NavChrome.MINIMAL,
        items = enabledItems,
        modules = enabledModules,
        featureFlags = featureFlags,
    )
}

private fun NavItemDto.toNavItem(): NavItem = NavItem(key = key, label = label, href = href)
