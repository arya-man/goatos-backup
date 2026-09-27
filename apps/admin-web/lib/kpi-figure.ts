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
