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
    fun `unrecognized failure falls back to the raw message`() {
        val (reason, detail) = classifyAuthError(RuntimeException("boom"))
        assertEquals(LoginError.UNKNOWN, reason)
        assertEquals("boom", detail)
    }
}
