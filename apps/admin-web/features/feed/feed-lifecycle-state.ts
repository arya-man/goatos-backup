// Pure lifecycle-state predicates, isolated from the banner JSX so they can be unit-tested directly
// (the test runner strips TS types but cannot parse JSX, so the decision cannot live in the .tsx).
//
// The one rule that must never break: a `preview` (rows GENERATED on demand for a day with no issued
// sheet) HAS data, so it must render the table/KPIs — it is NOT a lifecycle-empty wall. Only the
// genuinely empty, nothing-issued states (`pending` / `not_issued`) with zero rows collapse the table
// into the explanatory banner. A `preview` with zero rows means the park holds no animals that day —
// a normal empty result, shown with the standard empty state, never the billboard.

import type { FeedDirectionLifecycle } from "@/lib/api/server";

/**
 * A served park-day whose empty table is EXPLAINED by the lifecycle (nothing was issued), as opposed
 * to a filter excluding rows from a sheet that does exist, or a `preview`/frozen sheet that has rows.
 * Only the former replaces the table with the banner.
 */
export function isLifecycleEmpty(lifecycle: FeedDirectionLifecycle, rowCount: number): boolean {
  return (
    rowCount === 0 &&
    (lifecycle.state === "pending" ||
      lifecycle.state === "not_issued" ||
      // `beyond_horizon` = the requested day has no issued sheet AND is outside the [today, tomorrow]
      // projection window, so the backend REFUSED to generate rows (it would be fabricated). It always
      // has zero rows and is explained by the banner, exactly like the nothing-issued states.
      lifecycle.state === "beyond_horizon")
  );
}
