package sg.mesha.goatos.push

import androidx.lifecycle.ViewModel
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject

/**
 * Re-reports this device's push state to the backend on demand.
 *
 * The normal report happens on device register and on every heartbeat (once per bootstrap), which
 * carries both the FCM token and whether this phone will actually show what we send. That leaves
 * one gap: someone switches alerts back on mid-session, either from the alerts gate's prompt or
 * from system settings. Without this, the backend would keep treating the device as push-muted —
 * and correctly refuse to address it — until the next app launch.
 *
 * Reuses [PushTokenSync], the same seam bootstrap and `onNewToken` already use, so there is one
 * report path rather than a second one that can drift.
 */
@HiltViewModel
class DevicePushStateViewModel @Inject constructor(
    private val pushTokenSync: PushTokenSync,
) : ViewModel() {
    fun reportNow() {
        pushTokenSync.syncNow()
    }
}
