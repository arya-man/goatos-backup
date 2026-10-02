// Long KPI figures (FXC, /sales/loads Profit / loss "2,84,663.25" running under the widget's corner
// icon). The verbatim template widgets print a NUMBER (fNumber), so the adapter hands them a
// compacted en-IN figure (lakh / crore, two decimals) and moves the scale word plus the EXACT value
// into the visible sub-line: "2.85" over "₹ lakh · 2,84,663.25". Figures under one lakh are
// unchanged.

export const COMPACT_FROM = 100_000;

/** A short unit token that the scale word can join ("₹" -> "₹ lakh", "kg" -> "kg lakh" is not used). */
const UNIT = /^(₹|Rs\.?|INR)$/;

export function compactFigure(total: number | null | undefined, caption?: string): { total: number | null | undefined; caption?: string } {
  if (total == null || !Number.isFinite(total) || Math.abs(total) < COMPACT_FROM) return { total, caption };
  const [divisor, word] = Math.abs(total) >= 10_000_000 ? [10_000_000, "crore"] : [100_000, "lakh"];
  const exact = total.toLocaleString("en-IN", { maximumFractionDigits: 2 });
  const parts = caption ? caption.split(" · ") : [];
  const head = parts.length && UNIT.test(parts[0].trim()) ? [`${parts[0].trim()} ${word}`, exact, ...parts.slice(1)] : [word, exact, ...parts];
  return { total: Math.round((total / divisor) * 100) / 100, caption: head.join(" · ") };
}

// The figure's UNIT belongs beside the figure, not at the head of the sub-line (PR #294 C2/D1):
// callers follow the adapter's caption convention -- unit (or "—" for a missing figure) first,
// then the detail -- which painted "80" over "% · Packing + distribution approved…" and "" over
// "— · kg on the issued sheet". The adapter lifts that lead off the caption: a unit joins the figure
// ("80%", "₹23.77 lakh", "75,341 kg") and a missing figure shows "—" as the headline itself.

/** A caption lead that is a unit of the figure (or the missing-figure dash), never prose. */
const UNIT_LEAD = /^(—|%|₹|Rs\.?|INR|kg|g|h|hours|days|lakh|crore|(?:₹|Rs\.?|INR) (?:lakh|crore))$/;

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
  const lead = parts.length > 1 || (parts.length === 1 && parts[0].trim() === "—") ? parts[0].trim() : "";
  if (!UNIT_LEAD.test(lead)) return { prefix: "", suffix: "", caption, missing };
  const rest = parts.slice(1).join(" · ") || undefined;
  if (missing || lead === "—") return { prefix: "", suffix: "", caption: rest, missing };
  const [symbol, scale] = /^(₹|Rs\.?|INR)(?: (lakh|crore))?$/.exec(lead)?.slice(1) ?? [];
  if (symbol) return { prefix: "₹", suffix: scale ? ` ${scale}` : "", caption: rest, missing };
  return { prefix: "", suffix: lead === "%" ? "%" : ` ${lead}`, caption: rest, missing };
}
