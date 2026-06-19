// Pure, graph-driven scenario engine for the live Android preview. Generic
// across all SOP templates: it lights the actual task-flow graph based on the
// fields, enabled rules, and a few scenario toggles.

import type { BuilderState } from "./model";
import { hasProofField } from "./dsl";

export interface Scenario {
  proofCaptured: boolean;
  supervisor: "pending" | "approved" | "rejected";
  verifier: "pending" | "approved" | "rework";
  triggerBlock: boolean;
}

export const DEFAULT_SCENARIO: Scenario = {
  proofCaptured: false,
  supervisor: "pending",
  verifier: "approved",
  triggerBlock: false,
};

export interface Outcome {
  state: string;
  tone: "green" | "amber" | "red";
  canSubmit: boolean;
  hint: string;
  lit: Set<string>;
  kv: string;
}

export interface FlowFacts {
  hasApproval: boolean;
  hasProofVerification: boolean;
  hasProofField: boolean;
  hasBlockRule: boolean;
}

export function flowFacts(state: BuilderState): FlowFacts {
  return {
    hasApproval: state.nodes.some((n) => n.type === "approval"),
    hasProofVerification: state.nodes.some((n) => n.type === "proof_verification"),
    hasProofField: hasProofField(state.fields),
    hasBlockRule: state.rules.some((r) => r.on && r.type === "block_submission_if"),
  };
}

export function computeOutcome(state: BuilderState, scenario: Scenario): Outcome {
  const lit = new Set<string>();
  const byType = (type: string) => state.nodes.filter((n) => n.type === type).map((n) => n.id);
  const first = (type: string) => byType(type)[0];
  const start = state.nodes[0]?.id;
  if (start) lit.add(start);

  const facts = flowFacts(state);
  const approveId = first("approval");
  const proofId = first("proof_verification");
  const acceptId = byType("accepted")[0];
  const reworkId = byType("rework")[0];
  const blockedId = byType("blocked")[0] ?? byType("rejected")[0];
  const operatorIds = byType("operator_execution");

  let state_ = "ACCEPTED";
  let tone: Outcome["tone"] = "green";
  let canSubmit = true;
  let hint = "Backend re-validates · domain event written.";

  if (scenario.triggerBlock && facts.hasBlockRule) {
    state_ = "BLOCKED → REVIEW QUEUE";
    tone = "red";
    canSubmit = false;
    if (blockedId) lit.add(blockedId);
    hint = "A block_submission rule prevents a silent write — task goes to review.";
  } else {
    if (approveId) {
      lit.add(approveId);
      if (scenario.supervisor === "pending") {
        state_ = "NEEDS APPROVAL";
        tone = "amber";
        canSubmit = false;
        hint = "Request waits for supervisor authorization.";
      } else if (scenario.supervisor === "rejected") {
        state_ = "REQUEST REJECTED";
        tone = "red";
        canSubmit = false;
        if (blockedId) lit.add(blockedId);
        hint = "Supervisor rejected the request.";
      }
    }

    if (canSubmit) {
      operatorIds.forEach((id) => lit.add(id));
      if (facts.hasProofField && !scenario.proofCaptured) {
        state_ = "CANNOT SUBMIT";
        tone = "amber";
        canSubmit = false;
        if (proofId) lit.add(proofId);
        hint = "Proof is required before submit.";
      } else {
        if (proofId) lit.add(proofId);
        if (facts.hasProofVerification) {
          if (scenario.verifier === "pending") {
            state_ = "NEEDS VERIFICATION";
            tone = "amber";
            hint = "Submitted — awaiting proof verification.";
          } else if (scenario.verifier === "rework") {
            state_ = "REWORK REQUESTED";
            tone = "red";
            if (reworkId) lit.add(reworkId);
            hint = "Verifier rejected proof — rework task created, original kept.";
          } else if (acceptId) {
            lit.add(acceptId);
            state_ = "ACCEPTED → EVENT";
          }
        } else if (acceptId) {
          lit.add(acceptId);
          state_ = "ACCEPTED → EVENT";
        }
      }
    }
  }

  const kv = `Proof: ${facts.hasProofField ? (scenario.proofCaptured ? "captured" : "required") : "not required"} · Auth: ${
    facts.hasApproval ? scenario.supervisor : "pre-authorized"
  } · Review: ${facts.hasProofVerification ? scenario.verifier : "n/a"}`;

  return { state: state_, tone, canSubmit, hint, lit, kv };
}
