package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.feature.feed.FeedPackingCompleteUiState
import sg.mesha.goatos.feature.feed.FeedStatus
import sg.mesha.goatos.feature.feed.feedSessionCanCapture

/**
 * A feed shed-session already awaiting a verdict must not be re-openable for capture.
 *
 * Reported on STG 2026-08-09: Mandela 1 - Part 2 was submitted and showed "in review", yet the row
 * stayed tappable and the detail screen offered an empty capture form on a freshly installed app.
 * `lifecycleStatus` was rendered as a chip and never consulted by the tap gate, and the detail
 * screen — which never receives the status — decides "already recorded?" from the LOCAL draft, which
 * a reinstall wipes. The operator re-shot a video the backend then discarded as an idempotent replay.
 */
class FeedCaptureAvailabilityTest {

    @Test
    fun `a session awaiting a verdict cannot be reopened`() {
        assertFalse(feedSessionCanCapture(FeedStatus.AWAITING, isToday = true))
    }

    @Test
    fun `an approved session cannot be reopened`() {
        assertFalse(feedSessionCanCapture(FeedStatus.COMPLETED, isToday = true))
    }

    @Test
    fun `a pending session is capturable`() {
        assertTrue(feedSessionCanCapture(FeedStatus.PENDING, isToday = true))
    }

    /**
     * THE BRANCH A NAIVE "already submitted -> lock it" FIX BREAKS.
     *
     * A verifier-rejected session must stay capturable or every re-shoot is stranded: the operator
     * would see a row he is required to re-film and be unable to open it, with no other route in.
     *
     * The backend merges raw 'rework' into [FeedStatus.PENDING] before it reaches a client
     * (domain.NormalizeSessionStatus), so in practice this arrives as "pending" — asserted above.
     * This asserts the RAW token too, so that if that mapping is ever changed to pass 'rework'
     * through, the gate still lets the operator back in rather than silently locking the queue.
     */
    @Test
    fun `a reworked session stays capturable, however the status reaches the client`() {
        assertTrue(feedSessionCanCapture(FeedStatus.PENDING, isToday = true))
        assertTrue(feedSessionCanCapture("rework", isToday = true))
    }

    /** An absent status must not lock the row: unknown means "the operator may still act". */
    @Test
    fun `an unknown or blank status is treated as pending`() {
        assertTrue(feedSessionCanCapture("", isToday = true))
        assertTrue(feedSessionCanCapture("   ", isToday = true))
        assertTrue(feedSessionCanCapture("something_new", isToday = true))
    }

    /** The day gate is independent and still absolute: a past day is view-only whatever the status. */
    @Test
    fun `a past day is never capturable`() {
        assertFalse(feedSessionCanCapture(FeedStatus.PENDING, isToday = false))
        assertFalse(feedSessionCanCapture("rework", isToday = false))
        assertFalse(feedSessionCanCapture("", isToday = false))
    }

    /**
     * The screen must REFUSE, not merely hide the row. An operator who taps a submitted session
     * still gets here — a dead card reads as a broken app — so the detail state is what stops a
     * second video being recorded for work already queued.
     */
    @Test
    fun `an already-submitted session offers no capture and no submit`() {
        val submitted = FeedPackingCompleteUiState(
            videoCaptured = true,
            canComplete = true,
            alreadySubmitted = true,
        )

        assertFalse(submitted.captureEnabled)
        assertFalse(submitted.submitEnabled)
    }

    /** The same state with the session still open must behave exactly as before. */
    @Test
    fun `an open session still offers capture and submit`() {
        val open = FeedPackingCompleteUiState(
            videoCaptured = true,
            canComplete = true,
            alreadySubmitted = false,
        )

        assertTrue(open.captureEnabled)
        assertTrue(open.submitEnabled)
    }
}
