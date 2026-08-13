package sg.mesha.goatos.boot

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import sg.mesha.goatos.core.datastore.DataStoreDeviceStore

/**
 * Proof for the multi-device analytics requirement (defect A + the logout/re-login crux): the
 * `device_id` stamped on every analytics event by [sg.mesha.goatos.core.analytics.
 * FirebaseAnalyticsAdapter.track] must be generated ONCE per install and stay identical for the
 * whole install lifetime -- across every later cold start (simulated restart), and across a
 * different operator logging in on the same physical device.
 *
 * These tests exercise the real [DataStoreDeviceStore] (not [sg.mesha.goatos.core.datastore.
 * FakeDeviceStore], which trivially can't disagree with itself) against Robolectric's persisted
 * SharedPreferences/DataStore-backed app context, so a fresh store INSTANCE reading the same
 * on-disk state -- exactly what happens across a real process death -- is what's asserted, not
 * just in-memory field reuse within one object.
 */
@RunWith(RobolectricTestRunner::class)
class DeviceStoreIdentityStabilityTest {

    private val context: Context get() = ApplicationProvider.getApplicationContext()

    @Test
    fun `sync device id is stable across simulated app restarts`() {
        val firstLaunch = DataStoreDeviceStore(context)
        val idAtFirstLaunch = firstLaunch.appInstallIdSync()

        // A new DataStoreDeviceStore instance reading the same on-disk SharedPreferences/DataStore
        // simulates the process being killed and relaunched (GoatOsApplication.onCreate runs again
        // with a fresh DI graph, but the same disk state).
        val afterRestart = DataStoreDeviceStore(context)
        val idAfterRestart = afterRestart.appInstallIdSync()

        assertEquals(
            "device_id must be identical across a simulated restart, not regenerated",
            idAtFirstLaunch,
            idAfterRestart,
        )
    }

    @Test
    fun `suspend and sync device id accessors converge on the same value`() = runTest {
        val store = DataStoreDeviceStore(context)

        // Application.onCreate resolves the sync id first (before any coroutine is guaranteed to
        // run); the bootstrap path later calls the suspend accessor as a fallback. They must never
        // mint two different values for the same install.
        val syncId = store.appInstallIdSync()
        val suspendId = store.appInstallId()

        assertEquals(
            "the synchronous and suspend device-id accessors must read-through to one value",
            syncId,
            suspendId,
        )
    }

    @Test
    fun `re-login as a different user on the same device keeps device_id but rotates journey_id`() = runTest {
        val store = DataStoreDeviceStore(context)
        val deviceIdBeforeLogout = store.appInstallIdSync()
        val journeyIdBeforeLogout = store.journeyId()

        // Logout clean-slate wipe (LogoutCoordinator calls DeviceStore.clear()).
        store.clear()

        val deviceIdAfterLogout = store.appInstallIdSync()
        val journeyIdAfterLogin = store.journeyId()

        assertEquals(
            "the device did not change just because the signed-in operator did",
            deviceIdBeforeLogout,
            deviceIdAfterLogout,
        )
        assertNotEquals(
            "a new operator's work session must get its own journey id, not inherit the departed one",
            journeyIdBeforeLogout,
            journeyIdAfterLogin,
        )
    }
}
