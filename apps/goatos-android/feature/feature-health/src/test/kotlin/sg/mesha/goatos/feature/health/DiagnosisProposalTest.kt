package sg.mesha.goatos.feature.health

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The assessment screen's decision rules.
 *
 * The one worth reading twice is that an EMPTY selection can still be sent. It is
 * not an unfinished form -- it is the Director saying none of these should be
 * treated, which is a real decision and is recorded as one. Blocking it would
 * leave the only way to disagree with the register be to walk away, and the
 * disagreement would never be recorded.
 */
// telemetry:exempt Unit test; not a user-facing screen
class DiagnosisProposalTest {

    private fun problem(id: String, canConfirm: Boolean = true) = ProposedProblem(
        id = id,
        label = id,
        confidence = "Likely",
        canConfirm = canConfirm,
        blockedReason = if (canConfirm) "" else "No treatment plan has been set up yet.",
    )

    private fun proposed(vararg problems: ProposedProblem) = DiagnosisProposalState(
        status = "proposed",
        problems = problems.toList(),
        mayConfirm = true,
    )

    @Test
    fun `declining everything is a decision that can be sent`() {
        val state = proposed(problem("pneumonia"))
        assertTrue("an empty selection must remain sendable", state.canSend)
        assertTrue(state.selected.isEmpty())
    }

    @Test
    fun `a selection of treatable problems can be sent`() {
        val state = proposed(problem("pneumonia"), problem("pinkeye"))
            .copy(selected = setOf("pneumonia", "pinkeye"))
        assertTrue(state.canSend)
        assertTrue(state.blockedSelections.isEmpty())
    }

    // A confirmed diagnosis with no authored treatment card cannot open a course.
    // The Director must see that BEFORE deciding rather than hit it as a failure after.
    @Test
    fun `selecting a problem with no treatment plan blocks the send and names it`() {
        val state = proposed(problem("pneumonia"), problem("tetanus", canConfirm = false))
            .copy(selected = setOf("tetanus"))

        assertFalse(state.canSend)
        assertEquals(listOf("tetanus"), state.blockedSelections.map { it.id })
    }

    @Test
    fun `one unplanned selection blocks the whole decision, not just its own row`() {
        val state = proposed(problem("pneumonia"), problem("tetanus", canConfirm = false))
            .copy(selected = setOf("pneumonia", "tetanus"))
        assertFalse("a partial send would silently drop the blocked one", state.canSend)
    }

    // Whether this user may decide is BACKEND-owned. A manager and the Director see
    // the identical assessment; only one of them is offered the decision.
    @Test
    fun `a user without the authority is never offered the decision`() {
        val state = proposed(problem("pneumonia")).copy(mayConfirm = false, selected = setOf("pneumonia"))
        assertFalse(state.canSend)
    }

    @Test
    fun `a decided assessment cannot be decided again`() {
        val confirmed = proposed(problem("pneumonia")).copy(status = "confirmed")
        assertTrue(confirmed.decided)
        assertFalse(confirmed.canSend)

        val declined = proposed(problem("pneumonia")).copy(status = "declined")
        assertTrue(declined.decided)
        assertFalse(declined.canSend)
    }

    @Test
    fun `an assessment still awaiting a decision is not decided`() {
        assertFalse(proposed(problem("pneumonia")).decided)
        // Blank status is "not loaded yet", which must not read as decided either --
        // that would hide the decision behind an empty screen.
        assertFalse(DiagnosisProposalState().decided)
    }

    @Test
    fun `a send in flight cannot be sent again`() {
        assertFalse(proposed(problem("pneumonia")).copy(sending = true).canSend)
    }

    // A blocked problem stays SELECTABLE so the screen can explain why it cannot be
    // sent. A tap that does nothing reads as a broken screen.
    @Test
    fun `toggling adds and removes`() {
        assertEquals(setOf("a"), toggleSelection(emptySet(), "a"))
        assertEquals(setOf("a", "b"), toggleSelection(setOf("a"), "b"))
        assertEquals(setOf("b"), toggleSelection(setOf("a", "b"), "a"))
        assertEquals(emptySet<String>(), toggleSelection(setOf("a"), "a"))
    }

    @Test
    fun `a housing directive is only shown when the register gave one`() {
        assertFalse(HousingDirective().hasDirective)
        assertTrue(HousingDirective(acuity = "Sick pen").hasDirective)
        assertTrue(HousingDirective(containment = "Keep apart").hasDirective)
        assertTrue(HousingDirective(lowCompetition = true).hasDirective)
    }
}
