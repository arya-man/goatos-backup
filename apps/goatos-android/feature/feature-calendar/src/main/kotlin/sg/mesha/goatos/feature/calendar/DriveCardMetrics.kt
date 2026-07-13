package sg.mesha.goatos.feature.calendar

// Pure, unit-tested metrics for the park-level vaccination drive card. Extracted from
// CalendarScreen.kt so the coverage-percentage rounding and the drive-summary visibility
// rules are testable on the JVM (DriveCardMetricsTest) and cannot silently regress (CDR-005).

// Coverage-ring percentage on the DISTINCT-ANIMAL grain, rounded half-up to match the web
// card (which uses Math.round). Example: 46/77 -> 60, NOT the truncated 59 the pre-fix
// integer-division card produced. Returns 0 when there are no animals.
internal fun driveCoveragePct(completedAnimals: Int, totalAnimals: Int): Int =
    if (totalAnimals > 0) Math.round(completedAnimals * 100f / totalAnimals) else 0

// A drive event's flat subtitle/summary rows render ONLY when there is no backend
// drive_summary. When a summary is present the DriveProgressCard renders the sheds/
// vaccines/dose totals, so the flat rows would duplicate them (CDR-003). Returns true when
// the legacy rows should show (summary absent) and false when they must be hidden (present).
internal fun legacyDriveRowsVisible(drive: CalendarDriveSummary?): Boolean = drive == null

// The coverage ring's numerator/denominator + which grain it represents.
internal data class DriveCoverage(val completed: Int, val total: Int, val usesAnimals: Boolean)

// Distinct-animal counts are authoritative for the ring, BUT a Room cache written before the
// total_animals/completed_animals fields shipped -- or a mixed-version API response mid-rollout --
// has NO animal counts. They decode as null (not 0). Rendering them as "0 / 0 animals" over a drive
// with valid legacy dose counts is a false empty state (CDR-R1). When either animal count is absent,
// fall back to the obligation (dose) counts that ARE present, honestly labelled; a background refresh
// then repopulates the animal grain.
internal fun driveCoverage(
    completedAnimals: Int?,
    totalAnimals: Int?,
    completedDoses: Int,
    totalDoses: Int,
): DriveCoverage =
    if (completedAnimals != null && totalAnimals != null) {
        DriveCoverage(completedAnimals, totalAnimals, usesAnimals = true)
    } else {
        DriveCoverage(completedDoses, totalDoses, usesAnimals = false)
    }
