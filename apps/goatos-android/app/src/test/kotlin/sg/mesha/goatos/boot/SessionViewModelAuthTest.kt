package sg.mesha.goatos.boot

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import sg.mesha.goatos.feature.auth.LoginError
import java.io.IOException

class SessionViewModelAuthTest {

    @Test
    fun `dev flavor routes to the local bearer path`() {
        assertEquals(AuthMode.DEV_BEARER, authModeForFlavor("dev"))
    }

    @Test
    fun `a reinstalled dev APK replaces the previous local role token`() {
        assertEquals(true, devSessionNeedsRefresh(AuthMode.DEV_BEARER, "operator-token", "verifier-token"))
        assertEquals(true, devSessionNeedsRefresh(AuthMode.DEV_BEARER, "operator-token", ""))
        assertEquals(false, devSessionNeedsRefresh(AuthMode.DEV_BEARER, "verifier-token", "verifier-token"))
        assertEquals(false, devSessionNeedsRefresh(AuthMode.FIREBASE, "firebase-session", "dev-token"))
    }

    @Test
    fun `stg flavor routes to Firebase`() {
        assertEquals(AuthMode.FIREBASE, authModeForFlavor("stg"))
    }

    @Test
    fun `prod flavor routes to Firebase`() {
        assertEquals(AuthMode.FIREBASE, authModeForFlavor("prod"))
    }

    @Test
    fun `IOException classifies as network`() {
        val (reason, detail) = classifyAuthError(IOException("timeout"))
        assertEquals(LoginError.NETWORK, reason)
        assertNull(detail)
    }

    @Test
    fun `too many requests message classifies distinctly`() {
        val (reason, detail) = classifyAuthError(RuntimeException("The user has attempted too-many-requests recently"))
        assertEquals(LoginError.TOO_MANY_REQUESTS, reason)
        assertNull(detail)
    }

    @Test
    fun `unrecognized failure never exposes raw provider text`() {
        val (reason, detail) = classifyAuthError(RuntimeException("boom"))
        assertEquals(LoginError.UNKNOWN, reason)
        assertNull(detail)
    }

    @Test
    fun `wrapped invalid credential failure stays operator safe`() {
        val wrapped = java.util.concurrent.ExecutionException(
            "com.google.firebase.auth.FirebaseAuthInvalidCredentialsException: " +
                "The supplied auth credential is incorrect, malformed or has expired.",
            RuntimeException("The supplied auth credential is incorrect, malformed or has expired."),
        )

        val (reason, detail) = classifyAuthError(wrapped)

        assertEquals(LoginError.INVALID_CREDENTIALS, reason)
        assertNull(detail)
    }
}
