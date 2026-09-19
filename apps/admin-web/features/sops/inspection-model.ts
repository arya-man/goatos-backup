// PROCUREMENT SOP (maintainer decision 2026-09-14, docs/decisions/procurement-sop.md).
//
// Pure model of `form_dsl.inspection`: the animal purchase inspection the phone runs, as PAGES of
// QUESTIONS, authored on /procurement/sops. Parses the backend document into editor rows and
// emits it back byte-faithfully, so a round-trip with no edits publishes the SAME document. The
// backend (animalpurchase/domain.ValidateInspection) is the authority on what is valid; this file
// only shapes and pre-checks so the author gets the message beside the field.

export const INSPECTION_SCHEMA_VERSION = "goatos.sop-inspection.v1";

export type QuestionKind = "choice" | "multi" | "text" | "number" | "media" | "vendor";
export type CaptureKind = "both" | "photo" | "video";

export type InspectionOptionRow = { value: string; label: string };

export type InspectionQuestionRow = {
  id: string; // editor row id
  key: string; // question id on the wire
  kind: QuestionKind;
  title: string;
  hint: string;
  required: boolean;
  options: InspectionOptionRow[];
  allowOther: boolean;
  slot: string;
  maxFiles: number;
  accepts: CaptureKind;
  min: string;
  max: string;
  unit: string;
  onlyIfQuestion: string;
  onlyIfValue: string;
  // VENDOR FORM (2026-09-19): a pick-one whose choices come from the vendor catalog at compile
  // time (record types, states, ...) names that catalog; its options are not authored here.
  catalog: string;
};

export type InspectionPageRow = { id: string; key: string; title: string; hint: string; questions: InspectionQuestionRow[] };

// The load form is one page of questions recorded once per purchase load (no media).
export type InspectionRows = { loadForm: InspectionQuestionRow[]; pages: InspectionPageRow[] };

// Questions the register reads (typed columns, list titles, review chips): kind and closed
// choices are fixed by the backend; title, hint, compulsory, page and position stay editable.
export const LOCKED_QUESTION_KEYS = new Set(["species", "goat_id", "sex", "weight_kg", "height_cm", "rectal_temp_c", "field_verdict", "breed", "notes"]);
export const LOCKED_OPTION_KEYS = new Set(["species", "sex", "field_verdict", "farm"]);
export const LOCKED_LOAD_KEYS = new Set(["load_ref", "vendor", "farm", "expected_count", "notes"]);
export const REQUIRED_LOAD_KEYS = new Set(["load_ref", "vendor", "farm"]);

// VENDOR FORM (maintainer instruction 2026-09-19, docs/decisions/sales-sop.md -> "The vendor form"):
// `form_dsl.vendor_form` of the sales.vendor SOP, the same pages-of-questions shape without a load
// form or media. Typed questions are the register's own columns (locked id and kind; catalog-backed
// choices); four identity questions must stay compulsory. Mirrors procurement/domain/vendor_form.go.
export const VENDOR_FORM_SCHEMA_VERSION = "goatos.sop-vendor-form.v1";
export const VENDOR_LOCKED_KEYS = new Set([
  "business_name", "record_type", "contact_person_name", "phone_number", "state", "city", "status",
  "capacity_quantity", "capacity_unit", "supply_frequency", "feed", "breed", "price_per_goat",
  "eta_after_order_days", "average_animal_weight_kg", "comments", "details", "bank_name", "account_no",
  "ifsc_code", "upi_id", "pan_number", "filtered_stock",
]);
export const VENDOR_REQUIRED_KEYS = new Set(["business_name", "record_type", "state", "status"]);

function str(v: unknown, fallback = ""): string {
  return typeof v === "string" ? v : fallback;
}
function num(v: unknown): string {
  return typeof v === "number" && Number.isFinite(v) ? String(v) : "";
}
function obj(v: unknown): Record<string, unknown> | null {
  return v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : null;
}

let seq = 0;
export function newRowId(prefix = "iq"): string {
  seq += 1;
  return `${prefix}-${Date.now().toString(36)}-${seq}`;
}

export function blankQuestion(kind: QuestionKind = "choice"): InspectionQuestionRow {
  return {
    id: newRowId(), key: "", kind, title: "", hint: "", required: true,
    options: kind === "choice" ? [{ value: "yes", label: "Yes" }, { value: "no", label: "No" }] : [],
    allowOther: false, slot: "", maxFiles: kind === "media" ? 1 : 0, accepts: "both",
    min: "", max: "", unit: "", onlyIfQuestion: "", onlyIfValue: "", catalog: "",
  };
}

export function blankPage(): InspectionPageRow {
  return { id: newRowId("ip"), key: "", title: "", hint: "", questions: [] };
}

// slugKey derives a stable key from a title for NEW rows (an existing key is never rewritten).
export function slugKey(title: string, taken: Set<string>, fallback = "question"): string {
  const base = title.toLowerCase().replace(/[^a-z0-9]+/g, "_").replace(/^_+|_+$/g, "").replace(/^[^a-z]+/, "").slice(0, 40) || fallback;
  let key = base;
  let n = 2;
  while (taken.has(key)) {
    key = `${base}_${n}`;
    n += 1;
  }
  return key;
}

function acceptsOf(v: unknown): CaptureKind {
  const list = Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
  if (list.length === 1 && list[0] === "photo") return "photo";
  if (list.length === 1 && list[0] === "video") return "video";
  return "both";
}

function parseQuestion(rq: unknown): InspectionQuestionRow[] {
  const q = obj(rq);
  if (!q) return [];
  const onlyIf = obj(q["only_if"]);
  const options = Array.isArray(q["options"]) ? q["options"].flatMap((o) => { const oo = obj(o); return oo ? [{ value: str(oo["value"]), label: str(oo["label"]) }] : []; }) : [];
  return [
    {
      id: newRowId(),
      key: str(q["id"]),
      kind: (str(q["kind"], "choice") || "choice") as QuestionKind,
      title: str(q["title"]),
      hint: str(q["hint"]),
      required: q["required"] === true,
      options,
      allowOther: q["allow_other"] === true,
      slot: str(q["slot"]),
      maxFiles: typeof q["max_files"] === "number" ? Math.trunc(q["max_files"] as number) : 0,
      accepts: acceptsOf(q["accepts"]),
      min: num(q["min"]),
      max: num(q["max"]),
      unit: str(q["unit"]),
      onlyIfQuestion: onlyIf ? str(onlyIf["question_id"]) : "",
      onlyIfValue: onlyIf ? str(onlyIf["value"]) : "",
      catalog: str(q["catalog"]),
    },
  ];
}

function parsePages(section: Record<string, unknown>): InspectionPageRow[] {
  const pages = Array.isArray(section["pages"]) ? section["pages"] : [];
  return pages.flatMap((raw) => {
    const p = obj(raw);
    if (!p) return [];
    const qs = Array.isArray(p["questions"]) ? p["questions"] : [];
    return [{ id: newRowId("ip"), key: str(p["key"]), title: str(p["title"]), hint: str(p["hint"]), questions: qs.flatMap(parseQuestion) }];
  });
}

// parseVendorForm reads form_dsl.vendor_form into the same editor rows (no load form).
export function parseVendorForm(formDsl: unknown): InspectionRows | null {
  const dsl = obj(formDsl);
  const vf = dsl ? obj(dsl["vendor_form"]) : null;
  if (!vf) return null;
  return { loadForm: [], pages: parsePages(vf) };
}

export function emitVendorForm(rows: InspectionRows): Record<string, unknown> {
  return {
    schema_version: VENDOR_FORM_SCHEMA_VERSION,
    pages: rows.pages.map((p) => {
      const out: Record<string, unknown> = { key: p.key };
      if (p.title.trim()) out.title = p.title;
      if (p.hint.trim()) out.hint = p.hint;
      out.questions = p.questions.map(emitQuestion);
      return out;
    }),
  };
}

// vendorFormProblems: the page rules shared with the inspection, minus load form and media, plus
// the register's locked ids and compulsory identity questions.
export function vendorFormProblems(rows: InspectionRows): string[] {
  const problems: string[] = [];
  const seen = new Map<string, InspectionQuestionRow>();
  const pageKeys = new Set<string>();
  if (rows.pages.length === 0) problems.push("Add at least one page");
  rows.pages.forEach((p, pi) => {
    const pageAt = p.title.trim() || `Page ${pi + 1}`;
    if (!p.key.trim()) problems.push(`${pageAt}: needs a key`);
    if (pageKeys.has(p.key)) problems.push(`${pageAt}: key "${p.key}" is used twice`);
    pageKeys.add(p.key);
    if (p.questions.length === 0) problems.push(`${pageAt}: needs at least one question`);
    p.questions.forEach((q, qi) => {
      const at = `${pageAt} · question ${qi + 1}`;
      if (!q.key.trim()) problems.push(`${at}: needs a key`);
      if (seen.has(q.key)) problems.push(`${at}: key "${q.key}" is used twice`);
      if (!q.title.trim()) problems.push(`${at}: needs the question text`);
      if (q.kind === "media" || q.kind === "vendor") problems.push(`${at}: a vendor form takes a choice, text or number`);
      if ((q.kind === "choice" || q.kind === "multi") && !q.catalog && q.options.filter((o) => o.value.trim() && o.label.trim()).length === 0) problems.push(`${at}: a pick-one / pick-many question needs at least one choice`);
      if (q.allowOther && !q.options.some((o) => o.value.trim() === "other")) problems.push(`${at}: the free-text "other" needs a choice whose value is "other"`);
      if (q.kind === "number" && q.min.trim() && q.max.trim() && Number(q.min) > Number(q.max)) problems.push(`${at}: min must not exceed max`);
      if (VENDOR_REQUIRED_KEYS.has(q.key) && !q.required) problems.push(`${at}: "${q.key}" must stay compulsory`);
      if (q.onlyIfQuestion) {
        const dep = seen.get(q.onlyIfQuestion);
        if (!dep) problems.push(`${at}: "ask only when" must name an earlier question`);
        else if (dep.kind !== "choice") problems.push(`${at}: "ask only when" must name a pick-one question`);
        else if (!dep.catalog && !dep.options.some((o) => o.value === q.onlyIfValue)) problems.push(`${at}: "ask only when" needs one of that question's choices`);
      }
      seen.set(q.key, q);
    });
  });
  for (const k of VENDOR_REQUIRED_KEYS) {
    if (!seen.has(k)) problems.push(`A vendor cannot exist without "${k}" — it must stay on the form`);
  }
  return problems;
}

export function parseInspection(formDsl: unknown): InspectionRows | null {
  const dsl = obj(formDsl);
  const ins = dsl ? obj(dsl["inspection"]) : null;
  if (!ins) return null;
  const lf = obj(ins["load_form"]);
  const loadQs = lf && Array.isArray(lf["questions"]) ? lf["questions"] : [];
  return { loadForm: loadQs.flatMap(parseQuestion), pages: parsePages(ins) };
}

function emitQuestion(q: InspectionQuestionRow): Record<string, unknown> {
  const out: Record<string, unknown> = { id: q.key, kind: q.kind, title: q.title };
  if (q.hint.trim()) out.hint = q.hint;
  out.required = q.required;
  if (q.catalog) {
    // A catalog-backed question carries no authored options: the backend fills them at compile.
    out.catalog = q.catalog;
  } else if (q.kind === "choice" || q.kind === "multi") {
    out.options = q.options.map((o) => ({ value: o.value.trim(), label: o.label.trim() })).filter((o) => o.value && o.label);
    if (q.allowOther) out.allow_other = true;
  }
  if (q.kind === "media") {
    out.slot = q.slot.trim() || q.key;
    if (q.maxFiles > 0) out.max_files = q.maxFiles;
    out.accepts = q.accepts === "both" ? ["photo", "video"] : [q.accepts];
  }
  if (q.kind === "number") {
    if (q.min.trim() !== "" && Number.isFinite(Number(q.min))) out.min = Number(q.min);
    if (q.max.trim() !== "" && Number.isFinite(Number(q.max))) out.max = Number(q.max);
    if (q.unit.trim()) out.unit = q.unit.trim();
  }
  if (q.onlyIfQuestion) out.only_if = { question_id: q.onlyIfQuestion, value: q.onlyIfValue };
  return out;
}

export function emitInspection(rows: InspectionRows): Record<string, unknown> {
  return {
    schema_version: INSPECTION_SCHEMA_VERSION,
    load_form: { questions: rows.loadForm.map(emitQuestion) },
    pages: rows.pages.map((p) => {
      const out: Record<string, unknown> = { key: p.key };
      if (p.title.trim()) out.title = p.title;
      if (p.hint.trim()) out.hint = p.hint;
      out.questions = p.questions.map(emitQuestion);
      return out;
    }),
  };
}

// Client-side pre-checks mirroring the backend validator's cheapest rules; the backend stays the
// authority (its 400 names the path too).
export function inspectionProblems(rows: InspectionRows): string[] {
  const problems: string[] = [];
  const loadSeen = new Map<string, InspectionQuestionRow>();
  rows.loadForm.forEach((q, qi) => {
    const at = `Load form · question ${qi + 1}`;
    if (!q.key.trim()) problems.push(`${at}: needs a key`);
    if (loadSeen.has(q.key)) problems.push(`${at}: key "${q.key}" is used twice`);
    if (!q.title.trim()) problems.push(`${at}: needs the question text`);
    if (q.kind === "media") problems.push(`${at}: photos and videos are recorded per animal, not on the load`);
    if ((q.kind === "choice" || q.kind === "multi") && q.options.filter((o) => o.value.trim() && o.label.trim()).length === 0) problems.push(`${at}: a pick-one / pick-many question needs at least one choice`);
    if (q.kind === "number" && q.min.trim() && q.max.trim() && Number(q.min) > Number(q.max)) problems.push(`${at}: min must not exceed max`);
    if (REQUIRED_LOAD_KEYS.has(q.key) && !q.required) problems.push(`${at}: "${q.key}" must stay compulsory`);
    if (q.onlyIfQuestion) {
      const dep = loadSeen.get(q.onlyIfQuestion);
      if (!dep) problems.push(`${at}: "ask only when" must name an earlier question`);
      else if (dep.kind !== "choice") problems.push(`${at}: "ask only when" must name a pick-one question`);
      else if (!dep.options.some((o) => o.value === q.onlyIfValue)) problems.push(`${at}: "ask only when" needs one of that question's choices`);
    }
    loadSeen.set(q.key, q);
  });
  for (const k of LOCKED_LOAD_KEYS) {
    if (!loadSeen.has(k)) problems.push(`The load reads "${k}" — it must stay in the load form`);
  }
  const seen = new Map<string, InspectionQuestionRow>();
  const pageKeys = new Set<string>();
  if (rows.pages.length === 0) problems.push("Add at least one page");
  rows.pages.forEach((p, pi) => {
    const pageAt = p.title.trim() || `Page ${pi + 1}`;
    if (!p.key.trim()) problems.push(`${pageAt}: needs a key`);
    if (pageKeys.has(p.key)) problems.push(`${pageAt}: key "${p.key}" is used twice`);
    pageKeys.add(p.key);
    if (pi > 0 && !p.title.trim()) problems.push(`Page ${pi + 1}: needs a heading`);
    if (p.questions.length === 0) problems.push(`${pageAt}: needs at least one question`);
    p.questions.forEach((q, qi) => {
      const at = `${pageAt} · question ${qi + 1}`;
      if (!q.key.trim()) problems.push(`${at}: needs a key`);
      if (seen.has(q.key)) problems.push(`${at}: key "${q.key}" is used twice`);
      if (!q.title.trim()) problems.push(`${at}: needs the question text`);
      if ((q.kind === "choice" || q.kind === "multi") && q.options.filter((o) => o.value.trim() && o.label.trim()).length === 0) problems.push(`${at}: a pick-one / pick-many question needs at least one choice`);
      if (q.allowOther && !q.options.some((o) => o.value.trim() === "other")) problems.push(`${at}: the free-text "other" needs a choice whose value is "other"`);
      if (q.kind === "number" && q.min.trim() && q.max.trim() && Number(q.min) > Number(q.max)) problems.push(`${at}: min must not exceed max`);
      if (q.kind === "media" && (q.maxFiles < 0 || q.maxFiles > 10)) problems.push(`${at}: up to 10 files`);
      if (q.onlyIfQuestion) {
        const dep = seen.get(q.onlyIfQuestion);
        if (!dep) problems.push(`${at}: "ask only when" must name an earlier question`);
        else if (dep.kind !== "choice") problems.push(`${at}: "ask only when" must name a pick-one question`);
        else if (!dep.options.some((o) => o.value === q.onlyIfValue)) problems.push(`${at}: "ask only when" needs one of that question's choices`);
      }
      seen.set(q.key, q);
    });
  });
  for (const k of LOCKED_QUESTION_KEYS) {
    if (!seen.has(k)) problems.push(`The register reads "${k}" — it must stay in the SOP`);
  }
  return problems;
}

// Every question flattened in phone order with its page, for the drawer summary.
export function flattenInspection(rows: InspectionRows): Array<{ page: InspectionPageRow; question: InspectionQuestionRow; index: number }> {
  const out: Array<{ page: InspectionPageRow; question: InspectionQuestionRow; index: number }> = [];
  let n = 0;
  for (const page of rows.pages) {
    for (const question of page.questions) {
      n += 1;
      out.push({ page, question, index: n });
    }
  }
  return out;
}
