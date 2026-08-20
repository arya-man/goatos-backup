/**
 * Reads the stored rule_dsl into the shape the console shows.
 *
 * The document's real structure is `matrix_rows`: one row per vaccine, carrying
 * its name, class, disease and its own `schedule` of dose rules. A row whose
 * schedule is EMPTY is a vaccine the farm knows about but has switched off --
 * that is what makes "5 of 7" a fact rather than an estimate, and it is why a
 * dropped vaccine stays on screen instead of vanishing.
 *
 * A person does not think in dose rules; they think in a vaccine and, inside
 * one, in first doses and repeats. This module is that translation and nothing
 * else: no defaults are invented, so a field the farm never configured reads as
 * absent rather than as a plausible-looking guess.
 *
 * See docs/preventive-care-vaccination/design/MOCK-BEHAVIOUR-SPEC.md §4.
 */

import { formatDays } from "./duration-field";

export type ScheduleRule = {
  dose_code?: string;
  sequence?: number;
  trigger_type?: string;
  offset_days?: number;
  due_window_days?: number;
  max_delay_days?: number;
  repeat?: string;
  min_gap_days?: number;
};

export type VaccineGroup = {
  /** Stable identity: the matrix row's vaccine code, e.g. "ET_TT". */
  code: string;
  /** What the farm calls it, taken from the document, e.g. "ET+TT". */
  name: string;
  /** "live" | "killed" — drives the spacing rules, shown as reference only. */
  vaccineClass: string;
  disease: string;
  /** False when the row exists but carries no schedule: known, deliberately off. */
  inPlan: boolean;
  firstDoses: ScheduleRule[];
  repeats: ScheduleRule[];
};

type MatrixRow = {
  row_id?: string;
  vaccine?: { code?: string; name?: string; type?: string; disease?: string; priority?: number };
  schedule?: ScheduleRule[];
};

/**
 * Every vaccine in the plan document, switched-on ones first.
 *
 * Order matters: a reader scans the live plan far more often than the retired
 * rows, so the ones in force must not be interleaved with the ones that are not.
 */
export function readVaccines(ruleDsl: unknown): VaccineGroup[] {
  const rows = readMatrixRows(ruleDsl);
  if (rows.length === 0) return [];

  const groups = rows.map((row) => {
    const schedule = Array.isArray(row.schedule) ? row.schedule : [];
    const code = row.vaccine?.code ?? row.row_id ?? "";
    return {
      code,
      name: row.vaccine?.name?.trim() || prettyCode(code),
      vaccineClass: row.vaccine?.type ?? "",
      disease: row.vaccine?.disease ?? "",
      inPlan: schedule.length > 0,
      firstDoses: schedule.filter((r) => !r.repeat || r.repeat === "none"),
      repeats: schedule.filter((r) => r.repeat && r.repeat !== "none"),
    };
  });

  return [...groups.filter((g) => g.inPlan), ...groups.filter((g) => !g.inPlan)];
}

function readMatrixRows(ruleDsl: unknown): MatrixRow[] {
  if (!ruleDsl || typeof ruleDsl !== "object") return [];
  const rows = (ruleDsl as { matrix_rows?: unknown }).matrix_rows;
  return Array.isArray(rows) ? (rows as MatrixRow[]) : [];
}

/** Only for a row with no name of its own: "GOAT_POX" -> "Goat Pox". */
function prettyCode(code: string): string {
  const parts = code.split("_").filter(Boolean);
  if (parts.length === 0) return code;
  if (parts.every((p) => p.length <= 3)) return parts.map((p) => p.toUpperCase()).join(" + ");
  return parts.map((p) => p[0].toUpperCase() + p.slice(1).toLowerCase()).join(" ");
}

/**
 * "2 from date of birth · 1 on a drive" — how the doses are TRIGGERED, not
 * their codes.
 *
 * The trigger is the distinction that actually matters to a reader: a birth_age
 * dose lands on its own from the animal's date of birth, a manual_campaign dose
 * waits for someone to start a drive.
 */
export function describeFirstDoses(rules: ScheduleRule[]): string {
  if (rules.length === 0) return "—";
  const kid = rules.filter((r) => r.trigger_type === "birth_age").length;
  const campaign = rules.filter((r) => r.trigger_type === "manual_campaign").length;
  const followOn = rules.filter((r) => r.trigger_type === "after_previous_completion").length;
  const parts: string[] = [];
  if (kid) parts.push(`${kid} from date of birth`);
  if (campaign) parts.push(`${campaign} on a drive`);
  if (followOn) parts.push(`${followOn} follow-up`);
  return parts.length > 0 ? parts.join(" · ") : `${rules.length} dose${rules.length === 1 ? "" : "s"}`;
}

/** "Every 6 months" — the repeat cadence in the unit a person would say it in. */
export function describeRepeats(rules: ScheduleRule[]): string {
  if (rules.length === 0) return "Does not repeat";
  return rules.map((r) => `Every ${humanDays(r.offset_days)}`).join(" · ");
}

/**
 * Days, in the unit a person says — re-exported so every surface agrees.
 *
 * There is deliberately ONE implementation (duration-field.formatDays). When the
 * summary said "6 months" and the editable chip said "26 weeks" for the same 182
 * days, both were defensible and the pair was still a bug.
 */
export function humanDays(days: number | undefined | null): string {
  return formatDays(days);
}

/**
 * One line saying what a version changed, against the version before it.
 *
 * Derived by comparing the two documents rather than read from a stored note:
 * there is no change-note column, and a note a human typed could disagree with
 * what the version actually says. A derived line cannot.
 */
export function describeChange(current: VaccineGroup[], previous: VaccineGroup[] | null): string {
  const onNow = current.filter((g) => g.inPlan);
  if (!previous) return `First plan, with ${onNow.length} vaccine${onNow.length === 1 ? "" : "s"}.`;
  const onBefore = previous.filter((g) => g.inPlan);
  const before = new Set(onBefore.map((g) => g.code));
  const after = new Set(onNow.map((g) => g.code));
  const added = onNow.filter((g) => !before.has(g.code)).map((g) => g.name);
  const removed = onBefore.filter((g) => !after.has(g.code)).map((g) => g.name);

  const parts: string[] = [];
  if (added.length > 0) parts.push(`Added ${added.join(", ")}`);
  if (removed.length > 0) parts.push(`Dropped ${removed.join(", ")}`);
  if (parts.length > 0) return `${parts.join(" · ")}.`;

  return fingerprint(onNow) === fingerprint(onBefore) ? "No change to the vaccines." : "Timing changed.";
}

function fingerprint(groups: VaccineGroup[]): string {
  return groups
    .map((g) =>
      [...g.firstDoses, ...g.repeats]
        .map((r) => `${g.code}:${r.dose_code}@${r.offset_days ?? ""}/${r.repeat ?? ""}/${r.due_window_days ?? ""}`)
        .sort()
        .join(","),
    )
    .sort()
    .join("|");
}
