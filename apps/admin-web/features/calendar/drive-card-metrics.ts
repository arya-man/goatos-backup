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

export interface DriveCoverage {
  completed: number;
  total: number;
  usesAnimals: boolean;
}

// Distinct-animal counts are authoritative for the ring, BUT a mixed-version API response mid-rollout
// (an older backend instance that predates the total_animals/completed_animals fields) can omit them --
// they arrive as undefined, NOT 0. Rendering "0 / 0 animals" over a drive with valid dose counts is a
// false empty state (CDR-R1). When either animal count is missing, fall back to the obligation (dose)
// counts that ARE present, labelled accordingly; a later response repopulates the animal grain.
export function driveCoverage(
  completedAnimals: number | null | undefined,
  totalAnimals: number | null | undefined,
  completedDoses: number,
  totalDoses: number,
): DriveCoverage {
  if (typeof completedAnimals === "number" && typeof totalAnimals === "number") {
    return { completed: completedAnimals, total: totalAnimals, usesAnimals: true };
  }
  return { completed: completedDoses, total: totalDoses, usesAnimals: false };
}
