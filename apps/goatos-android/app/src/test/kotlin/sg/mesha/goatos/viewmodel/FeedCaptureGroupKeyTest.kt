package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

/**
 * The feed capture group key must identify ONE PEN of a shed-session ON ONE FEED DAY.
 *
 * The pen assertions fail against the pre-2026-08-09 key ("feed-pack:$shedId:$sessionNo:$workflow"),
 * which is the point: that key made two pens of a shed collide, so one pen's video became the
 * other's draft and one pen's submission collapsed the other's as an idempotent replay.
 *
 * The DAY assertions fail against both that key and its first pen-aware replacement — the day was
 * missing from every version until it was reported in review. Same collision, one dimension over:
 * the same pen and session recur every single day.
 */
class FeedCaptureGroupKeyTest {

    private val shed = "shed-mandela-2"
    private val today = "2026-08-09"
    private val tomorrow = "2026-08-10"

    @Test
    fun `two pens of the same shed-session-day get different keys`() {
        val part2 = feedCaptureGroupKey("feed-pack", shed, "Part 2", 1, "normal", today)
        val part3 = feedCaptureGroupKey("feed-pack", shed, "Part 3", 1, "normal", today)

        // The reported field failure: filming Part 2 then opening Part 3 showed Part 2's draft as
        // "video uploaded successfully" on a pen nobody had filmed.
        assertNotEquals(part2, part3)
    }

    @Test
    fun `the same pen on two days gets different keys`() {
        val todayKey = feedCaptureGroupKey("feed-pack", shed, "Part 2", 1, "normal", today)
        val tomorrowKey = feedCaptureGroupKey("feed-pack", shed, "Part 2", 1, "normal", tomorrow)

        // A feed task is raised per shed per DAY, so yesterday's committed draft must not be what
        // today's operator opens. Sharing the key showed a success screen for a clip nobody had
        // recorded today, and left the pen unfilmable because the shared draft carries the
        // committed flag.
        assertNotEquals(todayKey, tomorrowKey)
    }

    @Test
    fun `each pen-day gets its own submit idempotency key`() {
        // The quieter half of the same defect: a shared key made the backend collapse the second
        // submission as a replay of the first, so it got no completion row, no verification item
        // and no video — while the phone showed success.
        val part2 = "feed-packing-complete:" + feedCaptureGroupKey("feed-pack", shed, "Part 2", 1, "normal", today)
        val part3 = "feed-packing-complete:" + feedCaptureGroupKey("feed-pack", shed, "Part 3", 1, "normal", today)
        val part2Tomorrow =
            "feed-packing-complete:" + feedCaptureGroupKey("feed-pack", shed, "Part 2", 1, "normal", tomorrow)

        assertNotEquals(part2, part3)
        assertNotEquals(part2, part2Tomorrow)
    }

    @Test
    fun `distribution keys are pen and day scoped too and never collide with packing`() {
        val distPart2 = feedCaptureGroupKey("feed-dist", shed, "Part 2", 1, "normal", today)
        val distPart3 = feedCaptureGroupKey("feed-dist", shed, "Part 3", 1, "normal", today)
        val distTomorrow = feedCaptureGroupKey("feed-dist", shed, "Part 2", 1, "normal", tomorrow)
        val packPart2 = feedCaptureGroupKey("feed-pack", shed, "Part 2", 1, "normal", today)

        assertNotEquals(distPart2, distPart3)
        assertNotEquals(distPart2, distTomorrow)
        // Distribution carries two proofs and packing one; sharing a draft across the two flows
        // would tick the wrong boxes.
        assertNotEquals(distPart2, packPart2)
    }

    @Test
    fun `the same pen-day is stable across case and whitespace variants`() {
        // A pen must not split into two drafts because one page load spelled the label differently
        // — that would lose the recorded clip on re-entry, the 2026-07-30 defect one layer over.
        val canonical = feedCaptureGroupKey("feed-pack", shed, "Part 3", 2, "experiment", today)

        assertEquals(canonical, feedCaptureGroupKey("feed-pack", shed, "part 3", 2, "experiment", today))
        assertEquals(canonical, feedCaptureGroupKey("feed-pack", shed, "  Part 3  ", 2, "experiment", today))
        assertEquals(canonical, feedCaptureGroupKey("feed-pack", shed, "Part  3", 2, "experiment", today))
        // The date is padded on the route in one place and not another often enough to be worth
        // pinning: a surrounding space is not a different day.
        assertEquals(canonical, feedCaptureGroupKey("feed-pack", shed, "Part 3", 2, "experiment", " $today "))
    }

    @Test
    fun `an undivided shed collapses to one stable whole token`() {
        val blank = feedCaptureGroupKey("feed-pack", "shed-yashoda", "", 1, "normal", today)
        val whitespace = feedCaptureGroupKey("feed-pack", "shed-yashoda", "   ", 1, "normal", today)

        assertEquals(blank, whitespace)
        assertEquals("feed-pack:2026-08-09:shed-yashoda:whole:1:normal", blank)
    }

    @Test
    fun `a missing date yields a well-formed key rather than an empty segment`() {
        // Degraded, not supported: a route that drops target_date is a routing bug. The key must
        // still be parseable rather than "feed-pack::shed-x:whole:1:normal", and it must not read
        // the device clock — a capture started before midnight would then lose its draft when
        // submitted after.
        val undated = feedCaptureGroupKey("feed-pack", "shed-yashoda", "", 1, "normal", "")

        assertEquals("feed-pack:undated:shed-yashoda:whole:1:normal", undated)
        assertEquals(undated, feedCaptureGroupKey("feed-pack", "shed-yashoda", "", 1, "normal", "  "))
        assertNotEquals(undated, feedCaptureGroupKey("feed-pack", "shed-yashoda", "", 1, "normal", today))
    }

    @Test
    fun `session and workflow still separate keys within one pen-day`() {
        val session1 = feedCaptureGroupKey("feed-pack", shed, "Part 2", 1, "normal", today)
        val session2 = feedCaptureGroupKey("feed-pack", shed, "Part 2", 2, "normal", today)
        val experiment = feedCaptureGroupKey("feed-pack", shed, "Part 2", 1, "experiment", today)

        assertNotEquals(session1, session2)
        assertNotEquals(session1, experiment)
    }

    @Test
    fun `a shed name ending in a digit is not confused with a pen`() {
        // "Mandela 1" whole vs "Mandela" pen 1 are different operational locations. The shed id is
        // the real separator, but the segments must not run together either.
        val wholeMandela1 = feedCaptureGroupKey("feed-pack", "shed-mandela-1", "", 1, "normal", today)
        val mandelaPen1 = feedCaptureGroupKey("feed-pack", "shed-mandela", "1", 1, "normal", today)

        assertNotEquals(wholeMandela1, mandelaPen1)
    }
}
