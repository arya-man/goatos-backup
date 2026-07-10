package sg.mesha.goatos.auth

import android.content.Context
import androidx.credentials.CredentialManager
import androidx.credentials.CustomCredential
import androidx.credentials.GetCredentialRequest
import com.google.android.gms.tasks.Tasks
import com.google.android.libraries.identity.googleid.GetGoogleIdOption
import com.google.android.libraries.identity.googleid.GoogleIdTokenCredential
import com.google.firebase.auth.FirebaseAuth
import com.google.firebase.auth.GoogleAuthProvider
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import javax.inject.Inject

/**
 * Google's web OAuth server client id for the goatos-stg Firebase project. It is public
 * OAuth client metadata, not a secret.
 */
private const val GOOGLE_WEB_CLIENT_ID =
    "514832198871-vjnkll058jgr2ee1qkn7aclsuq7017fb.apps.googleusercontent.com"

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
    fun signOut()
}

class FirebaseAuthRepository @Inject constructor() : AuthRepository {

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
        val credentialManager = CredentialManager.create(activityContext)
        val googleIdOption = GetGoogleIdOption.Builder()
            .setFilterByAuthorizedAccounts(false)
            .setServerClientId(GOOGLE_WEB_CLIENT_ID)
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
            Tasks.await(firebaseAuth.sendPasswordResetEmail(email))
        }
        Unit
    }

    override suspend fun currentIdToken(forceRefresh: Boolean): String? {
        val user = firebaseAuth.currentUser ?: return null
        return withContext(Dispatchers.IO) {
            runCatching { Tasks.await(user.getIdToken(forceRefresh))?.token }.getOrNull()
        }
    }

    override fun signOut() {
        runCatching { firebaseAuth.signOut() }
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
