package sg.mesha.goatos.capture

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.location.Geocoder
import android.location.Location
import android.location.LocationListener
import android.location.LocationManager
import android.os.Bundle
import android.os.Looper
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.coroutines.withContext
import sg.mesha.goatos.core.data.capture.ProofLocationProvider
import sg.mesha.goatos.core.data.capture.ProofLocationSnapshot
import java.util.Locale
import javax.inject.Inject
import javax.inject.Singleton
import kotlin.coroutines.resume

@Singleton
class AppProofLocationProvider @Inject constructor(
    @ApplicationContext private val context: Context,
) : ProofLocationProvider {
    override suspend fun snapshot(): ProofLocationSnapshot = withContext(Dispatchers.IO) {
        if (!hasFineLocation()) {
            return@withContext ProofLocationSnapshot(
                locationStatus = "permission_missing",
                geocoderStatus = "skipped",
            )
        }

        val location = freshLocation() ?: latestKnownLocation()
            ?: return@withContext ProofLocationSnapshot(
                locationStatus = "unavailable",
                geocoderStatus = "skipped",
            )

        val address = reverseGeocode(location)
        ProofLocationSnapshot(
            locationStatus = "captured",
            latitude = location.latitude,
            longitude = location.longitude,
            gpsAccuracyM = if (location.hasAccuracy()) location.accuracy.toDouble() else null,
            geocoderStatus = if (address.isNullOrBlank()) "unavailable" else "resolved",
            address = address,
        )
    }

    private fun hasFineLocation(): Boolean =
        context.checkSelfPermission(Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED

    @Suppress("MissingPermission")
    private fun latestKnownLocation(): Location? {
        val manager = context.getSystemService(LocationManager::class.java) ?: return null
        return manager.getProviders(true)
            // exception:exempt proof overlay location is best-effort; unavailable providers fall back to null.
            .mapNotNull { provider -> runCatching { manager.getLastKnownLocation(provider) }.getOrNull() }
            .maxByOrNull { it.time }
    }

    @Suppress("MissingPermission", "DEPRECATION")
    private suspend fun freshLocation(): Location? {
        val manager = context.getSystemService(LocationManager::class.java) ?: return null
        val providers = listOf(LocationManager.GPS_PROVIDER, LocationManager.NETWORK_PROVIDER)
            // exception:exempt provider availability is best-effort overlay metadata; disabled/errors are skipped.
            .filter { provider -> runCatching { manager.isProviderEnabled(provider) }.getOrDefault(false) }
        if (providers.isEmpty()) return null

        return withTimeoutOrNull(3500) {
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
        // exception:exempt reverse geocode is optional overlay text; lat/long fallback covers lookup failures.
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
}
