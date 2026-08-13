package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

/**
 * The feed capture group key must identify exactly one unit of filmable work on one feed DAY.
 *
 * PACKING and DISTRIBUTION are both a SHED-SESSION and share [feedCaptureGroupKey], separated only by
 * their prefix. Packing was briefly a shed-DAY (2026-08-10) and that was reverted on 2026-08-11: a
 * shed's morning and evening bags are filmed separately, so each needs its own draft, its own submit
 * idempotency key and its own outbox group.
 *
 * Exact shed id is the physical shed. Compatibility partition labels must not split one exact shed
 * into multiple drafts; two physical sheds differ by shedId.
 *
 * The DAY assertions fail against both that key and its first partition-aware replacement — the day was
 * missing from every version until it was reported in review. Same collision, one dimension over:
 * the same shed and session recur every single day.
 *
 * The SESSION assertions fail against the day-level key, which is the third version of this same
 * collision: it made a shed's two bags share one draft, so filming the morning showed the evening as
 * already recorded.
 */
class FeedCaptureGroupKeyTest {

    private val shed = "shed-mandela-2"
    private val today = "2026-08-09"
    private val tomorrow = "2026-08-10"

    private fun packing(
        shedId: String = shed,
        partitionLabel: String = "Part 2",
        sessionNo: Int = 1,
        workflow: String = "normal",
        targetDate: String = today,
    ) = feedCaptureGroupKey("feed-pack", shedId, partitionLabel, sessionNo, workflow, targetDate)

    @Test
    fun `stale partition labels do not split one exact shed-day key`() {
        assertEquals(packing(partitionLabel = "Part 2"), packing(partitionLabel = "Part 3"))
        assertNotEquals(
            packing(shedId = "shed-castro-1", partitionLabel = "1"),
            packing(shedId = "shed-castro-2", partitionLabel = "1"),
        )
    }

    @Test
    fun `the same shed session on two days gets different keys`() {
        // A feed task is raised per shed per DAY, so yesterday's committed draft must not be what
        // today's operator opens. Sharing the key showed a success screen for a clip nobody had
        // recorded today, and left the shed unfilmable because the shared draft carries the
        // committed flag.
        assertNotEquals(packing(targetDate = today), packing(targetDate = tomorrow))
    }

    @Test
    fun `each physical shed session gets its own submit idempotency key`() {
        // The quieter half of the same defect: a shared key made the backend collapse the second
        // submission as a replay of the first, so it got no completion row, no verification item
        // and no video — while the phone showed success.
        val shed1 = "feed-packing-complete:" + packing(shedId = "shed-castro-1", partitionLabel = "Part 2")
        val shed2 = "feed-packing-complete:" + packing(shedId = "shed-castro-2", partitionLabel = "Part 3")
        val shed1Tomorrow = "feed-packing-complete:" + packing(partitionLabel = "Part 2", targetDate = tomorrow)

        assertNotEquals(shed1, shed2)
        assertNotEquals(shed1, shed1Tomorrow)
    }

    /**
     * THE 2026-08-11 REVERT, stated as a key property.
     *
     * A packing key carries a real session again, so a shed's morning and evening are TWO drafts, TWO
     * idempotency keys and TWO outbox groups — which is what makes them two cards and two videos.
     * Between 2026-08-10 and 2026-08-11 this segment was the literal "day" and the two bags shared
     * everything.
     *
     * The workflow still separates, because normal and experiment are genuinely different bags.
     */
    @Test
    fun `a packing key carries its session and still separates workflows`() {
        assertEquals(
            "feed-pack:2026-08-09:shed-mandela-2:1:normal",
            packing(),
        )
        assertNotEquals(packing(workflow = "normal"), packing(workflow = "experiment"))
    }

    /** A shed's two daily sessions must never share a draft, an idempotency key or an outbox group. */
    @Test
    fun `packing separates a shed's morning and evening`() {
        val morning = packing(sessionNo = 1)
        val evening = packing(sessionNo = 2)

        assertNotEquals(morning, evening)
        // The submit key too, which is the quieter half: sharing it made the backend collapse the
        // evening submission as a replay of the morning, so the evening got no completion row, no
        // verification item and no video — while the phone showed success.
        assertNotEquals("feed-packing-complete:$morning", "feed-packing-complete:$evening")
    }

    /** DISTRIBUTION separates its sessions on exactly the same terms. */
    @Test
    fun `distribution still separates its sessions`() {
        val morning = feedCaptureGroupKey("feed-dist", shed, "Part 2", 1, "normal", today)
        val evening = feedCaptureGroupKey("feed-dist", shed, "Part 2", 2, "normal", today)

        assertNotEquals(morning, evening)
    }

    @Test
    fun `distribution keys are shed-session and day scoped too and never collide with packing`() {
        val distPart2 = feedCaptureGroupKey("feed-dist", shed, "Part 2", 1, "normal", today)
        val distPart3 = feedCaptureGroupKey("feed-dist", shed, "Part 3", 1, "normal", today)
        val distTomorrow = feedCaptureGroupKey("feed-dist", shed, "Part 2", 1, "normal", tomorrow)

        assertEquals(distPart2, distPart3)
        assertNotEquals(distPart2, distTomorrow)
        // Distribution carries two proofs and packing one; sharing a draft across the two flows
        // would tick the wrong boxes. The PREFIX is the only thing keeping them apart, so it is
        // asserted on the matching session.
        assertNotEquals(distPart2, packing(sessionNo = 1))
    }

    @Test
    fun `legacy partition labels do not affect the same shed-session key`() {
        // A shed must not split into two drafts because a legacy compatibility label differed.
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
        assertEquals("feed-pack:2026-08-09:shed-yashoda:1:normal", blank)
    }

    @Test
    fun `a missing date yields a well-formed key rather than an empty segment`() {
        // Degraded, not supported: a route that drops target_date is a routing bug. The key must
        // still be parseable rather than "feed-pack::shed-x:whole:1:normal", and it must not read
        // the device clock — a capture started before midnight would then lose its draft when
        // submitted after.
        val undated = packing(shedId = "shed-yashoda", partitionLabel = "", targetDate = "")

        assertEquals("feed-pack:undated:shed-yashoda:1:normal", undated)
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
