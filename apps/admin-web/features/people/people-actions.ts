"use server";

// Write flow for the People/HRMS directory. The create is the whole onboarding:
// the backend mints the Firebase login, materializes the scope grant, admits the
// email through the allowlist, and inserts the roster row in one idempotent
// call. No optimistic success: the backend response drives the banner, so a
// duplicate email or an unavailable account service is what the admin sees.
import { revalidatePath } from "next/cache";
import { actionRedirect, optionalString, requiredString } from "@/lib/action-helpers";
// NOTE: every actionKey below MUST start with "action." and have matching
// page-contract copy — actionFeedbackCopy throws on a missing key.
import {
  createWorkforcePerson,
  setWorkforcePersonStatus,
  type CreateWorkforcePersonRequest,
} from "@/lib/api/server";

const PEOPLE_PATH = "/people";

export async function createPersonAction(formData: FormData): Promise<void> {
  // Minted once when the drawer opened, so a double-click or a network retry of
  // the same form submission converges on ONE person.
  const idempotencyKey = requiredString(formData, "idempotency_key");

  const body: CreateWorkforcePersonRequest = {
    first_name: requiredString(formData, "first_name"),
    email: requiredString(formData, "email"),
    role: requiredString(formData, "role") as CreateWorkforcePersonRequest["role"],
  };
  const lastName = optionalString(formData, "last_name");
  if (lastName) body.last_name = lastName;
  // Blank selects are OMITTED, not sent as "": the backend validates present
  // fields as UUIDs/enums and an empty string would be rejected.
  const parkId = optionalString(formData, "park_id");
  if (parkId) body.park_id = parkId;
  const departmentId = optionalString(formData, "department_id");
  if (departmentId) body.department_id = departmentId;
  const grade = optionalString(formData, "designation_grade");
  if (grade) body.designation_grade = grade as CreateWorkforcePersonRequest["designation_grade"];

  const result = await createWorkforcePerson(idempotencyKey, body);
  if (!result.ok) {
    const code = result.error.code;
    actionRedirect(
      formData,
      "error",
      code === "duplicate_email"
        ? "action.person_duplicate"
        : code === "identity_unavailable"
          ? "action.identity_unavailable"
          : "action.person_create_failed",
    );
  }
  revalidatePath(PEOPLE_PATH);
  // Three honest outcomes: fresh login (convention password), Google-SSO
  // account that just gained the convention password, or an account with its
  // own password that the flow deliberately left untouched.
  const accountStatus = result.data.login.account_status;
  actionRedirect(
    formData,
    "success",
    accountStatus === "existing"
      ? "action.person_created_existing"
      : accountStatus === "existing_password_added"
        ? "action.person_created_password_added"
        : "action.person_created",
  );
}

/**
 * Activate/deactivate a person from the drawer's confirm step. Uses the existing
 * operator status routes; row_version is the fence read with the row, so a
 * concurrent edit is refused rather than silently overwritten.
 */
export async function changePersonStatusAction(formData: FormData): Promise<void> {
  const personId = requiredString(formData, "person_id");
  const target = requiredString(formData, "target_status");
  const rowVersion = Number(formData.get("row_version")?.toString() ?? "0");
  if (target !== "activate" && target !== "deactivate") {
    actionRedirect(formData, "error", "action.person_status_failed");
  }

  const result = await setWorkforcePersonStatus(personId, target as "activate" | "deactivate", {
    reason: "people_hrms_admin_action",
    row_version: rowVersion,
  });
  if (!result.ok) {
    actionRedirect(formData, "error", "action.person_status_failed");
  }
  revalidatePath(PEOPLE_PATH);
  actionRedirect(
    formData,
    "success",
    target === "deactivate" ? "action.person_deactivated" : "action.person_activated",
  );
}
