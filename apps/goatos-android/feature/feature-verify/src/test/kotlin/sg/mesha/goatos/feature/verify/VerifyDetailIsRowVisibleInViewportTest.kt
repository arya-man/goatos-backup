package sg.mesha.goatos.feature.verify

import androidx.compose.ui.geometry.Rect
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Pins the scroll-pause invariant for [isRowVisibleInViewport]: a proof-video row's decoder
 * should keep running only while at least half its height overlaps the list's own viewport —
 * see the doc comment on the function under test for why 50% and not 0% or 90%.
 *
 * telemetry:exempt pure geometry unit test — asserts a derived `Boolean` off two `Rect`s and
 * renders no surface, so there is no user-facing event, funnel step, or error path to wire.
 */
class VerifyDetailIsRowVisibleInViewportTest {

    // A 200pt-tall viewport starting at y=0, matching a typical LazyColumn's onGloballyPositioned
    // bounds once laid out.
    private val viewport = Rect(left = 0f, top = 0f, right = 400f, bottom = 200f)

    private fun rowAt(top: Float, bottom: Float) = Rect(left = 0f, top = top, right = 400f, bottom = bottom)

    @Test
    fun `fully inside viewport is visible`() {
        assertTrue(isRowVisibleInViewport(rowAt(20f, 80f), viewport))
    }

    @Test
    fun `fully outside viewport below is not visible`() {
        assertFalse(isRowVisibleInViewport(rowAt(250f, 350f), viewport))
    }

    @Test
    fun `fully outside viewport above is not visible`() {
        assertFalse(isRowVisibleInViewport(rowAt(-150f, -50f), viewport))
    }

    @Test
    fun `exactly half scrolled off the bottom edge is still visible at the default threshold`() {
        // Row spans y=150..250 (100pt tall); viewport ends at 200, so exactly 50pt (50%) overlaps.
        assertTrue(isRowVisibleInViewport(rowAt(150f, 250f), viewport))
    }

    @Test
    fun `mostly scrolled off is not visible`() {
        // Row spans y=180..280 (100pt tall); only 20pt (20%) overlaps the viewport.
        assertFalse(isRowVisibleInViewport(rowAt(180f, 280f), viewport))
    }

    @Test
    fun `a stricter caller-supplied threshold is honoured`() {
        // Same 50%-overlapping row as above, but requiring 90% visibility should now fail.
        assertFalse(isRowVisibleInViewport(rowAt(150f, 250f), viewport, minVisibleFraction = 0.9f))
    }

    @Test
    fun `a zero-height row never divides by zero`() {
        // Degenerate (not-yet-laid-out) bounds must not crash; a zero-height row has zero overlap
        // height, which is below any positive threshold, so it is treated as not visible.
        assertFalse(isRowVisibleInViewport(rowAt(50f, 50f), viewport))
    }
}
