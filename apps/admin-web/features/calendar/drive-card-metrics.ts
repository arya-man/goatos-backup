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

export function driveClosedCoveragePct(completedAnimals: number, totalAnimals: number, eventStatus: string | null | undefined): number {
  if (eventStatus === "completed") return driveCoveragePct(completedAnimals, totalAnimals);
  if (totalAnimals <= 0 || completedAnimals <= 0) return 0;
  return Math.min(99, driveCoveragePct(completedAnimals, totalAnimals));
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

// The drive progress the card renders. The backend owns the numerator, the denominator and the
// GRAIN they are counted on (progress_basis), and BOTH clients render it verbatim -- this is the
// cross-surface parity contract. Before it existed this card used completed_animals while the
// Android card used max(submitted, completed), so the SAME drive showed two different completion
// numbers and two different ring percentages. The numerator is verified completion only; submitted-
// but-unverified work stays visible through submitted_animals and the verification-pending strip.
// The local derivation is a LEGACY fallback for a mixed-version response from an older backend that
// predates these fields (they arrive undefined, not 0).
export function driveVisibleProgress(summary: {
  progress_basis?: string | null;
  progress_completed?: number | null;
  progress_total?: number | null;
  completed_animals?: number | null;
  total_animals?: number | null;
  completed_count: number;
  total_count: number;
}): DriveCoverage {
  if (typeof summary.progress_completed === "number" && typeof summary.progress_total === "number") {
    return {
      completed: summary.progress_completed,
      total: summary.progress_total,
      usesAnimals: summary.progress_basis !== "doses",
    };
  }
  return driveCoverage(summary.completed_animals, summary.total_animals, summary.completed_count, summary.total_count);
}

// Backend-rounded percentage wins verbatim so the ring reads identically on web and mobile; the
// local half-up rounding is the same legacy fallback.
export function drivePctFor(summary: { progress_pct?: number | null }, coverage: DriveCoverage): number {
  if (typeof summary.progress_pct === "number") return summary.progress_pct;
  return driveCoveragePct(coverage.completed, coverage.total);
}

export interface DriveStatusChip {
  key: "completed" | "submitted" | "due" | "overdue" | "deferred";
  count: number;
}

export function driveStatusClass(key: DriveStatusChip["key"]): "completed" | "due" | "over" | "def" {
  if (key === "overdue") return "over";
  if (key === "deferred") return "def";
  if (key === "submitted") return "due";
  return key;
}

// Returns all nonzero status buckets in fixed order: completed, due, overdue, deferred.
// Used to render the status chips row on the drive card.
export function driveStatusChips(summary: {
  completed_count: number;
  submitted_count?: number;
  due_count: number;
  overdue_count: number;
  deferred_count: number;
}): DriveStatusChip[] {
  return (
    [
      { key: "completed" as const, count: summary.completed_count },
      { key: "submitted" as const, count: summary.submitted_count ?? 0 },
      { key: "due" as const, count: summary.due_count },
      { key: "overdue" as const, count: summary.overdue_count },
      { key: "deferred" as const, count: summary.deferred_count },
    ] as const
  ).filter((chip) => chip.count > 0);
}
