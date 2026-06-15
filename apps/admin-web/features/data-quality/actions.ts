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
  createCorrectionRequest,
  rejectIdentityCandidate,
  resolveCorrectionRequest,
  resolveIdentityConflict,
  type CorrectionRequestState,
  type CreateCorrectionRequestBody,
  type IdentifierType,
  type ResolveCorrectionRequestBody,
  type ResolveConflictRequestBody,
  type ReviewCandidateRequestBody,
} from "@/lib/api/server";

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
