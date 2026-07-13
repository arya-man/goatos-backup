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
