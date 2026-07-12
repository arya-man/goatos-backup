package sg.mesha.goatos.core.network

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import sg.mesha.goatos.core.model.nav.NavChrome
import sg.mesha.goatos.core.model.nav.NavItem
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.dto.CalendarEventListResponseDto
import sg.mesha.goatos.core.network.dto.ControlTowerResponseDto
import sg.mesha.goatos.core.network.dto.EnrichedPositionListResponseDto
import sg.mesha.goatos.core.network.dto.MyCoverageResponseDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.ProofUploadResponseDto
import sg.mesha.goatos.core.network.dto.ProtocolAdherenceResponseDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationResponseDto
import sg.mesha.goatos.core.network.dto.ReviewTaskRequestDto
import sg.mesha.goatos.core.network.dto.ReviewTaskResponseDto
import sg.mesha.goatos.core.network.dto.ScanRosterResponseDto
import sg.mesha.goatos.core.network.dto.SubmissionResponseDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.TaskDetailResponseDto
import sg.mesha.goatos.core.network.dto.TaskListResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionShedDrilldownDto
import sg.mesha.goatos.core.network.dto.VaccinationGapsResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationCoverageResponseDto
import sg.mesha.goatos.core.network.dto.AppConfigResponseDto

// Wire DTOs for the nav slice of GET /app/bootstrap. The response (BootstrapResponse)
// carries many more fields; with ignoreUnknownKeys the client only binds the ones it
// renders. Defaults keep deserialization lenient. These hand-mapped DTOs are replaced
// 1:1 by the OpenAPI-generated client next.
@Serializable
data class BootstrapDto(
    @SerialName("nav_chrome") val navChrome: String = "minimal",
    @SerialName("visible_navigation") val visibleNavigation: List<NavItemDto> = emptyList(),
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

/** Request body for POST /app/devices/register (RegisterDeviceRequest). */
@Serializable
data class RegisterDeviceRequestDto(
    @SerialName("app_install_id") val appInstallId: String,
    @SerialName("app_version") val appVersion: String,
    @SerialName("os_version") val osVersion: String = "",
    @SerialName("push_token_hash") val pushTokenHash: String? = null,
)

/** Request body for POST /app/devices/{device_id}/heartbeat (HeartbeatDeviceRequest). */
@Serializable
data class HeartbeatDeviceRequestDto(
    @SerialName("app_version") val appVersion: String = "",
    @SerialName("os_version") val osVersion: String = "",
    @SerialName("push_token_hash") val pushTokenHash: String? = null,
)

/** Response for register/heartbeat (DeviceResponse). */
@Serializable
data class DeviceResponseDto(
    @SerialName("device") val device: DeviceSummaryDto = DeviceSummaryDto(),
    @SerialName("trace_id") val traceId: String = "",
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

    /** GET /vaccination/execution — execution rows grouped by park + shed. */
    suspend fun listVaccinationExecution(
        parkId: String? = null,
        workState: String? = null,
        asOf: String? = null,
        dueBefore: String? = null,
        limit: Int? = null,
    ): VaccinationExecutionResponseDto

    /** GET /vaccination/execution/sheds/{shed_id} — one shed's execution context. */
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
        limit: Int? = null,
    ): TaskListResponseDto

    /** GET /app/tasks/{task_id} — the task PLUS its SOP version (`form_dsl`) so the operator
     *  form runner can render the drive form. The list endpoint omits the form. */
    suspend fun getAppTask(taskId: String): TaskDetailResponseDto

    /** POST /app/tasks/{task_id}/submissions — idempotent SOP task submission. The offline
     *  sync engine's outbox drains this with a stable [idempotencyKey] (same key on every
     *  retry) so a server-committed-but-client-unrecorded replay never duplicates the write. */
    suspend fun submitAppTask(
        taskId: String,
        idempotencyKey: String,
        request: SubmitTaskRequestDto,
    ): SubmissionResponseDto

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
        taskId: String,
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
            NavItemDto(key = "calendar", label = "Calendar", href = "/calendar"),
            NavItemDto(key = "vaccination", label = "Vaccination", href = "/vaccination"),
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
        limit: Int?,
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
        limit: Int?,
    ): TaskListResponseDto = TaskListResponseDto()

    override suspend fun getAppTask(taskId: String): TaskDetailResponseDto = TaskDetailResponseDto()

    override suspend fun submitAppTask(
        taskId: String,
        idempotencyKey: String,
        request: SubmitTaskRequestDto,
    ): SubmissionResponseDto = SubmissionResponseDto()

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
        taskId: String,
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
        ProofUploadResponseDto()

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
}

/**
 * DTO -> domain. The backend already computed the chrome; the client only
 * parses the enum and visible nav items (TRD §14 dumb-renderer).
 */
fun BootstrapDto.toNavState(): NavState = NavState(
    chrome = if (navChrome.equals("expanded", ignoreCase = true)) NavChrome.EXPANDED else NavChrome.MINIMAL,
    items = visibleNavigation.map { NavItem(key = it.key, label = it.label, href = it.href) },
)
