package sg.mesha.goatos.core.datastore

import android.content.Context
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.first
import java.util.UUID
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

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

    /**
     * This work session's journey/session id — same generated-once-and-persisted pattern as
     * [appInstallId]. Read on bootstrap (`BootstrapViewModel.applyAnalyticsIdentity`), so it
     * is minted once and then reused across every subsequent bootstrap on the same principal
     * (cold start, config change, process death) until the next [clear] — which is exactly what
     * makes a whole login-to-logout work session reconstructible from one id, unlike Firebase's
     * own 30-minute auto-session.
     */
    suspend fun journeyId(): String

    /**
     * Synchronous counterparts of [appInstallId]/[journeyId], backed by a small SharedPreferences
     * mirror instead of DataStore. DataStore is async-only (its own docs warn against
     * `runBlocking` on the main thread), but `Application.onCreate()` needs these ids available
     * BEFORE any coroutine is guaranteed to run -- a process killed 50ms into launch must not
     * lose the launch-marker events, and they must not be overtaken by whatever the first
     * coroutine happens to log. These generate-and-persist on first-ever call exactly like the
     * suspend versions, and the two are read-through of the same value: [appInstallId]/
     * [journeyId] prefer whatever value the mirror already holds, so a value minted here on the
     * very first launch is the one DataStore later converges to, never a second, different one.
     */
    fun appInstallIdSync(): String
    fun journeyIdSync(): String

    /**
     * Logout clean-slate (C35-001), scoped to what actually changes hands at logout: clears the
     * backend-assigned [deviceId] (so the next principal re-registers a fresh device record) and
     * [journeyId] (so a new work session starts a new journey) plus their persisted state.
     *
     * Deliberately does **NOT** clear [appInstallId]. [appInstallId] identifies this physical
     * Android install, not the signed-in principal -- the same phone handed to a different
     * operator is still the same phone, and the whole reason a stable per-install id exists is so
     * the SAME user's sessions on two different devices (or two different users' sessions on the
     * same shared field device) can be told apart in analytics. Wiping it on every logout would
     * mint a new device identity per login and make that impossible. The next [appInstallId]/
     * [appInstallIdSync] call after [clear] therefore keeps returning the same value it always
     * has; only [deviceId]/[journeyId] regenerate.
     */
    suspend fun clear()
}

class DataStoreDeviceStore(
    private val context: Context,
) : DeviceStore {
    private val identityMutex = Mutex()

    /** SharedPreferences-backed synchronous mirror of [Keys.APP_INSTALL_ID]/[Keys.JOURNEY_ID].
     *  Separate small file (not the DataStore-backed one) so it can be read/written
     *  synchronously from Application.onCreate() without ever touching DataStore's async API. */
    private val syncPrefs by lazy {
        context.getSharedPreferences("goatos_device_sync", Context.MODE_PRIVATE)
    }

    private object Keys {
        val APP_INSTALL_ID = stringPreferencesKey("app_install_id")
        val DEVICE_ID = stringPreferencesKey("device_id")
        val JOURNEY_ID = stringPreferencesKey("journey_id")
    }

    private object SyncKeys {
        const val APP_INSTALL_ID = "app_install_id"
        const val JOURNEY_ID = "journey_id"
    }

    /**
     * Read-then-write is NOT atomic across callers, so the get-or-create runs inside a single
     * [edit] transaction and the whole thing is serialized on [identityMutex].
     *
     * Two callers resolve identity on a cold start -- the Application, and the bootstrap once the
     * session is authenticated -- and they run concurrently. Unsynchronized, both could observe
     * "absent", each mint a different UUID, and the later write would win: the first events of a
     * run would carry an id that no longer matches the persisted one, so the session they were
     * meant to join would be split exactly as before this id existed.
     */
    override suspend fun appInstallId(): String = identityMutex.withLock {
        getOrCreate(Keys.APP_INSTALL_ID, SyncKeys.APP_INSTALL_ID)
    }

    override suspend fun journeyId(): String = identityMutex.withLock {
        getOrCreate(Keys.JOURNEY_ID, SyncKeys.JOURNEY_ID)
    }

    /** Generate-if-absent inside ONE DataStore transaction, so a concurrent caller cannot slip
     *  between the read and the write and mint a second value. Prefers whatever value the
     *  synchronous SharedPreferences mirror already holds ([syncGetOrCreate] may have minted one
     *  from Application.onCreate() before this suspend path ever ran) so the two never disagree;
     *  then writes the resolved value back into the mirror so it always reflects the latest
     *  DataStore-confirmed value too. */
    private suspend fun getOrCreate(
        key: androidx.datastore.preferences.core.Preferences.Key<String>,
        syncKey: String,
    ): String {
        var resolved = ""
        context.deviceDataStore.edit { prefs ->
            val existing = prefs[key]?.takeIf { it.isNotBlank() }
            val mirrored = syncPrefs.getString(syncKey, null)?.takeIf { it.isNotBlank() }
            resolved = existing ?: mirrored ?: UUID.randomUUID().toString()
            prefs[key] = resolved
        }
        syncPrefs.edit().putString(syncKey, resolved).apply()
        return resolved
    }

    override fun appInstallIdSync(): String = syncGetOrCreate(SyncKeys.APP_INSTALL_ID)

    override fun journeyIdSync(): String = syncGetOrCreate(SyncKeys.JOURNEY_ID)

    /** Generate-if-absent against the SharedPreferences mirror only, synchronously. Only writes
     *  disk on the very first-ever call (subsequent calls hit the already-populated read, which
     *  SharedPreferences serves from its in-memory cache) -- an acceptable one-time synchronous
     *  write, unlike DataStore whose API has no synchronous path at all. */
    private fun syncGetOrCreate(syncKey: String): String {
        val existing = syncPrefs.getString(syncKey, null)?.takeIf { it.isNotBlank() }
        if (existing != null) return existing
        val generated = UUID.randomUUID().toString()
        syncPrefs.edit().putString(syncKey, generated).commit()
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
        // Scoped wipe -- see the KDoc on [DeviceStore.clear]: APP_INSTALL_ID (and its sync
        // mirror) is this physical install's identity and must survive logout, so it is
        // deliberately excluded from both the DataStore edit and the SharedPreferences clear.
        context.deviceDataStore.edit { prefs ->
            prefs.remove(Keys.DEVICE_ID)
            prefs.remove(Keys.JOURNEY_ID)
        }
        syncPrefs.edit().remove(SyncKeys.JOURNEY_ID).apply()
    }
}

/** In-memory fake for tests/previews. Both the suspend and sync accessors read/write the same
 *  backing field, so they trivially never disagree (mirroring the production invariant without
 *  needing SharedPreferences in tests). */
class FakeDeviceStore : DeviceStore {
    private var installId: String? = null
    private var device: String? = null
    private var journey: String? = null
    override suspend fun appInstallId(): String =
        installId ?: UUID.randomUUID().toString().also { installId = it }
    override suspend fun journeyId(): String =
        journey ?: UUID.randomUUID().toString().also { journey = it }
    override fun appInstallIdSync(): String =
        installId ?: UUID.randomUUID().toString().also { installId = it }
    override fun journeyIdSync(): String =
        journey ?: UUID.randomUUID().toString().also { journey = it }
    override suspend fun deviceId(): String? = device
    override suspend fun setDeviceId(id: String?) { device = id }
    override suspend fun clear() {
        // Mirrors the production scoped wipe: appInstallId survives logout, device/journey don't.
        device = null
        journey = null
    }
}
