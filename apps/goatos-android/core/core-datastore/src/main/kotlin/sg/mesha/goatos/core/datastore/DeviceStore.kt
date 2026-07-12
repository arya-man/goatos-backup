package sg.mesha.goatos.core.datastore

import android.content.Context
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.first
import java.util.UUID

private val Context.deviceDataStore by preferencesDataStore(name = "goatos_device")

/**
 * Persists this Android install's device identity so the app can register once and be
 * recognized on later launches:
 *  - [appInstallId] is a stable per-install UUID, generated and persisted on first read.
 *  - [deviceId] is the backend-assigned id returned by device registration; it is sent as
 *    the `device_id` bootstrap query param so the backend returns a registered device_state.
 *
 * (Secure storage — EncryptedSharedPreferences — is tracked as a hardening follow-up over
 * this DataStore baseline, alongside SessionStore.)
 */
interface DeviceStore {
    /** Stable per-install id; generated + persisted on first access. */
    suspend fun appInstallId(): String
    suspend fun deviceId(): String?
    suspend fun setDeviceId(id: String?)

    /** Full wipe (logout clean-slate, C35-001): clears BOTH [appInstallId] and [deviceId] so
     *  the next principal on this device gets a fresh install identity and re-registers a new
     *  device record instead of inheriting the departing user's device binding. The next
     *  [appInstallId] call after this regenerates a brand-new UUID. */
    suspend fun clear()
}

class DataStoreDeviceStore(
    private val context: Context,
) : DeviceStore {

    private object Keys {
        val APP_INSTALL_ID = stringPreferencesKey("app_install_id")
        val DEVICE_ID = stringPreferencesKey("device_id")
    }

    override suspend fun appInstallId(): String {
        val existing = context.deviceDataStore.data.first()[Keys.APP_INSTALL_ID]
        if (!existing.isNullOrBlank()) return existing
        val generated = UUID.randomUUID().toString()
        context.deviceDataStore.edit { it[Keys.APP_INSTALL_ID] = generated }
        return generated
    }

    override suspend fun deviceId(): String? =
        context.deviceDataStore.data.first()[Keys.DEVICE_ID]

    override suspend fun setDeviceId(id: String?) {
        context.deviceDataStore.edit { prefs ->
            if (id.isNullOrBlank()) prefs.remove(Keys.DEVICE_ID) else prefs[Keys.DEVICE_ID] = id
        }
    }

    override suspend fun clear() {
        context.deviceDataStore.edit { it.clear() }
    }
}

/** In-memory fake for tests/previews. */
class FakeDeviceStore : DeviceStore {
    private var installId: String? = null
    private var device: String? = null
    override suspend fun appInstallId(): String =
        installId ?: UUID.randomUUID().toString().also { installId = it }
    override suspend fun deviceId(): String? = device
    override suspend fun setDeviceId(id: String?) { device = id }
    override suspend fun clear() {
        installId = null
        device = null
    }
}
