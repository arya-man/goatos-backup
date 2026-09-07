package sg.mesha.goatos.update

import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test
import java.nio.file.Files

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

    @Test
    fun `debug override is honored before Remote Config skip`() = runTest {
        val overrideFile = Files.createTempDirectory("force-update-override")
            .resolve(RemoteConfigUpdateGate.DEBUG_OVERRIDE_FILE)
            .toFile()
        overrideFile.writeText(
            "min_supported_version_code=2\nupdate_url=https://appdistribution.firebase.dev/i/x\n",
        )
        val gate = RemoteConfigUpdateGate(
            currentVersionCode = 1,
            minFetchIntervalSeconds = 0,
            skipRemoteConfig = true,
            enableDebugOverride = true,
            debugOverrideFile = overrideFile,
        )

        assertEquals(
            UpdateDecision.ForceUpdate("https://appdistribution.firebase.dev/i/x"),
            gate.check(),
        )
    }
}
