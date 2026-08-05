package sg.mesha.goatos.core.data.sync

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import java.net.URI

/** Whether the device currently has validated internet — gates [SyncEngine.drainOnce] (TRD:
 *  "Connectivity: ConnectivityManager gates sync workers; capture always works offline"). */
fun interface ConnectivityGate {
    fun isOnline(): Boolean
}

class AndroidConnectivityGate(private val context: Context) : ConnectivityGate {
    override fun isOnline(): Boolean {
        // Fails OPEN (returns true) if the service can't be read: an attempted drain that turns
        // out to be offline just fails transiently and backs off — safe. Failing closed here
        // would strand writes whenever the service is momentarily unavailable.
        val manager = context.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager ?: return true
        val network = manager.activeNetwork ?: return false
        val capabilities = manager.getNetworkCapabilities(network) ?: return false
        return capabilities.hasValidatedInternet()
    }
}

/**
 * Local Android device proof runs the app against a laptop backend through `adb reverse`
 * (`http://localhost:8080/` on the phone). Android can mark that network as not having
 * validated internet even though the loopback API is reachable, so the outbox must still
 * attempt a drain for local API bases. Non-local API bases keep the platform gate.
 */
class LocalBackendConnectivityGate(
    private val delegate: ConnectivityGate,
    private val apiBaseUrl: String,
) : ConnectivityGate {
    override fun isOnline(): Boolean =
        delegate.isOnline() || apiBaseUrl.isLoopbackHttpBase()
}

/**
 * Whether an API base URL points at the device's own loopback interface — i.e. a request to it
 * cannot possibly need internet. THE single source of truth for that question: both
 * [LocalBackendConnectivityGate] (the drain-time gate) and `SyncWorkScheduler` (the WorkManager
 * enqueue-time `Constraints`) must agree, or the OS-level constraint silently vetoes work the
 * gate would have allowed and a queued write never leaves the phone.
 */
fun String.isLoopbackHttpBase(): Boolean {
    val uri = runCatching { URI(this) }
        .onFailure { android.util.Log.d("ConnectivityGate", "isLoopbackHttpBase: parse URI failed for '$this'", it) }
        .getOrNull() ?: return false
    val scheme = uri.scheme?.lowercase() ?: return false
    if (scheme != "http" && scheme != "https") return false
    val host = uri.host?.lowercase() ?: return false
    return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

/**
 * Source of reactive online/offline transitions. Extracted as a port so [ConnectivitySyncTrigger]
 * — its idempotent start/stop and change-dedup logic — is unit-testable on the plain JVM without
 * Android or Robolectric (a fake source drives transitions and asserts the callback is registered
 * exactly once and always unregistered: the leak check). The only Android-touching implementation
 * is [AndroidConnectivitySource].
 */
fun interface ConnectivitySource {
    /** Begin observing connectivity; invoke [onChange] with the current online state on every
     *  transition. Returns a handle whose [AutoCloseable.close] stops observing and releases the
     *  underlying platform callback (no leak). */
    fun start(onChange: (online: Boolean) -> Unit): AutoCloseable
}

/**
 * Real platform source: registers a single [ConnectivityManager.NetworkCallback] on the default
 * network. `onCapabilitiesChanged` (not just `onAvailable`) drives the online signal, so a
 * reconnect that fires `onAvailable` BEFORE the network is `VALIDATED` still reports online the
 * moment validation lands — the earlier version only listened to `onAvailable` and could miss the
 * validated-reconnect, leaving the drain waiting for the next enqueue/relaunch.
 */
class AndroidConnectivitySource(private val context: Context) : ConnectivitySource {
    override fun start(onChange: (Boolean) -> Unit): AutoCloseable {
        val manager = context.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager
            ?: return AutoCloseable { }
        val callback = object : ConnectivityManager.NetworkCallback() {
            override fun onLost(network: Network) = onChange(manager.isCurrentlyOnline())
            override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) {
                onChange(caps.hasValidatedInternet())
            }
        }
        val registered = runCatching { manager.registerDefaultNetworkCallback(callback) }.isSuccess
        return if (registered) {
            onChange(manager.isCurrentlyOnline())
            AutoCloseable { runCatching { manager.unregisterNetworkCallback(callback) } }
        } else {
            AutoCloseable { }
        }
    }
}

/**
 * Stand-in for WorkManager's connectivity `Constraints`. Registers ONE [ConnectivitySource]
 * observation for the app's lifetime and forwards de-duplicated online/offline transitions so
 * [SyncRepository.observeStatus] reflects true connectivity and a reconnect re-triggers a drain.
 *
 * Started once from [sg.mesha.goatos.GoatOsApplication.onCreate]; a `@Singleton` via Hilt (not
 * `GlobalScope`). [start] is idempotent (never double-registers); [stop] always releases the
 * handle (never leaks the platform callback).
 */
class ConnectivitySyncTrigger(
    private val source: ConnectivitySource,
    private val onOnlineChanged: (online: Boolean) -> Unit,
) {
    private var handle: AutoCloseable? = null
    private var lastOnline: Boolean? = null

    fun start() {
        if (handle != null) return
        handle = source.start(::emit)
    }

    fun stop() {
        handle?.let { runCatching { it.close() } }
        handle = null
        lastOnline = null
    }

    // Dedup: onCapabilitiesChanged can fire repeatedly with the same online state — only forward
    // real transitions so we don't kick a redundant drain on every capability tweak.
    private fun emit(online: Boolean) {
        if (online == lastOnline) return
        lastOnline = online
        onOnlineChanged(online)
    }
}

private fun ConnectivityManager.isCurrentlyOnline(): Boolean {
    val network = activeNetwork ?: return false
    val capabilities = getNetworkCapabilities(network) ?: return false
    return capabilities.hasValidatedInternet()
}

private fun NetworkCapabilities.hasValidatedInternet(): Boolean =
    hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) &&
        hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)
