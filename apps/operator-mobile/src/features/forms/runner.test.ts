import assert from "node:assert/strict";
import { test } from "node:test";

import { buildSOPRunner } from "./runner.js";
import type { SOPVersionResponse } from "../../shared/api/client.js";

const version: SOPVersionResponse["version"] = {
  sop_version_id: "00000000-0000-4000-8000-000000000101",
  tenant_id: "00000000-0000-4000-8000-000000000001",
  sop_id: "00000000-0000-4000-8000-000000000100",
  sop_code: "SHIFTING",
  version: 1,
  version_label: "v1",
  status: "published",
  form_dsl: {
    schema_version: "goatos.sop-form.v1",
    sop_code: "SHIFTING",
    title: "Shifting",
    fields: [
      { key: "goat_id", label: "Goat", type: "goat_lookup", required: true },
      { key: "video", label: "Proof video", type: "video_proof", required: false, proof_action: "video_required" },
    ],
    workflow: {
      nodes: [
        { key: "operator_submission", label: "Operator submits", type: "operator" },
        { key: "proof_verification", label: "Verifier reviews", type: "verifier" },
      ],
      edges: [{ from: "operator_submission", to: "proof_verification" }],
    },
  },
  proof_policy: {
    required: true,
    verify_before_apply: true,
    accepted_types: ["video"],
  },
  compatibility: {},
  validation_report: {
    valid: true,
    errors: [],
    warnings: [],
  },
  row_version: 1,
  created_at: "2026-06-19T00:00:00Z",
  updated_at: "2026-06-19T00:00:00Z",
};

test("buildSOPRunner blocks missing required answers and proof", () => {
  const model = buildSOPRunner(version, {}, []);

  assert.equal(model.canSubmit, false);
  assert.equal(model.finalState, "blocked");
  assert.deepEqual(
    model.errors.map((error) => error.code),
    ["required", "proof_required"],
  );
  assert.equal(model.fields.find((field) => field.key === "goat_id")?.blocked, true);
});

test("buildSOPRunner maps field components and review workflow", () => {
  const model = buildSOPRunner(
    version,
    { goat_id: "00000000-0000-4000-8000-000000000201" },
    [
      {
        proof_id: "proof-1",
        proof_type: "video",
        subject_type: "goat",
        subject_id: "00000000-0000-4000-8000-000000000201",
        upload_state: "completed",
        metadata: {},
      },
    ],
  );

  assert.equal(model.canSubmit, true);
  assert.equal(model.finalState, "needs_review");
  assert.deepEqual(model.workflowPath, ["operator_submission", "proof_verification"]);
  assert.equal(model.fields.find((field) => field.key === "goat_id")?.component, "GoatLookup");
  assert.equal(model.fields.find((field) => field.key === "video")?.component, "VideoProof");
});
