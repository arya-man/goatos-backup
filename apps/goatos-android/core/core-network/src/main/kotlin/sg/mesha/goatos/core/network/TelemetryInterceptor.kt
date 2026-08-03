package sg.mesha.goatos.core.network

import kotlin.random.Random
import okhttp3.Interceptor
import okhttp3.Response

/**
 * One completed (or failed) network call's telemetry, reported after the response resolves.
 * [route] is a bounded-cardinality TEMPLATE — path-like ids are replaced with `{id}` — never a
 * raw path/goat-id/shed-id (see `docs/observability/OBSERVABILITY_DESIGN.md` §3 cardinality
 * guard). [statusCode] is `-1` when the call threw before a response was received.
 */
data class NetworkTelemetryEvent(
    val method: String,
    val route: String,
    val statusCode: Int,
    val durationMs: Long,
    val traceparent: String,
)

/**
 * Egress seam for [TelemetryInterceptor]. Kept vendor-free so `core-network` never depends on
 * Firebase/OTel directly — real reporting is wired from `:app` (see
 * `FirebasePerfNetworkTelemetryReporter` in `core-analytics`).
 *
 * Stable public API grepped by the telemetry CI guardrail (see `docs/TELEMETRY.md`) — do not
 * rename without updating that doc and the guard.
 */
fun interface NetworkTelemetryReporter {
    fun onNetworkCall(event: NetworkTelemetryEvent)
}

/** Fallback for tests/local/dev — reports nowhere, never throws. */
object NoopNetworkTelemetryReporter : NetworkTelemetryReporter {
    override fun onNetworkCall(event: NetworkTelemetryEvent) {}
}

/**
 * OkHttp interceptor that:
 *  1. Generates (or passes through, if the caller already set one) a W3C `traceparent` header on
 *     every request, so the backend's OTel span for this call links to the mobile round-trip
 *     (`docs/observability/OBSERVABILITY_DESIGN.md` §2.2/§2.5 — "propagate trace context").
 *  2. Times the call and reports method/route-template/status/duration via [reporter].
 *
 * `enabled = false` (e.g. a flavor with `TELEMETRY_ENABLED=false`) skips both — the request still
 * proceeds completely unchanged, so disabling telemetry can never affect the network path.
 *
 * TODO(otel-otlp): forward [NetworkTelemetryEvent] (plus the [Response]'s size/timing detail) to
 * a real OTLP/gRPC span exporter pointed at `BuildConfig.OTLP_ENDPOINT` once `opentelemetry-android`
 * is wired (tracked in `docs/observability/OBSERVABILITY_DESIGN.md` §2.5 and `docs/TELEMETRY.md`).
 * Today [reporter] only feeds Firebase Performance as a custom trace — Firebase Perf's OWN
 * automatic OkHttp instrumentation (enabled by the `com.google.firebase.firebase-perf` Gradle
 * plugin, no code required) already covers raw-URL network metrics independently of this class.
 *
 * Stable public API grepped by the telemetry CI guardrail (see `docs/TELEMETRY.md`) — do not
 * rename without updating that doc and the guard.
 */
class TelemetryInterceptor(
    private val enabled: Boolean = true,
    private val reporter: NetworkTelemetryReporter = NoopNetworkTelemetryReporter,
    /**
     * Maps a request to its bounded-cardinality route label. The default is the API stack's
     * path-template collapse. MEDIA playback overrides it: a signed proof-video URL's path is an
     * opaque per-object storage key that [routeTemplate] cannot collapse (it is neither UUID- nor
     * numeric-shaped), so leaving the default would grow the throttle map once per video watched
     * and put a raw object key into logcat. The media client passes a CONSTANT label instead —
     * see `sg.mesha.goatos.core.media.ProofMediaHttp`.
     *
     * Only ever receives the path: the query string (which on a signed URL carries the SIGNATURE)
     * is never read here and never reaches [NetworkTelemetryEvent].
     */
    private val routeMapper: (String) -> String = { routeTemplate(it) },
) : Interceptor {

    override fun intercept(chain: Interceptor.Chain): Response {
        val original = chain.request()
        if (!enabled) return chain.proceed(original)

        val traceparent = original.header(TRACEPARENT_HEADER) ?: newTraceparent()
        val request = original.newBuilder().header(TRACEPARENT_HEADER, traceparent).build()
        val route = routeMapper(request.url.encodedPath)

        val startNanos = System.nanoTime()
        var statusCode = -1
        try {
            val response = chain.proceed(request)
            statusCode = response.code
            return response
        } finally {
            val durationMs = (System.nanoTime() - startNanos) / 1_000_000
            val event = NetworkTelemetryEvent(
                method = request.method,
                route = route,
                statusCode = statusCode,
                durationMs = durationMs,
                traceparent = traceparent,
            )
            // Telemetry must never fail or slow the real call — it has already returned/thrown.
            runCatching { reporter.onNetworkCall(event) }
        }
    }

    companion object {
        const val TRACEPARENT_HEADER: String = "traceparent"

        private val uuidLike =
            Regex("^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$")
        private val numericLike = Regex("^[0-9]+$")

        /** W3C Trace Context header: `version-traceid(32 hex)-spanid(16 hex)-flags(2 hex)`. */
        fun newTraceparent(): String = "00-${randomHex(32)}-${randomHex(16)}-01"

        private fun randomHex(hexLength: Int): String {
            val bytes = ByteArray(hexLength / 2)
            Random.nextBytes(bytes)
            return bytes.joinToString(separator = "") { byte -> "%02x".format(byte) }
        }

        /**
         * Cardinality guard (`OBSERVABILITY_DESIGN.md` §3): replaces UUID/numeric path segments
         * with `{id}` so a reported route never carries a raw goat/task/shed id.
         */
        fun routeTemplate(encodedPath: String): String =
            encodedPath.split('/').joinToString(separator = "/") { segment ->
                when {
                    segment.isEmpty() -> segment
                    uuidLike.matches(segment) -> "{id}"
                    numericLike.matches(segment) -> "{id}"
                    else -> segment
                }
            }
    }
}
