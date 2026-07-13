// Pure, unit-tested metrics for the park-level vaccination drive card. Extracted from
// calendar-drive-card.tsx so the coverage-percentage rounding cannot silently regress
// (CDR-005). Covered by drive-card-metrics.test.mjs.

// Coverage-ring percentage on the DISTINCT-ANIMAL grain, rounded half-up to match the
// Android card (CalendarScreen.kt also uses Math.round). Example: 46/77 -> 60 (NOT the
// truncated 59 the pre-fix Android card produced). Returns 0 when there are no animals.
export function driveCoveragePct(completedAnimals: number, totalAnimals: number): number {
  if (totalAnimals <= 0) return 0;
  return Math.round((completedAnimals / totalAnimals) * 100);
}
