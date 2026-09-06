package sg.mesha.goatos.update

import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test

class RemoteConfigUpdateGateTest {

    @Test
    fun `dev flavor skip allows without consulting Remote Config`() = runTest {
        val gate = RemoteConfigUpdateGate(
            currentVersionCode = 1,
            minFetchIntervalSeconds = 0,
            skipRemoteConfig = true,
        )

        assertEquals(UpdateDecision.Allowed, gate.check())
    }
}
