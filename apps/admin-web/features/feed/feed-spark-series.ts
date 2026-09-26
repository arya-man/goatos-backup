/**
 * KPI sparkline series for the feed tiles.
 *
 * A trailing day whose figure is missing, zero, or below 60% of the median of the 7 real days
 * before it is a day still being recorded (a second park's sheet not issued yet, a half-fed day),
 * not a real drop, so it is dropped and the line ends at the last complete day. Only the TAIL is
 * trimmed; a genuine low day in the middle of the window stays on the line.
 */
export const PARTIAL_DAY_SHARE = 0.6;

function median(values: number[]): number {
  const sorted = [...values].sort((a, b) => a - b);
  const mid = Math.floor(sorted.length / 2);
  return sorted.length % 2 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2;
}

export function completeDaySeries(values: (number | null)[]): number[] | undefined {
  let end = values.length;
  while (end > 0) {
    const last = values[end - 1];
    if (last === null || last <= 0) {
      end -= 1;
      continue;
    }
    const trailing = values.slice(Math.max(0, end - 8), end - 1).filter((v): v is number => v !== null && v > 0);
    if (trailing.length >= 3 && last < PARTIAL_DAY_SHARE * median(trailing)) {
      end -= 1;
      continue;
    }
    break;
  }
  const kept = values.slice(0, end);
  return kept.length > 1 && kept.every((v) => v !== null) ? (kept as number[]) : undefined;
}
