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
 * "2 from date of birth · 1 on a drive" — how the doses are TRIGGERED, not
 * their codes.
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

/**
 * The vaccine catalog, and what each version changed.
 *
 * There is no vaccine catalog table with rows in it -- `vaccines` is empty in
 * staging -- but the catalog is not therefore unknowable: it is the set of
 * vaccines the farm has ever had in a plan, which is exactly the union across
 * every version's document. That makes "6 of 7" a real number rather than an
 * invented one, and it is how a vaccine that was dropped (Blue Tongue) can be
 * shown as "not in this plan" instead of vanishing from the screen with no
 * trace that it was ever there.
 */
export type PlanCatalogEntry = VaccineGroup & { inPlan: boolean };

export function buildCatalog(liveGroups: VaccineGroup[], allGroups: VaccineGroup[][]): PlanCatalogEntry[] {
  const inPlan = new Map(liveGroups.map((g) => [g.code, g]));
  const seen = new Map<string, VaccineGroup>();
  for (const groups of allGroups) {
    for (const group of groups) if (!seen.has(group.code)) seen.set(group.code, group);
  }
  // Vaccines currently in the plan come first, in the plan's own order; the
  // dropped ones follow. A reader scans the live plan far more often than the
  // history, so the live rows must not be interleaved with retired ones.
  const entries: PlanCatalogEntry[] = liveGroups.map((g) => ({ ...g, inPlan: true }));
  for (const [code, group] of seen) {
    if (!inPlan.has(code)) entries.push({ ...group, inPlan: false });
  }
  return entries;
}

/**
 * One line saying what a version changed, against the version before it.
 *
 * Derived by comparing the two documents rather than read from a stored note:
 * there is no change-note column, and a note a human typed could disagree with
 * what the version actually says. A derived line cannot.
 */
export function describeChange(current: VaccineGroup[], previous: VaccineGroup[] | null): string {
  if (!previous) return `First plan, with ${countLabel(current.length, "vaccine")}.`;
  const before = new Set(previous.map((g) => g.code));
  const after = new Set(current.map((g) => g.code));
  const added = current.filter((g) => !before.has(g.code)).map((g) => g.name);
  const removed = previous.filter((g) => !after.has(g.code)).map((g) => g.name);

  const parts: string[] = [];
  if (added.length > 0) parts.push(`Added ${added.join(", ")}`);
  if (removed.length > 0) parts.push(`Dropped ${removed.join(", ")}`);
  if (parts.length > 0) return `${parts.join(" · ")}.`;

  // Same vaccines: the change was to timing, which is the common case and must
  // not read as "nothing changed".
  return scheduleDiffers(current, previous) ? "Timing changed." : "No change to the vaccines.";
}

function scheduleDiffers(a: VaccineGroup[], b: VaccineGroup[]): boolean {
  return fingerprint(a) !== fingerprint(b);
}

function fingerprint(groups: VaccineGroup[]): string {
  return groups
    .map((g) =>
      [...g.firstDoses, ...g.repeats]
        .map((r) => `${r.dose_code}@${r.offset_days ?? ""}/${r.repeat ?? ""}`)
        .sort()
        .join(","),
    )
    .sort()
    .join("|");
}

function countLabel(n: number, unit: string): string {
  return `${n} ${unit}${n === 1 ? "" : "s"}`;
}
