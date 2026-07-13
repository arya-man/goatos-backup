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
}
