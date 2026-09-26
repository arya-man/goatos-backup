// The routine drawer's form contract: how the client form encodes a routine into FormData and how
// the Server Action decodes it back into the generated PenRoutineWrite body. Pure module -- no
// server-only import, no React -- so the node test can exercise the same decoder the action runs.
//
// Scalars travel as plain fields; every LIST (pens, weekdays, month days, after-work kinds, roles)
// and the whole EVIDENCE block travel as ONE hidden JSON field each, so the decoder never has to
// reassemble indexed field names. The backend re-validates everything (limits, vocabularies, the
// month-day clamp); this decoder only shapes, it never invents a value the form did not carry.

export type PenRoutineWriteBody = {
  park_id: string;
  name: string;
  instruction?: string;
  scope_kind: ScopeKind;
  occupied_only: boolean;
  pens?: { shed_id: string; partition_label?: string }[];
  cadence_kind: CadenceKind;
  weekdays?: number[];
  month_days?: number[];
  after_work_kinds?: WorkKind[];
  interval_days: number | null;
  start_date?: string;
  due_offset_days?: number;
  notify_time?: string;
  review_kind: "verifier" | "none";
  evidence: PenRoutineEvidenceBody;
  /** The ONE person the routine is for; the server derives the roles and refuses a blank one. */
  assignee_user_id: string;
  row_version?: number;
};

export type PenRoutineQuestionBody = {
  id: string;
  kind: "yes_no" | "choice" | "multi_choice" | "number" | "text";
  title: string;
  hint?: string;
  required: boolean;
  options?: { value: string; label: string }[];
  min?: number | null;
  max?: number | null;
  unit?: string;
  /** The capture this question needs to count as answered; absent = the value alone answers it. */
  proof?: PenRoutineQuestionProofBody;
};

export type PenRoutineQuestionProofBody = {
  kind: "photo" | "video" | "photo_or_video";
  count: "single" | "multiple";
};

export const QUESTION_PROOF_KINDS = ["photo", "video", "photo_or_video"] as const;
export const QUESTION_PROOF_COUNTS = ["single", "multiple"] as const;

export type PenRoutineEvidenceBody = {
  questions: PenRoutineQuestionBody[];
  photo: { min: number; max: number };
  video: { min: number; max: number };
  presence: "required" | "off";
};

/** The hidden JSON field names the client form writes and the decoder reads. */
export const FORM_JSON_FIELDS = {
  pens: "pens_json",
  weekdays: "weekdays_json",
  monthDays: "month_days_json",
  afterWorkKinds: "after_work_kinds_json",
  evidence: "evidence_json",
} as const;

export const SCOPE_KINDS = ["all_pens", "selected_pens", "park"] as const;
export type ScopeKind = (typeof SCOPE_KINDS)[number];
export const CADENCE_KINDS = ["daily", "weekly", "monthly", "every_n_days", "after_work"] as const;
export type CadenceKind = (typeof CADENCE_KINDS)[number];
export const REVIEW_KINDS = ["verifier", "none"] as const;
export const PRESENCE_KINDS = ["required", "off"] as const;
export const QUESTION_KINDS = ["yes_no", "choice", "multi_choice", "number", "text"] as const;
/** The closed after-work vocabulary (domain/routine.go WorkKinds); an unknown key is dropped, never sent. */
export const WORK_KINDS = ["vaccination", "deworming", "anti_protozoan", "ticks_removal", "hoof_trimming", "hair_trimming", "weighing", "feed_distribution", "shifting"] as const;
export type WorkKind = (typeof WORK_KINDS)[number];

/** Backend limits mirrored for the form's own `min`/`max`/`maxLength` attributes (domain/routine.go). */
export const LIMITS = {
  nameMax: 80,
  questionsMax: 20,
  optionsMax: 12,
  proofMax: 5,
  dueOffsetMin: 0,
  dueOffsetMax: 30,
  intervalMin: 2,
  intervalMax: 90,
} as const;

function text(formData: FormData, key: string): string {
  const value = formData.get(key);
  return typeof value === "string" ? value.trim() : "";
}

function oneOf<T extends string>(value: string, allowed: readonly T[], fallback: T): T {
  return (allowed as readonly string[]).includes(value) ? (value as T) : fallback;
}

function intOr(value: string, fallback: number): number {
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) ? parsed : fallback;
}

function parseJson(raw: string): unknown {
  if (!raw) return undefined;
  try {
    return JSON.parse(raw);
  } catch {
    return undefined;
  }
}

function intList(raw: unknown): number[] {
  if (!Array.isArray(raw)) return [];
  return raw.map((item) => (typeof item === "number" ? item : Number.parseInt(String(item), 10))).filter((item) => Number.isFinite(item));
}

function stringList(raw: unknown): string[] {
  if (!Array.isArray(raw)) return [];
  return raw.map((item) => String(item).trim()).filter(Boolean);
}

function penList(raw: unknown): { shed_id: string; partition_label?: string }[] {
  if (!Array.isArray(raw)) return [];
  const out: { shed_id: string; partition_label?: string }[] = [];
  for (const item of raw) {
    if (!item || typeof item !== "object") continue;
    const shedId = String((item as { shed_id?: unknown }).shed_id ?? "").trim();
    if (!shedId) continue;
    const label = String((item as { partition_label?: unknown }).partition_label ?? "").trim();
    out.push(label ? { shed_id: shedId, partition_label: label } : { shed_id: shedId });
  }
  return out;
}

/** A number field on a question: absent/blank means "no bound", never 0. */
function bound(raw: unknown): number | null | undefined {
  if (raw === null || raw === undefined || raw === "") return undefined;
  const parsed = typeof raw === "number" ? raw : Number(raw);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function proofRule(raw: unknown): { min: number; max: number } {
  const rule = raw && typeof raw === "object" ? (raw as { min?: unknown; max?: unknown }) : {};
  const min = Math.max(0, intOr(String(rule.min ?? "0"), 0));
  const max = Math.max(0, intOr(String(rule.max ?? "0"), 0));
  return { min, max };
}

export function decodeEvidence(raw: unknown): PenRoutineEvidenceBody {
  const block = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  const questions: PenRoutineQuestionBody[] = [];
  if (Array.isArray(block.questions)) {
    for (const item of block.questions) {
      if (!item || typeof item !== "object") continue;
      const q = item as Record<string, unknown>;
      const kind = oneOf(String(q.kind ?? ""), QUESTION_KINDS, "yes_no");
      const question: PenRoutineQuestionBody = {
        id: String(q.id ?? "").trim(),
        kind,
        title: String(q.title ?? "").trim(),
        required: q.required === true || q.required === "true",
      };
      const hint = String(q.hint ?? "").trim();
      if (hint) question.hint = hint;
      if (kind === "choice" || kind === "multi_choice") {
        const options: { value: string; label: string }[] = [];
        if (Array.isArray(q.options)) {
          for (const option of q.options) {
            if (!option || typeof option !== "object") continue;
            const value = String((option as { value?: unknown }).value ?? "").trim();
            const label = String((option as { label?: unknown }).label ?? "").trim();
            if (value || label) options.push({ value, label });
          }
        }
        question.options = options;
      }
      if (kind === "number") {
        const min = bound(q.min);
        const max = bound(q.max);
        if (min !== undefined) question.min = min;
        if (max !== undefined) question.max = max;
        const unit = String(q.unit ?? "").trim();
        if (unit) question.unit = unit;
      }
      // Per-question proof: only a recognised medium travels; "none" / blank / unknown means no
      // proof block at all, so the backend's validate-or-reject sees exactly what was chosen.
      if (q.proof && typeof q.proof === "object") {
        const proof = q.proof as { kind?: unknown; count?: unknown };
        const kind = String(proof.kind ?? "").trim();
        if ((QUESTION_PROOF_KINDS as readonly string[]).includes(kind)) {
          question.proof = {
            kind: kind as PenRoutineQuestionProofBody["kind"],
            count: oneOf(String(proof.count ?? ""), QUESTION_PROOF_COUNTS, "single"),
          };
        }
      }
      questions.push(question);
    }
  }
  return {
    questions,
    photo: proofRule(block.photo),
    video: proofRule(block.video),
    presence: oneOf(String(block.presence ?? ""), PRESENCE_KINDS, "off"),
  };
}

/**
 * Decodes the drawer's FormData into the create / update body. Throws only when a field the
 * backend cannot default is missing (park, name); every other refusal is the backend's to make,
 * in its own farm words.
 */
export function decodePenRoutineWrite(formData: FormData): PenRoutineWriteBody {
  const parkId = text(formData, "park_id");
  const name = text(formData, "name");
  if (!parkId || !name) throw new Error("park_id and name are required");
  const scopeKind = oneOf(text(formData, "scope_kind"), SCOPE_KINDS, "all_pens");
  const cadenceKind = oneOf(text(formData, "cadence_kind"), CADENCE_KINDS, "daily");
  const intervalRaw = text(formData, "interval_days");
  const body: PenRoutineWriteBody = {
    park_id: parkId,
    name,
    scope_kind: scopeKind,
    // An unticked checkbox sends nothing, so the field is explicit: "on" / "off". A whole-park
    // task has no pens to skip, so it never carries the flag on.
    occupied_only: scopeKind !== "park" && text(formData, "occupied_only") === "on",
    cadence_kind: cadenceKind,
    // Only an every-few-days routine carries its N; a blank or non-number stays null so the
    // backend refuses it in its own words rather than the form inventing one.
    interval_days: cadenceKind === "every_n_days" && intervalRaw !== "" && Number.isFinite(Number.parseInt(intervalRaw, 10)) ? Number.parseInt(intervalRaw, 10) : null,
    review_kind: oneOf(text(formData, "review_kind"), REVIEW_KINDS, "none"),
    evidence: decodeEvidence(parseJson(text(formData, FORM_JSON_FIELDS.evidence))),
    // Sent as the picker posted it; a blank one goes to the backend, which refuses it as no_assignee.
    assignee_user_id: text(formData, "assignee_user_id"),
  };
  const instruction = text(formData, "instruction");
  if (instruction) body.instruction = instruction;
  const startDate = text(formData, "start_date");
  if (/^\d{4}-\d{2}-\d{2}$/.test(startDate)) body.start_date = startDate;
  // A whole-park task names no pens: the list is sent empty, whatever the form still held.
  if (scopeKind === "park") body.pens = [];
  if (scopeKind === "selected_pens") body.pens = penList(parseJson(text(formData, FORM_JSON_FIELDS.pens)));
  if (cadenceKind === "weekly") body.weekdays = intList(parseJson(text(formData, FORM_JSON_FIELDS.weekdays)));
  // Month days are NOT clamped here: a 31 on a 30-day month means the last day, a rule the backend owns.
  if (cadenceKind === "monthly") body.month_days = intList(parseJson(text(formData, FORM_JSON_FIELDS.monthDays)));
  if (cadenceKind === "after_work") {
    body.after_work_kinds = stringList(parseJson(text(formData, FORM_JSON_FIELDS.afterWorkKinds))).filter((kind): kind is WorkKind =>
      (WORK_KINDS as readonly string[]).includes(kind),
    );
  }
  const dueOffset = text(formData, "due_offset_days");
  if (dueOffset !== "") body.due_offset_days = intOr(dueOffset, 0);
  const notifyTime = text(formData, "notify_time");
  if (notifyTime) body.notify_time = notifyTime;
  const rowVersion = text(formData, "row_version");
  if (rowVersion !== "") body.row_version = intOr(rowVersion, 0);
  return body;
}

/** A question id derived from its title: letters, digits and underscores only, never empty-by-accident. */
export function slugQuestionId(title: string): string {
  return title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "")
    .slice(0, 40);
}

/** Keeps a hand-edited id inside the same alphabet as the derived one. */
export function cleanQuestionId(raw: string): string {
  return raw.replace(/[^A-Za-z0-9_]/g, "").slice(0, 40);
}
