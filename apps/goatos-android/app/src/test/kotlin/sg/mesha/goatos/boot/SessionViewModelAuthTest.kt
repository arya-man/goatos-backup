package sg.mesha.goatos.boot

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import sg.mesha.goatos.feature.auth.LoginError
import java.io.IOException
import java.util.Base64

class SessionViewModelAuthTest {

    @Test
    fun `dev flavor routes to the local bearer path`() {
        assertEquals(AuthMode.DEV_BEARER, authModeForFlavor("dev"))
    }

    @Test
    fun `a reinstalled dev APK replaces the previous local role token`() {
        assertEquals(true, devSessionNeedsRefresh(AuthMode.DEV_BEARER, "operator-token", "verifier-token"))
        assertEquals(true, devSessionNeedsWipe(AuthMode.DEV_BEARER, "operator-token", "verifier-token"))
        assertEquals(true, devSessionNeedsRefresh(AuthMode.DEV_BEARER, "operator-token", ""))
        assertEquals(true, devSessionNeedsWipe(AuthMode.DEV_BEARER, "operator-token", ""))
        assertEquals(false, devSessionNeedsRefresh(AuthMode.DEV_BEARER, "verifier-token", "verifier-token"))
        assertEquals(false, devSessionNeedsWipe(AuthMode.DEV_BEARER, "verifier-token", "verifier-token"))
        assertEquals(false, devSessionNeedsRefresh(AuthMode.FIREBASE, "firebase-session", "dev-token"))
        assertEquals(false, devSessionNeedsWipe(AuthMode.FIREBASE, "firebase-session", "dev-token"))
    }

    @Test
    fun `rotating dev token for same principal refreshes token without wiping local evidence`() {
        val oldToken = unsignedDevJwt("tenant-1", "operator-1", issuedAt = 1)
        val newToken = unsignedDevJwt("tenant-1", "operator-1", issuedAt = 2)

        assertEquals("tenant-1:operator-1", devBearerPrincipalKey(oldToken))
        assertEquals(true, devSessionNeedsRefresh(AuthMode.DEV_BEARER, oldToken, newToken))
        assertEquals(false, devSessionNeedsWipe(AuthMode.DEV_BEARER, oldToken, newToken))
    }

    @Test
    fun `rotating dev token for different principal still wipes local evidence`() {
        val oldToken = unsignedDevJwt("tenant-1", "operator-1", issuedAt = 1)
        val newToken = unsignedDevJwt("tenant-1", "verifier-1", issuedAt = 2)

        assertEquals(true, devSessionNeedsRefresh(AuthMode.DEV_BEARER, oldToken, newToken))
        assertEquals(true, devSessionNeedsWipe(AuthMode.DEV_BEARER, oldToken, newToken))
    }

    @Test
    fun `firebase flavor ignores stale dev bearer left by previous APK`() {
        assertEquals(true, sessionIsAuthedForMode(AuthMode.DEV_BEARER, "operator-token"))
        assertEquals(true, sessionIsAuthedForMode(AuthMode.FIREBASE, FIREBASE_SESSION_MARKER))
        assertEquals(false, sessionIsAuthedForMode(AuthMode.FIREBASE, "operator-token"))
        assertEquals(false, sessionIsAuthedForMode(AuthMode.FIREBASE, null))
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

    private fun unsignedDevJwt(tenantId: String, subject: String, issuedAt: Int): String {
        fun enc(raw: String): String = Base64.getUrlEncoder().withoutPadding()
            .encodeToString(raw.toByteArray(Charsets.UTF_8))
        return listOf(
            enc("""{"alg":"none"}"""),
            enc("""{"tenant_id":"$tenantId","sub":"$subject","iat":$issuedAt}"""),
            "",
        ).joinToString(".")
    }
}
