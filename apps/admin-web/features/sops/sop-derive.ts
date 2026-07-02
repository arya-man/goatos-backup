// Pure derivation + DSL-builder helpers for the SOP Library. No React, no server — unit-friendly.
//
// The ONLY UI/UX source of truth is the committed dashboard mock (SOP Library / New SOP).
// The ONLY data source is the real admin API (`/admin/sops*`). The list endpoint returns thin
// SOPDefinition rows (no form_dsl); domain / trigger / steps / gates are derived from the real
// `code`, `name`, `description`, and the latest version's `form_dsl` + `proof_policy` — never invented.
//
// Backend authority for field types is `supportedFieldType` in
// backend/internal/sop/app/service.go:493 — text|number|date_time|select|multiselect|goat_lookup|
// rfid_scan|location_picker|photo_proof|video_proof. The builder offers the mock's richer type
// vocabulary and maps each to a backend-supported type so the emitted form_dsl always validates;
// types with no native backend equivalent are mapped + flagged as gaps (see STEP_TYPES / DSL_GAPS).

export type DomainId =
  | "counts"
  | "health"
  | "breeding"
  | "parks"
  | "procurement"
  | "farmernet"
  | "inventory"
  | "people";

const DOMAIN_LABEL: Record<DomainId | "general", string> = {
  counts: "Counts",
  health: "Health",
  breeding: "Breeding",
  parks: "Parks",
  procurement: "Procurement",
  farmernet: "Farmer Network",
  inventory: "Inventory",
  people: "HR",
  general: "General",
};

// Keyword → domain classifier. Backend SOPDefinition carries NO domain/category field (a real gap, see
// DSL_GAPS), so the chip is derived from the real `code` + `name` text. Order = priority.
const DOMAIN_KEYWORDS: Array<{ id: DomainId; words: string[] }> = [
  { id: "breeding", words: ["breed", "birth", "kid", "colostrum", "milk", "lactation", "pregnan", "ultrasound", "abortion", "ai_", "insemination", "mother"] },
  { id: "health", words: ["vacc", "health", "disease", "treatment", "diagnos", "icu", "death", "post_mortem", "post-mortem", "not_eating", "not-eating", "symptom", "problem", "lab_test", "lab-test"] },
  { id: "inventory", words: ["inventory", "stock", "cold_chain", "cold-chain", "reorder", "po_", "grn", "vaccine_batch", "medicine", "reorder_po"] },
  { id: "procurement", words: ["procure", "arrival", "load", "vendor", "intake", "warmup", "sale", "dispatch"] },
  { id: "farmernet", words: ["farmer", "crop", "fodder", "sowing", "harvest", "napier"] },
  { id: "people", words: ["operator", "attendance", "penalty", "pip", "kyc", "onboard", "checklist", "12q", "hr_", "eod"] },
  { id: "parks", words: ["shift", "feed", "pack", "distribut", "panel", "sanit", "quarantine", "clean", "park", "shed"] },
  { id: "counts", words: ["count", "tag", "weigh", "status", "stage", "transition", "reconcil"] },
];

export function classifyDomain(code: string, name: string): DomainId | "general" {
  const hay = `${code} ${name}`.toLowerCase();
  for (const rule of DOMAIN_KEYWORDS) {
    if (rule.words.some((w) => hay.includes(w))) return rule.id;
  }
  return "general";
}

// SCOPE LOCK (context/execution/sop-vaccination-backend-handoff.md): the visible /sops slice is
// vaccination ONLY. The shared SOP engine is generic, but the product surface is not yet. A SOP is in
// the visible slice when its real `code`/`name` is clearly vaccination (`vaccination.drive`,
// `vaccination.*`, or "vaccin…"). Everything else (shifting, feed, counts, …) is hidden from /sops.
export const VACCINATION_SLICE_LABEL = "Vaccination";

export function isVaccinationSop(code: string, name: string): boolean {
  const c = (code || "").toLowerCase();
  const n = (name || "").toLowerCase();
  return c === "vaccination" || c.startsWith("vaccination.") || c.startsWith("vaccination_") || c.includes("vaccin") || n.includes("vaccin");
}

export function domainLabel(id: DomainId | "general"): string {
  return DOMAIN_LABEL[id];
}

// ---- safe readers over the opaque form_dsl / proof_policy maps ----

type Json = Record<string, unknown>;

function asObject(value: unknown): Json | null {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Json) : null;
}
function asString(value: unknown): string | null {
  return typeof value === "string" ? value : null;
}
function asBool(value: unknown): boolean {
  return value === true;
}
function asNumber(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

const TRIGGERS = ["form", "cron", "sensor", "manual"] as const;
export type SopTrigger = (typeof TRIGGERS)[number];

// Trigger is NOT a first-class field in the backend SOPFormDSL (gap). SOPs authored by THIS builder
// embed it under form_dsl.trigger (an extra key the backend ignores), so they round-trip; SOPs without
// it return null and the trigger chip is omitted rather than faked.
export function deriveTrigger(formDsl: unknown): SopTrigger | null {
  const dsl = asObject(formDsl);
  const t = dsl ? asString(dsl["trigger"]) : null;
  return t && (TRIGGERS as readonly string[]).includes(t) ? (t as SopTrigger) : null;
}

export type DslField = { key: string; label: string; type: string; required: boolean; options: string[]; helpText: string | null };

// deriveFieldOptions reads the (backend-ignored, metadata) `options` a select/multiselect field carries,
// tolerating both the emitted `[{value,label}]` shape and a plain `string[]` of labels.
function deriveFieldOptions(field: Json): string[] {
  const raw = field["options"];
  if (!Array.isArray(raw)) return [];
  const out: string[] = [];
  for (const entry of raw) {
    const obj = asObject(entry);
    if (obj) {
      const label = asString(obj["label"]) ?? asString(obj["value"]);
      if (label) out.push(label);
    } else {
      const label = asString(entry);
      if (label) out.push(label);
    }
  }
  return out;
}

export function deriveFields(formDsl: unknown): DslField[] {
  const dsl = asObject(formDsl);
  const raw = dsl && Array.isArray(dsl["fields"]) ? (dsl["fields"] as unknown[]) : [];
  const out: DslField[] = [];
  raw.forEach((entry, i) => {
    const f = asObject(entry);
    if (!f) return;
    out.push({
      key: asString(f["key"]) ?? `field_${i + 1}`,
      label: asString(f["label"]) ?? asString(f["key"]) ?? `Step ${i + 1}`,
      type: asString(f["type"]) ?? "text",
      required: asBool(f["required"]),
      options: deriveFieldOptions(f),
      helpText: asString(f["help_text"]),
    });
  });
  return out;
}

export function deriveStepCount(formDsl: unknown): number | null {
  const dsl = asObject(formDsl);
  if (!dsl || !Array.isArray(dsl["fields"])) return null;
  return (dsl["fields"] as unknown[]).length;
}

// Gate summary derived from the REAL proof_policy + form-level flags — the mock's "gate summary text".
export function deriveGates(formDsl: unknown, proofPolicy: unknown): string[] {
  const gates: string[] = [];
  const policy = asObject(proofPolicy);
  if (policy && asBool(policy["required"])) {
    // Canonical proof_policy keys (backend evaluator + 000081 seed): types[], minimum_count.
    // Old SOP Library drafts used proof_type/min_count; keep read fallback only for existing rows.
    const types = proofPolicyTypes(policy);
    gates.push(types.length > 0 ? `${types.join("/")} proof` : "Proof required");
    if (asBool(policy["verify_before_apply"])) gates.push("Verify before apply");
    const min = proofPolicyMinimumCount(policy);
    if (min && min > 1) gates.push(`min ${min}`);
    const scope = proofPolicySubjectScope(policy);
    if (scope === "goat") gates.push("Per-goat");
    else if (scope === "batch") gates.push("Batch");
  }
  const dsl = asObject(formDsl);
  if (dsl && asBool(dsl["repeat_for_each_goat"]) && !gates.includes("Per-goat")) {
    gates.push("Repeat per goat");
  }
  if (Array.isArray(dsl?.["rules"]) && (dsl?.["rules"] as unknown[]).length > 0) {
    gates.push("Conditional rules");
  }
  return gates;
}

function proofPolicyTypes(policy: Json): string[] {
  if (Array.isArray(policy["types"])) {
    const canonical = (policy["types"] as unknown[]).filter((t): t is string => typeof t === "string" && t.trim() !== "");
    if (canonical.length > 0) return canonical;
  }
  const legacyType = asString(policy["proof_type"]);
  return legacyType ? [legacyType] : [];
}

function proofPolicyMinimumCount(policy: Json): number | null {
  return asNumber(policy["minimum_count"]) ?? asNumber(policy["min_count"]);
}

function proofPolicySubjectScope(policy: Json): string | null {
  return asString(policy["subject_scope"]) ?? asString(policy["scope"]);
}

// =====================================================================================
// Card / detail view model — built on the server from real SOPDefinition + latest SOPVersion
// =====================================================================================

type SopDefLike = {
  sop_id: string;
  code: string;
  name: string;
  description: string;
  status: "draft" | "active" | "retired";
  version_count: number;
  active_sop_version_id?: string | null;
};
type SopVersionLike = {
  version: number;
  version_label: string;
  status: "draft" | "published" | "retired";
  form_dsl: unknown;
  proof_policy: unknown;
};

export type SopCardView = {
  sopId: string;
  code: string;
  name: string;
  description: string;
  domain: DomainId | "general";
  domainLabel: string;
  trigger: SopTrigger | null;
  stepCount: number | null;
  gates: string[];
  status: "draft" | "active" | "retired";
  versionLabel: string | null;
  versionNumber: number | null;
  versionStatus: "draft" | "published" | "retired" | null;
  hasVersion: boolean;
  fields: Array<{ label: string; type: string; required: boolean; options: string[]; helpText: string | null }>;
};

// toSopView maps the real API rows to the card facets. Everything is derived — no invented inventory.
export function toSopView(def: SopDefLike, version: SopVersionLike | null): SopCardView {
  const domain = classifyDomain(def.code, def.name);
  // The page only passes vaccination SOPs into this slice, so the card chip reads "Vaccination" rather
  // than the generic classifier label — the surface must not imply all-domain SOP launch.
  const label = isVaccinationSop(def.code, def.name) ? VACCINATION_SLICE_LABEL : domainLabel(domain);
  return {
    sopId: def.sop_id,
    code: def.code,
    name: def.name,
    description: def.description,
    domain,
    domainLabel: label,
    trigger: version ? deriveTrigger(version.form_dsl) : null,
    stepCount: version ? deriveStepCount(version.form_dsl) : null,
    gates: version ? deriveGates(version.form_dsl, version.proof_policy) : [],
    status: def.status,
    versionLabel: version ? version.version_label : null,
    versionNumber: version ? version.version : null,
    versionStatus: version ? version.status : null,
    hasVersion: Boolean(version),
    fields: version
      ? deriveFields(version.form_dsl).map((f) => ({ label: f.label, type: f.type, required: f.required, options: f.options, helpText: f.helpText }))
      : [],
  };
}

// =====================================================================================
// New SOP form builder — step type vocabulary + DSL emit
// =====================================================================================

export type StepTypeValue =
  | "text"
  | "number"
  | "yesno"
  | "select"
  | "multiselect"
  | "goat_scan"
  | "shed_picker"
  | "vaccine_batch_picker"
  | "medicine_picker"
  | "photo_proof"
  | "video_proof";

// Backend-supported field types (authority: backend/internal/sop/app/service.go supportedFieldType).
type BackendFieldType =
  | "text"
  | "number"
  | "date_time"
  | "boolean"
  | "select"
  | "multiselect"
  | "goat_scan"
  | "rfid_scan"
  | "goat_lookup"
  | "shed_picker"
  | "cohort_picker"
  | "location_picker"
  | "vaccine_batch_picker"
  | "medicine_picker"
  | "session_picker"
  | "photo_proof"
  | "video_proof";

type StepTypeDef = {
  value: StepTypeValue;
  label: string;
  backend: BackendFieldType;
  optionSource?: string;
  proof?: boolean;
};

// Builder type list in the mock's order. Each emits its NATIVE backend field type — the backend now
// supports boolean, goat_scan, shed_picker, vaccine_batch_picker, medicine_picker, session_picker, etc.
// (service.go supportedFieldType), so no lossy select/option_source workaround is needed.
export const STEP_TYPES: StepTypeDef[] = [
  { value: "text", label: "text", backend: "text" },
  { value: "number", label: "number", backend: "number" },
  { value: "yesno", label: "yes/no", backend: "boolean" },
  { value: "select", label: "select", backend: "select" },
  { value: "multiselect", label: "multiselect", backend: "multiselect" },
  { value: "goat_scan", label: "goat scan/RFID", backend: "goat_scan" },
  { value: "shed_picker", label: "shed picker", backend: "shed_picker" },
  { value: "vaccine_batch_picker", label: "vaccine batch picker", backend: "vaccine_batch_picker", optionSource: "vaccine_batches" },
  { value: "medicine_picker", label: "medicine picker", backend: "medicine_picker", optionSource: "medicines" },
  { value: "photo_proof", label: "photo proof", backend: "photo_proof", proof: true },
  { value: "video_proof", label: "video proof", backend: "video_proof", proof: true },
];

const STEP_TYPE_BY_VALUE: Record<StepTypeValue, StepTypeDef> = Object.fromEntries(
  STEP_TYPES.map((t) => [t.value, t]),
) as Record<StepTypeValue, StepTypeDef>;

export function stepTypeDef(value: StepTypeValue): StepTypeDef {
  return STEP_TYPE_BY_VALUE[value] ?? STEP_TYPES[0];
}

export function isProofStepType(value: StepTypeValue): boolean {
  return Boolean(STEP_TYPE_BY_VALUE[value]?.proof);
}

// Remaining frontend/backend integration notes shown in the modal + handed to Codex. Field types and
// declarative rules are now natively supported + validated by the backend (service.go), so those are no
// longer gaps. What remains:
export const DSL_GAPS: string[] = [
  "No backend domain/category on SOPDefinition — the SOP Library domain chip + vaccination-slice filter are derived from `code`/`name` (need `code_prefix`/category on the list API for robust server-side scoping).",
  "`/admin/sops` has no `code_prefix` filter — the page fetches latest 200 then filters to vaccination in memory; fine for the current tiny set, not at scale.",
  "`trigger` is not a first-class backend concept — emitted as `form_dsl.trigger` metadata (accepted, not interpreted by the validator).",
  "Picker `option_source` (vaccine_batches / medicines) is emitted as field metadata but not yet backed by live backend option-source endpoints.",
];

export type BuilderOption = { id: string; label: string };

// A question's conditional-visibility rule ("show only when the answer to an EARLIER question …").
// Operators map 1:1 to the backend form_dsl condition operators (answered→not_empty,
// not_answered→empty). refIndex must always point at a question BEFORE this one.
export type ConditionOperator =
  | "answered"
  | "not_answered"
  | "equals"
  | "not_equals"
  | "is_one_of"
  | "gt"
  | "gte"
  | "lt"
  | "lte";

export type BuilderCondition = {
  refId: string; // stable id of the earlier question this depends on (position-independent under reorder)
  operator: ConditionOperator;
  value: string; // used by equals / not_equals / is_one_of (comma list) / gt / gte / lt / lte
};

export type BuilderStep = {
  id: string;
  type: StepTypeValue;
  label: string;
  helpText: string;
  required: boolean;
  options: BuilderOption[]; // select / multiselect
  min: string; // number lower bound (string so "" = unset)
  max: string; // number upper bound
  unit: string; // number unit (ml, kg…)
  placeholder: string; // text placeholder
  longText: boolean; // text → paragraph
  visibleWhen: BuilderCondition | null; // null = always show
};

// Which per-type config panel a question renders in the builder. Drives the option editor, number
// bounds, text placeholder, or the informational note for pickers / scan / proof / boolean.
export type FieldConfigKind = "options" | "number" | "text" | "proof" | "picker" | "scan" | "boolean";

export function fieldConfigKind(type: StepTypeValue): FieldConfigKind {
  switch (type) {
    case "select":
    case "multiselect":
      return "options";
    case "number":
      return "number";
    case "text":
      return "text";
    case "photo_proof":
    case "video_proof":
      return "proof";
    case "goat_scan":
      return "scan";
    case "yesno":
      return "boolean";
    default:
      return "picker"; // shed / vaccine batch / medicine pickers
  }
}

const OPERATORS_NEEDING_VALUE: ReadonlySet<ConditionOperator> = new Set(["equals", "not_equals", "is_one_of", "gt", "gte", "lt", "lte"]);

export function conditionNeedsValue(operator: ConditionOperator): boolean {
  return OPERATORS_NEEDING_VALUE.has(operator);
}

export function conditionNeedsList(operator: ConditionOperator): boolean {
  return operator === "is_one_of";
}

// The builder can only emit policies satisfiable by its form fields. Attachment media is supported by
// proof records, but SOP Library needs a real attachment_proof field before it can author attachment policy.
export type ProofType = "video" | "photo";
export type SubjectScope = "batch" | "goat";

// The New SOP builder is locked to the vaccination slice (handoff scope lock). The domain is not a
// free choice in this product slice — future domains are NOT exposed as selectable live product.
export type SopSliceDomain = "vaccination";

export type SopBuilderInput = {
  name: string;
  domain: SopSliceDomain;
  trigger: SopTrigger;
  steps: BuilderStep[];
  proofRequired: boolean;
  proofType: ProofType;
  verifyBeforeApply: boolean;
  minCount: number;
  subjectScope: SubjectScope;
};

// slugify → snake_case token valid for the backend code regex `^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`.
export function slugify(value: string, fallback: string): string {
  const s = value
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "")
    .replace(/_{2,}/g, "_");
  if (!s) return fallback;
  return /^[a-z]/.test(s) ? s : `s_${s}`;
}

export function buildSopCode(input: Pick<SopBuilderInput, "name" | "domain">): string {
  const prefix = input.domain; // "vaccination" for the current slice
  let slug = slugify(input.name, "session");
  // Avoid redundant `vaccination.vaccination_session`: drop a leading domain token from the slug.
  if (slug === prefix) slug = "session";
  else if (slug.startsWith(`${prefix}_`)) slug = slug.slice(prefix.length + 1) || "session";
  return `${prefix}.${slug}`;
}

type EmittedOption = { value: string; label: string };

type EmittedField = {
  key: string;
  label: string;
  type: StepTypeDef["backend"];
  required?: boolean;
  help_text?: string;
  options?: EmittedOption[];
  multiple?: boolean;
  min?: number;
  max?: number;
  unit?: string;
  placeholder?: string;
  long_text?: boolean;
  option_source?: string;
  repeat?: boolean;
};

type EmittedCondition = { field: string; operator: string; value?: unknown };

type EmittedRule = {
  type: "visible_if" | "required_if" | "proof_required_if" | "block_submission_if";
  field: string;
  message?: string;
  when: EmittedCondition;
};

export type EmittedFormDsl = {
  schema_version: "goatos.sop-form.v1";
  sop_code: string;
  title: string;
  trigger: SopTrigger;
  repeat_for_each_goat?: boolean;
  fields: EmittedField[];
  rules?: EmittedRule[];
};

// Canonical proof_policy shape — keys match the backend evaluator (service.go proofTypes /
// proofMinimumCount / proofRequiresReview) and the committed vaccination seed (000081): types[] +
// minimum_count, NOT proof_type/min_count.
export type EmittedProofPolicy = {
  required: boolean;
  types: ProofType[];
  minimum_count: number;
  subject_scope: SubjectScope;
  verify_before_apply: boolean;
};

function fieldKeys(steps: BuilderStep[]): string[] {
  const used = new Map<string, number>();
  return steps.map((s, i) => {
    let base = slugify(s.label, `step_${i + 1}`);
    const seen = used.get(base) ?? 0;
    used.set(base, seen + 1);
    if (seen > 0) base = `${base}_${seen + 1}`;
    return base;
  });
}

// emittedOptions turns a select/multiselect question's author-typed choices into unique {value,label}
// pairs (value = slugged label, de-duplicated). Blank choices are dropped so the field never carries
// empty options.
function emittedOptions(step: BuilderStep): EmittedOption[] {
  const used = new Map<string, number>();
  const out: EmittedOption[] = [];
  step.options.forEach((option, i) => {
    const label = option.label.trim();
    if (!label) return;
    let value = slugify(label, `choice_${i + 1}`);
    const seen = used.get(value) ?? 0;
    used.set(value, seen + 1);
    if (seen > 0) value = `${value}_${seen + 1}`;
    out.push({ value, label });
  });
  return out;
}

// mapCondition converts a builder visibility rule into a backend form_dsl condition targeting the
// referenced earlier question's field key. answered/not_answered map to not_empty/empty; is_one_of maps
// to `in` with a parsed list; numeric operators coerce the value to a number.
function mapCondition(cond: BuilderCondition, refKey: string): EmittedCondition {
  switch (cond.operator) {
    case "answered":
      return { field: refKey, operator: "not_empty" };
    case "not_answered":
      return { field: refKey, operator: "empty" };
    case "is_one_of":
      return {
        field: refKey,
        operator: "in",
        value: cond.value.split(",").map((v) => v.trim()).filter((v) => v !== ""),
      };
    case "gt":
    case "gte":
    case "lt":
    case "lte": {
      const n = Number(cond.value);
      return { field: refKey, operator: cond.operator, value: Number.isFinite(n) ? n : cond.value };
    }
    default: // equals / not_equals
      return { field: refKey, operator: cond.operator, value: cond.value };
  }
}

function parsedBound(raw: string): number | undefined {
  const trimmed = raw.trim();
  if (trimmed === "") return undefined;
  const n = Number(trimmed);
  return Number.isFinite(n) ? n : undefined;
}

// buildFormDsl emits a valid goatos.sop-form.v1 form_dsl. Every field.type is a NATIVE backend-supported
// type, so the version always passes service.ValidateFormDSL. Per-type config (choices, number bounds,
// unit, placeholder, help text) is carried as field metadata; conditional visibility + conditional
// requirement are emitted as declarative visible_if / required_if rules the backend evaluator honours.
export function buildFormDsl(input: SopBuilderInput): EmittedFormDsl {
  const keys = fieldKeys(input.steps);
  const fields: EmittedField[] = input.steps.map((step, i) => {
    const def = stepTypeDef(step.type);
    const kind = fieldConfigKind(step.type);
    const field: EmittedField = { key: keys[i], label: step.label.trim() || `Step ${i + 1}`, type: def.backend };

    if (step.helpText.trim()) field.help_text = step.helpText.trim();

    // required: proof fields are always required when proof is on so the proof gate has a target field.
    // A conditionally-visible question is made required via a required_if rule (below), never a hard
    // field.required — otherwise it would block submission even while hidden.
    const refIdx = step.visibleWhen ? input.steps.findIndex((s) => s.id === step.visibleWhen!.refId) : -1;
    const conditional = refIdx >= 0 && refIdx < i;
    if (def.proof && input.proofRequired) field.required = true;
    else if (step.required && !conditional) field.required = true;

    if (kind === "options") {
      const options = emittedOptions(step);
      if (options.length > 0) field.options = options;
      if (step.type === "multiselect") field.multiple = true;
    }
    if (kind === "number") {
      const min = parsedBound(step.min);
      const max = parsedBound(step.max);
      if (min !== undefined) field.min = min;
      if (max !== undefined) field.max = max;
      if (step.unit.trim()) field.unit = step.unit.trim();
    }
    if (kind === "text") {
      if (step.placeholder.trim()) field.placeholder = step.placeholder.trim();
      if (step.longText) field.long_text = true;
    }
    if (def.optionSource) field.option_source = def.optionSource;
    if (input.subjectScope === "goat") field.repeat = true;
    return field;
  });

  const rules: EmittedRule[] = [];
  input.steps.forEach((step, i) => {
    const key = keys[i];
    const cond = step.visibleWhen;
    if (!cond) return;
    const refIdx = input.steps.findIndex((s) => s.id === cond.refId);
    if (refIdx < 0 || refIdx >= i) return; // ref must be an EARLIER question in the current order
    const when = mapCondition(cond, keys[refIdx]);
    rules.push({ type: "visible_if", field: key, when });
    // A required + conditionally-visible question is required only while it is shown.
    if (step.required && !(stepTypeDef(step.type).proof && input.proofRequired)) {
      rules.push({ type: "required_if", field: key, when });
    }
  });

  const dsl: EmittedFormDsl = {
    schema_version: "goatos.sop-form.v1",
    sop_code: buildSopCode(input),
    title: input.name.trim() || "New SOP",
    trigger: input.trigger,
    fields,
  };
  if (input.subjectScope === "goat") dsl.repeat_for_each_goat = true;
  if (rules.length > 0) dsl.rules = rules;
  return dsl;
}

export function buildProofPolicy(input: SopBuilderInput): EmittedProofPolicy {
  return {
    required: input.proofRequired,
    types: [input.proofType],
    minimum_count: Math.max(1, Math.floor(input.minCount) || 1),
    subject_scope: input.subjectScope,
    verify_before_apply: input.verifyBeforeApply,
  };
}

// hasProofField mirrors the BACKEND validateProofPolicy → hasProofFieldForTypes: a required proof
// policy needs a proof field MATCHING the selected proof type (video→video_proof, photo→photo_proof),
// not merely any proof step. Blocking on the matching type prevents an orphaned SOP — create mode
// persists the SOP row before the version, so a type-mismatched version rejected by the backend would
// otherwise leave a versionless SOP behind.
export function hasProofField(input: SopBuilderInput): boolean {
  const want: StepTypeValue = input.proofType === "photo" ? "photo_proof" : "video_proof";
  return input.steps.some((s) => s.type === want);
}

// =====================================================================================
// Reverse of buildFormDsl — reconstruct full builder state from a persisted version so the
// full-page builder can EDIT an existing SOP faithfully (choices, number bounds, help text, and
// conditional visibility all round-trip, not just label/type/required).
// =====================================================================================

const BACKEND_TO_STEP: Record<string, StepTypeValue> = {
  text: "text",
  number: "number",
  boolean: "yesno",
  select: "select",
  multiselect: "multiselect",
  goat_scan: "goat_scan",
  rfid_scan: "goat_scan",
  goat_lookup: "goat_scan",
  shed_picker: "shed_picker",
  cohort_picker: "shed_picker",
  location_picker: "shed_picker",
  session_picker: "shed_picker",
  vaccine_batch_picker: "vaccine_batch_picker",
  medicine_picker: "medicine_picker",
  date_time: "text",
  photo_proof: "photo_proof",
  video_proof: "video_proof",
};

function stepTypeFromBackend(backend: string | null): StepTypeValue {
  return (backend && BACKEND_TO_STEP[backend]) || "text";
}

const OP_FROM_BACKEND: Record<string, ConditionOperator> = {
  not_empty: "answered",
  empty: "not_answered",
  equals: "equals",
  not_equals: "not_equals",
  in: "is_one_of",
  gt: "gt",
  gte: "gte",
  lt: "lt",
  lte: "lte",
};

function reverseId(prefix: string): string {
  return `${prefix}-${crypto.randomUUID()}`;
}

export function stepsFromFormDsl(formDsl: unknown): BuilderStep[] {
  const dsl = asObject(formDsl);
  const rawFields = dsl && Array.isArray(dsl["fields"]) ? (dsl["fields"] as unknown[]) : [];
  const keyToId = new Map<string, string>();
  const keyToStep = new Map<string, BuilderStep>();
  const steps: BuilderStep[] = [];

  rawFields.forEach((entry, i) => {
    const f = asObject(entry);
    if (!f) return;
    const key = asString(f["key"]) ?? `field_${i + 1}`;
    const id = reverseId("q");
    const options: BuilderOption[] = [];
    if (Array.isArray(f["options"])) {
      for (const raw of f["options"] as unknown[]) {
        const obj = asObject(raw);
        const label = obj ? asString(obj["label"]) ?? asString(obj["value"]) : asString(raw);
        if (label) options.push({ id: reverseId("opt"), label });
      }
    }
    const min = asNumber(f["min"]);
    const max = asNumber(f["max"]);
    const step: BuilderStep = {
      id,
      type: stepTypeFromBackend(asString(f["type"])),
      label: asString(f["label"]) ?? "",
      helpText: asString(f["help_text"]) ?? "",
      required: asBool(f["required"]),
      options,
      min: min !== null ? String(min) : "",
      max: max !== null ? String(max) : "",
      unit: asString(f["unit"]) ?? "",
      placeholder: asString(f["placeholder"]) ?? "",
      longText: asBool(f["long_text"]),
      visibleWhen: null,
    };
    keyToId.set(key, id);
    keyToStep.set(key, step);
    steps.push(step);
  });

  const rules = dsl && Array.isArray(dsl["rules"]) ? (dsl["rules"] as unknown[]) : [];
  for (const raw of rules) {
    const rule = asObject(raw);
    if (!rule) continue;
    const ruleType = asString(rule["type"]);
    const targetKey = asString(rule["field"]);
    const when = asObject(rule["when"]);
    if (!targetKey || !when) continue;
    const target = keyToStep.get(targetKey);
    if (!target) continue;
    if (ruleType === "required_if") {
      target.required = true;
      continue;
    }
    if (ruleType !== "visible_if") continue;
    const whenField = asString(when["field"]);
    const operator = asString(when["operator"]);
    const refId = whenField ? keyToId.get(whenField) : undefined;
    if (!refId || !operator) continue;
    const rawValue = when["value"];
    const value = Array.isArray(rawValue)
      ? (rawValue as unknown[]).map((v) => String(v)).join(", ")
      : rawValue === undefined || rawValue === null
        ? ""
        : String(rawValue);
    target.visibleWhen = { refId, operator: OP_FROM_BACKEND[operator] ?? "answered", value };
  }

  return steps;
}

export type BuilderInitial = {
  code: string;
  name: string;
  trigger: SopTrigger;
  steps: BuilderStep[];
  proofRequired: boolean;
  proofType: ProofType;
  verifyBeforeApply: boolean;
  minCount: number;
  subjectScope: SubjectScope;
};

// builderInitialFromVersion reconstructs the complete builder state for EDIT from a persisted SOP
// version's real form_dsl + proof_policy — no lossy round-trip through the thin card view model.
export function builderInitialFromVersion(code: string, name: string, formDsl: unknown, proofPolicy: unknown): BuilderInitial {
  const policy = asObject(proofPolicy);
  const types = policy ? proofPolicyTypes(policy) : [];
  const proofType: ProofType = types.includes("video") || !types.includes("photo") ? "video" : "photo";
  const scope = policy ? proofPolicySubjectScope(policy) : null;
  return {
    code,
    name,
    trigger: deriveTrigger(formDsl) ?? "cron",
    steps: stepsFromFormDsl(formDsl),
    proofRequired: policy ? asBool(policy["required"]) : false,
    proofType,
    verifyBeforeApply: policy ? asBool(policy["verify_before_apply"]) : false,
    minCount: (policy ? proofPolicyMinimumCount(policy) : null) ?? 1,
    subjectScope: scope === "goat" ? "goat" : "batch",
  };
}

// Field types this builder round-trips WITHOUT loss (BACKEND_TO_STEP is identity for these). The others
// (date_time, rfid_scan, goat_lookup, cohort_picker, location_picker, session_picker) get remapped, so a
// re-save would mutate the operator form.
const FAITHFUL_BACKEND_TYPES = new Set<string>([
  "text",
  "number",
  "boolean",
  "select",
  "multiselect",
  "goat_scan",
  "shed_picker",
  "vaccine_batch_picker",
  "medicine_picker",
  "photo_proof",
  "video_proof",
]);

// isVersionFaithfullyEditable returns false when a persisted version contains anything the builder
// cannot round-trip: a remapped field type, a rule type other than visible_if/required_if, or a
// composite (all/any) condition. Editing such a SOP here would silently drop rules or change field
// types on re-save, so the builder blocks save + publish with a visible reason instead.
export function isVersionFaithfullyEditable(formDsl: unknown): boolean {
  const dsl = asObject(formDsl);
  if (!dsl) return true;
  const fields = Array.isArray(dsl["fields"]) ? (dsl["fields"] as unknown[]) : [];
  for (const raw of fields) {
    const f = asObject(raw);
    const type = f ? asString(f["type"]) : null;
    if (type && !FAITHFUL_BACKEND_TYPES.has(type)) return false;
  }
  const rules = Array.isArray(dsl["rules"]) ? (dsl["rules"] as unknown[]) : [];
  for (const raw of rules) {
    const r = asObject(raw);
    if (!r) continue;
    const ruleType = asString(r["type"]);
    if (ruleType !== "visible_if" && ruleType !== "required_if") return false;
    const when = asObject(r["when"]);
    if (when && (when["all"] !== undefined || when["any"] !== undefined)) return false;
    if (when && !asString(when["field"])) return false;
  }
  return true;
}
