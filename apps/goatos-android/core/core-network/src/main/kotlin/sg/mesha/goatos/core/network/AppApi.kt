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
import sg.mesha.goatos.core.network.dto.CountsApprovalDecisionRequestDto
import sg.mesha.goatos.core.network.dto.CountsApprovalDecisionResponseDto
import sg.mesha.goatos.core.network.dto.CountsApprovalListResponseDto
import sg.mesha.goatos.core.network.dto.CountsBirthEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsBreakdownResponseDto
import sg.mesha.goatos.core.network.dto.CountsDeathEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsGoatLifecycleResponseDto
import sg.mesha.goatos.core.network.dto.CountsShiftingDestinationsResponseDto
import sg.mesha.goatos.core.network.dto.CountsShiftingEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsShiftingEventResponseDto
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
        mimeType: String,
        filePath: String,
        durationMs: Long?,
    ): ProofCompleteResponseDto

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
        cursor: String? = null,
        limit: Int? = null,
    ): VerificationQueueResponseDto

    /** GET /verification/action-queue — verifier-approved items within the leadership grant scope. */
    suspend fun listVerificationActionQueue(
        category: String? = null,
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

    /** POST /app/counts/shifting-events — an operator-reported movement between sheds. Drained
     *  through the offline-sync outbox with a stable [idempotencyKey]: the backend derives the
     *  movement's logical key from that key, so an exact retry collapses onto the SAME row
     *  instead of recording a second movement. */
    suspend fun recordCountsShiftingEvent(
        idempotencyKey: String,
        request: CountsShiftingEventRequestDto,
    ): CountsShiftingEventResponseDto

    /** POST /app/counts/birth-events — records a birth as goat creation with `origin_type`
     *  pinned to `birth` server-side. The `goat.created` event it emits still auto-generates the
     *  kid's vaccination obligations. Idempotent on [idempotencyKey] like every other write. */
    suspend fun recordCountsBirthEvent(
        idempotencyKey: String,
        request: CountsBirthEventRequestDto,
    ): CountsGoatLifecycleResponseDto

    /** POST /app/counts/death-events — records a death through identity's guardrailed
     *  critical-death exit. The `lifecycle_status="dead"` + `exit_reason="died"` pairing is
     *  enforced server-side; the emitted `goat.exited` event auto-cancels the animal's open
     *  obligations. Idempotent on [idempotencyKey]. */
    suspend fun recordCountsDeathEvent(
        idempotencyKey: String,
        request: CountsDeathEventRequestDto,
    ): CountsGoatLifecycleResponseDto

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

    /** POST /app/counts/approvals/{request_id}/approve — applies the request (creates the kid,
     *  exits the animal, or authorizes the movement AND relocates its animals), atomically with
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
            BootstrapModuleDto(key = "feed_direction", label = "Feed direction", status = "soon"),
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
        mimeType: String,
        filePath: String,
        durationMs: Long?,
    ): ProofCompleteResponseDto = ProofCompleteResponseDto(
        proof = ProofArtifactDto(proofId = proofId, uploadState = "completed", mimeType = mimeType, durationMs = durationMs),
    )

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
        cursor: String?,
        limit: Int?,
    ): VerificationQueueResponseDto = VerificationQueueResponseDto()

    override suspend fun listVerificationActionQueue(
        category: String?,
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

    override suspend fun recordCountsShiftingEvent(
        idempotencyKey: String,
        request: CountsShiftingEventRequestDto,
    ): CountsShiftingEventResponseDto =
        CountsShiftingEventResponseDto(shiftingEventId = "fake-shifting-$idempotencyKey")

    override suspend fun recordCountsBirthEvent(
        idempotencyKey: String,
        request: CountsBirthEventRequestDto,
    ): CountsGoatLifecycleResponseDto = CountsGoatLifecycleResponseDto()

    override suspend fun recordCountsDeathEvent(
        idempotencyKey: String,
        request: CountsDeathEventRequestDto,
    ): CountsGoatLifecycleResponseDto = CountsGoatLifecycleResponseDto()

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
)

private fun NavItemDto.toNavItem(): NavItem = NavItem(key = key, label = label, href = href)
