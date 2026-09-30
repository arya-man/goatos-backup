// Pure model for the HRMS SOP editor (maintainer instruction 2026-09-30: "every violation type,
// everything is SOP driven; in future every list should be changeable"). The backend
// (internal/hrmssop) owns every rule and refuses a bad version at save with a farm-worded message
// the editor shows verbatim; this file only turns the stored `violations` section into editable
// rows and back. Kept free of runtime imports so `node --test` exercises it directly.

export type HrmsQuestionKind = "text" | "yes_no";

export interface HrmsTypeRow {
  /** Stable identity. Set once from the first name and kept on every rename. */
  key: string;
  title: string;
  defaultFine: string;
  active: boolean;
  /** True for a type already in a published version: its key is locked. */
  stored: boolean;
}

export interface HrmsQuestionRow {
  id: string;
  kind: HrmsQuestionKind;
  title: string;
  required: boolean;
  stored: boolean;
}

export interface HrmsEnquiryRow {
  trigger: string;
  title: string;
  dueHours: string;
  questions: HrmsQuestionRow[];
}

export interface HrmsRows {
  types: HrmsTypeRow[];
  enquiries: HrmsEnquiryRow[];
}

export const HRMS_SCHEMA_VERSION = "goatos.sop-hrms-violations.v1";
/** The farm events the backend can open an enquiry for (hrmssop/domain.KnownTriggers). */
export const HRMS_TRIGGERS = ["animal_death"] as const;

function obj(v: unknown): Record<string, unknown> | null {
  return v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : null;
}
function arr(v: unknown): unknown[] {
  return Array.isArray(v) ? v : [];
}
function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

/** The editable rows of a version's `violations` section, or null when the version has none. */
export function parseHrms(formDsl: unknown): HrmsRows | null {
  const section = obj(obj(formDsl)?.["violations"]);
  if (!section) return null;
  const types = arr(section["violation_types"]).map((raw): HrmsTypeRow => {
    const t = obj(raw) ?? {};
    return {
      key: str(t["key"]),
      title: str(t["title"]),
      defaultFine: typeof t["default_fine"] === "number" ? String(t["default_fine"]) : "0",
      active: t["active"] !== false,
      stored: true,
    };
  });
  const enquiries = arr(section["enquiries"]).map((raw): HrmsEnquiryRow => {
    const e = obj(raw) ?? {};
    return {
      trigger: str(e["trigger"]),
      title: str(e["title"]),
      dueHours: typeof e["due_hours"] === "number" ? String(e["due_hours"]) : "48",
      questions: arr(e["questions"]).map((q): HrmsQuestionRow => {
        const o = obj(q) ?? {};
        return { id: str(o["id"]), kind: o["kind"] === "yes_no" ? "yes_no" : "text", title: str(o["title"]), required: o["required"] === true, stored: true };
      }),
    };
  });
  return { types, enquiries };
}

/** A lower-case key from a name, unique among `taken` (a_b, a_b_2, ...). */
export function keyFromTitle(title: string, taken: ReadonlySet<string>, fallback: string): string {
  let base = title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "")
    .slice(0, 36);
  if (!/^[a-z]/.test(base)) base = base ? `${fallback}_${base}`.slice(0, 36) : fallback;
  let key = base;
  for (let n = 2; taken.has(key); n++) key = `${base}_${n}`;
  return key;
}

function wholeNumber(raw: string): number {
  const n = Number.parseInt(raw.trim(), 10);
  return Number.isFinite(n) ? n : Number.NaN;
}

/**
 * The `violations` section a set of rows saves as. A new row gets its key from its name here, so
 * a type renamed after it is stored keeps the key its past records point at. Blank fines and
 * deadlines are sent as NaN->null so the backend refuses them with its own message rather than
 * the editor inventing a number.
 */
export function emitHrms(rows: HrmsRows): Record<string, unknown> {
  const typeKeys = new Set(rows.types.filter((t) => t.stored && t.key).map((t) => t.key));
  const types = rows.types.map((t) => {
    const key = t.stored && t.key ? t.key : keyFromTitle(t.title, typeKeys, "violation");
    typeKeys.add(key);
    const fine = wholeNumber(t.defaultFine);
    return { key, title: t.title.trim(), default_fine: Number.isNaN(fine) ? null : fine, active: t.active };
  });
  const enquiries = rows.enquiries.map((e) => {
    const ids = new Set(e.questions.filter((q) => q.stored && q.id).map((q) => q.id));
    const due = wholeNumber(e.dueHours);
    return {
      trigger: e.trigger,
      title: e.title.trim(),
      due_hours: Number.isNaN(due) ? null : due,
      questions: e.questions.map((q) => {
        const id = q.stored && q.id ? q.id : keyFromTitle(q.title, ids, "question");
        ids.add(id);
        return { id, kind: q.kind, title: q.title.trim(), required: q.required };
      }),
    };
  });
  return { schema_version: HRMS_SCHEMA_VERSION, violation_types: types, enquiries };
}

/** Events that have no enquiry yet, for the "Add enquiry" choice. */
export function freeTriggers(rows: HrmsRows): string[] {
  const used = new Set(rows.enquiries.map((e) => e.trigger));
  return HRMS_TRIGGERS.filter((t) => !used.has(t));
}
