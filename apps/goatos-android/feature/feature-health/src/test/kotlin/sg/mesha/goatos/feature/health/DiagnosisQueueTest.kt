package sg.mesha.goatos.feature.health

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The queue's two jobs: be scannable, and never bury an emergency.
 */
// telemetry:exempt Unit test; not a user-facing screen
class DiagnosisQueueTest {

    private fun row(
        problems: List<String> = listOf("Fever"),
        emergencies: Int = 0,
        unexplained: Int = 0,
    ) = DiagnosisQueueRow(
        diagnosisRunId = "run-1",
        goatDisplayId = "G-100",
        location = "Castro - 2",
        seen = "Today",
        problems = problems,
        emergencyCount = emergencies,
        unexplainedCount = unexplained,
    )

    // An emergency is work already owed that no decision gates. A queue that renders
    // it like everything else buries it.
    @Test
    fun `a row with an emergency is urgent`() {
        assertTrue(row(emergencies = 1).urgent)
        assertFalse(row(emergencies = 0).urgent)
    }

    // Unexplained findings matter, but they are not an emergency: nothing has to be
    // done this minute. Conflating them would make every second row shout.
    @Test
    fun `unexplained findings alone do not make a row urgent`() {
        assertFalse(row(unexplained = 3).urgent)
    }

    // The list is ranked severity-first by the backend, so the FRONT of it is what
    // matters. Printing all of it turns a scannable queue into a wall of text.
    @Test
    fun `the headline shows the worst two and counts the rest`() {
        assertEquals("Fever", problemHeadline(listOf("Fever")))
        assertEquals("Fever, Pinkeye", problemHeadline(listOf("Fever", "Pinkeye")))
        assertEquals(
            "Fever, Pinkeye +2 more",
            problemHeadline(listOf("Fever", "Pinkeye", "Bloating", "Ticks")),
        )
    }

    // "Nothing found" is a real outcome of a check, not an empty state. Rendering a
    // blank would read as a broken row.
    @Test
    fun `an assessment that found nothing still says so`() {
        assertEquals("Nothing found", problemHeadline(emptyList()))
    }
}
