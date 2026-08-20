/**
 * Reads the stored rule_dsl into the shape the console shows.
 *
 * The plan is stored as a flat `schedule` array of dose rules. A person does not
 * think in dose rules — they think in vaccines ("ET + TT", "FMD") and, inside
 * one, in first doses and repeats. This module is that translation and nothing
 * else: no defaults are invented here, so a field the farm never configured
 * renders as absent rather than as a plausible-looking guess.
 *
 * See docs/preventive-care-vaccination/design/MOCK-BEHAVIOUR-SPEC.md §4.
 */

export type ScheduleRule = {
  dose_code?: string;
  sequence?: number;
  trigger_type?: string;
  offset_days?: number;
  due_window_days?: number;
  repeat?: string;
  min_gap_days?: number;
};

export type VaccineGroup = {
  /** Family code, e.g. "et_tt". Stable; used as the React key. */
  code: string;
  /** What the CEO calls it, e.g. "ET + TT". */
  name: string;
  firstDoses: ScheduleRule[];
  repeats: ScheduleRule[];
};

/** Suffixes that mark a rule's role inside its vaccine rather than its identity. */
const ROLE_SUFFIX = /_(kid|adult|revac)(_[a-z0-9]+)?$/;

/**
 * Acronyms are shouted, words are capitalised: "et_tt" -> "ET + TT",
 * "fmd" -> "FMD", "goat_pox" -> "Goat Pox".
 *
 * A code is treated as an acronym only when EVERY part is short. Deciding part
 * by part would shout the short word in a mixed code and print "Goat POX",
 * which is not what anyone on the farm calls it.
 */
export function vaccineName(code: string): string {
  const parts = code.split("_").filter(Boolean);
  if (parts.length === 0) return code;
  const acronym = parts.every((p) => p.length <= 3);
  if (acronym) {
    // A multi-part acronym code is a combination vaccine given in one shot, and
    // the farm says it that way, so join those with a plus rather than a space.
    return parts.map((p) => p.toUpperCase()).join(" + ");
  }
  return parts.map((p) => p[0].toUpperCase() + p.slice(1)).join(" ");
}

export function groupSchedule(ruleDsl: unknown): VaccineGroup[] {
  const schedule = readSchedule(ruleDsl);
  const byCode = new Map<string, VaccineGroup>();

  for (const rule of schedule) {
    const dose = rule.dose_code;
    if (!dose) continue;
    const code = dose.replace(ROLE_SUFFIX, "") || dose;
    let group = byCode.get(code);
    if (!group) {
      group = { code, name: vaccineName(code), firstDoses: [], repeats: [] };
      byCode.set(code, group);
    }
    if (rule.repeat && rule.repeat !== "none") group.repeats.push(rule);
    else group.firstDoses.push(rule);
  }

  return [...byCode.values()];
}

function readSchedule(ruleDsl: unknown): ScheduleRule[] {
  if (!ruleDsl || typeof ruleDsl !== "object") return [];
  const schedule = (ruleDsl as { schedule?: unknown }).schedule;
  return Array.isArray(schedule) ? (schedule as ScheduleRule[]) : [];
}

/**
 * "2 as a kid · 2 as an adult" — how the doses are triggered, not their codes.
 *
 * Triggers are the CEO-facing distinction that actually matters: a birth_age
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
 * Days as the nearest whole unit a person actually says. 365 is "a year", not
 * "12 months"; 182 is "6 months"; 274 is "9 months". Anything that is not close
 * to a whole unit stays in days rather than being rounded into a lie.
 */
export function humanDays(days: number | undefined): string {
  if (!days || days <= 0) return "—";
  if (days % 365 === 0) return plural(days / 365, "year");
  const months = Math.round(days / 30.44);
  if (months >= 1 && Math.abs(days - months * 30.44) <= 4) return plural(months, "month");
  if (days % 7 === 0) return plural(days / 7, "week");
  return plural(days, "day");
}

function plural(n: number, unit: string): string {
  const rounded = Math.round(n);
  return rounded === 1 ? `${unit}` : `${rounded} ${unit}s`;
}
