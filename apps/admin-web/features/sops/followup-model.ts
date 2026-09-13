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
};

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
export const ENGINE_BOUND_TASK_TYPES = new Set(["weigh", "tag", "record_pen", "feed_colostrum", "death_evidence", "return_to_pen"]);

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
  };
}

// slugKey derives a stable step key from a title for NEW steps (an existing key is never rewritten).
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
      return {
        kind: "series",
        times: row.times.split(",").map((t) => t.trim()).filter(Boolean),
        days: row.days,
        pre_notify_minutes: row.preNotifyMinutes,
        key_pattern: row.keyPattern.trim() || `${row.key}_{day}_{hhmm}`,
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
      if (s.scheduleKind === "series") {
        const times = s.times.split(",").map((x) => x.trim()).filter(Boolean);
        if (times.length === 0 || times.some((x) => !/^\d{2}:\d{2}$/.test(x))) problems.push(`${at}: series times must be HH:MM, comma separated`);
        if (s.days < 1) problems.push(`${at}: a series needs at least one day`);
      }
      if (s.scheduleKind === "after_step") {
        const earlier = t.steps.slice(0, i).map((x) => x.key);
        if (!earlier.includes(s.afterStep)) problems.push(`${at}: "after step" must name an earlier step`);
        if (s.offsetMinutes <= 0) problems.push(`${at}: minutes after must be positive`);
      }
      for (const req of s.requires) {
        if (!t.steps.slice(0, i).some((x) => x.key === req)) problems.push(`${at}: "${req}" must be an earlier step`);
      }
    });
  }
  return problems;
}
