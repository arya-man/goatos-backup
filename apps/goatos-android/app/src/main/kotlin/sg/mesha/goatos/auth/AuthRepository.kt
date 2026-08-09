package sg.mesha.goatos.auth

import android.content.Context
import androidx.credentials.CredentialManager
import androidx.credentials.CustomCredential
import androidx.credentials.GetCredentialRequest
import com.google.android.gms.tasks.Tasks
import com.google.android.libraries.identity.googleid.GetGoogleIdOption
import com.google.android.libraries.identity.googleid.GoogleIdTokenCredential
import com.google.firebase.auth.ActionCodeSettings
import com.google.firebase.auth.FirebaseAuth
import com.google.firebase.auth.GoogleAuthProvider
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import sg.mesha.goatos.BuildConfig
import sg.mesha.goatos.core.analytics.CrashReporter
import javax.inject.Inject

/**
 * Wraps Firebase Auth for stg/prod. The dev flavor keeps the local HS256 bearer path in
 * [sg.mesha.goatos.boot.SessionViewModel] because the local backend does not validate
 * Firebase ID tokens.
 */
interface AuthRepository {
    suspend fun signInWithEmailPassword(email: String, password: String): Result<Unit>

    /** [activityContext] must be an Activity context because Credential Manager launches UI. */
    suspend fun signInWithGoogle(activityContext: Context): Result<Unit>
    suspend fun sendPasswordReset(email: String): Result<Unit>

    /** Current signed-in user's Firebase ID token, or null if nobody is signed in. */
    suspend fun currentIdToken(forceRefresh: Boolean = false): String?

    /** Current signed-in user's email (Firebase Auth), or null if nobody is signed in / no email. */
    fun currentEmail(): String?

    /** Current signed-in user's Firebase UID, or null if nobody is signed in. */
    fun currentFirebaseUid(): String?

    fun signOut()
}

class FirebaseAuthRepository @Inject constructor(
    private val crashReporter: CrashReporter,
) : AuthRepository {

    private val firebaseAuth: FirebaseAuth
        get() = FirebaseAuth.getInstance()

    override suspend fun signInWithEmailPassword(email: String, password: String): Result<Unit> =
        runCatching {
            withContext(Dispatchers.IO) {
                Tasks.await(firebaseAuth.signInWithEmailAndPassword(email, password))
            }
            Unit
        }

    override suspend fun signInWithGoogle(activityContext: Context): Result<Unit> = runCatching {
        val webClientId = googleWebClientId(activityContext)
        val credentialManager = CredentialManager.create(activityContext)
        val googleIdOption = GetGoogleIdOption.Builder()
            .setFilterByAuthorizedAccounts(false)
            .setServerClientId(webClientId)
            .build()
        val request = GetCredentialRequest.Builder()
            .addCredentialOption(googleIdOption)
            .build()
        val response = credentialManager.getCredential(activityContext, request)
        val credential = response.credential
        check(credential is CustomCredential && credential.type == GoogleIdTokenCredential.TYPE_GOOGLE_ID_TOKEN_CREDENTIAL) {
            "Credential Manager returned an unexpected credential type: ${credential.type}"
        }
        val googleIdTokenCredential = GoogleIdTokenCredential.createFrom(credential.data)
        val firebaseCredential = GoogleAuthProvider.getCredential(googleIdTokenCredential.idToken, null)
        withContext(Dispatchers.IO) {
            Tasks.await(firebaseAuth.signInWithCredential(firebaseCredential))
        }
        Unit
    }

    override suspend fun sendPasswordReset(email: String): Result<Unit> = runCatching {
        withContext(Dispatchers.IO) {
            Tasks.await(firebaseAuth.sendPasswordResetEmail(email, passwordResetActionCodeSettings()))
        }
        Unit
    }

    override suspend fun currentIdToken(forceRefresh: Boolean): String? {
        val user = firebaseAuth.currentUser ?: return null
        return withContext(Dispatchers.IO) {
            runCatching { Tasks.await(user.getIdToken(forceRefresh))?.token }
                .onFailure { crashReporter.recordException(it, "firebase id token refresh failed") }
                .getOrNull()
        }
    }

    override fun currentEmail(): String? = firebaseAuth.currentUser?.email?.ifBlank { null }

    override fun currentFirebaseUid(): String? = firebaseAuth.currentUser?.uid?.ifBlank { null }

    override fun signOut() {
        runCatching { firebaseAuth.signOut() }
            .onFailure { crashReporter.recordException(it, "firebase sign out failed") }
    }

    /**
     * The Google OAuth web client id for THIS flavor's Firebase project, read from the flavor's
     * generated-equivalent `default_web_client_id` string resource (src/<flavor>/res/values/
     * firebase.xml). Resolved by name — the same mechanism `FirebaseApp` uses to read
     * `google_app_id` — so a flavor that ships no Firebase config (e.g. prod until its own
     * project is wired) resolves nothing and this fails closed, instead of silently
     * authenticating against another environment's Firebase project.
     */
    private fun googleWebClientId(context: Context): String {
        val resources = context.resources
        val resId = resources.getIdentifier("default_web_client_id", "string", context.packageName)
        check(resId != 0) {
            "Google sign-in is unavailable: this build flavor ships no Firebase config " +
                "(default_web_client_id missing). Wire this environment's Firebase project first."
        }
        return resources.getString(resId).also {
            check(it.isNotBlank()) { "default_web_client_id is blank for this build flavor." }
        }
    }

    private fun passwordResetActionCodeSettings(): ActionCodeSettings {
        val builder = ActionCodeSettings.newBuilder()
            .setUrl(BuildConfig.AUTH_ACTION_CONTINUE_URL)
            .setHandleCodeInApp(false)
        BuildConfig.AUTH_ACTION_LINK_DOMAIN.trim()
            .takeIf { it.isNotEmpty() }
            ?.let { builder.setLinkDomain(it) }
        return builder.build()
    }
}

/**
 * Synchronous ID-token read for the OkHttp bearer interceptor. The interceptor's provider
 * runs off the main thread, and Firebase only refreshes over the network when the cached
 * token is near expiry.
 */
fun currentFirebaseIdTokenBlocking(): String? {
    val user = runCatching { FirebaseAuth.getInstance().currentUser }.getOrNull() ?: return null
    return runCatching { Tasks.await(user.getIdToken(false))?.token }.getOrNull()
}
