package sg.mesha.goatos.core.network

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import sg.mesha.goatos.core.model.nav.NavChrome
import sg.mesha.goatos.core.model.nav.NavItem
import sg.mesha.goatos.core.model.nav.NavModule
import sg.mesha.goatos.core.model.nav.NavModuleStatus
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.dto.CalendarEventListResponseDto
import sg.mesha.goatos.core.network.dto.ControlTowerResponseDto
import sg.mesha.goatos.core.network.dto.FeedDirectionCompleteRequestDto
import sg.mesha.goatos.core.network.dto.FeedDirectionCompleteResponseDto
import sg.mesha.goatos.core.network.dto.FeedDistributionCompleteRequestDto
import sg.mesha.goatos.core.network.dto.FeedDistributionCompleteResponseDto
import sg.mesha.goatos.core.network.dto.FeedPackingCompleteRequestDto
import sg.mesha.goatos.core.network.dto.FeedPackingCompleteResponseDto
import sg.mesha.goatos.core.network.dto.FeedDirectionPreviewPageDto
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
import sg.mesha.goatos.core.network.dto.CountsApprovalSubmitResponseDto
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
import sg.mesha.goatos.core.network.dto.ProofReferenceDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.ProofUploadResponseDto
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
import sg.mesha.goatos.core.network.dto.VerificationVerdictRequestDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictResponseDto
import sg.mesha.goatos.core.network.dto.VerificationCloseRequestDto
import sg.mesha.goatos.core.network.dto.VerificationCloseSubmissionResponseDto
import sg.mesha.goatos.core.network.dto.WorkflowActionAnswerRequestDto
import sg.mesha.goatos.core.network.dto.WorkflowActionCompleteRequestDto
import sg.mesha.goatos.core.network.dto.WorkflowActionWriteResponseDto
import sg.mesha.goatos.core.network.dto.WorkflowDetailResponseDto
import sg.mesha.goatos.core.network.dto.WorkflowListResponseDto
import sg.mesha.goatos.core.network.dto.WeighingAnimalObservationRequestDto
import sg.mesha.goatos.core.network.dto.WeighingCampaignListResponseDto
import sg.mesha.goatos.core.network.dto.WeighingObservationResponseDto
import sg.mesha.goatos.core.network.dto.WeighingRosterResponseDto
import sg.mesha.goatos.core.network.dto.WeighingShedObservationRequestDto

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
)

/** Request body for POST /app/devices/{device_id}/heartbeat (HeartbeatDeviceRequest).
 *  [fcmToken] / [pushTokenHash]: same wire-field note as [RegisterDeviceRequestDto]. */
@Serializable
data class HeartbeatDeviceRequestDto(
    @SerialName("app_version") val appVersion: String = "",
    @SerialName("os_version") val osVersion: String = "",
    @SerialName("push_token_hash") val pushTokenHash: String? = null,
    @SerialName("fcm_token") val fcmToken: String? = null,
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
    ): VaccinationExecutionResponseDto

    /** GET /app/vaccination/execution/sheds/{shed_id} — one mobile shed execution context. */
    suspend fun getVaccinationExecutionShed(
        shedId: String,
        asOf: String? = null,
        dueBefore: String? = null,
        limit: Int? = null,
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
    suspend fun getShedCompletionSummary(taskId: String, shedId: String? = null): ShedCompletionSummaryDto

    /** GET /app/weighing/campaigns — operator-visible Weighing campaigns. */
    suspend fun listWeighingCampaigns(): WeighingCampaignListResponseDto

    /** GET /app/weighing/campaigns/{campaign_id}/sheds/{campaign_shed_id}/roster — active scope RFID roster. */
    suspend fun getWeighingRoster(
        campaignId: String,
        campaignShedId: String,
        limit: Int = 250,
    ): WeighingRosterResponseDto

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

    suspend fun getFeedTransportTasks(businessDate: String, cursor: String? = null, limit: Int? = null): FeedTransportTaskPageDto
    suspend fun submitFeedTransport(taskId: String, idempotencyKey: String, request: FeedTransportSubmitRequestDto): FeedTransportSubmitResponseDto

    suspend fun getFeedPackingWorklist(
        parkId: String,
        targetDate: String,
        // Optional session filter (session_no; null = every session). Mirrors the preview.
        session: Int? = null,
        workflow: String? = null,
        // Optional verification-lifecycle filter, same contract as the preview.
        status: String? = null,
        limit: Int? = null,
        offset: Int? = null,
    ): FeedPackingWorklistPageDto

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

    /** GET /app/workflows/{workflow_id} — one workflow's card header, context facts, and full
     *  bounded action list (≤18 rows). */
    suspend fun getWorkflow(workflowId: String): WorkflowDetailResponseDto

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
}

/**
 * Canned bootstrap so the shell renders the backend-driven nav end-to-end before
 * auth/network are wired. `chrome` lets previews exercise both drawer + bottom-bar
 * states. This is test/dev scaffolding, NOT product truth. Data methods return empty
 * responses so previews/tests compile without a live backend.
 */
class FakeAppApi(private val chrome: String = "expanded") : AppApi {
    override suspend fun bootstrap(deviceId: String?): BootstrapDto = BootstrapDto(
        navChrome = chrome,
        visibleNavigation = listOf(
            NavItemDto(key = "vaccination", label = "Drives", href = "/vaccination"),
            NavItemDto(key = "calendar", label = "Calendar", href = "/calendar"),
            NavItemDto(key = "alerts", label = "Alerts", href = "/alerts"),
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
                    NavItemDto(key = "alerts", label = "Alerts", href = "/alerts"),
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
    ): VaccinationExecutionResponseDto = VaccinationExecutionResponseDto()

    override suspend fun getVaccinationExecutionShed(
        shedId: String,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
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

    override suspend fun getShedCompletionSummary(taskId: String, shedId: String?): ShedCompletionSummaryDto =
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

    override suspend fun listWeighingCampaigns(): WeighingCampaignListResponseDto = WeighingCampaignListResponseDto()

    override suspend fun getWeighingRoster(
        campaignId: String,
        campaignShedId: String,
        limit: Int,
    ): WeighingRosterResponseDto = WeighingRosterResponseDto()

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

    override suspend fun getFeedTransportTasks(businessDate: String, cursor: String?, limit: Int?): FeedTransportTaskPageDto = FeedTransportTaskPageDto()
    override suspend fun submitFeedTransport(taskId: String, idempotencyKey: String, request: FeedTransportSubmitRequestDto): FeedTransportSubmitResponseDto = FeedTransportSubmitResponseDto("fake-attempt", "verification_due", 1, true)

    override suspend fun getFeedDirectionPreview(
        parkId: String,
        targetDate: String,
        shedId: String?,
        session: Int?,
        workflow: String?,
        status: String?,
        limit: Int?,
        offset: Int?,
    ): FeedDirectionPreviewPageDto = FeedDirectionPreviewPageDto(targetDate = targetDate)

    override suspend fun getFeedPackingWorklist(
        parkId: String,
        targetDate: String,
        session: Int?,
        workflow: String?,
        status: String?,
        limit: Int?,
        offset: Int?,
    ): FeedPackingWorklistPageDto = FeedPackingWorklistPageDto(targetDate = targetDate)

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

    override suspend fun getWorkflow(workflowId: String): WorkflowDetailResponseDto =
        WorkflowDetailResponseDto(workflowId = workflowId)

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
}

/**
 * DTO -> domain. The backend already computed the chrome, the drawer modules, and each
 * module's bar; the client only parses them (TRD §14 dumb-renderer). Labels pass through
 * verbatim — they are localized backend-side in `bootstrap_copy.go`.
 */
fun BootstrapDto.toNavState(): NavState = NavState(
    chrome = if (navChrome.equals("expanded", ignoreCase = true)) NavChrome.EXPANDED else NavChrome.MINIMAL,
    items = visibleNavigation.map { it.toNavItem() },
    modules = modules.map { module ->
        NavModule(
            key = module.key,
            label = module.label,
            href = module.href,
            status = NavModuleStatus.from(module.status),
            navItems = module.navItems.map { it.toNavItem() },
        )
    },
    featureFlags = featureFlags,
)

private fun NavItemDto.toNavItem(): NavItem = NavItem(key = key, label = label, href = href)
