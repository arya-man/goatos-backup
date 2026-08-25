const DAYS_PER = { days: 1, weeks: 7, months: 30, years: 365 } as const;
export type DurationUnit = keyof typeof DAYS_PER;

/**
 * Picks the unit that expresses the value exactly.
 *
 * A month here is 30 days, so 182 days is not "6 months" -- rendering it as one
 * and then saving what the picker produces would move scheduled dates.
 */
export function splitDays(days: number): { value: number; unit: DurationUnit } {
  if (days > 0 && days % DAYS_PER.years === 0) return { value: days / DAYS_PER.years, unit: "years" };
  if (days > 0 && days % DAYS_PER.weeks === 0 && days < 70) return { value: days / DAYS_PER.weeks, unit: "weeks" };
  if (days > 0 && days % DAYS_PER.months === 0) return { value: days / DAYS_PER.months, unit: "months" };
  if (days > 0 && days % DAYS_PER.weeks === 0) return { value: days / DAYS_PER.weeks, unit: "weeks" };
  return { value: days, unit: "days" };
}

/** "4 weeks", "6 months", "1 year". The number is never dropped, even at one. */
export function formatDays(days: number | null | undefined): string {
  if (days === null || days === undefined || Number.isNaN(days)) return "—";
  const { value, unit } = splitDays(days);
  return `${value} ${value === 1 ? unit.slice(0, -1) : unit}`;
}
