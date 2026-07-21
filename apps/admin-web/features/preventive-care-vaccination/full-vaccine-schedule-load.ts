export type ScheduleLoadTone = "ok" | "warn" | "danger" | "done";

export type ScheduleLoadBucket = {
  key: "scheduled" | "deferred" | "overdue" | "done";
  tone: ScheduleLoadTone;
  value: number;
};

// The vaccination-operations counts are per-OBLIGATION tallies (vaccine tasks),
// NOT goats. `total` is COUNT(*) across every effective obligation status; the
// eff_status buckets are mutually exclusive and exhaustive. `animals` is the
// separate COUNT(DISTINCT goat_id). We only read the disjoint eff_status
// fields (never proof_pending / rejected, which are completion-status overlays
// that can co-occur with any eff_status and would double-count).
type ScheduleLoadCounts = {
  total: number;
  scheduled: number;
  due: number;
  inProgress: number;
  deferred: number;
  overdue: number;
  missed: number;
};

export type ScheduleLoadBuckets = {
  total: number;
  goats: number;
  buckets: ScheduleLoadBucket[];
};

// Partition the whole obligation total into disjoint display groups whose
// values sum EXACTLY to `total`:
//   scheduled = scheduled + due + in_progress   (open, on-track / upcoming)
//   deferred  = deferred                          (held for medical reason)
//   overdue   = overdue + missed                  (past due, needs attention)
//   done      = total - the above                 (completed family)
export function scheduleLoadBuckets(counts: ScheduleLoadCounts, animals: number): ScheduleLoadBuckets {
  const total = Math.max(0, counts.total);
  const scheduled = counts.scheduled + counts.due + counts.inProgress;
  const deferred = counts.deferred;
  const overdue = counts.overdue + counts.missed;
  const done = Math.max(0, total - scheduled - deferred - overdue);
  return {
    total,
    goats: animals,
    buckets: [
      { key: "scheduled", tone: "ok", value: scheduled },
      { key: "deferred", tone: "warn", value: deferred },
      { key: "overdue", tone: "danger", value: overdue },
      { key: "done", tone: "done", value: done },
    ],
  };
}
