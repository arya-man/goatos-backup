import { buildRunnerModel } from "../../../../../packages/mobile-forms-runner/src/index.js";
import type { ProofReference, SOPFormDSL } from "../../../../../packages/forms-dsl/src/index.js";
import type { SOPVersionResponse } from "../../shared/api/client.js";

export function buildSOPRunner(version: SOPVersionResponse["version"], answers: Record<string, unknown>, proofRefs: ProofReference[]) {
  return buildRunnerModel(version.form_dsl as SOPFormDSL, version.proof_policy as { required?: boolean; verify_before_apply?: boolean }, answers, proofRefs);
}
