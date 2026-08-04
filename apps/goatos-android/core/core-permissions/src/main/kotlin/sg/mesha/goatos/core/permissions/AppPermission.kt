package sg.mesha.goatos.core.permissions

import android.Manifest
import android.os.Build

/**
 * The fixed catalog of runtime permissions Goat OS Android requests, each OS-version
 * gated by [minSdkInt]. Login never requests these before the backend role is known.
 * Operator capture routes use their own mandatory gate; denying one here only degrades
 * its matching capability and never locks a verifier or leader out of the app.
 *
 * Sources (build to these, not an invented matrix):
 *  - docs/mobile/rfid-keyboard-reader.md — "Android Version And Permissions" +
 *    "V1 should avoid in-app Bluetooth discovery unless product explicitly needs a
 *    branded pairing wizard". The keyboard-wedge reader is read via `InputManager`
 *    (no permission) plus `BluetoothAdapter.getBondedDevices()` / ACL broadcasts as a
 *    secondary signal, which is what needs runtime `BLUETOOTH_CONNECT` on API 31+.
 *    V1 never calls `startDiscovery()`/BLE scan, so `BLUETOOTH_SCAN` is deliberately not
 *    part of this login-time catalog. LOCATION is mandatory for operators (maintainer 2026-08-02).
 *  - docs/mobile/trd-operator-mobile.md §7 — CameraX proof capture, FCM alerts.
 */
// Permission constants added after minSdk (BLUETOOTH_CONNECT API 31, POST_NOTIFICATIONS
// API 33) are referenced here as plain catalog data, gated at USE time by minSdkInt via
// requiredForSdkInt() rather than by an inline SDK-check next to each literal.
@Suppress("InlinedApi")
enum class AppPermission(
    val manifestPermission: String,
    val minSdkInt: Int,
    val optional: Boolean,
    /** Highest SDK on which this permission is declared in the manifest and can therefore be
     *  granted. Requesting past it can never succeed, which would strand the operator behind a
     *  mandatory gate forever (seen live: ACCESS_FINE_LOCATION is capped at 30, so on Android 13
     *  `pm grant` silently fails and the app is unusable). Int.MAX_VALUE = no cap. */
    val maxSdkInt: Int = Int.MAX_VALUE,
) {
    /** CameraX proof capture (shed-record submit video/photo evidence). Needed on every
     *  supported OS version (minSdk 29). */
    CAMERA(
        manifestPermission = Manifest.permission.CAMERA,
        minSdkInt = 0,
        optional = true,
    ),

    /** "Nearby devices" — Android 12+ runtime permission the RFID readiness check needs
     *  to read bonded-device state (`BluetoothAdapter.getBondedDevices()`) and ACL
     *  broadcasts. Pairing itself stays in system Bluetooth settings (no in-app scan). */
    BLUETOOTH_CONNECT(
        manifestPermission = Manifest.permission.BLUETOOTH_CONNECT,
        minSdkInt = Build.VERSION_CODES.S,
        optional = true,
    ),

    /** Obligation/escalation push alerts rendered from FCM. Runtime-requestable only on
     *  Android 13+ (TIRAMISU); pre-33 devices receive notifications without a prompt. */
    NOTIFICATIONS(
        manifestPermission = Manifest.permission.POST_NOTIFICATIONS,
        minSdkInt = Build.VERSION_CODES.TIRAMISU,
        optional = true,
    ),

    /** Location for operator field work (maintainer directive 2026-08-02). NOTE the manifest
     *  declares ACCESS_FINE_LOCATION with android:maxSdkVersion="30": from Android 12 the RFID
     *  reader scans with BLUETOOTH_SCAN android:usesPermissionFlags="neverForLocation", so
     *  location is neither needed nor grantable there. Capping it here keeps the mandatory gate
     *  satisfiable on modern phones instead of locking the operator out. */
    LOCATION(
        manifestPermission = Manifest.permission.ACCESS_FINE_LOCATION,
        minSdkInt = 0,
        optional = true,
        maxSdkInt = Build.VERSION_CODES.R,
    ),
    ;

    companion object {
        /** The OS-appropriate subset of [AppPermission] to request/display on the given
         *  (default: current) device — this IS the OS-version-aware gate. */
        fun requiredForSdkInt(sdkInt: Int = Build.VERSION.SDK_INT): List<AppPermission> =
            entries.filter { sdkInt >= it.minSdkInt && sdkInt <= it.maxSdkInt }
    }
}

/**
 * Raw manifest-permission strings for the OS-appropriate required set, ready to hand
 * straight to `ActivityResultContracts.RequestMultiplePermissions()`.
 */
fun requiredPermissions(sdkInt: Int = Build.VERSION.SDK_INT): List<String> =
    AppPermission.requiredForSdkInt(sdkInt).map { it.manifestPermission }
