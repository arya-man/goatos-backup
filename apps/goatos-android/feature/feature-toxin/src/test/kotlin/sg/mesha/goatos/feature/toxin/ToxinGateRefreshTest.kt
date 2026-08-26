package sg.mesha.goatos.feature.toxin

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * The detail screen re-reads the server's step states when a wait ELAPSES.
 *
 * The defect this pins was reported from the field: step state is server-composed and
 * time-dependent, but the phone's cache is only written on a network event, so a countdown ran to
 * zero and the screen stayed on WAITING indefinitely. The reading step never opened, "Send
 * reading" stayed dead, and the round could not be submitted at all until the tester happened to
 * leave the screen and return. These assertions cover WHEN the refresh is armed; the refresh
 * firing was verified on a physical device against the live gate.
 */
class ToxinGateRefreshTest {

    private fun step(no: Int, state: ToxinStepState, availableAt: Long = 0L) = ToxinStepUi(
        stepNo = no,
        kind = ToxinStepKind.VIDEO,
        state = state,
        title = "Step $no",
        instruction = "",
        availableAtEpochMs = availableAt,
    )

    @Test
    fun `nothing waiting arms no refresh`() {
        val steps = listOf(
            step(1, ToxinStepState.DONE),
            step(2, ToxinStepState.AVAILABLE),
            step(3, ToxinStepState.LOCKED),
        )
        assertEquals("a screen with no wait must schedule no work", 0L, nextGateInstantMs(steps))
    }

    @Test
    fun `the EARLIEST waiting gate is the one armed`() {
        // A round really can carry two waiting steps. Arming on the later one would let the
        // nearer gate expire unnoticed — the exact freeze being fixed, one step earlier.
        val steps = listOf(
            step(4, ToxinStepState.WAITING, availableAt = 9_000L),
            step(5, ToxinStepState.WAITING, availableAt = 3_000L),
            step(6, ToxinStepState.LOCKED),
        )
        assertEquals(3_000L, nextGateInstantMs(steps))
    }

    @Test
    fun `a waiting step with no instant is not armable`() {
        // availableAtEpochMs is absent unless the server declared one; scheduling off 0 would
        // fire immediately and hot-loop.
        val steps = listOf(step(5, ToxinStepState.WAITING, availableAt = 0L))
        assertEquals(0L, nextGateInstantMs(steps))
    }
}
