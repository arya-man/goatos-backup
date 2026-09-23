// The record-sale drawer's line drafts: pure state helpers, no copy, no JSX, so they can be unit
// tested and shared by the editor and the drawer.

/**
 * One product line of the sale being recorded. Kept as STRINGS: the inputs are controlled text
 * boxes, and a half-typed "12." must survive a re-render. Parsing happens once, in the server
 * action, where blank means "not recorded" rather than 0.
 *
 * `breed` is the line's VARIANT -- a breed on an animal line, the feed item on a feed line. It
 * keeps the older name because that is the field the backend stores and every reader already keys
 * on; widening the meaning cost nothing, renaming it would have cost six read models.
 */
export type SaleLineDraft = {
  /** Client-only key for React; never posted. */
  id: number;
  product: string;
  breed: string;
  animals: string;
  weightKg: string;
  value: string;
  /** Feed is sold by the kilogram at a rate. Blank on an animal line. */
  quantity: string;
  rate: string;
};

export function newSaleLine(id: number, product: string): SaleLineDraft {
  return { id, product, breed: "", animals: "", weightKg: "", value: "", quantity: "", rate: "" };
}

function parseOrZero(raw: string): number {
  const n = Number(raw.trim());
  return Number.isFinite(n) && raw.trim() !== "" ? n : 0;
}

/**
 * What a line is worth, for the footer preview.
 *
 * A line priced by the unit works its value out from quantity times rate, exactly as the backend
 * does -- so the total the operator watches while typing is the total that gets recorded, and the
 * value box on a feed line is a readout rather than an input somebody could contradict.
 */
export function saleLineValue(line: SaleLineDraft): number {
  if (line.quantity.trim() !== "" && line.rate.trim() !== "") {
    return parseOrZero(line.quantity) * parseOrZero(line.rate);
  }
  return parseOrZero(line.value);
}

/** The running total the footer previews -- the BACKEND computes the recorded one. */
export function saleLinesTotals(lines: SaleLineDraft[]): {
  value: number;
  animals: number;
  weightKg: number;
  feedKg: number;
} {
  return lines.reduce(
    (acc, line) => ({
      value: acc.value + saleLineValue(line),
      animals: acc.animals + parseOrZero(line.animals),
      weightKg: acc.weightKg + parseOrZero(line.weightKg),
      feedKg: acc.feedKg + parseOrZero(line.quantity),
    }),
    { value: 0, animals: 0, weightKg: 0, feedKg: 0 },
  );
}
