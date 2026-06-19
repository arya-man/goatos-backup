// Builder-model types for the interactive SOP Builder.
//
// These are the *editor* shapes the UI mutates. They are mapped to/from the
// backend `goatos.sop-form.v1` form DSL by `dsl.ts`. The vocabulary here is
// deliberately restricted to what the backend validates (see service.go /
// contracts/jsonschema/sop-form-version.schema.json) so everything built can
// actually be created, dry-run, and published.

export const FIELD_TYPES = [
  "text",
  "number",
  "date_time",
  "select",
  "multiselect",
  "goat_lookup",
  "rfid_scan",
  "location_picker",
  "photo_proof",
  "video_proof",
] as const;
export type FieldType = (typeof FIELD_TYPES)[number];

export const RULE_TYPES = [
  "visible_if",
  "required_if",
  "block_submission_if",
  "proof_required_if",
  "validation_rule",
] as const;
export type RuleType = (typeof RULE_TYPES)[number];

export const RULE_OPERATORS = ["equals", "not_equals", "in", "empty", "not_empty"] as const;
export type RuleOperator = (typeof RULE_OPERATORS)[number];

export const NODE_TYPES = [
  "operator_execution",
  "proof_verification",
  "approval",
  "accepted",
  "rework",
  "rejected",
  "blocked",
] as const;
export type NodeType = (typeof NODE_TYPES)[number];

export interface SopMeta {
  title: string;
  subtitle: string;
  code: string;
  domain: string;
  trigger: string;
  runner: string;
  submitLabel: string;
  typeLabel: string;
  repeatForEachGoat: boolean;
}

export interface BuilderField {
  key: string;
  label: string;
  type: FieldType;
  required: boolean;
  description: string;
  placeholder: string;
  defaultValue: string;
  /** comma-separated choices for select/multiselect */
  options: string;
  /** backend dynamic option source, e.g. "locations.active" */
  optionSource?: string;
  /** human note shown as a badge, e.g. "repeat for each" */
  note?: string;
  /** human show/require condition hint (display only) */
  cond?: string;
  custom?: boolean;
}

export interface BuilderRule {
  id: string;
  /** backend rule type */
  type: RuleType;
  /** field the condition reads */
  whenField: string;
  operator: RuleOperator;
  value: string;
  /** field the action targets (required_if / visible_if / proof_required_if) */
  targetField: string;
  message: string;
  /** active toggle — only enabled rules are persisted */
  on: boolean;
  custom?: boolean;
}

export interface BuilderNode {
  id: string;
  label: string;
  /** backend node type */
  type: NodeType;
  /** human role label shown under the node */
  role: string;
  x: number;
  y: number;
}

/** [from, to, label, about] — about is a human explanation of the handoff */
export type BuilderLink = [string, string, string, string];

export interface BuilderState {
  meta: SopMeta;
  fields: BuilderField[];
  rules: BuilderRule[];
  nodes: BuilderNode[];
  links: BuilderLink[];
  flowPattern: string;
  activeTemplate: string | null;
  selectedFieldIndex: number | null;
  draftField: BuilderField | null;
  selectedNodeId: string | null;
  selectedLinkIndex: number | null;
  activeTab: BuilderTab;
  dirty: boolean;
}

export type BuilderTab = "catalog" | "form" | "rules" | "flow";

export interface ServerVersionFacts {
  sopId: string | null;
  sopVersionId: string | null;
  versionLabel: string | null;
  status: string | null;
  rowVersion: number | null;
  validationValid: boolean | null;
}
