// Pure derivation + DSL-builder helpers for the SOP Library. No React, no server — unit-friendly.
//
// The ONLY UI/UX source of truth is the mock (mock/goatos-dashboard-mock.html · SOP Library / New SOP).
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

export type DslField = { key: string; label: string; type: string; required: boolean };

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
  fields: Array<{ label: string; type: string; required: boolean }>;
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
    fields: version ? deriveFields(version.form_dsl).map((f) => ({ label: f.label, type: f.type, required: f.required })) : [],
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

export type BuilderStep = {
  id: string;
  type: StepTypeValue;
  label: string;
  showIf: number; // -1 = always; otherwise 0-based index of the step it depends on
  onAnswer: OnAnswerAction;
};

export type OnAnswerAction = "none" | "require_if" | "require_proof" | "block_if_empty";

export const ON_ANSWER_ACTIONS: Array<{ value: OnAnswerAction; label: string }> = [
  { value: "none", label: "— no rule —" },
  { value: "require_if", label: "require this if previous answered" },
  { value: "require_proof", label: "require proof if answered" },
  { value: "block_if_empty", label: "block submission if empty" },
];

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

type EmittedField = {
  key: string;
  label: string;
  type: StepTypeDef["backend"];
  required?: boolean;
  option_source?: string;
  repeat?: boolean;
};

type EmittedRule = {
  type: "visible_if" | "required_if" | "proof_required_if" | "block_submission_if";
  field: string;
  message?: string;
  when: { field: string; operator: "not_empty" | "empty"; value?: unknown };
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

// buildFormDsl emits a valid goatos.sop-form.v1 form_dsl. Every field.type is a NATIVE backend-supported
// type, so the version always passes service.ValidateFormDSL; pickers carry option_source metadata.
export function buildFormDsl(input: SopBuilderInput): EmittedFormDsl {
  const keys = fieldKeys(input.steps);
  const fields: EmittedField[] = input.steps.map((step, i) => {
    const def = stepTypeDef(step.type);
    const field: EmittedField = { key: keys[i], label: step.label.trim() || `Step ${i + 1}`, type: def.backend };
    // Proof fields are required when proof is required so the proof gate has a target field.
    if (def.proof && input.proofRequired) field.required = true;
    if (step.onAnswer === "require_if") field.required = true;
    if (def.optionSource) field.option_source = def.optionSource;
    if (input.subjectScope === "goat") field.repeat = true;
    return field;
  });

  const rules: EmittedRule[] = [];
  input.steps.forEach((step, i) => {
    const key = keys[i];
    if (step.showIf >= 0 && step.showIf < i) {
      rules.push({ type: "visible_if", field: key, when: { field: keys[step.showIf], operator: "not_empty" } });
    }
    if (step.onAnswer === "require_proof") {
      rules.push({ type: "proof_required_if", field: key, when: { field: key, operator: "not_empty" } });
    } else if (step.onAnswer === "block_if_empty") {
      rules.push({ type: "block_submission_if", field: key, message: `${step.label || "Step"} must be answered`, when: { field: key, operator: "empty" } });
    } else if (step.onAnswer === "require_if" && step.showIf >= 0 && step.showIf < i) {
      rules.push({ type: "required_if", field: key, when: { field: keys[step.showIf], operator: "not_empty" } });
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

// hasProofField mirrors backend hasProofField: proof_policy.required needs a photo_proof/video_proof
// field present or the version fails validation. The modal blocks save with this exact reason.
export function hasProofField(input: SopBuilderInput): boolean {
  return input.steps.some((s) => isProofStepType(s.type));
}
