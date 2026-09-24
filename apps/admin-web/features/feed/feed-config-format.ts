// Display formatting for Feed Config's authored numbers. The WIRE keeps its exact decimals
// ("1200.000", "0.5000", "14:00:00"); only what a person reads is shaped here, in one place, so
// the ration grid, the experiment pens, the session split and the feeding clock read alike.

/** "1200.000" -> "1,200"; "12.500" -> "12.5"; "0.000" -> "0". Anything unparseable is shown as sent. */
export function fmtGrams(raw: string | null | undefined): string {
  if (raw === null || raw === undefined || raw.trim() === "") return "";
  const n = Number(raw);
  return Number.isFinite(n) ? n.toLocaleString("en-IN", { maximumFractionDigits: 3 }) : raw;
}

/** A session's share of the day: "0.5000" -> "50%", "0.3333" -> "33.33%". */
export function fmtSplit(raw: string | null | undefined): string {
  if (raw === null || raw === undefined || raw.trim() === "") return "";
  const n = Number(raw);
  return Number.isFinite(n) ? `${(n * 100).toLocaleString("en-IN", { maximumFractionDigits: 2 })}%` : raw;
}

/** A clock time without seconds: "14:00:00" -> "14:00". */
export function fmtClock(raw: string | null | undefined): string {
  if (raw === null || raw === undefined) return "";
  const m = /^(\d{1,2}:\d{2})(?::\d{2})?$/.exec(raw.trim());
  return m ? m[1] : raw;
}
