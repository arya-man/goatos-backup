// Park ORDER on every All-parks surface: CBE first, then CPT — two clusters, never interleaved
// (maintainer decision 2026-09-16).
//
// The backend owns the order: every park-bearing read sorts its rows by the park CODE, and the
// page's own park vocabulary (`parks`) arrives in that same order. Sorting park labels here by
// name is what went wrong before — "Channapatna" (CPT) sorts ahead of "Coimbatore" (CBE), so
// every name-keyed sort put CPT first — so this module never compares labels; it ranks a park by
// its position in an ordered list the backend served, and a park that list does not name goes
// last, after every ranked one, in its own arrival order.

/** The distinct parks of `rows`, in the order the backend served them. */
export function parksInArrivalOrder<T>(rows: readonly T[], park: (row: T) => string): string[] {
  const seen: string[] = [];
  for (const row of rows) {
    const label = park(row);
    if (!seen.includes(label)) seen.push(label);
  }
  return seen;
}

/** Rank of a park in the backend-served order; unknown parks rank after every known one. */
export function parkRank(order: readonly string[]): (park: string) => number {
  return (park) => {
    const index = order.indexOf(park);
    return index === -1 ? order.length : index;
  };
}

/**
 * A comparator that clusters by park in the served order and, inside a cluster, defers to
 * `within` (the surface's own order: pen A→Z, heaviest first, …). Stable, so two rows the
 * comparator ties keep their arrival order.
 */
export function byParkThen<T>(
  order: readonly string[],
  park: (row: T) => string,
  within: (a: T, b: T) => number = () => 0,
): (a: T, b: T) => number {
  const rank = parkRank(order);
  return (a, b) => rank(park(a)) - rank(park(b)) || within(a, b);
}
