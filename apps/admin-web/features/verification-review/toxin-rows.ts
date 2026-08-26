// Pure view-model mapping for the Toxin review list (maintainer decision 2026-08-25).
//
// Deliberately a server-safe module with no JSX: the list renders BACKEND-OWNED copy verbatim
// (status_chip, context_line, outcome_label, origin_line, cancel_reason) and this mapping is the
// one place that picks which payload fields a row shows — tested directly by
// toxin-review-render.test.mjs against a fixture payload, so a renderer edit cannot silently start
// composing business copy client-side.
import type { ToxinTask } from "@/lib/api/server";

export type ToxinReviewRow = {
  taskId: string;
  /** Backend-composed card subtitle: feed, vendor, load and date in one farm-worded line. */
  contextLine: string;
  /** Backend-owned status chip copy, rendered verbatim. */
  statusChip: string;
  /** Farm-worded reading ("Negative", "Positive", "Invalid strip"); "" until step 7. */
  outcomeLabel: string;
  /** Wire business date (YYYY-MM-DD); the renderer formats it through fmtDate. */
  purchaseDate: string;
  /** Backend retest explanation; non-empty exactly when this is a retest round (round_no > 1). */
  roundChip: string;
  stepsDone: number;
  stepsTotal: number;
  rowVersion: number;
};

export function toxinReviewRows(tasks: ToxinTask[]): ToxinReviewRow[] {
  return tasks.map((task) => ({
    taskId: task.task_id,
    contextLine: task.context_line,
    statusChip: task.status_chip,
    outcomeLabel: task.outcome_label ?? "",
    purchaseDate: task.purchase_date,
    // The round chip is shown only past round 1, and its copy is the backend's origin_line — never
    // a client-composed "Round N" sentence.
    roundChip: task.round_no > 1 ? (task.origin_line ?? "") : "",
    stepsDone: task.steps_done,
    stepsTotal: task.steps_total,
    rowVersion: task.row_version,
  }));
}
