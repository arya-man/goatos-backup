export type SOPFieldType =
  | "text"
  | "number"
  | "date_time"
  | "select"
  | "multiselect"
  | "goat_lookup"
  | "rfid_scan"
  | "location_picker"
  | "photo_proof"
  | "video_proof";

export type SOPField = {
  key: string;
  label: string;
  type: SOPFieldType;
  required?: boolean;
  repeat?: boolean;
  option_source?: string;
  options?: string[];
  proof_action?: string;
};

export type SOPFormDSL = {
  schema_version: "goatos.sop-form.v1";
  sop_code: string;
  title: string;
  repeat_for_each_goat?: boolean;
  fields: SOPField[];
  rules?: SOPRule[];
  workflow?: {
    nodes: Array<{ key: string; label: string; type: string }>;
    edges: Array<{ from: string; to: string; condition?: string }>;
  };
};

export type SOPRule = {
  type: "visible_if" | "required_if" | "block_submission_if" | "proof_required_if" | "validation_rule";
  field?: string;
  message?: string;
  when?: {
    field: string;
    operator: "equals" | "not_equals" | "empty" | "not_empty" | "in";
    value?: unknown;
  };
};

export type ProofReference = {
  proof_id: string;
  proof_type: "photo" | "video" | "attachment";
  subject_type: "batch" | "goat" | "shed" | "task" | "other";
  subject_id?: string | null;
  upload_state: "pending" | "uploading" | "completed" | "failed";
  metadata: Record<string, unknown>;
};

export type EvaluationResult = {
  valid: boolean;
  errors: Array<{ field: string; code: string; message: string }>;
  field_states: Array<{ key: string; visible: boolean; required: boolean; blocked: boolean; message?: string }>;
  workflow_path: string[];
  final_state: "accepted" | "needs_review" | "blocked";
};

export function evaluateForm(
  dsl: SOPFormDSL,
  answers: Record<string, unknown>,
  proofRefs: ProofReference[],
  proofRequired: boolean,
  verifyBeforeApply: boolean,
): EvaluationResult {
  const errors: EvaluationResult["errors"] = [];
  const fieldStates = dsl.fields.map((field) => {
    const required = Boolean(field.required);
    const missing = required && isEmpty(answers[field.key]);
    if (missing) {
      errors.push({ field: field.key, code: "required", message: `${field.label} is required.` });
    }
    const state: EvaluationResult["field_states"][number] = {
      key: field.key,
      visible: true,
      required,
      blocked: missing,
    };
    if (missing) {
      state.message = "Required answer missing.";
    }
    return state;
  });

  if (proofRequired && !proofRefs.some((proof) => proof.proof_id && proof.upload_state === "completed")) {
    errors.push({ field: "proof_refs", code: "proof_required", message: "Completed proof is required." });
  }

  const workflowPath = ["operator_submission"];
  let finalState: EvaluationResult["final_state"] = "accepted";
  if (errors.length > 0) {
    finalState = "blocked";
  } else if (verifyBeforeApply) {
    workflowPath.push("proof_verification");
    finalState = "needs_review";
  }

  return {
    valid: errors.length === 0,
    errors,
    field_states: fieldStates,
    workflow_path: workflowPath,
    final_state: finalState,
  };
}

function isEmpty(value: unknown): boolean {
  if (value === null || value === undefined) return true;
  if (typeof value === "string") return value.trim() === "";
  if (Array.isArray(value)) return value.length === 0;
  return false;
}
