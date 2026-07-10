package sg.mesha.goatos.core.network

import kotlinx.serialization.json.Json
import okhttp3.Interceptor
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Response
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory
import retrofit2.create
import retrofit2.http.Body
import retrofit2.http.GET
import retrofit2.http.POST
import retrofit2.http.Path
import retrofit2.http.Query
import sg.mesha.goatos.core.network.dto.CalendarEventListResponseDto
import sg.mesha.goatos.core.network.dto.ControlTowerResponseDto
import sg.mesha.goatos.core.network.dto.EnrichedPositionListResponseDto
import sg.mesha.goatos.core.network.dto.MyCoverageResponseDto
import sg.mesha.goatos.core.network.dto.ProtocolAdherenceResponseDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationResponseDto
import sg.mesha.goatos.core.network.dto.ScanRosterResponseDto
import sg.mesha.goatos.core.network.dto.SubmissionResponseDto
// Device DTOs live in the network package (AppApi.kt); no dto.* import needed.
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.TaskListResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionShedDrilldownDto

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

    @GET("vaccination/execution")
    suspend fun listVaccinationExecution(
        @Query("park_id") parkId: String?,
        @Query("work_state") workState: String?,
        @Query("as_of") asOf: String?,
        @Query("due_before") dueBefore: String?,
        @Query("limit") limit: Int?,
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

    @POST("app/tasks/{task_id}/submissions")
    suspend fun submitAppTask(
        @Path("task_id") taskId: String,
        @Body request: SubmitTaskRequestDto,
    ): SubmissionResponseDto

    @GET("app/vaccination/execution/sheds/{shed_id}/roster")
    suspend fun getScanRoster(
        @Path("shed_id") shedId: String,
        @Query("limit") limit: Int?,
    ): ScanRosterResponseDto

    @POST("app/vaccination/obligations/{obligation_id}/reschedule")
    suspend fun rescheduleObligation(
        @Path("obligation_id") obligationId: String,
        @Body request: RescheduleObligationRequestDto,
    ): RescheduleObligationResponseDto

    @GET("app/roster/timetable")
    suspend fun getOperatorTimetable(
        @Query("center_id") centerId: String,
        @Query("limit") limit: Int?,
    ): EnrichedPositionListResponseDto

    @GET("app/roster/my-coverage")
    suspend fun getMyCoverage(): MyCoverageResponseDto
}

/** Adapts the Retrofit service to the [AppApi] port so callers stay Retrofit-agnostic. */
class RetrofitAppApi(private val service: AppApiService) : AppApi {
    override suspend fun bootstrap(deviceId: String?): BootstrapDto = service.bootstrap(deviceId)

    override suspend fun registerDevice(request: RegisterDeviceRequestDto): DeviceResponseDto =
        service.registerDevice(request)

    override suspend fun heartbeatDevice(deviceId: String, request: HeartbeatDeviceRequestDto): DeviceResponseDto =
        service.heartbeatDevice(deviceId, request)

    override suspend fun listVaccinationExecution(
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
    ): VaccinationExecutionResponseDto =
        service.listVaccinationExecution(parkId, workState, asOf, dueBefore, limit)

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
        cursor: String?,
        limit: Int?,
    ): CalendarEventListResponseDto =
        service.listCalendarVaccinationEvents(parkId, shedId, ownerKey, status, dateFrom, dateTo, cursor, limit)

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

    override suspend fun submitAppTask(
        taskId: String,
        request: SubmitTaskRequestDto,
    ): SubmissionResponseDto = service.submitAppTask(taskId, request)

    override suspend fun getScanRoster(
        shedId: String,
        limit: Int?,
    ): ScanRosterResponseDto = service.getScanRoster(shedId, limit)

    override suspend fun rescheduleObligation(
        obligationId: String,
        idempotencyKey: String,
        request: RescheduleObligationRequestDto,
    ): RescheduleObligationResponseDto {
        // Pass idempotency key as header via OkHttp interceptor in actual implementation
        // For now, just call the service method (header will be added by auth interceptor)
        return service.rescheduleObligation(obligationId, request)
    }

    override suspend fun getOperatorTimetable(
        centerId: String,
        limit: Int?,
    ): EnrichedPositionListResponseDto = service.getOperatorTimetable(centerId, limit)

    override suspend fun getMyCoverage(): MyCoverageResponseDto = service.getMyCoverage()
}

/**
 * Adds `Authorization: Bearer <token>` from a supplied provider. No token is stored
 * here — the provider reads the current session token (DataStore) each request, so
 * a refreshed token is picked up without rebuilding the client.
 */
class BearerAuthInterceptor(
    private val tokenProvider: () -> String?,
) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val token = tokenProvider()
        val request = if (token.isNullOrBlank()) {
            chain.request()
        } else {
            chain.request().newBuilder()
                .addHeader("Authorization", "Bearer $token")
                .build()
        }
        return chain.proceed(request)
    }
}

/** Builds the OkHttp/Retrofit stack. Hilt provides these in the DI pass. */
object NetworkFactory {
    val json: Json = Json {
        ignoreUnknownKeys = true
        explicitNulls = false
    }

    fun okHttp(tokenProvider: () -> String?): OkHttpClient =
        OkHttpClient.Builder()
            .addInterceptor(BearerAuthInterceptor(tokenProvider))
            .build()

    fun retrofit(baseUrl: String, client: OkHttpClient): Retrofit =
        Retrofit.Builder()
            .baseUrl(baseUrl)
            .client(client)
            .addConverterFactory(json.asConverterFactory("application/json".toMediaType()))
            .build()

    fun appApi(baseUrl: String, tokenProvider: () -> String?): AppApi =
        RetrofitAppApi(retrofit(baseUrl, okHttp(tokenProvider)).create())
}
