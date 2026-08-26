package sg.mesha.goatos.core.media

import android.content.Context
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DataSource
import androidx.media3.datasource.DefaultDataSource
import androidx.media3.datasource.okhttp.OkHttpDataSource
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import java.util.concurrent.TimeUnit
import okhttp3.OkHttpClient
import sg.mesha.goatos.core.network.NetworkTelemetryReporter
import sg.mesha.goatos.core.network.NoopNetworkTelemetryReporter
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
 * Playback URLs are pre-signed: the grant is IN the URL, and the object usually lives on a
 * third-party storage host (GCS). Today's players attach no headers at all, and [proofMediaOkHttp]
 * deliberately keeps it that way — it is built WITHOUT `BearerAuthInterceptor`, exactly like
 * `NetworkFactory.bareOkHttp()`. Adding the bearer would both be redundant and leak this app's
 * token to a host outside its own API (the same rule `bareOkHttp`'s KDoc states for proof
 * UPLOADS). Playback behaviour is therefore unchanged; only the observability is new.
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
    ): OkHttpClient =
        OkHttpClient.Builder()
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

    @UnstableApi
    fun dataSourceFactory(client: OkHttpClient): DataSource.Factory =
        OkHttpDataSource.Factory(client)

    @UnstableApi
    fun playbackDataSourceFactory(context: Context, client: OkHttpClient): DataSource.Factory =
        DefaultDataSource.Factory(context, dataSourceFactory(client))
}

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
