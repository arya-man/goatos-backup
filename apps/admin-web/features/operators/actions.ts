"use server";

import { actionErrorMessage, actionRedirect, requiredNumber, requiredString } from "@/lib/action-helpers";
import {
  activateOperator,
  deactivateOperator,
  mapOperatorSourceCandidate,
  rejectOperatorSourceCandidate,
  type MapSourceCandidateRequest,
  type RejectSourceCandidateRequest,
  type StatusChangeRequest,
} from "@/lib/api/server";

export async function activateOperatorAction(formData: FormData) {
  await statusAction(formData, "activate");
}

export async function deactivateOperatorAction(formData: FormData) {
  await statusAction(formData, "deactivate");
}

export async function mapSourceCandidateAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const body: MapSourceCandidateRequest = {
      operator_id: requiredString(formData, "operator_id"),
      reason: stringField(formData, "reason") ?? "Mapped in Operator Management.",
      row_version: requiredNumber(formData, "row_version"),
    };
    const result = await mapOperatorSourceCandidate(requiredString(formData, "candidate_id"), body);
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `Source candidate ${result.data.candidate.candidate_id.slice(0, 8)} mapped.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to map source candidate.";
  }
  actionRedirect(formData, status, message);
}

export async function rejectSourceCandidateAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const body: RejectSourceCandidateRequest = {
      reason: stringField(formData, "reason") ?? "Rejected in Operator Management.",
      row_version: requiredNumber(formData, "row_version"),
    };
    const result = await rejectOperatorSourceCandidate(requiredString(formData, "candidate_id"), body);
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `Source candidate ${result.data.candidate.candidate_id.slice(0, 8)} rejected.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to reject source candidate.";
  }
  actionRedirect(formData, status, message);
}

async function statusAction(formData: FormData, target: "activate" | "deactivate") {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const body: StatusChangeRequest = {
      reason: stringField(formData, "reason") ?? `Operator ${target}.`,
      row_version: requiredNumber(formData, "row_version"),
    };
    const operatorId = requiredString(formData, "operator_id");
    const result = target === "activate" ? await activateOperator(operatorId, body) : await deactivateOperator(operatorId, body);
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `${result.data.operator.display_name} is ${result.data.operator.status}.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : `Unable to ${target} operator.`;
  }
  actionRedirect(formData, status, message);
}

function stringField(formData: FormData, key: string): string | undefined {
  const value = formData.get(key);
  if (typeof value !== "string") return undefined;
  const trimmed = value.trim();
  return trimmed === "" ? undefined : trimmed;
}
