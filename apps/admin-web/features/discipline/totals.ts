// How one violation change moves the Violations page's totals in place (maintainer decisions
// 2026-09-30), kept pure so `node --test` can pin it. The backend owns the numbers on load; after a
// write the page moves them the way the backend would have counted them:
//   recorded  -- a hand-recorded one: it counts, with its fine
//   withdrawn -- a recorded one taken back: it no longer counts
//   kept      -- HR kept a waiting one: it stops waiting and counts, with the fine HR typed
//   closed    -- HR closed a waiting one: it stops waiting and never counts
// Type-only imports keep this file import-free at runtime.

import { rupeesLabel } from "./format.ts";
import type { Violation, ViolationsPage } from "../../lib/api/server";

export type ViolationChange = "recorded" | "withdrawn" | "kept" | "closed";

type Totals = { summary: ViolationsPage["summary"]; byPerson: ViolationsPage["by_person"] };
type PersonRow = ViolationsPage["by_person"][number];

const deltas: Record<ViolationChange, { count: number; pending: number; closed: number }> = {
  recorded: { count: 1, pending: 0, closed: 0 },
  withdrawn: { count: -1, pending: 0, closed: 0 },
  kept: { count: 1, pending: -1, closed: 0 },
  closed: { count: 0, pending: -1, closed: 1 },
};

function hasAnything(r: PersonRow): boolean {
  return r.count + r.pending + r.closed + r.leave_days + r.leave_pending_days > 0;
}

/**
 * The totals after one change. The tiles count RECORDED violations whatever status tab is open (the
 * backend's rule), so they move on every tab -- a keep on "Waiting for HR" adds to Violations/Fines.
 */
export function applyViolationChange(cur: Totals, v: Violation, change: ViolationChange): Totals {
  const d = deltas[change];
  const fine = d.count * v.fine_rupees;
  const existing = cur.byPerson.find((r) => r.person_id === v.person_id);
  const base: PersonRow = existing ?? {
    person_id: v.person_id,
    person_name: v.person_name,
    designation: v.designation,
    park_label: v.park_label,
    count: 0,
    fine_rupees: 0,
    fine_label: rupeesLabel(0),
    pending: 0,
    closed: 0,
    leave_days: 0,
    leave_pending_days: 0,
    leave_label: "",
  };
  const nextRow: PersonRow = {
    ...base,
    count: Math.max(0, base.count + d.count),
    fine_rupees: base.fine_rupees + fine,
    fine_label: rupeesLabel(base.fine_rupees + fine),
    pending: Math.max(0, base.pending + d.pending),
    closed: base.closed + d.closed,
  };
  const byPerson = (existing ? cur.byPerson.map((r) => (r.person_id === v.person_id ? nextRow : r)) : [...cur.byPerson, nextRow])
    .filter(hasAnything)
    .sort((a, b) => b.fine_rupees - a.fine_rupees || b.count - a.count || b.pending - a.pending);
  const pending = Math.max(0, cur.summary.pending + d.pending);
  const summaryFine = cur.summary.fine_rupees + fine;
  return {
    byPerson,
    summary: {
      ...cur.summary,
      count: cur.summary.count + d.count,
      fine_rupees: summaryFine,
      fine_label: rupeesLabel(summaryFine),
      // People = distinct persons with a violation that counts -- the backend's own definition.
      people: byPerson.filter((r) => r.count > 0).length,
      pending,
    },
  };
}
