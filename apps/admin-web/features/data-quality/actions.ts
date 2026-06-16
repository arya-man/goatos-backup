"use server";

import {
  actionErrorMessage,
  actionRedirect,
  affectedGoatIDs,
  optionalString,
  requiredEvidenceRef,
  requiredNumber,
  requiredString,
} from "@/lib/action-helpers";
import {
  approveIdentityCandidate,
  bulkResolveConflicts,
  createCorrectionRequest,
  rejectIdentityCandidate,
  resolveCorrectionRequest,
  resolveIdentityConflict,
  type ApproveCandidateRequestBody,
  type BulkResolveConflictItem,
  type BulkResolveConflictsRequest,
  type CorrectionRequestState,
  type CreateCorrectionRequestBody,
  type IdentifierType,
  type ResolveCorrectionRequestBody,
  type ResolveConflictRequestBody,
  type ReviewCandidateRequestBody,
} from "@/lib/api/server";
import { bulkDecisionTypes, type BulkDecisionType } from "./review-groups";

const correctionStates: CorrectionRequestState[] = ["approved", "rejected", "needs_field_check", "closed"];
const identifierTypes: IdentifierType[] = ["old_tag", "rfid", "visual_tag", "sheet_row_id", "purchase_load_id", "temp_field_id", "external_system_id"];
const conflictActions = ["merge", "reject", "field_check", "dispute_identifier"] as const;
type ConflictAction = (typeof conflictActions)[number];

export async function createCorrectionRequestAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const identifierType = optionalString(formData, "identifier_type");
    if (identifierType && !identifierTypes.includes(identifierType as IdentifierType)) {
      throw new Error("identifier_type is not supported");
    }
    const body: CreateCorrectionRequestBody = {
      request_type: requiredString(formData, "request_type") as CreateCorrectionRequestBody["request_type"],
      goat_id: optionalString(formData, "goat_id") ?? null,
      identifier_type: identifierType ? (identifierType as IdentifierType) : null,
      identifier_value: optionalString(formData, "identifier_value") ?? null,
      location_scope: {},
      description: requiredString(formData, "description"),
      evidence_refs: requiredEvidenceRef(formData),
    };
    const result = await createCorrectionRequest(body, requiredString(formData, "idempotency_key"));
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `Correction request ${result.data.correction_request.correction_request_id} created.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to create correction request.";
  }
  actionRedirect(formData, status, message);
}

export async function rejectCandidateAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const body: ReviewCandidateRequestBody = {
      reason: requiredString(formData, "reason"),
      evidence_refs: requiredEvidenceRef(formData),
      row_version: requiredNumber(formData, "row_version"),
    };
    const result = await rejectIdentityCandidate(requiredString(formData, "candidate_id"), body, requiredString(formData, "idempotency_key"));
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `Candidate ${result.data.candidate_id} moved to ${result.data.state}.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to reject candidate.";
  }
  actionRedirect(formData, status, message);
}

export async function approveCandidateAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    // Candidate approve in the review queue confirms the two records are the same
    // goat and merges them (survivor keeps the canonical passport). Attach-identifier
    // approve is driven from the goat passport, where the goat row_version is known.
    const survivorGoatId = requiredString(formData, "survivor_goat_id");
    const body: ApproveCandidateRequestBody = {
      decision_type: "merge_goats",
      survivor_goat_id: survivorGoatId,
      affected_goat_ids: affectedGoatIDs(formData),
      reason: requiredString(formData, "reason"),
      evidence_refs: requiredEvidenceRef(formData),
      row_version: requiredNumber(formData, "row_version"),
    };
    const result = await approveIdentityCandidate(requiredString(formData, "candidate_id"), body, requiredString(formData, "idempotency_key"));
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `Candidate ${result.data.candidate_id} approved; goats merged into ${survivorGoatId}.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to approve candidate.";
  }
  actionRedirect(formData, status, message);
}

export async function resolveCorrectionRequestAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const state = requiredString(formData, "state");
    if (!correctionStates.includes(state as CorrectionRequestState)) {
      throw new Error("state is not supported");
    }
    const body: ResolveCorrectionRequestBody = {
      state: state as ResolveCorrectionRequestBody["state"],
      reason: requiredString(formData, "reason"),
      evidence_refs: requiredEvidenceRef(formData),
      row_version: requiredNumber(formData, "row_version"),
    };
    const result = await resolveCorrectionRequest(requiredString(formData, "correction_request_id"), body, requiredString(formData, "idempotency_key"));
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `Correction request ${result.data.correction_request.correction_request_id} moved to ${result.data.correction_request.state}.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to resolve correction request.";
  }
  actionRedirect(formData, status, message);
}

export async function resolveConflictAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const action = requiredConflictAction(formData);
    const common = {
      affected_goat_ids: affectedGoatIDs(formData),
      evidence_refs: requiredEvidenceRef(formData),
      reason: requiredString(formData, "reason"),
      row_version: requiredNumber(formData, "row_version"),
    };
    let body: ResolveConflictRequestBody;
    switch (action) {
      case "merge":
        body = {
          decision_type: "merge_goats",
          decision_result: "same_goat_merge",
          survivor_goat_id: requiredString(formData, "survivor_goat_id"),
          identifier_actions: [],
          ...common,
        };
        break;
      case "reject":
        body = {
          decision_type: "reject_match",
          decision_result: "candidate_rejected",
          survivor_goat_id: null,
          identifier_actions: [],
          ...common,
        };
        break;
      case "field_check":
        body = {
          decision_type: "request_field_verification",
          decision_result: "field_verification_required",
          survivor_goat_id: null,
          identifier_actions: [],
          ...common,
        };
        break;
      case "dispute_identifier":
        body = {
          decision_type: "mark_identifier_disputed",
          decision_result: "different_goats_identifier_disputed",
          survivor_goat_id: null,
          identifier_actions: disputedIdentifierActions(formData),
          ...common,
        };
        break;
    }
    const result = await resolveIdentityConflict(requiredString(formData, "conflict_id"), body, requiredString(formData, "idempotency_key"));
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `Conflict ${result.data.conflict_id} moved to ${result.data.state}.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to resolve conflict.";
  }
  actionRedirect(formData, status, message);
}

export async function bulkResolveConflictsAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const decisionType = requiredString(formData, "decision_type");
    if (!bulkDecisionTypes.includes(decisionType as BulkDecisionType)) {
      throw new Error("decision_type is not supported");
    }
    const conflicts = bulkSelectedConflicts(formData);
    if (conflicts.length === 0) {
      throw new Error("Select at least one conflict to resolve.");
    }
    const body: BulkResolveConflictsRequest = {
      decision_type: decisionType as BulkResolveConflictsRequest["decision_type"],
      conflicts,
      reason: requiredString(formData, "reason"),
    };
    const result = await bulkResolveConflicts(body);
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      const data = result.data;
      const parts = [`Resolved ${data.resolved_conflict_ids.length} conflict${data.resolved_conflict_ids.length === 1 ? "" : "s"}.`];
      if (data.goats_mutated > 0) parts.push(`${data.goats_mutated} goat${data.goats_mutated === 1 ? "" : "s"} updated.`);
      if (data.goats_returned_clean > 0) parts.push(`${data.goats_returned_clean} returned to clean.`);
      if (data.counters_rebuild_required) parts.push("Counter rebuild required.");
      message = parts.join(" ");
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to bulk-resolve conflicts.";
  }
  actionRedirect(formData, status, message);
}

// bulkSelectedConflicts parses the "conflict_id:row_version" pairs submitted by
// the bulk-review panel, de-duplicating by conflict_id.
function bulkSelectedConflicts(formData: FormData): BulkResolveConflictItem[] {
  const seen = new Set<string>();
  const items: BulkResolveConflictItem[] = [];
  for (const raw of formData.getAll("selected_conflict")) {
    if (typeof raw !== "string") continue;
    const separator = raw.lastIndexOf(":");
    if (separator <= 0) continue;
    const conflictId = raw.slice(0, separator).trim();
    const rowVersion = Number.parseInt(raw.slice(separator + 1), 10);
    if (!conflictId || seen.has(conflictId)) continue;
    if (!Number.isFinite(rowVersion) || rowVersion < 1) {
      throw new Error("A selected conflict had an invalid row version; reload and retry.");
    }
    seen.add(conflictId);
    items.push({ conflict_id: conflictId, row_version: rowVersion });
  }
  return items;
}

function requiredConflictAction(formData: FormData): ConflictAction {
  const action = requiredString(formData, "conflict_action");
  if (!conflictActions.includes(action as ConflictAction)) {
    throw new Error("conflict_action is not supported");
  }
  return action as ConflictAction;
}

function disputedIdentifierActions(formData: FormData): NonNullable<ResolveConflictRequestBody["identifier_actions"]> {
  const ids = formData
    .getAll("identifier_action_id")
    .filter((value): value is string => typeof value === "string")
    .map((value) => value.trim())
    .filter(Boolean);
  if (ids.length === 0) {
    throw new Error("Select at least one identifier to dispute.");
  }
  return ids.map((identifier_id) => ({
    action: "dispute",
    identifier_id,
  }));
}
