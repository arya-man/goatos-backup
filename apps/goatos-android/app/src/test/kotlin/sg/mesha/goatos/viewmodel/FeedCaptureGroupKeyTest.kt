package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

/**
 * The feed capture group key must identify ONE PEN of a shed-session, never the whole shed.
 *
 * Every assertion here fails against the pre-2026-08-09 key ("feed-pack:$shedId:$sessionNo:
 * $workflow"), which is the point: that key made two pens of a shed collide, so one pen's video
 * became the other's draft and one pen's submission collapsed the other's as an idempotent replay.
 */
class FeedCaptureGroupKeyTest {

    private val shed = "shed-mandela-2"

    @Test
    fun `two pens of the same shed-session get different keys`() {
        val part2 = feedCaptureGroupKey("feed-pack", shed, "Part 2", 1, "normal")
        val part3 = feedCaptureGroupKey("feed-pack", shed, "Part 3", 1, "normal")

        // The reported field failure: filming Part 2 then opening Part 3 showed Part 2's draft as
        // "video uploaded successfully" on a pen nobody had filmed.
        assertNotEquals(part2, part3)
    }

    @Test
    fun `each pen gets its own submit idempotency key`() {
        // The quieter half of the same defect: a shared key made the backend collapse the second
        // pen's completion as a replay of the first, so that pen got no row, no verification item
        // and no video — while the phone showed success.
        val part2 = "feed-packing-complete:" + feedCaptureGroupKey("feed-pack", shed, "Part 2", 1, "normal")
        val part3 = "feed-packing-complete:" + feedCaptureGroupKey("feed-pack", shed, "Part 3", 1, "normal")

        assertNotEquals(part2, part3)
    }

    @Test
    fun `distribution keys are pen-scoped too and never collide with packing`() {
        val distPart2 = feedCaptureGroupKey("feed-dist", shed, "Part 2", 1, "normal")
        val distPart3 = feedCaptureGroupKey("feed-dist", shed, "Part 3", 1, "normal")
        val packPart2 = feedCaptureGroupKey("feed-pack", shed, "Part 2", 1, "normal")

        assertNotEquals(distPart2, distPart3)
        // Distribution carries two proofs and packing one; sharing a draft across the two flows
        // would tick the wrong boxes.
        assertNotEquals(distPart2, packPart2)
    }

    @Test
    fun `the same pen is stable across case and whitespace variants`() {
        // A pen must not split into two drafts because one page load spelled the label differently
        // — that would lose the recorded clip on re-entry, the 2026-07-30 defect one layer over.
        val canonical = feedCaptureGroupKey("feed-pack", shed, "Part 3", 2, "experiment")

        assertEquals(canonical, feedCaptureGroupKey("feed-pack", shed, "part 3", 2, "experiment"))
        assertEquals(canonical, feedCaptureGroupKey("feed-pack", shed, "  Part 3  ", 2, "experiment"))
        assertEquals(canonical, feedCaptureGroupKey("feed-pack", shed, "Part  3", 2, "experiment"))
    }

    @Test
    fun `an undivided shed collapses to one stable whole token`() {
        val blank = feedCaptureGroupKey("feed-pack", "shed-yashoda", "", 1, "normal")
        val whitespace = feedCaptureGroupKey("feed-pack", "shed-yashoda", "   ", 1, "normal")

        assertEquals(blank, whitespace)
        assertEquals("feed-pack:shed-yashoda:whole:1:normal", blank)
    }

    @Test
    fun `session and workflow still separate keys within one pen`() {
        val session1 = feedCaptureGroupKey("feed-pack", shed, "Part 2", 1, "normal")
        val session2 = feedCaptureGroupKey("feed-pack", shed, "Part 2", 2, "normal")
        val experiment = feedCaptureGroupKey("feed-pack", shed, "Part 2", 1, "experiment")

        assertNotEquals(session1, session2)
        assertNotEquals(session1, experiment)
    }

    @Test
    fun `a shed name ending in a digit is not confused with a pen`() {
        // "Mandela 1" whole vs "Mandela" pen 1 are different operational locations. The shed id is
        // the real separator, but the segments must not run together either.
        val wholeMandela1 = feedCaptureGroupKey("feed-pack", "shed-mandela-1", "", 1, "normal")
        val mandelaPen1 = feedCaptureGroupKey("feed-pack", "shed-mandela", "1", 1, "normal")

        assertNotEquals(wholeMandela1, mandelaPen1)
    }
}
