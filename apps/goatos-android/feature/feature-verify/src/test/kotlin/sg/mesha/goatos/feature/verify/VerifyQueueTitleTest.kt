package sg.mesha.goatos.feature.verify

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

/**
 * Pins the title-is-the-module invariant for [verifyQueueTitle] / [QueueHeader]. Bug this
 * prevents: the verify queue's app-bar title was a hardcoded "Video verification" string
 * regardless of which module (Vaccination / Weighing / Counts / ...) the nav bar was actually
 * on -- the one thing that changed between taps was invisible, and it cost the most prominent
 * line in the header restating a fact the verifier already knew.
 *
 * telemetry:exempt pure string-selection unit test — asserts a derived `String` off a
 * [VerifyQueueUiState] and renders no surface, so there is no user-facing event to wire.
 */
class VerifyQueueTitleTest {

    private val genericFallback = "Video verification"
    private val actionQueueTitle = "Action queue"
    private val vaccinationLabel = "Vaccination"
    private val weighingLabel = "Weighing"

    private fun title(state: VerifyQueueUiState): String = verifyQueueTitle(
        state = state,
        actionQueueTitle = actionQueueTitle,
        vaccinationLabel = vaccinationLabel,
        weighingLabel = weighingLabel,
        genericFallback = genericFallback,
    )

    @Test
    fun `backend-owned module label wins when present`() {
        val state = VerifyQueueUiState(moduleLabel = "Herd Operations", hasLoadedOnce = true)
        assertEquals("Herd Operations", title(state))
    }

    @Test
    fun `vaccination tab resolves to the vaccination module name`() {
        val state = VerifyQueueUiState(
            moduleLabel = "",
            selectedModule = VerifyModuleTab.VACCINATION,
            hasLoadedOnce = true,
        )
        assertEquals(vaccinationLabel, title(state))
    }

    @Test
    fun `weighing tab resolves to the weighing module name`() {
        val state = VerifyQueueUiState(
            moduleLabel = "",
            selectedModule = VerifyModuleTab.WEIGHING,
            hasLoadedOnce = true,
        )
        assertEquals(weighingLabel, title(state))
    }

    @Test
    fun `every VerifyModuleTab resolves to its own module name, never the generic fallback`() {
        // The bug this locks down: the app bar showed the SAME "Video verification" string no
        // matter which module the nav bar was on. Sweeping every tab here means a newly added
        // VerifyModuleTab that falls through to the generic string fails loudly instead of
        // silently reproducing the bug.
        for (tab in VerifyModuleTab.entries) {
            val state = VerifyQueueUiState(moduleLabel = "", selectedModule = tab, hasLoadedOnce = true)
            val resolved = title(state)
            assertNotEquals("module=$tab must not fall back to the generic title", genericFallback, resolved)
        }
    }

    @Test
    fun `action queue always wins regardless of module`() {
        val state = VerifyQueueUiState(
            isActionQueue = true,
            moduleLabel = "Weighing",
            selectedModule = VerifyModuleTab.VACCINATION,
            hasLoadedOnce = true,
        )
        assertEquals(actionQueueTitle, title(state))
    }

    @Test
    fun `unresolved module after load falls back to generic rather than staying blank`() {
        // A permanently headerless app bar (blank title forever) is its own bug -- worse than a
        // generic label. Only fall back once hasLoadedOnce is true; see the next test for the
        // in-flight case.
        val state = VerifyQueueUiState(moduleLabel = "", selectedModule = null, hasLoadedOnce = true)
        assertEquals(genericFallback, title(state))
    }

    @Test
    fun `still loading and unresolved is blank, not the generic fallback`() {
        // The generic title used to flash for a few frames on every cold start and then swap --
        // a draw-a-wrong-answer-first defect. While nothing has resolved yet, prefer blank.
        val state = VerifyQueueUiState(moduleLabel = "", selectedModule = null, hasLoadedOnce = false)
        assertEquals("", title(state))
    }
}
