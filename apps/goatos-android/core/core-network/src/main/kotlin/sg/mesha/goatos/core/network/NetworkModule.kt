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
import sg.mesha.goatos.core.network.dto.EnrichedPositionListResponseDto
import sg.mesha.goatos.core.network.dto.MyCoverageResponseDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.ProofUploadResponseDto
import sg.mesha.goatos.core.network.dto.forCreateUpload
import sg.mesha.goatos.core.network.dto.ProtocolAdherenceResponseDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationResponseDto
import sg.mesha.goatos.core.network.dto.ReviewTaskRequestDto
import sg.mesha.goatos.core.network.dto.ReviewTaskResponseDto
import sg.mesha.goatos.core.network.dto.ScanRosterResponseDto
import sg.mesha.goatos.core.network.dto.SubmissionResponseDto
// Device DTOs live in the network package (AppApi.kt); no dto.* import needed.
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.TaskDetailResponseDto
import sg.mesha.goatos.core.network.dto.TaskListResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionShedDrilldownDto
import sg.mesha.goatos.core.network.dto.VaccinationGapsResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationCoverageResponseDto
import sg.mesha.goatos.core.network.dto.AppConfigResponseDto

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

    @GET("vaccination/execution")
    suspend fun listVaccinationExecution(
        @Query("park_id") parkId: String?,
        @Query("work_state") workState: String?,
        @Query("as_of") asOf: String?,
        @Query("due_before") dueBefore: String?,
        @Query("limit") limit: Int?,
        @Query("cursor") cursor: String?,
    ): VaccinationExecutionResponseDto

    @GET("vaccination/execution/sheds/{shed_id}")
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
        @Query("limit") limit: Int?,
    ): TaskListResponseDto

    @GET("app/tasks/{task_id}")
    suspend fun getAppTask(@Path("task_id") taskId: String): TaskDetailResponseDto

    @POST("app/tasks/{task_id}/submissions")
    suspend fun submitAppTask(
        @Path("task_id") taskId: String,
        @Header("Idempotency-Key") idempotencyKey: String,
        @Body request: SubmitTaskRequestDto,
    ): SubmissionResponseDto

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
        @Query("task_id") taskId: String,
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
}

/** Adapts the Retrofit service to the [AppApi] port so callers stay Retrofit-agnostic. */
class RetrofitAppApi(private val service: AppApiService) : AppApi {
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
        limit: Int?,
        cursor: String?,
    ): VaccinationExecutionResponseDto =
        service.listVaccinationExecution(parkId, workState, asOf, dueBefore, limit, cursor)

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
        limit: Int?,
    ): TaskListResponseDto = service.listAppTasks(state, limit)

    override suspend fun getAppTask(taskId: String): TaskDetailResponseDto = service.getAppTask(taskId)

    override suspend fun submitAppTask(
        taskId: String,
        idempotencyKey: String,
        request: SubmitTaskRequestDto,
    ): SubmissionResponseDto = service.submitAppTask(taskId, idempotencyKey, request)

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
        taskId: String,
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
    ): OkHttpClient =
        OkHttpClient.Builder()
            .addInterceptor(BearerAuthInterceptor(tokenProvider, tenantIdProvider, localeProvider))
            // Explicit bounds — never rely on the platform/OkHttp defaults (a stuck socket on a
            // field 2G link must fail and let the outbox back off, not hang the drain coroutine).
            .connectTimeout(15, TimeUnit.SECONDS)
            .readTimeout(30, TimeUnit.SECONDS)
            .writeTimeout(30, TimeUnit.SECONDS)
            .callTimeout(60, TimeUnit.SECONDS)
            .retryOnConnectionFailure(true)
            .build()

    fun retrofit(baseUrl: String, client: OkHttpClient): Retrofit =
        Retrofit.Builder()
            .baseUrl(baseUrl)
            .client(client)
            .addConverterFactory(json.asConverterFactory("application/json".toMediaType()))
            .build()

    fun appApi(
        baseUrl: String,
        tokenProvider: () -> String?,
        tenantIdProvider: () -> String? = { null },
        localeProvider: () -> String? = { null },
    ): AppApi =
        RetrofitAppApi(retrofit(baseUrl, okHttp(tokenProvider, tenantIdProvider, localeProvider)).create())
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
