"use server";

import {
  actionErrorMessage,
  actionRedirect,
  optionalBoolean,
  optionalString,
  requiredEvidenceRef,
  requiredNumber,
  requiredString,
} from "@/lib/action-helpers";
import {
  addGoatIdentifier,
  retireGoatIdentifier,
  type AddIdentifierRequestBody,
  type IdentifierType,
  type RetireIdentifierRequestBody,
} from "@/lib/api/server";

const identifierTypes: IdentifierType[] = ["old_tag", "rfid", "visual_tag", "sheet_row_id", "purchase_load_id", "temp_field_id", "external_system_id"];

export async function addIdentifierAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const identifierType = requiredString(formData, "identifier_type");
    if (!identifierTypes.includes(identifierType as IdentifierType)) {
      throw new Error("identifier_type is not supported");
    }
    const body: AddIdentifierRequestBody = {
      identifier_type: identifierType as IdentifierType,
      identifier_value: requiredString(formData, "identifier_value"),
      scope_key: requiredString(formData, "scope_key"),
      is_primary_for_goat: optionalBoolean(formData, "is_primary_for_goat"),
      evidence_refs: requiredEvidenceRef(formData),
      row_version: requiredNumber(formData, "row_version"),
    };
    const result = await addGoatIdentifier(requiredString(formData, "goat_id"), body, requiredString(formData, "idempotency_key"));
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `Identifier added for ${result.data.goat.display_id}.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to add identifier.";
  }
  actionRedirect(formData, status, message);
}

export async function retireIdentifierAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const body: RetireIdentifierRequestBody = {
      reason: optionalString(formData, "reason") ?? "Retired from admin passport review.",
      evidence_refs: requiredEvidenceRef(formData),
      row_version: requiredNumber(formData, "row_version"),
    };
    const result = await retireGoatIdentifier(
      requiredString(formData, "goat_id"),
      requiredString(formData, "identifier_id"),
      body,
      requiredString(formData, "idempotency_key"),
    );
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `Identifier retired for ${result.data.goat.display_id}.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to retire identifier.";
  }
  actionRedirect(formData, status, message);
}
