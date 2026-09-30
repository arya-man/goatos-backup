// Pure helpers for People / HRMS > Timetable's "Set time" form (maintainer request 2026-09-30).
// The backend (PUT /admin/workforce/timetable/parks/{park}/shifts/{shift}) is the authority on
// every rule and refuses a bad value with a farm-worded message the form shows verbatim; these
// helpers only turn stored minutes into the form's hour/minute selects and back. The console has
// no native time input (admin-web interaction rule 5): a time is an hour select plus a minute
// select, the Tasks deadline form's shape. Kept free of runtime imports so `node --test` runs it.

/** Minutes after IST midnight. A shift may END at 1440: midnight, e.g. the Second shift 3 pm-12 am. */
export const MINUTES_PER_DAY = 1440;

/** Every hour of the day, as the value the selects post. */
export const HOURS: readonly number[] = Array.from({ length: 24 }, (_, h) => h);

/** Five-minute steps; a stored time on another minute is added so it round-trips. */
export const MINUTE_STEPS: readonly number[] = Array.from({ length: 12 }, (_, i) => i * 5);

/** "6 am", "12 pm" (noon), "12 am" (midnight): how the farm names an hour. */
export function hourLabel(hour: number): string {
  const h = ((hour % 24) + 24) % 24;
  const h12 = h % 12 === 0 ? 12 : h % 12;
  return `${h12} ${h < 12 ? "am" : "pm"}`;
}

export function minuteLabel(minute: number): string {
  return `:${String(minute).padStart(2, "0")}`;
}

export interface TimeDraft {
  hour: string;
  minute: string;
}

export interface TimingDraft {
  start: TimeDraft;
  /** An empty end hour means "No end time yet". */
  end: TimeDraft;
}

const EMPTY: TimeDraft = { hour: "", minute: "" };

function toDraft(minutes: number | null | undefined): TimeDraft {
  if (minutes === null || minutes === undefined) return EMPTY;
  const m = ((minutes % MINUTES_PER_DAY) + MINUTES_PER_DAY) % MINUTES_PER_DAY;
  return { hour: String(Math.floor(m / 60)), minute: String(m % 60) };
}

/** The form's starting values: a stored timing opens as itself, an unset one opens empty. */
export function draftFromTiming(start: number | null | undefined, end: number | null | undefined): TimingDraft {
  return { start: toDraft(start), end: toDraft(end) };
}

/** Minute options for a select, keeping an off-step stored minute so it round-trips. */
export function minuteOptions(current: string): number[] {
  const n = Number.parseInt(current, 10);
  if (!Number.isFinite(n) || MINUTE_STEPS.includes(n)) return [...MINUTE_STEPS];
  return [...MINUTE_STEPS, n].sort((a, b) => a - b);
}

function toMinutes(draft: TimeDraft): number | null {
  if (draft.hour === "") return null;
  const h = Number.parseInt(draft.hour, 10);
  const m = draft.minute === "" ? 0 : Number.parseInt(draft.minute, 10);
  if (!Number.isFinite(h) || !Number.isFinite(m)) return null;
  return h * 60 + m;
}

export type TimingRequest = { start_minute: number | null; end_minute: number | null };

/**
 * The request body for the form. A start is required; an end of 12:00 am is MIDNIGHT (1440), the
 * end of the day, never the start of it -- a shift can end at midnight but cannot end before it
 * begins at 0. An empty end hour leaves the end unset.
 */
export function timingRequestFromDraft(draft: TimingDraft): { ok: true; body: TimingRequest } | { ok: false; reason: "start_required" } {
  const start = toMinutes(draft.start);
  if (start === null) return { ok: false, reason: "start_required" };
  let end = toMinutes(draft.end);
  if (end === 0) end = MINUTES_PER_DAY;
  return { ok: true, body: { start_minute: start, end_minute: end } };
}
