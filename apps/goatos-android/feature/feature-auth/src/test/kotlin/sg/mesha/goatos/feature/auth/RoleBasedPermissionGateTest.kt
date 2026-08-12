package sg.mesha.goatos.feature.auth

import android.os.Build
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.model.nav.NavChrome
import sg.mesha.goatos.core.model.nav.NavItem
import sg.mesha.goatos.core.model.nav.NavModule
import sg.mesha.goatos.core.model.nav.NavModuleStatus
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.permissions.AppPermission
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

// telemetry:exempt unit test of permission derivation; emits no user-facing surface.

@RunWith(RobolectricTestRunner::class)
class RoleBasedPermissionGateTest {

    @Test
    @Config(sdk = [Build.VERSION_CODES.S])
    fun `operator on android 12 requires camera microphone and bluetooth`() {
        val navState = NavState(
            chrome = NavChrome.EXPANDED,
            items = emptyList(),
            modules = listOf(
                NavModule(
                    key = "vaccination",
                    label = "Vaccination",
                    href = "/vaccination",
                    status = NavModuleStatus.AVAILABLE,
                    navItems = emptyList(),
                ),
            ),
            featureFlags = mapOf(
                "vaccination_execute" to true, // Operator has execute permissions
            ),
        )

        val required = deriveRequiredPermissions(navState)

        // Android 12+ operator requires proof capture and RFID permissions. Location is capped
        // before Android 12; notifications start at Android 13.
        assertTrue(required.contains(AppPermission.CAMERA.manifestPermission))
        assertTrue(required.contains(AppPermission.MICROPHONE.manifestPermission))
        assertTrue(required.contains(AppPermission.APPROXIMATE_LOCATION.manifestPermission))
        assertTrue(required.contains(AppPermission.PRECISE_LOCATION.manifestPermission))
        assertTrue(required.contains(AppPermission.BLUETOOTH_CONNECT.manifestPermission))
        assertFalse(required.contains(AppPermission.NOTIFICATIONS.manifestPermission))
        assertEquals(5, required.size)
    }

    @Test
    @Config(sdk = [Build.VERSION_CODES.S])
    fun `verifier on android 12 requires no runtime permissions`() {
        val navState = NavState(
            chrome = NavChrome.EXPANDED,
            items = emptyList(),
            modules = listOf(
                NavModule(
                    key = "verify",
                    label = "Verify",
                    href = "/verify",
                    status = NavModuleStatus.AVAILABLE,
                    navItems = emptyList(),
                ),
            ),
            featureFlags = mapOf(
                "vaccination_execute" to false,
                "weighing_execute" to false,
            ),
        )

        val required = deriveRequiredPermissions(navState)

        // Android 12 has no runtime notification permission.
        assertFalse(required.contains(AppPermission.NOTIFICATIONS.manifestPermission))
        assertFalse(required.contains(AppPermission.CAMERA.manifestPermission))
        assertFalse(required.contains(AppPermission.MICROPHONE.manifestPermission))
        assertFalse(required.contains(AppPermission.BLUETOOTH_CONNECT.manifestPermission))
        assertEquals(0, required.size)
    }

    @Test
    @Config(sdk = [Build.VERSION_CODES.S])
    fun `weighing operator on android 12 requires camera microphone and bluetooth`() {
        val navState = NavState(
            chrome = NavChrome.EXPANDED,
            items = emptyList(),
            modules = emptyList(),
            featureFlags = mapOf(
                "weighing_execute" to true, // Weighing operator
                "vaccination_execute" to false,
            ),
        )

        val required = deriveRequiredPermissions(navState)

        // Weighing proof capture is also video+audio.
        assertTrue(required.contains(AppPermission.CAMERA.manifestPermission))
        assertTrue(required.contains(AppPermission.MICROPHONE.manifestPermission))
        assertTrue(required.contains(AppPermission.APPROXIMATE_LOCATION.manifestPermission))
        assertTrue(required.contains(AppPermission.PRECISE_LOCATION.manifestPermission))
        assertTrue(required.contains(AppPermission.BLUETOOTH_CONNECT.manifestPermission))
        assertFalse(required.contains(AppPermission.NOTIFICATIONS.manifestPermission))
        assertEquals(5, required.size)
    }

    @Test
    @Config(sdk = [Build.VERSION_CODES.S])
    fun `director requires only notifications`() {
        val navState = NavState(
            chrome = NavChrome.EXPANDED,
            items = emptyList(),
            modules = listOf(
                NavModule(
                    key = "control_tower",
                    label = "Control Tower",
                    href = "/control-tower",
                    status = NavModuleStatus.AVAILABLE,
                    navItems = emptyList(),
                ),
            ),
            featureFlags = mapOf(
                "vaccination_execute" to false,
                "weighing_execute" to false,
            ),
        )

        val required = deriveRequiredPermissions(navState)

        // Android 12 has no runtime notification permission.
        assertFalse(required.contains(AppPermission.NOTIFICATIONS.manifestPermission))
        assertEquals(0, required.size)
    }

    @Test
    @Config(sdk = [Build.VERSION_CODES.S])
    fun `no permissions required when empty feature flags on android 12`() {
        val navState = NavState(
            chrome = NavChrome.MINIMAL,
            items = emptyList(),
            modules = emptyList(),
            featureFlags = emptyMap(),
        )

        val required = deriveRequiredPermissions(navState)

        assertTrue(required.isEmpty())
    }

    @Test
    @Config(sdk = [Build.VERSION_CODES.R]) // SDK 30, before BLUETOOTH_CONNECT was added
    fun `operator on pre-api31 does not require bluetooth`() {
        val navState = NavState(
            chrome = NavChrome.EXPANDED,
            items = emptyList(),
            modules = emptyList(),
            featureFlags = mapOf("vaccination_execute" to true),
        )

        val required = deriveRequiredPermissions(navState)

        // Location, camera, and microphone still required, but not BLUETOOTH_CONNECT.
        // Runtime notifications do not exist before Android 13.
        assertFalse(required.contains(AppPermission.APPROXIMATE_LOCATION.manifestPermission))
        assertTrue(required.contains(AppPermission.PRECISE_LOCATION.manifestPermission))
        assertTrue(required.contains(AppPermission.CAMERA.manifestPermission))
        assertTrue(required.contains(AppPermission.MICROPHONE.manifestPermission))
        assertFalse(required.contains(AppPermission.NOTIFICATIONS.manifestPermission))
        assertFalse(required.contains(AppPermission.BLUETOOTH_CONNECT.manifestPermission))
    }

    @Test
    @Config(sdk = [Build.VERSION_CODES.S])
    fun `permissions list respects SDK minimums`() {
        val navState = NavState(
            chrome = NavChrome.EXPANDED,
            items = emptyList(),
            modules = emptyList(),
            featureFlags = mapOf("vaccination_execute" to true),
        )

        val required = deriveRequiredPermissions(navState)

        // On SDK 31+, BLUETOOTH_CONNECT should be included (added in S)
        val hasBluetoothConnect = required.contains(AppPermission.BLUETOOTH_CONNECT.manifestPermission)
        assertTrue(required.contains(AppPermission.MICROPHONE.manifestPermission))
        assertTrue(required.contains(AppPermission.APPROXIMATE_LOCATION.manifestPermission))
        assertTrue(required.contains(AppPermission.PRECISE_LOCATION.manifestPermission))
        assertEquals(Build.VERSION.SDK_INT >= Build.VERSION_CODES.S, hasBluetoothConnect)
    }

    @Test
    @Config(sdk = [Build.VERSION_CODES.TIRAMISU])
    fun `operator on android 13 adds notifications to capture and bluetooth permissions`() {
        val navState = NavState(
            chrome = NavChrome.EXPANDED,
            items = emptyList(),
            modules = emptyList(),
            featureFlags = mapOf("vaccination_execute" to true),
        )

        val required = deriveRequiredPermissions(navState)

        assertTrue(required.contains(AppPermission.CAMERA.manifestPermission))
        assertTrue(required.contains(AppPermission.MICROPHONE.manifestPermission))
        assertTrue(required.contains(AppPermission.APPROXIMATE_LOCATION.manifestPermission))
        assertTrue(required.contains(AppPermission.PRECISE_LOCATION.manifestPermission))
        assertTrue(required.contains(AppPermission.BLUETOOTH_CONNECT.manifestPermission))
        assertTrue(required.contains(AppPermission.NOTIFICATIONS.manifestPermission))
    }
}
