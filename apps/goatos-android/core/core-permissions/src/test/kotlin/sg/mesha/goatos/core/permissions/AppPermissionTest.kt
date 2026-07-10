package sg.mesha.goatos.core.permissions

import android.Manifest
import android.os.Build
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Verifies the OS-version-aware required set at each SDK boundary named in
 * docs/mobile/rfid-keyboard-reader.md and trd-operator-mobile.md §7: CAMERA on every
 * supported OS (minSdk 29), BLUETOOTH_CONNECT only from API 31 (S), POST_NOTIFICATIONS
 * only from API 33 (TIRAMISU). Never includes BLUETOOTH_SCAN or location (V1 does not
 * scan/discover — see AppPermission's doc comment).
 */
class AppPermissionTest {

    @Test
    fun `android 10 (minSdk 29) requires only camera`() {
        assertEquals(listOf(AppPermission.CAMERA), AppPermission.requiredForSdkInt(29))
        assertEquals(listOf(Manifest.permission.CAMERA), requiredPermissions(29))
    }

    @Test
    fun `android 11 (30) still requires only camera`() {
        assertEquals(listOf(AppPermission.CAMERA), AppPermission.requiredForSdkInt(30))
    }

    @Test
    fun `android 12 (31, S) adds bluetooth connect`() {
        val required = AppPermission.requiredForSdkInt(Build.VERSION_CODES.S)
        assertEquals(listOf(AppPermission.CAMERA, AppPermission.BLUETOOTH_CONNECT), required)
    }

    @Test
    fun `android 12L (32) still has camera + bluetooth connect only`() {
        assertEquals(
            listOf(AppPermission.CAMERA, AppPermission.BLUETOOTH_CONNECT),
            AppPermission.requiredForSdkInt(32),
        )
    }

    @Test
    fun `android 13+ (33, TIRAMISU) adds notifications`() {
        val required = AppPermission.requiredForSdkInt(Build.VERSION_CODES.TIRAMISU)
        assertEquals(
            listOf(AppPermission.CAMERA, AppPermission.BLUETOOTH_CONNECT, AppPermission.NOTIFICATIONS),
            required,
        )
    }

    @Test
    fun `android 16 (36, current target) still requires the same three`() {
        assertEquals(
            listOf(AppPermission.CAMERA, AppPermission.BLUETOOTH_CONNECT, AppPermission.NOTIFICATIONS),
            AppPermission.requiredForSdkInt(36),
        )
    }

    @Test
    fun `every catalog entry is optional (never blocks sign-in)`() {
        AppPermission.entries.forEach { assertEquals(true, it.optional) }
    }

    @Test
    fun `catalog never requests bluetooth scan or location (V1 avoids in-app discovery)`() {
        val allManifestPermissions = AppPermission.entries.map { it.manifestPermission }
        assertEquals(false, allManifestPermissions.contains(Manifest.permission.BLUETOOTH_SCAN))
        assertEquals(false, allManifestPermissions.contains(Manifest.permission.ACCESS_FINE_LOCATION))
        assertEquals(false, allManifestPermissions.contains(Manifest.permission.ACCESS_COARSE_LOCATION))
    }
}
