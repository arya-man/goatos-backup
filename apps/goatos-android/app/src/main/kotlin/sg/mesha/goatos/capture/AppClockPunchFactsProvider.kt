package sg.mesha.goatos.capture

import android.Manifest
import android.content.Context
import android.content.pm.ApplicationInfo
import android.content.pm.PackageManager
import android.location.Geocoder
import android.location.Location
import android.location.LocationListener
import android.location.LocationManager
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.os.BatteryManager
import android.os.Build
import android.os.Bundle
import android.os.Looper
import android.provider.Settings
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.clock.MockLocationVerdict
import sg.mesha.goatos.core.common.clock.MockProviderApp
import sg.mesha.goatos.core.data.ClockPunchFacts
import sg.mesha.goatos.core.data.ClockPunchFactsProvider
import sg.mesha.goatos.core.network.dto.ClockLocationDto
import java.util.Locale
import javax.inject.Inject
import javax.inject.Singleton
import kotlin.coroutines.resume

/**
 * Collects everything the device can honestly tell us at clock-punch time (module clock,
 * maintainer decision 2026-08-27 — docs/features/clock-in-out/plan.md §4.1/§4.2). The only
 * Android-touching implementation of the core-data [ClockPunchFactsProvider] seam, mirroring how
 * [AppProofLocationProvider] implements `ProofLocationProvider`.
 *
 * The mock-location verdict is layered, strictest available per OS level, and RECOMPUTED on
 * every tap (never a one-time install check):
 *  1. the FIX itself — [Location.isMock] (API 31+) / `isFromMockProvider` below;
 *  2. INSTALLED fake-GPS apps — a [PackageManager] scan for packages whose requested permissions
 *     include `android.permission.ACCESS_MOCK_LOCATION` (the permission every mock-provider app
 *     must declare); needs `QUERY_ALL_PACKAGES` (declared in AndroidManifest.xml with the
 *     recorded justification);
 *  3. context signals — developer options enabled (recorded, never blocking).
 */
@Singleton
class AppClockPunchFactsProvider @Inject constructor(
    @ApplicationContext private val context: Context,
    private val crashReporter: CrashReporter,
) : ClockPunchFactsProvider {

    override suspend fun capture(): ClockPunchFacts = withContext(Dispatchers.IO) {
        val fix = capturedFix()
        val location = fix.toLocationDto()
        val verdict = MockLocationVerdict(
            mockFix = fix?.isMockFix() == true,
            mockApps = scanMockProviderApps(),
            developerOptions = developerOptionsEnabled(),
        )
        val networkKind = currentNetworkKind()
        ClockPunchFacts(
            location = location,
            verdict = verdict,
            batteryPct = batteryPct(),
            networkKind = networkKind,
            offline = networkKind == null,
        )
    }

    // --- location ---------------------------------------------------------------------------

    private fun hasFineLocation(): Boolean =
        context.checkSelfPermission(Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED

    private suspend fun capturedFix(): Location? {
        if (!hasFineLocation()) return null
        return freshLocation() ?: latestKnownLocation()
    }

    private fun Location?.toLocationDto(): ClockLocationDto {
        if (!hasFineLocation()) return ClockLocationDto(status = "permission_missing")
        if (this == null) return ClockLocationDto(status = "unavailable")
        return ClockLocationDto(
            status = "captured",
            latitude = latitude,
            longitude = longitude,
            gpsAccuracyM = if (hasAccuracy()) accuracy.toDouble() else null,
            address = reverseGeocode(this),
        )
    }

    @Suppress("DEPRECATION")
    private fun Location.isMockFix(): Boolean =
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) isMock else isFromMockProvider

    @Suppress("MissingPermission")
    private fun latestKnownLocation(): Location? {
        val manager = context.getSystemService(LocationManager::class.java) ?: return null
        return manager.getProviders(true)
            // exception:exempt punch location is best-effort; unavailable providers fall back to null.
            .mapNotNull { provider -> runCatching { manager.getLastKnownLocation(provider) }.getOrNull() }
            .maxByOrNull { it.time }
    }

    @Suppress("MissingPermission", "DEPRECATION")
    private suspend fun freshLocation(): Location? {
        val manager = context.getSystemService(LocationManager::class.java) ?: return null
        val providers = listOf(LocationManager.GPS_PROVIDER, LocationManager.NETWORK_PROVIDER)
            // exception:exempt provider availability is best-effort; disabled/errors are skipped.
            .filter { provider -> runCatching { manager.isProviderEnabled(provider) }.getOrDefault(false) }
        if (providers.isEmpty()) return null

        // Location is MANDATORY for a punch (maintainer decision 2026-08-29), so a
        // fix that arrives late is worth waiting for: 8s covers a cold GPS start
        // while the network provider usually answers in well under one. On timeout
        // the last-known fix still counts; only a phone with neither is refused.
        return withTimeoutOrNull(8_000) {
            suspendCancellableCoroutine { continuation ->
                var resumed = false
                val listeners = mutableListOf<LocationListener>()
                fun finish(location: Location?) {
                    if (resumed) return
                    resumed = true
                    listeners.forEach { listener -> runCatching { manager.removeUpdates(listener) } }
                    continuation.resume(location)
                }
                providers.forEach { provider ->
                    val listener = object : LocationListener {
                        override fun onLocationChanged(location: Location) = finish(location)
                        override fun onProviderDisabled(provider: String) = Unit
                        override fun onProviderEnabled(provider: String) = Unit
                        override fun onStatusChanged(provider: String?, status: Int, extras: Bundle?) = Unit
                    }
                    listeners += listener
                    runCatching {
                        manager.requestSingleUpdate(provider, listener, Looper.getMainLooper())
                    }.onFailure {
                        listeners.remove(listener)
                        runCatching { manager.removeUpdates(listener) }
                        if (listeners.isEmpty()) finish(null)
                    }
                }
                continuation.invokeOnCancellation {
                    listeners.forEach { listener -> runCatching { manager.removeUpdates(listener) } }
                }
            }
        }
    }

    @Suppress("DEPRECATION")
    private fun reverseGeocode(location: Location): String? =
        // exception:exempt reverse geocode is optional context; lat/long already captured.
        runCatching {
            val geocoder = Geocoder(context, Locale.getDefault())
            val addresses = geocoder.getFromLocation(location.latitude, location.longitude, 1).orEmpty()
            addresses.firstOrNull()?.let { address ->
                buildString {
                    listOfNotNull(
                        address.thoroughfare,
                        address.subLocality,
                        address.locality,
                        address.adminArea,
                    ).distinct().forEachIndexed { index, part ->
                        if (index > 0) append(", ")
                        append(part)
                    }
                }.ifBlank { null }
            }
        }.getOrNull()

    // --- integrity --------------------------------------------------------------------------

    /**
     * Installed packages whose REQUESTED permissions include ACCESS_MOCK_LOCATION. The scan runs
     * inside every punch tap, so installing a fake-GPS app after a clean first day still blocks
     * the next punch. A scan failure is reported (non-fatal) and treated as "none found" — the
     * SERVER still refuses a mock fix independently, so a broken scan cannot open a bypass for a
     * mock-provided location.
     */
    private fun scanMockProviderApps(): List<MockProviderApp> = try {
        val pm = context.packageManager
        val installed = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            pm.getInstalledPackages(PackageManager.PackageInfoFlags.of(PackageManager.GET_PERMISSIONS.toLong()))
        } else {
            @Suppress("DEPRECATION")
            pm.getInstalledPackages(PackageManager.GET_PERMISSIONS)
        }
        installed.mapNotNull { pkg ->
            val requestsMock = pkg.requestedPermissions?.any { it == ACCESS_MOCK_LOCATION } == true
            if (!requestsMock || pkg.packageName == context.packageName) return@mapNotNull null
            val appInfo: ApplicationInfo? = pkg.applicationInfo
            MockProviderApp(
                packageName = pkg.packageName,
                label = appInfo?.loadLabel(pm)?.toString() ?: pkg.packageName,
            )
        }
    } catch (t: Exception) {
        crashReporter.recordException(t, "clock mock-provider package scan failed")
        emptyList()
    }

    private fun developerOptionsEnabled(): Boolean = try {
        Settings.Global.getInt(context.contentResolver, Settings.Global.DEVELOPMENT_SETTINGS_ENABLED, 0) == 1
    } catch (t: Exception) {
        crashReporter.recordException(t, "clock developer-options read failed")
        false
    }

    // --- device context ---------------------------------------------------------------------

    private fun batteryPct(): Int? = try {
        val manager = context.getSystemService(BatteryManager::class.java)
        manager?.getIntProperty(BatteryManager.BATTERY_PROPERTY_CAPACITY)?.takeIf { it in 0..100 }
    } catch (t: Exception) {
        crashReporter.recordException(t, "clock battery read failed")
        null
    }

    /** `wifi` | `cellular` on a VALIDATED network; null = offline (queues through the outbox). */
    private fun currentNetworkKind(): String? = try {
        val manager = context.getSystemService(ConnectivityManager::class.java)
        val capabilities = manager?.getNetworkCapabilities(manager.activeNetwork)
        when {
            capabilities == null -> null
            !capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED) -> null
            capabilities.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> "wifi"
            capabilities.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> "cellular"
            else -> null
        }
    } catch (t: Exception) {
        crashReporter.recordException(t, "clock network-kind read failed")
        null
    }

    private companion object {
        const val ACCESS_MOCK_LOCATION = "android.permission.ACCESS_MOCK_LOCATION"
    }
}
