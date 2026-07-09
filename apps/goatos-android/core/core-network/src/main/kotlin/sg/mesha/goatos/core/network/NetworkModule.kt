package sg.mesha.goatos.core.network

import kotlinx.serialization.json.Json
import okhttp3.Interceptor
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Response
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory
import retrofit2.create
import retrofit2.http.GET

/** Retrofit surface for the app API. One method per consumed endpoint. */
interface AppApiService {
    @GET("app/bootstrap")
    suspend fun bootstrap(): BootstrapDto
}

/** Adapts the Retrofit service to the [AppApi] port so callers stay Retrofit-agnostic. */
class RetrofitAppApi(private val service: AppApiService) : AppApi {
    override suspend fun bootstrap(): BootstrapDto = service.bootstrap()
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
