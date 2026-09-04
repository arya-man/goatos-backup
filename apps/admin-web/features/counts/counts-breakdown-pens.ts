// Pure helpers for the Counts Breakdown pen table. Kept free of React so the identity and label
// rules the table relies on can be pinned by a node test without rendering anything.

import type { CountsBreakdownResponse } from "@/lib/api/server";

export type CountsBreakdownPenRow = CountsBreakdownResponse["pens"][number];
export type CountsBreakdownPoint = CountsBreakdownPenRow["stages"][number];

/**
 * The full pen identity: park + shed + partition. Dropping any part collapses two genuinely
 * different pens into one React identity (66 of 154 shed names exist in both parks, and a
 * partitioned shed is several pens under one shed id), and shed_id alone is not the ground
 * location when a partition exists.
 */
export function penRowId(pen: Pick<CountsBreakdownPenRow, "park_id" | "shed_id" | "partition_label">): string {
  return `${pen.park_id ?? ""}|${pen.shed_id ?? ""}|${pen.partition_label ?? ""}`;
}

/** A DOM id for the pen's opened detail cell, so the toggle can name it via aria-controls. */
export function penDetailDomId(pen: Pick<CountsBreakdownPenRow, "park_id" | "shed_id" | "partition_label">): string {
  return `pen-detail-${penRowId(pen).replace(/[^A-Za-z0-9_-]+/g, "_")}`;
}

/**
 * Labels one composition bucket. The backend repeats the raw key as the label and leaves the
 * blank bucket blank on purpose: the "No stage" / "No breed" wording is contract copy the page
 * owns, and a sex key is rendered through the page's own gender vocabulary so "female" reads as
 * the option label the filter beside it uses, never as the stored token.
 */
export function pointLabel(
  point: Pick<CountsBreakdownPoint, "key" | "label">,
  emptyLabel: string,
  vocabulary?: ReadonlyMap<string, string>,
): string {
  if (!point.key) return emptyLabel;
  return vocabulary?.get(point.key) ?? point.label ?? point.key;
}

/**
 * The value a sortable composition column orders by: the pen's DOMINANT bucket, which the backend
 * lists first. A pen with nothing recorded sorts as blank rather than by a fallback label, so the
 * "No stage" pens group together at one end instead of being interleaved by copy.
 */
export function dominantKey(points: readonly Pick<CountsBreakdownPoint, "key">[]): string {
  return points[0]?.key ?? "";
}

/** Whether every pen on the page is open — what decides which of Open all / Close all is offered. */
export function allOpen(pens: readonly CountsBreakdownPenRow[], open: ReadonlySet<string>): boolean {
  return pens.length > 0 && pens.every((pen) => open.has(penRowId(pen)));
}
