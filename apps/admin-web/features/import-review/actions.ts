"use server";

import { actionErrorMessage, actionRedirect, optionalString, requiredEvidenceRef, requiredNumber, requiredString } from "@/lib/action-helpers";
import { reviewImportRunRow, type ReviewImportRowRequestBody } from "@/lib/api/server";

const reviewActions = ["reject", "fix", "reapply"] as const;
type ReviewAction = (typeof reviewActions)[number];

export async function reviewImportRowAction(formData: FormData) {
  let status: "success" | "error" = "success";
  let message = "";
  try {
    const action = requiredString(formData, "action");
    if (!reviewActions.includes(action as ReviewAction)) {
      throw new Error("action is not supported");
    }
    const body: ReviewImportRowRequestBody = {
      action: action as ReviewAction,
      row_version: requiredNumber(formData, "row_version"),
      reason: requiredString(formData, "reason"),
      evidence_refs: requiredEvidenceRef(formData),
    };
    if (action === "fix") {
      const sex = optionalString(formData, "sex");
      const breed = optionalString(formData, "breed");
      if (!sex && !breed) {
        throw new Error("Provide a sex or breed value to fix.");
      }
      if (sex) body.sex = sex;
      if (breed) body.breed = breed;
    }
    const result = await reviewImportRunRow(
      requiredString(formData, "import_run_id"),
      requiredString(formData, "import_row_id"),
      body,
      requiredString(formData, "idempotency_key"),
    );
    if (!result.ok) {
      status = "error";
      message = actionErrorMessage(result.error);
    } else {
      message = `Row ${result.data.row.row_number} ${action} applied; state ${result.data.row.row_state}.`;
    }
  } catch (error) {
    status = "error";
    message = error instanceof Error ? error.message : "Unable to apply the row review action.";
  }
  actionRedirect(formData, status, message);
}
