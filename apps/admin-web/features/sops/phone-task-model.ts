// TASK WITH ITS OWN PHONE TAB (maintainer decision 2026-10-01, docs/decisions/simple-task-phone-tabs.md).
//
// A simple task -- which pens, when, the one person per park, the questions and photo/video
// captures, an optional verifier -- is authored as an SOP on its module's SOP page. The SOP version's
// `form_dsl.phone_task` holds EVERYTHING; publishing it writes the phone tab and one pen routine per
// park in the publish transaction. This module is the editor's pure half: the editing state, the
// document it emits, the document read back for an edit, and the problems that block a save.
// No React, no server-only import, so the node test exercises exactly what the editor sends.
//
// What is NOT a field, on purpose: the MODULE (it is the SOP page, by code prefix) and the tab
// LABEL (it is the SOP's own name). The backend refuses unknown keys inside `phone_task`, so the
// emitter writes only the keys below and omits the optional ones a cadence does not use.

import {
  CADENCE_KINDS,
  SCOPE_KINDS,
  WORK_KINDS,
  blankEvidenceDraft,
  decodeEvidence,
  evidenceBodyFromDraft,
  evidenceDraftFrom,
  penKey,
  type CadenceDraft,
  type CadenceKind,
  type EvidenceDraft,
  type PenRoutineEvidenceBody,
  type ScopeKind,
} from "../pen-routines/routine-form-model.ts";

export const PHONE_TASK_SECTION = "phone_task";
/** The bottom-bar label limit; the backend enforces the same number (domain.ValidateTab). */
export const PHONE_TASK_NAME_MAX = 24;
export const PHONE_TASK_FILTERS = ["status", "date", "pen"] as const;
export type PhoneTaskFilter = (typeof PHONE_TASK_FILTERS)[number];
/** The form_dsl schema version every SOP version carries (the backend requires one). */
export const PHONE_TASK_SCHEMA_VERSION = "goatos.sop-form.v1";

export type PhoneTaskPenRef = { shed_id: string; partition_label: string };

/** One park's half of the task: whether it runs there, who does it, and (for chosen pens) which pens. */
export type PhoneTaskParkDraft = {
  parkId: string;
  included: boolean;
  assigneeUserId: string;
  /** penKey(shed_id|partition_label) of each ticked pen. */
  pens: string[];
};

export type PhoneTaskDraft = CadenceDraft &
  EvidenceDraft & {
    name: string;
    icon: string;
    filters: PhoneTaskFilter[];
    instruction: string;
    scopeKind: ScopeKind;
    occupiedOnly: boolean;
    reviewKind: "verifier" | "none";
    parks: PhoneTaskParkDraft[];
  };

/** The document under form_dsl.phone_task, exactly the backend's PhoneTaskDoc. */
export type PhoneTaskDoc = {
  tab: { icon: string; filters: PhoneTaskFilter[] };
  instruction: string;
  scope_kind: ScopeKind;
  occupied_only?: boolean;
  cadence_kind: CadenceKind;
  weekdays?: number[];
  month_days?: number[];
  after_work_kinds?: string[];
  interval_days?: number;
  start_date?: string;
  due_offset_days?: number;
  notify_time?: string;
  review_kind: "verifier" | "none";
  evidence: PenRoutineEvidenceBody;
  parks: { park_id: string; assignee_user_id: string; pens: PhoneTaskPenRef[] }[];
};

function asObject(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : null;
}

function oneOf<T extends string>(value: unknown, allowed: readonly T[], fallback: T): T {
  return typeof value === "string" && (allowed as readonly string[]).includes(value) ? (value as T) : fallback;
}

function ints(value: unknown): number[] {
  return Array.isArray(value) ? value.filter((item): item is number => typeof item === "number" && Number.isFinite(item)) : [];
}

function strings(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string" && item !== "") : [];
}

/** A new task: every pen with animals, daily, no review, nothing to record yet, every park offered but none ticked. */
export function blankPhoneTask(parkIds: string[], defaults?: { start_date?: string; notify_time?: string; interval_days?: number }): PhoneTaskDraft {
  return {
    name: "",
    icon: "",
    filters: ["status"],
    instruction: "",
    scopeKind: "all_pens",
    occupiedOnly: true,
    cadenceKind: "daily",
    weekdays: [],
    monthDays: [],
    afterWorkKinds: [],
    intervalDays: defaults?.interval_days !== undefined ? String(defaults.interval_days) : "",
    startDate: defaults?.start_date ?? "",
    dueOffsetDays: "",
    notifyTime: defaults?.notify_time ?? "",
    reviewKind: "none",
    ...blankEvidenceDraft(),
    parks: parkIds.map((parkId) => ({ parkId, included: false, assigneeUserId: "", pens: [] })),
  };
}

/** Whether a form_dsl carries a phone task (the editor-selection test on the SOP page). */
export function hasPhoneTask(formDsl: unknown): boolean {
  return asObject(asObject(formDsl)?.[PHONE_TASK_SECTION]) !== null;
}

/**
 * The editing state of a stored version. Parks the document names come first in its own order;
 * every other park the editor offers is appended unticked, so a park can be added on edit.
 * Returns null when the version carries no phone task.
 */
export function parsePhoneTask(sopName: string, formDsl: unknown, parkIds: string[] = []): PhoneTaskDraft | null {
  const doc = asObject(asObject(formDsl)?.[PHONE_TASK_SECTION]);
  if (!doc) return null;
  const tab = asObject(doc.tab) ?? {};
  const cadenceKind = oneOf(doc.cadence_kind, CADENCE_KINDS, "daily");
  const scopeKind = oneOf(doc.scope_kind, SCOPE_KINDS, "all_pens");
  const parks: PhoneTaskParkDraft[] = [];
  const seen = new Set<string>();
  for (const raw of Array.isArray(doc.parks) ? doc.parks : []) {
    const park = asObject(raw);
    const parkId = typeof park?.park_id === "string" ? park.park_id : "";
    if (!park || !parkId || seen.has(parkId)) continue;
    seen.add(parkId);
    const pens = (Array.isArray(park.pens) ? park.pens : [])
      .map((pen) => asObject(pen))
      .filter((pen): pen is Record<string, unknown> => Boolean(pen && typeof pen.shed_id === "string" && pen.shed_id))
      .map((pen) => penKey({ shed_id: String(pen.shed_id), partition_label: typeof pen.partition_label === "string" ? pen.partition_label : "" }));
    parks.push({ parkId, included: true, assigneeUserId: typeof park.assignee_user_id === "string" ? park.assignee_user_id : "", pens });
  }
  for (const parkId of parkIds) if (!seen.has(parkId)) parks.push({ parkId, included: false, assigneeUserId: "", pens: [] });
  const evidence = evidenceDraftFrom(asObject(doc.evidence) as Partial<PenRoutineEvidenceBody> | null);
  return {
    name: sopName,
    icon: typeof tab.icon === "string" ? tab.icon : "",
    filters: strings(tab.filters).filter((key): key is PhoneTaskFilter => (PHONE_TASK_FILTERS as readonly string[]).includes(key)),
    instruction: typeof doc.instruction === "string" ? doc.instruction : "",
    scopeKind,
    // Absent means the backend default: skip empty pens.
    occupiedOnly: typeof doc.occupied_only === "boolean" ? doc.occupied_only : true,
    cadenceKind,
    weekdays: ints(doc.weekdays),
    monthDays: ints(doc.month_days),
    afterWorkKinds: strings(doc.after_work_kinds),
    intervalDays: typeof doc.interval_days === "number" ? String(doc.interval_days) : "",
    startDate: typeof doc.start_date === "string" ? doc.start_date : "",
    dueOffsetDays: typeof doc.due_offset_days === "number" ? String(doc.due_offset_days) : "",
    notifyTime: typeof doc.notify_time === "string" ? doc.notify_time : "",
    reviewKind: doc.review_kind === "verifier" ? "verifier" : "none",
    ...evidence,
    parks,
  };
}

/** Splits a pen key back into the wire pair. */
function penRef(key: string): PhoneTaskPenRef {
  const at = key.indexOf("|");
  return at < 0 ? { shed_id: key, partition_label: "" } : { shed_id: key.slice(0, at), partition_label: key.slice(at + 1) };
}

/**
 * The `phone_task` document of an editing state. Optional keys travel only for the cadence or scope
 * that reads them; a blank number is omitted so the backend refuses it in its own words rather than
 * the editor inventing a value.
 */
export function emitPhoneTask(draft: PhoneTaskDraft): PhoneTaskDoc {
  const doc: PhoneTaskDoc = {
    tab: { icon: draft.icon, filters: PHONE_TASK_FILTERS.filter((key) => draft.filters.includes(key)) },
    instruction: draft.instruction.trim(),
    scope_kind: draft.scopeKind,
    cadence_kind: draft.cadenceKind,
    review_kind: draft.reviewKind,
    evidence: decodeEvidence(evidenceBodyFromDraft(draft)),
    parks: draft.parks
      .filter((park) => park.included)
      .map((park) => ({
        park_id: park.parkId,
        assignee_user_id: park.assigneeUserId,
        // Only chosen pens name pens; every pen and the whole park carry none.
        pens: draft.scopeKind === "selected_pens" ? park.pens.map(penRef) : [],
      })),
  };
  if (draft.scopeKind === "all_pens") doc.occupied_only = draft.occupiedOnly;
  if (draft.cadenceKind === "weekly") doc.weekdays = [...draft.weekdays].sort((a, b) => a - b);
  if (draft.cadenceKind === "monthly") doc.month_days = [...draft.monthDays].sort((a, b) => a - b);
  if (draft.cadenceKind === "after_work") doc.after_work_kinds = draft.afterWorkKinds.filter((kind) => (WORK_KINDS as readonly string[]).includes(kind));
  if (draft.cadenceKind === "every_n_days") {
    const n = Number.parseInt(draft.intervalDays, 10);
    if (Number.isFinite(n)) doc.interval_days = n;
  }
  if (/^\d{4}-\d{2}-\d{2}$/.test(draft.startDate)) doc.start_date = draft.startDate;
  if (draft.dueOffsetDays.trim() !== "") {
    const n = Number.parseInt(draft.dueOffsetDays, 10);
    if (Number.isFinite(n)) doc.due_offset_days = n;
  }
  const notify = draft.notifyTime.slice(0, 5);
  if (/^\d{2}:\d{2}$/.test(notify)) doc.notify_time = notify;
  return doc;
}

/**
 * The whole form_dsl a save writes: the base version's other keys are kept (an edit of a
 * published version), `phone_task` replaced, and the capture form stays empty -- the task's
 * questions live in its evidence, not in form fields.
 */
export function phoneTaskFormDsl(doc: PhoneTaskDoc, base?: Record<string, unknown> | null): Record<string, unknown> {
  return {
    ...(base ?? {}),
    schema_version: typeof base?.schema_version === "string" && base.schema_version ? base.schema_version : PHONE_TASK_SCHEMA_VERSION,
    fields: [],
    [PHONE_TASK_SECTION]: doc,
  };
}

export type PhoneTaskProblem = { key: string; park?: string };

/**
 * What blocks a save before it reaches the backend: the few things the editor can know (a name, an
 * icon, at least one park, who does it there, a pen when pens are chosen). Everything else -- the
 * evidence limits, a person who does not work at that park, the cadence rules -- is the backend's
 * to refuse, in its own words. `parkName` names a park in the problem sentence.
 */
export function phoneTaskProblems(draft: PhoneTaskDraft, parkName: (parkId: string) => string = (id) => id): PhoneTaskProblem[] {
  const problems: PhoneTaskProblem[] = [];
  const name = draft.name.trim();
  if (!name) problems.push({ key: "ptask.problem.name" });
  else if ([...name].length > PHONE_TASK_NAME_MAX) problems.push({ key: "ptask.problem.name_long" });
  if (!draft.icon) problems.push({ key: "ptask.problem.icon" });
  const included = draft.parks.filter((park) => park.included);
  if (!included.length) problems.push({ key: "ptask.problem.park" });
  for (const park of included) {
    if (!park.assigneeUserId) problems.push({ key: "ptask.problem.assignee", park: parkName(park.parkId) });
    if (draft.scopeKind === "selected_pens" && !park.pens.length) problems.push({ key: "ptask.problem.pens", park: parkName(park.parkId) });
  }
  return problems;
}
