// SOP-DRIVEN HERD OPERATIONS (maintainer decision 2026-09-13,
// docs/decisions/sop-driven-herd-operations.md).
//
// Pure model of `form_dsl.follow_up`: the operator steps the phone runs after a birth / death /
// shifting / reconcile event, authored on /counts/sops. This file parses the backend document into
// editor rows and emits it back byte-faithfully (keys, task types, proof counts, schedules, gates),
// so a round-trip through the editor with no edits publishes the SAME document. The backend
// (tasks/domain.ValidateFollowUp) is the authority on what is valid; this file only shapes.

export const FOLLOW_UP_SCHEMA_VERSION = "goatos.sop-followup.v1";

export type ScheduleKind = "immediately" | "after_event" | "at_fixed_time" | "series" | "after_step";
// A repeating series runs either at fixed wall-clock times (event-day rounds already past are
// skipped) or from the event instant (every N minutes, M rounds -- nothing skipped).
export type SeriesBasis = "fixed_times" | "next_sessions" | "from_event";

export type FollowUpStepRow = {
  id: string;
  key: string;
  taskType: string;
  title: string;
  titlePattern: string;
  detail: string;
  section: string;
  answer: string;
  options: string[];
  proofVideos: number;
  proofPhotos: number;
  scheduleKind: ScheduleKind;
  basis: SeriesBasis;
  intervalMinutes: number;
  count: number;
  offsetMinutes: number;
  dayOffset: number;
  time: string;
  times: string;
  days: number;
  preNotifyMinutes: number;
  keyPattern: string;
  ordinalStart: number;
  afterStep: string;
  hardTimeGate: boolean;
  waitForAll: boolean;
  requires: string[];
  when: string;
  // Answer-driven branch (SOP studio phase 2): the step runs only when `whenStep`'s answer
  // satisfies `whenOp` against `whenValues`. Blank whenStep = always runs.
  whenStep: string;
  whenOp: AnswerOp;
  whenValues: string[];
  // Who does the step (SALES SOP, 2026-09-19): a designation code from the catalog served as
  // the `sop_step_owners` option group. Blank = anyone who can open the workflow.
  owner: string;
  // KID STAGE SHIFT TASKS (2026-09-30): the growth stage a litter shift step moves the kids to
  // (`K1`, `K2`). Only a step whose task type's engine hook is shift_kids_stage carries it.
  targetStage: string;
};

/** Answer comparisons the engine evaluates (tasks/domain.AnswerCondition). */
export type AnswerOp = "eq" | "ne" | "in" | "not_in" | "gt" | "gte" | "lt" | "lte";
export const ANSWER_OPS: AnswerOp[] = ["eq", "ne", "in", "not_in", "gt", "gte", "lt", "lte"];
export const NUMERIC_ANSWER_OPS: AnswerOp[] = ["gt", "gte", "lt", "lte"];

export type FollowUpTrackRows = {
  key: string;
  module: string;
  label: string;
  subject: string;
  steps: FollowUpStepRow[];
};

export type FollowUpRows = { tracks: FollowUpTrackRows[] };

// Steps whose KEY the engine matches a behaviour on (tag promotion, pen fallback, colostrum lens,
// numeric kg, death evidence, pen return, the dependency-timed ORS round). The editor keeps their
// key and type fixed; title, instruction, proof and schedule stay editable.
export const ENGINE_BOUND_TASK_TYPES = new Set(["weigh", "tag", "record_pen", "feed_colostrum", "death_evidence", "return_to_pen", "sale_tag_animals"]);

function str(v: unknown, fallback = ""): string {
  return typeof v === "string" ? v : fallback;
}
function int(v: unknown, fallback = 0): number {
  return typeof v === "number" && Number.isFinite(v) ? Math.trunc(v) : fallback;
}
function bool(v: unknown): boolean {
  return v === true;
}
function strList(v: unknown): string[] {
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
}
function obj(v: unknown): Record<string, unknown> | null {
  return v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : null;
}

let seq = 0;
export function newRowId(prefix = "fu"): string {
  seq += 1;
  return `${prefix}-${Date.now().toString(36)}-${seq}`;
}

export function blankStep(taskType = "record_yes_no"): FollowUpStepRow {
  return {
    id: newRowId(),
    key: "",
    taskType,
    title: "",
    titlePattern: "",
    detail: "",
    section: "main",
    answer: "",
    options: [],
    proofVideos: 0,
    proofPhotos: 0,
    scheduleKind: "immediately",
    basis: "fixed_times",
    intervalMinutes: 240,
    count: 10,
    offsetMinutes: 0,
    dayOffset: 0,
    time: "",
    times: "",
    days: 1,
    preNotifyMinutes: 0,
    keyPattern: "",
    ordinalStart: 1,
    afterStep: "",
    hardTimeGate: false,
    waitForAll: false,
    requires: [],
    when: "",
    whenStep: "",
    whenOp: "eq",
    whenValues: [],
    owner: "",
    targetStage: "",
  };
}

// slugKey derives a stable step key from a title for NEW steps (an existing key is never rewritten).
/**
 * followStepKey applies a patch to one step row. When the patch MOVES that step's key (an unsaved key
 * following its title), every step that named the old key in `requires` or as its after-step follows.
 */
export function followStepKey<T extends { id: string; key: string; requires: string[]; afterStep: string }>(steps: T[], id: string, patch: Partial<T>): T[] {
  const current = steps.find((s) => s.id === id);
  const moved = current && patch.key !== undefined && patch.key !== current.key && current.key ? current.key : "";
  const to = patch.key as string;
  return steps.map((s) => {
    if (s.id === id) return { ...s, ...patch };
    if (!moved || (s.afterStep !== moved && !s.requires.includes(moved))) return s;
    return { ...s, afterStep: s.afterStep === moved ? to : s.afterStep, requires: s.requires.map((r) => (r === moved ? to : r)) };
  });
}

export function slugKey(title: string, taken: Set<string>): string {
  const base = title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "")
    .slice(0, 48) || "step";
  let key = base;
  let n = 2;
  while (taken.has(key)) {
    key = `${base}_${n}`;
    n += 1;
  }
  return key;
}

/** keyForTitle: a saved step key never moves; an unsaved one follows the whole title (see weighing-model). */
export function keyForTitle(title: string, currentKey: string, savedKeys: Set<string>, siblings: Set<string>): string {
  if (currentKey && savedKeys.has(currentKey)) return currentKey;
  const taken = new Set(siblings);
  taken.delete(currentKey);
  return slugKey(title, taken);
}

export function parseFollowUp(formDsl: unknown): FollowUpRows | null {
  const dsl = obj(formDsl);
  const fu = dsl ? obj(dsl["follow_up"]) : null;
  if (!fu) return null;
  const tracks = Array.isArray(fu["tracks"]) ? fu["tracks"] : [];
  return {
    tracks: tracks.flatMap((raw) => {
      const t = obj(raw);
      if (!t) return [];
      const steps = Array.isArray(t["steps"]) ? t["steps"] : [];
      return [
        {
          key: str(t["key"]),
          module: str(t["module"]),
          label: str(t["label"]),
          subject: str(t["subject"]),
          steps: steps.flatMap((s) => {
            const step = obj(s);
            if (!step) return [];
            const proof = obj(step["proof"]) ?? {};
            const sched = obj(step["schedule"]) ?? {};
            const whenAnswer = obj(step["when_answer"]) ?? {};
            const kind = (str(sched["kind"], "immediately") || "immediately") as ScheduleKind;
            return [
              {
                id: newRowId(),
                key: str(step["key"]),
                taskType: str(step["task_type"]),
                title: str(step["title"]),
                titlePattern: str(step["title_pattern"]),
                detail: str(step["detail"]),
                section: str(step["section"], "main") || "main",
                answer: str(step["answer"]),
                options: strList(step["options"]),
                proofVideos: int(proof["video"]),
                proofPhotos: int(proof["photo"]),
                scheduleKind: kind,
                basis: (["from_event", "next_sessions"].includes(str(sched["basis"])) ? str(sched["basis"]) : "fixed_times") as SeriesBasis,
                intervalMinutes: int(sched["interval_minutes"], 240),
                count: int(sched["count"], 10),
                offsetMinutes: int(sched["offset_minutes"]),
                dayOffset: int(sched["day_offset"]),
                time: str(sched["time"]),
                times: strList(sched["times"]).join(", "),
                days: int(sched["days"], 1),
                preNotifyMinutes: int(sched["pre_notify_minutes"]),
                keyPattern: str(sched["key_pattern"]),
                ordinalStart: int(sched["ordinal_start"], 1),
                afterStep: str(sched["step"]),
                hardTimeGate: bool(step["hard_time_gate"]),
                waitForAll: bool(step["wait_for_all"]),
                requires: strList(step["requires"]),
                when: str(step["when"]),
                whenStep: str(whenAnswer["step"]),
                whenOp: (ANSWER_OPS.includes(str(whenAnswer["op"]) as AnswerOp) ? str(whenAnswer["op"]) : "eq") as AnswerOp,
                whenValues: strList(whenAnswer["value"]),
                owner: str(step["owner"]),
                targetStage: str(step["target_stage"]),
              } satisfies FollowUpStepRow,
            ];
          }),
        },
      ];
    }),
  };
}

function emitSchedule(row: FollowUpStepRow): Record<string, unknown> {
  switch (row.scheduleKind) {
    case "after_event":
      return { kind: "after_event", offset_minutes: row.offsetMinutes };
    case "at_fixed_time":
      return { kind: "at_fixed_time", day_offset: row.dayOffset, time: row.time.trim() };
    case "series":
      if (row.basis === "from_event") {
        return {
          kind: "series",
          basis: "from_event",
          interval_minutes: row.intervalMinutes,
          count: row.count,
          // Each round needs its own key; the fixed-times pattern ({day}_{hhmm}) cannot give one
          // to rounds counted from the event, so derive a {n} pattern unless the author has one.
          key_pattern: row.keyPattern.includes("{n}") ? row.keyPattern.trim() : `${row.key}_{n}`,
          ordinal_start: row.ordinalStart,
          // Carried so switching the basis back later keeps the farm's session times and the
          // same-day skip margin instead of resetting them (the engine ignores them here).
          ...(row.times.trim() ? { times: row.times.split(",").map((t) => t.trim()).filter(Boolean) } : {}),
          ...(row.days > 1 ? { days: row.days } : {}),
          ...(row.preNotifyMinutes > 0 ? { pre_notify_minutes: row.preNotifyMinutes } : {}),
        };
      }
      if (row.basis === "next_sessions") {
        return {
          kind: "series",
          basis: "next_sessions",
          times: row.times.split(",").map((t) => t.trim()).filter(Boolean),
          count: row.count,
          pre_notify_minutes: row.preNotifyMinutes,
          key_pattern: /\{day\}|\{hhmm\}/.test(row.keyPattern) ? row.keyPattern.trim() : `${row.key}_{day}_{hhmm}`,
          ordinal_start: row.ordinalStart,
        };
      }
      return {
        kind: "series",
        times: row.times.split(",").map((t) => t.trim()).filter(Boolean),
        days: row.days,
        pre_notify_minutes: row.preNotifyMinutes,
        key_pattern: /\{day\}|\{hhmm\}/.test(row.keyPattern) ? row.keyPattern.trim() : `${row.key}_{day}_{hhmm}`,
        ordinal_start: row.ordinalStart,
      };
    case "after_step":
      return { kind: "after_step", step: row.afterStep, offset_minutes: row.offsetMinutes };
    default:
      return { kind: "immediately" };
  }
}

export function emitFollowUp(rows: FollowUpRows): Record<string, unknown> {
  return {
    schema_version: FOLLOW_UP_SCHEMA_VERSION,
    tracks: rows.tracks.map((t) => ({
      key: t.key,
      module: t.module,
      label: t.label,
      subject: t.subject,
      steps: t.steps.map((row) => {
        const out: Record<string, unknown> = {
          key: row.key,
          task_type: row.taskType,
          title: row.title,
          detail: row.detail,
          proof: {
            ...(row.proofVideos > 0 ? { video: row.proofVideos } : {}),
            ...(row.proofPhotos > 0 ? { photo: row.proofPhotos } : {}),
          },
          schedule: emitSchedule(row),
        };
        if (row.titlePattern.trim()) out.title_pattern = row.titlePattern.trim();
        if (row.section && row.section !== "main") out.section = row.section;
        if (row.answer) out.answer = row.answer;
        if (row.options.length) out.options = row.options.map((o) => o.trim()).filter(Boolean);
        if (row.hardTimeGate) out.hard_time_gate = true;
        if (row.waitForAll) out.wait_for_all = true;
        if (row.requires.length) out.requires = row.requires;
        if (row.when) out.when = row.when;
        if (row.whenStep) out.when_answer = { step: row.whenStep, op: row.whenOp, value: row.whenValues.map((v) => v.trim()).filter(Boolean) };
        if (row.owner.trim()) out.owner = row.owner.trim();
        if (row.targetStage.trim()) out.target_stage = row.targetStage.trim();
        return out;
      }),
    })),
  };
}

// Client-side pre-checks that mirror the backend validator's cheapest rules so the author gets
// the message next to the field. The backend remains the authority (its 400 names the field too).
export function followUpProblems(rows: FollowUpRows, answerKinds: Record<string, string>): string[] {
  const problems: string[] = [];
  for (const t of rows.tracks) {
    const seen = new Set<string>();
    t.steps.forEach((s, i) => {
      const at = `${t.label || t.key} · step ${i + 1}`;
      if (!s.key.trim()) problems.push(`${at}: needs a key`);
      if (seen.has(s.key)) problems.push(`${at}: key "${s.key}" is used twice`);
      seen.add(s.key);
      if (!s.taskType) problems.push(`${at}: pick a step type`);
      if (!s.title.trim() && !s.titlePattern.trim()) problems.push(`${at}: needs a title`);
      const kind = s.answer || answerKinds[s.taskType] || "none";
      if ((kind === "select" || kind === "multiselect") && s.options.filter((o) => o.trim()).length === 0) {
        problems.push(`${at}: a pick-one / pick-many step needs at least one choice`);
      }
      if (s.scheduleKind === "at_fixed_time" && !/^\d{2}:\d{2}$/.test(s.time.trim())) problems.push(`${at}: time must be HH:MM`);
      if (s.scheduleKind === "series" && s.basis === "from_event") {
        if (s.intervalMinutes <= 0) problems.push(`${at}: the gap between rounds must be positive`);
        if (s.count < 1 || s.count > 100) problems.push(`${at}: a series needs 1 to 100 rounds`);
      } else if (s.scheduleKind === "series") {
        const times = s.times.split(",").map((x) => x.trim()).filter(Boolean);
        if (times.length === 0 || times.some((x) => !/^\d{2}:\d{2}$/.test(x))) problems.push(`${at}: series times must be HH:MM, comma separated`);
        if (s.basis === "next_sessions" && (s.count < 1 || s.count > 100)) problems.push(`${at}: a series needs 1 to 100 rounds`);
        if (s.basis !== "next_sessions" && s.days < 1) problems.push(`${at}: a series needs at least one day`);
      }
      if (s.scheduleKind === "after_step") {
        const earlier = t.steps.slice(0, i).map((x) => x.key);
        if (!earlier.includes(s.afterStep)) problems.push(`${at}: "after step" must name an earlier step`);
        if (s.offsetMinutes <= 0) problems.push(`${at}: minutes after must be positive`);
      }
      for (const req of s.requires) {
        if (!t.steps.slice(0, i).some((x) => x.key === req)) problems.push(`${at}: "${req}" must be an earlier step`);
      }
      if (s.whenStep) {
        const question = t.steps.slice(0, i).find((x) => x.key === s.whenStep);
        const kind = question ? stepAnswerKind(question, answerKinds) : "";
        if (!question) problems.push(`${at}: the branch must hang on an earlier question`);
        else if (!kind || kind === "none") problems.push(`${at}: "${question.title || question.key}" records no answer to branch on`);
        else if (NUMERIC_ANSWER_OPS.includes(s.whenOp) && kind !== "number") problems.push(`${at}: a number comparison needs a number question`);
        const values = s.whenValues.map((v) => v.trim()).filter(Boolean);
        if (values.length === 0) problems.push(`${at}: the branch needs a value`);
        if (kind === "yes_no" && values.some((v) => !["yes", "no"].includes(v.toLowerCase()))) problems.push(`${at}: the branch value must be yes or no`);
        if ((kind === "select" || kind === "multiselect") && question && values.some((v) => !question.options.map((o) => o.trim().toLowerCase()).includes(v.toLowerCase()))) {
          problems.push(`${at}: the branch value must be one of the question's choices`);
        }
        if (kind === "number" && values.some((v) => Number.isNaN(Number(v)))) problems.push(`${at}: the branch value must be a number`);
      }
    });
  }
  return problems;
}

/** The answer kind a step records: its own override, else its task type's registry kind. */
export function stepAnswerKind(step: FollowUpStepRow, answerKinds: Record<string, string>): string {
  if (step.answer) return step.answer;
  return answerKindOfTaskType(step.taskType, answerKinds);
}

/** Answer kind by task type key. `answerKinds` maps task type -> answer kind label/key from the registry option group. */
export function answerKindOfTaskType(taskType: string, answerKinds: Record<string, string>): string {
  return answerKinds[taskType] ?? "";
}

// ---------------------------------------------------------------------------------------------
// Read-side description of the authored steps: what the phone will actually schedule for ONE
// animal. Mirrors tasks/domain.expandSeries (sorted times × days, ordinal titles) so the SOP
// drawer and the editor's series preview show the same rounds the engine stamps.
// ---------------------------------------------------------------------------------------------

export type FollowUpCopy = (key: string, vars?: Record<string, string | number>) => string;

export function ordinal(n: number): string {
  const mod100 = n % 100;
  if (mod100 >= 11 && mod100 <= 13) return `${n}th`;
  switch (n % 10) {
    case 1: return `${n}st`;
    case 2: return `${n}nd`;
    case 3: return `${n}rd`;
    default: return `${n}th`;
  }
}

function sortedTimes(csv: string): string[] {
  return csv
    .split(",")
    .map((t) => t.trim())
    .filter((t) => /^\d{2}:\d{2}$/.test(t))
    .sort();
}

// A fixed-times round carries its day/time; a from-event round carries minutes after the event.
export type ExpandedRound = { title: string; dayOffset: number; time: string; afterMinutes?: number };

// expandSeriesRows lists every round a series step produces: day 0 = the event day (rounds
// already past at the event are skipped by the engine), then day 1, 2, … ; ordinals continue
// from `ordinalStart` so "2nd Colostrum" follows the separately authored "1st Colostrum".
// For next_sessions the rounds depend on the event time, so the preview takes an EXAMPLE event
// time (HH:MM) and walks the farm's session slots from the next one onward, like the engine.
export function expandSeriesRows(step: FollowUpStepRow, exampleEventTime = "15:00"): ExpandedRound[] {
  if (step.scheduleKind !== "series") return [];
  const out: ExpandedRound[] = [];
  let n = step.ordinalStart;
  if (step.basis === "next_sessions") {
    const times = sortedTimes(step.times);
    if (times.length === 0) return out;
    const [eh, em] = exampleEventTime.split(":").map(Number);
    const eventMin = (eh || 0) * 60 + (em || 0);
    const count = Math.min(100, Math.max(1, step.count));
    for (let d = 0; out.length < count && d <= count; d++) {
      for (const t of times) {
        if (out.length >= count) break;
        const [h, m] = t.split(":").map(Number);
        if (d === 0 && h * 60 + m - step.preNotifyMinutes <= eventMin) continue;
        const title = step.titlePattern.trim()
          ? step.titlePattern.replace("{ordinal}", ordinal(n)).replace("{time}", t).replace("{day}", String(d))
          : `${ordinal(n)} ${step.title}`.trim();
        out.push({ title, dayOffset: d, time: t });
        n += 1;
      }
    }
    return out;
  }
  if (step.basis === "from_event") {
    for (let k = 1; k <= Math.min(100, Math.max(1, step.count)); k++) {
      const mins = k * Math.max(1, step.intervalMinutes);
      const title = step.titlePattern.trim()
        ? step.titlePattern.replace("{ordinal}", ordinal(n)).replace("{time}", formatAfter(mins))
        : `${ordinal(n)} ${step.title}`.trim();
      out.push({ title, dayOffset: Math.floor(mins / 1440), time: "", afterMinutes: mins });
      n += 1;
    }
    return out;
  }
  const times = sortedTimes(step.times);
  for (let d = 0; d < Math.max(1, step.days); d++) {
    for (const t of times) {
      const title = step.titlePattern.trim()
        ? step.titlePattern.replace("{ordinal}", ordinal(n)).replace("{time}", t).replace("{day}", String(d))
        : `${ordinal(n)} ${step.title}`.trim();
      out.push({ title, dayOffset: d, time: t });
      n += 1;
    }
  }
  return out;
}

export function formatAfter(mins: number): string {
  const h = Math.floor(mins / 60);
  const m = mins % 60;
  if (h === 0) return `${m} min`;
  return m === 0 ? `${h} h` : `${h} h ${m} min`;
}

export function describeDue(step: FollowUpStepRow, copy: FollowUpCopy, stepTitleByKey: Record<string, string> = {}): string {
  switch (step.scheduleKind) {
    case "after_event":
      return copy("followup.due.after_event", { n: step.offsetMinutes });
    case "at_fixed_time":
      return copy("followup.due.fixed_time", { d: step.dayOffset, t: step.time.trim() });
    case "series":
      if (step.basis === "from_event") {
        return copy("followup.due.series_from_event", { n: step.count, every: formatAfter(step.intervalMinutes) });
      }
      if (step.basis === "next_sessions") {
        return copy("followup.due.series_next_sessions", { n: step.count, times: sortedTimes(step.times).join(", ") });
      }
      return copy("followup.due.series", { n: expandSeriesRows(step).length, times: sortedTimes(step.times).join(", "), days: step.days });
    case "after_step":
      return copy("followup.due.after_step", { n: step.offsetMinutes, step: stepTitleByKey[step.afterStep] || step.afterStep });
    default:
      return copy("followup.due.right_away");
  }
}

export function describeProof(step: FollowUpStepRow, copy: FollowUpCopy): string {
  const parts: string[] = [];
  if (step.proofVideos > 0) parts.push(copy("followup.proof.videos", { n: step.proofVideos }));
  if (step.proofPhotos > 0) parts.push(copy("followup.proof.photos", { n: step.proofPhotos }));
  return parts.join(" · ");
}
