// Mapping between the builder model and the backend `goatos.sop-form.v1` DSL.
// toFormDsl is used to create a version; fromFormDsl preloads an existing
// version for editing. Output conforms to
// contracts/jsonschema/sop-form-version.schema.json.

import { FIELD_TYPES, RULE_OPERATORS, type BuilderField, type BuilderLink, type BuilderNode, type BuilderRule, type BuilderState, type FieldType, type NodeType, type RuleOperator, type RuleType, type SopMeta } from "./model";
import { nodeTypeFromRole, slugify, titleCase } from "./templates";

const SCHEMA_VERSION = "goatos.sop-form.v1";

export interface FormDsl {
  schema_version: string;
  sop_code: string;
  title: string;
  repeat_for_each_goat: boolean;
  fields: Record<string, unknown>[];
  rules: Record<string, unknown>[];
  workflow: { nodes: Record<string, unknown>[]; edges: Record<string, unknown>[] };
}

export function normalizeCode(value: string, fallback = "ops.custom"): string {
  const segs = String(value || "")
    .trim()
    .toLowerCase()
    .split(".")
    .map((s) => slugify(s, ""))
    .filter(Boolean)
    .map((s) => (/^[a-z]/.test(s) ? s : `c_${s}`));
  return segs.length ? segs.join(".") : fallback;
}

function splitOptions(raw: string): string[] {
  return raw
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
}

function proofAction(type: FieldType): string | undefined {
  if (type === "photo_proof") return "photo.capture";
  if (type === "video_proof") return "video.capture";
  return undefined;
}

function fieldToDsl(field: BuilderField): Record<string, unknown> {
  const out: Record<string, unknown> = {
    key: slugify(field.key, "field"),
    label: field.label.trim() || titleCase(field.key),
    type: field.type,
    required: field.required,
  };
  if (field.type === "select" || field.type === "multiselect") {
    const opts = splitOptions(field.options);
    if (opts.length) out.options = opts;
  }
  if (field.optionSource) out.option_source = field.optionSource;
  if (field.note?.toLowerCase().includes("repeat")) out.repeat = true;
  const pa = proofAction(field.type);
  if (pa) out.proof_action = pa;
  return out;
}

function ruleNeedsValue(op: RuleOperator): boolean {
  return op === "equals" || op === "not_equals" || op === "in";
}

function ruleToDsl(rule: BuilderRule): Record<string, unknown> {
  const when: Record<string, unknown> = { field: rule.whenField, operator: rule.operator };
  if (ruleNeedsValue(rule.operator)) {
    when.value = rule.operator === "in" ? splitOptions(rule.value) : rule.value;
  }
  const out: Record<string, unknown> = { type: rule.type, when, message: rule.message };
  if (rule.targetField) out.field = rule.targetField;
  return out;
}

export function toFormDsl(state: BuilderState): FormDsl {
  return {
    schema_version: SCHEMA_VERSION,
    sop_code: normalizeCode(state.meta.code),
    title: state.meta.title.trim() || "Untitled SOP",
    repeat_for_each_goat: state.meta.repeatForEachGoat,
    fields: state.fields.map(fieldToDsl),
    rules: state.rules.filter((r) => r.on).map(ruleToDsl),
    workflow: {
      nodes: state.nodes.map((n) => ({ key: n.id, label: n.label, type: n.type })),
      edges: state.links.map(([from, to, label]) => ({ from, to, condition: slugify(label, "next") })),
    },
  };
}

export function hasProofField(fields: BuilderField[]): boolean {
  return fields.some((f) => f.type === "photo_proof" || f.type === "video_proof");
}

export function proofPolicy(fields: BuilderField[]): Record<string, unknown> {
  if (!hasProofField(fields)) {
    return { required: false, scope: "batch", types: [], minimum_count: 0, verify_before_apply: false };
  }
  const types: string[] = [];
  if (fields.some((f) => f.type === "video_proof")) types.push("video");
  if (fields.some((f) => f.type === "photo_proof")) types.push("photo");
  return { required: true, scope: "batch", types, minimum_count: 1, verify_before_apply: true };
}

export function compatibility(): Record<string, unknown> {
  return {
    min_app_version: "0.2.0",
    supported_field_types: [...FIELD_TYPES],
    supported_rule_operators: [...RULE_OPERATORS],
    supported_proof_actions: ["photo.capture", "video.capture"],
  };
}

// Sample submission for server dry-run after a version is created. Best-effort:
// fills required fields with representative values by type so the validator can
// exercise the rules.
export function sampleDryRun(state: BuilderState): { answers: Record<string, unknown>; proof_refs: unknown[]; context: Record<string, unknown> } {
  const answers: Record<string, unknown> = {};
  for (const field of state.fields) {
    answers[slugify(field.key, "field")] = sampleAnswer(field);
  }
  const proofRefs = hasProofField(state.fields)
    ? [{ proof_id: "proof-sample", proof_type: "video", subject_type: "batch", subject_id: null, upload_state: "completed", metadata: {} }]
    : [];
  return { answers, proof_refs: proofRefs, context: { scenario: "admin-preview" } };
}

function sampleAnswer(field: BuilderField): unknown {
  switch (field.type) {
    case "number":
      return 1;
    case "select":
    case "multiselect": {
      const opts = splitOptions(field.options);
      return field.type === "multiselect" ? (opts.length ? [opts[0]] : []) : opts[0] ?? "value";
    }
    case "goat_lookup":
      return ["66000000-0000-4000-8000-000000000001"];
    case "location_picker":
      return "67000000-0000-4000-8000-000000000001";
    case "photo_proof":
    case "video_proof":
      return "proof-sample";
    case "date_time":
      return "2026-06-19T00:00:00Z";
    default:
      return field.defaultValue || "sample";
  }
}

// ── reverse: preload an existing version into the builder ─────────────────────
type AnyRecord = Record<string, unknown>;

function asArray(value: unknown): AnyRecord[] {
  return Array.isArray(value) ? (value.filter((v) => v && typeof v === "object") as AnyRecord[]) : [];
}

function str(value: unknown, fallback = ""): string {
  return typeof value === "string" ? value : fallback;
}

export function fromFormDsl(formDsl: unknown, fallbackMeta: SopMeta): {
  meta: SopMeta;
  fields: BuilderField[];
  rules: BuilderRule[];
  nodes: BuilderNode[];
  links: BuilderLink[];
} {
  const dsl = (formDsl && typeof formDsl === "object" ? formDsl : {}) as AnyRecord;

  const meta: SopMeta = {
    ...fallbackMeta,
    title: str(dsl.title, fallbackMeta.title),
    code: str(dsl.sop_code, fallbackMeta.code),
    repeatForEachGoat: dsl.repeat_for_each_goat === true,
  };

  const fields: BuilderField[] = asArray(dsl.fields).map((raw) => {
    const type = (FIELD_TYPES as readonly string[]).includes(str(raw.type)) ? (str(raw.type) as FieldType) : "text";
    const options = Array.isArray(raw.options) ? (raw.options as unknown[]).map((o) => String(o)).join(", ") : "";
    return {
      key: str(raw.key, "field"),
      label: str(raw.label) || titleCase(str(raw.key, "field")),
      type,
      required: raw.required === true,
      description: "",
      placeholder: "",
      defaultValue: "",
      options,
      optionSource: str(raw.option_source) || undefined,
      note: raw.repeat === true ? "repeat for each" : undefined,
    };
  });

  const rules: BuilderRule[] = asArray(dsl.rules).map((raw, i) => {
    const when = (raw.when && typeof raw.when === "object" ? raw.when : {}) as AnyRecord;
    const operator = (RULE_OPERATORS as readonly string[]).includes(str(when.operator)) ? (str(when.operator) as RuleOperator) : "not_empty";
    const value = Array.isArray(when.value) ? (when.value as unknown[]).map((v) => String(v)).join(", ") : str(when.value);
    return {
      id: `loaded_${i}`,
      type: (str(raw.type, "validation_rule") as RuleType),
      whenField: str(when.field),
      operator,
      value,
      targetField: str(raw.field),
      message: str(raw.message),
      on: true,
    };
  });

  const workflow = (dsl.workflow && typeof dsl.workflow === "object" ? dsl.workflow : {}) as AnyRecord;
  const rawNodes = asArray(workflow.nodes);
  const nodes: BuilderNode[] = rawNodes.map((raw, i) => {
    const type = str(raw.type, "operator_execution") as NodeType;
    const label = str(raw.label) || titleCase(str(raw.key, "step"));
    return {
      id: str(raw.key, `node_${i}`),
      label,
      type,
      role: roleForType(type),
      x: 18 + (i % 3) * 240,
      y: 40 + Math.floor(i / 3) * 150,
    };
  });
  const links: BuilderLink[] = asArray(workflow.edges).map((raw) => {
    const from = str(raw.from);
    const to = str(raw.to);
    const label = str(raw.condition, "next");
    return [from, to, label, ""] as BuilderLink;
  });

  return { meta, fields, rules, nodes, links };
}

function roleForType(type: NodeType): string {
  switch (type) {
    case "approval":
      return "approval gate";
    case "proof_verification":
      return "verifier";
    case "accepted":
      return "domain event";
    case "rework":
      return "rework";
    case "blocked":
      return "review queue";
    case "rejected":
      return "rejected";
    default:
      return "operator";
  }
}

// keep nodeTypeFromRole referenced for symmetry/use by callers importing dsl
export { nodeTypeFromRole };
