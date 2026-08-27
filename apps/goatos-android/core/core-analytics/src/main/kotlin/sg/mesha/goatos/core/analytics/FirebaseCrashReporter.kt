package sg.mesha.goatos.core.analytics

import com.google.firebase.crashlytics.FirebaseCrashlytics
import java.net.ConnectException
import java.net.NoRouteToHostException
import java.net.SocketException
import java.net.SocketTimeoutException
import java.net.UnknownHostException
import kotlin.coroutines.cancellation.CancellationException

/**
 * Real [CrashReporter] backed by Firebase Crashlytics. Bound in place of [NoopCrashReporter] for
 * flavors with `BuildConfig.TELEMETRY_ENABLED = true` (see `di/TelemetryModule.kt` in `:app`).
 */
class FirebaseCrashReporter internal constructor(
    private val sink: CrashlyticsSink,
) : CrashReporter {

    constructor() : this(FirebaseCrashlyticsSink)

    override fun recordException(throwable: Throwable, message: String?) {
        message?.let { sink.log(it) }
        if (throwable.shouldSkipCrashlyticsNonFatal()) return
        sink.recordException(throwable)
    }

    override fun log(message: String) {
        sink.log(message)
    }

    override fun setCustomKey(key: String, value: String) {
        sink.setCustomKey(key, value)
    }
}

internal interface CrashlyticsSink {
    fun recordException(throwable: Throwable)
    fun log(message: String)
    fun setCustomKey(key: String, value: String)
}

private object FirebaseCrashlyticsSink : CrashlyticsSink {
    private val crashlytics: FirebaseCrashlytics
        get() = FirebaseCrashlytics.getInstance()

    override fun recordException(throwable: Throwable) {
        crashlytics.recordException(throwable)
    }

    override fun log(message: String) {
        crashlytics.log(message)
    }

    override fun setCustomKey(key: String, value: String) {
        crashlytics.setCustomKey(key, value)
    }
}

internal fun Throwable.hasNetworkConnectivityCause(): Boolean {
    var cursor: Throwable? = this
    while (cursor != null) {
        if (cursor.isNetworkConnectivityFailure()) return true
        cursor = cursor.cause
    }
    return false
}

private fun Throwable.isNetworkConnectivityFailure(): Boolean = when (this) {
    is UnknownHostException,
    is SocketTimeoutException,
    is ConnectException,
    is NoRouteToHostException,
    is SocketException,
    -> true
    else -> false
}

internal fun Throwable.shouldSkipCrashlyticsNonFatal(): Boolean =
    this is CancellationException || hasNetworkConnectivityCause() || hasRawHttpExceptionCause()

private fun Throwable.hasRawHttpExceptionCause(): Boolean {
    var cursor: Throwable? = this
    while (cursor != null) {
        if (cursor.javaClass.name == "retrofit2.HttpException") return true
        cursor = cursor.cause
    }
    return false
}
