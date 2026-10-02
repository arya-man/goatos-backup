import { fmtDate } from "../../lib/format.ts";
import { stageLabel } from "../../lib/stage-labels.ts";

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

/**
 * The value an EDITOR opens with: the stored number without its padding ("1500.000" -> "1500",
 * "12.500" -> "12.5"), and never a thousands separator, because it goes back to the server as typed.
 */
export function fmtInputNumber(raw: string | null | undefined): string {
  if (raw === null || raw === undefined || raw.trim() === "") return "";
  const n = Number(raw);
  return Number.isFinite(n) ? String(n) : raw;
}

/**
 * An experiment ARM as a reader sees it. Arms are free prose authored per pen and imported from the
 * trial workbook ("Sheep M NEW", "B+S Goat F OLD", "Shed-average plan 2026-09-07"), so there is no
 * vocabulary to look them up in; only the shapes the console bans are rewritten, at the point of
 * display (D4). The stored arm is untouched -- the editor still opens on it and the filter still
 * sends it.
 *  - an ISO date reads DD/MM/YYYY;
 *  - "shed" reads "pen" (the word on screen is pen);
 *  - the sex shorthand after a species reads as the word ("Goat F" -> "Goat Female");
 *  - NEW / OLD read as words ("New" / "Old");
 *  - the fattening token reads "Fattening" (stageLabel).
 * Breed shorthands ("B+S", "M+O") are kept: what they stand for is the trial's, not ours to guess.
 */
export function fmtExperimentArm(raw: string | null | undefined): string {
  if (!raw) return "";
  return stageLabel(raw)
    .replace(/\b(\d{4}-\d{2}-\d{2})\b/g, (iso: string) => fmtDate(iso))
    .replace(/\b(shed)(s?)\b/gi, (_m: string, word: string, plural: string) => (word[0] === "S" ? "Pen" : "pen") + plural)
    .replace(/\b(Goats?|Sheep)\s+([MF])\b/g, (_m: string, species: string, sex: string) => `${species} ${sex === "M" ? "Male" : "Female"}`)
    .replace(/\bNEW\b/g, "New")
    .replace(/\bOLD\b/g, "Old");
}
