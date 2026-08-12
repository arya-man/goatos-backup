package sg.mesha.goatos.core.permissions

import android.Manifest
import android.os.Build
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Verifies the OS-version-aware required set at each SDK boundary named in
 * docs/mobile/rfid-keyboard-reader.md and trd-operator-mobile.md §7: CAMERA and
 * RECORD_AUDIO on every supported OS (minSdk 29), BLUETOOTH_CONNECT only from API
 * 31 (S), POST_NOTIFICATIONS only from API 33 (TIRAMISU). Never includes
 * BLUETOOTH_SCAN (V1 does not scan/discover — see AppPermission's doc comment).
 */
class AppPermissionTest {

    @Test
    fun `android 10 (minSdk 29) requires camera microphone and location`() {
        assertEquals(
            listOf(AppPermission.CAMERA, AppPermission.MICROPHONE, AppPermission.PRECISE_LOCATION),
            AppPermission.requiredForSdkInt(29),
        )
        assertEquals(
            listOf(
                Manifest.permission.CAMERA,
                Manifest.permission.RECORD_AUDIO,
                Manifest.permission.ACCESS_FINE_LOCATION,
            ),
            requiredPermissions(29),
        )
    }

    @Test
    fun `android 11 (30) still requires camera microphone and location`() {
        assertEquals(
            listOf(AppPermission.CAMERA, AppPermission.MICROPHONE, AppPermission.PRECISE_LOCATION),
            AppPermission.requiredForSdkInt(30),
        )
    }

    @Test
    fun `android 12 (31, S) requires camera microphone and bluetooth connect`() {
        val required = AppPermission.requiredForSdkInt(Build.VERSION_CODES.S)
        assertEquals(
            listOf(
                AppPermission.CAMERA,
                AppPermission.MICROPHONE,
                AppPermission.BLUETOOTH_CONNECT,
                AppPermission.APPROXIMATE_LOCATION,
                AppPermission.PRECISE_LOCATION,
            ),
            required,
        )
    }

    @Test
    fun `android 12L (32) still has camera microphone and bluetooth connect only`() {
        assertEquals(
            listOf(
                AppPermission.CAMERA,
                AppPermission.MICROPHONE,
                AppPermission.BLUETOOTH_CONNECT,
                AppPermission.APPROXIMATE_LOCATION,
                AppPermission.PRECISE_LOCATION,
            ),
            AppPermission.requiredForSdkInt(32),
        )
    }

    @Test
    fun `android 13+ (33, TIRAMISU) adds notifications`() {
        val required = AppPermission.requiredForSdkInt(Build.VERSION_CODES.TIRAMISU)
        assertEquals(
            listOf(
                AppPermission.CAMERA,
                AppPermission.MICROPHONE,
                AppPermission.BLUETOOTH_CONNECT,
                AppPermission.NOTIFICATIONS,
                AppPermission.APPROXIMATE_LOCATION,
                AppPermission.PRECISE_LOCATION,
            ),
            required,
        )
    }

    @Test
    fun `android 16 (36, current target) still requires camera microphone bluetooth and notifications`() {
        assertEquals(
            listOf(
                AppPermission.CAMERA,
                AppPermission.MICROPHONE,
                AppPermission.BLUETOOTH_CONNECT,
                AppPermission.NOTIFICATIONS,
                AppPermission.APPROXIMATE_LOCATION,
                AppPermission.PRECISE_LOCATION,
            ),
            AppPermission.requiredForSdkInt(36),
        )
    }

    @Test
    fun `every catalog entry is optional (never blocks sign-in)`() {
        AppPermission.entries.forEach { assertEquals(true, it.optional) }
    }

    @Test
    fun `catalog never requests bluetooth scan and requests accuracy pair on android 12`() {
        val allManifestPermissions = AppPermission.entries.map { it.manifestPermission }
        assertEquals(false, allManifestPermissions.contains(Manifest.permission.BLUETOOTH_SCAN))
        assertEquals(true, AppPermission.requiredForSdkInt(Build.VERSION_CODES.S).contains(AppPermission.APPROXIMATE_LOCATION))
        assertEquals(true, AppPermission.requiredForSdkInt(Build.VERSION_CODES.S).contains(AppPermission.PRECISE_LOCATION))
    }
}
