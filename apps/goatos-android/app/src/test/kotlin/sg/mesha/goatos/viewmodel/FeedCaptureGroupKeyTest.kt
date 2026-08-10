package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

/**
 * The feed capture group key must identify exactly one unit of filmable work on one feed DAY.
 *
 * That unit differs by flow, and the difference is the whole point of there being two functions:
 *  - PACKING is a PEN-DAY ([feedPackingCaptureGroupKey]). One video covers the pen's morning and
 *    evening shares together (maintainer decision 2026-08-10).
 *  - DISTRIBUTION is a PEN-SESSION ([feedCaptureGroupKey]). It is still filmed per session.
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

    private fun packing(
        shedId: String = shed,
        partitionLabel: String = "Part 2",
        workflow: String = "normal",
        targetDate: String = today,
    ) = feedPackingCaptureGroupKey(shedId, partitionLabel, workflow, targetDate)

    @Test
    fun `two pens of the same shed-day get different keys`() {
        // The reported field failure: filming Part 2 then opening Part 3 showed Part 2's draft as
        // "video uploaded successfully" on a pen nobody had filmed.
        assertNotEquals(packing(partitionLabel = "Part 2"), packing(partitionLabel = "Part 3"))
    }

    @Test
    fun `the same pen on two days gets different keys`() {
        // A feed task is raised per pen per DAY, so yesterday's committed draft must not be what
        // today's operator opens. Sharing the key showed a success screen for a clip nobody had
        // recorded today, and left the pen unfilmable because the shared draft carries the
        // committed flag.
        assertNotEquals(packing(targetDate = today), packing(targetDate = tomorrow))
    }

    @Test
    fun `each pen-day gets its own submit idempotency key`() {
        // The quieter half of the same defect: a shared key made the backend collapse the second
        // submission as a replay of the first, so it got no completion row, no verification item
        // and no video — while the phone showed success.
        val part2 = "feed-packing-complete:" + packing(partitionLabel = "Part 2")
        val part3 = "feed-packing-complete:" + packing(partitionLabel = "Part 3")
        val part2Tomorrow = "feed-packing-complete:" + packing(partitionLabel = "Part 2", targetDate = tomorrow)

        assertNotEquals(part2, part3)
        assertNotEquals(part2, part2Tomorrow)
    }

    /**
     * THE 2026-08-10 MERGE, stated as a key property.
     *
     * A packing key takes no session at all, so a pen's whole day is ONE draft, ONE idempotency key
     * and ONE outbox group — which is what makes it one card and one video. There is deliberately no
     * way to ask for a session-scoped packing key: [feedPackingCaptureGroupKey] has no such
     * parameter, so this cannot regress by a caller passing one.
     *
     * The workflow still separates, because normal and experiment are genuinely different bags.
     */
    @Test
    fun `a packing key is the whole pen-day and still separates workflows`() {
        assertEquals(
            "feed-pack:2026-08-09:shed-mandela-2:part 2:day:normal",
            packing(),
        )
        assertNotEquals(packing(workflow = "normal"), packing(workflow = "experiment"))
    }

    /**
     * DISTRIBUTION did NOT merge. Its sessions must still separate, or the evening's video would
     * overwrite the morning's draft and its submission would collapse as a replay — the exact defect
     * the packing merge is allowed to have only because packing genuinely films the day once.
     */
    @Test
    fun `distribution still separates its sessions`() {
        val morning = feedCaptureGroupKey("feed-dist", shed, "Part 2", 1, "normal", today)
        val evening = feedCaptureGroupKey("feed-dist", shed, "Part 2", 2, "normal", today)

        assertNotEquals(morning, evening)
    }

    @Test
    fun `distribution keys are pen and day scoped too and never collide with packing`() {
        val distPart2 = feedCaptureGroupKey("feed-dist", shed, "Part 2", 1, "normal", today)
        val distPart3 = feedCaptureGroupKey("feed-dist", shed, "Part 3", 1, "normal", today)
        val distTomorrow = feedCaptureGroupKey("feed-dist", shed, "Part 2", 1, "normal", tomorrow)

        assertNotEquals(distPart2, distPart3)
        assertNotEquals(distPart2, distTomorrow)
        // Distribution carries two proofs and packing one; sharing a draft across the two flows
        // would tick the wrong boxes. Checked against BOTH distribution sessions, because packing's
        // key no longer carries a session and a naive "session 0" spelling could have collided with
        // one of them.
        assertNotEquals(distPart2, packing())
        assertNotEquals(feedCaptureGroupKey("feed-dist", shed, "Part 2", 2, "normal", today), packing())
    }

    @Test
    fun `the same pen-day is stable across case and whitespace variants`() {
        // A pen must not split into two drafts because one page load spelled the label differently
        // — that would lose the recorded clip on re-entry, the 2026-07-30 defect one layer over.
        val canonical = packing(partitionLabel = "Part 3", workflow = "experiment")

        assertEquals(canonical, packing(partitionLabel = "part 3", workflow = "experiment"))
        assertEquals(canonical, packing(partitionLabel = "  Part 3  ", workflow = "experiment"))
        assertEquals(canonical, packing(partitionLabel = "Part  3", workflow = "experiment"))
        // The date is padded on the route in one place and not another often enough to be worth
        // pinning: a surrounding space is not a different day.
        assertEquals(canonical, packing(partitionLabel = "Part 3", workflow = "experiment", targetDate = " $today "))
    }

    @Test
    fun `an undivided shed collapses to one stable whole token`() {
        val blank = packing(shedId = "shed-yashoda", partitionLabel = "")
        val whitespace = packing(shedId = "shed-yashoda", partitionLabel = "   ")

        assertEquals(blank, whitespace)
        assertEquals("feed-pack:2026-08-09:shed-yashoda:whole:day:normal", blank)
    }

    @Test
    fun `a missing date yields a well-formed key rather than an empty segment`() {
        // Degraded, not supported: a route that drops target_date is a routing bug. The key must
        // still be parseable rather than "feed-pack::shed-x:whole:day:normal", and it must not read
        // the device clock — a capture started before midnight would then lose its draft when
        // submitted after.
        val undated = packing(shedId = "shed-yashoda", partitionLabel = "", targetDate = "")

        assertEquals("feed-pack:undated:shed-yashoda:whole:day:normal", undated)
        assertEquals(undated, packing(shedId = "shed-yashoda", partitionLabel = "", targetDate = "  "))
        assertNotEquals(undated, packing(shedId = "shed-yashoda", partitionLabel = ""))
    }

    @Test
    fun `a shed name ending in a digit is not confused with a pen`() {
        // "Mandela 1" whole vs "Mandela" pen 1 are different operational locations. The shed id is
        // the real separator, but the segments must not run together either.
        val wholeMandela1 = packing(shedId = "shed-mandela-1", partitionLabel = "")
        val mandelaPen1 = packing(shedId = "shed-mandela", partitionLabel = "1")

        assertNotEquals(wholeMandela1, mandelaPen1)
    }
}
