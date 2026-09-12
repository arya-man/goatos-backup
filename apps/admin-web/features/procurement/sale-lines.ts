// The record-sale drawer's line drafts: pure state helpers, no copy, no JSX, so they can be unit
// tested and shared by the editor and the drawer.

/**
 * One product/breed line of the sale being recorded. Kept as STRINGS: the inputs are controlled
 * text boxes, and a half-typed "12." must survive a re-render. Parsing happens once, in the
 * server action, where blank means "not recorded" rather than 0.
 */
export type SaleLineDraft = {
  /** Client-only key for React; never posted. */
  id: number;
  product: string;
  breed: string;
  animals: string;
  weightKg: string;
  value: string;
};

export function newSaleLine(id: number, product: string): SaleLineDraft {
  return { id, product, breed: "", animals: "", weightKg: "", value: "" };
}

function parseOrZero(raw: string): number {
  const n = Number(raw.trim());
  return Number.isFinite(n) && raw.trim() !== "" ? n : 0;
}

/** The running total the footer previews -- the BACKEND computes the recorded one. */
export function saleLinesTotals(lines: SaleLineDraft[]): { value: number; animals: number; weightKg: number } {
  return lines.reduce(
    (acc, line) => ({
      value: acc.value + parseOrZero(line.value),
      animals: acc.animals + parseOrZero(line.animals),
      weightKg: acc.weightKg + parseOrZero(line.weightKg),
    }),
    { value: 0, animals: 0, weightKg: 0 },
  );
}
