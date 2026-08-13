import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ActionCenterObligation } from "@/lib/api/server";
import { vaccinationDriveDisplayName } from "@/lib/vaccine-display";
import { operationalLocationLabel } from "@/lib/operational-location";

export function actionDriveLabel(
  pageContract: AdminUiPageContract,
  row: Pick<ActionCenterObligation, "drive_name" | "protocol_name" | "dose_code">,
): string {
  const raw = row.drive_name || `${row.protocol_name} ${row.dose_code}` || copy(pageContract, "label.vaccination_drive");
  const cleaned = raw
    .replace(/\s+[-–]\s+PC-[A-Z0-9-]+$/i, "")
    .replace(/\s+[-–]\s+[A-Z]+-[A-Z0-9-]+$/i, "")
    .replace(/\s+/g, " ")
    .trim();
  return vaccinationDriveDisplayName(cleaned) || copy(pageContract, "label.vaccination_drive");
}

export function actionWorkTitle(pageContract: AdminUiPageContract, row: ActionCenterObligation): string {
  const shed = row.operational_location_display || operationalLocationLabel({ shedName: row.shed_name }) || copy(pageContract, "label.shed_fallback");
  if (row.owner_state === "missing" || !row.owner?.operator_name) {
    return `${copy(pageContract, "action.assign_owner_chain")} — ${shed}`;
  }
  if (row.proof_state === "missing") return `${copy(pageContract, "action.capture_vaccination_proof")} — ${shed}`;
  if (row.verification_state === "pending") return `${copy(pageContract, "action.verify_vaccination_proof")} — ${shed}`;
  if (row.work_state === "overdue") return `${actionDriveLabel(pageContract, row)} ${copy(pageContract, "label.overdue_suffix")} — ${shed}`;
  return `${actionDriveLabel(pageContract, row)} — ${shed}`;
}
