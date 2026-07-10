package sg.mesha.goatos.core.datastore

import android.content.Context
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map

private val Context.sessionDataStore by preferencesDataStore(name = "goatos_session")

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
    suspend fun setLanguage(code: String)
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
        context.sessionDataStore.data.map { it[Keys.LANGUAGE] ?: "en" }

    override suspend fun setLanguage(code: String) {
        context.sessionDataStore.edit { it[Keys.LANGUAGE] = code }
    }
}

/** In-memory fake for tests/previews. */
class FakeSessionStore : SessionStore {
    private var token: String? = null
    private var lang: String = "en"
    override val bearerToken: Flow<String?> = kotlinx.coroutines.flow.flowOf(token)
    override suspend fun currentToken(): String? = token
    override suspend fun setBearerToken(token: String?) { this.token = token }
    override val language: Flow<String> = kotlinx.coroutines.flow.flowOf(lang)
    override suspend fun setLanguage(code: String) { lang = code }
}
