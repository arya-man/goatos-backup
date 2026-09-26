import type { ReactNode } from "react";

/** One half of a two-part reading ("Individual 54 · Lump-sum 7"). */
export type KpiPart = { label: ReactNode; value: number | string };

/**
 * Pairs a contract label that names both halves in order ("Individual · Lump-sum") with its two
 * counts. Server-safe: server pages build `parts` before handing them to the client KpiCard.
 */
export function splitParts(label: string, values: [number, number]): KpiPart[] {
  const names = label.split(" · ");
  return values.map((value, index) => ({ label: names[index] ?? label, value }));
}
