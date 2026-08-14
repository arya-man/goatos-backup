package sg.mesha.goatos.feature.auth

import android.Manifest
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
@Config(sdk = [Build.VERSION_CODES.TIRAMISU])
class RoleBasedPermissionGateTest {

    @Test
    fun `operator requires full proof bundle, bluetooth, and notifications`() {
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

        // Operator requires the full proof bundle at startup.
        assertTrue(required.contains(AppPermission.NOTIFICATIONS.manifestPermission))
        assertTrue(required.contains(Manifest.permission.CAMERA))
        assertTrue(required.contains(Manifest.permission.RECORD_AUDIO))
        assertTrue(required.contains(Manifest.permission.ACCESS_FINE_LOCATION))
        assertTrue(required.contains(AppPermission.BLUETOOTH_CONNECT.manifestPermission))
        assertTrue(required.contains(AppPermission.BLUETOOTH_SCAN.manifestPermission))
        assertFalse(required.contains(Manifest.permission.ACCESS_COARSE_LOCATION))
        assertEquals(6, required.size)
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
    fun `weighing operator requires full proof bundle, bluetooth, and notifications`() {
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

        // Weighing operator requires the same proof/RFID bundle up front.
        assertTrue(required.contains(AppPermission.NOTIFICATIONS.manifestPermission))
        assertTrue(required.contains(Manifest.permission.CAMERA))
        assertTrue(required.contains(Manifest.permission.RECORD_AUDIO))
        assertTrue(required.contains(Manifest.permission.ACCESS_FINE_LOCATION))
        assertTrue(required.contains(AppPermission.BLUETOOTH_CONNECT.manifestPermission))
        assertTrue(required.contains(AppPermission.BLUETOOTH_SCAN.manifestPermission))
        assertFalse(required.contains(Manifest.permission.ACCESS_COARSE_LOCATION))
        assertEquals(6, required.size)
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
    @Config(sdk = [Build.VERSION_CODES.R]) // SDK 30, before BLUETOOTH_CONNECT was added
    fun `operator on pre-api31 requires proof bundle but not bluetooth or runtime notifications`() {
        val navState = NavState(
            chrome = NavChrome.EXPANDED,
            items = emptyList(),
            modules = emptyList(),
            featureFlags = mapOf("vaccination_execute" to true),
        )

        val required = deriveRequiredPermissions(navState)

        assertTrue(required.contains(Manifest.permission.CAMERA))
        assertTrue(required.contains(Manifest.permission.RECORD_AUDIO))
        assertTrue(required.contains(Manifest.permission.ACCESS_FINE_LOCATION))
        assertFalse(required.contains(Manifest.permission.ACCESS_COARSE_LOCATION))
        assertFalse(required.contains(AppPermission.NOTIFICATIONS.manifestPermission))
        assertFalse(required.contains(AppPermission.BLUETOOTH_CONNECT.manifestPermission))
        assertFalse(required.contains(AppPermission.BLUETOOTH_SCAN.manifestPermission))
        assertEquals(3, required.size)
    }

    @Test
    @Config(sdk = [Build.VERSION_CODES.S])
    fun `operator on api31 and api32 requires proof bundle and bluetooth but not runtime notifications`() {
        val navState = NavState(
            chrome = NavChrome.EXPANDED,
            items = emptyList(),
            modules = emptyList(),
            featureFlags = mapOf("vaccination_execute" to true),
        )

        val required = deriveRequiredPermissions(navState)

        assertTrue(required.contains(Manifest.permission.CAMERA))
        assertTrue(required.contains(Manifest.permission.RECORD_AUDIO))
        assertTrue(required.contains(Manifest.permission.ACCESS_FINE_LOCATION))
        assertTrue(required.contains(AppPermission.BLUETOOTH_CONNECT.manifestPermission))
        assertTrue(required.contains(AppPermission.BLUETOOTH_SCAN.manifestPermission))
        assertFalse(required.contains(Manifest.permission.ACCESS_COARSE_LOCATION))
        assertFalse(required.contains(AppPermission.NOTIFICATIONS.manifestPermission))
        assertEquals(5, required.size)
    }

    @Test
    fun `precise location request includes coarse without making coarse a blocker`() {
        assertEquals(
            listOf(
                Manifest.permission.ACCESS_COARSE_LOCATION,
                Manifest.permission.ACCESS_FINE_LOCATION,
            ),
            requestPermissionsFor(setOf(Manifest.permission.ACCESS_FINE_LOCATION)),
        )
    }
}
