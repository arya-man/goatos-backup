package sg.mesha.goatos.boot

import android.content.Context
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.auth.AuthRepository
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.datastore.SessionStore

@OptIn(ExperimentalCoroutinesApi::class)
class SessionViewModelAnalyticsTest {

    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private class FakeSessionStore : SessionStore {
        val tokenFlow = MutableStateFlow<String?>("existing-session")
        override val bearerToken: Flow<String?> = tokenFlow
        override suspend fun currentToken(): String? = tokenFlow.value
        override suspend fun setBearerToken(token: String?) { tokenFlow.value = token }
        override val language: Flow<String> = MutableStateFlow("en")
        override suspend fun currentLanguage(): String = "en"
        override suspend fun setLanguage(code: String) {}
    }

    private class FakeAuthRepository : AuthRepository {
        var signedOut = false
        override suspend fun signInWithEmailPassword(email: String, password: String): Result<Unit> = Result.success(Unit)
        override suspend fun signInWithGoogle(activityContext: Context): Result<Unit> = Result.success(Unit)
        override suspend fun sendPasswordReset(email: String): Result<Unit> = Result.success(Unit)
        override suspend fun currentIdToken(forceRefresh: Boolean): String? = null
        override fun signOut() { signedOut = true }
    }

    @Test
    fun `signOut clears the session and logs sign_out`() = runTest {
        val analytics = RecordingAnalytics()
        val store = FakeSessionStore()
        val auth = FakeAuthRepository()
        val vm = SessionViewModel(store, auth, analytics)

        vm.signOut()
        advanceUntilIdle()

        assertTrue("auth repository sign-out invoked", auth.signedOut)
        assertNull("session token cleared", store.tokenFlow.value)
        assertTrue(
            "sign_out event recorded",
            analytics.events.any { it.name == AnalyticsEvents.SIGN_OUT },
        )
    }
}
