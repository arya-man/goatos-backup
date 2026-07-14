package sg.mesha.goatos.update

import org.junit.Assert.assertEquals
import org.junit.Test

/** The pure gate rule — the force/allow boundary, isolated from Firebase. */
class UpdateGateDecisionTest {

    @Test
    fun `build below the minimum is forced to update`() {
        val d = decideUpdate(currentVersionCode = 3, minSupportedVersionCode = 5, updateUrl = "u")
        assertEquals(UpdateDecision.ForceUpdate("u"), d)
    }

    @Test
    fun `build at the minimum is allowed`() {
        assertEquals(
            UpdateDecision.Allowed,
            decideUpdate(currentVersionCode = 5, minSupportedVersionCode = 5, updateUrl = "u"),
        )
    }

    @Test
    fun `build above the minimum is allowed`() {
        assertEquals(
            UpdateDecision.Allowed,
            decideUpdate(currentVersionCode = 9, minSupportedVersionCode = 5, updateUrl = "u"),
        )
    }

    @Test
    fun `zero minimum means no floor set and always allows`() {
        assertEquals(
            UpdateDecision.Allowed,
            decideUpdate(currentVersionCode = 1, minSupportedVersionCode = 0, updateUrl = ""),
        )
    }

    @Test
    fun `negative minimum is treated as no floor`() {
        assertEquals(
            UpdateDecision.Allowed,
            decideUpdate(currentVersionCode = 1, minSupportedVersionCode = -1, updateUrl = ""),
        )
    }

    @Test
    fun `force decision carries the update url through unchanged`() {
        val d = decideUpdate(
            currentVersionCode = 1,
            minSupportedVersionCode = 2,
            updateUrl = "https://appdistribution.firebase.dev/i/x",
        )
        assertEquals(UpdateDecision.ForceUpdate("https://appdistribution.firebase.dev/i/x"), d)
    }
}
