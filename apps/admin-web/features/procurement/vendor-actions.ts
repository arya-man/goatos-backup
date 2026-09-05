"use server";

// Write flows for the procurement vendor register. Each action calls the backend endpoint and
// redirects back with a banner. No optimistic success: the backend response (or its error envelope)
// drives the message, so a rejected duplicate or a stale-write conflict is what the operator sees.
import { revalidatePath } from "next/cache";
import { getProofDownloadUrl } from "@/lib/api/server";
import { actionRedirect, optionalString, requiredString } from "@/lib/action-helpers";
// NOTE: every actionKey below MUST start with "action." -- withActionFeedback silently rewrites
// anything else to "action.error_form", which would report a generic failure after a SUCCESSFUL
// save. Each key also needs matching page-contract copy, because actionFeedbackCopy throws on a
// missing key and takes the whole page down with it.
import {
  createProcurementVendor,
  updateProcurementVendor,
  updateProcurementVendorStatus,
  type ProcurementVendorWrite,
} from "@/lib/api/server";

/**
 * BOTH pages that render the register, because a write reaches the same table either way.
 *
 * Revalidating only the page the form was submitted from would leave the other side showing a
 * stale list -- and the two are not disjoint in practice: re-categorising a vendor from a supply
 * type to a buyer type MOVES it between them, so exactly the write that changes what one page
 * lists is the write that changes what the other lists too.
 */
const VENDOR_LIST_PATHS = ["/procurement/vendors", "/sales/vendors"];

/**
 * Reads the shared vendor fields off the form.
 *
 * Optional fields are sent as "" rather than omitted, because the write is a REPLACE: a cleared
 * field must reach the backend as an explicit empty value so it stores NULL. Omitting it would be
 * indistinguishable from "leave as is" only if the API were a patch -- it is not.
 */
function readVendorForm(formData: FormData): ProcurementVendorWrite {
  const parseOptionalInt = (raw: string | undefined): number | null => {
    const trimmed = (raw ?? "").trim();
    // A CLEARED number must be null, never 0: 0 filtered stock is a real reading that means
    // "checked, none available", and coercing blank to 0 would invent that reading.
    if (trimmed === "") return null;
    const parsed = Number(trimmed);
    return Number.isFinite(parsed) ? Math.trunc(parsed) : null;
  };

  return {
    record_type: requiredString(formData, "record_type"),
    business_name: requiredString(formData, "business_name"),
    // The backend re-validates this against its own enum; the cast only satisfies the generated
    // client's literal union, it is not a trust boundary.
    status: requiredString(formData, "status") as ProcurementVendorWrite["status"],
    state: requiredString(formData, "state"),
    contact_person_name: optionalString(formData, "contact_person_name") ?? "",
    phone_number: optionalString(formData, "phone_number") ?? "",
    breed: optionalString(formData, "breed") ?? "",
    feed: optionalString(formData, "feed") ?? "",
    ready_to_filtered: optionalString(formData, "ready_to_filtered") ?? "",
    details: optionalString(formData, "details") ?? "",
    city: optionalString(formData, "city") ?? "",
    bank_name: optionalString(formData, "bank_name") ?? "",
    account_no: optionalString(formData, "account_no") ?? "",
    ifsc_code: optionalString(formData, "ifsc_code") ?? "",
    upi_id: optionalString(formData, "upi_id") ?? "",
    pan_number: optionalString(formData, "pan_number") ?? "",
    comments: optionalString(formData, "comments") ?? "",
    filtered_stock: parseOptionalInt(formData.get("filtered_stock")?.toString()),
    eta_after_order_days: parseOptionalInt(formData.get("eta_after_order_days")?.toString()),
    // Sent verbatim as a string so the backend validates the amount; parsing to a float here would
    // be the money-through-float round trip the contract avoids.
    price_per_goat: optionalString(formData, "price_per_goat") ?? null,
    // Capacity travels as a pair (the backend refuses one half without the other); the quantity is
    // a decimal string for the same no-float reason as the price.
    capacity_quantity: optionalString(formData, "capacity_quantity") ?? null,
    capacity_unit: optionalString(formData, "capacity_unit") ?? "",
    supply_frequency: optionalString(formData, "supply_frequency") ?? "",
    // The web form cannot record audio; it carries the note the phone recorded so an edit here
    // does not silently drop it (the write is a REPLACE).
    voice_note_proof_ref: optionalString(formData, "voice_note_proof_ref") ?? "",
  };
}

export async function createVendorAction(formData: FormData): Promise<void> {
  const result = await createProcurementVendor(readVendorForm(formData));
  if (!result.ok) {
    // actionRedirect derives the return path from the form's return_to field and appends the
    // banner params, so the operator lands back on the list they came from.
    actionRedirect(formData, "error", "action.vendor_save_failed");
  }
  for (const path of VENDOR_LIST_PATHS) revalidatePath(path);
  actionRedirect(formData, "success", "action.vendor_created");
}

export async function updateVendorAction(formData: FormData): Promise<void> {
  const vendorId = requiredString(formData, "vendor_id");
  const body = readVendorForm(formData);
  // The fence the operator's own read handed us. Sending 0 would be refused by the backend rather
  // than silently overwriting, which is the intended failure.
  body.row_version = Number(formData.get("row_version")?.toString() ?? "0");

  const result = await updateProcurementVendor(vendorId, body);
  if (!result.ok) {
    actionRedirect(formData, "error", "action.vendor_save_failed");
  }
  for (const path of VENDOR_LIST_PATHS) revalidatePath(path);
  actionRedirect(formData, "success", "action.vendor_updated");
}

/**
 * Quick status change from the vendor drawer.
 *
 * Uses the narrow status endpoint rather than the full update, so a caller who cannot see the
 * payment fields cannot wipe them by flipping a vendor to inactive.
 */
export async function changeVendorStatusAction(formData: FormData): Promise<void> {
  const vendorId = requiredString(formData, "vendor_id");
  const status = requiredString(formData, "status");
  const rowVersion = Number(formData.get("row_version")?.toString() ?? "0");

  const result = await updateProcurementVendorStatus(vendorId, { status, row_version: rowVersion });
  if (!result.ok) {
    actionRedirect(formData, "error", "action.vendor_status_failed");
  }
  for (const path of VENDOR_LIST_PATHS) revalidatePath(path);
  actionRedirect(formData, "success", "action.vendor_status_changed");
}

/**
 * Resolves the short-lived signed download URL of a vendor's voice note for the drawer's player.
 * Returns null when the proof cannot be served right now, so the player degrades to copy.
 */
export async function resolveVendorVoiceNoteUrl(proofRef: string): Promise<string | null> {
  const trimmed = proofRef.trim();
  if (!trimmed) return null;
  return getProofDownloadUrl(trimmed);
}
