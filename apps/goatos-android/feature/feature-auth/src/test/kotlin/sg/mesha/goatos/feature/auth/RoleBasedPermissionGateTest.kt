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
@Config(minSdk = Build.VERSION_CODES.S)
class RoleBasedPermissionGateTest {

    @Test
    fun `operator requires location, camera, bluetooth, and notifications`() {
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

        // Operator requires notifications, location, camera, and BLE
        assertTrue(required.contains(AppPermission.NOTIFICATIONS.manifestPermission))
        assertTrue(required.contains(AppPermission.LOCATION.manifestPermission))
        assertTrue(required.contains(AppPermission.CAMERA.manifestPermission))
        assertTrue(required.contains(AppPermission.BLUETOOTH_CONNECT.manifestPermission))
        assertEquals(4, required.size)
    }

    @Test
    fun `verifier requires only notifications`() {
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

        // Verifier requires only notifications
        assertTrue(required.contains(AppPermission.NOTIFICATIONS.manifestPermission))
        assertFalse(required.contains(AppPermission.CAMERA.manifestPermission))
        assertFalse(required.contains(AppPermission.BLUETOOTH_CONNECT.manifestPermission))
        assertEquals(1, required.size)
    }

    @Test
    fun `weighing operator requires location, camera, bluetooth, and notifications`() {
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

        // Weighing operator requires location, camera, BLE, and notifications
        assertTrue(required.contains(AppPermission.NOTIFICATIONS.manifestPermission))
        assertTrue(required.contains(AppPermission.LOCATION.manifestPermission))
        assertTrue(required.contains(AppPermission.CAMERA.manifestPermission))
        assertTrue(required.contains(AppPermission.BLUETOOTH_CONNECT.manifestPermission))
        assertEquals(4, required.size)
    }

    @Test
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

        // Director only gets notifications
        assertTrue(required.contains(AppPermission.NOTIFICATIONS.manifestPermission))
        assertEquals(1, required.size)
    }

    @Test
    fun `no permissions required when empty feature flags`() {
        val navState = NavState(
            chrome = NavChrome.MINIMAL,
            items = emptyList(),
            modules = emptyList(),
            featureFlags = emptyMap(),
        )

        val required = deriveRequiredPermissions(navState)

        // Still requires notifications (all roles)
        assertTrue(required.contains(AppPermission.NOTIFICATIONS.manifestPermission))
    }

    @Test
    @Config(minSdk = Build.VERSION_CODES.R) // SDK 30, before BLUETOOTH_CONNECT was added
    fun `operator on pre-api31 does not require bluetooth`() {
        val navState = NavState(
            chrome = NavChrome.EXPANDED,
            items = emptyList(),
            modules = emptyList(),
            featureFlags = mapOf("vaccination_execute" to true),
        )

        val required = deriveRequiredPermissions(navState)

        // Location, camera, and notifications still required, but not BLUETOOTH_CONNECT
        assertTrue(required.contains(AppPermission.LOCATION.manifestPermission))
        assertTrue(required.contains(AppPermission.CAMERA.manifestPermission))
        assertTrue(required.contains(AppPermission.NOTIFICATIONS.manifestPermission))
        assertFalse(required.contains(AppPermission.BLUETOOTH_CONNECT.manifestPermission))
    }

    @Test
    @Config(minSdk = Build.VERSION_CODES.S)
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
        assertEquals(Build.VERSION.SDK_INT >= Build.VERSION_CODES.S, hasBluetoothConnect)
    }
}
