// Long KPI figures (FXC, /sales/loads Profit / loss "2,84,663.25" running under the widget's corner
// icon). The verbatim template widgets print a NUMBER (fNumber), so the adapter hands them a
// compacted en-IN figure (lakh / crore, two decimals) and moves the scale word plus the EXACT value
// into the visible sub-line: "2.85" over "₹ lakh · 2,84,663.25". Figures under one lakh are
// unchanged.

export const COMPACT_FROM = 100_000;

/** A short unit token that the scale word can join ("₹" -> "₹ lakh", "kg" -> "kg lakh" is not used). */
const UNIT = /^(₹|Rs\.?|INR)$/;
/** A measured unit that stays beside its figure when the figure is compacted ("2.19 lakh kg"). */
const FIGURE_UNIT = /^(kg|g|h|hours|days)$/;

export function compactFigure(total: number | null | undefined, caption?: string): { total: number | null | undefined; caption?: string } {
  if (total == null || !Number.isFinite(total) || Math.abs(total) < COMPACT_FROM) return { total, caption };
  const [divisor, word] = Math.abs(total) >= 10_000_000 ? [10_000_000, "crore"] : [100_000, "lakh"];
  const parts = caption ? caption.split(" · ") : [];
  const lead = parts.length ? parts[0].trim() : "";
  const money = UNIT.test(lead);
  const unit = !money && FIGURE_UNIT.test(lead) ? lead : "";
  // The exact value carries its own unit: money is whole rupees with the ₹ attached ("₹89,93,623"),
  // never "₹ lakh · 89,93,623.25"; a measured figure keeps its unit beside it ("2,19,305 kg"), never
  // "lakh · 2,19,305 · kg" (PR #294 KPI sweep).
  const exact = total.toLocaleString("en-IN", { maximumFractionDigits: money ? 0 : 2 });
  const rest = money || unit ? parts.slice(1) : parts;
  const head = money ? [`₹ ${word}`, `${total < 0 ? "-" : ""}₹${exact.replace(/^-/, "")}`] : unit ? [`${word} ${unit}`, `${exact} ${unit}`] : [word, exact];
  return { total: Math.round((total / divisor) * 100) / 100, caption: [...head, ...rest].join(" · ") };
}

// The figure's UNIT belongs beside the figure, not at the head of the sub-line (PR #294 C2/D1):
// callers follow the adapter's caption convention -- unit (or "—" for a missing figure) first,
// then the detail -- which painted "80" over "% · Packing + distribution approved…" and "" over
// "— · kg on the issued sheet". The adapter lifts that lead off the caption: a unit joins the figure
// ("80%", "₹23.77 lakh", "75,341 kg") and a missing figure shows "—" as the headline itself.

/** A caption lead that is a unit of the figure (or the missing-figure dash), never prose. */
const UNIT_LEAD = /^(—|%|₹|Rs\.?|INR|kg|g|h|hours|days|lakh|crore|(?:₹|Rs\.?|INR) (?:lakh|crore)|(?:lakh|crore) (?:kg|g|h|hours|days))$/;

export type FigureUnit = {
  /** Printed right before the figure ("₹"). */
  prefix: string;
  /** Printed right after the figure ("%", " kg", " lakh"). */
  suffix: string;
  /** The sub-line without the lifted lead; undefined when nothing is left. */
  caption?: string;
  /** The figure is missing: the headline is "—". */
  missing: boolean;
};

export function liftFigureUnit(total: number | null | undefined, caption: string | undefined): FigureUnit {
  const missing = total == null || !Number.isFinite(total);
  const parts = caption ? caption.split(" · ") : [];
  const lead = parts.length ? parts[0].trim() : "";
  if (!UNIT_LEAD.test(lead)) return { prefix: "", suffix: "", caption, missing };
  const rest = parts.slice(1).join(" · ") || undefined;
  if (missing || lead === "—") return { prefix: "", suffix: "", caption: rest, missing };
  const [symbol, scale] = /^(₹|Rs\.?|INR)(?: (lakh|crore))?$/.exec(lead)?.slice(1) ?? [];
  if (symbol) return { prefix: "₹", suffix: scale ? ` ${scale}` : "", caption: rest, missing };
  return { prefix: "", suffix: lead === "%" ? "%" : ` ${lead}`, caption: rest, missing };
}
