package sg.mesha.goatos.viewmodel

import java.time.LocalDate
import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.network.dto.HealthDiagnosisQueueItemDto

/**
 * The wire-to-row mapping.
 *
 * Two things are load-bearing and easy to lose: the backend's ranking must survive
 * the map, and a register token must never reach a screen.
 */
// telemetry:exempt Unit test; not a user-facing screen
class DiagnosisQueueMappingTest {

    @Test
    fun `register tokens are turned into farm words`() {
        assertEquals("Pregnancy Toxemia", diagnosisLabel("pregnancy_toxemia"))
        assertEquals("Fly Strike", diagnosisLabel("fly_strike"))
        assertEquals("Fever", diagnosisLabel("FEVER".lowercase()))
    }

    // Severity first, then confidence -- the backend's order. Re-sorting here would
    // put a mild certainty above a serious maybe.
    @Test
    fun `the backend ranking survives the mapping`() {
        val row = HealthDiagnosisQueueItemDto(
            diagnosisRunId = "run-1",
            goatDisplayId = "G-1",
            problems = listOf("tetanus", "pinkeye", "fever"),
        ).toQueueRow()

        assertEquals(listOf("Tetanus", "Pinkeye", "Fever"), row.problems)
    }

    // The backend composes the location; the client renders it verbatim.
    //
    // The fixture uses a WORDED partition on purpose. The shed/partition separator
    // is a maintainer decision that has already changed once -- bare numerals moved
    // from "Castro - 2" to "Castro 2" -- and a fixture pinned to the numeric form
    // teaches every later reader a shape the farm may no longer use. A worded label
    // reads the same under both conventions, so this asserts PASSTHROUGH without
    // quietly asserting a composition rule this layer does not own.
    @Test
    fun `the location is taken from the backend, never re-derived`() {
        val row = HealthDiagnosisQueueItemDto(
            shedName = "Godel 1",
            partitionLabel = "Part 3",
            operationalLocationDisplay = "Godel 1 - Part 3",
        ).toQueueRow()
        assertEquals("Godel 1 - Part 3", row.location)
    }

    // A shed that does not resolve leaves the row's location blank. Showing the
    // animal alone is correct; a raw id or a dangling separator is not.
    @Test
    fun `an unresolved shed leaves the location blank`() {
        assertEquals("", HealthDiagnosisQueueItemDto(goatDisplayId = "G-1").toQueueRow().location)
    }

    @Test
    fun `recent business dates read as farm words`() {
        val today = LocalDate.of(2026, 8, 14)
        assertEquals("Today", relativeBusinessDate("2026-08-14", today))
        assertEquals("Yesterday", relativeBusinessDate("2026-08-13", today))
        assertEquals("12 Aug", relativeBusinessDate("2026-08-12", today))
    }

    // Showing the server's own string is honest; showing nothing hides which day
    // the animal was seen.
    @Test
    fun `an unparseable date falls back to the raw value rather than blank`() {
        assertEquals("not-a-date", relativeBusinessDate("not-a-date", LocalDate.of(2026, 8, 14)))
    }
}
