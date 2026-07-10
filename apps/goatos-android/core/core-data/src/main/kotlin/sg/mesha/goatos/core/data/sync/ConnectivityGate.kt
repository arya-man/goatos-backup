package sg.mesha.goatos.core.data.sync

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities

/** Whether the device currently has validated internet — gates [SyncEngine.drainOnce] (TRD:
 *  "Connectivity: ConnectivityManager gates sync workers; capture always works offline"). */
fun interface ConnectivityGate {
    fun isOnline(): Boolean
}

class AndroidConnectivityGate(private val context: Context) : ConnectivityGate {
    override fun isOnline(): Boolean {
        val manager = context.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager ?: return true
        val network = manager.activeNetwork ?: return false
        val capabilities = manager.getNetworkCapabilities(network) ?: return false
        return capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) &&
            capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)
    }
}

/**
 * Stand-in for WorkManager's connectivity `Constraints` (WorkManager itself is not wired —
 * see the KDoc on [SyncEngine] for why). Registers a platform
 * [ConnectivityManager.NetworkCallback] — no extra Gradle dependency, `android.net.*` is part
 * of the platform SDK — and reports connectivity changes so [SyncRepository.observeStatus]
 * reflects the true online/offline state and a reconnect re-triggers a drain pass.
 *
 * Started once from [sg.mesha.goatos.GoatOsApplication.onCreate]; lives for the app's
 * lifetime (a `@Singleton` via Hilt, not `GlobalScope` — no unmanaged background work).
 */
class ConnectivitySyncTrigger(
    private val context: Context,
    private val onOnlineChanged: (online: Boolean) -> Unit,
) {
    private var registered = false
    private val callback = object : ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: Network) = onOnlineChanged(true)
        override fun onLost(network: Network) = onOnlineChanged(false)
    }

    fun start() {
        if (registered) return
        val manager = context.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager ?: return
        runCatching { manager.registerDefaultNetworkCallback(callback) }.onSuccess { registered = true }
    }

    fun stop() {
        if (!registered) return
        val manager = context.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager ?: return
        runCatching { manager.unregisterNetworkCallback(callback) }
        registered = false
    }
}
