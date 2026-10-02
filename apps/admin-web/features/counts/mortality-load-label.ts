// Mortality's load labels, kept free of React so node --test can pin them (PR #294 O3).
//
// Every load chart names its pens (docs/decisions/load-charts-name-their-pens.md), but a mortality
// TABLE is not an axis: spelling two park-qualified pens out pushed "Rate" off the card at 1440 and
// "Deaths" off a phone. So a load row reads "Load 128 (CBE Castro 3 +2)" — the load number, its
// biggest pen and how many more — and the full bracket rides in the cell's title. The Load × cause
// cross tab and the deaths list used to print a bare "Load 128"; they take the SAME composition,
// resolved from the load series' own pens, so one load reads alike in all three places.
import { withLoadPens, type LoadPen } from "../../lib/load-pens.ts";

/** Pens spelled out in a mortality load cell; the rest are counted ("+2"). */
export const MORTALITY_LOAD_PENS_SHOWN = 1;

export type LoadLabel = { text: string; full: string };

/** The compact cell text and the full title for one load. A load with no known pen is its label alone. */
export function mortalityLoadLabel(label: string, pens: readonly LoadPen[] | null | undefined): LoadLabel {
  return { text: withLoadPens(label, pens, MORTALITY_LOAD_PENS_SHOWN), full: withLoadPens(label, pens, Number.POSITIVE_INFINITY) };
}

type LoadBucketLike = { key: string; label: string; pens?: readonly LoadPen[] | null };

/**
 * The load series' pens indexed two ways: by bucket KEY (the cross tab's row_key is the same
 * load_key) and by LABEL (the deaths list carries only the "Load 128" reference). A label two loads
 * share resolves to nothing — never one load's pens printed under another (agree-or-go-bare).
 */
export function loadPensIndex(buckets: readonly LoadBucketLike[]): {
  byKey: (key: string) => readonly LoadPen[] | undefined;
  byLabel: (label: string) => readonly LoadPen[] | undefined;
} {
  const byKey = new Map<string, readonly LoadPen[]>();
  const byLabel = new Map<string, readonly LoadPen[] | null>();
  for (const bucket of buckets) {
    const pens = bucket.pens ?? [];
    if (pens.length === 0) continue;
    byKey.set(bucket.key, pens);
    const label = bucket.label.trim();
    if (label === "") continue;
    byLabel.set(label, byLabel.has(label) ? null : pens);
  }
  return {
    byKey: (key) => byKey.get(key),
    byLabel: (label) => byLabel.get(label.trim()) ?? undefined,
  };
}
