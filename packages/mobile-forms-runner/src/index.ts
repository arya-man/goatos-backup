import { evaluateForm, type ProofReference, type SOPFormDSL } from "../../forms-dsl/src/index.js";

export type RunnerFieldModel = {
  key: string;
  label: string;
  component:
    | "TextInput"
    | "NumberInput"
    | "DateTimeInput"
    | "Select"
    | "MultiSelect"
    | "GoatLookup"
    | "AnimalIDScan"
    | "LocationPicker"
    | "PhotoProof"
    | "VideoProof";
  required: boolean;
  blocked: boolean;
  message?: string;
};

export type RunnerModel = {
  title: string;
  fields: RunnerFieldModel[];
  canSubmit: boolean;
  finalState: string;
  workflowPath: string[];
  errors: Array<{ field: string; code: string; message: string }>;
};

export function buildRunnerModel(
  dsl: SOPFormDSL,
  proofPolicy: { required?: boolean; verify_before_apply?: boolean },
  answers: Record<string, unknown>,
  proofRefs: ProofReference[],
): RunnerModel {
  const evaluation = evaluateForm(dsl, answers, proofRefs, Boolean(proofPolicy.required), Boolean(proofPolicy.verify_before_apply));
  return {
    title: dsl.title,
    fields: dsl.fields.map((field) => {
      const state = evaluation.field_states.find((item) => item.key === field.key);
      const model: RunnerFieldModel = {
        key: field.key,
        label: field.label,
        component: componentFor(field.type),
        required: Boolean(state?.required),
        blocked: Boolean(state?.blocked),
      };
      if (state?.message) {
        model.message = state.message;
      }
      return model;
    }),
    canSubmit: evaluation.valid,
    finalState: evaluation.final_state,
    workflowPath: evaluation.workflow_path,
    errors: evaluation.errors,
  };
}

function componentFor(type: SOPFormDSL["fields"][number]["type"]): RunnerFieldModel["component"] {
  switch (type) {
    case "number":
      return "NumberInput";
    case "date_time":
      return "DateTimeInput";
    case "select":
      return "Select";
    case "multiselect":
      return "MultiSelect";
    case "goat_lookup":
      return "GoatLookup";
    case "animal_id_scan":
      return "AnimalIDScan";
    case "location_picker":
      return "LocationPicker";
    case "photo_proof":
      return "PhotoProof";
    case "video_proof":
      return "VideoProof";
    default:
      return "TextInput";
  }
}
