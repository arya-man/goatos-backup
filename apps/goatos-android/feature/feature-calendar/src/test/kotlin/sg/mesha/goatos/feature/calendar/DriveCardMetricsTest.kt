package sg.mesha.goatos.feature.calendar

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

// CDR-005 regression coverage for the park-level drive card metrics extracted from
// CalendarScreen.kt (DriveCardMetrics.kt).
class DriveCardMetricsTest {

    // The exact case the Android card got wrong before the fix: integer division truncated
    // 46/77 to 59; Math.round rounds half-up to 60, matching the web card.
    @Test
    fun coveragePctRounds46Of77UpTo60() {
        assertEquals(60, driveCoveragePct(46, 77))
    }

    @Test
    fun coveragePctRoundsToNearestBothDirections() {
        assertEquals(33, driveCoveragePct(1, 3)) // 33.33 -> 33
        assertEquals(67, driveCoveragePct(2, 3)) // 66.66 -> 67
    }

    @Test
    fun coveragePctHandlesEdges() {
        assertEquals(0, driveCoveragePct(0, 55))
        assertEquals(100, driveCoveragePct(55, 55))
    }

    @Test
    fun coveragePctIsZeroWithNoAnimals() {
        assertEquals(0, driveCoveragePct(0, 0))
        assertEquals(0, driveCoveragePct(5, 0))
    }

    // Legacy subtitle/summary rows must be HIDDEN when a drive_summary is present (the ring
    // card already shows those totals) and PRESERVED as the fallback when it is absent.
    @Test
    fun legacyRowsHiddenWhenDriveSummaryPresent() {
        val summary = CalendarDriveSummary(
            parkName = "CBE",
            shedCount = 2,
            totalCount = 10,
            completedCount = 4,
            totalAnimals = 8,
            completedAnimals = 3,
        )
        assertFalse(legacyDriveRowsVisible(summary))
    }

    @Test
    fun legacyRowsPreservedWhenDriveSummaryAbsent() {
        assertTrue(legacyDriveRowsVisible(null))
    }

    // CDR-R1: a Room cache written before total_animals/completed_animals shipped decodes those
    // fields as null. driveCoverage must fall back to the obligation (dose) counts instead of
    // rendering a false "0 / 0 animals" over the valid legacy dose counts.
    @Test
    fun coverageUsesAnimalGrainWhenBothAnimalCountsPresent() {
        val c = driveCoverage(completedAnimals = 42, totalAnimals = 77, completedDoses = 46, totalDoses = 88)
        assertEquals(42, c.completed)
        assertEquals(77, c.total)
        assertTrue(c.usesAnimals)
    }

    @Test
    fun coverageFallsBackToDosesWhenAnimalCountsAbsent() {
        val c = driveCoverage(completedAnimals = null, totalAnimals = null, completedDoses = 46, totalDoses = 77)
        assertEquals(46, c.completed)
        assertEquals(77, c.total)
        assertFalse(c.usesAnimals)
        assertEquals(60, driveCoveragePct(c.completed, c.total))
    }

    @Test
    fun coverageFallsBackToDosesWhenOnlyOneAnimalCountPresent() {
        assertFalse(driveCoverage(10, null, 20, 30).usesAnimals)
        assertFalse(driveCoverage(null, 30, 20, 30).usesAnimals)
    }

    @Test
    fun coverageKeepsGenuineZeroAnimalsAsAnimalGrain() {
        // A real 0 distinct animals (present, not absent) stays animal-grain -- only null is legacy.
        assertTrue(driveCoverage(0, 0, 4, 8).usesAnimals)
    }
}
