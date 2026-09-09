package sg.mesha.goatos.core.media

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DataSource
import androidx.media3.datasource.DefaultDataSource
import androidx.media3.datasource.okhttp.OkHttpDataSource
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import java.io.IOException
import java.util.Locale
import java.util.concurrent.TimeUnit
import okhttp3.Interceptor
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import sg.mesha.goatos.core.network.ACCEPT_LANGUAGE_HEADER
import sg.mesha.goatos.core.network.LOCALE_CONTEXT_HEADER
import sg.mesha.goatos.core.network.NetworkTelemetryReporter
import sg.mesha.goatos.core.network.NoopNetworkTelemetryReporter
import sg.mesha.goatos.core.network.RequestMetadata
import sg.mesha.goatos.core.network.TENANT_CONTEXT_HEADER
import sg.mesha.goatos.core.network.TelemetryInterceptor

/**
 * Makes proof-video PLAYBACK visible to the same telemetry seam every API call already passes
 * through.
 *
 * ### The blind spot this closes
 * `ExoPlayer.Builder(context).build()` gives media3 its OWN `DefaultHttpDataSource` — a plain
 * `HttpURLConnection` stack that never touches this app's OkHttp client. So a proof video that
 * 403s on an expired signed URL, or 404s on a missing object, produced **nothing**: no
 * `TelemetryInterceptor` event, no logcat line, no Crashlytics breadcrumb, no `api_call_failure`.
 * Firebase Performance's automatic OkHttp instrumentation missed it for the same reason. The
 * verifier saw a dead player and the backend saw silence.
 *
 * Routing media3 through [OkHttpDataSource] over a client that carries [TelemetryInterceptor]
 * fixes that at the seam rather than adding a SECOND failure-reporting path. An
 * `AnalyticsListener` on each player would also observe errors, but it would need its own copy of
 * the non-fatal throttle and the redaction rules that
 * `FailureReportingNetworkTelemetryReporter` already owns — two implementations of one policy is
 * how the two halves drift apart.
 *
 * ### Auth
 * Proof playback now starts from an app-authenticated backend route such as
 * `/app/proofs/{id}/download`, so [proofMediaOkHttp] attaches the app bearer only to the GoatOS
 * API origin. OkHttp drops sensitive auth headers on a cross-host redirect, so the redirected GCS
 * request stays storage-only and does not receive the app token.
 *
 * ### What is never logged
 * The signed URL's QUERY STRING carries the signature, and it never crosses this seam:
 * [TelemetryInterceptor] only ever reads `url.encodedPath`, and this factory collapses even that
 * to the constant [PROOF_MEDIA_ROUTE]. Goat RFIDs/tags would be permissible (livestock data) but
 * are not present either. What IS reported is method, the constant route, status and duration.
 */
object ProofMediaHttp {

    /**
     * Constant route label for every media fetch.
     *
     * A signed proof URL's path is an opaque per-object storage key —
     * `TelemetryInterceptor.routeTemplate` cannot collapse it (it is neither UUID- nor
     * numeric-shaped), so the default mapper would emit a distinct route per video. That would
     * put a raw object key in logcat AND grow the non-fatal throttle map once per video watched,
     * turning a map that is bounded by endpoint count into one bounded by traffic. One label
     * keeps both bounded; the status code still separates "expired/forbidden" from "gone" from
     * "server fault".
     */
    const val PROOF_MEDIA_ROUTE: String = "media/proof-video"

    /**
     * The media HTTP client: no auth interceptor (see "Auth" above), telemetry interceptor
     * installed, and video-shaped timeouts — a multi-minute proof stream over a field connection
     * must not be capped by the small-JSON-body budget, and there is deliberately NO call timeout
     * (a call timeout bounds TOTAL elapsed time, which for a streamed video is playback length).
     */
    fun proofMediaOkHttp(
        reporter: NetworkTelemetryReporter = NoopNetworkTelemetryReporter,
        telemetryEnabled: Boolean = true,
        tokenProvider: () -> String? = { null },
        tenantIdProvider: () -> String? = { null },
        localeProvider: () -> String? = { null },
        requestMetadataProvider: () -> RequestMetadata = { RequestMetadata() },
        apiBaseUrl: String = "",
    ): OkHttpClient =
        OkHttpClient.Builder()
            .addInterceptor(
                proofApiAuthInterceptor(
                    apiBaseUrl = apiBaseUrl,
                    tokenProvider = tokenProvider,
                    tenantIdProvider = tenantIdProvider,
                    localeProvider = localeProvider,
                    requestMetadataProvider = requestMetadataProvider,
                ),
            )
            .addInterceptor(
                TelemetryInterceptor(
                    enabled = telemetryEnabled,
                    reporter = reporter,
                    routeMapper = { PROOF_MEDIA_ROUTE },
                ),
            )
            .connectTimeout(15, TimeUnit.SECONDS)
            .readTimeout(2, TimeUnit.MINUTES)
            .writeTimeout(2, TimeUnit.MINUTES)
            .retryOnConnectionFailure(true)
            .build()

    private fun proofApiAuthInterceptor(
        apiBaseUrl: String,
        tokenProvider: () -> String?,
        tenantIdProvider: () -> String?,
        localeProvider: () -> String?,
        requestMetadataProvider: () -> RequestMetadata,
    ): Interceptor {
        val apiOrigin = apiBaseUrl.toHttpUrlOrNull()
        return Interceptor { chain ->
            val request = chain.request()
            if (apiOrigin == null || !request.url.sameOriginAs(apiOrigin)) {
                return@Interceptor chain.proceed(request)
            }
            val localeTag = normalizeLocale(localeProvider())
            val builder = request.newBuilder()
            tokenProvider()?.takeIf { it.isNotBlank() }?.let { token ->
                builder.header("Authorization", "Bearer $token")
            }
            tenantIdProvider()?.takeIf { it.isNotBlank() }?.let { tenantId ->
                builder.header(TENANT_CONTEXT_HEADER, tenantId)
            }
            builder.header(ACCEPT_LANGUAGE_HEADER, acceptLanguage(localeTag))
            builder.header(LOCALE_CONTEXT_HEADER, localeTag)
            requestMetadataProvider().headers().forEach { (name, value) -> builder.header(name, value) }
            chain.proceed(builder.build())
        }
    }

    @UnstableApi
    fun dataSourceFactory(client: OkHttpClient): DataSource.Factory =
        OkHttpDataSource.Factory(client)

    @UnstableApi
    fun playbackDataSourceFactory(context: Context, client: OkHttpClient): DataSource.Factory =
        DefaultDataSource.Factory(context, dataSourceFactory(client))
}

private fun okhttp3.HttpUrl.sameOriginAs(other: okhttp3.HttpUrl): Boolean =
    scheme == other.scheme && host == other.host && port == other.port

private fun normalizeLocale(raw: String?): String {
    val candidate = raw?.trim()?.replace('_', '-')?.takeIf { it.isNotBlank() } ?: "en"
    return if (Regex("^[A-Za-z]{2,3}(?:-[A-Za-z0-9]{2,8})*$").matches(candidate)) {
        candidate.lowercase(Locale.ROOT)
    } else {
        "en"
    }
}

private fun acceptLanguage(localeTag: String): String =
    if (localeTag == "en") "en" else "$localeTag, en;q=0.8"

/**
 * Builds an [ExoPlayer] whose HTTP goes through the app's instrumented client.
 *
 * A `fun interface` rather than a raw `DataSource.Factory` so the two player call sites stay
 * media3-plumbing-free and so a screen that gets no provider (Compose preview, a screenshot test)
 * still renders with a stock player instead of crashing.
 */
fun interface ProofPlayerFactory {
    fun create(context: Context): ExoPlayer
}

@UnstableApi
class TelemetryProofPlayerFactory(private val client: OkHttpClient) : ProofPlayerFactory {
    override fun create(context: Context): ExoPlayer =
        ExoPlayer.Builder(context)
            .setMediaSourceFactory(
                DefaultMediaSourceFactory(ProofMediaHttp.playbackDataSourceFactory(context, client)),
            )
            .build()
}

/**
 * Stock, UNINSTRUMENTED fallback — media3's own `DefaultHttpDataSource`. Only reached when no
 * provider is installed above the screen (previews/tests). `:app` always provides the real one in
 * `MainActivity`.
 */
val DefaultProofPlayerFactory: ProofPlayerFactory =
    ProofPlayerFactory { context -> ExoPlayer.Builder(context).build() }

val LocalProofPlayerFactory = staticCompositionLocalOf { DefaultProofPlayerFactory }

fun interface ProofRemoteImageLoader {
    suspend fun load(context: Context, url: String, timeoutMs: Long): Bitmap?
}

class OkHttpProofRemoteImageLoader(private val client: OkHttpClient) : ProofRemoteImageLoader {
    override suspend fun load(context: Context, url: String, timeoutMs: Long): Bitmap? =
        try {
            // proof-media-egress:ignore Tap/fullscreen-only remote image loader; same client carries auth/telemetry and caller bounds timeout.
            val request = Request.Builder().url(url).build()
            client.newCall(request).execute().use { response ->
                if (!response.isSuccessful) return null
                response.body.byteStream().use(BitmapFactory::decodeStream)
            }
        } catch (_: IOException) {
            null
        } catch (_: IllegalArgumentException) {
            null
        } catch (_: SecurityException) {
            null
        }
}

val DefaultProofRemoteImageLoader = ProofRemoteImageLoader { _, _, _ -> null }

val LocalProofRemoteImageLoader = staticCompositionLocalOf { DefaultProofRemoteImageLoader }
