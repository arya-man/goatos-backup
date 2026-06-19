// Instant client-side draft validation. Mirrors the backend's structural
// concerns (service.go / sop-form-version.schema.json) so the builder gives
// immediate feedback before a version is created. Authoritative validation
// still happens server-side on create + dry-run.

import type { BuilderState } from "./model";
import { hasProofField } from "./dsl";

export interface ValidationIssue {
  level: "error" | "warn";
  message: string;
}

export interface ValidationResult {
  valid: boolean;
  issues: ValidationIssue[];
}

const KEY_RE = /^[a-z][a-z0-9_]*$/;
const CODE_RE = /^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$/;

export function validateDraft(state: BuilderState): ValidationResult {
  const issues: ValidationIssue[] = [];
  const err = (message: string) => issues.push({ level: "error", message });
  const warn = (message: string) => issues.push({ level: "warn", message });

  if (!state.meta.title.trim()) err("SOP title is required.");
  if (!CODE_RE.test(state.meta.code.trim())) err(`SOP code "${state.meta.code}" is invalid (use dotted lowercase, e.g. movement.shifting).`);

  const fieldKeys = new Set<string>();
  if (state.fields.length === 0) err("Add at least one operator form field.");
  for (const field of state.fields) {
    if (!KEY_RE.test(field.key)) err(`Field key "${field.key}" is invalid (lowercase, start with a letter).`);
    if (fieldKeys.has(field.key)) err(`Duplicate field key "${field.key}".`);
    fieldKeys.add(field.key);
    if ((field.type === "select" || field.type === "multiselect") && !field.options.trim()) {
      warn(`Field "${field.label || field.key}" is a ${field.type} with no options.`);
    }
  }

  const enabledRules = state.rules.filter((r) => r.on);
  let needsProof = false;
  for (const rule of enabledRules) {
    if (rule.whenField && !fieldKeys.has(rule.whenField)) err(`Rule references unknown field "${rule.whenField}".`);
    if (rule.targetField && !fieldKeys.has(rule.targetField)) err(`Rule targets unknown field "${rule.targetField}".`);
    if (rule.type === "proof_required_if") needsProof = true;
    if ((rule.operator === "equals" || rule.operator === "not_equals" || rule.operator === "in") && !rule.value.trim()) {
      warn(`Rule on "${rule.whenField || "field"}" has an empty comparison value.`);
    }
  }
  if (needsProof && !hasProofField(state.fields)) {
    err("A proof-required rule needs a photo or video proof field in the form.");
  }

  const nodeIds = new Set(state.nodes.map((n) => n.id));
  if (state.nodes.length === 0) err("Add at least one task-flow step.");
  if (state.nodes.length > 0 && !state.nodes.some((n) => n.type === "accepted")) {
    warn("Task flow has no terminal (accepted / domain event) step.");
  }
  for (const [from, to] of state.links) {
    if (!nodeIds.has(from) || !nodeIds.has(to)) err(`Handoff references a missing step (${from} → ${to}).`);
  }

  return { valid: issues.every((i) => i.level !== "error"), issues };
}
