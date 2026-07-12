package sg.mesha.goatos.core.datastore

import android.content.Context
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map

private val Context.sessionDataStore by preferencesDataStore(name = "goatos_session")
private const val DEFAULT_LANGUAGE_TAG = "en"

/**
 * Session/auth state (session token, language). Two things live under the same key by flavor:
 *  - dev flavor stores the local HS256 dev bearer, which the network interceptor reads and sends
 *    (that token is already baked into the APK, so DataStore adds no exposure);
 *  - stg/prod store only a non-sensitive presence marker (see `FIREBASE_SESSION_MARKER`) — the
 *    live Firebase ID token is re-fetched per request and is NEVER written to disk.
 * So no unencrypted credential is persisted at rest, and a non-blank value simply means "signed
 * in" for the session gate.
 */
interface SessionStore {
    val bearerToken: Flow<String?>
    suspend fun currentToken(): String?
    suspend fun setBearerToken(token: String?)

    val language: Flow<String>
    suspend fun currentLanguage(): String
    suspend fun setLanguage(code: String)

    /** Full wipe (logout clean-slate, C35-001): clears EVERY persisted key in this store —
     *  the session token AND the language preference — so no residual per-user state survives
     *  to the next principal signed in on this device. [setBearerToken] alone only ever
     *  touched the token key. */
    suspend fun clear()
}

class DataStoreSessionStore(
    private val context: Context,
) : SessionStore {

    private object Keys {
        val TOKEN = stringPreferencesKey("bearer_token")
        val LANGUAGE = stringPreferencesKey("language")
    }

    override val bearerToken: Flow<String?> =
        context.sessionDataStore.data.map { it[Keys.TOKEN] }

    override suspend fun currentToken(): String? =
        context.sessionDataStore.data.first()[Keys.TOKEN]

    override suspend fun setBearerToken(token: String?) {
        context.sessionDataStore.edit { prefs ->
            if (token.isNullOrBlank()) prefs.remove(Keys.TOKEN) else prefs[Keys.TOKEN] = token
        }
    }

    override val language: Flow<String> =
        context.sessionDataStore.data.map { normalizeLanguageTag(it[Keys.LANGUAGE]) }

    override suspend fun currentLanguage(): String =
        normalizeLanguageTag(context.sessionDataStore.data.first()[Keys.LANGUAGE])

    override suspend fun setLanguage(code: String) {
        context.sessionDataStore.edit { it[Keys.LANGUAGE] = normalizeLanguageTag(code) }
    }

    override suspend fun clear() {
        context.sessionDataStore.edit { it.clear() }
    }
}

/** In-memory fake for tests/previews. */
class FakeSessionStore : SessionStore {
    private val token = MutableStateFlow<String?>(null)
    private val lang = MutableStateFlow(DEFAULT_LANGUAGE_TAG)
    override val bearerToken: Flow<String?> = token
    override suspend fun currentToken(): String? = token.value
    override suspend fun setBearerToken(token: String?) { this.token.value = token }
    override val language: Flow<String> = lang
    override suspend fun currentLanguage(): String = lang.value
    override suspend fun setLanguage(code: String) { lang.value = normalizeLanguageTag(code) }
    override suspend fun clear() {
        token.value = null
        lang.value = DEFAULT_LANGUAGE_TAG
    }
}

private fun normalizeLanguageTag(code: String?): String =
    code?.trim()?.ifBlank { null } ?: DEFAULT_LANGUAGE_TAG
